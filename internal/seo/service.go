package seo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/FeiBaiKin/lumo/internal/content"
	"github.com/FeiBaiKin/lumo/internal/settings"
)

// 前台路径前缀，与主题路由约定一致。
const (
	pathPosts      = "/posts/"
	pathCategories = "/categories/"
	pathTags       = "/tags/"
)

// cacheTTL 是 sitemap 与订阅源的进程内缓存时长。
//
// 这两份文档的生成成本随内容量线性增长，而爬虫与阅读器会反复来取。
// 一分钟的延迟对它们完全无感，却能挡住绝大部分重复计算。
const cacheTTL = time.Minute

// maxSitemapEntries 是 sitemap 单文件的条目上限。
//
// 协议上限是 50000 条 / 50 MB；先用一个保守值，超过时截断并记日志，
// 等有真实站点撑满再实现分片索引。
const maxSitemapEntries = 10000

// ErrNoSiteURL 表示站点未配置对外地址。
//
// sitemap 与订阅源要求绝对地址，站点地址缺失时只能报错：
// 编一个相对地址出来的文档，爬虫会直接丢掉。
var ErrNoSiteURL = errors.New("站点未配置对外地址（site.url），无法生成绝对链接")

// SiteInfo 是生成文档所需的站点信息。
type SiteInfo struct {
	Title       string
	Description string
	URL         string
	Language    string
}

// Service 生成 sitemap、robots 与订阅源。
type Service struct {
	store    *Store
	settings *settings.Service
	logger   logger

	mu    sync.Mutex
	cache map[string]cachedDoc
}

// logger 是本包用到的最小日志能力。
type logger interface {
	Warn(msg string, args ...any)
	Info(msg string, args ...any)
}

type cachedDoc struct {
	body    []byte
	expires time.Time
}

// NewService 构造 Service。settings 为 nil 时全部退回缺省值。
func NewService(store *Store, svc *settings.Service, log logger) *Service {
	return &Service{store: store, settings: svc, logger: log, cache: map[string]cachedDoc{}}
}

// Site 读取站点信息。
func (s *Service) Site(ctx context.Context) SiteInfo {
	info := SiteInfo{Title: "Lumo"}
	if s.settings == nil {
		return info
	}
	var site struct {
		Title       string `json:"title"`
		Subtitle    string `json:"subtitle"`
		Description string `json:"description"`
		URL         string `json:"url"`
		Language    string `json:"language"`
	}
	if err := s.settings.Get(ctx, settings.GroupSite, &site); err != nil {
		if s.logger != nil {
			s.logger.Warn("读取站点设置失败，使用缺省值", "error", err)
		}
		return info
	}
	info.Title = site.Title
	info.Description = firstNonEmpty(site.Description, site.Subtitle)
	info.URL = strings.TrimSuffix(strings.TrimSpace(site.URL), "/")
	info.Language = site.Language
	return info
}

// SEO 读取 SEO 设置；未装配设置模块时用缺省值。
func (s *Service) SEO(ctx context.Context) Settings {
	var cfg Settings
	if s.settings == nil {
		return defaultSettings
	}
	if err := s.settings.Get(ctx, GroupSEO, &cfg); err != nil {
		if s.logger != nil {
			s.logger.Warn("读取 SEO 设置失败，使用缺省值", "error", err)
		}
		return defaultSettings
	}
	return cfg
}

// defaultSettings 与分组缺省值保持一致，供未装配设置模块的场景使用。
var defaultSettings = Settings{
	SitemapEnabled: true,
	FeedEnabled:    true,
	FeedSize:       20,
}

// Robots 生成 robots.txt。
func (s *Service) Robots(ctx context.Context) []byte {
	site := s.Site(ctx)
	cfg := s.SEO(ctx)
	return renderRobots(site.URL, cfg.RobotsExtra, cfg.SitemapEnabled)
}

// Sitemap 生成 sitemap.xml。
func (s *Service) Sitemap(ctx context.Context) ([]byte, error) {
	site := s.Site(ctx)
	if site.URL == "" {
		return nil, ErrNoSiteURL
	}
	return s.cached(ctx, "sitemap", func() ([]byte, error) {
		return s.buildSitemap(ctx, site)
	})
}

