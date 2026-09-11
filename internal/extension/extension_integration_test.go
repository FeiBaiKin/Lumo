package extension_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/FeiBaiKin/lumo/internal/auth/perm"
	"github.com/FeiBaiKin/lumo/internal/extension"
	"github.com/FeiBaiKin/lumo/internal/migrate"
	"github.com/FeiBaiKin/lumo/internal/server"
	"github.com/FeiBaiKin/lumo/internal/testsupport"
)

// 集成测试直连本机 PG 的独立测试库（agent.md §12），本包使用独占 schema。
const testSchema = "lumo_it_extension"

// base 是本测试用的资源地址；kind 为 Note，复数段为 notes。
const base = server.PrefixExtension + "/" + extension.GroupLumo + "/" + extension.VersionAlpha + "/notes"

func newStack(t *testing.T) *testsupport.Stack {
	t.Helper()
	ext := extension.New()
	db := testsupport.Open(t, testsupport.Options{
		Schema:  testSchema,
		Migrate: true,
		Sources: []migrate.Source{{Name: ext.Name(), FS: ext.Migrations()}},
	})
	return testsupport.NewStack(t, db, ext)
}

func mustStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) map[string]any {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("状态码 = %d，期望 %d：%s", rec.Code, want, rec.Body.String())
	}
	if rec.Body.Len() == 0 {
		return nil
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是合法 JSON: %v\n%s", err, rec.Body.String())
	}
	return body
}

func req(t *testing.T, s *testsupport.Stack, method, path, body, authz string) *httptest.ResponseRecorder {
	t.Helper()
	return s.Do(t, &testsupport.Request{Method: method, Path: path, Body: body, Auth: authz})
}

func str(t *testing.T, body map[string]any, key string) string {
	t.Helper()
	v, ok := body[key].(string)
	if !ok {
		t.Fatalf("响应缺少字符串字段 %s：%v", key, body)
	}
	return v
}

// names 取出列表响应里各条目的 name，用于断言顺序与筛选结果。
func names(t *testing.T, body map[string]any) []string {
	t.Helper()
	items, ok := body["items"].([]any)
	if !ok {
		t.Fatalf("响应缺少 items：%v", body)
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("条目不是对象：%v", item)
		}
		out = append(out, str(t, m, "name"))
	}
	return out
}

func total(t *testing.T, body map[string]any) int {
	t.Helper()
	v, ok := body["total"].(float64)
	if !ok {
		t.Fatalf("响应缺少 total：%v", body)
	}
	return int(v)
}

// TestExtensionCRUD 跑通一条记录的增删改查。
func TestExtensionCRUD(t *testing.T) {
	s := newStack(t)
	admin := s.Bearer(t, "admin", perm.RoleAdmin)

	created := mustStatus(t, req(t, s, http.MethodPost, base,
		`{"kind":"Note","name":"hello","spec":{"title":"你好","pinned":true}}`, admin), http.StatusCreated)
	if got := str(t, created, "resource"); got != "notes" {
		t.Fatalf("resource = %q，期望 notes", got)
	}
	if got := str(t, created, "selfLink"); got != base+"/hello" {
		t.Fatalf("selfLink = %q，期望 %q", got, base+"/hello")
	}

	got := mustStatus(t, req(t, s, http.MethodGet, base+"/hello", "", admin), http.StatusOK)
	spec, ok := got["spec"].(map[string]any)
	if !ok || spec["title"] != "你好" || spec["pinned"] != true {
		t.Fatalf("spec 未原样存回：%v", got["spec"])
	}

	// PUT 整体替换 spec：原有的 pinned 应当消失。
	updated := mustStatus(t, req(t, s, http.MethodPut, base+"/hello",
		`{"kind":"Note","name":"hello","spec":{"title":"改过了"}}`, admin), http.StatusOK)
	spec, ok = updated["spec"].(map[string]any)
	if !ok || spec["title"] != "改过了" {
		t.Fatalf("spec 未被替换：%v", updated["spec"])
	}
	if _, exists := spec["pinned"]; exists {
		t.Fatalf("PUT 应整体替换 spec，pinned 仍在：%v", spec)
	}

	listed := mustStatus(t, req(t, s, http.MethodGet, base, "", admin), http.StatusOK)
	if got := names(t, listed); len(got) != 1 || got[0] != "hello" {
		t.Fatalf("列表 = %v，期望只有 hello", got)
	}

	mustStatus(t, req(t, s, http.MethodDelete, base+"/hello", "", admin), http.StatusNoContent)
	mustStatus(t, req(t, s, http.MethodGet, base+"/hello", "", admin), http.StatusNotFound)
	mustStatus(t, req(t, s, http.MethodDelete, base+"/hello", "", admin), http.StatusNotFound)
}

