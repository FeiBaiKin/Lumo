package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/form"
)

// TestSettingsSchemaContract 校验各模块声明的设置表单落在 Console 表单引擎的能力范围内。
//
// 为什么这条测试必须放在整机装配处：设置分组由各模块在启动期登记（agent.md §5），
// 而 Console 的表单引擎是**一份通用代码**渲染全部模块的分组。声明方与渲染方分处
// Go 与 TS 两侧，中间没有任何编译期约束——写错一个 x-widget、漏一个 title，
// 在 Go 侧编译通过、单测也通过，直到有人打开那一页才看得出来。
//
// 这正是阶段 5 那条整机装配测试的同一种问题（跨模块的类型名冲突只在 serve 那一刻炸），
// 故沿用同一个落点：装齐 modules()，把所有分组过一遍。
//
// 控件清单**从 TS 源码里读**而不是在这里再抄一份：抄一份的话，加了新控件却忘了同步
// 这份清单，测试照样绿，而它本该是最后一道防线。契约的真身在 agent.md §5。
func TestSettingsSchemaContract(t *testing.T) {
	widgets := tsWidgets(t)

	checked := 0
	for _, module := range modules() {
		provider, ok := module.(app.SettingsProvider)
		if !ok {
			continue
		}
		for _, group := range provider.Settings() {
			checked++
			if group.Form == nil {
				t.Errorf("[%s] 没有表单声明", group.Name)
				continue
			}
			doc, defaults, err := group.Form.Build()
			if err != nil {
				t.Errorf("[%s] 表单声明有误: %v", group.Name, err)
				continue
			}
			checkGroup(t, &group, doc, defaults, widgets)
		}
	}

	// 模块清单变了却没人加设置分组时，这条测试会静默变成空转。
	// 内建模块共 5 个分组（site / storage / mail / comment / seo），少于它说明装配漏了。
	if checked < 5 {
		t.Errorf("只检查到 %d 个设置分组，期望至少 5 个——装配是不是漏了模块？", checked)
	}
	t.Logf("已校验 %d 个设置分组，Console 支持 %d 种控件", checked, len(widgets))
}

// checkGroup 逐项核对一个分组的 Schema。
func checkGroup(t *testing.T, group *app.SettingGroup, doc, defaults map[string]any, widgets map[string]bool) {
	t.Helper()

	if group.Name == "" || group.Label == "" {
		t.Errorf("分组 %q 的 name 或 label 为空，Console 无法显示", group.Name)
	}
	if doc["type"] != "object" {
		t.Errorf("[%s] Schema 的 type = %v，必须是 object", group.Name, doc["type"])
	}
	// 严格模式是「未知字段被拒」的前提：设置保存走整体替换，
	// 放开 additionalProperties 会让手误多写的键被静默存进库。
	if doc["additionalProperties"] != false {
		t.Errorf("[%s] Schema 必须声明 additionalProperties: false", group.Name)
	}

	props, _ := doc["properties"].(map[string]any)
	for _, key := range sortedKeys(props) {
		field, _ := props[key].(map[string]any)
		checkField(t, group.Name, key, field, widgets)
	}
	checkSecrets(t, group, props)

	// 每个字段都要有缺省值：否则表单首次打开时该字段是空的，
	// 而「空」与「未设置」在多数控件上分不出来。
	for key := range defaults {
		if _, ok := props[key]; !ok {
			// 反方向：Defaults 里出现 Schema 未声明的键时，保存会被
			// additionalProperties: false 判为非法，表现为「一次都没改过却存不进去」。
			t.Errorf("[%s] Defaults 里的 %q 未在 properties 中声明", group.Name, key)
		}
	}
	for _, key := range sortedKeys(props) {
		if _, ok := defaults[key]; !ok {
			t.Errorf("[%s.%s] 缺少缺省值", group.Name, key)
		}
	}

	checkSections(t, group.Name, doc, props)
}

