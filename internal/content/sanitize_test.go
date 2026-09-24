package content

import (
	"strings"
	"testing"
)

// 净化守的是「编辑能不能在管理员浏览器里执行脚本」这条边界，每一类绕过方式各一条。
func TestSanitizeRemovesScriptVectors(t *testing.T) {
	cases := []struct {
		name, in, mustNot string
	}{
		{"script 标签", `<p>a</p><script>alert(1)</script>`, "<script"},
		{"事件属性", `<img src="/a.png" onerror="alert(1)">`, "onerror"},
		{"javascript 链接", `<a href="javascript:alert(1)">x</a>`, "javascript:"},
		{"大小写混写的 javascript", `<a href="JaVaScRiPt:alert(1)">x</a>`, "alert"},
		{"data 链接", `<a href="data:text/html,<script>alert(1)</script>">x</a>`, "data:"},
		{"data 图片", `<img src="data:image/svg+xml;base64,PHN2Zz4=">`, "data:"},
		{"srcdoc", `<iframe srcdoc="<script>alert(1)</script>"></iframe>`, "srcdoc"},
		{"同源 iframe", `<iframe src="/console/"></iframe>`, `src="/console/"`},
		{"style 标签", `<style>body{display:none}</style>`, "<style"},
		{"form", `<form action="/api/v1/console/users"><input name="x"></form>`, "<form"},
		{"object", `<object data="/evil.swf"></object>`, "<object"},
		{"整页覆盖的行内样式", `<div style="position:fixed;inset:0">x</div>`, "position"},
		{"meta 跳转", `<meta http-equiv="refresh" content="0;url=https://evil.example">`, "<meta"},
		{"base 标签", `<base href="https://evil.example/">`, "<base"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := Sanitize(tc.in)
			if strings.Contains(strings.ToLower(out), strings.ToLower(tc.mustNot)) {
				t.Fatalf("净化后仍含 %q：%s", tc.mustNot, out)
			}
		})
	}
}

// 允许列表以外的都去掉，但作者正常写的排版、代码块、表格与远程视频嵌入必须原样保留。
func TestSanitizeKeepsAuthoringMarkup(t *testing.T) {
	cases := []struct {
		name, in, must string
	}{
		{"代码块语言", `<pre><code class="language-go">x</code></pre>`, `class="language-go"`},
		{"标题锚点", `<h2 id="intro">引言</h2>`, `id="intro"`},
		{"自定义块的 data 属性", `<div data-callout="tip">提示</div>`, `data-callout="tip"`},
		{"表格合并", `<table><tr><td colspan="2">x</td></tr></table>`, `colspan="2"`},
		{"远程 iframe", `<iframe src="https://player.example.com/v/1"></iframe>`, `src="https://player.example.com/v/1"`},
		{"站内相对链接", `<a href="/posts/hello">x</a>`, `href="/posts/hello"`},
		{"表现性样式", `<p style="text-align: center">x</p>`, "text-align"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if out := Sanitize(tc.in); !strings.Contains(out, tc.must) {
				t.Fatalf("净化后丢了 %q：%s", tc.must, out)
			}
		})
	}
}

func TestSanitizeExternalLinksGetNoReferrer(t *testing.T) {
	out := Sanitize(`<a href="https://example.com">x</a>`)
	if !strings.Contains(out, "noreferrer") || !strings.Contains(out, `target="_blank"`) {
		t.Fatalf("站外链接应带 noreferrer 并在新窗口打开：%s", out)
	}
}
