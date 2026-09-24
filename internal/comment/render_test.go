package comment

import (
	"strings"
	"testing"
	"time"
)

// 评论来自匿名访客：任何输入都不能变成标签，锚点只能由 Render 自己生成。
func TestRenderEscapesEverything(t *testing.T) {
	for _, in := range []string{
		`<script>alert(1)</script>`,
		`<img src=x onerror=alert(1)>`,
		`"><svg onload=alert(1)>`,
		`javascript:alert(1)`,
		`data:text/html,<script>alert(1)</script>`,
	} {
		out := Render(in)
		if strings.Contains(out, "<script") || strings.Contains(out, "<img") ||
			strings.Contains(out, "<svg") || strings.Contains(out, "<a ") {
			t.Errorf("输入 %q 渲染出了标签：%s", in, out)
		}
	}
}

func TestRenderLinksOnlyHTTP(t *testing.T) {
	out := Render(`看这里 https://example.com/a?b=1&c=2 和 javascript:alert(1)`)
	if strings.Count(out, "<a ") != 1 {
		t.Fatalf("应只有 https 那一个链接：%s", out)
	}
	for _, want := range []string{`href="https://example.com/a?b=1&amp;c=2"`, "nofollow", "ugc", "noopener"} {
		if !strings.Contains(out, want) {
			t.Errorf("链接缺少 %q：%s", want, out)
		}
	}
	// 链接里夹着引号也不能提前闭合属性
	quoted := Render(`https://example.com/"onmouseover="alert(1)`)
	if strings.Contains(quoted, `onmouseover="alert`) {
		t.Fatalf("引号闭合了 href 属性：%s", quoted)
	}
}

func TestRenderParagraphs(t *testing.T) {
	if got := Render("第一行\n第二行\n\n\n第二段"); got != "<p>第一行<br>第二行</p><p>第二段</p>" {
		t.Fatalf("分段不对：%s", got)
	}
}

func TestSpamChecker(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	cfg := &Settings{IntervalSeconds: 30, MaxLinks: 2, Blocklist: "# 注释行\n博彩\n"}
	check := NewSpamChecker()
	cases := []struct {
		name string
		in   SpamInput
		spam bool
	}{
		{"正常评论", SpamInput{Content: "写得好", Now: now}, false},
		{"蜜罐被填", SpamInput{Content: "写得好", Honeypot: "x", Now: now}, true},
		{"同 IP 太快", SpamInput{Content: "写得好", LastFromIP: now.Add(-10 * time.Second), Now: now}, true},
		{"间隔够了", SpamInput{Content: "写得好", LastFromIP: now.Add(-31 * time.Second), Now: now}, false},
		{"链接太多", SpamInput{Content: "http://a http://b https://c", Now: now}, true},
		{"关键词在名字里", SpamInput{AuthorName: "博彩推广", Content: "你好", Now: now}, true},
		{"注释行不参与匹配", SpamInput{Content: "# 注释行", Now: now}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := check.Check(cfg, &tc.in).Spam; got != tc.spam {
				t.Fatalf("Spam = %v，应为 %v", got, tc.spam)
			}
		})
	}
}
