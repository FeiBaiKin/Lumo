package main

import (
	"encoding/json"
	"testing"

	"github.com/FeiBaiKin/lumo/internal/app"
)

// TestSettingsSchemaContract 校验各模块声明的设置 Schema 落在 Console 表单引擎的能力范围内。
//
// 为什么这条测试必须放在整机装配处：设置分组由各模块在启动期登记（agent.md §5），
// 而 Console 的表单引擎是**一份通用代码**渲染全部模块的分组。声明方与渲染方分处
// Go 与 TS 两侧，中间没有任何编译期约束——写错一个 x-widget、漏一个 title，
// 在 Go 侧编译通过、单测也通过，直到有人打开那一页才看得出来。
//
// 这正是阶段 5 那条整机装配测试的同一种问题（跨模块的类型名冲突只在 serve 那一刻炸），
// 故沿用同一个落点：装齐 modules()，把所有分组过一遍。
//
// 契约的真身在 agent.md §5；此处逐条落地。
func TestSettingsSchemaContract(t *testing.T) {
	// Console 表单引擎支持的 widget（console/src/components/form/schema.ts 的 WIDGETS）。
	// 不在此列的 x-widget 会被引擎**静默退回**按类型推断——
	// 功能不会崩，但作者写的意图没生效，故在声明侧就拦下。
	supportedWidgets := map[string]bool{
		"text": true, "textarea": true, "code": true, "select": true,
		"color": true, "image": true, "number": true, "switch": true,
		"repeater": true, "list": true, "group": true,
	}
	supportedTypes := map[string]bool{
		"string": true, "integer": true, "number": true,
		"boolean": true, "object": true, "array": true,
	}

	checked := 0
	for _, module := range modules() {
		provider, ok := module.(app.SettingsProvider)
		if !ok {
			continue
		}
		for _, group := range provider.Settings() {
			checked++
			var schema struct {
				Type                 string                     `json:"type"`
				AdditionalProperties *bool                      `json:"additionalProperties"`
				Properties           map[string]json.RawMessage `json:"properties"`
			}
			if err := json.Unmarshal(group.Schema, &schema); err != nil {
				t.Errorf("[%s] Schema 不是合法 JSON: %v", group.Name, err)
				continue
			}
			if schema.Type != "object" {
				t.Errorf("[%s] Schema 的 type = %q，必须是 object", group.Name, schema.Type)
			}
			// 严格模式是「未知字段被拒」的前提：设置保存走整体替换，
			// 放开 additionalProperties 会让手误多写的键被静默存进库。
			if schema.AdditionalProperties == nil || *schema.AdditionalProperties {
				t.Errorf("[%s] Schema 必须声明 additionalProperties: false", group.Name)
			}

			defaults := map[string]any{}
			if err := json.Unmarshal(group.Defaults, &defaults); err != nil {
				t.Errorf("[%s] Defaults 不是合法 JSON: %v", group.Name, err)
				continue
			}
			for key, raw := range schema.Properties {
				var field struct {
					Type       string          `json:"type"`
					Title      string          `json:"title"`
					Widget     string          `json:"x-widget"`
					Enum       []any           `json:"enum"`
					Items      json.RawMessage `json:"items"`
					Properties map[string]any  `json:"properties"`
					EnumNames  []string        `json:"enumNames"`
				}
				if err := json.Unmarshal(raw, &field); err != nil {
					t.Errorf("[%s.%s] 字段 Schema 不是合法 JSON: %v", group.Name, key, err)
					continue
				}

				if field.Type != "" && !supportedTypes[field.Type] {
					t.Errorf("[%s.%s] type = %q 不被表单引擎支持", group.Name, key, field.Type)
				}
				if field.Widget != "" && !supportedWidgets[field.Widget] {
					t.Errorf("[%s.%s] x-widget = %q 不在引擎支持的集合里，"+
						"会被静默退回按类型推断（agent.md §5）", group.Name, key, field.Widget)
				}
				// 没有 title 的字段会在表单上显示成 camelCase 的键名。
				// 中文界面里那几乎总是遗漏而不是有意为之。
				if field.Title == "" {
					t.Errorf("[%s.%s] 缺少 title，表单上会显示原始字段名", group.Name, key)
				}
				// 数组必须声明 items，否则 repeater / list 无从渲染
				if field.Type == "array" && len(field.Items) == 0 {
					t.Errorf("[%s.%s] 是数组但未声明 items", group.Name, key)
				}
				// 枚举项若给了中文名，数量必须与 enum 对齐——错位会让选项张冠李戴
				if len(field.EnumNames) > 0 && len(field.EnumNames) != len(field.Enum) {
					t.Errorf("[%s.%s] enumNames 有 %d 项而 enum 有 %d 项，数量必须一致",
						group.Name, key, len(field.EnumNames), len(field.Enum))
				}
				// 每个字段都要有缺省值：否则表单首次打开时该字段是空的，
				// 而「空」与「未设置」在多数控件上分不出来。
				if _, ok := defaults[key]; !ok {
					t.Errorf("[%s.%s] Defaults 里没有这个字段的缺省值", group.Name, key)
				}
			}

			// 反方向：Defaults 里不该出现 Schema 未声明的键——
			// 保存时 additionalProperties: false 会直接把它判为非法，
			// 表现为「一次都没改过却存不进去」。
			for key := range defaults {
				if _, ok := schema.Properties[key]; !ok {
					t.Errorf("[%s] Defaults 里的 %q 未在 properties 中声明", group.Name, key)
				}
			}

			// 分组本身的元信息：Console 的标签栏要用 label 显示
			if group.Name == "" || group.Label == "" {
				t.Errorf("分组 %q 的 name 或 label 为空，Console 无法显示", group.Name)
			}
		}
	}

	// 模块清单变了却没人加设置分组时，这条测试会静默变成空转。
	// 内建模块共 5 个分组（site / storage / mail / comment / seo），少于它说明装配漏了。
	if checked < 5 {
		t.Errorf("只检查到 %d 个设置分组，期望至少 5 个——装配是不是漏了模块？", checked)
	}
	t.Logf("已校验 %d 个设置分组", checked)
}
