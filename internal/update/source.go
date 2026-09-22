package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"time"
)

// 更新源相关的错误哨兵。措辞要能直接显示给站长：这些错误多半不是「程序坏了」，
// 而是「源仓库还没发过版」「今天问得太频繁」这类需要人去处置的状况。
var (
	// ErrNoRelease 表示源仓库还没有任何可用发布。
	ErrNoRelease = errors.New("更新源还没有发布任何版本")
	// ErrRateLimited 表示被 GitHub 限流。
	ErrRateLimited = errors.New("已达到 GitHub 的访问频率上限")
	// ErrNoAsset 表示该版本没有当前平台的发布包。
	ErrNoAsset = errors.New("该版本没有提供当前平台的发布包")
)

// defaultAPIBase 是 GitHub REST API 的根地址。
const defaultAPIBase = "https://api.github.com"

// 单次 API 调用的期限与响应体上限。
//
// 响应体要限长：release 的正文由发布者填写，接口本身不保证长度，
// 而这份数据会原样进内存再发给浏览器。
const (
	apiTimeout      = 15 * time.Second
	maxAPIBodyBytes = 2 << 20
	maxNotesRunes   = 20000
)

// Asset 是一个发布资产。
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Size int64  `json:"size"`
}

// Release 是一次发布，已挑好当前平台要用的资产。
type Release struct {
	Tag         string    `json:"tag"`
	Name        string    `json:"name"`
	Notes       string    `json:"notes"`
	HTMLURL     string    `json:"htmlUrl"`
	PublishedAt time.Time `json:"publishedAt"`
	Prerelease  bool      `json:"prerelease"`
	// Version 是从 Tag 解析出的版本，解析失败时为零值。
	Version Version `json:"-"`
	// Asset 是当前平台的发布包。
	Asset Asset `json:"asset"`
	// Checksums 是校验和清单，没有它就不允许安装（见 download.go）。
	Checksums Asset `json:"-"`
}

// Source 从 GitHub Releases 查询版本。
type Source struct {
	repo      string
	token     string
	userAgent string
	baseURL   string
	client    *http.Client
}

// NewSource 构造更新源。repo 形如 owner/name，token 与 baseURL 可为空。
func NewSource(repo, token, userAgent, baseURL string) *Source {
	if baseURL == "" {
		baseURL = defaultAPIBase
	}
	return &Source{
		repo:      repo,
		token:     token,
		userAgent: userAgent,
		baseURL:   strings.TrimSuffix(baseURL, "/"),
		client:    &http.Client{Timeout: apiTimeout},
	}
}

// Repo 返回更新源仓库。
func (s *Source) Repo() string { return s.repo }

