package slug

import (
	"strings"
	"testing"
)

func TestMake(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"英文空格", "Hello World", "hello-world"},
		{"保留中文", "  Go 1.26 发布了! ", "go-1-26-发布了"},
		{"连续标点折叠", "C++ / Rust", "c-rust"},
		{"全角归一化", "ＦＵＬＬ　ＷＩＤＴＨ", "full-width"},
		{"带变音符号", "Ünïcödé Café", "ünïcödé-café"},
		{"下划线保留", "snake_case_name", "snake_case_name"},
		{"斜杠与问号", "a/b?c#d", "a-b-c-d"},
		{"纯标点无法生成", "!!! ??? ...", ""},
		{"表情符号无法生成", "🎉🎉", ""},
		{"中英混排", "从 Halo 迁移到 Lumo", "从-halo-迁移到-lumo"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := Make(tt.in); got != tt.want {
				t.Errorf("Make(%q) = %q，期望 %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestMakeTruncates(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("字", 200)
	got := Make(long)
	if n := len([]rune(got)); n != MaxLength {
		t.Errorf("截断后长度 = %d，期望 %d", n, MaxLength)
	}

	// 截断点恰好落在分隔符上时，尾部连字符应被去掉。
	pieces := make([]string, 0, MaxLength)
	for range MaxLength {
		pieces = append(pieces, "a")
	}
	withDash := strings.Join(pieces, "-") + "-tail"
	if got := Make(withDash); strings.HasSuffix(got, "-") {
		t.Errorf("截断后不应以连字符结尾: %q", got)
	}
}

func TestPinyin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want string
	}{
		{"你好 World", "ni-hao-world"},
		{"中文标题", "zhong-wen-biao-ti"},
		{"Go 语言入门", "go-yu-yan-ru-men"},
		{"no chinese", "no-chinese"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			if got := Pinyin(tt.in); got != tt.want {
				t.Errorf("Pinyin(%q) = %q，期望 %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestValid(t *testing.T) {
	t.Parallel()

	valid := []string{"hello-world", "技术", "go-1-26", "snake_case"}
	for _, s := range valid {
		if !Valid(s) {
			t.Errorf("Valid(%q) 应为 true", s)
		}
	}
	invalid := []string{"", "Hello", "a b", "a/b", "-lead", "trail-", strings.Repeat("a", MaxLength+1)}
	for _, s := range invalid {
		if Valid(s) {
			t.Errorf("Valid(%q) 应为 false", s)
		}
	}
}
