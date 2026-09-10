package content_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/FeiBaiKin/lumo/internal/auth/perm"
	"github.com/FeiBaiKin/lumo/internal/content"
	"github.com/FeiBaiKin/lumo/internal/migrate"
	"github.com/FeiBaiKin/lumo/internal/server"
	"github.com/FeiBaiKin/lumo/internal/taxonomy"
	"github.com/FeiBaiKin/lumo/internal/testsupport"
)

// 集成测试直连本机 PG 的独立测试库（agent.md §12），本包使用独占 schema。
const testSchema = "lumo_it_content"

const (
	consolePrefix = server.PrefixConsole
	publicPrefix  = server.PrefixPublic
)

// newStack 装配 taxonomy + content 的整机：两个模块的迁移 + 真实路由与鉴权。
func newStack(t *testing.T) (*testsupport.Stack, *content.Module) {
	t.Helper()
	tax, mod := taxonomy.New(), content.New()
	db := testsupport.Open(t, testsupport.Options{
		Schema:  testSchema,
		Migrate: true,
		Sources: []migrate.Source{
			{Name: tax.Name(), FS: tax.Migrations()},
			{Name: mod.Name(), FS: mod.Migrations()},
		},
	})
	return testsupport.NewStack(t, db, tax, mod), mod
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
	if rec.Code == http.StatusNoContent {
		return nil
	}
	return decode(t, rec)
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

func total(body map[string]any) float64 {
	v, _ := body["total"].(float64)
	return v
}

// req 是简写：s.Do(t, &Request{...})。
func req(t *testing.T, s *testsupport.Stack, method, path, body, auth string) *httptest.ResponseRecorder {
	t.Helper()
	return s.Do(t, &testsupport.Request{Method: method, Path: path, Body: body, Auth: auth})
}

func TestPostsEndToEnd(t *testing.T) {
	s, mod := newStack(t)
	editor := s.Bearer(t, "editor", perm.RoleEditor)
	author := s.Bearer(t, "author", perm.RoleAuthor)

	catID := idOf(t, mustStatus(t, req(t, s, http.MethodPost, consolePrefix+"/categories", `{"name":"技术"}`, editor), http.StatusCreated))
	tagID := idOf(t, mustStatus(t, req(t, s, http.MethodPost, consolePrefix+"/tags", `{"name":"Go"}`, editor), http.StatusCreated))

	// author 创建 Markdown 文章。
	created := mustStatus(t, req(t, s, http.MethodPost, consolePrefix+"/posts",
		`{"title":"Hello Lumo","rawType":"markdown","raw":"# 标题\n\n正文 **加粗**","categoryIds":[`+itoa(catID)+`],"tagIds":[`+itoa(tagID)+`]}`,
		author), http.StatusCreated)
	postID := idOf(t, created)
	if created["status"] != "draft" || created["slug"] != "hello-lumo" || created["type"] != "post" || created["visibility"] != "public" {
		t.Fatalf("新建文章 = %v", created)
	}
	if c, _ := created["content"].(string); !strings.Contains(c, "<strong>加粗</strong>") || !strings.Contains(c, `id="标题"`) {
		t.Errorf("正文未渲染: %v", created["content"])
	}
	if created["excerptAuto"] != true || created["excerpt"] != "标题 正文 加粗" {
		t.Errorf("自动摘要 = %v / %v", created["excerptAuto"], created["excerpt"])
	}
	if a, _ := created["author"].(map[string]any); a["username"] != "author" {
		t.Errorf("作者 = %v", created["author"])
	}
	if cats, _ := created["categories"].([]any); len(cats) != 1 {
		t.Errorf("分类 = %v", created["categories"])
	}
	if tags, _ := created["tags"].([]any); len(tags) != 1 {
		t.Errorf("标签 = %v", created["tags"])
	}
	if created["publishedAt"] != nil {
		t.Errorf("草稿不应有发布时间: %v", created["publishedAt"])
	}

	editorPostID := idOf(t, mustStatus(t, req(t, s, http.MethodPost, consolePrefix+"/posts", `{"title":"编辑的文章","raw":"<p>hi</p>"}`, editor), http.StatusCreated))

	t.Run("列表可见性与筛选", func(t *testing.T) {
		if got := total(mustStatus(t, req(t, s, http.MethodGet, consolePrefix+"/posts", "", author), http.StatusOK)); got != 1 {
			t.Errorf("author 只应看到自己的文章，实际 %v", got)
		}
		if got := total(mustStatus(t, req(t, s, http.MethodGet, consolePrefix+"/posts", "", editor), http.StatusOK)); got != 2 {
			t.Errorf("editor 应看到全部文章，实际 %v", got)
		}
		for query, want := range map[string]float64{
			"?author=author":                     1,
			"?author=nobody":                     0,
			"?q=Hello":                           1,
			"?category=" + url.QueryEscape("技术"): 1,
			"?tag=go":                            1,
			"?status=draft":                      2,
			"?status=published":                  0,
		} {
			if got := total(mustStatus(t, req(t, s, http.MethodGet, consolePrefix+"/posts"+query, "", editor), http.StatusOK)); got != want {
				t.Errorf("筛选 %s 得到 %v，期望 %v", query, got, want)
			}
		}
	})

	t.Run("所有权：author 只能改自己的", func(t *testing.T) {
		updated := mustStatus(t, req(t, s, http.MethodPut, consolePrefix+"/posts/"+itoa(postID),
			`{"title":"Hello Lumo v2","rawType":"markdown","raw":"# 标题\n\n正文 **加粗**","categoryIds":[`+itoa(catID)+`]}`, author), http.StatusOK)
		if updated["title"] != "Hello Lumo v2" || updated["slug"] != "hello-lumo" {
			t.Errorf("更新结果 = %v", updated)
		}
		if tags, _ := updated["tags"].([]any); len(tags) != 0 {
			t.Errorf("未传 tagIds 应清空标签: %v", updated["tags"])
		}
		revs := mustStatus(t, req(t, s, http.MethodGet, consolePrefix+"/posts/"+itoa(postID)+"/revisions", "", author), http.StatusOK)
		if items, _ := revs["items"].([]any); len(items) != 2 {
			t.Errorf("标题变化应记录修订，实际 %d 条", len(items))
		}

		mustStatus(t, req(t, s, http.MethodPut, consolePrefix+"/posts/"+itoa(editorPostID), `{"title":"篡改"}`, author), http.StatusForbidden)
		mustStatus(t, req(t, s, http.MethodGet, consolePrefix+"/posts/"+itoa(editorPostID), "", author), http.StatusForbidden)
		mustStatus(t, req(t, s, http.MethodDelete, consolePrefix+"/posts/"+itoa(editorPostID), "", author), http.StatusForbidden)
	})

	t.Run("发布需要 posts:publish", func(t *testing.T) {
		rec := req(t, s, http.MethodPost, consolePrefix+"/posts/"+itoa(postID)+"/publish", "", author)
		body := mustStatus(t, rec, http.StatusForbidden)
		if required, _ := body["requiredPermissions"].([]any); len(required) == 0 {
			t.Errorf("403 应列出所需权限: %v", body)
		}

		published := mustStatus(t, req(t, s, http.MethodPost, consolePrefix+"/posts/"+itoa(postID)+"/publish", "", editor), http.StatusOK)
		if published["status"] != "published" || published["publishedAt"] == nil {
			t.Fatalf("发布结果 = %v", published)
		}

		if got := total(mustStatus(t, req(t, s, http.MethodGet, publicPrefix+"/posts", "", ""), http.StatusOK)); got != 1 {
			t.Errorf("公开列表 = %v，期望 1", got)
		}
		pub := mustStatus(t, req(t, s, http.MethodGet, publicPrefix+"/posts/hello-lumo", "", ""), http.StatusOK)
		if pub["title"] != "Hello Lumo v2" {
			t.Errorf("公开文章 = %v", pub)
		}
		mustStatus(t, req(t, s, http.MethodGet, publicPrefix+"/posts/编辑的文章", "", ""), http.StatusNotFound)
	})

	t.Run("定时发布由后台任务推进", func(t *testing.T) {
		scheduledID := idOf(t, mustStatus(t, req(t, s, http.MethodPost, consolePrefix+"/posts", `{"title":"定时","raw":"<p>x</p>"}`, editor), http.StatusCreated))
		at := time.Now().Add(300 * time.Millisecond).Format(time.RFC3339Nano)
		scheduled := mustStatus(t, req(t, s, http.MethodPost, consolePrefix+"/posts/"+itoa(scheduledID)+"/publish", `{"publishAt":"`+at+`"}`, editor), http.StatusOK)
		if scheduled["status"] != "scheduled" {
			t.Fatalf("定时发布结果 = %v", scheduled)
		}
		if n, err := mod.PublishDue(t.Context()); err != nil || n != 0 {
			t.Fatalf("未到点不应推进：n=%d err=%v", n, err)
		}
		time.Sleep(400 * time.Millisecond)
		if n, err := mod.PublishDue(t.Context()); err != nil || n != 1 {
			t.Fatalf("到点应推进一条：n=%d err=%v", n, err)
		}
		if got := total(mustStatus(t, req(t, s, http.MethodGet, publicPrefix+"/posts", "", ""), http.StatusOK)); got != 2 {
			t.Errorf("推进后公开列表 = %v，期望 2", got)
		}

		// 过去时间视为立即发布（允许回填发布日期）。
		past := time.Now().Add(-time.Hour).Format(time.RFC3339)
		back := mustStatus(t, req(t, s, http.MethodPost, consolePrefix+"/posts/"+itoa(editorPostID)+"/publish", `{"publishAt":"`+past+`"}`, editor), http.StatusOK)
		if back["status"] != "published" {
			t.Errorf("过去时间应立即发布: %v", back)
		}
	})

	t.Run("私密内容只对作者与有 write_any 者可见", func(t *testing.T) {
		mustStatus(t, req(t, s, http.MethodPut, consolePrefix+"/posts/"+itoa(postID),
			`{"title":"Hello Lumo v2","rawType":"markdown","raw":"# 标题\n\n正文","visibility":"private"}`, editor), http.StatusOK)
		mustStatus(t, req(t, s, http.MethodGet, publicPrefix+"/posts/hello-lumo", "", ""), http.StatusNotFound)
		mustStatus(t, req(t, s, http.MethodGet, publicPrefix+"/posts/hello-lumo", "", author), http.StatusOK)
		mustStatus(t, req(t, s, http.MethodGet, publicPrefix+"/posts/hello-lumo", "", editor), http.StatusOK)
		anon := total(mustStatus(t, req(t, s, http.MethodGet, publicPrefix+"/posts", "", ""), http.StatusOK))
		own := total(mustStatus(t, req(t, s, http.MethodGet, publicPrefix+"/posts", "", author), http.StatusOK))
		if anon != 2 || own != 3 {
			t.Errorf("匿名应见 2 篇、作者应见 3 篇，实际 %v / %v", anon, own)
		}
	})

	t.Run("撤回后前台不可见", func(t *testing.T) {
		if body := mustStatus(t, req(t, s, http.MethodPost, consolePrefix+"/posts/"+itoa(editorPostID)+"/unpublish", "", editor), http.StatusOK); body["status"] != "draft" {
			t.Errorf("撤回结果 = %v", body)
		}
		mustStatus(t, req(t, s, http.MethodGet, publicPrefix+"/posts/编辑的文章", "", ""), http.StatusNotFound)
	})

	t.Run("回收站与彻底删除", func(t *testing.T) {
		mustStatus(t, req(t, s, http.MethodDelete, consolePrefix+"/posts/"+itoa(postID)+"/permanent", "", author), http.StatusConflict)
		mustStatus(t, req(t, s, http.MethodDelete, consolePrefix+"/posts/"+itoa(postID), "", author), http.StatusNoContent)
		if body := mustStatus(t, req(t, s, http.MethodGet, consolePrefix+"/posts/"+itoa(postID), "", author), http.StatusOK); body["status"] != "trashed" || body["trashedAt"] == nil {
			t.Errorf("回收站状态 = %v", body)
		}
		if got := total(mustStatus(t, req(t, s, http.MethodGet, consolePrefix+"/posts", "", author), http.StatusOK)); got != 0 {
			t.Errorf("默认列表应排除回收站，实际 %v", got)
		}
		if got := total(mustStatus(t, req(t, s, http.MethodGet, consolePrefix+"/posts?status=trashed", "", author), http.StatusOK)); got != 1 {
			t.Errorf("回收站列表 = %v", got)
		}
		mustStatus(t, req(t, s, http.MethodPost, consolePrefix+"/posts/"+itoa(postID)+"/publish", "", editor), http.StatusConflict)
		if body := mustStatus(t, req(t, s, http.MethodPost, consolePrefix+"/posts/"+itoa(postID)+"/restore", "", author), http.StatusOK); body["status"] != "draft" || body["trashedAt"] != nil {
			t.Errorf("恢复结果 = %v", body)
		}
		mustStatus(t, req(t, s, http.MethodPost, consolePrefix+"/posts/"+itoa(postID)+"/restore", "", author), http.StatusConflict)
		mustStatus(t, req(t, s, http.MethodDelete, consolePrefix+"/posts/"+itoa(postID), "", author), http.StatusNoContent)
		mustStatus(t, req(t, s, http.MethodDelete, consolePrefix+"/posts/"+itoa(postID)+"/permanent", "", author), http.StatusNoContent)
		mustStatus(t, req(t, s, http.MethodGet, consolePrefix+"/posts/"+itoa(postID), "", author), http.StatusNotFound)
	})

	t.Run("修订恢复", func(t *testing.T) {
		mustStatus(t, req(t, s, http.MethodPut, consolePrefix+"/posts/"+itoa(editorPostID), `{"title":"第二版","raw":"<p>v2</p>"}`, editor), http.StatusOK)
		mustStatus(t, req(t, s, http.MethodPut, consolePrefix+"/posts/"+itoa(editorPostID), `{"title":"第二版","raw":"<p>v2</p>","pinned":true}`, editor), http.StatusOK)
		revs := mustStatus(t, req(t, s, http.MethodGet, consolePrefix+"/posts/"+itoa(editorPostID)+"/revisions", "", editor), http.StatusOK)
		items, _ := revs["items"].([]any)
		if len(items) != 2 {
			t.Fatalf("只有标题或正文变化才记录修订，实际 %d 条", len(items))
		}
		first, _ := items[len(items)-1].(map[string]any)
		firstID := idOf(t, first)
		if _, hasRaw := first["raw"]; hasRaw {
			t.Error("修订列表不应携带正文")
		}
		full := mustStatus(t, req(t, s, http.MethodGet, consolePrefix+"/posts/"+itoa(editorPostID)+"/revisions/"+itoa(firstID), "", editor), http.StatusOK)
		if full["title"] != "编辑的文章" || full["raw"] != "<p>hi</p>" {
			t.Errorf("首个修订 = %v", full)
		}
		restored := mustStatus(t, req(t, s, http.MethodPost, consolePrefix+"/posts/"+itoa(editorPostID)+"/revisions/"+itoa(firstID)+"/restore", "", editor), http.StatusOK)
		if restored["title"] != "编辑的文章" || restored["pinned"] != true {
			t.Errorf("恢复修订应只回滚标题与正文: %v", restored)
		}
		after := mustStatus(t, req(t, s, http.MethodGet, consolePrefix+"/posts/"+itoa(editorPostID)+"/revisions", "", editor), http.StatusOK)
		if items, _ := after["items"].([]any); len(items) != 3 {
			t.Errorf("恢复应记录新修订，实际 %d 条", len(items))
		}
	})
}

func TestPostValidation(t *testing.T) {
	s, _ := newStack(t)
	editor := s.Bearer(t, "editor", perm.RoleEditor)
	post := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		return req(t, s, http.MethodPost, consolePrefix+"/posts", body, editor)
	}

	mustStatus(t, post(`{"title":"重复","slug":"dup"}`), http.StatusCreated)
	tests := []struct {
		name string
		body string
		want int
	}{
		{"显式 slug 冲突", `{"title":"另一篇","slug":"dup"}`, http.StatusConflict},
		{"标题过长", `{"title":"` + strings.Repeat("长", 257) + `"}`, http.StatusUnprocessableEntity},
		{"空白标题", `{"title":"   "}`, http.StatusBadRequest},
		{"未知原稿格式", `{"title":"x","rawType":"docx"}`, http.StatusUnprocessableEntity},
		{"分类不存在", `{"title":"x","categoryIds":[999999]}`, http.StatusBadRequest},
		{"标签不存在", `{"title":"x","tagIds":[999999]}`, http.StatusBadRequest},
		{"非法可见性", `{"title":"x","visibility":"secret"}`, http.StatusUnprocessableEntity},
		{"未知字段", `{"title":"x","bogus":1}`, http.StatusUnprocessableEntity},
		{"空正文允许", `{"title":"空稿"}`, http.StatusCreated},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if rec := post(tt.body); rec.Code != tt.want {
				t.Errorf("状态码 = %d，期望 %d：%s", rec.Code, tt.want, rec.Body.String())
			}
		})
	}

	t.Run("自动 slug 冲突追加序号", func(t *testing.T) {
		if body := mustStatus(t, post(`{"title":"重复"}`), http.StatusCreated); body["slug"] != "重复" {
			t.Errorf("slug = %v", body["slug"])
		}
		if body := mustStatus(t, post(`{"title":"重复"}`), http.StatusCreated); body["slug"] != "重复-2" {
			t.Errorf("slug = %v，期望 重复-2", body["slug"])
		}
	})

	t.Run("手写摘要优先且不被正文覆盖", func(t *testing.T) {
		body := mustStatus(t, post(`{"title":"摘要","raw":"<p>正文内容</p>","excerpt":"手写摘要"}`), http.StatusCreated)
		if body["excerpt"] != "手写摘要" || body["excerptAuto"] != false {
			t.Errorf("摘要 = %v / %v", body["excerpt"], body["excerptAuto"])
		}
	})
}

