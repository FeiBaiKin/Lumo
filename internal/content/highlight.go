package content

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"html"
	"io"
	"strings"
	"sync"

	"github.com/alecthomas/chroma/v2"
	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	xhtml "golang.org/x/net/html"
)

/*
 * 代码块语法高亮。
 *
 * 位置的取舍：高亮是**渲染期**而非写入期做的，与净化（sanitize.go）相反。
 *
 *   - 写入期做：存进 content 的就带上高亮标记。代价是已有文章要全部重存一遍才生效，
 *     且换主题、换配色都得重跑一次全库——而 content 是「主题消费的产物」（agent.md §3.3），
 *     不该固化任何一个主题的呈现细节。
 *   - 渲染期做（本实现）：库里存的始终是语义化的 <pre><code class="language-go">，
 *     第三方主题拿到的也是它。行号栏与按钮是主题的事，不进数据库。
 *
 * 渲染期的代价是每次请求都要重新着色（100 行约 4ms），故带一层按内容哈希索引的缓存：
 * 正文极少变动，缓存命中率接近 100%，未命中也只有几毫秒。
 *
 * 输出的结构里**不含**行号文本：行号由主题 CSS 的计数器生成（::before），
 * 这样读者选中代码复制时不会把行号一并复制走——那是带行号的代码块最常见的缺陷。
 */

// maxHighlightBytes 是单个代码块参与着色的源码上限。
//
// 超过它就退回纯文本：着色是 O(n) 的正则匹配，一段几 MB 的压缩后 JS
// 贴进正文会让每次渲染都空耗几百毫秒，而那种内容本来也没人逐行读。
const maxHighlightBytes = 256 << 10

// 代码块识别用到的标签名与语言属性前缀。
const (
	tagPre  = "pre"
	tagCode = "code"
)

// highlightFormatter 输出带 class 的 token（颜色由主题 CSS 给），不自带 <pre> 外壳。
//
// 不用内联样式：内联样式写死一套配色，暗色模式就没法接管了。
var highlightFormatter = chromahtml.New(
	chromahtml.WithClasses(true),
	chromahtml.WithPreWrapper(nopPreWrapper{}),
)

// nopPreWrapper 让 chroma 只吐 token，外壳由本文件自己拼。
type nopPreWrapper struct{}

// Start 实现 chromahtml.PreWrapper。
func (nopPreWrapper) Start(bool, string) string { return "" }

// End 实现 chromahtml.PreWrapper。
func (nopPreWrapper) End(bool) string { return "" }

// Highlight 给渲染后的 HTML 里的代码块加上语法高亮与结构外壳。
//
// 只改写 <pre>…</pre> 这一段，其余字节原样保留——正文里其他部分不该因为
// 「顺手过了一遍 HTML 解析器」而被重新序列化。
func Highlight(rendered string) string {
	if !strings.Contains(rendered, "<pre") {
		return rendered
	}

	var b strings.Builder
	b.Grow(len(rendered) + len(rendered)/2)

	rest := rendered
	for {
		start := indexPreOpen(rest)
		if start < 0 {
			break
		}
		end := indexFold(rest[start:], "</pre>")
		if end < 0 {
			break
		}
		end += start + len("</pre>")

		b.WriteString(rest[:start])
		b.WriteString(highlightBlock(rest[start:end]))
		rest = rest[end:]
	}
	b.WriteString(rest)
	return b.String()
}

// indexPreOpen 找出下一个 <pre 开始标签的位置；-1 表示没有。
//
// 要求 <pre 之后是 >、/ 或空白，否则 <prefetch> 之类会被误判。
func indexPreOpen(s string) int {
	offset := 0
	for {
		i := indexFold(s[offset:], "<pre")
		if i < 0 {
			return -1
		}
		i += offset
		next := i + len("<pre")
		if next >= len(s) {
			return -1
		}
		switch c := s[next]; c {
		case '>', '/', ' ', '\t', '\n', '\r':
			return i
		}
		offset = next
	}
}

// indexFold 是大小写不敏感的子串查找。
func indexFold(s, substr string) int {
	return strings.Index(strings.ToLower(s), substr)
}

// highlightBlock 把一整段 <pre>…</pre> 换成带高亮的结构。
//
// 解析失败时原样返回：正文里出现一个诡异的 pre 不该让整篇文章的排版塌掉。
func highlightBlock(block string) string {
	source, lang := parseCodeBlock(block)
	if strings.TrimSpace(source) == "" {
		return block
	}

	lexer := lookupLexer(lang, source)
	label := ""
	if cfg := lexer.Config(); cfg != nil && cfg.Name != "plaintext" {
		label = cfg.Name
	}

	body, err := highlightSource(lexer, source)
	if err != nil {
		return block
	}

	var b strings.Builder
	b.WriteString(`<figure class="code-block"`)
	if label != "" {
		b.WriteString(` data-lang="`)
		b.WriteString(html.EscapeString(label))
		b.WriteString(`"`)
	}
	b.WriteString(`><pre class="code-lines"><code>`)
	b.WriteString(body)
	b.WriteString(`</code></pre></figure>`)
	return b.String()
}

