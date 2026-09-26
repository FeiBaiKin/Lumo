package update

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

// 下载与解包的限额。
//
// 发布包是「一个静态二进制 + 两个文本文件」，几十 MB 量级；留到 256 MB
// 是给将来内嵌更多资产留余地，同时仍挡得住「下载源被换成一个无底洞」。
const (
	maxArchiveBytes  int64 = 256 << 20
	maxBinaryBytes   int64 = 256 << 20
	maxChecksumBytes int64 = 1 << 20
	// responseHeaderTimeout 只约束「服务端多久开始回话」，不约束整体下载时长——
	// 整体时长由调用方的 context 控制，否则慢网络上的大文件永远下不完。
	responseHeaderTimeout = 30 * time.Second
	tlsHandshakeTimeout   = 15 * time.Second
	// dialTimeout 是建立一条连接的总期限（含 DNS 解析）。域名解析出多个地址时，
	// Go 把它分给各地址轮流试；不设的话，一个不通的地址要等内核放弃
	// （Linux 约 127 秒）才轮到下一个，进度条就停在 0 好几分钟。
	dialTimeout = 20 * time.Second
	// connectAttempts 是没拿到响应就失败时的总尝试次数：丢包严重或握手被干扰的线路上，
	// 换一条连接往往就通了。
	connectAttempts   = 3
	connectRetryDelay = 2 * time.Second
)

// ErrChecksumMismatch 表示下载内容与发布方公布的校验和不符。
var ErrChecksumMismatch = errors.New("下载的文件与校验和不符")

// Downloader 取回发布资产并验明正身。
type Downloader struct {
	client    *http.Client
	token     string
	userAgent string
	logger    *slog.Logger
}

// NewDownloader 构造下载器。
func NewDownloader(token, userAgent string, logger *slog.Logger) *Downloader {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	dialer := &net.Dialer{Timeout: dialTimeout}
	return &Downloader{
		client: &http.Client{
			Transport: &http.Transport{
				// 走环境里的代理设置：更新源在境外，不少部署靠代理才能访问。
				Proxy:       http.ProxyFromEnvironment,
				DialContext: dialer.DialContext,
				// 自定义了 DialContext，HTTP/2 就不再自动启用，这里显式打开。
				ForceAttemptHTTP2:     true,
				ResponseHeaderTimeout: responseHeaderTimeout,
				TLSHandshakeTimeout:   tlsHandshakeTimeout,
			},
		},
		token:     token,
		userAgent: userAgent,
		logger:    logger,
	}
}

// 多来源下载的参数。
const (
	// probeBytes 是测速时每个来源先下的字节数：够看出快慢，又不至于白下太多。
	probeBytes = 256 << 10
	// probeTimeout 是一次测速的期限，超时的来源这一轮不用。
	probeTimeout = 10 * time.Second
	// parallelParts 是分段下载的连接数：国内线路常按单条连接限速，几条并行能叠起来。
	parallelParts = 4
	// minPartBytes 是每段的下限，发布包小于两段时整个一次下。
	minPartBytes = 1 << 20
	// stallTimeout 是一条连接多久没收到数据就算卡住，断开重试。
	stallTimeout = 30 * time.Second
	// partAttempts 是一段下载的总尝试次数，重试从断点接着下。
	partAttempts = 3
)

// FetchHooks 是下载过程中给调用方的回调，都可以为 nil。
type FetchHooks struct {
	// Source 在开始用某个下载来源时调用，参数是它的域名。
	Source func(name string)
	// Progress 汇报这个来源已下载与总共的字节数，已节流；换来源时从 0 重新算。
	Progress func(done, total int64)
}

// source 是一个下载来源：某个加速地址，或 GitHub 直连。
type source struct {
	name   string
	url    string
	direct bool
	// ranged 为真表示它按字节区间返回，可以分段并行下载。
	ranged bool
	// took 是测速那一小段的用时。
	took time.Duration
}

