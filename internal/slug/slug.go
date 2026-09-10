// Package slug 生成与校验 URL 片段。
//
// 规则（阶段 3 定稿，文章、页面、分类、标签共用）：
//   - NFKC 归一化后转小写；
//   - 保留任何文字的字母、数字、下划线与组合标记，因此中文等 CJK 原样保留；
//   - 其余字符（空白、标点、斜杠等）折叠为单个连字符，并修剪首尾连字符；
//   - 长度上限 MaxLength 个字符；
//   - 无法生成（如纯表情符号）时返回空串，由调用方决定回退策略。
//
// 站点可选拼音策略：Pinyin 先把汉字转成拼音再走同一规则，得到全 ASCII 的片段。
package slug

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mozillazg/go-pinyin"
	"golang.org/x/text/unicode/norm"
)

// MaxLength 是 slug 的最大字符数（按 Unicode 字符计）。
const MaxLength = 128

// Make 由任意文本生成 slug；无法生成时返回空串。
func Make(s string) string {
	s = strings.ToLower(norm.NFKC.String(s))

	var b strings.Builder
	b.Grow(len(s))
	pendingDash := false
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r) || r == '_' {
			if pendingDash && b.Len() > 0 {
				b.WriteByte('-')
			}
			pendingDash = false
			b.WriteRune(r)
			continue
		}
		// 其余字符一律视为分隔符；连续分隔符只产生一个连字符，首尾不产生。
		pendingDash = true
	}
	return truncate(b.String(), MaxLength)
}

// pinyinArgs 是拼音转换参数：不带声调，多音字取首选读音。
var pinyinArgs = pinyin.NewArgs()

// Pinyin 把汉字转为拼音后再生成 slug，非汉字部分按 Make 的规则处理。
//
// 例：「你好 World」→ "ni-hao-world"。多音字按字典首选读音，不做词级消歧。
func Pinyin(s string) string {
	var b strings.Builder
	b.Grow(len(s) * 2)
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			if readings := pinyin.SinglePinyin(r, pinyinArgs); len(readings) > 0 {
				// 前后补空格，让每个音节成为独立片段。
				b.WriteByte(' ')
				b.WriteString(readings[0])
				b.WriteByte(' ')
				continue
			}
		}
		b.WriteRune(r)
	}
	return Make(b.String())
}

// Valid 报告 s 是否为规范 slug：非空、不超长，且是 Make 的不动点。
func Valid(s string) bool {
	return s != "" && utf8.RuneCountInString(s) <= MaxLength && Make(s) == s
}

// truncate 按字符数截断，并去掉截断后残留在尾部的分隔符。
func truncate(s string, limit int) string {
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	runes := []rune(s)
	return strings.TrimRight(string(runes[:limit]), "-_")
}