// buildSitemap 查库并渲染 sitemap。
func (s *Service) buildSitemap(ctx context.Context, site SiteInfo) ([]byte, error) {
	entries, err := s.store.ListAll(ctx, maxSitemapEntries)
	if err != nil {
		return nil, err
	}
	categories, err := s.store.ListCategories(ctx)
	if err != nil {
		return nil, err
	}
	tags, err := s.store.ListTags(ctx)
	if err != nil {
		return nil, err
	}

	urls := make([]Entry, 0, len(entries)+len(categories)+len(tags)+1)
	// 首页永远在第一位。
	urls = append(urls, Entry{URL: site.URL + "/"})
	for i := range entries {
		entries[i].URL = site.URL + contentPath(entries[i].Type, entries[i].Slug)
	}
	urls = append(urls, entries...)
	for i := range categories {
		categories[i].URL = site.URL + pathCategories + categories[i].Slug
	}
	urls = append(urls, categories...)
	for i := range tags {
		tags[i].URL = site.URL + pathTags + tags[i].Slug
	}
	urls = append(urls, tags...)
	return renderSitemap(urls)
}

// Feed 生成订阅源；kind 取 rss 或 atom。
func (s *Service) Feed(ctx context.Context, kind string) ([]byte, error) {
	site := s.Site(ctx)
	if site.URL == "" {
		return nil, ErrNoSiteURL
	}
	cfg := s.SEO(ctx)

	return s.cached(ctx, "feed-"+kind, func() ([]byte, error) {
		entries, err := s.feedEntries(ctx, &cfg)
		if err != nil {
			return nil, err
		}
		for i := range entries {
			entries[i].URL = site.URL + contentPath(entries[i].Type, entries[i].Slug)
			entries[i].ContentHTML = ""
			if cfg.FeedFullText {
				// 正文由 content 模块渲染并已存库，订阅源直接消费它。
				entries[i].ContentHTML = entries[i].Content
			}
			entries[i].Summary = summaryOf(&entries[i])
		}

		meta := feedMeta{
			Title:       site.Title,
			Description: site.Description,
			Language:    site.Language,
			SiteURL:     site.URL + "/",
			SelfRSS:     site.URL + "/feed.xml",
			SelfAtom:    site.URL + "/atom.xml",
		}
		if kind == "atom" {
			return renderAtom(&meta, entries)
		}
		return renderRSS(&meta, entries)
	})
}

// feedEntries 取订阅源要输出的内容：默认只取文章，可按设置带上独立页面。
func (s *Service) feedEntries(ctx context.Context, cfg *Settings) ([]Entry, error) {
	if cfg.IncludePages {
		return s.store.ListAll(ctx, cfg.FeedSize)
	}
	return s.store.ListPosts(ctx, cfg.FeedSize)
}

// Meta 描述一条内容的搜索引擎元信息，供主题渲染 head。
type Meta struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Canonical   string `json:"canonical"`
	Type        string `json:"type" doc:"OpenGraph 的 og:type"`
	Image       string `json:"image,omitempty"`
	PublishedAt string `json:"publishedAt,omitempty"`
	UpdatedAt   string `json:"updatedAt,omitempty"`
	Author      string `json:"author,omitempty"`
	SiteName    string `json:"siteName"`
	Locale      string `json:"locale"`
	TwitterSite string `json:"twitterSite,omitempty"`
	// JSONLD 是可直接内联进 <script type="application/ld+json"> 的结构化数据。
	JSONLD map[string]any `json:"jsonld,omitempty"`
}

