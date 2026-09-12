package form

import (
	"slices"
	"testing"
)

// build 是测试里最常用的动作：编译并断言无错。
func build(t *testing.T, f *Form) (doc, defaults map[string]any) {
	t.Helper()
	doc, defs, err := f.Build()
	if err != nil {
		t.Fatalf("构建表单失败: %v", err)
	}
	return doc, defs
}

// props 取 Schema 的 properties。
func props(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	out, ok := doc["properties"].(map[string]any)
	if !ok {
		t.Fatalf("Schema 缺少 properties")
	}
	return out
}

// prop 取某个字段的 Schema 片段。
func prop(t *testing.T, doc map[string]any, key string) map[string]any {
	t.Helper()
	raw, ok := props(t, doc)[key]
	if !ok {
		t.Fatalf("Schema 里没有字段 %q", key)
	}
	out, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("字段 %q 不是对象", key)
	}
	return out
}

// requiredKeys 取 Schema 的 required 数组。
func requiredKeys(doc map[string]any) []string {
	raw, _ := doc["required"].([]any)
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		key, _ := item.(string)
		out = append(out, key)
	}
	slices.Sort(out)
	return out
}

func TestBuildProducesSchemaAndDefaults(t *testing.T) {
	f := New(
		NewSection("基础",
			Text("title").Label("站点标题").Required().MaxLen(128).Default("Lumo"),
			Text("subtitle").Label("副标题").Default(""),
		).Describe("站点的名字"),
	)
	doc, defs := build(t, f)

	if doc["type"] != "object" {
		t.Errorf("顶层 type = %v，期望 object", doc["type"])
	}
	if doc["additionalProperties"] != false {
		t.Error("顶层必须声明 additionalProperties: false，否则手误多写的键会被静默存进库")
	}
	if got := doc["$schema"]; got != "https://json-schema.org/draft/2020-12/schema" {
		t.Errorf("$schema = %v", got)
	}

	title := prop(t, doc, "title")
	if title["type"] != "string" || title["title"] != "站点标题" {
		t.Errorf("title 字段 = %v", title)
	}
	// Doc() 返回的是纯 JSON 值树（见 compileDSL 末尾的说明），故数值是 float64。
	// 这正是客户端会看到的那一份。
	if title["maxLength"] != float64(128) {
		t.Errorf("maxLength = %v (%T)，期望 128", title["maxLength"], title["maxLength"])
	}

	if !slices.Equal(requiredKeys(doc), []string{"title"}) {
		t.Errorf("required = %v，期望只有 title", requiredKeys(doc))
	}
	if defs["title"] != "Lumo" || defs["subtitle"] != "" {
		t.Errorf("缺省值 = %v", defs)
	}

	// 分段信息要原样带给渲染方
	sections, ok := doc["x-sections"].([]any)
	if !ok || len(sections) != 1 {
		t.Fatalf("x-sections = %v", doc["x-sections"])
	}
	first, _ := sections[0].(map[string]any)
	if first["title"] != "基础" || first["description"] != "站点的名字" {
		t.Errorf("分段 = %v", first)
	}
	if fields, _ := first["fields"].([]any); !slices.Equal(toStrings(fields), []string{"title", "subtitle"}) {
		t.Errorf("分段的字段 = %v", first["fields"])
	}
}

func TestWidgetOnlyWrittenWhenItDiffersFromInference(t *testing.T) {
	f := New(NewSection("全部",
		Text("plain").Label("普通文本").Default(""),
		Select("mode", Opt("a", "甲"), Opt("b", "乙")).Label("模式").Default("a"),
		Slider("width").Label("栏宽").Min(1).Max(48).Default(36),
		Color("seal").Label("印色").Default("#000000"),
		MultiSelect("tags", Opt("x", "X")).Label("标签").Default([]any{}),
		Radio("layout", Opt("l", "列表"), Opt("g", "网格")).Label("版式").Default("l"),
	))
	doc, _ := build(t, f)

	cases := map[string]string{
		// 推断结果与声明一致 → 不写 x-widget，Schema 才不会被噪音填满
		"plain": "",
		"mode":  "",
		// 推断不出这些控件 → 必须写
		"width":  "slider",
		"seal":   "color",
		"tags":   "multiselect",
		"layout": "radio",
	}
	for key, want := range cases {
		got, _ := prop(t, doc, key)["x-widget"].(string)
		if got != want {
			t.Errorf("字段 %q 的 x-widget = %q，期望 %q", key, got, want)
		}
	}
}

func TestEnumGoesToEnumNames(t *testing.T) {
	f := New(NewSection("s",
		Select("driver", Opt("local", "本地"), Opt("s3", "S3 兼容")).Label("驱动").Default("local"),
	))
	doc, _ := build(t, f)
	field := prop(t, doc, "driver")

	values, _ := field["enum"].([]any)
	names, _ := field["enumNames"].([]any)
	if !slices.Equal(toStrings(values), []string{"local", "s3"}) {
		t.Errorf("enum = %v", values)
	}
	if !slices.Equal(toStrings(names), []string{"本地", "S3 兼容"}) {
		t.Errorf("enumNames = %v", names)
	}
}

func TestMultiSelectPutsEnumOnItems(t *testing.T) {
	f := New(NewSection("s",
		MultiSelect("kinds", Opt("post", "文章"), Opt("page", "页面")).Label("类型").Default([]any{}),
	))
	doc, _ := build(t, f)
	field := prop(t, doc, "kinds")

	if _, has := field["enum"]; has {
		t.Error("多选的取值是数组，顶层不该有 enum——那会表达成「数组等于其中一个字符串」")
	}
	items, _ := field["items"].(map[string]any)
	if items["type"] != "string" {
		t.Errorf("items.type = %v", items["type"])
	}
	if values, _ := items["enum"].([]any); !slices.Equal(toStrings(values), []string{"post", "page"}) {
		t.Errorf("items.enum = %v", items["enum"])
	}
}

// toStrings 把 []any 转成 []string，便于比较。
func toStrings(items []any) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		text, _ := item.(string)
		out = append(out, text)
	}
	return out
}
