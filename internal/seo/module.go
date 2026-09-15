// Package seo 提供搜索引擎与订阅源的输出。
//
// 分两部分：
//   - 根路径上的 robots.txt、sitemap.xml、feed.xml、atom.xml —— 非 JSON 且必须位于根路径，
//     经 app.Router.Raw 注册；
//   - Public 平面上的 SEO 元信息端点，供主题渲染 head 时取 canonical、OpenGraph 与 JSON-LD。
//
// 无数据库表，故不实现 Migrator。依赖 content 模块的 posts 表与 settings 模块的站点设置。
package seo

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/settings"
)

// Name 是模块名。
const Name = "seo"

// 根路径上的文档地址。
const (
	PathRobots  = "/robots.txt"
	PathSitemap = "/sitemap.xml"
	PathFeed    = "/feed.xml"
	PathAtom    = "/atom.xml"
)

// Module 是 SEO 模块。
type Module struct {
	service *Service
}

// New 构造模块。
func New() *Module { return &Module{} }

// Name 实现 app.Module。
func (m *Module) Name() string { return Name }

// Register 实现 app.Module：只做装配，不访问数据库。
func (m *Module) Register(a *app.App) error {
	var store *Store
	if db := a.DB(); db != nil {
		store = NewStore(db.DB)
	}
	m.service = NewService(store, settings.From(a), a.Logger())
	a.Provide(Name, m.service)
	return nil
}

// Settings 实现 app.SettingsProvider。
func (m *Module) Settings() []app.SettingGroup {
	return []app.SettingGroup{settingsGroup()}
}

// Start 实现 app.Starter：本模块没有需要启动的后台任务。
//
// 文档缓存按 TTL 自然过期（见 cacheTTL），不额外挂钩设置变更事件：
// 一分钟的延迟对爬虫与阅读器完全无感，为它引入一条跨模块的事件链不划算。
func (m *Module) Start(context.Context) error { return nil }

// Routes 实现 app.RouteProvider。
func (m *Module) Routes(r app.Router) {
	if m.service == nil || m.service.store == nil {
		return
	}
	m.registerDocuments(r)
	m.registerMeta(r.Public())
}

// registerDocuments 在根路径挂上四份非 JSON 文档。
//
// 它们必须是根路径：爬虫只认 https://example.com/robots.txt，
// 放在 /api/v1/public 下没有任何搜索引擎会去看。
func (m *Module) registerDocuments(r app.Router) {
	r.Raw(http.MethodGet, PathRobots, func(w http.ResponseWriter, req *http.Request) {
		writeDocument(w, req, "text/plain", m.service.Robots(req.Context()))
	})
	r.Raw(http.MethodGet, PathSitemap, m.document(func(ctx context.Context) ([]byte, error) {
		if !m.service.SEO(ctx).SitemapEnabled {
			return nil, errDisabled
		}
		return m.service.Sitemap(ctx)
	}, "application/xml"))
	r.Raw(http.MethodGet, PathFeed, m.document(func(ctx context.Context) ([]byte, error) {
		if !m.service.SEO(ctx).FeedEnabled {
			return nil, errDisabled
		}
		return m.service.Feed(ctx, "rss")
	}, "application/rss+xml"))
	r.Raw(http.MethodGet, PathAtom, m.document(func(ctx context.Context) ([]byte, error) {
		if !m.service.SEO(ctx).FeedEnabled {
			return nil, errDisabled
		}
		return m.service.Feed(ctx, "atom")
	}, "application/atom+xml"))
}

// document 把一个生成函数包装成 HTTP 处理器。
func (m *Module) document(build func(context.Context) ([]byte, error), contentType string) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		body, err := build(req.Context())
		switch {
		case err == nil:
			writeDocument(w, req, contentType, body)
		case errors.Is(err, errDisabled):
			http.NotFound(w, req)
		case errors.Is(err, ErrNoSiteURL):
			// 站点没配对外地址时，生成的绝对链接一定是错的，宁可明确报错。
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
		default:
			if m.service.logger != nil {
				m.service.logger.Warn("生成 SEO 文档失败", "path", req.URL.Path, "error", err)
			}
			http.Error(w, "生成文档失败", http.StatusInternalServerError)
		}
	}
}

// errDisabled 表示该类文档在设置里被关闭。
var errDisabled = errors.New("该文档已在设置中关闭")

// ---------- 元信息端点 ----------

type metaInput struct {
	Kind string `path:"kind" enum:"post,page" doc:"内容类型"`
	Slug string `path:"slug" minLength:"1" maxLength:"128"`
}

type metaOutput struct {
	Body Meta
}

// registerMeta 在 Public 平面挂上 SEO 元信息端点，供主题渲染 head。
func (m *Module) registerMeta(public huma.API) {
	huma.Register(public, huma.Operation{
		OperationID: "seo-meta",
		Method:      http.MethodGet,
		Path:        "/seo/{kind}/{slug}",
		Summary:     "获取内容的 SEO 元信息",
		Description: "返回标题、描述、canonical、OpenGraph 字段与 JSON-LD，" +
			"主题直接内联进 head 即可，不必自己拼绝对地址。",
		Tags:   []string{"seo"},
		Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *metaInput) (*metaOutput, error) {
		meta, err := m.service.BuildMeta(ctx, in.Kind, in.Slug, nil)
		if err != nil {
			return nil, err
		}
		if meta == nil {
			return nil, huma.Error404NotFound(ErrNotFound.Error())
		}
		return &metaOutput{Body: *meta}, nil
	})
}

// Service 返回 SEO 服务，供其他模块（主题、站点地图扩展）取用。
func (m *Module) Service() *Service { return m.service }

// From 取回 SEO 服务；模块未装配时返回 nil。
func From(a *app.App) *Service {
	if a == nil {
		return nil
	}
	v, ok := a.Lookup(Name)
	if !ok {
		return nil
	}
	svc, _ := v.(*Service)
	return svc
}