// BuildMeta 组装一条内容的 SEO 元信息。
//
// 内容里写过的 meta 覆写优先于设置里的默认值：站长对单篇内容的判断比全局默认更准。
func (s *Service) BuildMeta(ctx context.Context, kind, slug string, overrides map[string]any) (*Meta, error) {
	site := s.Site(ctx)
	cfg := s.SEO(ctx)

	entry, err := s.store.GetPost(ctx, kind, slug)
	if err != nil {
		return nil, err
	}
	if entry == nil {
		return nil, nil
	}

	title := entry.Title
	if suffix := overrides["title"]; suffix != nil {
		title = fmt.Sprint(suffix)
	} else if cfg.TitleSuffix != "" {
		title += cfg.TitleSuffix
	}

	description := firstNonEmpty(
		overrideString(overrides, "description"),
		entry.Excerpt,
		trimSummary(entry.Content, 160),
		cfg.DefaultDesc,
		site.Description,
	)
	image := firstNonEmpty(overrideString(overrides, "image"), cfg.DefaultImage)

	meta := &Meta{
		Title:       title,
		Description: description,
		Canonical:   site.URL + contentPath(entry.Type, entry.Slug),
		Type:        "article",
		Image:       image,
		SiteName:    site.Title,
		Locale:      site.Language,
		TwitterSite: cfg.TwitterSite,
		Author:      entry.AuthorName,
	}
	if !entry.PublishedAt.IsZero() {
		meta.PublishedAt = entry.PublishedAt.Format(time.RFC3339)
	}
	if !entry.UpdatedAt.IsZero() {
		meta.UpdatedAt = entry.UpdatedAt.Format(time.RFC3339)
	}
	meta.JSONLD = buildJSONLD(meta, entry, site)
	return meta, nil
}

// JSON-LD 的键名，集中定义避免同一字面量散落多处。
const (
	keyContext = "@context"
	keyType    = "@type"
)

// buildJSONLD 组装 schema.org 的 Article 结构化数据。
func buildJSONLD(meta *Meta, entry *Entry, site SiteInfo) map[string]any {
	doc := map[string]any{
		keyContext:         "https://schema.org",
		keyType:            "Article",
		"headline":         entry.Title,
		"mainEntityOfPage": map[string]any{keyType: "WebPage", "@id": meta.Canonical},
		"description":      meta.Description,
	}
	if meta.Image != "" {
		doc["image"] = meta.Image
	}
	if meta.PublishedAt != "" {
		doc["datePublished"] = meta.PublishedAt
	}
	if meta.UpdatedAt != "" {
		doc["dateModified"] = meta.UpdatedAt
	}
	if entry.AuthorName != "" {
		doc["author"] = map[string]any{keyType: "Person", "name": entry.AuthorName}
	}
	if site.Title != "" {
		doc["publisher"] = map[string]any{keyType: "Organization", "name": site.Title}
	}
	return doc
}

// cached 按时长缓存一份文档；生成失败不写缓存。
func (s *Service) cached(_ context.Context, key string, build func() ([]byte, error)) ([]byte, error) {
	s.mu.Lock()
	if doc, ok := s.cache[key]; ok && time.Now().Before(doc.expires) {
		s.mu.Unlock()
		return doc.body, nil
	}
	s.mu.Unlock()

	body, err := build()
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.cache[key] = cachedDoc{body: body, expires: time.Now().Add(cacheTTL)}
	s.mu.Unlock()
	return body, nil
}

// Invalidate 清空缓存，供设置变更后立即生效。
func (s *Service) Invalidate() {
	s.mu.Lock()
	s.cache = map[string]cachedDoc{}
	s.mu.Unlock()
}

// contentPath 返回内容的前台路径。
func contentPath(kind, slug string) string {
	if kind == "page" {
		return "/" + slug
	}
	return pathPosts + slug
}

// firstNonEmpty 返回第一个非空字符串。
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// overrideString 读取内容级覆写里的字符串字段。
func overrideString(overrides map[string]any, key string) string {
	if overrides == nil {
		return ""
	}
	if v, ok := overrides[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// summaryOf 取内容的摘要：优先手写摘要，其次从正文提取。
func summaryOf(e *Entry) string {
	if e.Excerpt != "" {
		return e.Excerpt
	}
	return trimSummary(e.Content, 200)
}

// trimSummary 从渲染后的 HTML 正文里提取一段纯文本。
func trimSummary(html string, limit int) string {
	return content.Excerpt(html, limit)
}

// ErrNotFound 表示内容不存在。
var ErrNotFound = errors.New("内容不存在")

// writeDocument 写出一个非 JSON 文档并带上缓存头。
func writeDocument(w http.ResponseWriter, r *http.Request, contentType string, body []byte) {
	w.Header().Set("Content-Type", contentType+"; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	_, _ = w.Write(body)
}