// TestExtensionAddressing 覆盖命名约定：地址、kind 与请求体三者必须自洽。
func TestExtensionAddressing(t *testing.T) {
	s := newStack(t)
	admin := s.Bearer(t, "admin", perm.RoleAdmin)

	t.Run("kind 与资源段不一致", func(t *testing.T) {
		// Note 对应 notes，写到 posts 下的记录将取不回来，必须当场拒绝。
		mustStatus(t, req(t, s, http.MethodPost,
			server.PrefixExtension+"/"+extension.GroupLumo+"/"+extension.VersionAlpha+"/posts",
			`{"kind":"Note","name":"x","spec":{}}`, admin), http.StatusUnprocessableEntity)
	})

	t.Run("分组不是反向域名", func(t *testing.T) {
		mustStatus(t, req(t, s, http.MethodGet,
			server.PrefixExtension+"/lumo/v1alpha1/notes", "", admin), http.StatusUnprocessableEntity)
	})

	t.Run("版本形态不合法", func(t *testing.T) {
		mustStatus(t, req(t, s, http.MethodGet,
			server.PrefixExtension+"/"+extension.GroupLumo+"/alpha/notes", "", admin),
			http.StatusUnprocessableEntity)
	})

	t.Run("名称不是 DNS-1123", func(t *testing.T) {
		mustStatus(t, req(t, s, http.MethodPost, base,
			`{"kind":"Note","name":"Hello","spec":{}}`, admin), http.StatusUnprocessableEntity)
	})

	t.Run("请求体的 name 与地址不符", func(t *testing.T) {
		mustStatus(t, req(t, s, http.MethodPost, base,
			`{"kind":"Note","name":"conflict-check","spec":{}}`, admin), http.StatusCreated)
		mustStatus(t, req(t, s, http.MethodPut, base+"/conflict-check",
			`{"name":"other","spec":{}}`, admin), http.StatusUnprocessableEntity)
	})

	t.Run("同名记录", func(t *testing.T) {
		mustStatus(t, req(t, s, http.MethodPost, base,
			`{"kind":"Note","name":"dup","spec":{}}`, admin), http.StatusCreated)
		mustStatus(t, req(t, s, http.MethodPost, base,
			`{"kind":"Note","name":"dup","spec":{}}`, admin), http.StatusConflict)
	})

	t.Run("同一资源段上换一种 kind 拼法", func(t *testing.T) {
		// Replicaset 与 ReplicaSet 都映射到 replicasets，先写入的那个是正规拼法。
		sets := server.PrefixExtension + "/" + extension.GroupLumo + "/" + extension.VersionAlpha + "/replicasets"
		mustStatus(t, req(t, s, http.MethodPost, sets,
			`{"kind":"ReplicaSet","name":"a","spec":{}}`, admin), http.StatusCreated)
		mustStatus(t, req(t, s, http.MethodPost, sets,
			`{"kind":"Replicaset","name":"b","spec":{}}`, admin), http.StatusConflict)
	})

	t.Run("更新不存在的记录不会顺手创建", func(t *testing.T) {
		mustStatus(t, req(t, s, http.MethodPut, base+"/missing", `{"spec":{}}`, admin), http.StatusNotFound)
	})
}

