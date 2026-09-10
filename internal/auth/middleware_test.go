package auth_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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

func TestRequirePermission(t *testing.T) {
	t.Parallel()

	t.Run("匿名请求返回 401 而非 403", func(t *testing.T) {
		t.Parallel()
		handler := auth.RequirePermission(perm.PostsWrite)(okHandler())
		rec := serveWith(t, nil, handler)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("状态码 = %d，期望 401（未认证应优先于无权限）", rec.Code)
		}
	})

	t.Run("权限不足返回 403 并列出所需权限", func(t *testing.T) {
		t.Parallel()
		handler := auth.RequirePermission(perm.UsersManage)(okHandler())
		principal := auth.NewSessionPrincipal(testUser(1, "author", perm.PostsWrite))
		rec := serveWith(t, principal, handler)

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
		handler := auth.RequirePermission(perm.UsersManage)(okHandler())
		principal := auth.NewSessionPrincipal(testUser(1, "admin", perm.UsersManage))
		rec := serveWith(t, principal, handler)

		if rec.Code != http.StatusOK {
			t.Fatalf("状态码 = %d，期望 200", rec.Code)
		}
	})

	t.Run("满足其一即可", func(t *testing.T) {
		t.Parallel()
		handler := auth.RequirePermission(perm.UsersManage, perm.RolesManage)(okHandler())
		principal := auth.NewSessionPrincipal(testUser(1, "admin", perm.RolesManage))
		rec := serveWith(t, principal, handler)

		if rec.Code != http.StatusOK {
			t.Fatalf("状态码 = %d，期望 200", rec.Code)
		}
	})

	t.Run("中间件不做所有权判定", func(t *testing.T) {
		t.Parallel()
		// author 持有 posts:write 但只能操作自己的对象；
		// 中间件只检查「是否直接持有」，所有权由处理器负责。
		handler := auth.RequirePermission(perm.PostsWrite)(okHandler())
		principal := auth.NewSessionPrincipal(testUser(1, "author", perm.PostsWrite))
		rec := serveWith(t, principal, handler)

		if rec.Code != http.StatusOK {
			t.Fatalf("状态码 = %d，期望 200", rec.Code)
		}
	})
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
