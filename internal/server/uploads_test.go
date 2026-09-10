package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRequestSizeSeparatesUploads 验证 multipart 请求走放宽后的上限，其余请求走普通上限。
//
// 两条上限混用是这块最容易写错的地方：写小了正常上传被截断，写大了等于给所有
// JSON 接口开了同样大的口子。
func TestRequestSizeSeparatesUploads(t *testing.T) {
	t.Parallel()

	const (
		smallLimit = 16
		largeLimit = 1024
	)
	handler := requestSize(smallLimit, largeLimit)(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if _, err := io.ReadAll(r.Body); err != nil {
				w.WriteHeader(http.StatusRequestEntityTooLarge)
				return
			}
			w.WriteHeader(http.StatusOK)
		}))

	cases := []struct {
		name        string
		contentType string
		size        int
		want        int
	}{
		{name: "普通请求在上限内", contentType: "application/json", size: smallLimit, want: http.StatusOK},
		{name: "普通请求超上限", contentType: "application/json", size: smallLimit + 1, want: http.StatusRequestEntityTooLarge},
		{name: "上传请求可超普通上限", contentType: "multipart/form-data; boundary=x", size: largeLimit, want: http.StatusOK},
		{name: "上传请求超上传上限", contentType: "multipart/form-data; boundary=x", size: largeLimit + 1, want: http.StatusRequestEntityTooLarge},
		{name: "无内容类型按普通上限", contentType: "", size: smallLimit + 1, want: http.StatusRequestEntityTooLarge},
	}
	for _, c := range cases {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x",
			strings.NewReader(strings.Repeat("a", c.size)))
		if c.contentType != "" {
			req.Header.Set("Content-Type", c.contentType)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != c.want {
			t.Errorf("%s：状态码 = %d，期望 %d", c.name, rec.Code, c.want)
		}
	}
}

// TestUploadsHandlerHardensResponses 验证附件静态路由带上防护响应头并挡住越界路径。
//
// 上传内容与 Console 同源，一个能执行脚本的 SVG 就是一次存储型 XSS。
func TestUploadsHandlerHardensResponses(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "2026", "09"), 0o750); err != nil {
		t.Fatalf("准备目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "2026", "09", "a.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatalf("准备文件失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(dir), "secret.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatalf("准备文件失败: %v", err)
	}

	root, _ := NewRouter(&Options{UploadsDir: dir})

	rec := do(t, root, http.MethodGet, UploadsPath+"/2026/09/a.txt", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200", rec.Code)
	}
	if rec.Body.String() != "hello" {
		t.Errorf("内容 = %q，期望 hello", rec.Body.String())
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q，期望 nosniff", got)
	}
	csp := rec.Header().Get("Content-Security-Policy")
	for _, want := range []string{"default-src 'none'", "sandbox"} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP 应包含 %q，实际 %q", want, csp)
		}
	}

	// 越界路径与不存在的文件都不该泄漏任何内容。
	for _, target := range []string{
		UploadsPath + "/../secret.txt",
		UploadsPath + "/2026/09/nope.txt",
	} {
		rec := do(t, root, http.MethodGet, target, "")
		if rec.Code == http.StatusOK {
			t.Errorf("%s 不应返回 200：%s", target, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "secret") {
			t.Errorf("%s 泄漏了目录之外的内容", target)
		}
	}
}

// TestUploadsRouteAbsentWhenNotConfigured 验证未配置目录时不挂载该路由。
func TestUploadsRouteAbsentWhenNotConfigured(t *testing.T) {
	t.Parallel()

	root, _ := NewRouter(&Options{})
	rec := do(t, root, http.MethodGet, UploadsPath+"/a.txt", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("状态码 = %d，期望 404", rec.Code)
	}
}