// Fetch 把发布包下载到 dir 下并核对校验和，返回归档文件的路径。
//
// 校验和不是可选步骤。优先用 GitHub 接口给的 SHA-256：它和资产列表出自同一次接口调用，
// 发布包从加速地址下载时，加速地址改不了这个值；老的发布没有这个字段，退回 checksums.txt，
// 且只从 GitHub 直连取。HTTPS 只保证「东西是从那台服务器来的」，校验和才保证「是发布者打出来的那一份」——
// 这个文件接下来要被当作本机的主程序执行。
//
// 来源是加速地址加上 GitHub 直连：先各下一小段测速，按快慢依次试，前一个失败或内容对不上就换下一个。
func (d *Downloader) Fetch(ctx context.Context, rel *Release, dir string, mirrors []string, hooks FetchHooks) (string, error) {
	if rel.Asset.URL == "" {
		return "", ErrNoAsset
	}
	if rel.Asset.Size > maxArchiveBytes {
		return "", fmt.Errorf("发布包超过 %d MB，已中止", maxArchiveBytes>>20)
	}
	want, err := d.expectedSum(ctx, rel)
	if err != nil {
		return "", err
	}

	dst := filepath.Join(dir, rel.Asset.Name)
	var failures []error
	for _, src := range d.rank(ctx, rel.Asset, mirrors) {
		if hooks.Source != nil {
			hooks.Source(src.name)
		}
		err := d.fetchFrom(ctx, &src, rel.Asset, dst, want, hooks.Progress)
		if err == nil {
			return dst, nil
		}
		_ = os.Remove(dst)
		if ctx.Err() != nil {
			return "", fmt.Errorf("下载失败: %w", err)
		}
		d.logger.Warn("下载来源失败，换下一个", slog.String("source", src.name), slog.Any("error", err))
		failures = append(failures, fmt.Errorf("%s：%w", src.name, err))
	}
	return "", fmt.Errorf("所有下载来源都失败了：%w", errors.Join(failures...))
}

// expectedSum 取发布包应有的 SHA-256。
func (d *Downloader) expectedSum(ctx context.Context, rel *Release) (string, error) {
	if rel.Asset.SHA256 != "" {
		return rel.Asset.SHA256, nil
	}
	if rel.Checksums.URL == "" {
		return "", errors.New("该版本既没有 GitHub 给出的摘要，也没有 checksums.txt，无法校验下载内容，已中止")
	}
	sums, err := d.fetchChecksums(ctx, rel.Checksums)
	if err != nil {
		return "", err
	}
	want, ok := sums[rel.Asset.Name]
	if !ok {
		return "", fmt.Errorf("校验和清单里没有 %s，已中止", rel.Asset.Name)
	}
	return strings.ToLower(want), nil
}

// fetchChecksums 取回校验和清单，解析成「文件名 → sha256」。
func (d *Downloader) fetchChecksums(ctx context.Context, asset Asset) (map[string]string, error) {
	resp, err := d.open(ctx, asset.URL)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxChecksumBytes))
	if err != nil {
		return nil, fmt.Errorf("读取校验和清单: %w", err)
	}

	sums := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		// 清单里的文件名可能带星号前缀（sha256sum 的二进制模式）。
		sums[strings.TrimPrefix(fields[1], "*")] = fields[0]
	}
	if len(sums) == 0 {
		return nil, errors.New("校验和清单是空的，已中止")
	}
	return sums, nil
}

// rank 同时向各来源要开头一小段，按用时从快到慢排；测速失败的来源这一轮不用，
// GitHub 直连例外，总是留作最后的退路。
func (d *Downloader) rank(ctx context.Context, asset Asset, mirrors []string) []source {
	candidates := make([]source, 0, len(mirrors)+1)
	for _, m := range mirrors {
		candidates = append(candidates, source{name: hostOf(m), url: m + asset.URL})
	}
	candidates = append(candidates, source{name: hostOf(asset.URL), url: asset.URL, direct: true})

	ok := make([]bool, len(candidates))
	var wg sync.WaitGroup
	for i := range candidates {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok[i] = d.probe(ctx, &candidates[i], asset.Size)
		}()
	}
	wg.Wait()

	ranked := make([]source, 0, len(candidates))
	var fallback []source
	for i := range candidates {
		switch {
		case ok[i]:
			ranked = append(ranked, candidates[i])
		case candidates[i].direct:
			fallback = append(fallback, candidates[i])
		default:
			d.logger.Info("下载来源测速失败，这次不用", slog.String("source", candidates[i].name))
		}
	}
	sort.SliceStable(ranked, func(a, b int) bool { return ranked[a].took < ranked[b].took })
	return append(ranked, fallback...)
}

