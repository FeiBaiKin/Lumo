package theme

import (
	"html/template"
	"strings"
	"testing"
	"time"
)

// TestCountWordsMixedScript 验证中英混排的字数统计。
//
// 纯空白切分会把一整段中文算成一个词，严重低估篇幅；纯按字计又会让英文
// 每个字母都算一个。故 CJK 按字、拉丁按词。
func TestCountWordsMixedScript(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want int
	}{
		{"空串", "", 0},
		{"纯英文", "hello world foo", 3},
		{"纯中文", "你好世界", 4},
		// 你好(2 字) + world(1 词) + 世界(2 字) = 5
		{"中英混排", "你好 world 世界", 5},
		{"标点不单独计数", "hello, world!", 2},
		{"连续空白算一个分隔", "a   b", 2},
		{"日文假名按字", "こんにちは", 5},
		{"中文标点算入拉丁词", "你好，世界", 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := countWords(tt.in); got != tt.want {
				t.Errorf("countWords(%q) = %d，期望 %d", tt.in, got, tt.want)
			}
		})
	}
}

// TestReadingMinutes 验证阅读时长估算，至少 1 分钟、空内容为 0。
func TestReadingMinutes(t *testing.T) {
	t.Parallel()

	if got := readingMinutes(""); got != 0 {
		t.Errorf("空内容 = %d，期望 0", got)
	}
	if got := readingMinutes("你好"); got != 1 {
		t.Errorf("极短内容 = %d，期望至少 1 分钟", got)
	}
	long := strings.Repeat("字", readingWordsPerMinute*3)
	if got := readingMinutes(long); got != 3 {
		t.Errorf("900 字 = %d 分钟，期望 3", got)
	}
}

// TestTruncate 验证按字符而非字节截断。
func TestTruncate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		limit int
		in    string
		want  string
	}{
		{0, "abc", ""},
		{-1, "abc", ""},
		{10, "短文本", "短文本"},
		{2, "你好世界", "你好…"},
		{3, "abcdef", "abc…"},
	}
	for _, tt := range tests {
		if got := truncate(tt.limit, tt.in); got != tt.want {
			t.Errorf("truncate(%d, %q) = %q，期望 %q", tt.limit, tt.in, got, tt.want)
		}
	}
}

// TestSeqBounded 验证 seq 有上限，防止模板一行就把内存吃光。
func TestSeqBounded(t *testing.T) {
	t.Parallel()

	if got := len(seq(1, 5)); got != 5 {
		t.Errorf("seq(1,5) 长度 = %d，期望 5", got)
	}
	if got := seq(5, 1); len(got) != 0 {
		t.Errorf("倒序区间应为空，实际 %v", got)
	}
	if got := seq(1, maxSeq+1); len(got) != 0 {
		t.Errorf("超出上限应返回空，实际长度 %d", len(got))
	}
}

// TestSafeDivisors 验证除零不会让整个页面 500。
func TestSafeDivisors(t *testing.T) {
	t.Parallel()

	if got := safeDiv(1, 0); got != 0 {
		t.Errorf("safeDiv(1,0) = %d，期望 0", got)
	}
	if got := safeMod(1, 0); got != 0 {
		t.Errorf("safeMod(1,0) = %d，期望 0", got)
	}
	if got := ceilDiv(1, 0); got != 0 {
		t.Errorf("ceilDiv(1,0) = %d，期望 0", got)
	}
	if got := ceilDiv(10, 3); got != 4 {
		t.Errorf("ceilDiv(10,3) = %d，期望 4", got)
	}
	if got := ceilDiv(9, 3); got != 3 {
		t.Errorf("ceilDiv(9,3) = %d，期望 3", got)
	}
}

