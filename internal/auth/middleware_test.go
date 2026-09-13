package auth_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"

	"github.com/FeiBaiKin/lumo/internal/auth"
	"github.com/FeiBaiKin/lumo/internal/auth/perm"
	"github.com/FeiBaiKin/lumo/internal/httpx"
)

// serveWith 以给定调用者（可为 nil 表示匿名）执行 next，返回响应记录器。
func serveWith(t *testing.T, principal *auth.Principal, next http.Handler) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/console/posts", http.NoBody)
	if principal != nil {
		req = req.WithContext(auth.WithPrincipal(req.Context(), principal))
	}

	rec := httptest.NewRecorder()
	next.ServeHTTP(rec, req)
	return rec
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
}

func TestRequireAuth(t *testing.T) {
	t.Parallel()

	handler := auth.RequireAuth(okHandler())

	t.Run("匿名请求返回 401", func(t *testing.T) {
		t.Parallel()
		rec := serveWith(t, nil, handler)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("状态码 = %d，期望 401", rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != httpx.ContentTypeProblem {
			t.Errorf("Content-Type = %q，期望 %q", ct, httpx.ContentTypeProblem)
		}
	})

	t.Run("已认证请求放行", func(t *testing.T) {
		t.Parallel()
		principal := auth.NewSessionPrincipal(testUser(1, "author", perm.PostsWrite))
		rec := serveWith(t, principal, handler)

		if rec.Code != http.StatusOK {
			t.Fatalf("状态码 = %d，期望 200", rec.Code)
		}
		if rec.Body.String() != "ok" {
			t.Errorf("响应体 = %q", rec.Body.String())
		}
	})
}

// runPermission 用 huma 上下文执行权限中间件；放行时 next 写出 200 "ok"。
func runPermission(t *testing.T, principal *auth.Principal, permissions ...perm.Permission) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/console/users", http.NoBody)
	if principal != nil {
		req = req.WithContext(auth.WithPrincipal(req.Context(), principal))
	}
	rec := httptest.NewRecorder()
	ctx := humachi.NewContext(&huma.Operation{OperationID: "test"}, req, rec)

	auth.RequirePermission(permissions...)(ctx, func(c huma.Context) {
		c.SetStatus(http.StatusOK)
		_, _ = c.BodyWriter().Write([]byte("ok"))
	})
	return rec
}

func TestRequirePermission(t *testing.T) {
	t.Parallel()

	t.Run("匿名请求返回 401 而非 403", func(t *testing.T) {
		t.Parallel()
		rec := runPermission(t, nil, perm.PostsWrite)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("状态码 = %d，期望 401（未认证应优先于无权限）", rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != httpx.ContentTypeProblem {
			t.Errorf("Content-Type = %q，期望 %q", ct, httpx.ContentTypeProblem)
		}
	})

	t.Run("权限不足返回 403 并列出所需权限", func(t *testing.T) {
		t.Parallel()
		principal := auth.NewSessionPrincipal(testUser(1, "author", perm.PostsWrite))
		rec := runPermission(t, principal, perm.UsersManage)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("状态码 = %d，期望 403", rec.Code)
		}

		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("响应不是合法 JSON: %v", err)
		}
		required, ok := body["requiredPermissions"].([]any)
		if !ok || len(required) != 1 || required[0] != "users:manage" {
			t.Errorf("requiredPermissions = %v，期望 [users:manage]", body["requiredPermissions"])
		}
	})

	t.Run("持有权限则放行", func(t *testing.T) {
		t.Parallel()
		principal := auth.NewSessionPrincipal(testUser(1, "admin", perm.UsersManage))
		rec := runPermission(t, principal, perm.UsersManage)

		if rec.Code != http.StatusOK {
			t.Fatalf("状态码 = %d，期望 200", rec.Code)
		}
		if rec.Body.String() != "ok" {
			t.Errorf("响应体 = %q", rec.Body.String())
		}
	})

	t.Run("满足其一即可", func(t *testing.T) {
		t.Parallel()
		principal := auth.NewSessionPrincipal(testUser(1, "admin", perm.RolesManage))
		rec := runPermission(t, principal, perm.UsersManage, perm.RolesManage)

		if rec.Code != http.StatusOK {
			t.Fatalf("状态码 = %d，期望 200", rec.Code)
		}
	})

	t.Run("中间件不做所有权判定", func(t *testing.T) {
		t.Parallel()
		// author 持有 posts:write 但只能操作自己的对象；
		// 中间件只检查「是否直接持有」，所有权由处理器负责。
		principal := auth.NewSessionPrincipal(testUser(1, "author", perm.PostsWrite))
		rec := runPermission(t, principal, perm.PostsWrite)

		if rec.Code != http.StatusOK {
			t.Fatalf("状态码 = %d，期望 200", rec.Code)
		}
	})
}

// TestForbiddenProblemCarriesPermissions 验证处理器内所有权判定失败时返回的错误可直接给 huma。
func TestForbiddenProblemCarriesPermissions(t *testing.T) {
	t.Parallel()

	problem := auth.ForbiddenProblem(perm.PostsDeleteAny)
	if problem.GetStatus() != http.StatusForbidden {
		t.Errorf("状态码 = %d", problem.GetStatus())
	}
	raw, err := json.Marshal(problem)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	required, ok := body["requiredPermissions"].([]any)
	if !ok || len(required) != 1 || required[0] != "posts:delete_any" {
		t.Errorf("requiredPermissions = %v", body["requiredPermissions"])
	}
}

func TestBearerToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		header string
		want   string
	}{
		{"Bearer lumo_pat_abc", "lumo_pat_abc"},
		{"bearer lumo_pat_abc", "lumo_pat_abc"}, // scheme 大小写不敏感
		{"BEARER lumo_pat_abc", "lumo_pat_abc"},
		{"Bearer   lumo_pat_abc  ", "lumo_pat_abc"},
		{"", ""},
		{"Bearer", ""},
		{"Bearer ", ""},
		{"Basic dXNlcjpwYXNz", ""},
		{"lumo_pat_abc", ""}, // 缺少 scheme
	}

	for _, tt := range tests {
		if got := auth.BearerToken(tt.header); got != tt.want {
			t.Errorf("BearerToken(%q) = %q，期望 %q", tt.header, got, tt.want)
		}
	}
}

// TestHashTokenIsStableAndIrreversible 固定令牌哈希的行为：
// 同一输入恒定、不同输入不同、且不包含原文。
func TestHashTokenIsStableAndIrreversible(t *testing.T) {
	t.Parallel()

	const token = "lumo_pat_supersecrettoken"
	first := auth.HashToken(token)
	second := auth.HashToken(token)

	if first != second {
		t.Fatal("同一令牌的哈希应恒定")
	}
	if len(first) != 64 {
		t.Errorf("SHA-256 十六进制长度应为 64，实际 %d", len(first))
	}
	if auth.HashToken("other") == first {
		t.Error("不同令牌不应产生相同哈希")
	}
	if first == token {
		t.Error("哈希不应等于原文")
	}
}

func TestConstantTimeEqual(t *testing.T) {
	t.Parallel()

	if !auth.ConstantTimeEqual("abc", "abc") {
		t.Error("相同字符串应返回 true")
	}
	if auth.ConstantTimeEqual("abc", "abd") {
		t.Error("不同字符串应返回 false")
	}
	if auth.ConstantTimeEqual("abc", "abcd") {
		t.Error("长度不同应返回 false")
	}
	if !auth.ConstantTimeEqual("", "") {
		t.Error("两个空串应返回 true")
	}
}

// TestOptionalDoesNotForceAuthentication 验证前台用的宽松鉴权中间件。
//
// 与 Middleware 的差别只有一条，但它决定了访客前台的可用性：
// 凭据无效时这里必须按匿名继续（顺手清掉失效 Cookie），而不是返回 401 ——
// 一个带着过期 Cookie 的访客打开首页，该看到首页，而不是一段 JSON 错误。
//
// 三条用例共用一个 Authenticator，但会话各不相同；不用 t.Parallel：
// 同包的其他用例会重建整个 schema。
func TestOptionalDoesNotForceAuthentication(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	user := e.createUser(t, "optuser", perm.RoleAuthor)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, ok := auth.FromContext(r.Context())
		if ok {
			w.Header().Set("X-Test-User", strconv.FormatInt(principal.UserID(), 10))
		}
		w.WriteHeader(http.StatusOK)
	})
	handler := e.authn.Optional(next)

	do := func(t *testing.T, cookie *http.Cookie) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody)
		if cookie != nil {
			req.AddCookie(cookie)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	t.Run("有效会话注入调用者", func(t *testing.T) {
		issued, err := e.sessions.Create(ctx, user.ID, "test-ua", "127.0.0.1")
		if err != nil {
			t.Fatalf("签发会话失败: %v", err)
		}
		rec := do(t, &http.Cookie{Name: e.sessions.SessionCookieName(), Value: issued.Token})

		if rec.Code != http.StatusOK {
			t.Fatalf("状态码 = %d，期望 200", rec.Code)
		}
		if got := rec.Header().Get("X-Test-User"); got != strconv.FormatInt(user.ID, 10) {
			t.Errorf("注入的用户 ID = %q，期望 %d", got, user.ID)
		}
	})

	t.Run("过期会话按匿名继续并清除 Cookie", func(t *testing.T) {
		issued, err := e.sessions.Create(ctx, user.ID, "test-ua", "127.0.0.1")
		if err != nil {
			t.Fatalf("签发会话失败: %v", err)
		}
		// 直接把会话拨到过去。走 SQL 而不是等，是因为 SessionTTL 是 7 天。
		if _, err := e.db.ExecContext(ctx,
			"UPDATE sessions SET expires_at = now() - interval '1 hour' WHERE token_hash = ?",
			auth.HashToken(issued.Token)); err != nil {
			t.Fatalf("使会话过期失败: %v", err)
		}

		rec := do(t, &http.Cookie{Name: e.sessions.SessionCookieName(), Value: issued.Token})

		if rec.Code != http.StatusOK {
			t.Fatalf("状态码 = %d，期望 200（前台不该因凭据失效而报错）", rec.Code)
		}
		if got := rec.Header().Get("X-Test-User"); got != "" {
			t.Errorf("过期会话不该注入调用者，实际 %q", got)
		}
		cleared := false
		for _, c := range rec.Result().Cookies() {
			if c.Name == e.sessions.SessionCookieName() && c.MaxAge < 0 {
				cleared = true
			}
		}
		if !cleared {
			t.Error("失效会话的 Cookie 应被清除，否则浏览器会一直带着它重复请求")
		}
	})

	t.Run("无 Cookie 时匿名放行", func(t *testing.T) {
		rec := do(t, nil)

		if rec.Code != http.StatusOK {
			t.Fatalf("状态码 = %d，期望 200", rec.Code)
		}
		if got := rec.Header().Get("X-Test-User"); got != "" {
			t.Errorf("匿名请求不该注入调用者，实际 %q", got)
		}
		if header := rec.Header().Get("Set-Cookie"); header != "" {
			t.Errorf("匿名请求不该下发任何 Cookie，实际 %q", header)
		}
	})
}
