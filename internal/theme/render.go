package theme

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/FeiBaiKin/lumo/internal/auth"
	"github.com/FeiBaiKin/lumo/internal/seo"
	"github.com/FeiBaiKin/lumo/internal/settings"
	"github.com/FeiBaiKin/lumo/internal/version"
)

// FormCSRFFunc 为本次响应准备一枚表单 CSRF 令牌并返回明文。
//
// 由 account 模块实现（它拥有前台表单的那套双提交），serve 在两个模块都装配完之后
// 接上去。做成函数而不是让本包引 account：account 用本包的 Renderer 渲染账户页，
// 反过来引就是一个导入环。
type FormCSRFFunc func(w http.ResponseWriter, r *http.Request) string

// Renderer 把路由上下文渲染成 HTML 响应。
type Renderer struct {
	registry *Registry
	store    *Store
	settings *settings.Service
	themeCfg *SettingsStore
	logger   *slog.Logger
	// assetsBase 是主题静态资源的访问前缀。
	assetsBase string
	// formCSRF 签发页眉表单要用的令牌；未接上时页眉不渲染那张表单。
	formCSRF FormCSRFFunc
}

// UseFormCSRF 接上表单令牌的签发钩子。
func (r *Renderer) UseFormCSRF(fn FormCSRFFunc) { r.formCSRF = fn }

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

	r.fillCSRF(w, req, ctx)
	fillSEO(ctx)

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

// fillSEO 按站点设置与本页内容算出 SEO 派生信息。
//
// 放在 Render 里而不是 NewContext：描述与分享图要看 .Post 与 .Description，
// 而那两项是路由在 NewContext 之后才填的。
//
// 取值直接来自 Public 里的 seo 分组——那四项本来就在公开白名单上
// （见 internal/seo/settings.go 的 Public），不必再读一次设置。
func fillSEO(ctx *Context) {
	if ctx == nil {
		return
	}
	cfg := ctx.Public[seo.GroupSEO]
	// 后缀保留原样：它的前导空格是有意义的（后台给的示例就是「 — 我的站点」）。
	if suffix := publicString(cfg, "titleSuffix"); strings.TrimSpace(suffix) != "" {
		ctx.SEO.TitleSuffix = suffix
	}
	ctx.SEO.TwitterSite = publicTrimmed(cfg, "twitterSite")

	// 描述的回落顺序：本页写了的 → SEO 默认描述 → 站点描述。
	if ctx.Description == "" {
		ctx.Description = firstNonEmpty(publicTrimmed(cfg, "defaultDescription"), ctx.Site.Description)
	}

	// 分享图必须是绝对地址：社交平台抓卡片时不带 referer，解析不了相对路径。
	cover := ""
	if ctx.Post != nil {
		cover = ctx.Post.CoverURL
	}
	ctx.SEO.Image = absoluteAsset(ctx.Site.URL, firstNonEmpty(cover, publicTrimmed(cfg, "defaultImage")))
	ctx.SEO.JSONLD = jsonLD(ctx)
}

// jsonLD 生成本页的 schema.org 结构化数据。
//
// 只有内容页与首页有：列表页上一段 ItemList 对搜索引擎没有增量信息，
// 却要为每页多渲染一份 JSON。
func jsonLD(ctx *Context) template.JS {
	var doc map[string]any
	switch {
	case ctx.Post != nil && (ctx.Kind == KindPost || ctx.Kind == KindPage):
		article := seo.Article{
			Title:       ctx.Post.Title,
			Description: ctx.Description,
			Canonical:   absoluteURL(ctx.Site.URL, ctx.Post.URL),
			Image:       ctx.SEO.Image,
			SiteTitle:   ctx.Site.Title,
		}
		if ctx.Post.Author != nil {
			article.AuthorName = ctx.Post.Author.DisplayName
		}
		if !ctx.Post.PublishedAt.IsZero() {
			article.PublishedAt = ctx.Post.PublishedAt.Format(time.RFC3339)
		}
		if !ctx.Post.UpdatedAt.IsZero() {
			article.UpdatedAt = ctx.Post.UpdatedAt.Format(time.RFC3339)
		}
		doc = seo.ArticleJSONLD(article)
	case ctx.Kind == KindIndex:
		doc = seo.WebSiteJSONLD(ctx.Site.Title, ctx.Site.URL, ctx.Description)
	default:
		return ""
	}

	// json.Marshal 默认转义 < > &，产出可以安全地放进 <script>。
	raw, err := json.Marshal(doc)
	if err != nil {
		return ""
	}
	return template.JS(raw) //nolint:gosec // 内容由本包构造并经 json.Marshal 转义
}

