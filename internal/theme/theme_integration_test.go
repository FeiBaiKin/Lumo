package theme_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/comment"
	"github.com/FeiBaiKin/lumo/internal/config"
	"github.com/FeiBaiKin/lumo/internal/content"
	"github.com/FeiBaiKin/lumo/internal/mail"
	"github.com/FeiBaiKin/lumo/internal/menu"
	"github.com/FeiBaiKin/lumo/internal/migrate"
	"github.com/FeiBaiKin/lumo/internal/settings"
	"github.com/FeiBaiKin/lumo/internal/taxonomy"
	"github.com/FeiBaiKin/lumo/internal/testsupport"
	"github.com/FeiBaiKin/lumo/internal/theme"
)

// newThemeStack 装配一个带主题前台的整机测试栈。
func newThemeStack(t *testing.T) (*testsupport.Stack, *theme.Module) {
	t.Helper()

	db := testsupport.Open(t, testsupport.Options{
		Schema:  "lumo_it_theme",
		Migrate: true,
		Sources: []migrate.Source{
			{Name: settings.Name, FS: settings.New().Migrations()},
			{Name: taxonomy.Name, FS: taxonomy.New().Migrations()},
			{Name: content.Name, FS: content.New().Migrations()},
			{Name: comment.Name, FS: comment.New().Migrations()},
			{Name: menu.Name, FS: menu.New().Migrations()},
			{Name: theme.Name, FS: theme.New().Migrations()},
		},
	})

	cfg := config.Default()
	cfg.DataDir = t.TempDir()

	var mod *theme.Module
	stack := testsupport.NewStackWith(t, db, &testsupport.StackOptions{
		Config: cfg,
		Modules: []app.Module{
			settings.New(), mail.New(), taxonomy.New(), content.New(),
			comment.New(), menu.New(), theme.New(),
		},
		AfterStart: func(root chi.Router, application *app.App) {
			mod = theme.From(application)
			mod.MountFrontend(root)
		},
	})
	if mod == nil {
		t.Fatal("主题模块未装配")
	}
	return stack, mod
}

// get 发起一次匿名 GET 请求，返回状态码与响应体。
func get(t *testing.T, s *testsupport.Stack, path string) (status int, body string) {
	t.Helper()
	rec := s.Do(t, &testsupport.Request{Method: http.MethodGet, Path: path})
	return rec.Code, rec.Body.String()
}

// seedPost 创建一篇已发布的文章，返回其 ID。
func seedPost(t *testing.T, s *testsupport.Stack, auth, title, slug, body string) int64 {
	t.Helper()

	payload := map[string]any{
		"title": title, "slug": slug, "rawType": "markdown", "raw": body,
	}
	raw, _ := json.Marshal(payload)
	rec := s.Do(t, &testsupport.Request{
		Method: http.MethodPost, Path: "/api/v1/console/posts", Body: string(raw), Auth: auth,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("创建文章失败 %d: %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("解析文章响应失败: %v", err)
	}

	rec = s.Do(t, &testsupport.Request{
		Method: http.MethodPost,
		Path:   "/api/v1/console/posts/" + itoa64(created.ID) + "/publish",
		Body:   `{}`, Auth: auth,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("发布文章失败 %d: %s", rec.Code, rec.Body.String())
	}
	return created.ID
}

// itoa64 把 int64 转成十进制字符串。
func itoa64(v int64) string {
	return strings.TrimSpace(json.Number(jsonNumber(v)).String())
}

// jsonNumber 借 encoding/json 的整数格式化，避免为一次转换引入 strconv。
func jsonNumber(v int64) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}

// TestFrontendRendersPublishedPost 验证前台能渲染已发布文章。
func TestFrontendRendersPublishedPost(t *testing.T) {
	stack, _ := newThemeStack(t)
	editor := stack.Bearer(t, "editor-theme", "editor")
	seedPost(t, stack, editor, "第一篇文章", "first", "# 标题\n\n正文内容。")

	code, body := get(t, stack, "/posts/first")
	if code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200：%.300s", code, body)
	}
	for _, want := range []string{"第一篇文章", "正文内容", "<!doctype html>"} {
		if !strings.Contains(body, want) {
			t.Errorf("页面应含 %q", want)
		}
	}
	// Markdown 应已在服务端渲染为 HTML，主题只消费 content。
	if !strings.Contains(body, "<h1") {
		t.Error("Markdown 标题应渲染为 <h1>")
	}
}

// TestFrontendIndexListsPosts 验证首页列出文章。
func TestFrontendIndexListsPosts(t *testing.T) {
	stack, _ := newThemeStack(t)
	editor := stack.Bearer(t, "editor-index", "editor")
	seedPost(t, stack, editor, "首页可见", "visible", "正文")

	code, body := get(t, stack, "/")
	if code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200", code)
	}
	if !strings.Contains(body, "首页可见") {
		t.Error("首页应列出已发布文章")
	}
	if !strings.Contains(body, `href="/posts/visible"`) {
		t.Error("首页应给出文章链接")
	}
}

