package theme

import (
	"fmt"
	"html/template"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/FeiBaiKin/lumo/internal/content"
)

// defaultDateLayout 是 date 函数的缺省格式。
const defaultDateLayout = "2006-01-02"

// namedLayouts 把易记的名字映射到 Go 的时间格式串。
//
// Go 的参考时间写法（2006-01-02）对模板作者极不友好，第一次见没人猜得到。
// 给常用格式起名字，同时仍允许直接传格式串。
var namedLayouts = map[string]string{
	"date":     "2006-01-02",
	"datetime": "2006-01-02 15:04",
	"time":     "15:04",
	"chinese":  "2006年1月2日",
	"rfc3339":  time.RFC3339,
	"kitchen":  time.Kitchen,
}

// baseFuncs 返回与请求无关的模板函数库。
//
// 设计取舍：只提供**纯函数**与格式化工具，数据访问一律走路由上下文或 Finder
// （agent.md §4.2）。不给模板开任意查询的口子——那会把主题变成应用，
// 也会让「主题里写了一条慢查询」这种问题无从排查。
func baseFuncs() template.FuncMap {
	return template.FuncMap{
		// ---------- 字符串 ----------
		"upper":      strings.ToUpper,
		"lower":      strings.ToLower,
		"title":      strings.Title, //nolint:staticcheck // 仅用于拉丁文本的展示，CJK 不受影响
		"trim":       strings.TrimSpace,
		"contains":   strings.Contains,
		"hasPrefix":  strings.HasPrefix,
		"hasSuffix":  strings.HasSuffix,
		"replace":    strings.ReplaceAll,
		"split":      strings.Split,
		"join":       strings.Join,
		"repeat":     safeRepeat,
		"truncate":   truncate,
		"plainify":   content.StripTags,
		"countWords": countWords,
		"readingTime": func(html string) int {
			return readingMinutes(content.StripTags(html))
		},

		// ---------- 数值 ----------
		"add":   func(a, b int) int { return a + b },
		"sub":   func(a, b int) int { return a - b },
		"mul":   func(a, b int) int { return a * b },
		"div":   safeDiv,
		"mod":   safeMod,
		"min":   func(a, b int) int { return min(a, b) },
		"max":   func(a, b int) int { return max(a, b) },
		"seq":   seq,
		"ceil":  ceilDiv,
		"float": func(i int) float64 { return float64(i) },

		// ---------- 时间 ----------
		"now":  time.Now,
		"date": formatDate,
		"year": func(t time.Time) int { return t.Year() },
		"since": func(t time.Time) string {
			return humanizeSince(time.Since(t))
		},

		// ---------- 集合 ----------
		"first":   firstN,
		"last":    lastN,
		"slice":   sliceRange,
		"len":     length,
		"default": defaultValue,
		"dict":    dict,
		"list":    func(items ...any) []any { return items },

		// ---------- URL 与转义 ----------
		"urlquery":  url.QueryEscape,
		"urlize":    urlize,
		"absURL":    joinURL,
		"safeHTML":  func(s string) template.HTML { return template.HTML(s) }, //nolint:gosec // 见下方说明
		"safeCSS":   func(s string) template.CSS { return template.CSS(s) },   //nolint:gosec // 同上
		"safeURL":   func(s string) template.URL { return template.URL(s) },   //nolint:gosec // 同上
		"safeAttr":  func(s string) template.HTMLAttr { return template.HTMLAttr(s) },
		"jsonify":   jsonify,
		"attrEmpty": func(s string) bool { return strings.TrimSpace(s) == "" },
	}
}

// safeHTML 一族是主题作者显式声明「这段内容我担保安全」的出口。
//
// 必须提供：正文 content 就是已渲染的 HTML，主题不能把它当纯文本输出。
// 但它也是主题里唯一能绕过上下文转义的地方，因此集中在此处、逐个命名，
// 便于审查主题时 grep 一遍就知道有几处例外。

// safeRepeat 重复字符串，次数越界时返回空串而不是耗尽内存。
func safeRepeat(s string, count int) string {
	const limit = 1000
	if count <= 0 || count > limit || len(s)*count > 1<<20 {
		return ""
	}
	return strings.Repeat(s, count)
}

// truncate 按字符数截断文本，超长时追加省略号。
func truncate(limit int, s string) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return strings.TrimSpace(string(runes[:limit])) + "…"
}

// countWords 统计词数：拉丁按空白切分，CJK 按字计。
//
// 中英混排的站点用纯空白切分会严重低估中文篇幅（一整段中文算一个词），
// 所以两种规则并用。
func countWords(s string) int {
	total := 0
	inLatinWord := false
	for _, r := range s {
		switch {
		case isCJK(r):
			total++
			inLatinWord = false
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			inLatinWord = false
		default:
			if !inLatinWord {
				total++
				inLatinWord = true
			}
		}
	}
	return total
}