// publicString 从一个设置分组的公开值里取字符串；缺键或类型不符时为空串。
//
// 不做 TrimSpace：标题后缀那一项的前导空格是排版的一部分。
func publicString(group map[string]any, key string) string {
	v, _ := group[key].(string)
	return v
}

// publicTrimmed 取值并去掉首尾空白，用于地址与账号这类不该带空格的项。
func publicTrimmed(group map[string]any, key string) string {
	return strings.TrimSpace(publicString(group, key))
}

// firstNonEmpty 返回第一个非空字符串。
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// absoluteAsset 把站内资源路径补成绝对地址；已是绝对地址或站点未配置对外地址时原样返回。
func absoluteAsset(base, ref string) string {
	if ref == "" || strings.Contains(ref, "://") || strings.HasPrefix(ref, "//") {
		return ref
	}
	if !strings.HasPrefix(ref, "/") {
		return ref
	}
	return absoluteURL(base, ref)
}

// fillCSRF 为已登录访客准备页眉表单要用的令牌。
//
// 必须在写出响应头之前调用：签发令牌要 Set-Cookie，而 Render 是先把整页渲染进缓冲、
// 最后才写头，故放在渲染之前。
//
// 表单页（账户页、登录页等）自己已经签过一枚，那时沿用它而不是再签一枚：
// 一次响应里签两次，只有后写进 Cookie 的那枚有效，而页面表单里的是先写的那枚——
// 再签一次就等于把访客眼前那张表单当场作废。
//
// 匿名访客一律不签：令牌逐人不同，写进 HTML 就意味着匿名页不能再被共享缓存，
// 而匿名访客的页眉上根本没有退出登录（见 partials/header.html）。
func (r *Renderer) fillCSRF(w http.ResponseWriter, req *http.Request, ctx *Context) {
	if ctx == nil || ctx.CurrentUser == nil || ctx.CSRFToken != "" {
		return
	}
	if ctx.Form != nil && ctx.Form.CSRFToken != "" {
		ctx.CSRFToken = ctx.Form.CSRFToken
		return
	}
	if r.formCSRF != nil {
		ctx.CSRFToken = r.formCSRF(w, req)
	}
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
		Name:          loaded.Manifest.Name,
		Label:         loaded.Manifest.Label,
		Version:       loaded.Manifest.Version,
		AssetsBase:    r.assetsBase + "/" + loaded.Manifest.Name,
		AssetsVersion: loaded.AssetVersion,
		Settings:      map[string]map[string]any{},
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
		Kind:        kind,
		Site:        site,
		Theme:       themeCtx,
		Path:        req.URL.Path,
		Canonical:   absoluteURL(site.URL, req.URL.Path),
		Find:        newFinder(ctx, r.store, site.URL),
		Params:      map[string]string{},
		Public:      r.publicSettings(ctx),
		CurrentUser: currentUser(req.Context()),
	}, nil
}

// publicSettings 读取各分组声明为公开的字段值。
//
// 读不到就返回空表：一次设置读取失败不该让整站不可访问，
// 而主题对缺失的键本来就该按「不显示」处理（见 Context.Setting）。
func (r *Renderer) publicSettings(ctx context.Context) map[string]map[string]any {
	if r.settings == nil {
		return nil
	}
	values, err := r.settings.Public(ctx)
	if err != nil {
		if r.logger != nil {
			r.logger.Warn("读取公开设置失败", slog.Any("error", err))
		}
		return nil
	}
	return values
}

// currentUser 从请求上下文里的调用者构造前台视图；匿名时为 nil。
//
// 这是 routes.go 的 viewerID 能拿到真实用户的前提：前台路由必须挂在
// auth.Authenticator.Optional 下，否则 context 里永远没有 principal。
func currentUser(ctx context.Context) *CurrentUserView {
	principal, ok := auth.FromContext(ctx)
	if !ok || principal.User == nil {
		return nil
	}
	user := principal.User
	return &CurrentUserView{
		ID:          user.ID,
		Username:    user.Username,
		DisplayName: user.Name(),
		AvatarURL:   user.AvatarURL,
		// 用**本次调用**的有效权限而不是角色名：令牌调用时权限可能被 scope 收窄，
		// 而「有没有后台可进」问的正是有效权限。
		ConsoleAccess: len(principal.Permissions().List()) > 0,
	}
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
