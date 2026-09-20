// Package server 组装 HTTP 路由与服务生命周期。
package server

import (
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/FeiBaiKin/lumo/internal/api"
	"github.com/FeiBaiKin/lumo/internal/auth"
	"github.com/FeiBaiKin/lumo/internal/console"
	"github.com/FeiBaiKin/lumo/internal/httpx"
)

// 三平面路由前缀，定义在 api 包，这里保留别名便于引用与测试。
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
	// MaxBodySize 是普通请求体的字节上限，0 表示用默认值。
	MaxBodySize int64
	// MaxUploadSize 是 multipart 请求体的字节上限，0 表示用默认值。
	MaxUploadSize int64
	// UploadsDir 非空时把该目录以静态文件形式挂在 /uploads 下，供本地存储的附件访问。
	UploadsDir string
	// FallbackRootToConsole 为真时把根路径重定向到 Console。
	//
	// 仅供没有装配主题模块的场景（路由骨架测试、未来的纯 API 模式）使用：
	// 正常运行时根路径是访客前台，由 theme 模块接管。
	FallbackRootToConsole bool
	// InstallRedirect 非空时（首次安装向导模式）把站点入口重定向到该地址。
	//
	// 站长此时打开浏览器应当看到安装向导，而不是一个登不进去的登录页。
	// 拦的只是入口，/console/assets/* 照常提供 —— 向导自己也跑在这个 SPA 里。
	InstallRedirect string
}

// 请求体上限的兜底默认值，仅在调用方未提供时生效。
const (
	defaultMaxBodySize   int64 = 10 << 20
	defaultMaxUploadSize int64 = 64 << 20
)

// NewRouter 构造根路由并返回供模块注册用的三平面注册面。
//
// 三平面的鉴权策略由 api.NewPlanes 落实：
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
	root.Use(requestSize(orDefault(opts.MaxBodySize, defaultMaxBodySize),
		orDefault(opts.MaxUploadSize, defaultMaxUploadSize)))
	// 评论模块要留痕 User-Agent；放在这里是为了让模块不必直接读请求头。
	root.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(httpx.WithUserAgent(r.Context(), r.UserAgent())))
		})
	})

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

	if opts.UploadsDir != "" {
		mountCanonical(root, UploadsPath+"/", uploadsHandler(opts.UploadsDir))
	}

	// Console SPA。根路径不在这里注册：阶段 4 起它属于主题渲染的访客前台，
	// 由 theme 模块在全部模块注册之后挂载（见 serve.go）。
	consoleHandler := console.Handler()
	if opts.InstallRedirect != "" {
		consoleHandler = redirectEntry(consoleHandler, console.MountPath, opts.InstallRedirect)
		root.Get("/", redirectTo(opts.InstallRedirect))
	}
	mountCanonical(root, console.MountPath, consoleHandler)
	if opts.InstallRedirect == "" && opts.FallbackRootToConsole {
		root.Get("/", redirectTo(console.MountPath))
	}

	return root, planes
}

// redirectTo 返回一个恒定跳转到 target 的处理器。
func redirectTo(target string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target, http.StatusFound)
	}
}

// redirectEntry 把 Console 的入口请求指向别处，静态资源照常放行。
//
// 只认 /console 与 /console/ 两个路径：向导自己跑在这个 SPA 里，
// 一刀切重定向会让 /console/assets/* 也被拦掉，向导自己都打不开。
func redirectEntry(next http.Handler, mountPath, target string) http.Handler {
	bare := strings.TrimSuffix(mountPath, "/")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == mountPath || r.URL.Path == bare {
			http.Redirect(w, r, target, http.StatusFound)
			return
		}
		next.ServeHTTP(w, r)
	})
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

// UploadsPath 是本地存储附件的对外访问前缀。
const UploadsPath = "/uploads"

// mountCanonical 把 handler 挂到 mountPath 下，并让不带末尾斜杠的写法重定向过去。
//
// chi 的 Mount 只对**不以斜杠结尾**的 pattern 额外注册裸路径（见 chi mux.go 的
// Mount），而 console 这类挂载点的 pattern 天然带斜杠（base 就是 "/console/"）。
// 于是 /console 不会被任何一个 console 路由接住，直接漏给后注册的兜底路由：
// 生产环境里被主题的 /{slug} 当成名为 console 的独立页面，访客看到的是 404 页，
// 而不是后台。这里显式补一条重定向，让 /console 与 /console/ 都进后台。
//
// 用 301 而非 302：挂载点是静态约定，不存在临时跳转的语义。
func mountCanonical(r chi.Router, mountPath string, handler http.Handler) {
	canonical := mountPath
	if !strings.HasSuffix(canonical, "/") {
		canonical += "/"
	}
	if bare := strings.TrimSuffix(canonical, "/"); bare != "" {
		r.Get(bare, func(w http.ResponseWriter, req *http.Request) {
			http.Redirect(w, req, canonical, http.StatusMovedPermanently)
		})
	}
	r.Mount(canonical, handler)
}

// orDefault 返回 value，非正数时返回 fallback。
func orDefault(value, fallback int64) int64 {
	if value <= 0 {
		return fallback
	}
	return value
}

// requestSize 按内容类型施加请求体上限。
//
// multipart 走附件上限、其余走普通上限：附件动辄几十兆，而 JSON 接口不该有那么大的口子。
// 判据用 Content-Type 而非路径，是为了不把「哪些路径是上传接口」这种模块知识写进核心路由。
func requestSize(maxBody, maxUpload int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			limit := maxBody
			if mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err == nil &&
				mediaType == "multipart/form-data" {
				limit = maxUpload
			}
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, limit)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// uploadsHandler 以静态文件形式提供本地存储的附件。
//
// 上传内容与 Console 同源，必须假定其中可能有 SVG 或 HTML 一类可执行文档：
//   - nosniff 阻止浏览器把 .txt 猜成 HTML 去执行；
//   - CSP 的 sandbox 与 default-src 'none' 让直接打开这些文件时脚本无法运行，
//     只放行图片与音视频自身的渲染。作为子资源（<img src>）加载时 CSP 不生效，主题不受影响。
func uploadsHandler(dir string) http.Handler {
	fileServer := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy",
			"default-src 'none'; img-src 'self'; media-src 'self'; style-src 'unsafe-inline'; sandbox")
		http.StripPrefix(UploadsPath, fileServer).ServeHTTP(w, r)
	})
}