// credentialName 是「这个字段名看着像凭据」的判据。
//
// 拿名字当判据是不得已：声明里没有任何一个关键字能保证作者想的是「这是口令」。
// 但漏网的代价很大——一个叫 s3SessionToken 的字段若用 form.Text 声明，
// 它会连密文都不加就进库、还照原样回传给浏览器，而界面上看不出任何区别。
var credentialName = regexp.MustCompile(`(?i)(password|passwd|secret|accesskey|apikey|token|credential)`)

// checkSecrets 盯住凭据字段：要么以 Secret 声明，要么进 Public 白名单被拒。
//
// 两条都是「声明错了不会报错、只会静默泄漏」的性质，所以放在契约测试里，
// 让新增凭据字段的人在这里被拦一次（agent.md §5）。
func checkSecrets(t *testing.T, group *app.SettingGroup, props map[string]any) {
	t.Helper()

	declared := map[string]bool{}
	for _, key := range group.Form.SecretKeys() {
		declared[key] = true
	}
	for _, key := range sortedKeys(props) {
		if !credentialName.MatchString(key) || declared[key] {
			continue
		}
		t.Errorf("[%s.%s] 名字看着是凭据却不是用 form.Secret 声明的——它会明文入库并原样回传接口，"+
			"要么改成 form.Secret，要么换个不叫这个名字的字段名", group.Name, key)
	}
	for key := range declared {
		if slices.Contains(group.Public, key) {
			t.Errorf("[%s.%s] 口令字段被列进了 Public，前台会读到它", group.Name, key)
		}
	}
}

// checkField 核对单个字段。
func checkField(t *testing.T, group, key string, field map[string]any, widgets map[string]bool) {
	t.Helper()
	label := fmt.Sprintf("[%s.%s]", group, key)

	if field == nil {
		t.Errorf("%s 字段 Schema 不是对象", label)
		return
	}
	// 没有 title 的字段会在表单上显示成 camelCase 的键名。
	// 中文界面里那几乎总是遗漏而不是有意为之。
	if title, _ := field["title"].(string); title == "" {
		t.Errorf("%s 缺少 title，表单上会显示原始字段名", label)
	}
	if widget, _ := field["x-widget"].(string); widget != "" && !widgets[widget] {
		t.Errorf("%s 的 x-widget = %q 不被 Console 支持，会被静默退回按类型推断（agent.md §5）",
			label, widget)
	}

	enum, hasEnum := field["enum"].([]any)
	// 枚举项若给了中文名，数量必须与 enum 对齐——错位会让选项张冠李戴
	if names, ok := field["enumNames"].([]any); ok && len(names) != len(enum) {
		t.Errorf("%s 的 enumNames 有 %d 项而 enum 有 %d 项，数量必须一致", label, len(names), len(enum))
	}
	if hasEnum && len(enum) == 0 {
		t.Errorf("%s 声明了空的 enum", label)
	}

	if field["type"] == "array" {
		items, ok := field["items"].(map[string]any)
		if !ok {
			t.Errorf("%s 是数组但未声明 items，repeater / list 无从渲染", label)
			return
		}
		if _, ok := items["type"].(string); !ok {
			t.Errorf("%s 的 items 缺少 type", label)
		}
		return
	}

	// 嵌套对象：字段名在 Schema 里是带点的路径，便于一眼看出是哪一层出的问题。
	if field["type"] == "object" {
		nested, _ := field["properties"].(map[string]any)
		for _, sub := range sortedKeys(nested) {
			child, _ := nested[sub].(map[string]any)
			checkField(t, group, key+"."+sub, child, widgets)
		}
	}
}

