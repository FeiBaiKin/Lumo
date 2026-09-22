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
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
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
)

// ErrChecksumMismatch 表示下载内容与发布方公布的校验和不符。
var ErrChecksumMismatch = errors.New("下载的文件与校验和不符")

// Downloader 取回发布资产并验明正身。
type Downloader struct {
	client    *http.Client
	token     string
	userAgent string
}

// NewDownloader 构造下载器。
func NewDownloader(token, userAgent string) *Downloader {
	return &Downloader{
		client: &http.Client{
			Transport: &http.Transport{
				// 走环境里的代理设置：更新源在境外，不少部署靠代理才能访问。
				Proxy:                 http.ProxyFromEnvironment,
				ResponseHeaderTimeout: responseHeaderTimeout,
				TLSHandshakeTimeout:   tlsHandshakeTimeout,
			},
		},
		token:     token,
		userAgent: userAgent,
	}
}

// Fetch 把发布包下载到 dir 下并核对校验和，返回归档文件的路径。
//
// 校验和不是可选步骤：没有 checksums.txt 就直接拒绝安装。HTTPS 只保证
// 「东西是从 GitHub 来的」，校验和才保证「是发布者打出来的那一份」——
// 而这个文件接下来要被当作本机的主程序执行。
func (d *Downloader) Fetch(ctx context.Context, rel *Release, dir string, onProgress func(done, total int64)) (string, error) {
	if rel.Asset.URL == "" {
		return "", ErrNoAsset
	}
	if rel.Checksums.URL == "" {
		return "", errors.New("该版本没有提供 checksums.txt，无法校验下载内容，已中止")
	}

	sums, err := d.fetchChecksums(ctx, rel.Checksums)
	if err != nil {
		return "", err
	}
	want, ok := sums[rel.Asset.Name]
	if !ok {
		return "", fmt.Errorf("校验和清单里没有 %s，已中止", rel.Asset.Name)
	}

	dst := filepath.Join(dir, rel.Asset.Name)
	sum, err := d.download(ctx, rel.Asset, dst, onProgress)
	if err != nil {
		_ = os.Remove(dst)
		return "", err
	}
	if !strings.EqualFold(sum, want) {
		_ = os.Remove(dst)
		return "", fmt.Errorf("%w：期望 %s，实际 %s", ErrChecksumMismatch, want, sum)
	}
	return dst, nil
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

// download 下载到 dst 并返回内容的 sha256。
func (d *Downloader) download(ctx context.Context, asset Asset, dst string, onProgress func(done, total int64)) (string, error) {
	resp, err := d.open(ctx, asset.URL)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	total := asset.Size
	if total <= 0 {
		total = resp.ContentLength
	}
	if total > maxArchiveBytes {
		return "", fmt.Errorf("发布包超过 %d MB，已中止", maxArchiveBytes>>20)
	}

	f, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", fmt.Errorf("创建下载文件: %w", err)
	}
	defer func() { _ = f.Close() }()

	hasher := sha256.New()
	counter := &progressWriter{total: total, onProgress: onProgress}
	// 多写一份到 hasher 与计数器：文件只读一遍，校验和与进度顺带算出来。
	written, err := io.Copy(io.MultiWriter(f, hasher, counter), io.LimitReader(resp.Body, maxArchiveBytes+1))
	if err != nil {
		return "", fmt.Errorf("下载发布包: %w", err)
	}
	if written > maxArchiveBytes {
		return "", fmt.Errorf("发布包超过 %d MB，已中止", maxArchiveBytes>>20)
	}
	if err := f.Sync(); err != nil {
		return "", fmt.Errorf("写入下载文件: %w", err)
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// open 发起一次下载请求并检查状态码。
func (d *Downloader) open(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("构造下载请求: %w", err)
	}
	req.Header.Set("User-Agent", d.userAgent)
	req.Header.Set("Accept", "application/octet-stream")
	if d.token != "" {
		// 资产下载会重定向到另一个域名，Go 的客户端在跨站重定向时会主动
		// 剥掉 Authorization，故这里带上令牌不会把它送去第三方。
		req.Header.Set("Authorization", "Bearer "+d.token)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("下载失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("下载失败（HTTP %d）", resp.StatusCode)
	}
	return resp, nil
}

// progressWriter 在写入过程中汇报进度。
type progressWriter struct {
	done       int64
	total      int64
	onProgress func(done, total int64)
	// last 用于节流：几十 MB 的下载每 32 KB 回调一次，一秒能有上千次，
	// 而进度只需要能看出在动。
	last time.Time
}

func (w *progressWriter) Write(p []byte) (int, error) {
	w.done += int64(len(p))
	if w.onProgress != nil && time.Since(w.last) > 200*time.Millisecond {
		w.last = time.Now()
		w.onProgress(w.done, w.total)
	}
	return len(p), nil
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
