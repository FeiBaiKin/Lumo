package main

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// 前台共享缓存：改了文章、分类，或者绕过接口直接写库，刷新前台立刻看到；页面与主题样式都压缩。
func TestFrontendCacheFollowsWrites(t *testing.T) {
	s := newTestSite(t)
	s.createUser(t, "admin", "admin")
	admin := s.client(t)
	if status := admin.login("admin", "password-admin"); status != http.StatusOK {
		t.Fatalf("登录：%d", status)
	}

	status, out := admin.do(http.MethodPost, "/api/v1/console/categories", map[string]any{"name": "旧分类", "slug": "cat"})
	mustStatus(t, "建分类", status, http.StatusCreated, out)
	categoryID := out["id"]
	status, out = admin.do(http.MethodPost, "/api/v1/console/tags", map[string]any{"name": "某标签", "slug": "tag"})
	mustStatus(t, "建标签", status, http.StatusCreated, out)
	tagID := out["id"]

	post := map[string]any{
		"title": "缓存检查", "slug": "cache-check", "rawType": "markdown", "raw": "正文一。\n",
		"categoryIds": []any{categoryID}, "tagIds": []any{tagID},
	}
	status, out = admin.do(http.MethodPost, "/api/v1/console/posts", post)
	mustStatus(t, "发文", status, http.StatusCreated, out)
	postID := jsonID(out["id"].(float64))
	status, out = admin.do(http.MethodPost, "/api/v1/console/posts/"+postID+"/publish", map[string]any{})
	mustStatus(t, "发布", status, http.StatusOK, out)

	anon := s.client(t)
	expect := func(step string, want ...string) {
		t.Helper()
		code, html := anon.page("/posts/cache-check")
		if code != http.StatusOK {
			t.Fatalf("%s：文章页 %d", step, code)
		}
		for _, w := range want {
			if !strings.Contains(html, w) {
				t.Fatalf("%s：页面里没有 %q", step, w)
			}
		}
	}
	expect("首次打开", "缓存检查", "正文一", "旧分类", "某标签")
	expect("再次打开", "缓存检查", "旧分类")

	post["title"], post["raw"] = "改过的标题", "正文二。\n"
	status, out = admin.do(http.MethodPut, "/api/v1/console/posts/"+postID, post)
	mustStatus(t, "改文章", status, http.StatusOK, out)
	expect("改完文章", "改过的标题", "正文二")

	status, out = admin.do(http.MethodPut, "/api/v1/console/categories/"+jsonID(categoryID.(float64)),
		map[string]any{"name": "新分类", "slug": "cat"})
	mustStatus(t, "改分类", status, http.StatusOK, out)
	expect("改完分类", "新分类")

	// 插件经宿主写内容、定时发布都不走后台接口，靠数据库层的写入钩子失效
	if _, err := s.site.application.DB().NewRaw("UPDATE posts SET title = '直接写库' WHERE slug = 'cache-check'").
		Exec(context.Background()); err != nil {
		t.Fatal(err)
	}
	expect("直接写库之后", "直接写库")

	resp, body := anon.raw(http.MethodGet, "/posts/cache-check", nil, map[string]string{"Accept-Encoding": "gzip"})
	if resp.Header.Get("Content-Encoding") != "gzip" || !strings.Contains(resp.Header.Get("Vary"), "Accept-Encoding") {
		t.Fatalf("页面应当 gzip：%v", resp.Header)
	}
	if len(body) == 0 || body[0] != 0x1f {
		t.Fatal("正文不是 gzip 数据")
	}
	resp, _ = anon.raw(http.MethodGet, "/theme-assets/ink/theme.css", nil,
		map[string]string{"Accept-Encoding": "gzip", "Range": "bytes=0-9"})
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Encoding") != "gzip" {
		t.Fatalf("主题样式应当整份 gzip 发回：%d %v", resp.StatusCode, resp.Header)
	}
}