// highlightSource 着色一段源码，带进程内缓存。
func highlightSource(lexer chroma.Lexer, source string) (string, error) {
	cfg := lexer.Config()
	name := ""
	if cfg != nil {
		name = cfg.Name
	}
	key := sha256.Sum256([]byte(name + "\x00" + source))
	if cached, ok := highlightCache.get(key); ok {
		return cached, nil
	}

	iterator, err := lexer.Tokenise(nil, source)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := highlightFormatter.Format(&buf, styles.Fallback, iterator); err != nil {
		return "", err
	}
	out := buf.String()
	highlightCache.put(key, out)
	return out, nil
}

// lookupLexer 按语言标识挑选词法器，挑不到就退回纯文本。
//
// 不做内容嗅探式的语言猜测：猜错的代价是满屏乱色，比不高亮更糟；
// 作者没写语言就按纯文本排版，行号与按钮照常。
func lookupLexer(lang, source string) chroma.Lexer {
	if len(source) > maxHighlightBytes {
		return lexers.Get("plaintext")
	}
	if lang != "" {
		if l := lexers.Get(lang); l != nil {
			return chroma.Coalesce(l)
		}
	}
	return lexers.Get("plaintext")
}

// parseCodeBlock 从 <pre>…</pre> 里取出源码与语言标识。
//
// 用 HTML tokenizer 而不是正则取文本：实体解码（&lt; &amp; &#39;）必须正确，
// 否则代码里的每个 < 都会变成 &lt; 显示给读者。
func parseCodeBlock(block string) (source, lang string) {
	tokenizer := xhtml.NewTokenizer(strings.NewReader(block))
	var text strings.Builder

	for {
		switch tokenizer.Next() {
		case xhtml.ErrorToken:
			if !errors.Is(tokenizer.Err(), io.EOF) {
				return "", ""
			}
			return text.String(), lang
		case xhtml.StartTagToken, xhtml.SelfClosingTagToken:
			name, hasAttr := tokenizer.TagName()
			tag := string(name)
			if tag == tagPre || tag == tagCode {
				if l := langFromAttrs(tokenizer, hasAttr); l != "" && lang == "" {
					lang = l
				}
				continue
			}
			// <br> 在代码块里是换行，其余标签（高亮残留的 span 等）只取其文本。
			if tag == "br" {
				text.WriteByte('\n')
			}
		case xhtml.EndTagToken:
			// 闭合标签本身不用处理：代码文本已在 TextToken 里收齐。
		case xhtml.TextToken:
			text.Write(tokenizer.Text())
		}
	}
}

// langFromAttrs 从标签属性里读出语言标识。
//
// 认 class="language-go"（goldmark 与 TipTap 的产出）、class="lang-go"
// 与 data-language="go" 三种写法。
func langFromAttrs(tokenizer *xhtml.Tokenizer, hasAttr bool) string {
	for hasAttr {
		var key, val []byte
		key, val, hasAttr = tokenizer.TagAttr()
		switch string(key) {
		case "class":
			for _, cls := range strings.Fields(string(val)) {
				for _, prefix := range []string{"language-", "lang-"} {
					if rest, ok := strings.CutPrefix(cls, prefix); ok && rest != "" {
						return normalizeLang(rest)
					}
				}
			}
		case "data-language":
			if v := strings.TrimSpace(string(val)); v != "" {
				return normalizeLang(v)
			}
		}
	}
	return ""
}

// normalizeLang 把语言标识收敛成安全的小写短串。
//
// 它最终会进 lexers.Get 与 data-lang 属性，故只放行字母、数字与少数几个符号。
func normalizeLang(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if len(s) > 32 {
		return ""
	}
	for i := range len(s) {
		c := s[i]
		ok := (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') ||
			c == '+' || c == '-' || c == '#' || c == '.' || c == '_'
		if !ok {
			return ""
		}
	}
	return s
}

// ---------- 缓存 ----------

// highlightCache 是按内容哈希索引的着色结果缓存。
var highlightCache = &blockCache{entries: map[[32]byte]string{}}

// blockCache 是一个带条数上限的缓存。
//
// 满了就整体清空，不做 LRU：着色结果只是省时间，不是正确性的一部分，
// 一个精确的淘汰策略在这里换不来任何东西，而清空是 O(1) 且没有并发陷阱。
type blockCache struct {
	mu      sync.Mutex
	entries map[[32]byte]string
}

// cacheLimit 是缓存的条数上限。
//
// 512 段代码块够覆盖一个中等站点的全部正文；按单段 8 KB 估算，上限约 4 MB。
const cacheLimit = 512

// get 取缓存。
func (c *blockCache) get(key [32]byte) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.entries[key]
	return v, ok
}

// put 写缓存。
func (c *blockCache) put(key [32]byte, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= cacheLimit {
		c.entries = map[[32]byte]string{}
	}
	c.entries[key] = value
}
