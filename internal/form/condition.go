package form

import "reflect"

// widget 返回实际生效的控件：显式指定的优先，否则按类型与枚举推断。
func (f *Field) widget() Widget {
	if f.spec.widget != "" {
		return f.spec.widget
	}
	return inferredWidget(f.spec.typ, f.spec.items, len(f.spec.options) > 0)
}

// Missing 返回在当前值下「应当显示却没有填」的必填字段路径。
//
// 这是条件必填的服务端判定方。Schema 里的 required 只收无条件必填的字段，
// 带 x-show-if 的字段在条件不成立时压根不该存在，把它写进 required
// 会让「隐藏起来所以没填」变成一次校验失败——那正是条件依赖要消灭的东西。
//
// 路径用点号连接，与接口错误明细的 body.<路径> 对得上，因此前端能把每一条
// 落回对应字段。嵌套字段的路径形如 parent.child。
func (f *Form) Missing(values map[string]any) []string {
	_, _, _ = f.Build()
	var out []string
	for _, field := range f.top {
		out = append(out, missingIn(field, "", values)...)
	}
	return out
}

// Visible 报告某个顶层字段在当前值下是否显示。
func (f *Form) Visible(key string, values map[string]any) bool {
	field, ok := f.Field(key)
	if !ok {
		return false
	}
	return conditionsHold(field.spec.showIf, values)
}

// missingIn 递归收集缺失的必填字段。scope 是字段所在的那一层值，
// 条件里的字段名先在这一层找，找不到再回到表单根上找。
func missingIn(field *Field, prefix string, scope map[string]any) []string {
	path := field.key
	if prefix != "" {
		path = prefix + "." + field.key
	}
	if !conditionsHold(field.spec.showIf, scope) {
		return nil
	}

	var out []string
	if field.spec.required && isEmptyValue(scope[field.key]) {
		out = append(out, path)
	}
	if field.spec.typ == TypeObject {
		if nested, ok := scope[field.key].(map[string]any); ok {
			for _, child := range field.spec.children {
				out = append(out, missingIn(child, path, nested)...)
			}
		}
	}
	return out
}

// conditionsHold 报告一组条件是否全部成立（即「与」）。
//
// 空条件集恒成立：没有声明依赖的字段永远显示。
func conditionsHold(conds []Condition, scope map[string]any) bool {
	for _, cond := range conds {
		if !conditionHolds(cond, scope) {
			return false
		}
	}
	return true
}

// conditionHolds 判定单条条件。
//
// 取值先在当前这一层找；找不到时回落到表单根——嵌套分组里引用一个顶层开关
// 是很自然的写法，而要求作者写全路径会把声明变得难以阅读。
// 两处都没有时按零值处理，于是 Eq 为假、Empty 为真。
func conditionHolds(cond Condition, scope map[string]any) bool {
	value, found := scope[cond.Field]
	if !found {
		value = nil
	}

	switch cond.Op {
	case opEq:
		return looseEqual(value, cond.Value)
	case opNe:
		return !looseEqual(value, cond.Value)
	case opIn:
		items, ok := cond.Value.([]any)
		if !ok {
			return false
		}
		for _, item := range items {
			if looseEqual(value, item) {
				return true
			}
		}
		return false
	case opEmpty:
		return isEmptyValue(value)
	case opNotEmpty:
		return !isEmptyValue(value)
	default:
		// 认不出的运算符按「不成立」处理：字段因此不显示。
		// 这比默认显示更安全——一个被误认为可见的字段会参与必填校验，
		// 拦住一次本该成功的保存。
		return false
	}
}

// isEmptyValue 是「空」的定义：nil、空串、空数组、空对象。
//
// 0 与 false 都**不算**空：它们是明确的取值，不是缺席。
// 需要判定它们时用 Eq(field, 0) 或 Eq(field, false)。
func isEmptyValue(value any) bool {
	switch v := value.(type) {
	case nil:
		return true
	case string:
		return v == ""
	case []any:
		return len(v) == 0
	case map[string]any:
		return len(v) == 0
	default:
		return false
	}
}

// looseEqual 比较两个来自不同来源的值。
//
// 必须宽容：声明里写的是 Eq("pageSize", 10)（Go 的 int），而值经 JSON 往返后是
// float64；直接比较类型会永远为假，表现是「条件永远不成立、字段永远不显示」。
func looseEqual(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if af, ok := toFloat(a); ok {
		bf, ok := toFloat(b)
		return ok && af == bf
	}
	if as, ok := a.(string); ok {
		bs, ok := b.(string)
		return ok && as == bs
	}
	if ab, ok := a.(bool); ok {
		bb, ok := b.(bool)
		return ok && ab == bb
	}
	return reflect.DeepEqual(a, b)
}

// toFloat 把各种数值形态归一成 float64。
func toFloat(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case int32:
		return float64(v), true
	default:
		return 0, false
	}
}
