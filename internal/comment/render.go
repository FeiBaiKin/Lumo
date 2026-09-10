package comment

import (
	"html"
	"net/url"
	"strings"
	"unicode"
)

// Render 把访客提交的纯文本转成可以直出的 HTML。
//
// 这里的信任模型与文章正文**相反**：文章由已认证用户撰写，原样保留 HTML
// （agent.md §3.4）；评论来自匿名访客，必须假定其中有攻击载荷。
// 因此顺序是「先全文转义，再做有限的富化」——先转义保证任何输入都不可能变成标签，
// 后续加进去的标签全部由本函数自己生成，形态与属性都在掌控之中。
func Render(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	paragraphs := splitParagraphs(text)
	var b strings.Builder
	for _, paragraph := range paragraphs {
		b.WriteString("<p>")
		lines := strings.Split(paragraph, "\n")
		for i, line := range lines {
			if i > 0 {
				b.WriteString("<br>")
			}
			b.WriteString(linkify(line))
		}
		b.WriteString("</p>")
	}
	return b.String()
}

// splitParagraphs 按空行切段，并丢掉空白段。
func splitParagraphs(text string) []string {
	raw := strings.Split(text, "\n\n")
	out := make([]string, 0, len(raw))
	for _, paragraph := range raw {
		if trimmed := strings.Trim(paragraph, "\n"); strings.TrimSpace(trimmed) != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// linkify 转义一行文本，并把其中的 http/https 链接变成锚点。
//
// 只认这两种协议：javascript: 与 data: 是 XSS 的老入口，它们会被当作普通文本转义掉。
// 锚点一律带 rel="nofollow ugc noopener noreferrer"——评论区是 SEO 垃圾的重灾区，
// 也不该把来源页泄漏给外站。
func linkify(line string) string {
	var b strings.Builder
	for line != "" {
		start := findScheme(line)
		if start < 0 {
			b.WriteString(html.EscapeString(line))
			break
		}
		b.WriteString(html.EscapeString(line[:start]))

		rest := line[start:]
		end := len(rest)
		for i, r := range rest {
			if unicode.IsSpace(r) || r == '<' || r == '>' || r == '"' {
				end = i
				break
			}
		}
		raw := trimTrailingPunct(rest[:end])
		if link, ok := safeURL(raw); ok {
			escaped := html.EscapeString(link)
			b.WriteString(`<a href="` + escaped + `" rel="nofollow ugc noopener noreferrer" target="_blank">`)
			b.WriteString(escaped)
			b.WriteString("</a>")
		} else {
			b.WriteString(html.EscapeString(raw))
		}
		line = rest[len(raw):]
	}
	return b.String()
}

// findScheme 返回下一处 http:// 或 https:// 的起始下标，没有则返回 -1。
func findScheme(line string) int {
	lower := strings.ToLower(line)
	http := strings.Index(lower, "http://")
	https := strings.Index(lower, "https://")
	switch {
	case http < 0:
		return https
	case https < 0:
		return http
	default:
		return min(http, https)
	}
}

// trimTrailingPunct 去掉链接末尾的句读：「见 https://example.com。」里的句号不属于地址。
func trimTrailingPunct(s string) string {
	return strings.TrimRight(s, ".,;:!?)]}'\"，。；：！？）】」")
}

// safeURL 校验链接确实是 http(s) 绝对地址。
func safeURL(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", false
	}
	return raw, true
}

// sanitizeAuthorURL 校验访客填写的个人主页地址。
//
// 只接受 http(s)：站点会把它渲染成评论者名字上的链接，javascript: 一旦漏过去，
// 每个看评论的人都会中招。
func sanitizeAuthorURL(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", true
	}
	link, ok := safeURL(raw)
	if !ok {
		return "", false
	}
	return link, true
}
