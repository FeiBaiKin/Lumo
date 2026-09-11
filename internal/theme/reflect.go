package theme

import (
	"encoding/json"
	"html/template"
	"reflect"
)

// 本文件集中存放模板函数里需要反射的部分。
//
// 单独成文件是因为反射代码的出错方式与其余纯函数完全不同：
// 它必须对任意输入都不 panic——模板里传错类型是常态，而一次 panic 就是一个 500 页面。

// length 返回切片、数组、map 或字符串的长度；其他类型返回 0。
func length(v any) int {
	if v == nil {
		return 0
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map, reflect.String, reflect.Chan:
		return rv.Len()
	case reflect.Pointer, reflect.Interface:
		if rv.IsNil() {
			return 0
		}
		return length(rv.Elem().Interface())
	default:
		return 0
	}
}

// takeSlice 返回 list 的 [from, to) 子切片，越界自动收敛而非报错。
//
// 收敛而不报错：模板里写 first 5 却只有 3 条数据是完全正常的情形，
// 让它渲染出 3 条远比让整页 500 有用。
func takeSlice(list any, from, to int) any {
	if list == nil {
		return nil
	}
	rv := reflect.ValueOf(list)
	if rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return list
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return list
	}

	size := rv.Len()
	if from < 0 {
		from = 0
	}
	if to > size {
		to = size
	}
	if from >= to {
		// 返回同类型的空切片，让模板里的 range 与 len 行为一致。
		return reflect.MakeSlice(sliceTypeOf(rv), 0, 0).Interface()
	}
	if rv.Kind() == reflect.Array {
		// 数组不可直接切片，先拷进切片。
		out := reflect.MakeSlice(sliceTypeOf(rv), to-from, to-from)
		reflect.Copy(out, rv.Slice(from, to))
		return out.Interface()
	}
	return rv.Slice(from, to).Interface()
}

// sliceTypeOf 返回与 rv 元素类型一致的切片类型。
func sliceTypeOf(rv reflect.Value) reflect.Type {
	if rv.Kind() == reflect.Slice {
		return rv.Type()
	}
	return reflect.SliceOf(rv.Type().Elem())
}

// isEmpty 判定值是否为「空」，供 default 函数使用。
//
// 语义与模板 if 的真值判定一致：零值、空串、空集合、nil 都算空。
func isEmpty(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.String, reflect.Slice, reflect.Array, reflect.Map:
		return rv.Len() == 0
	case reflect.Bool:
		return !rv.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return rv.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return rv.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return rv.Float() == 0
	case reflect.Pointer, reflect.Interface:
		return rv.IsNil()
	default:
		return false
	}
}

// jsonify 把值编码为可安全内联进 <script> 的 JSON。
//
// 返回 template.JS 而非字符串：JSON-LD 与前端初始数据都需要原样输出。
// html/template 对 template.JS 仍会做 JS 上下文的检查，故这里不是完全放行。
func jsonify(v any) (template.JS, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return template.JS(data), nil //nolint:gosec // 内容经 json.Marshal 转义，用于 <script> 内联
}
