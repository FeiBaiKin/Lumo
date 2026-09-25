package plugin

import (
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

// shortcodePattern 匹配 [名字 参数="值" 参数='值' 参数=值 开关]，结尾可带 /。
//
// 写成 [[名字]] 的是转义：原样输出 [名字]，作者在文章里介绍短代码时要用到。
var shortcodePattern = regexp.MustCompile(`\[(\[?)([a-z][a-z0-9-]{0,31})((?:\s+[a-zA-Z][-a-zA-Z0-9_]*(?:=(?:"[^"]*"|'[^']*'|[^\s\]"']+))?)*)\s*/?\](\]?)`)

var shortcodeAttr = regexp.MustCompile(`([a-zA-Z][-a-zA-Z0-9_]*)(?:=(?:"([^"]*)"|'([^']*)'|([^\s\]"']+)))?`)

// rawTextTags 里的文字不展开短代码：代码块里的 [x] 是作者要给读者看的原文。
var rawTextTags = map[string]bool{"pre": true, "code": true, "script": true, "style": true, "textarea": true, "kbd": true, "samp": true}

// expandShortcodes 找出 HTML 文本节点里的短代码，交给 render 渲染；render 返回 false 表示不认得，原样保留。
//
// 只动文本，不动标签与属性：写在链接地址或图片说明属性里的 [x] 不是短代码。
func expandShortcodes(src string, render func(name string, attrs map[string]string) (string, bool)) (string, bool) {
	z := html.NewTokenizer(strings.NewReader(src))
	var b strings.Builder
	changed := false
	skip := 0
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			if !changed {
				return src, false
			}
			return b.String(), true
		case html.StartTagToken, html.EndTagToken:
			name, _ := z.TagName()
			if rawTextTags[string(name)] {
				if tt == html.StartTagToken {
					skip++
				} else if skip > 0 {
					skip--
				}
			}
			b.Write(z.Raw())
		case html.TextToken:
			raw := z.Raw()
			if skip > 0 || !strings.Contains(string(raw), "[") {
				b.Write(raw)
				continue
			}
			text := string(z.Text())
			out, ok := expandText(text, render)
			if !ok {
				b.Write(raw)
				continue
			}
			b.WriteString(out)
			changed = true
		default:
			b.Write(z.Raw())
		}
	}
}

// expandText 展开一段纯文本里的短代码，返回拼好的 HTML（文字部分已转义）。
func expandText(text string, render func(name string, attrs map[string]string) (string, bool)) (string, bool) {
	matches := shortcodePattern.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return "", false
	}
	var b strings.Builder
	changed := false
	last := 0
	for _, loc := range matches {
		start, end := loc[0], loc[1]
		b.WriteString(html.EscapeString(text[last:start]))
		last = end
		whole := text[start:end]
		if loc[3] > loc[2] && loc[len(loc)-1] > loc[len(loc)-2] {
			// [[名字]]：去掉外面那一层括号原样输出
			b.WriteString(html.EscapeString(whole[1 : len(whole)-1]))
			changed = true
			continue
		}
		if loc[3] > loc[2] || loc[len(loc)-1] > loc[len(loc)-2] {
			// 括号只多了一边，不是短代码也不是转义
			b.WriteString(html.EscapeString(whole))
			continue
		}
		name := text[loc[4]:loc[5]]
		attrs := map[string]string{}
		for _, m := range shortcodeAttr.FindAllStringSubmatch(text[loc[6]:loc[7]], -1) {
			attrs[m[1]] = m[2] + m[3] + m[4]
		}
		out, ok := render(name, attrs)
		if !ok {
			b.WriteString(html.EscapeString(whole))
			continue
		}
		b.WriteString(out)
		changed = true
	}
	b.WriteString(html.EscapeString(text[last:]))
	return b.String(), changed
}
