package taxonomy_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/FeiBaiKin/lumo/internal/auth/perm"
	"github.com/FeiBaiKin/lumo/internal/httpx"
	"github.com/FeiBaiKin/lumo/internal/migrate"
	"github.com/FeiBaiKin/lumo/internal/server"
	"github.com/FeiBaiKin/lumo/internal/taxonomy"
	"github.com/FeiBaiKin/lumo/internal/testsupport"
)

// 集成测试直连本机 PG 的独立测试库（agent.md §12），本包使用独占 schema。
const testSchema = "lumo_it_taxonomy"

const (
	consolePrefix = server.PrefixConsole
	publicPrefix  = server.PrefixPublic
)

// newStack 装配含 taxonomy 模块的整机：核心迁移 + 模块迁移 + 真实路由与鉴权。
func newStack(t *testing.T) *testsupport.Stack {
	t.Helper()
	module := taxonomy.New()
	db := testsupport.Open(t, testsupport.Options{
		Schema:  testSchema,
		Migrate: true,
		Sources: []migrate.Source{{Name: module.Name(), FS: module.Migrations()}},
	})
	return testsupport.NewStack(t, db, module)
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是合法 JSON: %v\n%s", err, rec.Body.String())
	}
	return body
}

func mustStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("状态码 = %d，期望 %d：%s", rec.Code, want, rec.Body.String())
	}
}

func idOf(t *testing.T, body map[string]any) int64 {
	t.Helper()
	v, _ := body["id"].(float64)
	if v == 0 {
		t.Fatalf("响应缺少 id: %v", body)
	}
	return int64(v)
}

func itoa(id int64) string { return strconv.FormatInt(id, 10) }

