// Package server 组装 HTTP 路由与服务生命周期。
package server

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/FeiBaiKin/lumo/internal/api"
	"github.com/FeiBaiKin/lumo/internal/auth"
	"github.com/FeiBaiKin/lumo/internal/console"
	"github.com/FeiBaiKin/lumo/internal/httpx"
)

// 三平面路由前缀（agent.md §6），定义在 api 包，这里保留别名便于引用与测试。
const (
	PrefixConsole   = api.PrefixConsole
	PrefixPublic    = api.PrefixPublic
	PrefixExtension = api.PrefixExtension
)

// Authenticator 是路由层所需的鉴权能力。
//
// 定义在使用方（消费者侧接口，Go 惯例），使 server 不依赖 auth 的具体实现，
// 也让接线测试可以替换成轻量替身而不必连数据库。
type Authenticator interface {
	// Middleware 解析凭据并注入 context，但不强制要求已认证。
	Middleware(next http.Handler) http.Handler
	// CSRF 校验非安全方法的 CSRF 令牌。
	CSRF(next http.Handler) http.Handler
}

// Options 是构造路由所需的依赖。
type Options struct {
	Logger *slog.Logger
	// Authenticator 为 nil 时不挂载鉴权中间件，仅用于测试路由骨架。
	Authenticator Authenticator
	// ClientIP 为 nil 时不解析转发头，一律使用直连地址。
	ClientIP *httpx.ClientIPResolver
	// Version 写入 OpenAPI 文档的 info.version。
	Version string
}

// NewRouter 构造根路由并返回供模块注册用的三平面注册面。
//
// 三平面的鉴权策略（agent.md §6）由 api.NewPlanes 落实：
//   - Console：解析凭据 + CSRF + **强制已认证**；细粒度权限由各操作声明
//   - Public：解析凭据但不强制，匿名可读已发布内容
//   - Extension：解析凭据 + CSRF + 强制已认证
//
// 注意：未注册的路径由根路由的 NotFound 处理并返回 404，不会先经过各平面的
// 鉴权中间件——这是正确行为，但排查时容易误以为鉴权没生效。
func NewRouter(opts *Options) (root chi.Router, planes *api.Planes) {
	logger := opts.Logger
	mux := chi.NewRouter()
	root = mux

	root.Use(middleware.RequestID)
	// 不用 middleware.RealIP：它无条件信任 X-Forwarded-For / X-Real-IP，
	// 任何客户端都能伪造（GHSA-3fxj-6jh8-hvhx）。改用需显式配置可信代理
	// 范围的解析器，且不覆盖 r.RemoteAddr。
	if opts.ClientIP != nil {
		root.Use(opts.ClientIP.Middleware)
	}
	root.Use(requestLogger(logger))
	root.Use(recoverer(logger))
	// 请求体上限 10 MiB；附件上传在阶段 3 单独放宽。
	root.Use(middleware.RequestSize(10 << 20))

	// 404 与 405 也必须返回 problem+json，避免 API 出现两套错误格式。
	root.NotFound(func(w http.ResponseWriter, r *http.Request) {
		httpx.Error(w, r, http.StatusNotFound, "请求的资源不存在")
	})
	root.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		httpx.Error(w, r, http.StatusMethodNotAllowed, "该资源不支持此请求方法")
	})

	// huma 在此挂上根路由：必须晚于全部 Use、早于任何路由注册。
	planeOpts := &api.Options{Title: "Lumo API", Version: opts.Version}
	if opts.Authenticator != nil {
		planeOpts.Resolve = opts.Authenticator.Middleware
		planeOpts.CSRF = opts.Authenticator.CSRF
		planeOpts.RequireAuth = auth.RequireAuth
	}
	api.SetErrorLogger(logger)
	planes = api.NewPlanes(root, planeOpts)

	// Console SPA 与根路径重定向。
	root.Mount(console.MountPath, console.Handler())
	root.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, console.MountPath, http.StatusFound)
	})

	return root, planes
}

// requestLogger 记录每个请求的方法、路径、状态码与耗时。
func requestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if logger == nil {
				next.ServeHTTP(w, r)
				return
			}
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()
			next.ServeHTTP(ww, r)

			logger.Info("请求完成",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", ww.Status()),
				slog.Int("bytes", ww.BytesWritten()),
				slog.Duration("duration", time.Since(start)),
				slog.String("requestId", middleware.GetReqID(r.Context())),
			)
		})
	}
}

// recoverer 捕获 panic 并以 problem+json 返回 500。
//
// 不用 chi 自带的 middleware.Recoverer：它输出纯文本，会破坏 API 的统一错误格式。
func recoverer(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				rec := recover()
				if rec == nil {
					return
				}
				// http.ErrAbortHandler 是约定的静默中止信号，应原样向上传递。
				if rec == http.ErrAbortHandler { //nolint:errorlint // 哨兵值按约定用等值比较
					panic(rec)
				}
				if logger != nil {
					logger.Error("请求处理 panic",
						slog.Any("panic", rec),
						slog.String("method", r.Method),
						slog.String("path", r.URL.Path),
						slog.String("requestId", middleware.GetReqID(r.Context())),
					)
				}
				httpx.Error(w, r, http.StatusInternalServerError, "")
			}()
			next.ServeHTTP(w, r)
		})
	}
}
