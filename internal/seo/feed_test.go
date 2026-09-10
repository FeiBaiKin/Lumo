package seo

import (
	"encoding/xml"
	"strings"
	"testing"
	"time"
)

// sampleEntries 返回两条样例内容，覆盖有摘要与无摘要两种情形。
func sampleEntries() []Entry {
	at := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	return []Entry{
		{
			ID: 1, Type: "post", Title: "第一篇 <标题>", Slug: "first",
			Content: "<p>正文一</p>", Excerpt: "摘要一",
			PublishedAt: at, UpdatedAt: at, AuthorName: "甲",
			URL: "https://example.com/posts/first", Summary: "摘要一",
		},
		{
			ID: 2, Type: "post", Title: "第二篇", Slug: "second",
			Content:     "<p>正文二</p>",
			PublishedAt: at.Add(-time.Hour), UpdatedAt: at.Add(-time.Hour), AuthorName: "乙",
			URL: "https://example.com/posts/second", Summary: "正文二",
		},
	}
}

func sampleMeta() *feedMeta {
	return &feedMeta{
		Title: "测试站点", Description: "描述", Language: "zh-CN",
		SiteURL: "https://example.com/",
		SelfRSS: "https://example.com/feed.xml", SelfAtom: "https://example.com/atom.xml",
	}
}

// TestRenderRSSIsWellFormed 同时验证文档能被解析、以及标题里的尖括号被正确转义。
func TestRenderRSSIsWellFormed(t *testing.T) {
	t.Parallel()

	body, err := renderRSS(sampleMeta(), sampleEntries())
	if err != nil {
		t.Fatalf("生成 RSS 失败: %v", err)
	}

	var doc rss
	if err := xml.Unmarshal(body, &doc); err != nil {
		t.Fatalf("RSS 不是合法 XML: %v\n%s", err, body)
	}
	if doc.Version != "2.0" {
		t.Errorf("版本应为 2.0，实际 %q", doc.Version)
	}
	if len(doc.Channel.Items) != 2 {
		t.Fatalf("应有 2 条，实际 %d", len(doc.Channel.Items))
	}
	if doc.Channel.Items[0].Title != "第一篇 <标题>" {
		t.Errorf("标题应原样还原，实际 %q", doc.Channel.Items[0].Title)
	}
	if doc.Channel.Items[0].GUID.Value != "https://example.com/posts/first" {
		t.Errorf("GUID 应为绝对地址，实际 %q", doc.Channel.Items[0].GUID.Value)
	}
	if !strings.Contains(string(body), "<rss") {
		t.Errorf("应输出 rss 根元素：%s", body)
	}
}

func TestRenderAtomIsWellFormed(t *testing.T) {
	t.Parallel()

	body, err := renderAtom(sampleMeta(), sampleEntries())
	if err != nil {
		t.Fatalf("生成 Atom 失败: %v", err)
	}

	var doc atom
	if err := xml.Unmarshal(body, &doc); err != nil {
		t.Fatalf("Atom 不是合法 XML: %v\n%s", err, body)
	}
	if doc.XMLNS != "http://www.w3.org/2005/Atom" {
		t.Errorf("命名空间不对：%q", doc.XMLNS)
	}
	if len(doc.Entries) != 2 {
		t.Fatalf("应有 2 条，实际 %d", len(doc.Entries))
	}
	if doc.Entries[0].Updated == "" {
		t.Error("每条都应有 updated")
	}
}

func TestRenderSitemapIsWellFormed(t *testing.T) {
	t.Parallel()

	body, err := renderSitemap([]Entry{
		{URL: "https://example.com/"},
		{URL: "https://example.com/posts/first", UpdatedAt: time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)},
	})
	if err != nil {
		t.Fatalf("生成 sitemap 失败: %v", err)
	}

	var doc sitemap
	if err := xml.Unmarshal(body, &doc); err != nil {
		t.Fatalf("sitemap 不是合法 XML: %v\n%s", err, body)
	}
	if doc.XMLNS != "http://www.sitemaps.org/schemas/sitemap/0.9" {
		t.Errorf("命名空间不对：%q", doc.XMLNS)
	}
	if len(doc.URLs) != 2 {
		t.Fatalf("应有 2 条，实际 %d", len(doc.URLs))
	}
	if doc.URLs[1].LastMod != "2026-09-10" {
		t.Errorf("lastmod 应为日期精度，实际 %q", doc.URLs[1].LastMod)
	}
}

func TestRenderRobots(t *testing.T) {
	t.Parallel()

	body := string(renderRobots("https://example.com/", "", true))
	for _, want := range []string{"User-agent: *", "Disallow: /console/", "Disallow: /api/",
		"Sitemap: https://example.com/sitemap.xml"} {
		if !strings.Contains(body, want) {
			t.Errorf("robots.txt 应含 %q：\n%s", want, body)
		}
	}

	// 附加内容原样追加；关掉 sitemap 后不再声明。
	custom := string(renderRobots("", "Disallow: /private/", false))
	if !strings.Contains(custom, "Disallow: /private/") {
		t.Errorf("附加规则应保留：\n%s", custom)
	}
	if strings.Contains(custom, "Sitemap:") {
		t.Errorf("未启用 sitemap 时不应声明：\n%s", custom)
	}
}

func TestRenderRobotsWithoutSiteURL(t *testing.T) {
	t.Parallel()

	// 站点地址缺失时不应输出半截 Sitemap 行。
	body := string(renderRobots("", "", true))
	if strings.Contains(body, "Sitemap:") {
		t.Errorf("无站点地址时不应声明 Sitemap：\n%s", body)
	}
}
