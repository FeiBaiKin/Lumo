package content

import (
	"bytes"
	"fmt"
	"html"
	"strconv"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	goldmarkhtml "github.com/yuin/goldmark/renderer/html"

	"github.com/FeiBaiKin/lumo/internal/slug"
)

// markdown 是共享的 Markdown 转换器：GFM 扩展（表格、删除线、任务列表、自动链接）、
// 自动标题 ID；允许原始 HTML 直通——服务端不做净化（agent.md §3.4）。
// goldmark.Markdown 可并发使用。
var markdown = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	goldmark.WithRendererOptions(goldmarkhtml.WithUnsafe()),
)

// Render 把原稿渲染为主题消费的 HTML（agent.md §3.3）。
//
// HTML 原稿原样返回：编辑器产出的就是规范 HTML；Markdown 经 goldmark 渲染，
// 标题锚点用 slug 规则生成，中文标题得到可读的中文 ID。
func Render(rawType RawType, raw string) (string, error) {
	switch rawType {
	case RawHTML:
		return raw, nil
	case RawMarkdown:
		var buf bytes.Buffer
		pctx := parser.NewContext(parser.WithIDs(newHeadingIDs()))
		if err := markdown.Convert([]byte(raw), &buf, parser.WithContext(pctx)); err != nil {
			return "", fmt.Errorf("渲染 Markdown: %w", err)
		}
		return buf.String(), nil
	default:
		return "", fmt.Errorf("未知的原稿格式 %q", rawType)
	}
}

// headingIDs 实现 parser.IDs：用 slug 规则生成标题锚点，重复时追加序号。
//
// goldmark 默认只保留 ASCII，中文标题会退化成 "heading"，站内目录与外链锚点都不可读。
type headingIDs struct {
	used map[string]bool
}

func newHeadingIDs() *headingIDs {
	return &headingIDs{used: map[string]bool{}}
}

// Generate 实现 parser.IDs。
func (h *headingIDs) Generate(value []byte, _ ast.NodeKind) []byte {
	base := slug.Make(string(value))
	if base == "" {
		base = "heading"
	}
	id := base
	for i := 2; h.used[id]; i++ {
		id = base + "-" + strconv.Itoa(i)
	}
	h.used[id] = true
	return []byte(id)
}

// Put 实现 parser.IDs：登记文档中显式指定的 ID，避免生成的锚点与之重复。
func (h *headingIDs) Put(value []byte) {
	h.used[string(value)] = true
}

// ExcerptLength 是自动摘要的最大字符数（按 Unicode 字符计）。
const ExcerptLength = 200

// Excerpt 从渲染后的 HTML 生成纯文本摘要：去标签、解实体、折叠空白，超长时截断并加省略号。
func Excerpt(content string, limit int) string {
	text := StripTags(content)
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return strings.TrimSpace(string(runes[:limit])) + "…"
}

// StripTags 去掉 HTML 标签（含 script 与 style 的内容），解码实体并折叠空白。
//
// 每个标签边界都产生一个空格，"<p>a</p><p>b</p>" 得到 "a b" 而不是 "ab"。
// 这不是净化器，只用于生成摘要与搜索文本。
func StripTags(s string) string {
	var b strings.Builder
	b.Grow(len(s))

	for i := 0; i < len(s); {
		if s[i] != '<' {
			b.WriteByte(s[i])
			i++
			continue
		}
		end := strings.IndexByte(s[i:], '>')
		if end < 0 {
			// 未闭合的标签：丢弃余下内容。
			break
		}
		tagName := strings.ToLower(strings.TrimLeft(s[i+1:i+end], "/ "))
		if name, skip := skippedElement(tagName); skip {
			// 连同元素内容一起跳过，直到对应的闭合标签。
			closing := strings.Index(strings.ToLower(s[i+end:]), "</"+name)
			if closing < 0 {
				break
			}
			i += end + closing
			if e := strings.IndexByte(s[i:], '>'); e >= 0 {
				i += e + 1
			} else {
				break
			}
			b.WriteByte(' ')
			continue
		}
		b.WriteByte(' ')
		i += end + 1
	}

	text := html.UnescapeString(b.String())
	return strings.Join(strings.Fields(text), " ")
}

// skippedElement 报告开始标签是否属于内容不应进入摘要的元素。
func skippedElement(tag string) (name string, skip bool) {
	for _, candidate := range []string{"script", "style"} {
		if tag == candidate || strings.HasPrefix(tag, candidate+" ") {
			return candidate, true
		}
	}
	return "", false
}
