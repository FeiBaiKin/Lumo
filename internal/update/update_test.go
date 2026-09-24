package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.1.9", "0.1.8", 1},
		{"v0.1.10", "0.1.9", 1},
		{"1.0.0", "1.0.0", 0},
		{"1.0.0-rc.1", "1.0.0", -1},
		{"1.0.0-rc.2", "1.0.0-rc.10", -1},
		{"1.0.0-alpha", "1.0.0-alpha.1", -1},
		{"1.0.0+build.5", "1.0.0", 0},
	}
	for _, tc := range cases {
		a, okA := ParseVersion(tc.a)
		b, okB := ParseVersion(tc.b)
		if !okA || !okB {
			t.Fatalf("解析 %q / %q 失败", tc.a, tc.b)
		}
		if got := CompareVersions(a, b); got != tc.want {
			t.Errorf("CompareVersions(%s, %s) = %d，应为 %d", tc.a, tc.b, got, tc.want)
		}
	}
	for _, bad := range []string{"", "v", "1.2.3.4", "1.x.0", "latest"} {
		if _, ok := ParseVersion(bad); ok {
			t.Errorf("%q 不是合法版本，却解析成功了", bad)
		}
	}
}

// 能不能就地升级取决于程序目录归哪个挂载点管：最长匹配、只认真正的子路径。
func TestParseMountInfo(t *testing.T) {
	// 1Panel 运行环境：容器根是 overlay，站点目录从宿主机挂进来
	panel := "22 1 0:20 / / rw - overlay overlay rw\n" +
		"30 22 8:1 /opt/1panel/www /wwwroot rw,relatime - ext4 /dev/sda1 rw\n" +
		"31 22 0:25 / /wwwrootx rw - tmpfs tmpfs rw\n"
	if point, fstype, ok := parseMountInfo(panel, "/wwwroot/sites/a/index"); !ok || point != "/wwwroot" || fstype != "ext4" {
		t.Fatalf("挂载目录下的程序应归 /wwwroot（ext4），得到 %q %q %v", point, fstype, ok)
	}
	if point, _, _ := parseMountInfo(panel, "/wwwrootx/app"); point != "/wwwrootx" {
		t.Fatalf("/wwwrootx 不是 /wwwroot 的子路径，得到 %q", point)
	}
	if point, fstype, _ := parseMountInfo(panel, "/usr/local/bin"); point != "/" || fstype != "overlay" {
		t.Fatalf("镜像层里的程序应归容器根（overlay），得到 %q %q", point, fstype)
	}
	// 挂载点里的空格按八进制转义写在表里
	escaped := "40 22 8:1 / /mnt/my\\040apps rw - ext4 /dev/sdb1 rw\n"
	if point, _, ok := parseMountInfo(escaped, "/mnt/my apps/lumo"); !ok || point != "/mnt/my apps" {
		t.Fatalf("转义过的挂载点没认出来：%q %v", point, ok)
	}
}

func TestDeleteBackupRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(filepath.Dir(dir), "keep.txt")
	if err := os.WriteFile(outside, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(outside) }()
	for _, name := range []string{"../keep.txt", "..", "a/b", `..\keep.txt`, "keep.txt"} {
		if err := DeleteBackup(dir, name); !errors.Is(err, ErrBadBackupName) {
			t.Errorf("备份名 %q 应被拒绝，得到 %v", name, err)
		}
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatal("目录外的文件被删掉了")
	}
}

// 校验和是升级的第一道闸：没有清单不装，对不上不装，对不上的文件不留在盘上。
func TestFetchVerifiesChecksum(t *testing.T) {
	archive := []byte("pretend this is lumo_9.9.9_linux_amd64.tar.gz")
	sum := sha256.Sum256(archive)
	good := hex.EncodeToString(sum[:])

	checksums := good + "  lumo.tar.gz\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/lumo.tar.gz":
			_, _ = w.Write(archive)
		case "/checksums.txt":
			_, _ = w.Write([]byte(checksums))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	d := NewDownloader("", "lumo-test", nil)
	release := func() *Release {
		return &Release{
			Asset:     Asset{Name: "lumo.tar.gz", URL: srv.URL + "/lumo.tar.gz"},
			Checksums: Asset{Name: "checksums.txt", URL: srv.URL + "/checksums.txt"},
		}
	}

	dir := t.TempDir()
	path, err := d.Fetch(context.Background(), release(), dir, nil)
	if err != nil {
		t.Fatalf("校验和对得上却失败了：%v", err)
	}
	if data, _ := os.ReadFile(path); !bytes.Equal(data, archive) {
		t.Fatal("下载内容不对")
	}

	noSums := release()
	noSums.Checksums = Asset{}
	if _, err := d.Fetch(context.Background(), noSums, t.TempDir(), nil); err == nil {
		t.Fatal("没有 checksums.txt 的发布应被拒绝")
	}

	checksums = "0000000000000000000000000000000000000000000000000000000000000000  lumo.tar.gz\n"
	badDir := t.TempDir()
	if _, err := d.Fetch(context.Background(), release(), badDir, nil); !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("校验和不符应返回 ErrChecksumMismatch，得到 %v", err)
	}
	if _, err := os.Stat(filepath.Join(badDir, "lumo.tar.gz")); err == nil {
		t.Fatal("校验失败的文件留在了盘上")
	}

	checksums = good + "  other-file.tar.gz\n"
	if _, err := d.Fetch(context.Background(), release(), t.TempDir(), nil); err == nil {
		t.Fatal("清单里没有这个包时应被拒绝")
	}
}
