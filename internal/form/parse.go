package form

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Parse 从一份已有的 JSON Schema 与缺省值构造表单。
//
// 这条路径供主题包的 settings.yaml 使用：主题设置是第三方上传的资产，
// 声明写在 YAML 里，不可能用 Go 的 DSL 表达。插件声明自己的设置时也走这里。
//
// 之所以不直接把原始 Schema 交给校验器了事：条件依赖需要结构化的信息才能判定
// 「这个字段此刻是否可见、是否该参与必填校验」，而那是 x-show-if 在 JSON 里的形态
// 看不出来的。解析一次、两边共用同一套语义，主题设置与站点设置才不会分家。
//
// 解析会规范化 required：带 x-show-if 的字段从 Schema 的 required 数组里移出，
// 改由 RequiredMissing 在条件成立时判定。不移出的话，「条件不成立所以字段没显示、
// 因此没填」会被校验器判成失败——而这正是条件依赖要解决的问题。
func Parse(name string, schema, defaults json.RawMessage) (*Form, error) {
	if len(schema) == 0 {
		return nil, fmt.Errorf("表单 %s 缺少 Schema", name)
	}
	var doc map[string]any
	if err := json.Unmarshal(schema, &doc); err != nil {
		return nil, fmt.Errorf("表单 %s 的 Schema 不是合法 JSON 对象: %w", name, err)
	}
	if typ, _ := doc[keyType].(string); typ != "object" {
		return nil, fmt.Errorf("表单 %s 的 Schema 顶层 type 须为 object", name)
	}

	defs := map[string]any{}
	if len(defaults) > 0 {
		if err := json.Unmarshal(defaults, &defs); err != nil {
			return nil, fmt.Errorf("表单 %s 的缺省值不是合法 JSON 对象: %w", name, err)
		}
	}

	f := &Form{name: name, rawDoc: doc, rawDefs: defs}
	if _, _, err := f.Build(); err != nil {
		return nil, err
	}
	return f, nil
}

// compileRaw 从原始 Schema 重建字段索引并规范化 required。
func (f *Form) compileRaw() {
	props, ok := f.rawDoc["properties"].(map[string]any)
	if !ok {
		f.err = fmt.Errorf("表单 %s 的 Schema 缺少 properties", f.name)
		return
	}

	// required 在 Schema 顶层声明，字段自己看不见它，故在这一层补上。
	// 条件必填的字段随后会被 normalizeRequired 从数组里移出，但这里的标记要留着：
	// 判定「此刻该不该填」用的正是它。
	required := map[string]bool{}
	if raw, ok := f.rawDoc["required"].([]any); ok {
		for _, item := range raw {
			key, _ := item.(string)
			required[key] = true
		}
	}

	var problems []error
	for key, raw := range props {
		prop, ok := raw.(map[string]any)
		if !ok {
			problems = append(problems, fmt.Errorf("表单 %s 的字段 %q 不是对象", f.name, key))
			continue
		}
		field, err := parseField(key, prop)
		if err != nil {
			problems = append(problems, err)
			continue
		}
		field.spec.required = required[key]
		f.top = append(f.top, field)
	}
	if len(problems) > 0 {
		f.err = errors.Join(problems...)
		return
	}

	// 按 properties 的键序排列不方便（map 无序），改用 required 与 x-sections 的声明顺序。
	f.top = orderFields(f.top, f.rawDoc)
	f.doc = f.rawDoc
	f.defs = f.rawDefs
	normalizeRequired(f.rawDoc, f.top)
}

// orderFields 让字段顺序跟随 x-sections 的声明顺序，没有分段信息时保持原样。
//
// 顺序只在 x-sections 里表达了：JSON 对象的键本就无序，而表单的字段顺序
// 恰恰是声明方要控制的东西，故必须借分段把它记下来。
func orderFields(fields []*Field, doc map[string]any) []*Field {
	sections, ok := doc["x-sections"].([]any)
	if !ok {
		return sortByName(fields)
	}
	byKey := make(map[string]*Field, len(fields))
	for _, field := range fields {
		byKey[field.key] = field
	}
	out := make([]*Field, 0, len(fields))
	seen := make(map[string]bool, len(fields))
	for _, raw := range sections {
		section, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		names, _ := section["fields"].([]any)
		for _, name := range names {
			key, _ := name.(string)
			field, ok := byKey[key]
			if !ok || seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, field)
		}
	}
	// 没被任何分段收下的字段仍然保留，放在末尾：主题作者可能忘了更新分段，
	// 少一个字段比顺序难看严重得多。
	for _, field := range sortByName(fields) {
		if !seen[field.key] {
			out = append(out, field)
		}
	}
	return out
}

// sortByName 按字段名排序，给没有声明顺序可依据的场景一个确定的顺序。
func sortByName(fields []*Field) []*Field {
	out := make([]*Field, len(fields))
	copy(out, fields)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].key < out[j-1].key; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// normalizeRequired 把「条件必填」的字段移出 Schema 的 required 数组。
func normalizeRequired(doc map[string]any, fields []*Field) {
	raw, ok := doc["required"].([]any)
	if !ok {
		return
	}
	conditional := map[string]bool{}
	for _, field := range fields {
		if len(field.spec.showIf) > 0 && field.spec.required {
			conditional[field.key] = true
		}
	}
	if len(conditional) == 0 {
		return
	}
	kept := make([]any, 0, len(raw))
	for _, item := range raw {
		key, _ := item.(string)
		if conditional[key] {
			continue
		}
		kept = append(kept, item)
	}
	if len(kept) == 0 {
		delete(doc, "required")
		return
	}
	doc["required"] = kept
}

