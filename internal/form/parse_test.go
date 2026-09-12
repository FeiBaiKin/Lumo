package form

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"
)

// rawSchema 是一段手写的 Schema，形态照抄主题包 settings.yaml 里的内容。
// 主题设置只能这么声明——它们是第三方上传的资产，用不了 Go 的 DSL。
const rawSchema = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "showRelated": {"type": "boolean", "title": "显示相关文章"},
    "relatedCount": {"type": "integer", "title": "相关文章条数", "minimum": 1, "maximum": 12,
                     "x-show-if": {"field": "showRelated", "op": "eq", "value": true}},
    "indexMode": {"type": "string", "title": "首页形态", "enum": ["list", "excerpt"],
                  "enumNames": ["只列标题", "显示摘要"], "x-widget": "select", "format": "custom-thing"}
  },
  "required": ["showRelated", "relatedCount", "indexMode"],
  "x-sections": [
    {"title": "内容", "fields": ["indexMode", "showRelated", "relatedCount"]}
  ]
}`

const rawDefaults = `{"showRelated": true, "relatedCount": 3, "indexMode": "excerpt"}`

func TestParseReadsRawSchema(t *testing.T) {
	f, err := Parse("ink.content", json.RawMessage(rawSchema), json.RawMessage(rawDefaults))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}

	doc, defs, err := f.Build()
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}

	// 字段顺序取自 x-sections，而不是 map 的随机顺序：
	// 表单上「相关文章条数」必须紧跟在「显示相关文章」后面。
	keys := make([]string, 0, len(f.Fields()))
	for _, field := range f.Fields() {
		keys = append(keys, field.key)
	}
	if !slices.Equal(keys, []string{"indexMode", "showRelated", "relatedCount"}) {
		t.Errorf("字段顺序 = %v", keys)
	}

	// 认不出的键原样留着：主题作者手写的 format 不归我们管，丢掉它会让 Schema 悄悄变样。
	if got := prop(t, doc, "indexMode")["format"]; got != "custom-thing" {
		t.Errorf("format = %v，未知键应当原样保留", got)
	}
	if defs["relatedCount"] != float64(3) {
		t.Errorf("缺省值 = %v", defs)
	}
}

// TestParseNormalizesConditionalRequired 是这条路径上最要紧的一条。
//
// settings.yaml 的作者会理所当然地把「相关文章条数」写进 required——
// 它确实是必填的。但它只在「显示相关文章」打开时才存在。
// 不把它从 required 里移出来，站长就会遇到「关掉相关文章，设置反而存不进去」。
func TestParseNormalizesConditionalRequired(t *testing.T) {
	f, err := Parse("ink.content", json.RawMessage(rawSchema), json.RawMessage(rawDefaults))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	doc, _, _ := f.Build()

	if got := requiredKeys(doc); !slices.Equal(got, []string{"indexMode", "showRelated"}) {
		t.Errorf("required = %v，条件必填的 relatedCount 应当被移出", got)
	}

	// 移出 required 不等于不管它：条件成立时它仍然必填。
	visible := map[string]any{"indexMode": "excerpt", "showRelated": true, "relatedCount": nil}
	if got := f.Missing(visible); !slices.Equal(got, []string{"relatedCount"}) {
		t.Errorf("Missing = %v，条件成立时仍应追究", got)
	}
	// 条件不成立时不该追究——这正是把 required 移出来的目的。
	hidden := map[string]any{"indexMode": "excerpt", "showRelated": false}
	if got := f.Missing(hidden); len(got) != 0 {
		t.Errorf("Missing = %v，条件不成立时不该追究", got)
	}
}

func TestParseKeepsOriginalDocIntact(t *testing.T) {
	// 解析会改动 required 数组，但不能动别的地方——主题作者写的约束一条都不能少。
	f, err := Parse("x", json.RawMessage(rawSchema), json.RawMessage(rawDefaults))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	doc := f.Doc()

	var want map[string]any
	if err := json.Unmarshal([]byte(rawSchema), &want); err != nil {
		t.Fatal(err)
	}
	wantProps, _ := want["properties"].(map[string]any)
	gotProps := props(t, doc)
	for key, rawWant := range wantProps {
		wantProp, _ := rawWant.(map[string]any)
		gotProp, ok := gotProps[key].(map[string]any)
		if !ok {
			t.Errorf("字段 %q 丢了", key)
			continue
		}
		for field, wantValue := range wantProp {
			if !reflect.DeepEqual(gotProp[field], wantValue) {
				t.Errorf("字段 %q 的 %s 被改动了：%v → %v", key, field, wantValue, gotProp[field])
			}
		}
	}
}

func TestParseRejectsBadInput(t *testing.T) {
	cases := []struct {
		name     string
		schema   string
		defaults string
	}{
		{"Schema 为空", "", `{}`},
		{"Schema 不是对象", `[]`, `{}`},
		{"顶层 type 不是 object", `{"type": "array"}`, `{}`},
		{"缺省值不是对象", `{"type": "object", "properties": {}}`, `[]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Parse("x", json.RawMessage(tc.schema), json.RawMessage(tc.defaults)); err == nil {
				t.Error("期望报错，实际通过了")
			}
		})
	}
}

// TestParseWithoutSectionsFallsBackToStableOrder 确认没有 x-sections 时顺序仍然确定。
//
// 主题作者可能根本不写分段。那时顺序只影响观感，但「每次刷新顺序都不一样」
// 会让人以为数据在变。
func TestParseWithoutSectionsFallsBackToStableOrder(t *testing.T) {
	schema := `{"type": "object", "properties": {
		"zeta": {"type": "string", "title": "Z"},
		"alpha": {"type": "string", "title": "A"},
		"mid": {"type": "string", "title": "M"}}}`
	f, err := Parse("x", json.RawMessage(schema), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	keys := make([]string, 0, 3)
	for _, field := range f.Fields() {
		keys = append(keys, field.key)
	}
	if !slices.Equal(keys, []string{"alpha", "mid", "zeta"}) {
		t.Errorf("字段顺序 = %v，期望按名字稳定排序", keys)
	}
}