// probe 下载开头一小段测速，顺带看来源认不认字节区间。
func (d *Downloader) probe(ctx context.Context, src *source, size int64) bool {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	began := time.Now()
	resp, err := d.request(ctx, src, fmt.Sprintf("bytes=0-%d", probeBytes-1))
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusPartialContent:
		_, total, ok := parseContentRange(resp.Header.Get("Content-Range"))
		src.ranged = ok && size > 0 && total == size
	case http.StatusOK:
	default:
		return false
	}
	if _, err := io.Copy(io.Discard, io.LimitReader(resp.Body, probeBytes)); err != nil {
		return false
	}
	src.took = time.Since(began)
	return true
}

// fetchFrom 从一个来源下载整个发布包并核对摘要。
func (d *Downloader) fetchFrom(ctx context.Context, src *source, asset Asset, dst, want string, onProgress func(done, total int64)) error {
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("创建下载文件: %w", err)
	}
	defer func() { _ = f.Close() }()

	counter := &progress{total: asset.Size, report: onProgress}
	counter.add(0)
	if src.ranged && asset.Size >= 2*minPartBytes {
		err = d.fetchParts(ctx, src, asset.Size, f, counter)
	} else {
		err = d.fetchWhole(ctx, src, f, counter)
	}
	if err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return fmt.Errorf("写入下载文件: %w", err)
	}
	sum, err := fileSHA256(f)
	if err != nil {
		return err
	}
	if !strings.EqualFold(sum, want) {
		return fmt.Errorf("%w：期望 %s，实际 %s", ErrChecksumMismatch, want, sum)
	}
	return nil
}

// fetchParts 把发布包分成几段，各开一条连接同时下，写到文件各自的位置。
func (d *Downloader) fetchParts(ctx context.Context, src *source, size int64, f *os.File, counter *progress) error {
	if err := f.Truncate(size); err != nil {
		return fmt.Errorf("创建下载文件: %w", err)
	}
	partSize := max((size+parallelParts-1)/parallelParts, minPartBytes)
	g, gctx := errgroup.WithContext(ctx)
	for start := int64(0); start < size; start += partSize {
		end := min(start+partSize, size) - 1
		g.Go(func() error { return d.fetchPart(gctx, src, f, start, end, counter) })
	}
	return g.Wait()
}

// fetchPart 下载 [start, end] 这一段，断了从断点接着下。
func (d *Downloader) fetchPart(ctx context.Context, src *source, f *os.File, start, end int64, counter *progress) error {
	offset := start
	var lastErr error
	for range partAttempts {
		n, err := d.copyRange(ctx, src, f, offset, end, counter)
		offset += n
		if offset > end {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(connectRetryDelay):
		}
	}
	return fmt.Errorf("第 %d–%d 字节下不下来: %w", start, end, lastErr)
}

// copyRange 请求 [offset, end] 并写到文件对应位置，返回写入的字节数。
func (d *Downloader) copyRange(ctx context.Context, src *source, f *os.File, offset, end int64, counter *progress) (int64, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stall := time.AfterFunc(stallTimeout, cancel)
	defer stall.Stop()

	resp, err := d.request(ctx, src, fmt.Sprintf("bytes=%d-%d", offset, end))
	if err != nil {
		return 0, stallError(ctx, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if first, _, ok := parseContentRange(resp.Header.Get("Content-Range")); resp.StatusCode != http.StatusPartialContent || !ok || first != offset {
		return 0, fmt.Errorf("来源没有按区间返回（HTTP %d）", resp.StatusCode)
	}

	buf := make([]byte, 32<<10)
	var written int64
	for {
		remaining := end - offset - written + 1
		if remaining <= 0 {
			return written, nil
		}
		n, err := resp.Body.Read(buf[:min(int64(len(buf)), remaining)])
		if n > 0 {
			stall.Reset(stallTimeout)
			if _, werr := f.WriteAt(buf[:n], offset+written); werr != nil {
				return written, fmt.Errorf("写入下载文件: %w", werr)
			}
			written += int64(n)
			counter.add(int64(n))
		}
		switch {
		case errors.Is(err, io.EOF):
			if offset+written > end {
				return written, nil
			}
			return written, io.ErrUnexpectedEOF
		case err != nil:
			return written, stallError(ctx, err)
		}
	}
}

// fetchWhole 用一条连接从头下到尾，来源不认字节区间或发布包很小时用它；失败了从头重来。
func (d *Downloader) fetchWhole(ctx context.Context, src *source, f *os.File, counter *progress) error {
	var lastErr error
	for attempt := range partAttempts {
		if attempt > 0 {
			if err := f.Truncate(0); err != nil {
				return fmt.Errorf("写入下载文件: %w", err)
			}
			counter.restart()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(connectRetryDelay):
			}
		}
		lastErr = d.copyWhole(ctx, src, f, counter)
		if lastErr == nil || ctx.Err() != nil {
			return lastErr
		}
	}
	return lastErr
}

func (d *Downloader) copyWhole(ctx context.Context, src *source, f *os.File, counter *progress) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stall := time.AfterFunc(stallTimeout, cancel)
	defer stall.Stop()

	resp, err := d.request(ctx, src, "")
	if err != nil {
		return stallError(ctx, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载失败（HTTP %d）", resp.StatusCode)
	}

	buf := make([]byte, 32<<10)
	var written int64
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			stall.Reset(stallTimeout)
			if written+int64(n) > maxArchiveBytes {
				return fmt.Errorf("发布包超过 %d MB，已中止", maxArchiveBytes>>20)
			}
			if _, werr := f.WriteAt(buf[:n], written); werr != nil {
				return fmt.Errorf("写入下载文件: %w", werr)
			}
			written += int64(n)
			counter.add(int64(n))
		}
		switch {
		case errors.Is(err, io.EOF):
			return nil
		case err != nil:
			return stallError(ctx, err)
		}
	}
}

