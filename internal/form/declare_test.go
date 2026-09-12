package form

import (
	"strings"
	"testing"
)

// TestDeclarationErrorsAreReported 覆盖声明错误。
//
// 这些是「写错一行、页面打不开」的那一类问题。DSL 存在的全部理由就是把它们
// 从「有人点开那一页才发现」提前到「启动时就报出是哪个字段」，
// 所以每一条都必须有确定的报错，且报错里要带上字段名。
func TestDeclarationErrorsAreReported(t *testing.T) {
	cases := []struct {
		name  string
		form  *Form
		wants []string
	}{
		{
			name: "字段重复声明",
			form: New(NewSection("s",
				Text("title").Label("标题").Default(""),
				Text("title").Label("另一个标题").Default(""),
			)),
			wants: []string{"重复声明"},
		},
		{
			name: "缺少显示名",
			form: New(NewSection("s",
				Text("title").Default(""),
			)),
			wants: []string{"缺少 Label"},
		},
		{
			name: "缺少缺省值",
			form: New(NewSection("s",
				Text("title").Label("标题"),
			)),
			wants: []string{"缺少 Default"},
		},
		{
			name: "条件指向不存在的字段",
			form: New(NewSection("s",
				Text("host").Label("主机").Default("").ShowIf(Eq("enabld", true)),
			)),
			wants: []string{"ShowIf 指向不存在的字段"},
		},
		{
			name: "条件指向自己",
			form: New(NewSection("s",
				Text("host").Label("主机").Default("").ShowIf(Eq("host", "x")),
			)),
			wants: []string{"ShowIf 引用了自己"},
		},
		{
			name:  "没有任何字段",
			form:  New(),
			wants: []string{"没有任何字段"},
		},
		{
			name: "未命名的字段",
			form: New(NewSection("s",
				Text("").Label("标题").Default(""),
			)),
			wants: []string{"未命名的字段"},
		},
		{
			name: "嵌套字段缺显示名",
			form: New(NewSection("s",
				Group("size", Text("width")).Label("尺寸").Default(map[string]any{}),
			)),
			wants: []string{"缺少 Label"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := tc.form.Build()
			if err == nil {
				t.Fatal("期望构建失败，实际成功了")
			}
			for _, want := range tc.wants {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("错误信息里没有 %q：\n%v", want, err)
				}
			}
		})
	}
}

// TestMultipleProblemsAreReportedTogether 确认错误是一次性汇总而不是只报第一条。
//
// 逐条报的话，作者要改一处、重跑一次、再看到下一处，
// 十个字段的表单能来回十次。
func TestMultipleProblemsAreReportedTogether(t *testing.T) {
	f := New(NewSection("s",
		Text("a").Default(""),
		Text("b").Default(""),
	))
	_, _, err := f.Build()
	if err == nil {
		t.Fatal("期望构建失败")
	}
	if !strings.Contains(err.Error(), `"a"`) || !strings.Contains(err.Error(), `"b"`) {
		t.Errorf("两个字段的问题应一并报出：\n%v", err)
	}
}
