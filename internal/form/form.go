package form

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"sync"
)

// Section 是表单里的一段，带标题与说明。
//
// 分节是纯呈现：它不影响值、不影响校验，只决定字段在页面上怎么分组。
// 设置项多起来以后，一长条没有分隔的表单会让人找不到东西。
type Section struct {
	Title       string
	Description string
	Fields      []*Field
}

// NewSection 构造一段表单。没被任何一段收下的字段不会被静默藏起来，
// 构建时会报错——声明了却不显示，是最难排查的一类问题。
func NewSection(title string, fields ...*Field) Section {
	return Section{Title: title, Fields: fields}
}

// Describe 给这一段加上说明文字。
func (s Section) Describe(desc string) Section {
	s.Description = desc
	return s
}

// Form 是一份完整的表单声明。
//
// 它有两个来源，产出同一种形态：
//   - New：Go 代码声明，模块与插件走这条；
//   - Parse：已有一份 JSON Schema 与缺省值，主题包的 settings.yaml 走这条。
//
// 无论哪条来源，条件依赖（x-show-if）的语义都一致：条件不成立的字段不显示、
// 不参与必填校验，值仍保留。
type Form struct {
	name string

	// DSL 来源：非空表示由 New 构造。
	sections []Section
	// 原始来源：由 Parse 填入。
	rawDoc  map[string]any
	rawDefs map[string]any

	mu       sync.Mutex
	compiled bool
	doc      map[string]any
	defs     map[string]any
	top      []*Field
	err      error
}

// New 构造一份表单声明。
//
// 刻意不返回 error：调用它的地方是模块的 Settings()，那个签名是固定形状的。
// 声明错误（字段没写标题、缺省值缺失、条件指向不存在的字段）记在表单上，
// 由 Build 在注册设置分组时一并抛出——那是模块第一次被检查的时刻，也是它该失败的地方。
func New(sections ...Section) *Form {
	return &Form{name: "form", sections: sections}
}

// Named 给表单起个名字，只用于错误信息与内部资源定位。
func (f *Form) Named(name string) *Form {
	f.name = name
	return f
}

// Sections 返回声明时的分节。
func (f *Form) Sections() []Section { return f.sections }

// Build 编译表单，返回 JSON Schema 文档与缺省值。
//
// 结果是缓存的：同一份声明可能被 Build 多次（契约测试、启动注册、接口输出各一次），
// 而声明在构造后不再变化。
func (f *Form) Build() (doc, defaults map[string]any, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.compileLocked()
	return f.doc, f.defs, f.err
}

// Doc 返回编译后的 Schema 文档；编译失败时为 nil，错误经 Build 取。
func (f *Form) Doc() map[string]any {
	doc, _, _ := f.Build()
	return doc
}

// DefaultValues 返回缺省值的副本。
func (f *Form) DefaultValues() map[string]any {
	_, defs, _ := f.Build()
	return maps.Clone(defs)
}

// Schema 返回 Schema 文档的 JSON 表示。
func (f *Form) Schema() json.RawMessage {
	raw, err := json.Marshal(f.Doc())
	if err != nil || f.Doc() == nil {
		return nil
	}
	return raw
}

// Defaults 返回缺省值的 JSON 表示。
func (f *Form) Defaults() json.RawMessage {
	_, defs, err := f.Build()
	if err != nil || defs == nil {
		return nil
	}
	raw, marshalErr := json.Marshal(defs)
	if marshalErr != nil {
		return nil
	}
	return raw
}

// Fields 返回顶层字段，顺序即声明顺序。
func (f *Form) Fields() []*Field {
	_, _, _ = f.Build()
	return f.top
}

// Field 按名字取顶层字段。
func (f *Form) Field(key string) (*Field, bool) {
	_, _, _ = f.Build()
	for _, field := range f.top {
		if field.key == key {
			return field, true
		}
	}
	return nil, false
}

// compileLocked 按来源分派编译。调用方须已持锁。
func (f *Form) compileLocked() {
	if f.compiled {
		return
	}
	f.compiled = true
	if f.rawDoc != nil {
		f.compileRaw()
		return
	}
	f.compileDSL()
}

