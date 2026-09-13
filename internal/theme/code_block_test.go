package theme

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/FeiBaiKin/lumo/internal/content"
)

/*
 * 代码块的两个契约（agent.md §12：「两端靠契约测试对齐」）。
 *
 * 服务端（chroma）吐 token 类名，主题 CSS 给颜色，主题 JS 挂按钮。
 * 两侧各自都对、中间对不上时，页面不会报任何错——只是代码块退化成
 * 一段没有颜色的灰底文本。这类「静默退化」正是契约测试要挡的东西，
 * 与 TestSettingsSchemaContract 守设置表单是同一个理由。
 */

// styledTokenClasses 是 theme.css 里真实写了颜色的 chroma token 类。
//
// 刻意不写全：运算符（.o/.ow/.p）与标识符（.n/.nx/.nv）不上色是设计决定，
// 见 theme.css 里「运算符与标点刻意不另上色」那段注释。
var styledTokenClasses = []string{
	"c", "c1", "ch", "cm", "cp", "cpf", "cs", "sd",
	"k", "kc", "kd", "kn", "kp", "kr", "kt",
	"s", "s1", "s2", "sa", "sb", "sc", "se", "sh", "si", "sr", "ss", "sx", "dl",
	"m", "mb", "mf", "mh", "mi", "mo", "il",
	"na", "nt",
	"nf", "fm", "nc", "nn", "nd", "ne",
	"gi", "gd", "gh", "gu", "err",
}

// TestCodeHighlightCoversThemesCSS 验证主题样式表覆盖了服务端会吐出的 token 类。
//
// 这条测试在 chroma 升级、换了类名时立刻失败，而不是等到有人发现代码块
// 悄悄变成一片单色。
func TestCodeHighlightCoversThemesCSS(t *testing.T) {
	t.Parallel()

	css := readThemeFile(t, "static/theme.css")

	// 逐个类名去样式表里找：必须作为 .code-lines .<类> 选择器出现。
	var missing []string
	for _, cls := range styledTokenClasses {
		selector := ".code-lines ." + cls
		if !strings.Contains(css, selector) {
			missing = append(missing, selector)
		}
	}
	if len(missing) > 0 {
		t.Errorf("theme.css 缺少这些 token 选择器：\n  %s", strings.Join(missing, "\n  "))
	}
}

// TestCodeHighlightEmitsOnlyKnownTokenClasses 反向验证服务端吐出的类名都在名单里。
//
// 与上一条互为兜底：只查「名单里的都在 CSS 里」会漏掉「chroma 新吐了一个
// 谁也没管的类」；这条把新类名暴露出来，逼着人来决定它该不该有色。
func TestCodeHighlightEmitsOnlyKnownTokenClasses(t *testing.T) {
	t.Parallel()

	samples := map[string]string{
		"php":        "<?php\n// 注释\n$a = \"str\";\nif ($a) { echo 1; }\n",
		"go":         "package main\n// 注释\nfunc main() { s := \"x\"; _ = s }\n",
		"python":     "def f(x):\n    # 注释\n    return \"s\" + str(1)\n",
		"javascript": "// 注释\nfunction f(a) { return \"s\" + 1; }\n",
		"html":       "<div class=\"a\">文本</div>\n",
		"css":        "/* 注释 */\n.a { color: #fff; }\n",
		"json":       "{\"a\": 1, \"b\": \"s\"}\n",
		"bash":       "# 注释\nif [ -f x ]; then echo \"s\"; fi\n",
		"sql":        "-- 注释\nSELECT a FROM t WHERE b = 'x';\n",
		"yaml":       "# 注释\na: 1\nb: \"s\"\n",
		"rust":       "// 注释\nfn main() { let s = \"x\"; }\n",
		"java":       "// 注释\nclass A { int x = 1; String s = \"y\"; }\n",
		"diff":       "--- a\n+++ b\n+added\n-removed\n",
	}

	known := map[string]bool{}
	for _, cls := range styledTokenClasses {
		known[cls] = true
	}
	// 这几个不在名单里是有意的：它们继承墨色（不上色）。
	// .nb（内建名）、.nx（标识符）、.nv（变量）都是高频出现的名字，
	// 上色只会让一段 bash 或 PHP 花掉，区分度也不增反降。
	for _, cls := range []string{
		"o", "ow", "p", "n", "nx", "nv", "nl", "nb", "bp", "py", "w", "x",
		"gp", "gs", "ge", "gh",
	} {
		known[cls] = true
	}

	classRe := regexp.MustCompile(`<span class="([a-z0-9]+)">`)
	unknown := map[string]string{}

	for lang, src := range samples {
		rendered, err := content.Render(content.RawMarkdown, "```"+lang+"\n"+src+"```\n")
		if err != nil {
			t.Fatalf("渲染 %s 失败: %v", lang, err)
		}
		for _, m := range classRe.FindAllStringSubmatch(content.Highlight(rendered), -1) {
			cls := m[1]
			if cls == "line" || cls == "cl" {
				continue
			}
			if !known[cls] {
				unknown[cls] = lang
			}
		}
	}

	if len(unknown) > 0 {
		var lines []string
		for cls, lang := range unknown {
			lines = append(lines, "chroma 吐出了未处置的 token 类 ."+cls+"（来自 "+lang+"）")
		}
		sort.Strings(lines)
		t.Errorf("发现未处置的 token 类，请决定上色还是继承墨色后更新名单：\n  %s",
			strings.Join(lines, "\n  "))
	}
}

