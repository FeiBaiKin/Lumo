package content

import (
	"strings"
	"testing"
)

// TestSanitize 是正文净化的安全回归测试。
//
// 每一条都对应一种真实的绕过手法：正文是站点同源输出的，而 Console 与管理 API
// 在同一个源上，任何一条漏掉都等于「能写内容 = 能以管理员身份调用管理接口」。
func TestSanitize(t *testing.T) {
	t.Parallel()

	kept := []struct {
		name  string
		in    string
		parts []string
	}{
		{
			name:  "普通排版原样保留",
			in:    `<p>中文 <strong>加粗</strong> <em>斜体</em></p>`,
			parts: []string{"<p>", "<strong>加粗</strong>", "<em>斜体</em>"},
		},
		{
			name:  "代码块与语言 class",
			in:    `<pre><code class="language-go">x := 1</code></pre>`,
			parts: []string{`<code class="language-go">`},
		},
		{
			name:  "标题锚点 id 与中文 id",
			in:    `<h2 id="安装步骤">安装步骤</h2>`,
			parts: []string{`id="安装步骤"`},
		},
		{
			name:  "自定义块的 data-* 属性",
			in:    `<div class="note" data-block="tip" data-size="s">提示</div>`,
			parts: []string{`data-block="tip"`, `data-size="s"`, `class="note"`},
		},
		{
			name:  "表格结构属性",
			in:    `<table><tr><td colspan="2">a</td></tr></table>`,
			parts: []string{`colspan="2"`},
		},
		{
			name:  "远程视频嵌入保留",
			in:    `<iframe src="https://www.youtube.com/embed/abc" allowfullscreen></iframe>`,
			parts: []string{`src="https://www.youtube.com/embed/abc"`, "allowfullscreen"},
		},
		{
			name:  "站内相对地址的链接与图片",
			in:    `<a href="/posts/x">x</a><img src="/uploads/a.png" alt="图">`,
			parts: []string{`href="/posts/x"`, `src="/uploads/a.png"`},
		},
		{
			name:  "纯表现性的行内样式保留",
			in:    `<div style="color: red; text-align: center">x</div>`,
			parts: []string{"color: red", "text-align: center"},
		},
	}

	for _, tc := range kept {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Sanitize(tc.in)
			for _, part := range tc.parts {
				if !strings.Contains(got, part) {
					t.Errorf("应保留 %q\n输入: %s\n输出: %s", part, tc.in, got)
				}
			}
		})
	}

	dropped := []struct {
		name  string
		in    string
		parts []string
	}{
		{name: "脚本标签", in: `<p>ok</p><script>alert(1)</script>`, parts: []string{"<script", "alert(1)"}},
		{name: "事件属性", in: `<img src="/a.png" onerror="alert(1)">`, parts: []string{"onerror", "alert(1)"}},
		{name: "javascript 协议", in: `<a href="javascript:alert(1)">x</a>`, parts: []string{"javascript:"}},
		{name: "iframe srcdoc", in: `<iframe srcdoc="<script>alert(1)</script>"></iframe>`, parts: []string{"srcdoc", "<script"}},
		{name: "同源 iframe", in: `<iframe src="/console/posts"></iframe>`, parts: []string{"src="}},
		{name: "相对地址 iframe", in: `<iframe src="posts/1"></iframe>`, parts: []string{"src="}},
		{name: "data 协议 iframe", in: `<iframe src="data:text/html,<script>alert(1)</script>"></iframe>`, parts: []string{"src=", "<script"}},
		{name: "表单元素", in: `<form action="/x"><input name="a"></form>`, parts: []string{"<form", "<input"}},
		{name: "meta 与 base", in: `<meta http-equiv="refresh" content="0;url=/x"><base href="/">`, parts: []string{"<meta", "<base"}},
		{name: "object 与 embed", in: `<object data="/x.swf"></object><embed src="/x.swf">`, parts: []string{"<object", "<embed"}},
		{name: "svg 内嵌脚本", in: `<svg><script>alert(1)</script></svg>`, parts: []string{"<svg", "<script"}},
		{name: "style 标签与表达式", in: `<style>body{background:url(javascript:alert(1))}</style>`, parts: []string{"<style", "javascript:"}},
		{name: "定位型行内样式", in: `<div style="position: fixed; inset: 0; z-index: 999">x</div>`, parts: []string{"position", "inset", "z-index"}},
		{name: "data 协议的图片", in: `<img src="data:text/html;base64,PHNjcmlwdD4=">`, parts: []string{"data:"}},
		{name: "伪装成大小写的脚本标签", in: `<ScRiPt>alert(1)</ScRiPt>`, parts: []string{"alert(1)"}},
	}

	for _, tc := range dropped {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Sanitize(tc.in)
			for _, part := range tc.parts {
				if strings.Contains(strings.ToLower(got), strings.ToLower(part)) {
					t.Errorf("应去掉 %q\n输入: %s\n输出: %s", part, tc.in, got)
				}
			}
		})
	}
}

// TestSanitizeForWriter 验证「有权限就原样输出」这条策略分支。
func TestSanitizeForWriter(t *testing.T) {
	t.Parallel()

	const risky = `<p>x</p><script>alert(1)</script>`
	if got := sanitizeForWriter(risky, true); got != risky {
		t.Errorf("持有 content:unsafe_html 时应原样输出，实际 %s", got)
	}
	if got := sanitizeForWriter(risky, false); strings.Contains(got, "<script") {
		t.Errorf("无权限时应净化，实际 %s", got)
	}
	// 空正文不必走净化器，也不能因此变成空串以外的别的东西。
	if got := Sanitize("   "); got != "   " {
		t.Errorf("空白输入应原样返回，实际 %q", got)
	}
}
