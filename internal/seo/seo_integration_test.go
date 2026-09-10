package seo_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/FeiBaiKin/lumo/internal/auth/perm"
	"github.com/FeiBaiKin/lumo/internal/content"
	"github.com/FeiBaiKin/lumo/internal/migrate"
	"github.com/FeiBaiKin/lumo/internal/seo"
	"github.com/FeiBaiKin/lumo/internal/server"
	"github.com/FeiBaiKin/lumo/internal/settings"
	"github.com/FeiBaiKin/lumo/internal/taxonomy"
	"github.com/FeiBaiKin/lumo/internal/testsupport"
)

// 集成测试直连本机 PG 的独立测试库（agent.md §12），本包使用独占 schema。
const testSchema = "lumo_it_seo"

const (
	consolePrefix = server.PrefixConsole
	publicPrefix  = server.PrefixPublic
	siteURL       = "https://example.com"
)

func newStack(t *testing.T) *testsupport.Stack {
	t.Helper()
	set, tax, con, so := settings.New(), taxonomy.New(), content.New(), seo.New()
	db := testsupport.Open(t, testsupport.Options{
		Schema:  testSchema,
		Migrate: true,
		Sources: []migrate.Source{
			{Name: set.Name(), FS: set.Migrations()},
			{Name: tax.Name(), FS: tax.Migrations()},
			{Name: con.Name(), FS: con.Migrations()},
		},
	})
	return testsupport.NewStack(t, db, set, tax, con, so)
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

func TestSEOEndToEnd(t *testing.T) {
	s := newStack(t)
	admin := s.Bearer(t, "admin", perm.RoleAdmin)

	// 站点地址是生成绝对链接的前提，先配上。
	mustStatus(t, req(t, s, http.MethodPut, consolePrefix+"/settings/site",
		`{"title":"测试站点","url":"`+siteURL+`","description":"站点描述"}`, admin), http.StatusOK)

	t.Run("未配置站点地址时 sitemap 明确报错而非产出相对地址", func(t *testing.T) {
		// 先清空站点地址。
		mustStatus(t, req(t, s, http.MethodPut, consolePrefix+"/settings/site",
			`{"title":"测试站点","url":""}`, admin), http.StatusOK)
		rec := req(t, s, http.MethodGet, seo.PathSitemap, "", "")
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("状态码 = %d，期望 503：%s", rec.Code, rec.Body.String())
		}
		mustStatus(t, req(t, s, http.MethodPut, consolePrefix+"/settings/site",
			`{"title":"测试站点","url":"`+siteURL+`","description":"站点描述"}`, admin), http.StatusOK)
	})

	post := mustStatus(t, req(t, s, http.MethodPost, consolePrefix+"/posts",
		`{"title":"SEO 测试文章","raw":"正文内容","rawType":"markdown","excerpt":"手写摘要"}`, admin), http.StatusCreated)
	postID := int64(post["id"].(float64))
	postSlug, _ := post["slug"].(string)
	mustStatus(t, req(t, s, http.MethodPost, consolePrefix+"/posts/"+strconv.FormatInt(postID, 10)+"/publish",
		`{}`, admin), http.StatusOK)

	// 再加一篇草稿，它不该出现在任何文档里。
	mustStatus(t, req(t, s, http.MethodPost, consolePrefix+"/posts",
		`{"title":"草稿不该出现","raw":"x","rawType":"markdown"}`, admin), http.StatusCreated)

	t.Run("robots.txt 位于根路径且挡掉后台与接口", func(t *testing.T) {
		rec := req(t, s, http.MethodGet, seo.PathRobots, "", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("状态码 = %d", rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
			t.Errorf("Content-Type = %q", ct)
		}
		body := rec.Body.String()
		for _, want := range []string{"User-agent: *", "Disallow: /console/", "Disallow: /api/",
			"Sitemap: " + siteURL + "/sitemap.xml"} {
			if !strings.Contains(body, want) {
				t.Errorf("robots.txt 应含 %q：\n%s", want, body)
			}
		}
	})

	t.Run("sitemap 收录已发布内容、排除草稿", func(t *testing.T) {
		rec := req(t, s, http.MethodGet, seo.PathSitemap, "", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("状态码 = %d：%s", rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		if !strings.Contains(body, siteURL+"/") {
			t.Error("应含首页")
		}
		if !strings.Contains(body, siteURL+"/posts/"+postSlug) {
			t.Errorf("应含已发布文章 %s：\n%s", postSlug, body)
		}
		if strings.Contains(body, "草稿不该出现") || strings.Contains(body, "/posts/draft") {
			t.Errorf("草稿不应进 sitemap：\n%s", body)
		}
	})

	t.Run("RSS 与 Atom 可用且含已发布文章", func(t *testing.T) {
		for _, path := range []string{seo.PathFeed, seo.PathAtom} {
			rec := req(t, s, http.MethodGet, path, "", "")
			if rec.Code != http.StatusOK {
				t.Fatalf("%s 状态码 = %d：%s", path, rec.Code, rec.Body.String())
			}
			body := rec.Body.String()
			if !strings.HasPrefix(body, "<?xml") {
				t.Errorf("%s 应以 XML 声明开头：%s", path, body[:min(60, len(body))])
			}
			if !strings.Contains(body, "SEO 测试文章") {
				t.Errorf("%s 应含已发布文章：\n%s", path, body)
			}
			if strings.Contains(body, "草稿不该出现") {
				t.Errorf("%s 不应含草稿", path)
			}
		}
	})

	t.Run("关掉开关后对应文档 404", func(t *testing.T) {
		mustStatus(t, req(t, s, http.MethodPut, consolePrefix+"/settings/seo",
			`{"sitemapEnabled":false,"feedEnabled":false}`, admin), http.StatusOK)

		// 这两条是纯文本 404，不能按 JSON 解析，直接看状态码。
		for _, path := range []string{seo.PathSitemap, seo.PathFeed, seo.PathAtom} {
			if rec := req(t, s, http.MethodGet, path, "", ""); rec.Code != http.StatusNotFound {
				t.Errorf("%s 状态码 = %d，期望 404", path, rec.Code)
			}
		}
		// robots.txt 总是在，只是不再声明 Sitemap。
		rec := req(t, s, http.MethodGet, seo.PathRobots, "", "")
		if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "Sitemap:") {
			t.Errorf("关闭 sitemap 后 robots.txt 不应再声明：%s", rec.Body.String())
		}

		// 复原，免得影响后续用例。
		mustStatus(t, req(t, s, http.MethodPut, consolePrefix+"/settings/seo",
			`{"sitemapEnabled":true,"feedEnabled":true}`, admin), http.StatusOK)
	})

	t.Run("元信息端点返回绝对地址与 JSON-LD", func(t *testing.T) {
		body := mustStatus(t, req(t, s, http.MethodGet,
			publicPrefix+"/seo/post/"+postSlug, "", ""), http.StatusOK)

		if body["canonical"] != siteURL+"/posts/"+postSlug {
			t.Errorf("canonical 应为绝对地址，实际 %v", body["canonical"])
		}
		if body["description"] != "手写摘要" {
			t.Errorf("应优先用手写摘要，实际 %v", body["description"])
		}
		if body["siteName"] != "测试站点" {
			t.Errorf("siteName 不对：%v", body["siteName"])
		}
		ld, _ := body["jsonld"].(map[string]any)
		if ld["@type"] != "Article" || ld["headline"] != "SEO 测试文章" {
			t.Errorf("JSON-LD 不完整：%v", ld)
		}
		if ld["mainEntityOfPage"] == nil {
			t.Error("JSON-LD 应带 mainEntityOfPage")
		}
	})

	t.Run("未知内容 404，未知类型 422", func(t *testing.T) {
		mustStatus(t, req(t, s, http.MethodGet, publicPrefix+"/seo/post/不存在", "", ""), http.StatusNotFound)
		mustStatus(t, req(t, s, http.MethodGet, publicPrefix+"/seo/unknown/x", "", ""), http.StatusUnprocessableEntity)
	})

	t.Run("匿名可读全部文档", func(t *testing.T) {
		for _, path := range []string{seo.PathRobots, seo.PathFeed, seo.PathAtom} {
			if rec := req(t, s, http.MethodGet, path, "", ""); rec.Code != http.StatusOK {
				t.Errorf("%s 匿名访问状态码 = %d", path, rec.Code)
			}
		}
	})
}
