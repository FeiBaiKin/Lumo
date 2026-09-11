package search_test

import (
	"strings"
	"testing"

	"github.com/FeiBaiKin/lumo/internal/search"
)

func TestTokenize(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"中文按二元组切", "全文搜索", []string{"全文", "文搜", "搜索"}},
		{"单个汉字原样保留", "书", []string{"书"}},
		{"标点断段", "搜索，很难", []string{"搜索", "很难"}},
		{"拉丁词整体成词并转小写", "Hello Go", []string{"hello", "go"}},
		{"中英混排各自成段", "用 Go 写 CMS", []string{"用", "go", "写", "cms"}},
		{"数字保留", "PostgreSQL 17", []string{"postgresql", "17"}},
		{"中英相邻不粘连", "Go语言", []string{"go", "语言"}},
		{"空白与符号不产生词元", "  —— !!  ", nil},
		{"假名与谚文同样按二元组", "ひらがな", []string{"ひら", "らが", "がな"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := search.Tokenize(c.in)
			if len(got) != len(c.want) {
				t.Fatalf("Tokenize(%q) = %v，期望 %v", c.in, got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("Tokenize(%q) = %v，期望 %v", c.in, got, c.want)
				}
			}
		})
	}
}

// TestTokenizeLongWordDropped 确认超长串不进索引：正文里的 base64 与哈希只是噪音。
func TestTokenizeLongWordDropped(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("a", 65)
	if got := search.Tokenize(long); len(got) != 0 {
		t.Fatalf("超长串仍被切出词元：%v", got)
	}
	if got := search.Tokenize(strings.Repeat("a", 64)); len(got) != 1 {
		t.Fatalf("恰好到上限的串应当保留：%v", got)
	}
}

// TestTokenizeBounded 确认切词有上限，避免超出 tsvector 的 1 MB 限制。
func TestTokenizeBounded(t *testing.T) {
	t.Parallel()

	huge := strings.Repeat("中", 80000)
	if got := len(search.Tokenize(huge)); got > 60000 {
		t.Fatalf("词元数 = %d，超过上限", got)
	}
}

func TestTSQuery(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"多字中文用短语算子相连", "东京都", "('东京' <-> '京都')"},
		{"单字退化为前缀匹配", "书", "'书':*"},
		{"拉丁词直接成词", "golang", "'golang'"},
		{"段与段之间取交集", "用 Go 写", "'用':* & 'go' & '写':*"},
		{"混排", "Go语言入门", "'go' & ('语言' <-> '言入' <-> '入门')"},
		{"无可用词元时为空", "  ??  ", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := search.TSQuery(c.in); got != c.want {
				t.Errorf("TSQuery(%q) = %q，期望 %q", c.in, got, c.want)
			}
		})
	}
}

// TestTSQueryBounded 确认查询串有词元上限，超长输入不会撑出一条巨大的 tsquery。
func TestTSQueryBounded(t *testing.T) {
	t.Parallel()

	got := search.TSQuery(strings.Repeat("中文", 200))
	if strings.Count(got, "<->") > 64 {
		t.Fatalf("查询串词元数超过上限：%q", got)
	}
}
