package theme

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/FeiBaiKin/lumo/internal/settings"
	"github.com/FeiBaiKin/lumo/internal/version"
)

// Renderer 把路由上下文渲染成 HTML 响应。
type Renderer struct {
	registry *Registry
	store    *Store
	settings *settings.Service
	themeCfg *SettingsStore
	logger   *slog.Logger
	// assetsBase 是主题静态资源的访问前缀。
	assetsBase string
}

// RendererOptions 是构造 Renderer 的参数。
type RendererOptions struct {
	Registry *Registry
	Store    *Store
	Settings *settings.Service
	Themes   *SettingsStore
	Logger   *slog.Logger
}

// NewRenderer 构造 Renderer。
func NewRenderer(opts *RendererOptions) *Renderer {
	return &Renderer{
		registry:   opts.Registry,
		store:      opts.Store,
		settings:   opts.Settings,
		themeCfg:   opts.Themes,
		logger:     opts.Logger,
		assetsBase: AssetsPath,
	}
}

// AssetsPath 是主题静态资源的挂载前缀。
const AssetsPath = "/theme-assets"

// maxRenderBytes 是单个页面渲染输出的字节上限。
//
// 32 MiB 远超任何正常页面（一篇长文的 HTML 通常几十 KB），
// 它挡的是「模板递归或死循环把内存写爆」这类第三方主题的 bug。
const maxRenderBytes = 32 << 20

// errRenderTooLarge 表示渲染输出超过上限。
var errRenderTooLarge = errors.New("渲染输出超过上限，模板可能存在递归或死循环")

// limitedWriter 是带字节上限的 io.Writer，超限即报错中止模板执行。
type limitedWriter struct {
	w         io.Writer
	remaining int64
}

// Write 实现 io.Writer。
func (l *limitedWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > l.remaining {
		return 0, errRenderTooLarge
	}
	l.remaining -= int64(len(p))
	return l.w.Write(p)
}