// TestCodeBlockCSSCoversServerMarkup 验证服务端产出的每个结构类都有样式与脚本接管。
func TestCodeBlockCSSCoversServerMarkup(t *testing.T) {
	t.Parallel()

	css := readThemeFile(t, "static/theme.css")
	js := readThemeFile(t, "static/code.js")

	// 服务端产出的类（internal/content/highlight.go）必须被主题接管，
	// 否则就是「结构变了、样式没跟上」。
	for _, cls := range []string{"code-block", "code-lines"} {
		if !strings.Contains(css, "."+cls) {
			t.Errorf("theme.css 缺少 .%s 的样式", cls)
		}
	}
	// 脚本必须认这三个类，否则按钮挂不上。
	for _, cls := range []string{"code-block", "code-tools", "code-tool"} {
		if !strings.Contains(js, cls) {
			t.Errorf("code.js 未处理 .%s", cls)
		}
	}
	// 三个动作都要在脚本里实现。
	for _, action := range []string{"wrap", "copy", "expand"} {
		if !strings.Contains(js, `makeButton("`+action+`"`) {
			t.Errorf("code.js 缺少 %s 动作", action)
		}
	}
}

// TestCodeBlockKeepLineNumbersOutOfSelection 验证行号不进可复制文本。
//
// 契约的两端在这里合拢：服务端不写行号文本、CSS 用计数器生成，
// 于是 code.js 的 textContent 天然不含行号。这条守住这个前提。
func TestCodeBlockKeepLineNumbersOutOfSelection(t *testing.T) {
	t.Parallel()

	css := readThemeFile(t, "static/theme.css")
	js := readThemeFile(t, "static/code.js")

	if !strings.Contains(css, "counter(line)") || !strings.Contains(css, "counter-increment: line") {
		t.Error("行号应由 CSS 计数器生成")
	}
	if !strings.Contains(css, "user-select: none") {
		t.Error("行号应禁止选中")
	}
	// 复制取的是 textContent，不能改成读 innerText 之外的东西。
	if !strings.Contains(js, "pre.textContent") {
		t.Error("复制应取代码的 textContent")
	}
}

// TestCodeToolHiddenIsHonoured 验证「短代码块不显示展开按钮」这条真的能生效。
//
// .code-tool 的 display 是作者样式，会盖过浏览器默认表里的 [hidden]{display:none}。
// 少了这条规则，脚本把 hidden 设成 true 也白设——每个代码块都会顶着一个
// 按下去毫无反应的展开键。
func TestCodeToolHiddenIsHonoured(t *testing.T) {
	t.Parallel()

	css := readThemeFile(t, "static/theme.css")
	if !strings.Contains(css, ".code-tool[hidden]") {
		t.Error("theme.css 需要显式的 .code-tool[hidden] { display: none }")
	}
	// 脚本确实在用 hidden 属性控制可见性。
	js := readThemeFile(t, "static/code.js")
	if !strings.Contains(js, "expandBtn.hidden") {
		t.Error("code.js 应用 hidden 控制展开按钮")
	}
}

// readThemeFile 读取内置主题目录下的文件。
func readThemeFile(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile("builtin/" + BuiltinName + "/" + name)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", name, err)
	}
	return string(data)
}
