// Package api 装配 huma：三平面分组、OpenAPI 配置、错误模型桥接与分页约定。
//
// 所有 REST 接口都经 huma.Register 注册：请求校验、problem+json
// 与 OpenAPI 3.1 由代码直接生成，模块不再直接接触 chi 路由。
package api

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	"github.com/FeiBaiKin/lumo/internal/httpx"
)

// 三平面路由前缀。
const (
	// PrefixConsole 需会话或 PAT + 权限校验。
	PrefixConsole = "/api/v1/console"
	// PrefixPublic 匿名只读已发布内容 + 发评论 + 搜索。
	PrefixPublic = "/api/v1/public"
	// PrefixExtension 是 Extension 通用 CRUD，v1 内部使用。
	PrefixExtension = "/apis"
)

// 规范与文档端点。放在 /api 下，避免与主题的路由空间冲突。
const (
	// OpenAPIPath 加扩展名即规范地址：/api/openapi.json（3.1）与 /api/openapi-3.0.json（降级）。
	OpenAPIPath = "/api/openapi"
	// DocsPath 是交互式文档页面。
	DocsPath = "/api/docs"
	// SchemasPath 下按名称提供各 JSON Schema。
	SchemasPath = "/api/schemas"
)

// OpenAPI 安全方案名，供文档标注受保护的操作。
const (
	SecuritySession = "session"
	SecurityBearer  = "bearer"
)

// Middleware 是标准 net/http 中间件。
type Middleware = func(http.Handler) http.Handler

// Options 是构造三平面所需的依赖。
type Options struct {
	// Title 与 Version 写入 OpenAPI 的 info。
	Title   string
	Version string
	// Resolve 解析凭据并注入 context，但不强制已认证；nil 表示不挂鉴权（仅用于测试骨架）。
	Resolve Middleware
	// CSRF 校验非安全方法的 CSRF 令牌。
	CSRF Middleware
	// RequireAuth 要求请求已认证。
	RequireAuth Middleware
}

// Planes 持有三平面的 huma 分组，实现 app.Router。
type Planes struct {
	root          chi.Router
	api           huma.API
	console       *huma.Group
	consolePublic *huma.Group
	public        *huma.Group
	extension     *huma.Group
}

// NewPlanes 在根路由上创建 huma API 与三平面分组。
//
// 鉴权策略：
//   - Console：解析凭据 + CSRF + **强制已认证**；细粒度权限由各操作声明
//   - ConsolePublic：只解析凭据，仅供登录一类极少数免认证端点
//   - Public：解析凭据但不强制，匿名可读已发布内容；会话身份的写请求要过 CSRF
//   - Extension：解析凭据 + CSRF + 强制已认证
//
// 中间件挂在分组上，模块注册的每个操作自动继承，不存在「漏挂」的可能。
// 必须在根路由的全部 Use 之后调用：chi 不允许在注册路由后再添加中间件。
func NewPlanes(root chi.Router, opts *Options) *Planes {
	humaAPI := humachi.New(root, NewConfig(opts.Title, opts.Version))
	p := &Planes{
		root:          root,
		api:           humaAPI,
		console:       huma.NewGroup(humaAPI, PrefixConsole),
		consolePublic: huma.NewGroup(humaAPI, PrefixConsole),
		public:        huma.NewGroup(humaAPI, PrefixPublic),
		extension:     huma.NewGroup(humaAPI, PrefixExtension),
	}

	if opts.Resolve != nil {
		resolve := HTTPMiddleware(opts.Resolve)
		for _, group := range []*huma.Group{p.console, p.consolePublic, p.public, p.extension} {
			group.UseMiddleware(resolve)
		}
	}
	// CSRF 置于 RequireAuth 之前：CSRF 中间件对匿名请求放行，
	// 于是匿名写请求得到 401（未登录）而非 403（CSRF 失败），客户端才知道该去登录。
	//
	// Public 平面同样挂 CSRF：它是匿名可读可写的，但**已登录访客**在 Public 平面
	// 发评论时会自动带上会话 Cookie，若不校验就留下一个同站点跨源页面
	// 借受害者会话写评论的口子（评论还可能因为作者身份被自动通过审核）。
	// 匿名请求不带 Cookie，中间件直接放行，不影响访客评论。
	if opts.CSRF != nil {
		csrf := HTTPMiddleware(opts.CSRF)
		p.console.UseMiddleware(csrf)
		p.extension.UseMiddleware(csrf)
		p.public.UseMiddleware(csrf)
	}
	if opts.RequireAuth != nil {
		requireAuth := HTTPMiddleware(opts.RequireAuth)
		p.console.UseMiddleware(requireAuth)
		p.extension.UseMiddleware(requireAuth)
	}

	// 文档层面标注受保护平面的安全方案，供 /api/docs 与 Console TS 客户端使用。
	secured := func(op *huma.Operation) {
		if op.Security == nil {
			op.Security = []map[string][]string{{SecuritySession: {}}, {SecurityBearer: {}}}
		}
	}
	p.console.UseSimpleModifier(secured)
	p.extension.UseSimpleModifier(secured)
	return p
}

// Console 返回 Console 平面的注册面：已解析凭据、CSRF 校验、强制已认证。
func (p *Planes) Console() huma.API { return p.console }

// ConsolePublic 返回 Console 平面下免认证的注册面，仅供登录一类极少数端点。
func (p *Planes) ConsolePublic() huma.API { return p.consolePublic }

// Public 返回 Public 平面的注册面：解析凭据但不强制。
func (p *Planes) Public() huma.API { return p.public }

// Extension 返回 Extension 平面的注册面：强制已认证。
func (p *Planes) Extension() huma.API { return p.extension }

// API 返回不带前缀与鉴权的根 huma API，仅供核心挂载诊断类操作。
func (p *Planes) API() huma.API { return p.api }

// Raw 在站点根路径挂一个直接写响应的处理器，实现 app.Router。
func (p *Planes) Raw(method, path string, handler http.HandlerFunc) {
	p.root.Method(method, path, handler)
}

// OpenAPI 返回生成中的规范文档。
func (p *Planes) OpenAPI() *huma.OpenAPI { return p.api.OpenAPI() }

// NewConfig 返回 huma 配置。
func NewConfig(title, version string) huma.Config {
	if version == "" {
		version = "dev"
	}
	cfg := huma.DefaultConfig(title, version)
	cfg.OpenAPIPath = OpenAPIPath
	cfg.DocsPath = DocsPath
	cfg.SchemasPath = SchemasPath
	// 不注入 $schema 字段与 Link 头：对 Console 客户端是噪音，还会让每个响应体多一份无关字段。
	cfg.CreateHooks = nil
	cfg.Transformers = []huma.Transformer{problemInstance}
	cfg.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		SecuritySession: {
			Type: "apiKey",
			In:   "cookie",
			Name: "lumo_session",
			Description: "Console 登录后由服务端下发的会话 Cookie（HTTPS 下名为 __Host-lumo_session）；" +
				"非安全方法还须携带 X-CSRF-Token 请求头。",
		},
		SecurityBearer: {
			Type:         "http",
			Scheme:       "bearer",
			BearerFormat: "lumo_pat_…",
			Description:  "Personal Access Token，供无头调用；不需要 CSRF 令牌。",
		},
	}
	return cfg
}

// problemInstance 给错误响应补上 instance（请求路径）。
func problemInstance(ctx huma.Context, _ string, v any) (any, error) {
	if problem, ok := v.(*httpx.Problem); ok && problem.Instance == "" {
		problem.Instance = ctx.URL().Path
	}
	return v, nil
}