// ghRelease 是 GitHub 发布对象中本模块用得到的字段。
type ghRelease struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	Body        string    `json:"body"`
	HTMLURL     string    `json:"html_url"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
	Assets      []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
		Size               int64  `json:"size"`
	} `json:"assets"`
}

// Latest 取回可升级到的最新发布。
//
// allowPrerelease 为假时直接问 GitHub 的 latest —— 它本就排除草稿与预发布，
// 且尊重仓库主人手工指定的「最新」。为真时改拉列表自行挑版本号最大的那个：
// 列表按创建时间倒序，而「后创建的 tag 版本号一定更大」并不成立
// （补丁版往往在新主版本之后才发）。
func (s *Source) Latest(ctx context.Context, allowPrerelease bool) (*Release, error) {
	if allowPrerelease {
		return s.latestIncludingPrerelease(ctx)
	}
	var raw ghRelease
	if err := s.get(ctx, "/repos/"+s.repo+"/releases/latest", &raw); err != nil {
		return nil, err
	}
	return s.toRelease(&raw)
}

func (s *Source) latestIncludingPrerelease(ctx context.Context) (*Release, error) {
	var list []ghRelease
	if err := s.get(ctx, "/repos/"+s.repo+"/releases?per_page=30", &list); err != nil {
		return nil, err
	}

	var best *ghRelease
	var bestVersion Version
	for i := range list {
		item := &list[i]
		if item.Draft {
			continue
		}
		version, ok := ParseVersion(item.TagName)
		if !ok {
			continue
		}
		if best == nil || CompareVersions(version, bestVersion) > 0 {
			best, bestVersion = item, version
		}
	}
	if best == nil {
		return nil, ErrNoRelease
	}
	return s.toRelease(best)
}

// toRelease 把 GitHub 的发布对象转成本模块的形态，并挑出当前平台的资产。
//
// 找不到资产不算致命错误：版本信息仍然有用（「有新版了，但这个平台没打包」
// 比「检查失败」有用得多），故把 ErrNoAsset 连同已填好的 Release 一起返回。
func (s *Source) toRelease(raw *ghRelease) (*Release, error) {
	rel := &Release{
		Tag:         raw.TagName,
		Name:        strings.TrimSpace(raw.Name),
		Notes:       truncateRunes(raw.Body, maxNotesRunes),
		HTMLURL:     raw.HTMLURL,
		PublishedAt: raw.PublishedAt,
		Prerelease:  raw.Prerelease,
	}
	if version, ok := ParseVersion(raw.TagName); ok {
		rel.Version = version
	}
	if rel.Name == "" {
		rel.Name = rel.Tag
	}

	for _, a := range raw.Assets {
		asset := Asset{Name: a.Name, URL: a.BrowserDownloadURL, Size: a.Size}
		switch {
		case isChecksumAsset(a.Name):
			rel.Checksums = asset
		case matchesPlatform(a.Name, rel.Version.Raw):
			rel.Asset = asset
		}
	}
	if rel.Asset.URL == "" {
		return rel, ErrNoAsset
	}
	return rel, nil
}

// get 发起一次 API 调用并解码响应。
func (s *Source) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+path, http.NoBody)
	if err != nil {
		return fmt.Errorf("构造请求: %w", err)
	}
	s.setHeaders(req)

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("访问更新源失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		// 仓库不存在与「仓库在但没发过版」在这里是同一个状态码，
		// 而站长需要的下一步动作也一样：去仓库看看。
		return ErrNoRelease
	case http.StatusForbidden, http.StatusTooManyRequests:
		if resp.Header.Get("X-RateLimit-Remaining") == "0" {
			return fmt.Errorf("%w，请稍后再试（%s 后重置）", ErrRateLimited, rateLimitReset(resp))
		}
		return fmt.Errorf("更新源拒绝了请求（HTTP %d）", resp.StatusCode)
	default:
		return fmt.Errorf("更新源返回了意外状态（HTTP %d）", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAPIBodyBytes))
	if err != nil {
		return fmt.Errorf("读取更新源响应: %w", err)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("解析更新源响应: %w", err)
	}
	return nil
}

// setHeaders 填好 GitHub API 要求的请求头。
//
// User-Agent 不是可选项：GitHub 对没有 UA 的请求一律返回 403。
func (s *Source) setHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", s.userAgent)
	if s.token != "" {
		req.Header.Set("Authorization", "Bearer "+s.token)
	}
}

// rateLimitReset 把限流重置时刻转成「还要等多久」。
func rateLimitReset(resp *http.Response) string {
	var epoch int64
	if _, err := fmt.Sscanf(resp.Header.Get("X-RateLimit-Reset"), "%d", &epoch); err != nil || epoch == 0 {
		return "一段时间"
	}
	wait := time.Until(time.Unix(epoch, 0)).Round(time.Minute)
	if wait <= 0 {
		return "片刻"
	}
	return wait.String()
}

// isChecksumAsset 判断一个资产是不是校验和清单。
func isChecksumAsset(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasPrefix(lower, "checksums") && strings.HasSuffix(lower, ".txt")
}

// matchesPlatform 判断资产名是否是当前平台的发布包。
//
// 先认 goreleaser 的命名（lumo_1.2.3_linux_amd64.tar.gz），再退回包含式匹配：
// 归档命名模板改一次，精确匹配就全线失效，而站长看到的只是「没有当前平台的包」。
func matchesPlatform(name, version string) bool {
	lower := strings.ToLower(name)
	if !hasArchiveExt(lower) {
		return false
	}
	if version != "" {
		exact := strings.ToLower(fmt.Sprintf("%s_%s_%s_%s", binaryStem, version, runtime.GOOS, runtime.GOARCH))
		if strings.HasPrefix(lower, exact+".") {
			return true
		}
	}
	return strings.Contains(lower, "_"+runtime.GOOS+"_") && strings.Contains(lower, "_"+runtime.GOARCH+".")
}

// hasArchiveExt 报告文件名是否是本模块认得的归档格式。
func hasArchiveExt(lower string) bool {
	return strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz") ||
		strings.HasSuffix(lower, ".zip")
}

// truncateRunes 按字符数截断，避免把发布说明整篇搬进内存与响应。
func truncateRunes(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit]) + "\n\n（发布说明过长，已截断）"
}
