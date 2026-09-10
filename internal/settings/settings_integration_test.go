package settings_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/FeiBaiKin/lumo/internal/auth/perm"
	"github.com/FeiBaiKin/lumo/internal/content"
	"github.com/FeiBaiKin/lumo/internal/migrate"
	"github.com/FeiBaiKin/lumo/internal/server"
	"github.com/FeiBaiKin/lumo/internal/settings"
	"github.com/FeiBaiKin/lumo/internal/taxonomy"
	"github.com/FeiBaiKin/lumo/internal/testsupport"
)

// 集成测试直连本机 PG 的独立测试库（agent.md §12），本包使用独占 schema。
const testSchema = "lumo_it_settings"

const (
	consolePrefix = server.PrefixConsole
	publicPrefix  = server.PrefixPublic
)

// newStack 按生产装配顺序装配 settings + taxonomy + content。
func newStack(t *testing.T) *testsupport.Stack {
	t.Helper()
	set, tax, con := settings.New(), taxonomy.New(), content.New()
	db := testsupport.Open(t, testsupport.Options{
		Schema:  testSchema,
		Migrate: true,
		Sources: []migrate.Source{
			{Name: set.Name(), FS: set.Migrations()},
			{Name: tax.Name(), FS: tax.Migrations()},
			{Name: con.Name(), FS: con.Migrations()},
		},
	})
	return testsupport.NewStack(t, db, set, tax, con)
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是合法 JSON: %v\n%s", err, rec.Body.String())
	}
	return body
}

func mustStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) map[string]any {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("状态码 = %d，期望 %d：%s", rec.Code, want, rec.Body.String())
	}
	return decode(t, rec)
}

func req(t *testing.T, s *testsupport.Stack, method, path, body, auth string) *httptest.ResponseRecorder {
	t.Helper()
	return s.Do(t, &testsupport.Request{Method: method, Path: path, Body: body, Auth: auth})
}

