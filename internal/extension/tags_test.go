package extension

import (
	"reflect"
	"testing"
)

// TestPatternTagsMatchConstants 盯住命名模式的两处写法。
//
// 同一套形态有两份实现：Go 侧的 Validate*（用 resource.go 里的常量）与结构体标签
// （写进 OpenAPI，也是 huma 的第一道拦截）。标签里只能写字面量，两者一旦不同步，
// 就成了「文档一套、服务端另一套」。更阴的是标签值要经 strconv.Unquote——
// 反斜杠少写一层时整条 pattern 被静默丢弃，校验看上去还在，其实已经不生效了。
func TestPatternTagsMatchConstants(t *testing.T) {
	t.Parallel()

	cases := []struct {
		typ   reflect.Type
		field string
		want  string
	}{
		{reflect.TypeOf(ResourceParams{}), "Group", GroupPattern},
		{reflect.TypeOf(ResourceParams{}), "Version", VersionPattern},
		{reflect.TypeOf(ResourceParams{}), "Resource", ResourcePattern},
		{reflect.TypeOf(NameParam{}), "Name", NamePattern},
		{reflect.TypeOf(extensionCreateBody{}), "Kind", KindPattern},
		{reflect.TypeOf(extensionCreateBody{}), "Name", NamePattern},
	}
	for _, c := range cases {
		f, ok := c.typ.FieldByName(c.field)
		if !ok {
			t.Fatalf("%s 没有字段 %s", c.typ, c.field)
		}
		if got := f.Tag.Get("pattern"); got != c.want {
			t.Errorf("%s.%s 的 pattern 标签 = %q，期望 %q", c.typ.Name(), c.field, got, c.want)
		}
	}
}