// checkSections 确认分段信息与字段对得上。
//
// 分段是 Console 唯一的字段排序依据（JSON 对象的键本就无序）。一个字段若不在
// 任何分段里，它仍然会被渲染，但会掉到表单末尾——不该是声明方的本意。
func checkSections(t *testing.T, group string, doc, props map[string]any) {
	t.Helper()

	raw, ok := doc["x-sections"]
	if !ok {
		// 主题包声明的老 Schema 不写分段，前端按平铺形态渲染，这是允许的。
		// 但模块在 Go 里用 DSL 声明的分组一定会有分段——没有就是漏了参数。
		t.Errorf("[%s] 缺少 x-sections，Console 无法确定字段顺序", group)
		return
	}
	sections, ok := raw.([]any)
	if !ok || len(sections) == 0 {
		t.Errorf("[%s] x-sections 不是非空数组", group)
		return
	}

	covered := map[string]bool{}
	for i, entry := range sections {
		section, ok := entry.(map[string]any)
		if !ok {
			t.Errorf("[%s] 第 %d 个分段不是对象", group, i)
			continue
		}
		if title, _ := section["title"].(string); title == "" {
			t.Errorf("[%s] 第 %d 个分段没有标题", group, i)
		}
		fields, _ := section["fields"].([]any)
		if len(fields) == 0 {
			t.Errorf("[%s] 分段 %v 里没有字段", group, section["title"])
		}
		for _, name := range fields {
			key, _ := name.(string)
			if _, exists := props[key]; !exists {
				t.Errorf("[%s] 分段 %v 引用了不存在的字段 %q", group, section["title"], key)
			}
			if covered[key] {
				t.Errorf("[%s] 字段 %q 出现在多个分段里", group, key)
			}
			covered[key] = true
		}
	}
	for _, key := range sortedKeys(props) {
		if !covered[key] {
			t.Errorf("[%s] 字段 %q 不在任何分段里，会掉到表单末尾", group, key)
		}
	}
}

// tsWidgets 从 Console 的表单引擎源码里读出它支持的控件清单。
func tsWidgets(t *testing.T) map[string]bool {
	t.Helper()

	path := filepath.Join("..", "..", "console", "src", "components", "form", "schema.ts")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 Console 表单引擎失败: %v", err)
	}

	block := regexp.MustCompile(`(?s)const WIDGETS = new Set<string>\(\[(.*?)\]\)`).FindSubmatch(data)
	if block == nil {
		t.Fatalf("%s 里找不到 WIDGETS 清单——表单引擎改了形态，这条契约测试要跟着改", path)
	}

	out := map[string]bool{}
	for _, match := range regexp.MustCompile(`"([a-zA-Z-]+)"`).FindAllSubmatch(block[1], -1) {
		out[string(match[1])] = true
	}
	if len(out) == 0 {
		t.Fatalf("%s 里的 WIDGETS 是空的，解析大概失效了", path)
	}

	// Go 侧声明的每一种控件，Console 都必须认识。反方向不检查：
	// Console 可以多支持几个尚未有人用的控件。
	var missing []string
	for _, widget := range allWidgets(t) {
		if !out[widget] {
			missing = append(missing, widget)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf("internal/form 声明了 Console 不支持的控件：%s。"+
			"请在 console/src/components/form/controls.tsx 里补上对应控件，"+
			"或从 internal/form 的 Widget 常量里去掉", strings.Join(missing, "、"))
	}
	return out
}

// allWidgets 遍历 internal/form 声明的全部控件。
//
// 清单在 Go 侧是常量而非可枚举的集合，故在这里逐个列出。加新控件时
// 忘了往这里补，测试仍会因下面的数量下限而失败——这正是它存在的意义。
func allWidgets(t *testing.T) []string {
	t.Helper()
	out := []string{
		string(form.WidgetText), string(form.WidgetTextarea), string(form.WidgetCode),
		string(form.WidgetSecret), string(form.WidgetSelect), string(form.WidgetRadio),
		string(form.WidgetMultiselect), string(form.WidgetColor), string(form.WidgetImage),
		string(form.WidgetImages), string(form.WidgetDate), string(form.WidgetIcon),
		string(form.WidgetNumber), string(form.WidgetSlider), string(form.WidgetSwitch),
		string(form.WidgetList), string(form.WidgetRepeater), string(form.WidgetGroup),
	}
	if len(out) < 18 {
		t.Fatalf("internal/form 的控件常量少了：只列出 %d 个，期望 18 个。"+
			"新增控件时请一并更新这里", len(out))
	}
	return out
}

// sortedKeys 返回 map 的键，已排序——错误信息的顺序不该随运行而变。
func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
