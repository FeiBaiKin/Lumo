package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/FeiBaiKin/lumo/internal/api"
	"github.com/FeiBaiKin/lumo/internal/httpx"
)

// pingOutput 是接线测试用的最小响应体。
type pingOutput struct {
	Body struct {
		Plane string `json:"plane"`
	}
}

// registerPing 在给定注册面挂一个 GET /ping，回显平面名。
func registerPing(target huma.API, id, plane string) {
	huma.Register(target, huma.Operation{OperationID: id, Method: http.MethodGet, Path: "/ping"},
		func(context.Context, *struct{}) (*pingOutput, error) {
			out := &pingOutput{}
			out.Body.Plane = plane
			return out, nil
		})
}

// do 发起请求并返回记录器；body 非空时按 JSON 提交。
func do(t *testing.T, root http.Handler, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()

	var reader io.Reader = http.NoBody
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequestWithContext(t.Context(), method, target, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	root.ServeHTTP(rec, req)
	return rec
}

// decodeProblem 断言响应是 problem+json 并解析。
func decodeProblem(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	if ct := rec.Header().Get("Content-Type"); ct != httpx.ContentTypeProblem {
		t.Fatalf("Content-Type = %q，期望 %q", ct, httpx.ContentTypeProblem)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	return body
}

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
		// 用 /api/docs 而非 /：根路径在装配主题后由前台接管，本测试构造的是
		// 不带主题的裸骨架，那时 / 上没有任何方法可言，走的是 404 而非 405。
		// /api/docs 由 huma 在 chi 上注册为 GET，是稳定的「路径存在、方法不对」样本。
		{"不支持的方法返回 405", http.MethodDelete, api.DocsPath, http.StatusMethodNotAllowed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := do(t, root, tt.method, tt.target, "")
			if rec.Code != tt.wantStatus {
				t.Fatalf("状态码 = %d，期望 %d", rec.Code, tt.wantStatus)
			}
			body := decodeProblem(t, rec)
			if body["status"] != float64(tt.wantStatus) {
				t.Errorf("problem.status = %v", body["status"])
			}
		})
	}
}

// TestThreePlanesMounted 验证三平面挂载点存在且模块注册的操作可达。
func TestThreePlanesMounted(t *testing.T) {
	t.Parallel()

	root, planes := NewRouter(&Options{})
	registerPing(planes.Console(), "console-ping", "console")
	registerPing(planes.Public(), "public-ping", "public")
	registerPing(planes.Extension(), "extension-ping", "extension")

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

			rec := do(t, root, http.MethodGet, tt.target, "")
			if rec.Code != http.StatusOK {
				t.Fatalf("状态码 = %d，期望 200：%s", rec.Code, rec.Body.String())
			}
			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("响应不是合法 JSON: %v", err)
			}
			if body["plane"] != tt.want {
				t.Errorf("plane = %v，期望 %q", body["plane"], tt.want)
			}
			// 响应体不应混入 $schema 之类的附加字段。
			if _, leaked := body["$schema"]; leaked {
				t.Error("响应体不应包含 $schema 字段")
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

// TestRootIsFreeForFrontend 验证根路径默认不被核心路由占用。
//
// 阶段 4 起根路径属于主题渲染的访客前台（agent.md §3.1、§4.2），
// 由 theme 模块在全部模块注册之后挂载。核心路由若在此处抢先注册一个
// 重定向，前台首页就永远到不了——这条断言是防止那次回归。
func TestRootIsFreeForFrontend(t *testing.T) {
	t.Parallel()

	root, _ := NewRouter(&Options{})
	rec := do(t, root, http.MethodGet, "/", "")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("状态码 = %d，期望 404（根路径应留给前台）", rec.Code)
	}
	decodeProblem(t, rec)
}

// TestRootFallsBackToConsole 验证显式开启回退开关时根路径跳转到 Console。
//
// 这是没有装配主题模块的场景（如未来的纯 API 模式）的兜底行为。
func TestRootFallsBackToConsole(t *testing.T) {
	t.Parallel()

	root, _ := NewRouter(&Options{FallbackRootToConsole: true})
	rec := do(t, root, http.MethodGet, "/", "")

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
	huma.Register(planes.Public(), huma.Operation{OperationID: "boom", Method: http.MethodGet, Path: "/boom"},
		func(context.Context, *struct{}) (*pingOutput, error) {
			panic("内部实现细节：" + secret)
		})

	rec := do(t, root, http.MethodGet, PrefixPublic+"/boom", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("状态码 = %d，期望 500", rec.Code)
	}
	decodeProblem(t, rec)
	if strings.Contains(rec.Body.String(), secret) {
		t.Fatalf("panic 细节泄漏到响应: %q", rec.Body.String())
	}
}

