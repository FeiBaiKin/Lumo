package console

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

// 阶段 0 的单元测试聚焦 SPA 静态服务的行为契约：
// 真实文件命中、history 路由回退、静态资源 404、缓存头与方法限制。
// 这些规则一旦回归，Console 会出现「白屏」或「把 HTML 当 JS 解析」这类难查问题。

func testAssets() fstest.MapFS {
	return fstest.MapFS{
		"index.html":         {Data: []byte("<!doctype html><title>lumo</title>")},
		"assets/app-abc.js":  {Data: []byte("console.log(1)")},
		"assets/app-abc.css": {Data: []byte(".a{}")},
		"favicon.ico":        {Data: []byte("icon")},
	}
}

func TestSPAHandler(t *testing.T) {
	t.Parallel()

	handler := spaHandler(testAssets())

	tests := []struct {
		name        string
		method      string
		target      string
		wantStatus  int
		wantCache   string
		wantHTML    bool
		wantAllowed string
	}{
		{
			name:       "根路径返回入口文件",
			method:     http.MethodGet,
			target:     "/",
			wantStatus: http.StatusOK,
			wantCache:  "no-cache",
			wantHTML:   true,
		},
		{
			name:       "命中静态资源使用长缓存",
			method:     http.MethodGet,
			target:     "/assets/app-abc.js",
			wantStatus: http.StatusOK,
			wantCache:  "public, max-age=31536000, immutable",
		},
		{
			name:       "非 assets 目录下的文件不长缓存",
			method:     http.MethodGet,
			target:     "/favicon.ico",
			wantStatus: http.StatusOK,
			wantCache:  "no-cache",
		},
		{
			name:       "无扩展名路径回退到入口文件",
			method:     http.MethodGet,
			target:     "/posts/123/edit",
			wantStatus: http.StatusOK,
			wantCache:  "no-cache",
			wantHTML:   true,
		},
		{
			name:       "缺失的静态资源返回 404 而非入口文件",
			method:     http.MethodGet,
			target:     "/assets/missing.js",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "HEAD 请求不返回响应体",
			method:     http.MethodHead,
			target:     "/dashboard",
			wantStatus: http.StatusOK,
		},
		{
			name:        "不支持的方法返回 405",
			method:      http.MethodPost,
			target:      "/",
			wantStatus:  http.StatusMethodNotAllowed,
			wantAllowed: "GET, HEAD",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(
				t.Context(), tt.method, tt.target, http.NoBody)
			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("状态码 = %d，期望 %d", rec.Code, tt.wantStatus)
			}
			if tt.wantCache != "" {
				if got := rec.Header().Get("Cache-Control"); got != tt.wantCache {
					t.Errorf("Cache-Control = %q，期望 %q", got, tt.wantCache)
				}
			}
			if tt.wantAllowed != "" {
				if got := rec.Header().Get("Allow"); got != tt.wantAllowed {
					t.Errorf("Allow = %q，期望 %q", got, tt.wantAllowed)
				}
			}
			if tt.wantHTML && rec.Body.String() != "<!doctype html><title>lumo</title>" {
				t.Errorf("响应体未返回入口文件，实际 = %q", rec.Body.String())
			}
			if tt.method == http.MethodHead && rec.Body.Len() != 0 {
				t.Errorf("HEAD 响应体应为空，实际长度 = %d", rec.Body.Len())
			}
		})
	}
}

// TestHandlerNotBuilt 验证前端未构建时返回可操作提示而非白屏。
func TestHandlerNotBuilt(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(), http.MethodGet, "/", http.NoBody)
	http.HandlerFunc(notBuilt).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("状态码 = %d，期望 %d", rec.Code, http.StatusNotImplemented)
	}
	if rec.Body.Len() == 0 {
		t.Error("提示信息不应为空")
	}
}

// TestAssetsAlwaysAvailable 保证 embed 的 dist 目录始终可访问，
// 即使前端未构建（此时目录内只有 .gitkeep）。
func TestAssetsAlwaysAvailable(t *testing.T) {
	t.Parallel()

	if _, err := Assets(); err != nil {
		t.Fatalf("Assets() 返回错误: %v", err)
	}
}

// TestMountPathShape 固定挂载路径的形状：
// 必须以斜杠开头并以斜杠结尾，否则 http.ServeMux 的子树匹配与
// StripPrefix 的行为都会出错，且需与 vite.config.ts 的 base 保持一致。
func TestMountPathShape(t *testing.T) {
	t.Parallel()

	if MountPath == "" || MountPath[0] != '/' {
		t.Fatalf("MountPath 必须以 / 开头，实际 = %q", MountPath)
	}
	if MountPath[len(MountPath)-1] != '/' {
		t.Fatalf("MountPath 必须以 / 结尾，实际 = %q", MountPath)
	}
}