func TestCategoriesEndToEnd(t *testing.T) {
	s := newStack(t)
	editor := s.Bearer(t, "editor", perm.RoleEditor)
	author := s.Bearer(t, "author", perm.RoleAuthor)

	create := func(body string) map[string]any {
		t.Helper()
		rec := s.Do(t, &testsupport.Request{Method: http.MethodPost, Path: consolePrefix + "/categories", Body: body, Auth: editor})
		mustStatus(t, rec, http.StatusCreated)
		return decode(t, rec)
	}

	root := create(`{"name":"技术"}`)
	if root["slug"] != "技术" || root["parentId"] != nil || root["position"] != float64(0) {
		t.Fatalf("根分类 = %v", root)
	}
	rootID := idOf(t, root)

	goCat := create(`{"name":"Go","parentId":` + itoa(rootID) + `}`)
	if goCat["slug"] != "go" || goCat["position"] != float64(0) {
		t.Fatalf("子分类 Go = %v", goCat)
	}
	goID := idOf(t, goCat)

	rust := create(`{"name":"Rust","parentId":` + itoa(rootID) + `,"slug":"Rust Lang"}`)
	if rust["slug"] != "rust-lang" || rust["position"] != float64(1) {
		t.Fatalf("子分类 Rust = %v", rust)
	}
	rustID := idOf(t, rust)

	t.Run("分类树按层级与排序组织", func(t *testing.T) {
		rec := s.Do(t, &testsupport.Request{Method: http.MethodGet, Path: consolePrefix + "/categories/tree", Auth: editor})
		mustStatus(t, rec, http.StatusOK)
		var body struct {
			Items []struct {
				Name     string `json:"name"`
				Children []struct {
					Name     string `json:"name"`
					Children []any  `json:"children"`
				} `json:"children"`
			} `json:"items"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("解析树失败: %v", err)
		}
		if len(body.Items) != 1 || body.Items[0].Name != "技术" {
			t.Fatalf("根节点 = %+v", body.Items)
		}
		children := body.Items[0].Children
		if len(children) != 2 || children[0].Name != "Go" || children[1].Name != "Rust" {
			t.Fatalf("子节点 = %+v", children)
		}
		if children[0].Children == nil {
			t.Error("叶子节点的 children 应为 [] 而非 null")
		}
	})

	t.Run("平铺分页", func(t *testing.T) {
		rec := s.Do(t, &testsupport.Request{Method: http.MethodGet, Path: consolePrefix + "/categories?page=1&size=2", Auth: editor})
		mustStatus(t, rec, http.StatusOK)
		body := decode(t, rec)
		if body["total"] != float64(3) || body["page"] != float64(1) || body["size"] != float64(2) {
			t.Errorf("分页元数据 = %v", body)
		}
		if items, _ := body["items"].([]any); len(items) != 2 {
			t.Errorf("items 数量 = %d，期望 2", len(items))
		}
	})

	t.Run("按 ID 获取", func(t *testing.T) {
		rec := s.Do(t, &testsupport.Request{Method: http.MethodGet, Path: consolePrefix + "/categories/" + itoa(goID), Auth: editor})
		mustStatus(t, rec, http.StatusOK)
		if decode(t, rec)["name"] != "Go" {
			t.Errorf("响应 = %s", rec.Body.String())
		}
		missing := s.Do(t, &testsupport.Request{Method: http.MethodGet, Path: consolePrefix + "/categories/999999", Auth: editor})
		mustStatus(t, missing, http.StatusNotFound)
	})

	t.Run("更新可改名并移动，slug 留空则保留", func(t *testing.T) {
		rec := s.Do(t, &testsupport.Request{Method: http.MethodPut, Path: consolePrefix + "/categories/" + itoa(rustID),
			Body: `{"name":"Rust 语言","parentId":` + itoa(goID) + `}`, Auth: editor})
		mustStatus(t, rec, http.StatusOK)
		body := decode(t, rec)
		if body["name"] != "Rust 语言" || body["slug"] != "rust-lang" || body["parentId"] != float64(goID) {
			t.Errorf("更新结果 = %v", body)
		}
		if body["position"] != float64(1) {
			t.Errorf("position 留空应保留原值，实际 %v", body["position"])
		}
	})

	t.Run("成环与非法父分类被拒绝", func(t *testing.T) {
		cases := map[string]string{
			"挂到后代":   `{"name":"技术","parentId":` + itoa(goID) + `}`,
			"挂到自身":   `{"name":"技术","parentId":` + itoa(rootID) + `}`,
			"父分类不存在": `{"name":"技术","parentId":999999}`,
		}
		for name, body := range cases {
			rec := s.Do(t, &testsupport.Request{Method: http.MethodPut, Path: consolePrefix + "/categories/" + itoa(rootID), Body: body, Auth: editor})
			if rec.Code != http.StatusBadRequest {
				t.Errorf("%s：状态码 = %d，期望 400：%s", name, rec.Code, rec.Body.String())
			}
		}
	})

	t.Run("删除分类时子分类挂到祖父分类", func(t *testing.T) {
		rec := s.Do(t, &testsupport.Request{Method: http.MethodDelete, Path: consolePrefix + "/categories/" + itoa(goID), Auth: editor})
		mustStatus(t, rec, http.StatusNoContent)

		after := s.Do(t, &testsupport.Request{Method: http.MethodGet, Path: consolePrefix + "/categories/" + itoa(rustID), Auth: editor})
		mustStatus(t, after, http.StatusOK)
		if decode(t, after)["parentId"] != float64(rootID) {
			t.Errorf("子分类应挂到祖父分类: %s", after.Body.String())
		}
		again := s.Do(t, &testsupport.Request{Method: http.MethodDelete, Path: consolePrefix + "/categories/" + itoa(goID), Auth: editor})
		mustStatus(t, again, http.StatusNotFound)
	})

	t.Run("author 可读不可写", func(t *testing.T) {
		rec := s.Do(t, &testsupport.Request{Method: http.MethodPost, Path: consolePrefix + "/categories", Body: `{"name":"越权"}`, Auth: author})
		mustStatus(t, rec, http.StatusForbidden)
		body := decode(t, rec)
		required, _ := body["requiredPermissions"].([]any)
		if len(required) != 1 || required[0] != perm.TaxonomiesManage.String() {
			t.Errorf("requiredPermissions = %v", body["requiredPermissions"])
		}
		if ct := rec.Header().Get("Content-Type"); ct != httpx.ContentTypeProblem {
			t.Errorf("Content-Type = %q", ct)
		}

		read := s.Do(t, &testsupport.Request{Method: http.MethodGet, Path: consolePrefix + "/categories", Auth: author})
		mustStatus(t, read, http.StatusOK)
	})

	t.Run("匿名不能访问 Console 平面", func(t *testing.T) {
		rec := s.Do(t, &testsupport.Request{Method: http.MethodGet, Path: consolePrefix + "/categories"})
		mustStatus(t, rec, http.StatusUnauthorized)
	})

	t.Run("Public 平面匿名可读", func(t *testing.T) {
		list := s.Do(t, &testsupport.Request{Method: http.MethodGet, Path: publicPrefix + "/categories"})
		mustStatus(t, list, http.StatusOK)
		if items, _ := decode(t, list)["items"].([]any); len(items) != 2 {
			t.Errorf("公开列表数量 = %d，期望 2", len(items))
		}

		tree := s.Do(t, &testsupport.Request{Method: http.MethodGet, Path: publicPrefix + "/categories/tree"})
		mustStatus(t, tree, http.StatusOK)

		bySlug := s.Do(t, &testsupport.Request{Method: http.MethodGet, Path: publicPrefix + "/categories/" + url.PathEscape("技术")})
		mustStatus(t, bySlug, http.StatusOK)
		if decode(t, bySlug)["name"] != "技术" {
			t.Errorf("按中文 slug 获取失败: %s", bySlug.Body.String())
		}

		missing := s.Do(t, &testsupport.Request{Method: http.MethodGet, Path: publicPrefix + "/categories/nope"})
		mustStatus(t, missing, http.StatusNotFound)
	})
}

func TestCategoryConflictsAndValidation(t *testing.T) {
	s := newStack(t)
	editor := s.Bearer(t, "editor", perm.RoleEditor)

	post := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		return s.Do(t, &testsupport.Request{Method: http.MethodPost, Path: consolePrefix + "/categories", Body: body, Auth: editor})
	}

	tech := post(`{"name":"技术"}`)
	mustStatus(t, tech, http.StatusCreated)
	techID := idOf(t, decode(t, tech))

	tests := []struct {
		name string
		body string
		want int
	}{
		{"slug 重复", `{"name":"Tech","slug":"技术"}`, http.StatusConflict},
		{"同级名称重复", `{"name":"技术"}`, http.StatusConflict},
		{"同级名称重复不区分大小写", `{"name":"TECH","slug":"tech-upper"}`, http.StatusCreated},
		{"纯标点 slug", `{"name":"X","slug":"///"}`, http.StatusBadRequest},
		{"空白名称", `{"name":"   "}`, http.StatusBadRequest},
		{"保留路径 tree", `{"name":"T","slug":"tree"}`, http.StatusBadRequest},
		{"父分类不存在", `{"name":"Y","parentId":999999}`, http.StatusBadRequest},
		{"未知字段", `{"name":"Z","bogus":1}`, http.StatusUnprocessableEntity},
		{"名称过长", `{"name":"` + strings.Repeat("长", 65) + `"}`, http.StatusUnprocessableEntity},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := post(tt.body)
			if rec.Code != tt.want {
				t.Errorf("状态码 = %d，期望 %d：%s", rec.Code, tt.want, rec.Body.String())
			}
		})
	}

	t.Run("不同父分类下可同名，自动生成的 slug 冲突时追加序号", func(t *testing.T) {
		rec := post(`{"name":"技术","parentId":` + itoa(techID) + `}`)
		mustStatus(t, rec, http.StatusCreated)
		if got := decode(t, rec)["slug"]; got != "技术-2" {
			t.Errorf("slug = %v，期望 技术-2", got)
		}
		again := post(`{"name":"技术","parentId":` + itoa(techID) + `,"slug":"tech-3"}`)
		mustStatus(t, again, http.StatusConflict) // 同级同名仍受唯一约束
	})

	// 再创建一次同名 tech 应触发大小写不敏感的同级唯一约束。
	if rec := post(`{"name":"tech","slug":"tech-lower"}`); rec.Code != http.StatusConflict {
		t.Errorf("同级名称大小写不敏感唯一：状态码 = %d，期望 409：%s", rec.Code, rec.Body.String())
	}
}

func TestTagsEndToEnd(t *testing.T) {
	s := newStack(t)
	editor := s.Bearer(t, "editor", perm.RoleEditor)

	post := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		return s.Do(t, &testsupport.Request{Method: http.MethodPost, Path: consolePrefix + "/tags", Body: body, Auth: editor})
	}

	goTag := post(`{"name":"Go","color":"#00ADD8"}`)
	mustStatus(t, goTag, http.StatusCreated)
	goBody := decode(t, goTag)
	if goBody["slug"] != "go" || goBody["color"] != "#00add8" {
		t.Fatalf("标签 Go = %v", goBody)
	}
	goID := idOf(t, goBody)

	t.Run("非法颜色与重名被拒绝", func(t *testing.T) {
		mustStatus(t, post(`{"name":"Bad","color":"red"}`), http.StatusUnprocessableEntity)
		mustStatus(t, post(`{"name":"go"}`), http.StatusConflict)
		mustStatus(t, post(`{"name":"Golang","slug":"go"}`), http.StatusConflict)
	})

	t.Run("名称不同但自动 slug 相同时追加序号", func(t *testing.T) {
		cpp := post(`{"name":"C++"}`)
		mustStatus(t, cpp, http.StatusCreated)
		csharp := post(`{"name":"C#"}`)
		mustStatus(t, csharp, http.StatusCreated)
		if decode(t, cpp)["slug"] != "c" || decode(t, csharp)["slug"] != "c-2" {
			t.Errorf("slug = %v / %v，期望 c / c-2", decode(t, cpp)["slug"], decode(t, csharp)["slug"])
		}
		for _, id := range []int64{idOf(t, decode(t, cpp)), idOf(t, decode(t, csharp))} {
			mustStatus(t, s.Do(t, &testsupport.Request{Method: http.MethodDelete, Path: consolePrefix + "/tags/" + itoa(id), Auth: editor}), http.StatusNoContent)
		}
	})

	mustStatus(t, post(`{"name":"Rust"}`), http.StatusCreated)
	python := post(`{"name":"Python"}`)
	mustStatus(t, python, http.StatusCreated)
	pythonID := idOf(t, decode(t, python))

	t.Run("筛选与分页", func(t *testing.T) {
		search := s.Do(t, &testsupport.Request{Method: http.MethodGet, Path: consolePrefix + "/tags?q=go", Auth: editor})
		mustStatus(t, search, http.StatusOK)
		if decode(t, search)["total"] != float64(1) {
			t.Errorf("筛选结果 = %s", search.Body.String())
		}

		page := s.Do(t, &testsupport.Request{Method: http.MethodGet, Path: consolePrefix + "/tags?page=1&size=2", Auth: editor})
		mustStatus(t, page, http.StatusOK)
		body := decode(t, page)
		if body["total"] != float64(3) || body["size"] != float64(2) {
			t.Errorf("分页元数据 = %v", body)
		}
		if items, _ := body["items"].([]any); len(items) != 2 {
			t.Errorf("items 数量 = %d，期望 2", len(items))
		}

		bad := s.Do(t, &testsupport.Request{Method: http.MethodGet, Path: consolePrefix + "/tags?size=1000", Auth: editor})
		mustStatus(t, bad, http.StatusUnprocessableEntity)
	})

	t.Run("更新保留 slug", func(t *testing.T) {
		rec := s.Do(t, &testsupport.Request{Method: http.MethodPut, Path: consolePrefix + "/tags/" + itoa(goID),
			Body: `{"name":"Golang","description":"Go 语言"}`, Auth: editor})
		mustStatus(t, rec, http.StatusOK)
		body := decode(t, rec)
		if body["name"] != "Golang" || body["slug"] != "go" || body["description"] != "Go 语言" || body["color"] != "" {
			t.Errorf("更新结果 = %v", body)
		}
	})

	t.Run("删除", func(t *testing.T) {
		mustStatus(t, s.Do(t, &testsupport.Request{Method: http.MethodDelete, Path: consolePrefix + "/tags/" + itoa(pythonID), Auth: editor}), http.StatusNoContent)
		mustStatus(t, s.Do(t, &testsupport.Request{Method: http.MethodGet, Path: consolePrefix + "/tags/" + itoa(pythonID), Auth: editor}), http.StatusNotFound)
		mustStatus(t, s.Do(t, &testsupport.Request{Method: http.MethodDelete, Path: consolePrefix + "/tags/" + itoa(pythonID), Auth: editor}), http.StatusNotFound)
	})

	t.Run("Public 平面匿名可读", func(t *testing.T) {
		list := s.Do(t, &testsupport.Request{Method: http.MethodGet, Path: publicPrefix + "/tags"})
		mustStatus(t, list, http.StatusOK)
		if decode(t, list)["total"] != float64(2) {
			t.Errorf("公开标签数 = %s", list.Body.String())
		}
		bySlug := s.Do(t, &testsupport.Request{Method: http.MethodGet, Path: publicPrefix + "/tags/go"})
		mustStatus(t, bySlug, http.StatusOK)
		if decode(t, bySlug)["name"] != "Golang" {
			t.Errorf("按 slug 获取 = %s", bySlug.Body.String())
		}
		mustStatus(t, s.Do(t, &testsupport.Request{Method: http.MethodGet, Path: publicPrefix + "/tags/nope"}), http.StatusNotFound)
	})

	t.Run("匿名不能写", func(t *testing.T) {
		mustStatus(t, s.Do(t, &testsupport.Request{Method: http.MethodPost, Path: consolePrefix + "/tags", Body: `{"name":"匿名"}`}), http.StatusUnauthorized)
	})
}
