package update

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// rangeServer 按字节区间提供 data。mutate 非空时改写内容（冒充不老实的加速地址）；
// cut 大于 0 时，前 cut 次非开头的区间请求只发一半就断开（冒充不稳的线路）。
type rangeServer struct {
	*httptest.Server
	data    []byte
	delay   time.Duration
	mutate  bool
	cut     atomic.Int32
	ranged  atomic.Int32
	sawAuth atomic.Bool
}

func newRangeServer(t *testing.T, data []byte) *rangeServer {
	t.Helper()
	s := &rangeServer{data: data}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.Close)
	return s
}

func (s *rangeServer) serve(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "" {
		s.sawAuth.Store(true)
	}
	time.Sleep(s.delay)
	body := s.data
	if s.mutate {
		body = bytes.Clone(body)
		body[len(body)/2] ^= 0xff
	}
	var first, last int64
	if _, err := fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &first, &last); err != nil {
		_, _ = w.Write(body)
		return
	}
	last = min(last, int64(len(body))-1)
	part := body[first : last+1]
	if first > 0 {
		s.ranged.Add(1)
	}
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", first, last, len(body)))
	w.Header().Set("Content-Length", fmt.Sprint(len(part)))
	w.WriteHeader(http.StatusPartialContent)
	if first > 0 && s.cut.Add(-1) >= 0 {
		_, _ = w.Write(part[:len(part)/2])
		panic(http.ErrAbortHandler)
	}
	_, _ = w.Write(part)
}

// mirrorOf 把「前缀 + 原地址」转给 target，就像公共加速服务那样。
func mirrorOf(target *rangeServer) string {
	return target.URL + "/"
}

func randomArchive(t *testing.T, size int) (data []byte, sum string) {
	t.Helper()
	data = make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	return data, hex.EncodeToString(digest[:])
}

// 加速地址测得快就从它分段下；它收不到站长的令牌；内容按 GitHub 给的摘要核对。
func TestFetchFromMirrorInParts(t *testing.T) {
	data, sum := randomArchive(t, 5<<20)
	github := newRangeServer(t, data)
	github.delay = 300 * time.Millisecond
	mirror := newRangeServer(t, data)
	mirror.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 路径是「/原地址」，转给同一份内容
		if !strings.HasPrefix(r.URL.Path, "/http") {
			http.NotFound(w, r)
			return
		}
		mirror.serve(w, r)
	})

	var mu sync.Mutex
	var sources []string
	d := NewDownloader("secret-token", "lumo-test", nil)
	rel := &Release{Asset: Asset{Name: "lumo.tar.gz", URL: github.URL + "/lumo.tar.gz", Size: int64(len(data)), SHA256: sum}}
	path, err := d.Fetch(context.Background(), rel, t.TempDir(), []string{mirrorOf(mirror)}, FetchHooks{
		Source: func(name string) { mu.Lock(); sources = append(sources, name); mu.Unlock() },
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, data) {
		t.Fatal("下载内容不对")
	}
	if len(sources) != 1 || sources[0] != hostOf(mirror.URL) {
		t.Fatalf("应该只用加速地址，用了 %v", sources)
	}
	if mirror.ranged.Load() < parallelParts-1 {
		t.Fatalf("应分段并行下载，非开头的区间请求只有 %d 个", mirror.ranged.Load())
	}
	if mirror.sawAuth.Load() {
		t.Fatal("令牌不能发给加速地址")
	}
	if !github.sawAuth.Load() {
		t.Fatal("GitHub 直连应带上令牌")
	}
}

