package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

// 本文件验证三平面的鉴权**接线**是否正确。
// 各中间件自身的逻辑在 internal/auth 中测试，这里只关心「有没有挂上去」——
// 漏挂一次就是一个无鉴权的后台接口，且不会以任何可见方式报错。

// stubAuthenticator 是 Authenticator 的轻量替身：
// 不解析凭据、不做 CSRF 校验，因此只保留 RequireAuth 的效果。
// 这样接线测试无需数据库即可验证「平面是否默认要求认证」。
type stubAuthenticator struct{}

func (stubAuthenticator) Middleware(next http.Handler) http.Handler { return next }
func (stubAuthenticator) CSRF(next http.Handler) http.Handler       { return next }

// TestConsolePlaneRequiresAuthByDefault 是本文件最重要的测试。
//
// 历史背景：Console 平面曾不挂 RequireAuth，仅靠 serve.go 给认证端点
// 单独包一层 Group 来保护。那样一旦有新模块往 Console 平面注册路由，
// 就会默认暴露一个无鉴权接口。现在改为平面层面默认强制认证。
func TestConsolePlaneRequiresAuthByDefault(t *testing.T) {
	t.Parallel()

	// Authenticator 为 nil 时不挂鉴权中间件，故这里需要一个非 nil 的实例。
	// 用真实构造器但依赖为空：本测试只走「无凭据」路径，不会触库。
	authenticator := stubAuthenticator{}

	root, planes := NewRouter(&Options{Authenticator: authenticator})

	// 模拟模块往 Console 平面注册一个后台接口，**不**自行加鉴权。
	planes.Console(func(r chi.Router) {
		r.Get("/admin-only", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("secret"))
		})
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, PrefixConsole+"/admin-only", http.NoBody)
	root.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("Console 平面未默认要求认证：状态码 = %d，期望 401", rec.Code)
	}
	if rec.Body.String() == "secret" {
		t.Fatal("未认证请求读到了受保护内容")
	}
}

// TestPublicConsoleRoutesBypassAuth 验证免认证入口仍然可用。
func TestPublicConsoleRoutesBypassAuth(t *testing.T) {
	t.Parallel()

	authenticator := stubAuthenticator{}

	root, _ := NewRouter(&Options{
		Authenticator: authenticator,
		PublicConsoleRoutes: func(r chi.Router) {
			r.Post("/auth/login", func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("login"))
			})
		},
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, PrefixConsole+"/auth/login", http.NoBody)
	root.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("免认证端点被拦截：状态码 = %d，期望 200", rec.Code)
	}
}

// TestExtensionPlaneRequiresAuth 验证 Extension 平面同样默认强制认证。
func TestExtensionPlaneRequiresAuth(t *testing.T) {
	t.Parallel()

	authenticator := stubAuthenticator{}
	root, planes := NewRouter(&Options{Authenticator: authenticator})

	planes.Extension(func(r chi.Router) {
		r.Get("/posts", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("secret"))
		})
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, PrefixExtension+"/posts", http.NoBody)
	root.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("Extension 平面未要求认证：状态码 = %d，期望 401", rec.Code)
	}
}

// TestPublicPlaneAllowsAnonymous 验证 Public 平面匿名可访问（agent.md §6）。
func TestPublicPlaneAllowsAnonymous(t *testing.T) {
	t.Parallel()

	authenticator := stubAuthenticator{}
	root, planes := NewRouter(&Options{Authenticator: authenticator})

	planes.Public(func(r chi.Router) {
		r.Get("/posts", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("published"))
		})
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, PrefixPublic+"/posts", http.NoBody)
	root.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Public 平面应允许匿名访问：状态码 = %d，期望 200", rec.Code)
	}
}

// TestAnonymousWriteReturns401NotForbidden 验证匿名写请求得到 401 而非 403。
//
// 顺序很关键：CSRF 中间件若排在 RequireAuth 之前且对匿名请求返回 403，
// 客户端会以为是自己令牌有误，而不知道需要先登录。
func TestAnonymousWriteReturns401NotForbidden(t *testing.T) {
	t.Parallel()

	authenticator := stubAuthenticator{}
	root, planes := NewRouter(&Options{Authenticator: authenticator})

	planes.Console(func(r chi.Router) {
		r.Post("/posts", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("created"))
		})
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, PrefixConsole+"/posts", http.NoBody)
	root.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("匿名写请求状态码 = %d，期望 401", rec.Code)
	}
}

// TestNilAuthenticatorKeepsSkeletonUsable 验证未配置认证时路由骨架仍可用，
// 便于阶段 1 的骨架测试与诊断端点。
func TestNilAuthenticatorKeepsSkeletonUsable(t *testing.T) {
	t.Parallel()

	root, planes := NewRouter(&Options{})

	planes.Console(func(r chi.Router) {
		r.Get("/open", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("open"))
		})
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, PrefixConsole+"/open", http.NoBody)
	root.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("未配置认证时骨架应放行：状态码 = %d", rec.Code)
	}
}