// compileDSL 把 Go 声明编译成 JSON Schema 与缺省值。
//
// 声明错误在这里一次性汇总抛出：每个模块的 Settings() 都会被 settings 模块在启动时
// 逐个编译，写错一个字段应当在启动那一刻就说清楚是哪一个，而不是等有人点开那一页。
func (f *Form) compileDSL() {
	if len(f.sections) == 0 {
		f.err = fmt.Errorf("表单 %s 没有任何字段", f.name)
		return
	}

	props := make(map[string]any)
	defs := make(map[string]any)
	required := make([]any, 0)
	sections := make([]any, 0, len(f.sections))
	declared := make(map[string]bool)
	var problems []error

	for _, sec := range f.sections {
		names := make([]any, 0, len(sec.Fields))
		for _, field := range sec.Fields {
			if field == nil {
				problems = append(problems, fmt.Errorf("表单 %s 的分段 %q 里有一个 nil 字段", f.name, sec.Title))
				continue
			}
			if field.key == "" {
				problems = append(problems, fmt.Errorf("表单 %s 的分段 %q 里有一个未命名的字段", f.name, sec.Title))
				continue
			}
			if declared[field.key] {
				problems = append(problems, field.errf("重复声明"))
				continue
			}
			declared[field.key] = true

			problems = append(problems, checkLabels(field)...)
			if !field.spec.hasDefault {
				problems = append(problems, field.errf("缺少 Default；没有缺省值时，表单首次打开该字段是空的，而「空」与「未设置」在多数控件上分不出来"))
			}

			marshalled, err := field.marshal()
			if err != nil {
				problems = append(problems, err)
				continue
			}
			props[field.key] = marshalled
			defs[field.key] = field.spec.def
			if field.spec.required && len(field.spec.showIf) == 0 {
				required = append(required, field.key)
			}
			names = append(names, field.key)
			f.top = append(f.top, field)
		}

		entry := map[string]any{"title": sec.Title, "fields": names}
		if sec.Description != "" {
			entry["description"] = sec.Description
		}
		sections = append(sections, entry)
	}

	// 条件指向的字段必须存在。指向一个拼错的字段名，表现是「字段永远不显示」，
	// 而界面上不会有任何提示——只能在这里拦。
	for _, field := range f.top {
		problems = append(problems, checkConditions(field, declared)...)
	}

	if len(problems) > 0 {
		f.err = errors.Join(problems...)
		return
	}

	doc := map[string]any{
		"$schema":              "https://json-schema.org/draft/2020-12/schema",
		keyType:                "object",
		"additionalProperties": false,
		"properties":           props,
	}
	if len(required) > 0 {
		doc["required"] = required
	}
	// 无分段信息时前端按原来的平铺形态渲染，与主题包声明的老 Schema 兼容。
	if len(sections) > 0 {
		doc["x-sections"] = sections
	}

	// 过一遍 JSON：声明里写的是 Go 值（[]Condition、int、[]string），
	// 而 Doc() 的承诺是「返回的就是即将发出去的那份文档」。
	// 不过一遍的话，读它的人会拿到 []Condition 而不是数组，
	// 而客户端拿到的是数组——同一个名字下两种形态，迟早有人踩。
	normalized, err := normalizeJSON(doc)
	if err != nil {
		f.err = fmt.Errorf("表单 %s 的 Schema 无法序列化: %w", f.name, err)
		return
	}
	normalizedDefs, err := normalizeJSON(defs)
	if err != nil {
		f.err = fmt.Errorf("表单 %s 的缺省值无法序列化: %w", f.name, err)
		return
	}
	f.doc, _ = normalized.(map[string]any)
	f.defs, _ = normalizedDefs.(map[string]any)
}

// normalizeJSON 把含 Go 类型值的结构过一遍 JSON，转成纯 JSON 值树。
func normalizeJSON(value any) (any, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// checkLabels 递归检查字段与它的子字段都有显示名。
func checkLabels(field *Field) []error {
	var problems []error
	if field.spec.label == "" {
		problems = append(problems, field.errf("缺少 Label，表单上会显示原始字段名"))
	}
	for _, child := range field.spec.children {
		if child != nil {
			problems = append(problems, checkLabels(child)...)
		}
	}
	// 数组的 items 不是表单上的一个字段，只是条目类型，不要求它有显示名。
	return problems
}

// checkConditions 递归检查条件引用的字段都已声明。
func checkConditions(field *Field, declared map[string]bool) []error {
	var problems []error
	for _, cond := range field.spec.showIf {
		if cond.Field == field.key {
			problems = append(problems, field.errf("ShowIf 引用了自己"))
			continue
		}
		if !declared[cond.Field] {
			problems = append(problems, field.errf("ShowIf 指向不存在的字段 %q", cond.Field))
		}
	}
	for _, child := range field.spec.children {
		if child != nil {
			problems = append(problems, checkConditions(child, declared)...)
		}
	}
	return problems
}