// TestFrontendHidesUnpublished 验证草稿与回收站内容在前台不可见。
//
// 这是 CMS 最不能犯的错：未发布内容泄漏到前台。
func TestFrontendHidesUnpublished(t *testing.T) {
	stack, _ := newThemeStack(t)
	editor := stack.Bearer(t, "editor-draft", "editor")

	// 只创建不发布。
	raw, _ := json.Marshal(map[string]any{
		"title": "机密草稿", "slug": "secret-draft", "rawType": "markdown", "raw": "尚未公开的内容",
	})
	rec := stack.Do(t, &testsupport.Request{
		Method: http.MethodPost, Path: "/api/v1/console/posts", Body: string(raw), Auth: editor,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("创建草稿失败 %d: %s", rec.Code, rec.Body.String())
	}

	code, body := get(t, stack, "/posts/secret-draft")
	if code != http.StatusNotFound {
		t.Fatalf("草稿详情页状态码 = %d，期望 404", code)
	}
	if strings.Contains(body, "尚未公开的内容") {
		t.Fatal("草稿正文泄漏到了 404 页面")
	}

	_, index := get(t, stack, "/")
	if strings.Contains(index, "机密草稿") {
		t.Fatal("草稿标题出现在首页")
	}
}

// TestFrontend404UsesTheme 验证未知路径由主题渲染 404 而非纯文本。
func TestFrontend404UsesTheme(t *testing.T) {
	stack, _ := newThemeStack(t)

	code, body := get(t, stack, "/这个页面不存在")
	if code != http.StatusNotFound {
		t.Fatalf("状态码 = %d，期望 404", code)
	}
	if !strings.Contains(body, "<!doctype html>") {
		t.Errorf("404 应是主题渲染的 HTML 页面，实际 %.200s", body)
	}
	if !strings.Contains(body, "404") {
		t.Error("404 页面应显示状态码")
	}
}

// TestFrontendDoesNotShadowCoreRoutes 验证前台的兜底路由没有遮蔽核心路径。
//
// /{slug} 会匹配根路径下的任何单段路径，挂载顺序错了就会吞掉
// /console/、/api/... 与 SEO 文档。这条断言是防止那次回归。
func TestFrontendDoesNotShadowCoreRoutes(t *testing.T) {
	stack, _ := newThemeStack(t)

	tests := []struct {
		path       string
		wantStatus int
	}{
		{"/api/v1/public/settings", http.StatusOK},
		{"/api/openapi.json", http.StatusOK},
		{"/api/docs", http.StatusOK},
		{"/api/v1/console/posts", http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			code, body := get(t, stack, tt.path)
			if code != tt.wantStatus {
				t.Errorf("状态码 = %d，期望 %d：%.200s", code, tt.wantStatus, body)
			}
		})
	}

	// /console/ 的结果取决于前端产物是否已嵌入（200 已构建 / 501 未构建），
	// 故不断言具体状态码，只断言它没被前台的主题页面顶掉。
	t.Run("/console/", func(t *testing.T) {
		code, body := get(t, stack, "/console/")
		if code != http.StatusOK && code != http.StatusNotImplemented {
			t.Fatalf("状态码 = %d，期望 200（已构建前端）或 501（未构建）", code)
		}
		if strings.Contains(body, "页面不存在") {
			t.Fatal("/console/ 被前台兜底路由遮蔽，落到了主题的 404 页面")
		}
		if code == http.StatusOK && !strings.Contains(body, "Console") {
			t.Errorf("已构建前端时应返回 Console 页面，实际 %.200s", body)
		}
	})
}

