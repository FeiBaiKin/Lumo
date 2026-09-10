package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProblemMarshalDefaults(t *testing.T) {
	t.Parallel()

	p := &Problem{Status: http.StatusNotFound}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if got["type"] != "about:blank" {
		t.Errorf("type = %v，期望 about:blank", got["type"])
	}
	if got["title"] != "Not Found" {
		t.Errorf("title = %v，期望 Not Found", got["title"])
	}
	if got["status"] != float64(http.StatusNotFound) {
		t.Errorf("status = %v", got["status"])
	}
}

// TestProblemExtensionsFlattened 验证扩展成员平铺到顶层，
// 且不得覆盖标准成员——否则响应会自相矛盾。
func TestProblemExtensionsFlattened(t *testing.T) {
	t.Parallel()

	p := &Problem{
		Status: http.StatusBadRequest,
		Title:  "Bad Request",
		Extensions: map[string]any{
			"errors": []any{"字段 title 不能为空"},
			"status": 999, // 必须被忽略
		},
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if _, ok := got["errors"]; !ok {
		t.Error("扩展成员 errors 未平铺到顶层")
	}
	if got["status"] != float64(http.StatusBadRequest) {
		t.Errorf("扩展成员不应覆盖标准成员 status，实际 %v", got["status"])
	}
}

func TestWriteProblemHeaders(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/console/posts", http.NoBody)
	WriteProblem(rec, req, NewProblem(http.StatusForbidden, "权限不足"), nil)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("状态码 = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != ContentTypeProblem {
		t.Errorf("Content-Type = %q，期望 %q", ct, ContentTypeProblem)
	}
	// 错误响应不应被缓存。
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q，期望 no-store", cc)
	}

	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	// instance 应自动填为请求路径。
	if got["instance"] != "/api/v1/console/posts" {
		t.Errorf("instance = %v", got["instance"])
	}
}

func TestWriteProblemHeadHasNoBody(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodHead, "/x", http.NoBody)
	WriteProblem(rec, req, NewProblem(http.StatusInternalServerError, "boom"), nil)

	if rec.Body.Len() != 0 {
		t.Errorf("HEAD 响应体应为空，实际 %q", rec.Body.String())
	}
}

// TestWriteErrorHidesInternalDetail 是安全相关测试：
// 未分类的内部错误不得把细节回传客户端。
func TestWriteErrorHidesInternalDetail(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x", http.NoBody)
	const secret = "s3cr3t-must-not-leak"
	WriteError(rec, req, errors.New("数据库连接失败：postgres:"+secret), nil)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("状态码 = %d", rec.Code)
	}
	if body := rec.Body.String(); contains(body, secret) {
		t.Fatalf("内部错误细节泄漏到响应: %q", body)
	}
}

// TestWriteErrorUsesProblemFromChain 验证 error 链上的 *Problem 被沿用。
func TestWriteErrorUsesProblemFromChain(t *testing.T) {
	t.Parallel()

	base := NewProblem(http.StatusConflict, "名称已存在")
	wrapped := fmt.Errorf("创建分类: %w", base)

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x", http.NoBody)
	WriteError(rec, req, wrapped, nil)

	if rec.Code != http.StatusConflict {
		t.Fatalf("状态码 = %d，期望 %d", rec.Code, http.StatusConflict)
	}
	if !contains(rec.Body.String(), "名称已存在") {
		t.Errorf("未沿用链上 Problem 的说明: %q", rec.Body.String())
	}
}

func TestProblemError(t *testing.T) {
	t.Parallel()

	withDetail := NewProblem(http.StatusNotFound, "文章不存在")
	if got := withDetail.Error(); !contains(got, "文章不存在") || !contains(got, "404") {
		t.Errorf("Error() = %q", got)
	}

	noDetail := NewProblem(http.StatusNotFound, "")
	if got := noDetail.Error(); !contains(got, "Not Found") {
		t.Errorf("Error() = %q", got)
	}
}

func TestWriteJSON(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x", http.NoBody)
	WriteJSON(rec, req, http.StatusCreated, map[string]string{"name": "hello"})

	if rec.Code != http.StatusCreated {
		t.Fatalf("状态码 = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
}

// contains 是 strings.Contains 的别名，仅为让断言语句更短。
func contains(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}