// parseField 把一个 JSON Schema 属性还原成字段声明。
//
// 只认本包 marshal 会写出的那些键，外加主题作者可能手写的等价写法。
// 认不出的键原样留在 Schema 里不影响校验，故不报错。
func parseField(key string, raw map[string]any) (*Field, error) {
	typ, _ := raw[keyType].(string)
	field := &Field{key: key}
	field.spec.typ = Type(typ)

	if w, ok := raw["x-widget"].(string); ok {
		field.spec.widget = Widget(w)
	}
	if s, ok := raw["title"].(string); ok {
		field.spec.label = s
	}
	if s, ok := raw["description"].(string); ok {
		field.spec.help = s
	}
	if s, ok := raw["x-placeholder"].(string); ok {
		field.spec.placeholder = s
	}
	if s, ok := raw["x-unit"].(string); ok {
		field.spec.unit = s
	}
	if s, ok := raw["x-item-label"].(string); ok {
		field.spec.itemLabel = s
	}
	if n, ok := raw["x-rows"].(float64); ok {
		field.spec.rows = int(n)
	}
	if s, ok := raw["pattern"].(string); ok {
		field.spec.pattern = s
	}
	if n, ok := raw["minimum"].(float64); ok {
		field.spec.minimum = &n
	}
	if n, ok := raw["maximum"].(float64); ok {
		field.spec.maximum = &n
	}
	if n, ok := raw["minLength"].(float64); ok {
		v := int(n)
		field.spec.minLength = &v
	}
	if n, ok := raw["maxLength"].(float64); ok {
		v := int(n)
		field.spec.maxLength = &v
	}
	if conds, ok := raw["x-show-if"]; ok {
		parsed, err := parseConditions(conds)
		if err != nil {
			return nil, fmt.Errorf("表单字段 %q 的 x-show-if 无法解析: %w", key, err)
		}
		field.spec.showIf = parsed
	}

	// 枚举在字符串字段上，或多选字段的 items 上。
	if opts, ok := parseOptions(raw); ok {
		field.spec.options = opts
	}
	if items, ok := raw["items"].(map[string]any); ok {
		itemType, _ := items[keyType].(string)
		if itemType == string(TypeObject) {
			children, err := parseChildren(items)
			if err != nil {
				return nil, err
			}
			field.spec.items = &Field{spec: spec{typ: TypeObject, children: children}}
		} else {
			child := &Field{spec: spec{typ: Type(itemType)}}
			if w, ok := items["x-widget"].(string); ok {
				child.spec.widget = Widget(w)
			}
			// 多选的候选项声明在 items 上，取值仍在字段这一层。
			if opts, ok := parseOptions(items); ok {
				field.spec.options = opts
			}
			field.spec.items = child
		}
	}
	if _, ok := raw["properties"].(map[string]any); ok {
		children, err := parseChildren(raw)
		if err != nil {
			return nil, err
		}
		field.spec.children = children
	}

	// required 在父层声明，故这里先标记、稍后由调用方补齐。
	return field, nil
}

// parseChildren 解析嵌套对象的子字段，并就地标记各自的必填。
func parseChildren(holder map[string]any) ([]*Field, error) {
	props, _ := holder["properties"].(map[string]any)
	required := map[string]bool{}
	if raw, ok := holder["required"].([]any); ok {
		for _, item := range raw {
			key, _ := item.(string)
			required[key] = true
		}
	}
	out := make([]*Field, 0, len(props))
	for key, raw := range props {
		prop, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		child, err := parseField(key, prop)
		if err != nil {
			return nil, err
		}
		child.spec.required = required[key]
		out = append(out, child)
	}
	return sortByName(out), nil
}

// parseOptions 从 enum / enumNames 还原选项；两者数量对不上时放弃 enumNames，
// 宁可用原始值显示也不错位——错位的选项比英文值更难发现。
func parseOptions(raw map[string]any) ([]Option, bool) {
	values, ok := raw["enum"].([]any)
	if !ok || len(values) == 0 {
		return nil, false
	}
	names, _ := raw["enumNames"].([]any)
	out := make([]Option, 0, len(values))
	for i, value := range values {
		label := fmt.Sprint(value)
		if len(names) == len(values) {
			label = fmt.Sprint(names[i])
		}
		out = append(out, Option{Value: fmt.Sprint(value), Label: label})
	}
	return out, true
}

// parseConditions 解析 x-show-if。单个对象与数组两种写法都接受：
// 手写 YAML 的主题作者多半会写单个对象。
func parseConditions(raw any) ([]Condition, error) {
	items, ok := raw.([]any)
	if !ok {
		items = []any{raw}
	}
	out := make([]Condition, 0, len(items))
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("条件须为对象")
		}
		field, _ := entry["field"].(string)
		op, _ := entry["op"].(string)
		if field == "" || op == "" {
			return nil, fmt.Errorf("条件缺少 field 或 op")
		}
		out = append(out, Condition{Field: field, Op: op, Value: entry["value"]})
	}
	return out, nil
}
