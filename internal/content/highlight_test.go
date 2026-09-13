package content

import (
	"errors"
	"io"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// textOf 取出 HTML 里的纯文本，不在标签边界补空白。
//
// 不用 StripTags：它为摘要而生，每个标签边界都补一个空格，
// 而这里要验证的恰恰是「代码的每个字符都没被改动」。
func textOf(t *testing.T, s string) string {
	t.Helper()
	tokenizer := xhtml.NewTokenizer(strings.NewReader(s))
	var b strings.Builder
	for {
		switch tokenizer.Next() {
		case xhtml.ErrorToken:
			if err := tokenizer.Err(); !errors.Is(err, io.EOF) {
				t.Fatalf("解析输出失败: %v", err)
			}
			return b.String()
		case xhtml.TextToken:
			b.Write(tokenizer.Text())
		}
	}
}

// TestHighlightWrapsCodeBlocks 验证代码块被套上结构外壳并带上语言标识。
func TestHighlightWrapsCodeBlocks(t *testing.T) {
	in := `<p>前言</p><pre><code class="language-go">package main</code></pre>`
	got := Highlight(in)

	if !strings.Contains(got, `<figure class="code-block"`) {
		t.Errorf("缺少 figure 外壳：%s", got)
	}
	if !strings.Contains(got, `data-lang="Go"`) {
		t.Errorf("缺少语言标识：%s", got)
	}
	if !strings.Contains(got, `<pre class="code-lines"><code>`) {
		t.Errorf("缺少行号容器：%s", got)
	}
	if !strings.Contains(got, "<p>前言</p>") {
		t.Errorf("代码块之外的内容被改动了：%s", got)
	}
	// 着色必须真的发生，否则行号与按钮就套在一段纯文本上。
	if !strings.Contains(got, `<span class="line">`) {
		t.Errorf("没有生成 token：%s", got)
	}
}

// TestHighlightPreservesSourceText 是最要紧的一条：代码的字符不能被改。
//
// 实体解码走错一步，读者看到的就是满屏 &lt; 与 &amp;。
func TestHighlightPreservesSourceText(t *testing.T) {
	in := `<pre><code class="language-html">&lt;div a=&#34;1&#34;&gt;x &amp;&amp; y&lt;/div&gt;</code></pre>`
	got := Highlight(in)

	const want = `<div a="1">x && y</div>`
	if text := textOf(t, got); text != want {
		t.Errorf("源码文本被改动了：\n got %q\nwant %q", text, want)
	}
}

// TestHighlightUnknownLanguage 验证未知语言与无语言时退回纯文本但仍套外壳。
func TestHighlightUnknownLanguage(t *testing.T) {
	cases := map[string]string{
		"无语言":  `<pre><code>hello world</code></pre>`,
		"未知语言": `<pre><code class="language-notalanguage">hello world</code></pre>`,
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			got := Highlight(in)
			if !strings.Contains(got, `<figure class="code-block"`) {
				t.Errorf("应仍套外壳：%s", got)
			}
			// 认不出语言时不该硬贴一个标签上去。
			if strings.Contains(got, "data-lang=") {
				t.Errorf("不该标注语言：%s", got)
			}
			if txt := textOf(t, got); txt != "hello world" {
				t.Errorf("源码被改动：%q", txt)
			}
		})
	}
}

// TestHighlightLeavesOtherHTMLAlone 验证没有代码块时原样返回。
func TestHighlightLeavesOtherHTMLAlone(t *testing.T) {
	in := `<p>一段正文</p><blockquote>引文</blockquote><p><code>行内代码</code></p>`
	if got := Highlight(in); got != in {
		t.Errorf("正文被改动了：\n got %s\nwant %s", got, in)
	}
}

// TestHighlightNoLineNumberText 验证行号不作为文本进入 DOM。
//
// 行号若是文本，读者选中复制代码时会把行号一并复制走——那是带行号的
// 代码块最常见的缺陷，本主题的行号由 CSS 计数器生成。
func TestHighlightNoLineNumberText(t *testing.T) {
	in := "<pre><code class=\"language-go\">a := 1\nb := 2\nc := 3</code></pre>"
	got := Highlight(in)

	if strings.Contains(got, `class="ln"`) {
		t.Errorf("行号不该进 DOM：%s", got)
	}
	// 可复制的文本必须逐字等于原代码，多一个数字都说明行号混了进来。
	const want = "a := 1\nb := 2\nc := 3"
	if txt := textOf(t, got); txt != want {
		t.Errorf("可复制文本与原码不一致：\n got %q\nwant %q", txt, want)
	}
}

// TestHighlightMalformedInput 验证异常输入不 panic 也不吞内容。
func TestHighlightMalformedInput(t *testing.T) {
	cases := []string{
		"<pre>",
		"<pre><code>",
		"<pre></pre>",
		"<pre>   </pre>",
		"<prefetch>不是代码块</prefetch>",
		"",
	}
	for _, in := range cases {
		got := Highlight(in)
		if got == "" && in != "" {
			t.Errorf("输入 %q 被吞掉了", in)
		}
	}
	// <prefetch> 不该被当成 <pre> 处理。
	const pre = "<prefetch>不是代码块</prefetch>"
	if got := Highlight(pre); got != pre {
		t.Errorf("<prefetch> 被误判为代码块：%s", got)
	}
}

// TestHighlightMultipleBlocks 验证一篇文章里的多个代码块都被处理。
func TestHighlightMultipleBlocks(t *testing.T) {
	in := `<pre><code class="language-go">a := 1</code></pre>` +
		`<p>中间</p>` +
		`<pre><code class="language-python">b = 2</code></pre>`
	got := Highlight(in)

	if n := strings.Count(got, `<figure class="code-block"`); n != 2 {
		t.Errorf("应处理 2 个代码块，实际 %d：%s", n, got)
	}
	if !strings.Contains(got, `data-lang="Go"`) || !strings.Contains(got, `data-lang="Python"`) {
		t.Errorf("两个代码块的语言标识不全：%s", got)
	}
}

// TestNormalizeLang 验证语言标识的放行与拒绝边界。
//
// 它会进 data-lang 属性，故必须挡掉引号一类可能破坏属性的字符。
func TestNormalizeLang(t *testing.T) {
	cases := map[string]string{
		"go":                    "go",
		"Go":                    "go",
		"  JavaScript  ":        "javascript",
		"c++":                   "c++",
		"c#":                    "c#",
		"objective-c":           "objective-c",
		`x" onload="alert(1)`:   "",
		"<script>":              "",
		strings.Repeat("a", 33): "",
		"中文":                    "",
	}
	for in, want := range cases {
		if got := normalizeLang(in); got != want {
			t.Errorf("normalizeLang(%q) = %q，期望 %q", in, got, want)
		}
	}
}

// TestHighlightCacheReturnsSameResult 验证缓存命中与未命中结果一致。
func TestHighlightCacheReturnsSameResult(t *testing.T) {
	in := `<pre><code class="language-go">x := 42</code></pre>`
	first := Highlight(in)
	second := Highlight(in)
	if first != second {
		t.Errorf("缓存前后结果不一致：\nfirst  %s\nsecond %s", first, second)
	}
}