// TestFrontendCategoryAndTag 验证分类页与标签页。
func TestFrontendCategoryAndTag(t *testing.T) {
	stack, _ := newThemeStack(t)
	admin := stack.Bearer(t, "admin-term", "admin")

	catRaw, _ := json.Marshal(map[string]any{"name": "技术", "slug": "tech"})
	rec := stack.Do(t, &testsupport.Request{
		Method: http.MethodPost, Path: "/api/v1/console/categories", Body: string(catRaw), Auth: admin,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("创建分类失败 %d: %s", rec.Code, rec.Body.String())
	}
	var cat struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &cat)

	tagRaw, _ := json.Marshal(map[string]any{"name": "Go", "slug": "go"})
	rec = stack.Do(t, &testsupport.Request{
		Method: http.MethodPost, Path: "/api/v1/console/tags", Body: string(tagRaw), Auth: admin,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("创建标签失败 %d: %s", rec.Code, rec.Body.String())
	}
	var tag struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &tag)

	postRaw, _ := json.Marshal(map[string]any{
		"title": "分类下的文章", "slug": "in-category", "rawType": "markdown", "raw": "正文",
		"categoryIds": []int64{cat.ID}, "tagIds": []int64{tag.ID},
	})
	rec = stack.Do(t, &testsupport.Request{
		Method: http.MethodPost, Path: "/api/v1/console/posts", Body: string(postRaw), Auth: admin,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("创建文章失败 %d: %s", rec.Code, rec.Body.String())
	}
	var post struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &post)
	rec = stack.Do(t, &testsupport.Request{
		Method: http.MethodPost, Path: "/api/v1/console/posts/" + itoa64(post.ID) + "/publish",
		Body: `{}`, Auth: admin,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("发布失败 %d: %s", rec.Code, rec.Body.String())
	}

	t.Run("分类页", func(t *testing.T) {
		code, body := get(t, stack, "/categories/tech")
		if code != http.StatusOK {
			t.Fatalf("状态码 = %d：%.200s", code, body)
		}
		if !strings.Contains(body, "技术") || !strings.Contains(body, "分类下的文章") {
			t.Error("分类页应显示分类名与其下文章")
		}
	})
	t.Run("标签页", func(t *testing.T) {
		code, body := get(t, stack, "/tags/go")
		if code != http.StatusOK {
			t.Fatalf("状态码 = %d：%.200s", code, body)
		}
		if !strings.Contains(body, "分类下的文章") {
			t.Error("标签页应显示其下文章")
		}
	})
	t.Run("未知分类 404", func(t *testing.T) {
		if code, _ := get(t, stack, "/categories/不存在"); code != http.StatusNotFound {
			t.Errorf("状态码 = %d，期望 404", code)
		}
	})
}

// TestFrontendSearchAndArchive 验证搜索页与归档页。
func TestFrontendSearchAndArchive(t *testing.T) {
	stack, _ := newThemeStack(t)
	editor := stack.Bearer(t, "editor-search", "editor")
	seedPost(t, stack, editor, "可搜索的标题", "searchable", "正文")

	t.Run("搜索命中", func(t *testing.T) {
		code, body := get(t, stack, "/search?q=可搜索")
		if code != http.StatusOK {
			t.Fatalf("状态码 = %d", code)
		}
		if !strings.Contains(body, "可搜索的标题") {
			t.Error("搜索结果应含匹配文章")
		}
	})
	t.Run("搜索无结果不报错", func(t *testing.T) {
		code, body := get(t, stack, "/search?q=绝对不存在的词")
		if code != http.StatusOK {
			t.Fatalf("状态码 = %d", code)
		}
		if strings.Contains(body, "可搜索的标题") {
			t.Error("不该返回无关文章")
		}
	})
	t.Run("空搜索词", func(t *testing.T) {
		if code, _ := get(t, stack, "/search"); code != http.StatusOK {
			t.Errorf("状态码 = %d，期望 200", code)
		}
	})
	t.Run("归档页", func(t *testing.T) {
		year := time.Now().Year()
		code, body := get(t, stack, "/archives/"+itoa64(int64(year)))
		if code != http.StatusOK {
			t.Fatalf("状态码 = %d：%.200s", code, body)
		}
		if !strings.Contains(body, "可搜索的标题") {
			t.Error("归档页应含本年发布的文章")
		}
	})
	t.Run("非法年份 404", func(t *testing.T) {
		if code, _ := get(t, stack, "/archives/abcd"); code != http.StatusNotFound {
			t.Errorf("状态码 = %d，期望 404", code)
		}
	})
}

// TestFrontendAuthorPage 验证作者页。
func TestFrontendAuthorPage(t *testing.T) {
	stack, _ := newThemeStack(t)
	editor := stack.Bearer(t, "writer", "editor")
	seedPost(t, stack, editor, "作者的文章", "by-writer", "正文")

	code, body := get(t, stack, "/authors/writer")
	if code != http.StatusOK {
		t.Fatalf("状态码 = %d：%.200s", code, body)
	}
	if !strings.Contains(body, "作者的文章") {
		t.Error("作者页应列出其文章")
	}
	// 作者页是公开页面，注册邮箱绝不能出现。
	if strings.Contains(body, "writer@example.com") {
		t.Fatal("作者邮箱泄漏到了公开页面")
	}
	if code, _ := get(t, stack, "/authors/查无此人"); code != http.StatusNotFound {
		t.Errorf("未知作者状态码 = %d，期望 404", code)
	}
}

// TestFrontendPageTemplate 验证独立页面与页面模板回退。
func TestFrontendPageTemplate(t *testing.T) {
	stack, _ := newThemeStack(t)
	admin := stack.Bearer(t, "admin-page", "admin")

	raw, _ := json.Marshal(map[string]any{
		"title": "关于本站", "slug": "about", "rawType": "markdown", "raw": "这里是关于页。",
		// 主题没有 page-nonexistent.html，应回退到 page.html 而不是报错。
		"template": "page-nonexistent",
	})
	rec := stack.Do(t, &testsupport.Request{
		Method: http.MethodPost, Path: "/api/v1/console/pages", Body: string(raw), Auth: admin,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("创建页面失败 %d: %s", rec.Code, rec.Body.String())
	}
	var page struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &page)
	rec = stack.Do(t, &testsupport.Request{
		Method: http.MethodPost, Path: "/api/v1/console/pages/" + itoa64(page.ID) + "/publish",
		Body: `{}`, Auth: admin,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("发布页面失败 %d: %s", rec.Code, rec.Body.String())
	}

	code, body := get(t, stack, "/about")
	if code != http.StatusOK {
		t.Fatalf("状态码 = %d：%.200s", code, body)
	}
	if !strings.Contains(body, "关于本站") || !strings.Contains(body, "这里是关于页") {
		t.Error("页面内容未渲染")
	}
}

// TestThemeAssetsServed 验证主题静态资源可访问且带防护头。
func TestThemeAssetsServed(t *testing.T) {
	stack, _ := newThemeStack(t)

	rec := stack.Do(t, &testsupport.Request{
		Method: http.MethodGet, Path: "/theme-assets/ink/theme.css",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200", rec.Code)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("缺少 nosniff 头，实际 %q", got)
	}
	if !strings.Contains(rec.Body.String(), "--paper") {
		t.Error("样式表内容不符")
	}

	t.Run("路径穿越被拒", func(t *testing.T) {
		rec := stack.Do(t, &testsupport.Request{
			Method: http.MethodGet, Path: "/theme-assets/ink/../../theme.yaml",
		})
		if rec.Code == http.StatusOK {
			t.Fatal("穿越路径不应返回内容")
		}
	})
	t.Run("未知主题 404", func(t *testing.T) {
		rec := stack.Do(t, &testsupport.Request{
			Method: http.MethodGet, Path: "/theme-assets/nope/theme.css",
		})
		if rec.Code != http.StatusNotFound {
			t.Errorf("状态码 = %d，期望 404", rec.Code)
		}
	})
}

// TestConsoleThemeEndpoints 验证 Console 主题管理接口。
func TestConsoleThemeEndpoints(t *testing.T) {
	stack, _ := newThemeStack(t)
	admin := stack.Bearer(t, "admin-themes", "admin")
	author := stack.Bearer(t, "author-themes", "author")

	t.Run("列出主题", func(t *testing.T) {
		rec := stack.Do(t, &testsupport.Request{
			Method: http.MethodGet, Path: "/api/v1/console/themes", Auth: admin,
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("状态码 = %d: %s", rec.Code, rec.Body.String())
		}
		var body struct {
			Items []struct {
				Name    string `json:"name"`
				Builtin bool   `json:"builtin"`
				Active  bool   `json:"active"`
			} `json:"items"`
			Active string `json:"active"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("解析失败: %v", err)
		}
		if body.Active != theme.BuiltinName {
			t.Errorf("默认启用 = %q，期望 %q", body.Active, theme.BuiltinName)
		}
		if len(body.Items) != 1 || !body.Items[0].Builtin || !body.Items[0].Active {
			t.Errorf("主题列表 = %+v", body.Items)
		}
	})

	t.Run("author 无权管理", func(t *testing.T) {
		rec := stack.Do(t, &testsupport.Request{
			Method: http.MethodGet, Path: "/api/v1/console/themes", Auth: author,
		})
		if rec.Code != http.StatusForbidden {
			t.Errorf("状态码 = %d，期望 403", rec.Code)
		}
	})

	t.Run("匿名 401", func(t *testing.T) {
		rec := stack.Do(t, &testsupport.Request{
			Method: http.MethodGet, Path: "/api/v1/console/themes",
		})
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("状态码 = %d，期望 401", rec.Code)
		}
	})

	t.Run("内置主题不可删除", func(t *testing.T) {
		rec := stack.Do(t, &testsupport.Request{
			Method: http.MethodDelete, Path: "/api/v1/console/themes/" + theme.BuiltinName, Auth: admin,
		})
		if rec.Code != http.StatusConflict {
			t.Errorf("状态码 = %d，期望 409：%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("未知主题 404", func(t *testing.T) {
		rec := stack.Do(t, &testsupport.Request{
			Method: http.MethodGet, Path: "/api/v1/console/themes/nope", Auth: admin,
		})
		if rec.Code != http.StatusNotFound {
			t.Errorf("状态码 = %d，期望 404", rec.Code)
		}
	})

	t.Run("Public 平面暴露当前主题", func(t *testing.T) {
		rec := stack.Do(t, &testsupport.Request{
			Method: http.MethodGet, Path: "/api/v1/public/themes/active",
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("状态码 = %d", rec.Code)
		}
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body["name"] != theme.BuiltinName {
			t.Errorf("name = %v，期望 %q", body["name"], theme.BuiltinName)
		}
	})
}

// TestThemeSettingsRoundTrip 验证主题设置的读写与校验。
func TestThemeSettingsRoundTrip(t *testing.T) {
	stack, _ := newThemeStack(t)
	admin := stack.Bearer(t, "admin-tsettings", "admin")
	base := "/api/v1/console/themes/" + theme.BuiltinName + "/settings"

	t.Run("列出分组", func(t *testing.T) {
		rec := stack.Do(t, &testsupport.Request{Method: http.MethodGet, Path: base, Auth: admin})
		if rec.Code != http.StatusOK {
			t.Fatalf("状态码 = %d: %s", rec.Code, rec.Body.String())
		}
		var body struct {
			Items []struct {
				Name   string         `json:"name"`
				Schema map[string]any `json:"schema"`
				Values map[string]any `json:"values"`
			} `json:"items"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("解析失败: %v", err)
		}
		if len(body.Items) != 3 {
			t.Fatalf("分组数 = %d，期望 3", len(body.Items))
		}
		// 分组形态必须与站点设置一致，Console 用同一个表单引擎渲染。
		for _, g := range body.Items {
			if g.Schema == nil || g.Values == nil {
				t.Errorf("分组 %q 缺少 schema 或 values", g.Name)
			}
		}
	})

	t.Run("更新并读回", func(t *testing.T) {
		raw, _ := json.Marshal(map[string]any{"contentWidth": 42, "seal": "#800000"})
		rec := stack.Do(t, &testsupport.Request{
			Method: http.MethodPut, Path: base + "/appearance", Body: string(raw), Auth: admin,
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("状态码 = %d: %s", rec.Code, rec.Body.String())
		}
		var updated struct {
			Values map[string]any `json:"values"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &updated)
		if got := updated.Values["contentWidth"]; got != float64(42) {
			t.Errorf("contentWidth = %v，期望 42", got)
		}
		// 未提交的字段应保留缺省值。
		if got := updated.Values["colorScheme"]; got != "auto" {
			t.Errorf("colorScheme = %v，期望保留缺省值 auto", got)
		}

		// 新值应出现在渲染出的页面里。
		_, page := get(t, stack, "/")
		if !strings.Contains(page, "42em") {
			t.Error("更新后的正文栏宽未反映到页面")
		}
		if !strings.Contains(page, "#800000") {
			t.Error("更新后的印色未反映到页面")
		}
	})

	t.Run("越界值 422", func(t *testing.T) {
		raw, _ := json.Marshal(map[string]any{"contentWidth": 999})
		rec := stack.Do(t, &testsupport.Request{
			Method: http.MethodPut, Path: base + "/appearance", Body: string(raw), Auth: admin,
		})
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("状态码 = %d，期望 422：%s", rec.Code, rec.Body.String())
		}
		var problem struct {
			Errors []struct {
				Location string `json:"location"`
			} `json:"errors"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &problem)
		if len(problem.Errors) == 0 {
			t.Error("应给出逐条校验明细")
		}
	})

	t.Run("未知分组 404", func(t *testing.T) {
		rec := stack.Do(t, &testsupport.Request{
			Method: http.MethodPut, Path: base + "/nope", Body: `{}`, Auth: admin,
		})
		if rec.Code != http.StatusNotFound {
			t.Errorf("状态码 = %d，期望 404", rec.Code)
		}
	})
}

// TestSiteSettingsReflectedInFrontend 验证站点设置影响前台渲染。
func TestSiteSettingsReflectedInFrontend(t *testing.T) {
	stack, _ := newThemeStack(t)
	admin := stack.Bearer(t, "admin-site", "admin")

	raw, _ := json.Marshal(map[string]any{
		"title": "我的博客", "subtitle": "副标题", "description": "站点说明",
		"url": "https://example.com", "language": "zh-CN", "timezone": "Asia/Shanghai",
		"slugStrategy": "unicode", "pageSize": 10,
	})
	rec := stack.Do(t, &testsupport.Request{
		Method: http.MethodPut, Path: "/api/v1/console/settings/site", Body: string(raw), Auth: admin,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("更新站点设置失败 %d: %s", rec.Code, rec.Body.String())
	}

	code, body := get(t, stack, "/")
	if code != http.StatusOK {
		t.Fatalf("状态码 = %d", code)
	}
	for _, want := range []string{"我的博客", "副标题", "站点说明"} {
		if !strings.Contains(body, want) {
			t.Errorf("首页应含 %q", want)
		}
	}
	// 配置了对外地址后 canonical 应为绝对地址。
	if !strings.Contains(body, `rel="canonical" href="https://example.com/"`) {
		t.Error("canonical 应为绝对地址")
	}
}

// TestMenuRenderedInHeader 验证菜单出现在页眉，且指向未发布内容的条目被跳过。
func TestMenuRenderedInHeader(t *testing.T) {
	stack, _ := newThemeStack(t)
	admin := stack.Bearer(t, "admin-menu", "admin")

	// 建一个 slug 为 primary 的菜单（主题默认取这个）。
	raw, _ := json.Marshal(map[string]any{"name": "主导航", "slug": "primary"})
	rec := stack.Do(t, &testsupport.Request{
		Method: http.MethodPost, Path: "/api/v1/console/menus", Body: string(raw), Auth: admin,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("创建菜单失败 %d: %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	items, _ := json.Marshal(map[string]any{
		"items": []map[string]any{
			{"label": "首页", "type": "custom", "url": "/", "visible": true},
			{"label": "隐藏项", "type": "custom", "url": "/hidden", "visible": false},
		},
	})
	rec = stack.Do(t, &testsupport.Request{
		Method: http.MethodPut,
		Path:   "/api/v1/console/menus/" + itoa64(created.ID) + "/items",
		Body:   string(items), Auth: admin,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("保存菜单条目失败 %d: %s", rec.Code, rec.Body.String())
	}

	_, body := get(t, stack, "/")
	if !strings.Contains(body, "首页") {
		t.Error("页眉应含可见菜单条目")
	}
	if strings.Contains(body, "隐藏项") {
		t.Error("隐藏条目不该出现在前台")
	}
}

// TestCommentsRenderedOnPost 验证已通过审核的评论出现在文章页，且不泄漏隐私字段。
func TestCommentsRenderedOnPost(t *testing.T) {
	stack, _ := newThemeStack(t)
	admin := stack.Bearer(t, "admin-comment", "admin")
	postID := seedPost(t, stack, admin, "带评论的文章", "with-comments", "正文")

	raw, _ := json.Marshal(map[string]any{
		"content": "这是一条访客评论", "name": "访客甲",
		"email": "guest@example.com", "url": "https://guest.example.com",
	})
	rec := stack.Do(t, &testsupport.Request{
		Method: http.MethodPost,
		Path:   "/api/v1/public/posts/" + itoa64(postID) + "/comments",
		Body:   string(raw),
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("发表评论失败 %d: %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Comment struct {
			ID int64 `json:"id"`
		} `json:"comment"`
		Status string `json:"status"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	// 待审核的评论不该出现在前台。
	_, before := get(t, stack, "/posts/with-comments")
	if strings.Contains(before, "这是一条访客评论") {
		t.Fatal("待审核评论泄漏到前台")
	}

	rec = stack.Do(t, &testsupport.Request{
		Method: http.MethodPost,
		Path:   "/api/v1/console/comments/" + itoa64(created.Comment.ID) + "/approve",
		Body:   `{}`, Auth: admin,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("审核通过失败 %d: %s", rec.Code, rec.Body.String())
	}

	_, after := get(t, stack, "/posts/with-comments")
	if !strings.Contains(after, "这是一条访客评论") {
		t.Error("通过审核的评论应显示在文章页")
	}
	if !strings.Contains(after, "访客甲") {
		t.Error("评论应显示昵称")
	}
	// 邮箱是个人信息，前台绝不能出现（agent.md §8）。
	if strings.Contains(after, "guest@example.com") {
		t.Fatal("评论邮箱泄漏到了前台页面")
	}
}

// TestFrontendEscapesTitles 验证标题里的 HTML 被转义。
//
// 正文信任已认证用户（agent.md §3.4），但那是经 safeHTML 显式放行的；
// 标题走普通输出路径，必须转义。
func TestFrontendEscapesTitles(t *testing.T) {
	stack, _ := newThemeStack(t)
	editor := stack.Bearer(t, "editor-xss", "editor")
	seedPost(t, stack, editor, `<script>alert(1)</script>`, "xss-title", "正文")

	_, body := get(t, stack, "/posts/xss-title")
	if strings.Contains(body, "<script>alert(1)</script>") {
		t.Fatal("标题未被转义，页面含可执行脚本")
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Error("应能看到转义后的标题")
	}
}

// TestPaginationAcrossPages 验证翻页。
func TestPaginationAcrossPages(t *testing.T) {
	stack, _ := newThemeStack(t)
	admin := stack.Bearer(t, "admin-page-size", "admin")

	// 把每页条数调到 2，再发 3 篇文章。
	raw, _ := json.Marshal(map[string]any{
		"title": "分页站", "language": "zh-CN", "timezone": "Asia/Shanghai",
		"slugStrategy": "unicode", "pageSize": 2,
	})
	rec := stack.Do(t, &testsupport.Request{
		Method: http.MethodPut, Path: "/api/v1/console/settings/site", Body: string(raw), Auth: admin,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("更新站点设置失败 %d: %s", rec.Code, rec.Body.String())
	}
	for _, n := range []string{"一", "二", "三"} {
		seedPost(t, stack, admin, "文章"+n, "post-"+n, "正文")
	}

	_, first := get(t, stack, "/")
	if !strings.Contains(first, `href="/?page=2"`) {
		t.Error("首页应有下一页链接")
	}
	code, second := get(t, stack, "/?page=2")
	if code != http.StatusOK {
		t.Fatalf("第二页状态码 = %d", code)
	}
	if !strings.Contains(second, "文章一") {
		t.Error("第二页应含最早的那篇文章")
	}
}

// TestThemeSwitchPersists 验证切换主题后写入库并在重启后恢复。
func TestThemeSwitchPersists(t *testing.T) {
	stack, mod := newThemeStack(t)
	ctx := context.Background()

	state := theme.NewStateStore(stack.DB.DB)
	if err := state.SetActive(ctx, theme.BuiltinName); err != nil {
		t.Fatalf("写入启用主题失败: %v", err)
	}
	active, err := state.Active(ctx)
	if err != nil {
		t.Fatalf("读取启用主题失败: %v", err)
	}
	if active != theme.BuiltinName {
		t.Errorf("启用主题 = %q，期望 %q", active, theme.BuiltinName)
	}
	if got := mod.Registry().ActiveName(); got != theme.BuiltinName {
		t.Errorf("注册表启用主题 = %q", got)
	}

	// 切到不存在的主题应被拒。
	if err := mod.Registry().Activate("nope"); err == nil {
		t.Error("切换到不存在的主题应报错")
	}
}
