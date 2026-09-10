package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"

	"github.com/FeiBaiKin/lumo/internal/httpx"
)

// TestNewErrorProducesProblem 验证 huma 的错误构造已桥接到 httpx.Problem，
// 且校验明细被逐条转换。
func TestNewErrorProducesProblem(t *testing.T) {
	t.Parallel()

	err := huma.Error422UnprocessableEntity("validation failed",
		&huma.ErrorDetail{Message: "expected string", Location: "body.title", Value: 1},
		&huma.ErrorDetail{Message: "expected integer", Location: "query.page", Value: "x"},
		errors.New("plain"),
		nil,
	)
	problem, ok := err.(*httpx.Problem)
	if !ok {
		t.Fatalf("错误类型 = %T，期望 *httpx.Problem", err)
	}
	if problem.Status != http.StatusUnprocessableEntity || problem.Detail != "validation failed" {
		t.Errorf("problem = %+v", problem)
	}
	if len(problem.Errors) != 3 {
		t.Fatalf("errors 明细数量 = %d，期望 3（nil 应被跳过）", len(problem.Errors))
	}
	if problem.Errors[0].Location != "body.title" || problem.Errors[2].Message != "plain" {
		t.Errorf("errors 明细 = %+v", problem.Errors)
	}
	// 请求体的值不回显（可能含密码），查询串的值保留以便排查。
	if problem.Errors[0].Value != nil {
		t.Errorf("body 位置的值不应回显: %v", problem.Errors[0].Value)
	}
	if problem.Errors[1].Value != "x" {
		t.Errorf("query 位置的值应保留: %v", problem.Errors[1].Value)
	}
	if ct := problem.ContentType("application/json"); ct != httpx.ContentTypeProblem {
		t.Errorf("ContentType = %q", ct)
	}

	raw, marshalErr := json.Marshal(problem)
	if marshalErr != nil {
		t.Fatalf("序列化失败: %v", marshalErr)
	}
	if !strings.Contains(string(raw), `"errors":[`) {
		t.Errorf("序列化结果缺少 errors: %s", raw)
	}
}

// TestServerErrorHidesDetails 验证 5xx 经上下文构造时不回传任何内部细节。
func TestServerErrorHidesDetails(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/public/x", http.NoBody)
	ctx := humachi.NewContext(&huma.Operation{OperationID: "x"}, req, httptest.NewRecorder())

	err := huma.NewErrorWithContext(ctx, http.StatusInternalServerError, "unexpected error occurred",
		errors.New("pq: connection refused"))
	problem, ok := err.(*httpx.Problem)
	if !ok {
		t.Fatalf("错误类型 = %T", err)
	}
	if problem.Detail != "" || len(problem.Errors) != 0 {
		t.Errorf("5xx 不应携带内部细节: %+v", problem)
	}
	if problem.Status != http.StatusInternalServerError {
		t.Errorf("状态码 = %d", problem.Status)
	}

	// 4xx 走同一入口时应保留明细。
	client := huma.NewErrorWithContext(ctx, http.StatusBadRequest, "bad", errors.New("why"))
	if p, ok := client.(*httpx.Problem); !ok || p.Detail != "bad" || len(p.Errors) != 1 {
		t.Errorf("4xx 应保留明细: %+v", client)
	}
}

// TestPageParams 固定分页约定：默认值、上限与 offset 换算（agent.md §6）。
func TestPageParams(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		in         PageParams
		wantOffset int
		wantLimit  int
	}{
		{"零值补默认", PageParams{}, 0, DefaultPageSize},
		{"第二页", PageParams{Page: 2, Size: 10}, 10, 10},
		{"超上限收敛", PageParams{Page: 3, Size: 1000}, 2 * MaxPageSize, MaxPageSize},
		{"负数纠正", PageParams{Page: -1, Size: -5}, 0, DefaultPageSize},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.in.Offset(); got != tt.wantOffset {
				t.Errorf("Offset = %d，期望 %d", got, tt.wantOffset)
			}
			if got := tt.in.Limit(); got != tt.wantLimit {
				t.Errorf("Limit = %d，期望 %d", got, tt.wantLimit)
			}
		})
	}
}

// TestNewPageNeverNullItems 验证空列表输出 [] 而非 null，前端不必做空值判断。
func TestNewPageNeverNullItems(t *testing.T) {
	t.Parallel()

	page := NewPage[string](nil, PageParams{Page: 2, Size: 5}, 0)
	raw, err := json.Marshal(page)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	if string(raw) != `{"items":[],"page":2,"size":5,"total":0}` {
		t.Errorf("序列化结果 = %s", raw)
	}
}