// TestSafeRepeat 验证重复字符串有上限。
func TestSafeRepeat(t *testing.T) {
	t.Parallel()

	if got := safeRepeat("ab", 3); got != "ababab" {
		t.Errorf("safeRepeat = %q", got)
	}
	if got := safeRepeat("ab", 0); got != "" {
		t.Errorf("次数为 0 应返回空串，实际 %q", got)
	}
	if got := safeRepeat("ab", 999999); got != "" {
		t.Errorf("超限应返回空串，实际长度 %d", len(got))
	}
}

// TestFormatDate 验证命名格式与零值处理。
func TestFormatDate(t *testing.T) {
	t.Parallel()

	ts := time.Date(2026, 9, 11, 14, 30, 0, 0, time.UTC)
	tests := []struct {
		layout string
		want   string
	}{
		{"date", "2026-09-11"},
		{"datetime", "2026-09-11 14:30"},
		{"chinese", "2026年9月11日"},
		{"", "2026-09-11"},
		{"2006/01", "2026/09"},
	}
	for _, tt := range tests {
		if got := formatDate(tt.layout, ts); got != tt.want {
			t.Errorf("formatDate(%q) = %q，期望 %q", tt.layout, got, tt.want)
		}
	}
	if got := formatDate("date", time.Time{}); got != "" {
		t.Errorf("零值时间应返回空串，实际 %q", got)
	}
}

// TestJoinURL 验证绝对地址拼接不产生双斜杠，且不改写已有的绝对地址。
func TestJoinURL(t *testing.T) {
	t.Parallel()

	tests := []struct{ base, path, want string }{
		{"https://a.com", "/x", "https://a.com/x"},
		{"https://a.com/", "/x", "https://a.com/x"},
		{"https://a.com/", "x", "https://a.com/x"},
		{"https://a.com", "", "https://a.com"},
		{"https://a.com", "https://b.com/x", "https://b.com/x"},
		{"https://a.com", "//cdn.com/x", "//cdn.com/x"},
	}
	for _, tt := range tests {
		if got := joinURL(tt.base, tt.path); got != tt.want {
			t.Errorf("joinURL(%q,%q) = %q，期望 %q", tt.base, tt.path, got, tt.want)
		}
	}
}

// TestDictRejectsOddArgs 验证 dict 的参数校验。
func TestDictRejectsOddArgs(t *testing.T) {
	t.Parallel()

	got, err := dict("a", 1, "b", 2)
	if err != nil {
		t.Fatalf("合法调用报错: %v", err)
	}
	if got["a"] != 1 || got["b"] != 2 {
		t.Errorf("结果 = %v", got)
	}
	if _, err := dict("a"); err == nil {
		t.Error("奇数个参数应报错")
	}
	if _, err := dict(1, "a"); err == nil {
		t.Error("非字符串键应报错")
	}
}

// TestCollectionHelpers 验证 first / last / slice 越界时收敛而不 panic。
func TestCollectionHelpers(t *testing.T) {
	t.Parallel()

	list := []string{"a", "b", "c"}
	if got := firstN(2, list).([]string); len(got) != 2 || got[0] != "a" {
		t.Errorf("firstN = %v", got)
	}
	if got := firstN(99, list).([]string); len(got) != 3 {
		t.Errorf("firstN 超长应收敛到全部，实际 %v", got)
	}
	if got := lastN(2, list).([]string); len(got) != 2 || got[0] != "b" {
		t.Errorf("lastN = %v", got)
	}
	if got := lastN(99, list).([]string); len(got) != 3 {
		t.Errorf("lastN 超长应收敛到全部，实际 %v", got)
	}
	if got := sliceRange(1, 99, list).([]string); len(got) != 2 {
		t.Errorf("sliceRange 越界应收敛，实际 %v", got)
	}
	if got := sliceRange(5, 9, list).([]string); len(got) != 0 {
		t.Errorf("完全越界应返回空，实际 %v", got)
	}
	// 非切片与 nil 不应 panic。
	if got := firstN(1, "字符串"); got != "字符串" {
		t.Errorf("非切片应原样返回，实际 %v", got)
	}
	if got := firstN(1, nil); got != nil {
		t.Errorf("nil 应返回 nil，实际 %v", got)
	}
}

