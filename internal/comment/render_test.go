package comment

import (
	"strings"
	"testing"
	"time"
)

// TestRenderEscapesInjectedHTML 是本模块最关键的一条安全断言。
//
// 评论是本项目里唯一由匿名访客写入的用户内容，渲染路径一旦漏掉转义，
// 每个看到这条评论的人都会中招。
func TestRenderEscapesInjectedHTML(t *testing.T) {
	t.Parallel()

	cases := []struct {
		input   string
		escaped string
		// forbidden 是绝不该出现在结果里的原文片段。
		forbidden string
	}{
		{"<script>alert(1)</script>", "&lt;script&gt;", "<script"},
		{"<img src=x onerror=alert(1)>", "&lt;img", "<img"},
		{`<a href="javascript:void(0)">`, "&lt;a href=", "<a href"},
		{"正常文字 <b>加粗</b>", "&lt;b&gt;", "<b>"},
	}
	for _, c := range cases {
		got := Render(c.input)
		if !strings.Contains(got, c.escaped) {
			t.Errorf("Render(%q) 应含转义结果 %q：%s", c.input, c.escaped, got)
		}
		if strings.Contains(got, c.forbidden) {
			t.Errorf("Render(%q) 泄漏了可执行标签 %q：%s", c.input, c.forbidden, got)
		}
	}
}

// TestRenderOnlyAllowsHTTPLinks 验证只有 http(s) 会被转成锚点。
func TestRenderOnlyAllowsHTTPLinks(t *testing.T) {
	t.Parallel()

	ok := Render("见 https://example.com/a 一节")
	if !strings.Contains(ok, `<a href="https://example.com/a"`) {
		t.Errorf("http 链接应变成锚点：%s", ok)
	}
	for _, want := range []string{"nofollow", "ugc", "noopener"} {
		if !strings.Contains(ok, want) {
			t.Errorf("锚点应带 %s：%s", want, ok)
		}
	}

	bad := Render("点 javascript:alert(1) 试试")
	if strings.Contains(bad, "<a ") {
		t.Errorf("非 http 协议不应变成锚点：%s", bad)
	}
}

// TestRenderTrimsTrailingPunctuation 验证中文句读不会被吞进链接里。
func TestRenderTrimsTrailingPunctuation(t *testing.T) {
	t.Parallel()

	got := Render("见 https://example.com/a。")
	if !strings.Contains(got, `href="https://example.com/a"`) {
		t.Errorf("句号不应属于链接：%s", got)
	}
	if got := Render("见 https://example.com/a. 后文"); !strings.Contains(got, `href="https://example.com/a"`) {
		t.Errorf("西文句号同样不应属于链接：%s", got)
	}
}

func TestRenderParagraphsAndLineBreaks(t *testing.T) {
	t.Parallel()

	got := Render("第一行\n第二行\n\n第二段")
	if strings.Count(got, "<p>") != 2 {
		t.Errorf("应按空行分成两段：%s", got)
	}
	if !strings.Contains(got, "第一行<br>第二行") {
		t.Errorf("段内换行应保留为 <br>：%s", got)
	}
}

func TestSanitizeAuthorURL(t *testing.T) {
	t.Parallel()

	if _, ok := sanitizeAuthorURL("javascript:alert(1)"); ok {
		t.Error("javascript: 协议应被拒绝")
	}
	if _, ok := sanitizeAuthorURL("data:text/html,x"); ok {
		t.Error("data: 协议应被拒绝")
	}
	if _, ok := sanitizeAuthorURL(""); !ok {
		t.Error("留空应被接受")
	}
	if got, ok := sanitizeAuthorURL(" https://example.com "); !ok || got != "https://example.com" {
		t.Errorf("合法地址应被接受并去空白，实际 %q %v", got, ok)
	}
}

func TestSpamChecker(t *testing.T) {
	t.Parallel()

	checker := NewSpamChecker()
	base := Settings{IntervalSeconds: 30, MaxLinks: 2}
	now := time.Now()

	cases := []struct {
		name string
		in   SpamInput
		spam bool
	}{
		{name: "正常评论", in: SpamInput{Content: "写得不错", Now: now}},
		{name: "蜜罐被填", in: SpamInput{Content: "正常内容", Honeypot: "http://spam", Now: now}, spam: true},
		{name: "间隔过短", in: SpamInput{Content: "正常内容", LastFromIP: now.Add(-5 * time.Second), Now: now}, spam: true},
		{name: "间隔足够", in: SpamInput{Content: "正常内容", LastFromIP: now.Add(-time.Minute), Now: now}},
		{name: "链接过多", in: SpamInput{Content: "http://a.com http://b.com http://c.com", Now: now}, spam: true},
	}
	for _, c := range cases {
		if got := checker.Check(&base, &c.in); got.Spam != c.spam {
			t.Errorf("%s：Spam = %v，期望 %v（判据 %q）", c.name, got.Spam, c.spam, got.Reason)
		}
	}

	blocked := Settings{Blocklist: "# 广告词\n博彩\n免费领取"}
	if got := checker.Check(&blocked, &SpamInput{Content: "快来免费领取奖品", Now: now}); !got.Spam {
		t.Error("命中关键词应判为垃圾")
	}
	// 名字里的关键词同样要被抓到。
	if got := checker.Check(&blocked, &SpamInput{AuthorName: "博彩代理", Content: "你好", Now: now}); !got.Spam {
		t.Error("昵称里的关键词也应命中")
	}
}

func TestBuildTree(t *testing.T) {
	t.Parallel()

	parent := int64(1)
	items := []Comment{
		{ID: 1, AuthorName: "甲", ContentHTML: "顶层", CreatedAt: time.Unix(1, 0)},
		{ID: 2, ParentID: &parent, AuthorName: "乙", ContentHTML: "回复", CreatedAt: time.Unix(2, 0)},
		{ID: 3, AuthorName: "丙", ContentHTML: "另一个顶层", CreatedAt: time.Unix(3, 0)},
		// 父节点不在列表里（待审或已标垃圾），这一支应被丢弃。
		{ID: 4, ParentID: ptrInt64(99), AuthorName: "丁", ContentHTML: "孤儿", CreatedAt: time.Unix(4, 0)},
	}

	roots := BuildTree(items, 0)
	if len(roots) != 2 {
		t.Fatalf("顶层评论应有 2 条，实际 %d", len(roots))
	}
	if len(roots[0].Children) != 1 || roots[0].Children[0].ID != 2 {
		t.Errorf("第一条顶层评论应带一条回复：%+v", roots[0])
	}
	if Count(roots) != 3 {
		t.Errorf("树中节点数应为 3，实际 %d", Count(roots))
	}
}

func TestBuildTreeMarksPostAuthor(t *testing.T) {
	t.Parallel()

	uid := int64(7)
	items := []Comment{
		{ID: 1, UserID: &uid, ContentHTML: "作者的话"},
		{ID: 2, ContentHTML: "访客的话"},
	}
	roots := BuildTree(items, 7)
	if !roots[0].IsAuthor {
		t.Error("内容作者本人的评论应标记 isAuthor")
	}
	if roots[1].IsAuthor {
		t.Error("访客的评论不应标记 isAuthor")
	}
}

func ptrInt64(v int64) *int64 { return &v }
