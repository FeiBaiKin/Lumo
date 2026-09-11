package search

import (
	"strings"
	"unicode"
)

// maxTokens 是单个字段的词元上限。
//
// tsvector 单列上限 1 MB，超了 PostgreSQL 直接报错而不是截断。一篇六万汉字的长文
// 约合六万个二元组、420 KB，这个上限留足了余量。
const maxTokens = 60000

// maxTokenRunes 是单个拉丁词的长度上限：更长的多半是 base64 或哈希，进索引只是噪音。
const maxTokenRunes = 64

// maxQueryTokens 是查询串的词元上限，防止有人拿一整篇文章当关键词提交。
const maxQueryTokens = 64

// segment 是一段连续同类字符切出的词元。
//
// 查询侧需要知道哪些词元原本挨在一起，才能用短语算子还原相邻关系，
// 故切词的原语按段返回，Tokenize 只是把它摊平。
type segment struct {
	tokens []string
	// cjk 为真表示该段是中日韩文字，词元为二元组。
	cjk bool
	// single 为真表示该段只有一个 CJK 字，没能切出任何二元组。
	single bool
}

// Tokenize 把一段文本切成检索用的词元。
//
// 中日韩文字按**二元组**切分（「全文搜索」→ 全文 / 文搜 / 搜索），拉丁字母与数字
// 按连续串切分并转小写，其余字符一律当分隔符。
//
// 不引分词词典：词典要么体积可观、要么需要持续更新，而一旦没收录某个新词，
// 整篇文章就再也搜不到——二元组不会有这种「静默漏召回」。代价是「东京都」这类
// 查询会连带命中「京都」，由查询侧的短语匹配收窄（见 TSQuery）。
func Tokenize(text string) []string {
	segments := scan(text)
	out := make([]string, 0, 16)
	for _, seg := range segments {
		for _, token := range seg.tokens {
			if len(out) >= maxTokens {
				return out
			}
			out = append(out, token)
		}
	}
	return out
}

// TSQuery 把用户输入转成 to_tsquery('simple', …) 能解析的查询串；无可用词元时返回空串。
//
// 同一段 CJK 文字切出的二元组用短语算子 `<->` 相连：「东京都」切成 东京 / 京都，
// 若用 `&` 连接，「京都的东京饭店」这种八竿子打不着的文字也会命中；`<->` 要求两个
// 二元组在原文中相邻，效果等价于子串匹配。段与段之间用 `&`，即各段都要出现。
//
// 只有一个汉字的段落对不上二元组索引（索引里是「中文」而不是「中」），故退化为
// 前缀匹配 `'中':*`——它能命中所有以该字开头的二元组。
func TSQuery(text string) string {
	segments := scan(text)
	parts := make([]string, 0, len(segments))
	budget := maxQueryTokens

	for _, seg := range segments {
		if len(seg.tokens) == 0 || budget <= 0 {
			break
		}
		tokens := seg.tokens
		if len(tokens) > budget {
			tokens = tokens[:budget]
		}
		budget -= len(tokens)

		switch {
		case seg.single:
			parts = append(parts, quoteLexeme(tokens[0])+":*")
		case seg.cjk && len(tokens) > 1:
			quoted := make([]string, 0, len(tokens))
			for _, token := range tokens {
				quoted = append(quoted, quoteLexeme(token))
			}
			parts = append(parts, "("+strings.Join(quoted, " <-> ")+")")
		default:
			parts = append(parts, quoteLexeme(tokens[0]))
		}
	}
	return strings.Join(parts, " & ")
}

// scan 按「连续同类字符」把文本分段切词。
func scan(text string) []segment {
	var (
		out  []segment
		run  []rune
		cjk  bool
		have bool // run 里是否已有字符，用于区分「换了类别」与「刚开始」
	)

	flush := func() {
		if !have || len(run) == 0 {
			run, have = run[:0], false
			return
		}
		if cjk {
			out = append(out, cjkSegment(run))
		} else if token := wordToken(run); token != "" {
			out = append(out, segment{tokens: []string{token}})
		}
		run, have = run[:0], false
	}

	for _, r := range text {
		switch {
		case isCJK(r):
			if have && !cjk {
				flush()
			}
			cjk, have = true, true
			run = append(run, r)
		case isWordRune(r):
			if have && cjk {
				flush()
			}
			cjk, have = false, true
			run = append(run, unicode.ToLower(r))
		default:
			flush()
		}
	}
	flush()
	return out
}

// cjkSegment 把一段 CJK 文字切成二元组；只有一个字时原样保留并标记 single。
func cjkSegment(run []rune) segment {
	if len(run) == 1 {
		return segment{tokens: []string{string(run)}, cjk: true, single: true}
	}
	tokens := make([]string, 0, len(run)-1)
	for i := 0; i+1 < len(run); i++ {
		tokens = append(tokens, string(run[i:i+2]))
	}
	return segment{tokens: tokens, cjk: true}
}

// wordToken 把一段拉丁字母或数字收成一个词元，超长的直接丢弃。
func wordToken(run []rune) string {
	if len(run) > maxTokenRunes {
		return ""
	}
	return string(run)
}

// isCJK 判断是否为按字切分的东亚文字。
//
// 这些文字不用空格分词，逐字成义，故走二元组；其余文字（含西里尔、希腊、阿拉伯）
// 都以空格或标点分词，按整词处理即可。
func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) ||
		unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) ||
		unicode.Is(unicode.Hangul, r)
}

// isWordRune 判断是否为构成词的字符。
func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// quoteLexeme 把词元包成 tsquery 的引号形式。
//
// 词元只可能是字母、数字或 CJK 字符，不会含单引号；转义仍然写上，
// 免得将来放宽切词规则时这里成了注入点。
func quoteLexeme(token string) string {
	return "'" + strings.ReplaceAll(token, "'", "''") + "'"
}