// stallError 把「卡住被掐断」说清楚，不然看到的只是一句 context canceled。
func stallError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return fmt.Errorf("%s 内没有收到数据: %w", stallTimeout, err)
	}
	return err
}

// request 向一个来源发一次请求，byteRange 为空时要整个文件。
func (d *Downloader) request(ctx context.Context, src *source, byteRange string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src.url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("构造下载请求: %w", err)
	}
	req.Header.Set("User-Agent", d.userAgent)
	req.Header.Set("Accept", "application/octet-stream")
	if byteRange != "" {
		req.Header.Set("Range", byteRange)
	}
	// 令牌只给 GitHub：加速地址是第三方，站长的令牌不能交给它。跳转到别的域名时 Go 的客户端也会剥掉它。
	if src.direct && d.token != "" {
		req.Header.Set("Authorization", "Bearer "+d.token)
	}
	return d.client.Do(req)
}

// open 发起下载请求并检查状态码，只用于 GitHub 直连。
//
// 没拿到响应的失败（DNS、建连、TLS 握手被断或超时、等响应头超时）换一条新连接重试；
// 拿到了响应的失败（HTTP 错误码）重试也没用，直接返回。
func (d *Downloader) open(ctx context.Context, rawURL string) (*http.Response, error) {
	direct := &source{name: hostOf(rawURL), url: rawURL, direct: true}
	for attempt := 1; ; attempt++ {
		resp, err := d.request(ctx, direct, "")
		if err == nil {
			if resp.StatusCode != http.StatusOK {
				_ = resp.Body.Close()
				return nil, fmt.Errorf("下载失败（HTTP %d）", resp.StatusCode)
			}
			return resp, nil
		}
		if ctx.Err() != nil {
			return nil, fmt.Errorf("下载失败: %w", err)
		}
		if attempt == connectAttempts {
			return nil, connectFailure(err, attempt)
		}
		d.logger.Warn("连接下载源失败，稍后重试", slog.Int("attempt", attempt), slog.Any("error", err))
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("下载失败: %w", err)
		case <-time.After(connectRetryDelay):
		}
	}
}

// connectFailure 说明连接失败：连的是哪个域名、试了几次、底层原因。
// GitHub 的下载会重定向到 CDN，失败的往往是跳转之后的那个域名，url.Error 里记的正是它。
func connectFailure(err error, attempts int) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		if u, parseErr := url.Parse(urlErr.URL); parseErr == nil && u.Host != "" {
			return fmt.Errorf("连不上 %s（已尝试 %d 次）: %w", u.Host, attempts, urlErr.Err)
		}
	}
	return fmt.Errorf("下载失败（已尝试 %d 次）: %w", attempts, err)
}

// progress 汇总几条连接的下载量，节流后回调：几十 MB 每 32 KB 回调一次，一秒能有上千次，
// 而进度只需要能看出在动。
type progress struct {
	mu     sync.Mutex
	done   int64
	total  int64
	last   time.Time
	report func(done, total int64)
}