func TestSettingsEndToEnd(t *testing.T) {
	s := newStack(t)
	admin := s.Bearer(t, "admin", perm.RoleAdmin)
	editor := s.Bearer(t, "editor", perm.RoleEditor)

	t.Run("只有 settings:manage 能读写", func(t *testing.T) {
		mustStatus(t, req(t, s, http.MethodGet, consolePrefix+"/settings", "", editor), http.StatusForbidden)
		mustStatus(t, req(t, s, http.MethodPut, consolePrefix+"/settings/site", `{"title":"x"}`, editor), http.StatusForbidden)
		mustStatus(t, req(t, s, http.MethodGet, consolePrefix+"/settings", "", ""), http.StatusUnauthorized)
	})

	t.Run("列出分组携带 Schema、缺省值与有效值", func(t *testing.T) {
		body := mustStatus(t, req(t, s, http.MethodGet, consolePrefix+"/settings", "", admin), http.StatusOK)
		items, _ := body["items"].([]any)
		if len(items) != 1 {
			t.Fatalf("分组数 = %d，期望 1（site）", len(items))
		}
		site, _ := items[0].(map[string]any)
		schema, _ := site["schema"].(map[string]any)
		props, _ := schema["properties"].(map[string]any)
		desc, _ := props["description"].(map[string]any)
		if site["name"] != "site" || desc["x-widget"] != "textarea" {
			t.Errorf("site 分组 = %v", site)
		}
		values, _ := site["values"].(map[string]any)
		if values["title"] != "Lumo" || values["pageSize"] != float64(10) {
			t.Errorf("有效值应为缺省值: %v", values)
		}
	})

	t.Run("更新后有效值合并并对外可见", func(t *testing.T) {
		body := mustStatus(t, req(t, s, http.MethodPut, consolePrefix+"/settings/site",
			`{"title":"我的站点","url":"https://example.com","pageSize":5}`, admin), http.StatusOK)
		values, _ := body["values"].(map[string]any)
		if values["title"] != "我的站点" || values["pageSize"] != float64(5) || values["timezone"] != "Asia/Shanghai" {
			t.Errorf("合并后的有效值 = %v", values)
		}

		again := mustStatus(t, req(t, s, http.MethodGet, consolePrefix+"/settings/site", "", admin), http.StatusOK)
		if v, _ := again["values"].(map[string]any); v["title"] != "我的站点" {
			t.Errorf("重新读取应得到已保存值: %v", again["values"])
		}

		public := mustStatus(t, req(t, s, http.MethodGet, publicPrefix+"/settings", "", ""), http.StatusOK)
		site, _ := public["site"].(map[string]any)
		if site["title"] != "我的站点" || site["url"] != "https://example.com" {
			t.Errorf("公开设置 = %v", public)
		}
		for _, hidden := range []string{"pageSize", "timezone", "slugStrategy"} {
			if _, leaked := site[hidden]; leaked {
				t.Errorf("非公开字段 %s 不应出现在 Public 平面", hidden)
			}
		}
	})

	t.Run("校验失败返回 422 并逐条定位", func(t *testing.T) {
		cases := map[string]string{
			`{"pageSize":1000}`:           "body.pageSize",
			`{"timezone":"Mars/Olympus"}`: "body.timezone",
			`{"bogus":true}`:              "body",
			`{"slugStrategy":"uuid"}`:     "body.slugStrategy",
			`{"url":"not a url"}`:         "body.url",
		}
		for body, location := range cases {
			rec := req(t, s, http.MethodPut, consolePrefix+"/settings/site", body, admin)
			resp := mustStatus(t, rec, http.StatusUnprocessableEntity)
			details, _ := resp["errors"].([]any)
			found := false
			for _, d := range details {
				if m, _ := d.(map[string]any); m["location"] == location {
					found = true
				}
			}
			if !found {
				t.Errorf("%s 应定位到 %s：%s", body, location, rec.Body.String())
			}
		}
		// 校验失败不应改变已保存值。
		cur := mustStatus(t, req(t, s, http.MethodGet, consolePrefix+"/settings/site", "", admin), http.StatusOK)
		if v, _ := cur["values"].(map[string]any); v["pageSize"] != float64(5) {
			t.Errorf("校验失败后值被改动: %v", cur["values"])
		}
	})

	t.Run("未知分组 404", func(t *testing.T) {
		mustStatus(t, req(t, s, http.MethodGet, consolePrefix+"/settings/nope", "", admin), http.StatusNotFound)
		mustStatus(t, req(t, s, http.MethodPut, consolePrefix+"/settings/nope", `{}`, admin), http.StatusNotFound)
	})

	t.Run("slug 策略即时生效于分类与文章", func(t *testing.T) {
		mustStatus(t, req(t, s, http.MethodPut, consolePrefix+"/settings/site", `{"title":"我的站点","slugStrategy":"pinyin"}`, admin), http.StatusOK)
		cat := mustStatus(t, req(t, s, http.MethodPost, consolePrefix+"/categories", `{"name":"技术分享"}`, admin), http.StatusCreated)
		if cat["slug"] != "ji-shu-fen-xiang" {
			t.Errorf("拼音策略下分类 slug = %v", cat["slug"])
		}
		post := mustStatus(t, req(t, s, http.MethodPost, consolePrefix+"/posts", `{"title":"你好世界"}`, admin), http.StatusCreated)
		if post["slug"] != "ni-hao-shi-jie" {
			t.Errorf("拼音策略下文章 slug = %v", post["slug"])
		}
		// 用户显式指定的 slug 不受策略影响。
		explicit := mustStatus(t, req(t, s, http.MethodPost, consolePrefix+"/tags", `{"name":"随笔","slug":"随笔"}`, admin), http.StatusCreated)
		if explicit["slug"] != "随笔" {
			t.Errorf("显式 slug 应原样规范化保留: %v", explicit["slug"])
		}

		mustStatus(t, req(t, s, http.MethodPut, consolePrefix+"/settings/site", `{"title":"我的站点","slugStrategy":"unicode"}`, admin), http.StatusOK)
		back := mustStatus(t, req(t, s, http.MethodPost, consolePrefix+"/categories", `{"name":"生活"}`, admin), http.StatusCreated)
		if back["slug"] != "生活" {
			t.Errorf("切回 unicode 后分类 slug = %v", back["slug"])
		}
	})
}