// isCJK 报告字符是否属于需要按字计数的东亚文字区段。
func isCJK(r rune) bool {
	switch {
	case r >= 0x4E00 && r <= 0x9FFF, // CJK 统一表意文字
		r >= 0x3400 && r <= 0x4DBF, // 扩展 A
		r >= 0x3040 && r <= 0x30FF, // 平假名与片假名
		r >= 0xAC00 && r <= 0xD7AF: // 谚文音节
		return true
	}
	return false
}

// readingWordsPerMinute 是估算阅读时长用的速度。
//
// 取 300：中文默读约 300–500 字/分，英文约 200–250 词/分，本站 zh-CN 优先，
// 取中文区间下沿，宁可估长也不要让读者觉得被骗。
const readingWordsPerMinute = 300

// readingMinutes 估算阅读分钟数，至少 1 分钟。
func readingMinutes(text string) int {
	words := countWords(text)
	if words == 0 {
		return 0
	}
	return max(1, int(math.Ceil(float64(words)/readingWordsPerMinute)))
}

// safeDiv 整除，除数为 0 时返回 0 而不是让整个页面 500。
func safeDiv(a, b int) int {
	if b == 0 {
		return 0
	}
	return a / b
}

// safeMod 取余，除数为 0 时返回 0。
func safeMod(a, b int) int {
	if b == 0 {
		return 0
	}
	return a % b
}

// ceilDiv 向上取整的整除，供分页计算总页数。
func ceilDiv(a, b int) int {
	if b == 0 {
		return 0
	}
	if a%b == 0 {
		return a / b
	}
	return a/b + 1
}

// maxSeq 是 seq 能生成的最大长度，防止模板里一行 seq 就把内存吃光。
const maxSeq = 10000

// seq 生成 [from, to] 的整数序列，供模板做分页条一类的循环。
func seq(from, to int) []int {
	if to < from || to-from+1 > maxSeq {
		return []int{}
	}
	out := make([]int, 0, to-from+1)
	for i := from; i <= to; i++ {
		out = append(out, i)
	}
	return out
}

// formatDate 按名称或格式串格式化时间。
func formatDate(layout string, t time.Time) string {
	if t.IsZero() {
		return ""
	}
	if named, ok := namedLayouts[layout]; ok {
		layout = named
	}
	if layout == "" {
		layout = defaultDateLayout
	}
	return t.Format(layout)
}

// humanizeSince 把时间差说成人话。
func humanizeSince(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "刚刚"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + " 分钟前"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d.Hours())) + " 小时前"
	case d < 30*24*time.Hour:
		return strconv.Itoa(int(d.Hours()/24)) + " 天前"
	case d < 365*24*time.Hour:
		return strconv.Itoa(int(d.Hours()/(24*30))) + " 个月前"
	default:
		return strconv.Itoa(int(d.Hours()/(24*365))) + " 年前"
	}
}

// firstN 取前 n 个元素，用反射以支持任意切片。
func firstN(n int, list any) any { return takeSlice(list, 0, n) }

// lastN 取后 n 个元素。
func lastN(n int, list any) any {
	size := length(list)
	if n >= size {
		return takeSlice(list, 0, size)
	}
	return takeSlice(list, size-n, size)
}

// sliceRange 取 [from, to) 区间。
func sliceRange(from, to int, list any) any { return takeSlice(list, from, to) }

// urlize 把任意文本变成适合放进 URL 的片段。
//
// 与 internal/slug 的规则不同：这里只做最小处理（空白转连字符、去首尾），
// 内容的真实 slug 由服务端生成并随对象传入上下文，主题不该自己再算一遍。
func urlize(s string) string {
	return url.PathEscape(strings.Join(strings.Fields(strings.ToLower(s)), "-"))
}

// joinURL 拼接基地址与路径，避免出现双斜杠或缺斜杠。
func joinURL(base, p string) string {
	if p == "" {
		return base
	}
	if strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://") || strings.HasPrefix(p, "//") {
		return p
	}
	return strings.TrimSuffix(base, "/") + "/" + strings.TrimPrefix(p, "/")
}

// defaultValue 在值为空时返回缺省值。
func defaultValue(fallback, value any) any {
	if isEmpty(value) {
		return fallback
	}
	return value
}

// dict 由交替的键值构造 map，供 partial 传参。
//
// html/template 的 template 动作只接受一个参数，要给 partial 传多个值只能靠它。
func dict(values ...any) (map[string]any, error) {
	if len(values)%2 != 0 {
		return nil, fmt.Errorf("dict 需要偶数个参数，实际 %d 个", len(values))
	}
	out := make(map[string]any, len(values)/2)
	for i := 0; i < len(values); i += 2 {
		key, ok := values[i].(string)
		if !ok {
			return nil, fmt.Errorf("dict 的第 %d 个键不是字符串", i/2+1)
		}
		out[key] = values[i+1]
	}
	return out, nil
}