// TestExtensionList 覆盖分页、排序与 spec 筛选。
func TestExtensionList(t *testing.T) {
	s := newStack(t)
	admin := s.Bearer(t, "admin", perm.RoleAdmin)

	seed := []string{
		`{"kind":"Note","name":"alpha","spec":{"status":"draft","weight":1}}`,
		`{"kind":"Note","name":"beta","spec":{"status":"published","weight":2}}`,
		`{"kind":"Note","name":"gamma","spec":{"status":"draft","weight":3}}`,
	}
	for _, body := range seed {
		mustStatus(t, req(t, s, http.MethodPost, base, body, admin), http.StatusCreated)
	}
	// 另一个资源段下的记录不应出现在 notes 的列表里。
	mustStatus(t, req(t, s, http.MethodPost,
		server.PrefixExtension+"/"+extension.GroupLumo+"/"+extension.VersionAlpha+"/tasks",
		`{"kind":"Task","name":"alpha","spec":{"status":"draft"}}`, admin), http.StatusCreated)

	t.Run("默认按名称升序", func(t *testing.T) {
		body := mustStatus(t, req(t, s, http.MethodGet, base, "", admin), http.StatusOK)
		if got := names(t, body); len(got) != 3 || got[0] != "alpha" || got[2] != "gamma" {
			t.Fatalf("列表 = %v", got)
		}
		if got := total(t, body); got != 3 {
			t.Fatalf("total = %d，期望 3（另一个资源段的记录不该计入）", got)
		}
	})

	t.Run("倒序", func(t *testing.T) {
		body := mustStatus(t, req(t, s, http.MethodGet, base+"?sort=-name", "", admin), http.StatusOK)
		if got := names(t, body); len(got) != 3 || got[0] != "gamma" {
			t.Fatalf("列表 = %v", got)
		}
	})

	t.Run("分页", func(t *testing.T) {
		body := mustStatus(t, req(t, s, http.MethodGet, base+"?page=2&size=2", "", admin), http.StatusOK)
		if got := names(t, body); len(got) != 1 || got[0] != "gamma" {
			t.Fatalf("第二页 = %v", got)
		}
		if got := total(t, body); got != 3 {
			t.Fatalf("total = %d，期望 3", got)
		}
	})

	t.Run("按 spec 字段筛选", func(t *testing.T) {
		body := mustStatus(t, req(t, s, http.MethodGet, base+"?where=status=draft", "", admin), http.StatusOK)
		if got := names(t, body); len(got) != 2 || got[0] != "alpha" || got[1] != "gamma" {
			t.Fatalf("筛选结果 = %v", got)
		}
	})

	t.Run("数字按数字比较", func(t *testing.T) {
		body := mustStatus(t, req(t, s, http.MethodGet, base+"?where=weight=2", "", admin), http.StatusOK)
		if got := names(t, body); len(got) != 1 || got[0] != "beta" {
			t.Fatalf("筛选结果 = %v", got)
		}
	})

	t.Run("多条筛选取交集", func(t *testing.T) {
		body := mustStatus(t, req(t, s, http.MethodGet,
			base+"?where=status=draft&where=weight=3", "", admin), http.StatusOK)
		if got := names(t, body); len(got) != 1 || got[0] != "gamma" {
			t.Fatalf("筛选结果 = %v", got)
		}
	})

	t.Run("筛选条件形态不合法", func(t *testing.T) {
		mustStatus(t, req(t, s, http.MethodGet, base+"?where=status", "", admin),
			http.StatusUnprocessableEntity)
	})

	t.Run("未知资源段返回空列表", func(t *testing.T) {
		body := mustStatus(t, req(t, s, http.MethodGet,
			server.PrefixExtension+"/"+extension.GroupLumo+"/"+extension.VersionAlpha+"/widgets",
			"", admin), http.StatusOK)
		if got := total(t, body); got != 0 {
			t.Fatalf("total = %d，期望 0", got)
		}
	})
}

// TestExtensionPermissions 固定 extensions:manage 的门禁。
func TestExtensionPermissions(t *testing.T) {
	s := newStack(t)
	editor := s.Bearer(t, "editor", perm.RoleEditor)

	// editor 管内容，但扩展记录是站点级数据，读写都不归它。
	mustStatus(t, req(t, s, http.MethodGet, base, "", editor), http.StatusForbidden)
	mustStatus(t, req(t, s, http.MethodPost, base, `{"kind":"Note","name":"x","spec":{}}`, editor),
		http.StatusForbidden)
	mustStatus(t, req(t, s, http.MethodGet, base, "", ""), http.StatusUnauthorized)
	mustStatus(t, req(t, s, http.MethodDelete, base+"/x", "", ""), http.StatusUnauthorized)
}

// TestExtensionPermissionDeclared 确认权限清单里有 extensions:manage，
// 供 Console 的角色编辑页展示。
func TestExtensionPermissionDeclared(t *testing.T) {
	s := newStack(t)
	admin := s.Bearer(t, "admin", perm.RoleAdmin)

	rec := req(t, s, http.MethodGet, server.PrefixConsole+"/permissions", "", admin)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d：%s", rec.Code, rec.Body.String())
	}
	if want := strconv.Quote(perm.ExtensionsManage.String()); !strings.Contains(rec.Body.String(), want) {
		t.Fatalf("权限清单里没有 %s：%s", perm.ExtensionsManage, rec.Body.String())
	}
}
