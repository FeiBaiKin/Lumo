package content

import (
	"regexp"
	"strings"
	"sync"

	"github.com/microcosm-cc/bluemonday"
)

// remoteURL 只匹配带协议的远程地址，用于 iframe 的 src：
// 相对地址（含站内绝对路径）会把同源文档嵌进来，而那与本站共享源。
var remoteURL = regexp.MustCompile(`^https?://`)

/**
 * 正文净化。
 *
 * 背景：正文是**站点同源**输出的，而 Console 与管理 API 也在同一个源上。
 * 一段写进正文的脚本，会在任何访问该页面的浏览器里运行 —— 包括管理员，
 * 而浏览器会自动带上管理员的会话 Cookie。于是「能写内容」就等于
 * 「能在他人浏览器里以他人身份调用管理接口」，编辑与管理员之间的隔离就没了。
 *
 * 因此策略是：
 *   - 持有 content:unsafe_html 的角色（超级管理员、管理员）原样输出，保留
 *     iframe 嵌入与自定义 HTML 块能力；
 *   - 其余编辑者的正文在**保存时**按允许列表净化，`raw` 原稿不动，
 *     编辑器里看到的仍是作者写的原文。
 *
 * 净化只发生在写入路径上（content 是渲染后的产物，主题只消费它），
 * 读取路径因此不必重复付出代价。
 */

// sanitizer 是允许列表策略，进程内共享；bluemonday 的策略是只读的，可并发使用。
var sanitizer = sync.OnceValue(newSanitizer)

func newSanitizer() *bluemonday.Policy {
	p := bluemonday.NewPolicy()

	// 结构与排版。只列真正常用的标签，宁少勿多：
	// 少一个标签是作者会来提的功能请求，多一个是没人再想起来的口子。
	p.AllowElements(
		"p", "br", "hr", "div", "span",
		"h1", "h2", "h3", "h4", "h5", "h6",
		"ul", "ol", "li", "dl", "dt", "dd",
		"blockquote", "pre", "code", "kbd", "samp", "var",
		"strong", "b", "em", "i", "u", "s", "del", "ins", "mark", "sub", "sup", "small", "abbr",
		"figure", "figcaption", "img", "a",
		"table", "thead", "tbody", "tfoot", "tr", "th", "td", "caption", "colgroup", "col",
		"details", "summary", "time", "address", "cite", "q",
	)

	// 媒体。iframe 保留（视频嵌入是刚需），但只允许嵌入**远程 http(s) 文档**：
	// 同源 iframe 与父页面互相可访问，把站内地址塞进 iframe 等于给净化开一扇后门；
	// srcdoc 的文档同样继承本站源，一并拒绝。相对地址与 data: 因此都不放行。
	p.AllowAttrs("src", "controls", "loop", "muted", "poster", "preload", "width", "height").
		OnElements("video", "audio", "source", "track")
	p.AllowAttrs("src").Matching(remoteURL).OnElements("iframe")
	p.AllowAttrs("width", "height", "title", "allow", "allowfullscreen", "loading", "referrerpolicy").
		OnElements("iframe")

	// 链接与图片。href/src 的协议由 URL 策略把关：只有下面这三个加上相对路径，
	// javascript: 与 data: 一律丢掉（data: 图片同样不放行：它可以是 text/html）。
	p.AllowAttrs("href", "title").OnElements("a")
	p.AllowAttrs("src", "alt", "title", "width", "height", "loading", "decoding").OnElements("img")
	p.AllowURLSchemes("http", "https", "mailto")
	p.AllowRelativeURLs(true)
	p.RequireNoFollowOnLinks(false)
	p.RequireNoReferrerOnLinks(true)
	p.AddTargetBlankToFullyQualifiedLinks(true)

	// 表格与代码块需要的结构属性。
	p.AllowAttrs("colspan", "rowspan", "scope", "headers").OnElements("td", "th")
	p.AllowAttrs("start", "reversed", "type").OnElements("ol")
	p.AllowAttrs("value").OnElements("li")
	p.AllowAttrs("open").OnElements("details")
	p.AllowAttrs("datetime").OnElements("time")

	// class 与 id：主题与编辑器都依赖它们（代码高亮、标题锚点、目录跳转）。
	// 它们不构成脚本执行面；真正危险的是事件属性与脚本标签，那些不在允许列表里。
	p.AllowAttrs("class").Globally()
	p.AllowAttrs("id").Globally()
	// 自定义块的 data-* 属性（富文本用 data-* 携带结构）。
	p.AllowDataAttributes()

	// 行内样式：只放行一批纯表现性的属性，且由 bluemonday 校验取值，
	// 避免 style="position:fixed;inset:0" 这类整页覆盖式钓鱼。
	for _, prop := range []string{
		"color", "background-color", "text-align", "text-decoration",
		"font-weight", "font-style", "font-size", "font-family", "line-height",
		"margin", "margin-top", "margin-right", "margin-bottom", "margin-left",
		"padding", "padding-top", "padding-right", "padding-bottom", "padding-left",
		"border", "border-radius", "width", "max-width", "height", "max-height",
	} {
		p.AllowStyles(prop).Globally()
	}

	// 明确拒绝的东西（写出来是为了让下一个人知道这是有意的，而不是遗漏）：
	// script / style / object / embed / form / input / meta / base / link /
	// 事件属性（on*）/ srcdoc / javascript: 与 data: 协议。
	return p
}

// NewPolicy 返回一份新的正文允许列表策略，供要在它之上再放行几样东西的调用方扩展（插件的前台片段）。
func NewPolicy() *bluemonday.Policy { return newSanitizer() }

// Sanitize 按允许列表净化 HTML。仅用于没有 content:unsafe_html 权限的作者产出的正文。
func Sanitize(html string) string {
	if strings.TrimSpace(html) == "" {
		return html
	}
	return sanitizer().Sanitize(html)
}

// sanitizeForWriter 按作者是否持有高危权限决定要不要净化正文。
func sanitizeForWriter(html string, unsafeAllowed bool) string {
	if unsafeAllowed {
		return html
	}
	return Sanitize(html)
}
