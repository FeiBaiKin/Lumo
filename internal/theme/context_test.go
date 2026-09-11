package theme

import (
	"testing"

	"github.com/FeiBaiKin/lumo/internal/taxonomy"
)

// 供测试共用的分类与标签。
var (
	testCategory = taxonomy.Category{ID: 1, Name: "技术", Slug: "tech"}
	testTag      = taxonomy.Tag{ID: 1, Name: "Go", Slug: "go", Color: "#00ADD8"}
)

// TestNewPagination 验证翻页信息的计算。
func TestNewPagination(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		page, size     int
		total          int
		wantTotalPages int
		wantPrev       bool
		wantNext       bool
		wantNextURL    string
	}{
		{name: "空列表也有一页", page: 1, size: 10, total: 0, wantTotalPages: 1},
		{name: "不足一页", page: 1, size: 10, total: 3, wantTotalPages: 1},
		{name: "整除", page: 1, size: 10, total: 20, wantTotalPages: 2,
			wantNext: true, wantNextURL: "/?page=2"},
		{name: "有余数", page: 2, size: 10, total: 21, wantTotalPages: 3,
			wantPrev: true, wantNext: true, wantNextURL: "/?page=3"},
		{name: "最后一页", page: 3, size: 10, total: 21, wantTotalPages: 3, wantPrev: true},
		{name: "非法页码归一", page: 0, size: 10, total: 5, wantTotalPages: 1},
		{name: "非法页长归一", page: 1, size: 0, total: 5, wantTotalPages: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := newPagination(tt.page, tt.size, tt.total, "/")
			if p.TotalPages != tt.wantTotalPages {
				t.Errorf("TotalPages = %d，期望 %d", p.TotalPages, tt.wantTotalPages)
			}
			if p.HasPrev != tt.wantPrev {
				t.Errorf("HasPrev = %v，期望 %v", p.HasPrev, tt.wantPrev)
			}
			if p.HasNext != tt.wantNext {
				t.Errorf("HasNext = %v，期望 %v", p.HasNext, tt.wantNext)
			}
			if tt.wantNextURL != "" && p.NextURL != tt.wantNextURL {
				t.Errorf("NextURL = %q，期望 %q", p.NextURL, tt.wantNextURL)
			}
		})
	}
}

// TestPageURLFirstPageHasNoQuery 验证第一页不带 page 参数。
//
// /posts 与 /posts?page=1 是同一个页面，两个地址会稀释搜索引擎的权重。
func TestPageURLFirstPageHasNoQuery(t *testing.T) {
	t.Parallel()

	if got := pageURL("/categories/go", 1); got != "/categories/go" {
		t.Errorf("第一页 = %q，期望不带查询参数", got)
	}
	if got := pageURL("/categories/go", 2); got != "/categories/go?page=2" {
		t.Errorf("第二页 = %q", got)
	}
	// 基地址已带查询串时用 & 续接，而不是再来一个 ?。
	if got := pageURL("/search?q=x", 2); got != "/search?q=x&page=2" {
		t.Errorf("带查询串的基地址 = %q", got)
	}
}

// TestPaginationPages 验证页码序列与省略号。
func TestPaginationPages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		page       int
		totalPages int
		want       []int
	}{
		{"不足窗口不省略", 1, 5, []int{1, 2, 3, 4, 5}},
		{"恰好七页不省略", 4, 7, []int{1, 2, 3, 4, 5, 6, 7}},
		{"开头省略右侧", 1, 20, []int{1, 2, 3, 0, 20}},
		{"中间两侧都省略", 10, 20, []int{1, 0, 8, 9, 10, 11, 12, 0, 20}},
		{"结尾省略左侧", 20, 20, []int{1, 0, 18, 19, 20}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := Pagination{Page: tt.page, TotalPages: tt.totalPages}
			got := p.Pages()
			if len(got) != len(tt.want) {
				t.Fatalf("页码 = %v，期望 %v", got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("页码 = %v，期望 %v", got, tt.want)
				}
			}
		})
	}
}

// TestContentPath 验证内容路径与 seo / menu 模块的约定一致。
func TestContentPath(t *testing.T) {
	t.Parallel()

	if got := ContentPath("post", "hello"); got != "/posts/hello" {
		t.Errorf("文章路径 = %q", got)
	}
	// 独立页面直接挂在根路径下。
	if got := ContentPath("page", "about"); got != "/about" {
		t.Errorf("页面路径 = %q", got)
	}
}

// TestWithWeights 验证标签云的权重分档。
func TestWithWeights(t *testing.T) {
	t.Parallel()

	// 数量差距悬殊时仍应铺满 1–5 档，而不是全挤在最小号。
	tags := []TermView{{Count: 500}, {Count: 100}, {Count: 50}, {Count: 3}, {Count: 1}}
	got := withWeights(tags)
	if got[0].Weight != 5 {
		t.Errorf("最热标签权重 = %d，期望 5", got[0].Weight)
	}
	if got[len(got)-1].Weight != 1 {
		t.Errorf("最冷标签权重 = %d，期望 1", got[len(got)-1].Weight)
	}
	for _, tag := range got {
		if tag.Weight < 1 || tag.Weight > 5 {
			t.Errorf("权重 %d 越界", tag.Weight)
		}
	}

	// 全部数量相同时取中间档。
	same := withWeights([]TermView{{Count: 7}, {Count: 7}})
	for _, tag := range same {
		if tag.Weight != 3 {
			t.Errorf("同数量时权重 = %d，期望 3", tag.Weight)
		}
	}
	// 空列表不应 panic。
	if got := withWeights(nil); len(got) != 0 {
		t.Errorf("空列表 = %v", got)
	}
}

// TestAbsoluteURL 验证未配置站点地址时不编造绝对地址。
//
// canonical 指向一个错误的域名，比没有 canonical 更糟。
func TestAbsoluteURL(t *testing.T) {
	t.Parallel()

	if got := absoluteURL("", "/posts/x"); got != "/posts/x" {
		t.Errorf("无站点地址时 = %q，期望原样返回相对路径", got)
	}
	if got := absoluteURL("https://a.com", "/posts/x"); got != "https://a.com/posts/x" {
		t.Errorf("拼接结果 = %q", got)
	}
	if got := absoluteURL("https://a.com/", "/posts/x"); got != "https://a.com/posts/x" {
		t.Errorf("尾斜杠应被去掉，实际 %q", got)
	}
}

// TestItoa 验证本地整数转字符串的正确性。
func TestItoa(t *testing.T) {
	t.Parallel()

	tests := map[int]string{0: "0", 1: "1", 42: "42", 1000: "1000", -7: "-7"}
	for in, want := range tests {
		if got := itoa(in); got != want {
			t.Errorf("itoa(%d) = %q，期望 %q", in, got, want)
		}
	}
}
