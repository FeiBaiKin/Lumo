package content

import (
	"strings"
	"testing"
)

func TestRenderMarkdown(t *testing.T) {
	t.Parallel()

	src := "# 标题\n\n## 标题\n\n段落 **加粗**\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n- [x] 任务\n\n<div class=\"custom\">原始 HTML</div>\n"
	out, err := Render(RawMarkdown, src)
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	for _, want := range []string{
		`<h1 id="标题">标题</h1>`,   // 中文标题锚点可读
		`<h2 id="标题-2">标题</h2>`, // 重复标题追加序号
		"<strong>加粗</strong>",
		"<table>",                           // GFM 表格
		`type="checkbox"`,                   // GFM 任务列表
		`<div class="custom">原始 HTML</div>`, // 原始 HTML 直通，不净化
	} {
		if !strings.Contains(out, want) {
			t.Errorf("渲染结果缺少 %q：\n%s", want, out)
		}
	}
}

func TestRenderHTMLPassthrough(t *testing.T) {
	t.Parallel()

	raw := `<p>hello</p><script>alert(1)</script>`
	out, err := Render(RawHTML, raw)
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	if out != raw {
		t.Errorf("HTML 原稿应原样返回，得到 %q", out)
	}
	if _, err := Render(RawType("docx"), "x"); err == nil {
		t.Error("未知格式应报错")
	}
}

func TestStripTags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"段落之间补空格", "<p>你好</p><p>世界</p>", "你好 世界"},
		{"实体解码", "a &amp; b &lt;c&gt; &nbsp;d", "a & b <c> d"},
		{"跳过脚本与样式", `<p>正文</p><script>alert("x")</script><style>p{}</style><p>结尾</p>`, "正文 结尾"},
		{"属性含尖括号的标签", `<a href="/x" title="a>b">链接</a>`, "b\">链接"},
		{"未闭合标签丢弃余下", "前<p", "前"},
		{"折叠空白", "  a \n\n b\t c ", "a b c"},
		{"无标签原样", "plain text", "plain text"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := StripTags(tt.in); got != tt.want {
				t.Errorf("StripTags(%q) = %q，期望 %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestExcerpt(t *testing.T) {
	t.Parallel()

	short := Excerpt("<p>短文</p>", 10)
	if short != "短文" {
		t.Errorf("短内容不应截断: %q", short)
	}
	long := Excerpt("<p>"+strings.Repeat("字", 300)+"</p>", ExcerptLength)
	runes := []rune(long)
	if len(runes) != ExcerptLength+1 || runes[len(runes)-1] != '…' {
		t.Errorf("长内容应截断到 %d 字并加省略号，实际长度 %d", ExcerptLength, len(runes))
	}
}
