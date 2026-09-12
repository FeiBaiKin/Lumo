package form

import (
	"slices"
	"testing"
)

// storageForm 复刻 media 模块的 storage 分组：S3 的三项只在选了 S3 时才有意义，
// 且地址与桶名在那种情况下必填。这正是条件依赖要取代的那段 Check 代码。
func storageForm() *Form {
	return New(
		NewSection("存储",
			Select("driver", Opt("local", "本地"), Opt("s3", "S3 兼容")).Label("存储驱动").Default("local"),
			Text("s3Endpoint").Label("服务地址").Required().ShowIf(Eq("driver", "s3")).Default(""),
			Text("s3Bucket").Label("存储桶").Required().ShowIf(Eq("driver", "s3")).Default(""),
			Bool("s3UseSsl").Label("使用 HTTPS").ShowIf(Eq("driver", "s3")).Default(true),
		),
	)
}

func TestConditionalRequiredStaysOutOfSchemaRequired(t *testing.T) {
	doc, _ := build(t, storageForm())

	if got := requiredKeys(doc); len(got) != 0 {
		t.Fatalf("required = %v，条件必填的字段不该写进 Schema 的 required——"+
			"条件不成立时字段并不存在，写进去会让「隐藏所以没填」变成一次校验失败", got)
	}

	field := prop(t, doc, "s3Endpoint")
	conds, ok := field["x-show-if"].([]any)
	if !ok || len(conds) != 1 {
		t.Fatalf("x-show-if = %v", field["x-show-if"])
	}
	cond, _ := conds[0].(map[string]any)
	if cond["field"] != "driver" || cond["op"] != "eq" || cond["value"] != "s3" {
		t.Errorf("条件 = %v", cond)
	}
}

func TestMissingFollowsConditions(t *testing.T) {
	f := storageForm()
	_, _, err := f.Build()
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}

	cases := []struct {
		name   string
		values map[string]any
		want   []string
	}{
		{
			name:   "本地存储时不追究 S3 字段",
			values: map[string]any{"driver": "local"},
			want:   nil,
		},
		{
			name:   "选了 S3 但没填地址与桶名",
			values: map[string]any{"driver": "s3"},
			want:   []string{"s3Bucket", "s3Endpoint"},
		},
		{
			name:   "选了 S3 且填齐",
			values: map[string]any{"driver": "s3", "s3Endpoint": "https://s3.example.com", "s3Bucket": "media"},
			want:   nil,
		},
		{
			name:   "从 S3 切回本地后不再追究，填过的值也还留着",
			values: map[string]any{"driver": "local", "s3Endpoint": "", "s3Bucket": ""},
			want:   nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := f.Missing(tc.values)
			slices.Sort(got)
			if !slices.Equal(got, tc.want) {
				t.Errorf("Missing = %v，期望 %v", got, tc.want)
			}
		})
	}
}

func TestVisible(t *testing.T) {
	f := storageForm()
	if f.Visible("s3Bucket", map[string]any{"driver": "local"}) {
		t.Error("driver=local 时 s3Bucket 不该显示")
	}
	if !f.Visible("s3Bucket", map[string]any{"driver": "s3"}) {
		t.Error("driver=s3 时 s3Bucket 应当显示")
	}
	if !f.Visible("driver", map[string]any{}) {
		t.Error("没有条件的字段永远显示")
	}
	if f.Visible("nope", map[string]any{}) {
		t.Error("不存在的字段不显示")
	}
}

func TestConditionInAndEmpty(t *testing.T) {
	f := New(NewSection("s",
		Text("mode").Label("模式").Default("simple"),
		Text("advanced").Label("高级配置").Default("").ShowIf(In("mode", "advanced", "expert")),
		Text("fallback").Label("兜底").Default("").ShowIf(Empty("mode")),
		Text("filled").Label("已填").Default("").ShowIf(NotEmpty("mode")),
	))
	if _, _, err := f.Build(); err != nil {
		t.Fatalf("构建失败: %v", err)
	}

	if !f.Visible("advanced", map[string]any{"mode": "expert"}) {
		t.Error("In 应当匹配枚举里的任一项")
	}
	if f.Visible("advanced", map[string]any{"mode": "simple"}) {
		t.Error("In 不该匹配枚举外的值")
	}
	if !f.Visible("fallback", map[string]any{"mode": ""}) {
		t.Error("Empty 在字段为空时成立")
	}
	if !f.Visible("filled", map[string]any{"mode": "simple"}) {
		t.Error("NotEmpty 在字段有值时成立")
	}
}

// TestNestedRequiredUsesDottedPath 确认嵌套字段的缺失路径与接口错误明细的形态一致。
//
// 接口把校验错误报成 body.<点分路径>，前端据此落回字段。
// 路径拼错的表现是「保存失败但页面上没有任何红框」。
func TestNestedRequiredUsesDottedPath(t *testing.T) {
	f := New(NewSection("s",
		Group("s3", Text("bucket").Label("桶名").Required().Default("")).Label("S3").Default(map[string]any{}),
	))
	if _, _, err := f.Build(); err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	got := f.Missing(map[string]any{"s3": map[string]any{"bucket": ""}})
	if !slices.Equal(got, []string{"s3.bucket"}) {
		t.Errorf("Missing = %v，期望 [s3.bucket]", got)
	}
}

// TestConditionComparesAcrossNumberTypes 确认 int 与 float64 能比出相等。
//
// 声明里写的是 Go 的 int，值经 JSON 往返后是 float64。直接比较类型会永远为假，
// 表现是「条件永远不成立、字段永远不显示」，而界面上不会有任何报错。
func TestConditionComparesAcrossNumberTypes(t *testing.T) {
	cond := Eq("pageSize", 10)
	if !conditionHolds(cond, map[string]any{"pageSize": float64(10)}) {
		t.Error("int 10 与 float64 10 应当相等")
	}
	if conditionHolds(cond, map[string]any{"pageSize": float64(11)}) {
		t.Error("不同的数值不该相等")
	}
}