// TestLengthAndEmpty 验证 len 与 default 的判定。
func TestLengthAndEmpty(t *testing.T) {
	t.Parallel()

	if got := length([]int{1, 2}); got != 2 {
		t.Errorf("length 切片 = %d", got)
	}
	if got := length("你好"); got != 6 {
		t.Errorf("length 字符串按字节 = %d，期望 6", got)
	}
	if got := length(nil); got != 0 {
		t.Errorf("length(nil) = %d", got)
	}
	if got := length(42); got != 0 {
		t.Errorf("length 非集合 = %d，期望 0", got)
	}

	if got := defaultValue("兜底", ""); got != "兜底" {
		t.Errorf("空串应取缺省值，实际 %v", got)
	}
	if got := defaultValue("兜底", "实值"); got != "实值" {
		t.Errorf("非空应保留原值，实际 %v", got)
	}
	if got := defaultValue(3, 0); got != 3 {
		t.Errorf("零值应取缺省值，实际 %v", got)
	}
	if got := defaultValue("兜底", []string{}); got != "兜底" {
		t.Errorf("空切片应取缺省值，实际 %v", got)
	}
}

// TestJsonifyEscapes 验证 jsonify 产出可安全内联的 JSON。
func TestJsonifyEscapes(t *testing.T) {
	t.Parallel()

	got, err := jsonify(map[string]string{"k": `</script>`})
	if err != nil {
		t.Fatalf("jsonify 报错: %v", err)
	}
	// encoding/json 默认把 < 与 > 转成 \u003c / \u003e，闭合标签不会提前结束脚本块。
	if strings.Contains(string(got), "</script>") {
		t.Errorf("输出未转义闭合标签: %s", got)
	}
}

// TestSafeHTMLIsExplicit 验证 safeHTML 确实绕过转义。
//
// 这是主题里唯一能绕过上下文转义的口子，必须是显式调用。
func TestSafeHTMLIsExplicit(t *testing.T) {
	t.Parallel()

	funcs := baseFuncs()
	tmpl := template.Must(template.New("t").Funcs(funcs).
		Parse(`{{ .C }}|{{ safeHTML .C }}`))
	var sb strings.Builder
	if err := tmpl.Execute(&sb, map[string]string{"C": "<b>粗</b>"}); err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	escaped, raw, found := strings.Cut(sb.String(), "|")
	if !found {
		t.Fatalf("输出格式异常: %q", sb.String())
	}
	if strings.Contains(escaped, "<b>") {
		t.Errorf("默认输出应被转义，实际 %q", escaped)
	}
	if raw != "<b>粗</b>" {
		t.Errorf("safeHTML 应原样输出，实际 %q", raw)
	}
}

// TestPlainifyStripsTags 验证 plainify 去标签。
func TestPlainifyStripsTags(t *testing.T) {
	t.Parallel()

	funcs := baseFuncs()
	plainify, ok := funcs["plainify"].(func(string) string)
	if !ok {
		t.Fatal("plainify 签名不符")
	}
	if got := plainify("<p>你好</p><p>世界</p>"); got != "你好 世界" {
		t.Errorf("plainify = %q", got)
	}
}

// TestHumanizeSince 验证相对时间的分档。
func TestHumanizeSince(t *testing.T) {
	t.Parallel()

	tests := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "刚刚"},
		{5 * time.Minute, "5 分钟前"},
		{3 * time.Hour, "3 小时前"},
		{50 * time.Hour, "2 天前"},
		{40 * 24 * time.Hour, "1 个月前"},
		{400 * 24 * time.Hour, "1 年前"},
	}
	for _, tt := range tests {
		if got := humanizeSince(tt.d); got != tt.want {
			t.Errorf("humanizeSince(%v) = %q，期望 %q", tt.d, got, tt.want)
		}
	}
}