// TestHandlerErrorHidesInternals 验证处理器返回的普通 error 变成 500，且细节不外泄。
func TestHandlerErrorHidesInternals(t *testing.T) {
	t.Parallel()

	const secret = "pq: relation users does not exist"

	root, planes := NewRouter(&Options{})
	huma.Register(planes.Public(), huma.Operation{OperationID: "fail", Method: http.MethodGet, Path: "/fail"},
		func(context.Context, *struct{}) (*pingOutput, error) {
			return nil, errors.New(secret)
		})

	rec := do(t, root, http.MethodGet, PrefixPublic+"/fail", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("状态码 = %d，期望 500", rec.Code)
	}
	body := decodeProblem(t, rec)
	if strings.Contains(rec.Body.String(), "relation") {
		t.Fatalf("内部错误细节泄漏到响应: %q", rec.Body.String())
	}
	if body["instance"] != PrefixPublic+"/fail" {
		t.Errorf("instance = %v，期望请求路径", body["instance"])
	}
}

// thingInput 是校验测试用的请求体。
type thingInput struct {
	Body struct {
		Name string `json:"name" minLength:"1"`
	}
}

// TestValidationFailureIsProblem 验证请求校验失败返回 422 problem+json，
// 且未知字段被拒绝而非静默忽略。
func TestValidationFailureIsProblem(t *testing.T) {
	t.Parallel()

	root, planes := NewRouter(&Options{})
	huma.Register(planes.Public(), huma.Operation{OperationID: "create-thing", Method: http.MethodPost, Path: "/things"},
		func(_ context.Context, in *thingInput) (*pingOutput, error) {
			out := &pingOutput{}
			out.Body.Plane = in.Body.Name
			return out, nil
		})

	t.Run("缺少必填字段", func(t *testing.T) {
		t.Parallel()
		rec := do(t, root, http.MethodPost, PrefixPublic+"/things", `{}`)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("状态码 = %d，期望 422：%s", rec.Code, rec.Body.String())
		}
		body := decodeProblem(t, rec)
		if details, ok := body["errors"].([]any); !ok || len(details) == 0 {
			t.Errorf("errors 明细缺失: %v", body["errors"])
		}
	})

	t.Run("未知字段被拒绝", func(t *testing.T) {
		t.Parallel()
		rec := do(t, root, http.MethodPost, PrefixPublic+"/things", `{"name":"x","extra":1}`)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("状态码 = %d，期望 422：%s", rec.Code, rec.Body.String())
		}
		decodeProblem(t, rec)
	})

	t.Run("合法请求放行", func(t *testing.T) {
		t.Parallel()
		rec := do(t, root, http.MethodPost, PrefixPublic+"/things", `{"name":"x"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("状态码 = %d，期望 200：%s", rec.Code, rec.Body.String())
		}
	})
}

// TestOpenAPIDocument 验证规范由代码生成、路径含平面前缀、安全方案已声明（agent.md §6）。
func TestOpenAPIDocument(t *testing.T) {
	t.Parallel()

	root, planes := NewRouter(&Options{Version: "1.2.3"})
	registerPing(planes.Public(), "public-ping", "public")
	registerPing(planes.Console(), "console-ping", "console")

	rec := do(t, root, http.MethodGet, api.OpenAPIPath+".json", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200", rec.Code)
	}

	var doc struct {
		OpenAPI string `json:"openapi"`
		Info    struct {
			Version string `json:"version"`
		} `json:"info"`
		Paths      map[string]map[string]map[string]any `json:"paths"`
		Components struct {
			SecuritySchemes map[string]any `json:"securitySchemes"`
		} `json:"components"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("规范不是合法 JSON: %v", err)
	}
	if !strings.HasPrefix(doc.OpenAPI, "3.1") {
		t.Errorf("openapi 版本 = %q，期望 3.1.x", doc.OpenAPI)
	}
	if doc.Info.Version != "1.2.3" {
		t.Errorf("info.version = %q", doc.Info.Version)
	}
	if _, ok := doc.Paths[PrefixPublic+"/ping"]; !ok {
		t.Errorf("规范缺少带前缀的 Public 路径，实际路径：%v", keys(doc.Paths))
	}
	consoleOp, ok := doc.Paths[PrefixConsole+"/ping"]["get"]
	if !ok {
		t.Fatalf("规范缺少带前缀的 Console 路径，实际路径：%v", keys(doc.Paths))
	}
	if _, ok := consoleOp["security"]; !ok {
		t.Error("Console 操作应标注安全方案")
	}
	for _, scheme := range []string{api.SecuritySession, api.SecurityBearer} {
		if _, ok := doc.Components.SecuritySchemes[scheme]; !ok {
			t.Errorf("缺少安全方案 %q", scheme)
		}
	}

	// 3.0 降级版本也应可用，供尚不支持 3.1 的工具链使用。
	if rec := do(t, root, http.MethodGet, api.OpenAPIPath+"-3.0.json", ""); rec.Code != http.StatusOK {
		t.Errorf("3.0 降级规范状态码 = %d", rec.Code)
	}
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestDocsPageServed 验证交互式文档页面可访问，且指向本项目的规范地址。
func TestDocsPageServed(t *testing.T) {
	t.Parallel()

	root, _ := NewRouter(&Options{})
	rec := do(t, root, http.MethodGet, api.DocsPath, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q，期望 text/html", ct)
	}
	if !strings.Contains(rec.Body.String(), api.OpenAPIPath) {
		t.Errorf("文档页面应引用 %s", api.OpenAPIPath)
	}
}
