package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/FeiBaiKin/lumo/internal/httpx"
)

// TestErrorsAreProblemJSON 验证 404 与 405 也走统一错误格式。
// 若这里回归，API 会同时存在两套错误格式，客户端无法统一处理。
func TestErrorsAreProblemJSON(t *testing.T) {
	t.Parallel()

	root, _ := NewRouter(&Options{})

	tests := []struct {
		name       string
		method     string
		target     string
		wantStatus int
	}{
		{"未匹配路径返回 404", http.MethodGet, "/api/v1/console/nope", http.StatusNotFound},
		{"不支持的方法返回 405", http.MethodDelete, "/", http.StatusMethodNotAllowed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(t.Context(), tt.method, tt.target, http.NoBody)
			root.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("状态码 = %d，期望 %d", rec.Code, tt.wantStatus)
			}
			if ct := rec.Header().Get("Content-Type"); ct != httpx.ContentTypeProblem {
				t.Errorf("Content-Type = %q，期望 %q", ct, httpx.ContentTypeProblem)
			}
			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("响应不是合法 JSON: %v", err)
			}
			if body["status"] != float64(tt.wantStatus) {
				t.Errorf("problem.status = %v", body["status"])
			}
		})
	}
}

// TestThreePlanesMounted 验证三平面挂载点存在且模块注册的路由可达。
func TestThreePlanesMounted(t *testing.T) {
	t.Parallel()

	root, planes := NewRouter(&Options{})

	planes.Console(func(r chi.Router) {
		r.Get("/ping", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("console"))
		})
	})
	planes.Public(func(r chi.Router) {
		r.Get("/ping", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("public"))
		})
	})
	planes.Extension(func(r chi.Router) {
		r.Get("/ping", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("extension"))
		})
	})

	tests := []struct {
		target string
		want   string
	}{
		{PrefixConsole + "/ping", "console"},
		{PrefixPublic + "/ping", "public"},
		{PrefixExtension + "/ping", "extension"},
	}

	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tt.target, http.NoBody)
			root.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("状态码 = %d，期望 200", rec.Code)
			}
			if got := rec.Body.String(); got != tt.want {
				t.Errorf("响应体 = %q，期望 %q", got, tt.want)
			}
		})
	}
}

// TestPlanePrefixes 固定三平面前缀，避免被无意改动破坏 API 契约（agent.md §6）。
func TestPlanePrefixes(t *testing.T) {
	t.Parallel()

	if PrefixConsole != "/api/v1/console" {
		t.Errorf("Console 前缀 = %q", PrefixConsole)
	}
	if PrefixPublic != "/api/v1/public" {
		t.Errorf("Public 前缀 = %q", PrefixPublic)
	}
	if PrefixExtension != "/apis" {
		t.Errorf("Extension 前缀 = %q", PrefixExtension)
	}
}

// TestRootRedirectsToConsole 验证根路径跳转到 Console。
func TestRootRedirectsToConsole(t *testing.T) {
	t.Parallel()

	root, _ := NewRouter(&Options{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody)
	root.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("状态码 = %d，期望 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/console/" {
		t.Errorf("Location = %q，期望 /console/", loc)
	}
}

// TestRecovererReturnsProblemJSON 验证 panic 被捕获且返回统一错误格式，
// 同时不得把 panic 细节泄漏给客户端。
func TestRecovererReturnsProblemJSON(t *testing.T) {
	t.Parallel()

	const secret = "s3cr3t-must-not-leak"

	root, planes := NewRouter(&Options{})
	planes.Public(func(r chi.Router) {
		r.Get("/boom", func(http.ResponseWriter, *http.Request) {
			panic("内部实现细节：" + secret)
		})
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, PrefixPublic+"/boom", http.NoBody)
	root.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("状态码 = %d，期望 500", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != httpx.ContentTypeProblem {
		t.Errorf("Content-Type = %q", ct)
	}
	if strings.Contains(rec.Body.String(), secret) {
		t.Fatalf("panic 细节泄漏到响应: %q", rec.Body.String())
	}
}