// Render 渲染一个页面上下文并写出响应。
//
// 渲染先写到内存缓冲再一次性输出：模板执行到一半才出错时，
// 直接写 ResponseWriter 会留下半个页面且已经发了 200，客户端无从判断。
func (r *Renderer) Render(w http.ResponseWriter, req *http.Request, status int, name string, ctx *Context) {
	loaded := r.registry.Active()
	engine := loaded.Engine()

	var buf bytes.Buffer
	// 缓冲也是上限：主题是第三方代码，一个写坏的模板（如对空切片无限递归）
	// 会一直往缓冲里写直到内存耗尽。超出上限即视为模板有问题。
	lw := &limitedWriter{w: &buf, remaining: maxRenderBytes}
	if err := engine.Render(lw, name, ctx); err != nil {
		r.renderError(w, req, err, name)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// 前台页面随内容变化，不设长缓存；由反向代理按需覆盖。
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(status)
	if req.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(buf.Bytes())
}

// renderError 处理渲染失败。
//
// 模板出错是主题作者的 bug，但呈现给访客的必须是一个像样的页面，
// 且绝不能把模板内部信息（行号、变量名、SQL）写进响应。
func (r *Renderer) renderError(w http.ResponseWriter, req *http.Request, err error, name string) {
	if r.logger != nil {
		r.logger.Error("渲染主题模板失败",
			slog.String("template", name),
			slog.String("path", req.URL.Path),
			slog.Any("error", err))
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusInternalServerError)
	if req.Method == http.MethodHead {
		return
	}
	_, _ = w.Write([]byte(fallbackErrorPage))
}

// fallbackErrorPage 是模板渲染失败时的兜底页面。
//
// 内联而非走模板：走模板就可能再次失败，那会变成无限递归。
const fallbackErrorPage = `<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>页面暂时无法显示</title>
<style>body{margin:0;min-height:100vh;display:flex;align-items:center;justify-content:center;
background:#f7f8f7;color:#1a1d1a;font:16px/1.75 system-ui,-apple-system,"PingFang SC","Microsoft YaHei",sans-serif}
main{max-width:32em;padding:2rem}h1{font-size:1.25rem;font-weight:600;margin:0 0 .5rem}
p{margin:0;color:#5a615a}</style></head>
<body><main><h1>页面暂时无法显示</h1>
<p>当前主题的模板存在问题，站点管理员可在后台查看详情。</p></main></body></html>`

// NewContext 组装一个页面上下文的公共部分。
func (r *Renderer) NewContext(ctx context.Context, req *http.Request, kind string) (*Context, error) {
	loaded := r.registry.Active()
	site := r.siteContext(ctx)

	themeCtx := ThemeContext{
		Name:       loaded.Manifest.Name,
		Label:      loaded.Manifest.Label,
		Version:    loaded.Manifest.Version,
		AssetsBase: r.assetsBase + "/" + loaded.Manifest.Name,
		Settings:   map[string]map[string]any{},
	}
	if r.themeCfg != nil {
		stored, err := r.themeCfg.Load(ctx, loaded.Manifest.Name)
		if err != nil {
			// 读不到主题设置就用缺省值：一次设置读取失败不该让整站不可访问。
			if r.logger != nil {
				r.logger.Warn("读取主题设置失败，使用缺省值", slog.Any("error", err))
			}
			stored = map[string]map[string]any{}
		}
		themeCtx.Settings = loaded.settings.effectiveSettings(stored)
	} else {
		themeCtx.Settings = loaded.settings.effectiveSettings(nil)
	}

	return &Context{
		Kind:      kind,
		Site:      site,
		Theme:     themeCtx,
		Path:      req.URL.Path,
		Canonical: absoluteURL(site.URL, req.URL.Path),
		Find:      newFinder(ctx, r.store, site.URL),
		Params:    map[string]string{},
	}, nil
}

// siteContext 读取站点设置并组装 SiteContext。
func (r *Renderer) siteContext(ctx context.Context) SiteContext {
	info := version.Get()
	out := SiteContext{
		Title:     "Lumo",
		Language:  "zh-CN",
		Now:       time.Now(),
		Generator: "Lumo " + info.Version,
	}
	if r.settings == nil {
		return out
	}

	var site settings.Site
	if err := r.settings.Get(ctx, settings.GroupSite, &site); err != nil {
		if r.logger != nil {
			r.logger.Warn("读取站点设置失败，使用缺省值", slog.Any("error", err))
		}
		return out
	}
	out.Title = site.Title
	out.Subtitle = site.Subtitle
	out.Description = site.Description
	out.URL = strings.TrimSuffix(strings.TrimSpace(site.URL), "/")
	out.Language = site.Language
	out.LogoURL = site.LogoURL
	out.FaviconURL = site.FaviconURL
	// 页脚年份与文章日期都该按站点时区显示，而不是按服务器所在时区。
	if loc, err := time.LoadLocation(site.Timezone); err == nil {
		out.Now = out.Now.In(loc)
	}
	return out
}

// PageSize 返回前台每页条数，取自站点设置。
func (r *Renderer) PageSize(ctx context.Context) int {
	const fallback = 10
	if r.settings == nil {
		return fallback
	}
	var site settings.Site
	if err := r.settings.Get(ctx, settings.GroupSite, &site); err != nil || site.PageSize <= 0 {
		return fallback
	}
	return site.PageSize
}

// absoluteURL 拼出绝对地址；站点未配置对外地址时退回相对路径。
//
// 不编造绝对地址：canonical 指向一个错误的域名，比没有 canonical 更糟。
func absoluteURL(base, p string) string {
	if base == "" {
		return p
	}
	return strings.TrimSuffix(base, "/") + p
}

// ErrTemplateMissing 报告错误是否为模板缺失。
func ErrTemplateMissing(err error) bool { return errors.Is(err, ErrTemplateNotFound) }