// 加速地址返回的内容和摘要对不上，换 GitHub 直连，不装那份假的。
func TestFetchFallsBackWhenMirrorLies(t *testing.T) {
	data, sum := randomArchive(t, 3<<20)
	github := newRangeServer(t, data)
	github.delay = 200 * time.Millisecond
	liar := newRangeServer(t, data)
	liar.mutate = true

	var sources []string
	var mu sync.Mutex
	d := NewDownloader("", "lumo-test", nil)
	rel := &Release{Asset: Asset{Name: "lumo.tar.gz", URL: github.URL + "/lumo.tar.gz", Size: int64(len(data)), SHA256: sum}}
	path, err := d.Fetch(context.Background(), rel, t.TempDir(), []string{mirrorOf(liar)}, FetchHooks{
		Source: func(name string) { mu.Lock(); sources = append(sources, name); mu.Unlock() },
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, data) {
		t.Fatal("装的不是 GitHub 上那一份")
	}
	if len(sources) != 2 || sources[1] != hostOf(github.URL) {
		t.Fatalf("应先试加速地址再退回直连，实际 %v", sources)
	}

	// 所有来源都对不上时报校验和不符
	github.mutate = true
	if _, err := d.Fetch(context.Background(), rel, t.TempDir(), []string{mirrorOf(liar)}, FetchHooks{}); !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("应返回 ErrChecksumMismatch，得到 %v", err)
	}
}

// 分段下载时连接断了，从断点接着下，不从头来。
func TestFetchResumesBrokenPart(t *testing.T) {
	data, sum := randomArchive(t, 4<<20)
	github := newRangeServer(t, data)
	github.cut.Store(2)

	var last int64
	d := NewDownloader("", "lumo-test", nil)
	rel := &Release{Asset: Asset{Name: "lumo.tar.gz", URL: github.URL + "/lumo.tar.gz", Size: int64(len(data)), SHA256: sum}}
	path, err := d.Fetch(context.Background(), rel, t.TempDir(), nil, FetchHooks{
		Progress: func(done, _ int64) { atomic.StoreInt64(&last, done) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, data) {
		t.Fatal("续传拼出来的文件不对")
	}
	if got := atomic.LoadInt64(&last); got > int64(len(data)) {
		t.Fatalf("续传不该重复计数，进度到了 %d / %d", got, len(data))
	}
}

func TestNormalizeMirrors(t *testing.T) {
	got, err := NormalizeMirrors([]string{" https://ghfast.top ", "https://ghfast.top/", "", "https://a.example/gh/"})
	if err != nil || strings.Join(got, ",") != "https://ghfast.top/,https://a.example/gh/" {
		t.Fatalf("整理结果不对：%v %v", got, err)
	}
	for _, bad := range []string{"http://ghfast.top/", "ghfast.top", "https://u:p@x.example/", "https://x.example/?a=1", "https:///x"} {
		if _, err := NormalizeMirrors([]string{bad}); !errors.Is(err, ErrInvalidMirror) {
			t.Errorf("%q 应被拒绝，得到 %v", bad, err)
		}
	}
	many := make([]string, maxMirrors+1)
	for i := range many {
		many[i] = fmt.Sprintf("https://m%d.example/", i)
	}
	if _, err := NormalizeMirrors(many); !errors.Is(err, ErrInvalidMirror) {
		t.Fatal("超过上限应被拒绝")
	}
}

// 没改过用内置地址；改成空列表就只直连；恢复默认回到内置地址。
func TestMirrorsFile(t *testing.T) {
	dir := t.TempDir()
	if got, custom := loadMirrors(dir); custom || len(got) != len(DefaultMirrors) {
		t.Fatalf("没改过应是内置地址：%v %v", got, custom)
	}
	if err := saveMirrors(dir, []string{}); err != nil {
		t.Fatal(err)
	}
	if got, custom := loadMirrors(dir); !custom || len(got) != 0 {
		t.Fatalf("存了空列表应只直连：%v %v", got, custom)
	}
	if err := resetMirrors(dir); err != nil {
		t.Fatal(err)
	}
	if _, custom := loadMirrors(dir); custom {
		t.Fatal("恢复默认后应回到内置地址")
	}
}
