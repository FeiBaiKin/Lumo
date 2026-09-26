package update

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// DefaultMirrors 是内置的下载加速地址：发布包在 GitHub 上，国内服务器直连常常只有几十 KB/s，
// 这几个公共服务把 GitHub 的下载转一手。都是别人的免费服务，随时可能失效，所以升级时先测速再挑；
// 装的是哪个文件由 GitHub 接口给的 SHA-256 说了算，转手的一方改不了内容，最多让下载失败。
var DefaultMirrors = []string{
	"https://ghfast.top/",
	"https://gh-proxy.com/",
	"https://gh-proxy.org/",
	"https://gh.llkk.cc/",
}

// 加速地址的存放与上限。
const (
	// mirrorsFileName 在数据目录下：地址好不好用取决于这台机器的网络，跟着机器走，不进设置表。
	mirrorsFileName = "update-sources.json"
	maxMirrors      = 8
	maxMirrorLength = 200
)

// ErrInvalidMirror 表示加速地址不合法。
var ErrInvalidMirror = errors.New("加速地址不合法")

type mirrorsFile struct {
	Mirrors []string `json:"mirrors"`
}

// loadMirrors 读取站长改过的加速地址；没改过（文件不存在）时给内置的那几个，custom 为假。
// 文件读坏了也退回内置地址：一个写坏的配置不该让升级走不下去。
func loadMirrors(dataDir string) (mirrors []string, custom bool) {
	data, err := os.ReadFile(filepath.Join(dataDir, mirrorsFileName))
	if err != nil {
		return append([]string(nil), DefaultMirrors...), false
	}
	var file mirrorsFile
	if err = json.Unmarshal(data, &file); err != nil {
		return append([]string(nil), DefaultMirrors...), false
	}
	normalized, err := NormalizeMirrors(file.Mirrors)
	if err != nil {
		return append([]string(nil), DefaultMirrors...), false
	}
	return normalized, true
}

// saveMirrors 写入加速地址。空列表表示只直连 GitHub，与「没改过」不同。
func saveMirrors(dataDir string, mirrors []string) error {
	data, err := json.MarshalIndent(mirrorsFile{Mirrors: mirrors}, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(dataDir, mirrorsFileName)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("保存加速地址: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("保存加速地址: %w", err)
	}
	return nil
}

// resetMirrors 删掉站长改过的地址，回到内置的那几个。
func resetMirrors(dataDir string) error {
	err := os.Remove(filepath.Join(dataDir, mirrorsFileName))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("恢复默认加速地址: %w", err)
	}
	return nil
}

// NormalizeMirrors 校验并整理加速地址：只收 https，不带账号、查询串与锚点，统一以斜杠结尾，去重。
//
// 地址用作前缀，下载时拼成「前缀 + GitHub 原地址」，这是这类加速服务通用的写法。
func NormalizeMirrors(in []string) ([]string, error) {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, raw := range in {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if len(raw) > maxMirrorLength {
			return nil, fmt.Errorf("%w：%q 超过 %d 个字符", ErrInvalidMirror, raw, maxMirrorLength)
		}
		u, err := url.Parse(raw)
		switch {
		case err != nil:
			return nil, fmt.Errorf("%w：%q 不是网址", ErrInvalidMirror, raw)
		case u.Scheme != "https":
			return nil, fmt.Errorf("%w：%q 须以 https:// 开头", ErrInvalidMirror, raw)
		case u.Host == "" || u.User != nil:
			return nil, fmt.Errorf("%w：%q 缺少域名或带了账号", ErrInvalidMirror, raw)
		case u.RawQuery != "" || u.Fragment != "":
			return nil, fmt.Errorf("%w：%q 不能带 ? 或 #", ErrInvalidMirror, raw)
		}
		prefix := u.Scheme + "://" + u.Host + strings.TrimSuffix(u.EscapedPath(), "/") + "/"
		if seen[prefix] {
			continue
		}
		seen[prefix] = true
		out = append(out, prefix)
	}
	if len(out) > maxMirrors {
		return nil, fmt.Errorf("%w：最多 %d 个", ErrInvalidMirror, maxMirrors)
	}
	return out, nil
}