func TestPagesEndToEnd(t *testing.T) {
	s, _ := newStack(t)
	editor := s.Bearer(t, "editor", perm.RoleEditor)
	author := s.Bearer(t, "author", perm.RoleAuthor)

	// author 没有 pages:write，连列表都不能看。
	mustStatus(t, req(t, s, http.MethodPost, consolePrefix+"/pages", `{"title":"关于"}`, author), http.StatusForbidden)
	mustStatus(t, req(t, s, http.MethodGet, consolePrefix+"/pages", "", author), http.StatusForbidden)

	page := mustStatus(t, req(t, s, http.MethodPost, consolePrefix+"/pages",
		`{"title":"关于","slug":"about","raw":"<p>关于我们</p>","template":"page-about","categoryIds":[1]}`, editor), http.StatusCreated)
	pageID := idOf(t, page)
	if page["type"] != "page" || page["template"] != "page-about" {
		t.Fatalf("页面 = %v", page)
	}
	if cats, _ := page["categories"].([]any); len(cats) != 0 {
		t.Errorf("页面不参与分类，categoryIds 应被忽略: %v", page["categories"])
	}

	// 文章与页面可以同 slug：前台路径不同。
	mustStatus(t, req(t, s, http.MethodPost, consolePrefix+"/posts", `{"title":"About","slug":"about"}`, editor), http.StatusCreated)

	mustStatus(t, req(t, s, http.MethodGet, publicPrefix+"/pages/about", "", ""), http.StatusNotFound)
	mustStatus(t, req(t, s, http.MethodPost, consolePrefix+"/pages/"+itoa(pageID)+"/publish", "", editor), http.StatusOK)
	pub := mustStatus(t, req(t, s, http.MethodGet, publicPrefix+"/pages/about", "", ""), http.StatusOK)
	if pub["template"] != "page-about" || pub["content"] != "<p>关于我们</p>" {
		t.Errorf("公开页面 = %v", pub)
	}
	// 页面模板名只允许小写字母、数字与连字符。
	mustStatus(t, req(t, s, http.MethodPut, consolePrefix+"/pages/"+itoa(pageID), `{"title":"关于","template":"Page About"}`, editor), http.StatusUnprocessableEntity)
	if got := total(mustStatus(t, req(t, s, http.MethodGet, publicPrefix+"/pages", "", ""), http.StatusOK)); got != 1 {
		t.Errorf("公开页面列表 = %v", got)
	}
}
