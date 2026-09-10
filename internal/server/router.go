// Package server 组装 HTTP 路由与服务生命周期。
package server

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/console"
	"github.com/FeiBaiKin/lumo/internal/httpx"
)

// 三平面路由前缀（agent.md §6）。
const (
	// PrefixConsole 需会话或 PAT + 权限校验。
	PrefixConsole = "/api/v1/console"
	// PrefixPublic 匿名只读已发布内容 + 发评论 + 搜索。
	PrefixPublic = "/api/v1/public"
	// PrefixExtension 是 Extension 通用 CRUD，v1 内部使用。
	PrefixExtension = "/apis"
)

// planeRouter 实现 app.Router，把模块的路由注册收敛到三个平面内。
type planeRouter struct {
	console   chi.Router
	public    chi.Router
	extension chi.Router
}

func (p *planeRouter) Console(fn func(r chi.Router))   { fn(p.console) }
func (p *planeRouter) Public(fn func(r chi.Router))    { fn(p.public) }
func (p *planeRouter) Extension(fn func(r chi.Router)) { fn(p.extension) }

// NewRouter 构造根路由并返回供模块注册用的三平面注册面。
//
// 阶段 1 只搭挂载点与通用中间件；鉴权中间件在阶段 2 接入，
// 各平面的业务路由由模块在阶段 3 起自行注册。
func NewRouter(logger *slog.Logger) (root chi.Router, planes app.Router) {
	mux := chi.NewRouter()
	root = mux

	root.Use(middleware.RequestID)
	// 不用 middleware.RealIP：它无条件信任 X-Forwarded-For / X-Real-IP，
	// 存在 IP 伪造风险（GHSA-3fxj-6jh8-hvhx）。真实客户端 IP 的解析需要
	// 知道可信代理范围，留到阶段 2 随反向代理配置一并实现。
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

	registry := &planeRouter{
		console:   chi.NewRouter(),
		public:    chi.NewRouter(),
		extension: chi.NewRouter(),
	}
	planes = registry

	root.Mount(PrefixConsole, registry.console)
	root.Mount(PrefixPublic, registry.public)
	root.Mount(PrefixExtension, registry.extension)

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
