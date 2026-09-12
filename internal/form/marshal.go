package form

import "fmt"

// Schema 里反复出现的键名。写成常量是为了让 goconst 满意，也避免拼错——
// 拼错一个键的表现是「这条约束悄悄失效」，不会有任何报错。
const keyType = "type"

// marshal 把字段渲染成 JSON Schema 的属性片段。
//
// 只写确实需要写的东西：能从类型推断出的控件不写 x-widget，没有约束的字段不带约束键。
// 写满每一个字段会让 Schema 变长，也掩盖了「这个字段确实需要特定控件」
// 与「它本来就该是默认控件」的区别。
func (f *Field) marshal() (map[string]any, error) {
	out := map[string]any{keyType: string(f.spec.typ)}

	if f.spec.label != "" {
		out["title"] = f.spec.label
	}
	if f.spec.help != "" {
		out["description"] = f.spec.help
	}

	if f.spec.minimum != nil {
		out["minimum"] = *f.spec.minimum
	}
	if f.spec.maximum != nil {
		out["maximum"] = *f.spec.maximum
	}
	if f.spec.minLength != nil {
		out["minLength"] = *f.spec.minLength
	}
	if f.spec.maxLength != nil {
		out["maxLength"] = *f.spec.maxLength
	}
	if f.spec.pattern != "" {
		out["pattern"] = f.spec.pattern
	}

	if f.spec.placeholder != "" {
		out["x-placeholder"] = f.spec.placeholder
	}
	if f.spec.unit != "" {
		out["x-unit"] = f.spec.unit
	}
	if f.spec.rows > 0 {
		out["x-rows"] = f.spec.rows
	}
	if f.spec.itemLabel != "" {
		out["x-item-label"] = f.spec.itemLabel
	}
	if len(f.spec.showIf) > 0 {
		out["x-show-if"] = f.spec.showIf
	}

	// 枚举落在字符串自身，或数组的 items 上：多选的取值范围是条目类型的事，
	// 写成顶层 enum 会让「数组等于其中一个字符串」这种无解的约束。
	if len(f.spec.options) > 0 {
		values, names := optionArrays(f.spec.options)
		switch f.spec.typ {
		case TypeString:
			out["enum"] = values
			out["enumNames"] = names
		case TypeArray:
			// 见下面的 items 分支
		default:
			return nil, f.errf("枚举只适用于字符串或多选，当前类型是 %s", f.spec.typ)
		}
	}

	if f.spec.typ == TypeArray {
		if f.spec.items == nil {
			return nil, f.errf("数组未声明条目类型")
		}
		var item map[string]any
		if f.spec.items.spec.typ == TypeObject {
			nested, err := marshalObject(f.spec.items)
			if err != nil {
				return nil, err
			}
			item = nested
		} else {
			item = f.spec.items.marshalShallow()
		}
		if len(f.spec.options) > 0 {
			values, names := optionArrays(f.spec.options)
			item["enum"] = values
			item["enumNames"] = names
		}
		out["items"] = item
	}

	if f.spec.typ == TypeObject {
		nested, err := marshalObject(f)
		if err != nil {
			return nil, err
		}
		for key, value := range nested {
			out[key] = value
		}
	}

	// x-widget 只在推断不出同样结果时才写。
	if w := f.widget(); w != inferredWidget(f.spec.typ, f.spec.items, len(f.spec.options) > 0) {
		out["x-widget"] = string(w)
	}
	return out, nil
}

// marshalShallow 渲染数组条目这类不需要递归到对象层的浅层 Schema。
func (f *Field) marshalShallow() map[string]any {
	out := map[string]any{keyType: string(f.spec.typ)}
	if f.spec.minLength != nil {
		out["minLength"] = *f.spec.minLength
	}
	if f.spec.maxLength != nil {
		out["maxLength"] = *f.spec.maxLength
	}
	if f.spec.pattern != "" {
		out["pattern"] = f.spec.pattern
	}
	return out
}

// marshalObject 渲染对象的 properties 与 required。
//
// required 只收「无条件必填」的字段：带 x-show-if 的字段在条件不成立时并不存在，
// 把它写进 required 会让「隐藏起来但没填」变成一次校验失败——
// 那正是条件依赖要消灭的东西。条件必填由 Form.RequiredMissing 精确判定。
func marshalObject(f *Field) (map[string]any, error) {
	props := map[string]any{}
	required := make([]any, 0, len(f.spec.children))
	seen := make(map[string]bool, len(f.spec.children))

	for _, child := range f.spec.children {
		if child.key == "" {
			return nil, fmt.Errorf("字段 %q 下有一个未命名的子字段", f.key)
		}
		if seen[child.key] {
			return nil, f.errf("子字段 %q 重复声明", child.key)
		}
		seen[child.key] = true

		marshalled, err := child.marshal()
		if err != nil {
			return nil, err
		}
		props[child.key] = marshalled
		if child.spec.required && len(child.spec.showIf) == 0 {
			required = append(required, child.key)
		}
	}

	out := map[string]any{"properties": props, "additionalProperties": false}
	if len(required) > 0 {
		out["required"] = required
	}
	return out, nil
}

// optionArrays 把选项拆成 enum 与 enumNames 两个数组。
func optionArrays(opts []Option) (values, names []any) {
	values = make([]any, 0, len(opts))
	names = make([]any, 0, len(opts))
	for _, o := range opts {
		values = append(values, o.Value)
		names = append(names, o.Label)
	}
	return values, names
}

// inferredWidget 是省略 x-widget 时前端会推断出的控件。
//
// 必须与 console/src/components/form/schema.ts 的 widgetFor 给出同样的结果，
// 只有两边算出同一个答案时，省略 x-widget 才是安全的。
func inferredWidget(typ Type, items *Field, hasOptions bool) Widget {
	if hasOptions && typ == TypeString {
		return WidgetSelect
	}
	switch typ {
	case TypeBoolean:
		return WidgetSwitch
	case TypeInteger, TypeNumber:
		return WidgetNumber
	case TypeObject:
		return WidgetGroup
	case TypeArray:
		if items != nil && items.spec.typ == TypeObject {
			return WidgetRepeater
		}
		return WidgetList
	default:
		return WidgetText
	}
}