func (p *progress) add(n int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.done += n
	if p.report != nil && (n == 0 || time.Since(p.last) > 200*time.Millisecond) {
		p.last = time.Now()
		p.report(p.done, p.total)
	}
}

func (p *progress) restart() {
	p.mu.Lock()
	p.done = 0
	p.mu.Unlock()
	p.add(0)
}

// fileSHA256 从头读一遍文件算摘要。
func fileSHA256(f *os.File) (string, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("读取下载文件: %w", err)
	}
	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return "", fmt.Errorf("读取下载文件: %w", err)
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// parseContentRange 解析「bytes 起-止/总」，返回起点与总长度。
func parseContentRange(value string) (first, total int64, ok bool) {
	spec, found := strings.CutPrefix(value, "bytes ")
	if !found {
		return 0, 0, false
	}
	span, size, found := strings.Cut(spec, "/")
	if !found {
		return 0, 0, false
	}
	from, _, found := strings.Cut(span, "-")
	if !found {
		return 0, 0, false
	}
	first, err1 := strconv.ParseInt(from, 10, 64)
	total, err2 := strconv.ParseInt(size, 10, 64)
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return first, total, true
}

// hostOf 取网址的域名，作下载来源的名字。
func hostOf(rawURL string) string {
	if u, err := url.Parse(rawURL); err == nil && u.Host != "" {
		return u.Host
	}
	return rawURL
}

// extractBinary 从归档里取出主程序，写到 dst。
//
// 只取一个文件，且按基名匹配：归档里还有 LICENSE 与 README，
// 而包内路径不可信——按基名取，路径穿越这条路从一开始就不存在。
func extractBinary(archivePath, dst string) error {
	lower := strings.ToLower(archivePath)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		return extractFromZip(archivePath, dst)
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		return extractFromTarGz(archivePath, dst)
	default:
		return fmt.Errorf("不认识的发布包格式：%s", filepath.Base(archivePath))
	}
}

func extractFromZip(archivePath, dst string) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("打开发布包: %w", err)
	}
	defer func() { _ = zr.Close() }()

	for _, entry := range zr.File {
		if entry.FileInfo().IsDir() || !isBinaryEntry(entry.Name) {
			continue
		}
		rc, openErr := entry.Open()
		if openErr != nil {
			return fmt.Errorf("读取发布包: %w", openErr)
		}
		err := writeBinary(rc, dst)
		_ = rc.Close()
		return err
	}
	return errBinaryNotFound()
}

func extractFromTarGz(archivePath, dst string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("打开发布包: %w", err)
	}
	defer func() { _ = f.Close() }()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("解压发布包: %w", err)
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	for {
		header, nextErr := tr.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return fmt.Errorf("读取发布包: %w", nextErr)
		}
		if header.Typeflag != tar.TypeReg || !isBinaryEntry(header.Name) {
			continue
		}
		return writeBinary(tr, dst)
	}
	return errBinaryNotFound()
}

// isBinaryEntry 判断归档内的一个条目是不是主程序。
func isBinaryEntry(name string) bool {
	// 归档内一律用斜杠分隔，不能用 filepath.Base：Windows 上它会把反斜杠也当分隔符，
	// 于是一个条目名里的反斜杠会被当成目录层级，基名就取错了。
	base := path.Base(strings.ReplaceAll(name, "\\", "/"))
	return base == binaryName()
}

func errBinaryNotFound() error {
	return fmt.Errorf("发布包里没有找到 %s，已中止", binaryName())
}

// writeBinary 把主程序写到 dst，带长度上限。
func writeBinary(src io.Reader, dst string) error {
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, binaryPerm)
	if err != nil {
		return fmt.Errorf("创建新版本文件: %w", err)
	}
	defer func() { _ = f.Close() }()

	written, err := io.Copy(f, io.LimitReader(src, maxBinaryBytes+1))
	if err != nil {
		return fmt.Errorf("写入新版本文件: %w", err)
	}
	if written > maxBinaryBytes {
		return fmt.Errorf("发布包里的主程序超过 %d MB，已中止", maxBinaryBytes>>20)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("写入新版本文件: %w", err)
	}
	// 解压出来的文件默认没有执行位，Unix 上必须显式补上，否则新版本根本起不来。
	if err := os.Chmod(dst, binaryPerm); err != nil {
		return fmt.Errorf("设置新版本文件权限: %w", err)
	}
	return nil
}
