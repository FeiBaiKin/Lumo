// Package form 提供声明式表单：用 Go 代码描述一组设置项，产出
// JSON Schema 2020-12 子集 + 缺省值，供 Console 的通用表单引擎渲染。
//
// 为什么要有这一层：此前每个模块手写一段 JSON 字符串当 Schema（见 internal/settings/site.go
// 的 siteSchema），字段名在 struct、Schema、Defaults、Public 四处各写一遍，没有任何
// 编译期约束，拼错一个键要到有人打开那一页才发现。本包把这些声明收进 Go 类型：
// 字段名是标识符、约束是方法调用、条件依赖是表达式，编译器于是能帮上忙。
//
// 声明读起来是这样：
//
//	form.New(
//	    form.NewSection("基础",
//	        form.Text("title").Label("站点标题").Required().MaxLen(128),
//	    ),
//	    form.NewSection("存储",
//	        form.Select("driver", form.Opt("local", "本地")).Label("存储驱动"),
//	        form.Text("s3Endpoint").Label("服务地址").Required().ShowIf(form.Eq("driver", "s3")),
//	    ),
//	)
//
// 产出的仍是 JSON Schema：渲染方是 TS、校验方是 JSON Schema 校验器，
// 这个格式已经是两端之间的既成契约，不另起一套。
package form

import "fmt"

// Type 是字段的 JSON Schema 类型。
type Type string

// 支持的字段类型，与 Console 表单引擎的 SchemaType 一一对应。
const (
	TypeString  Type = "string"
	TypeInteger Type = "integer"
	TypeNumber  Type = "number"
	TypeBoolean Type = "boolean"
	TypeObject  Type = "object"
	TypeArray   Type = "array"
)

// Widget 是控件的具体形态，序列化为 Schema 里的 x-widget 提示。
//
// 类型与控件是两件事：同为 string，可以是单行文本、多行文本、颜色或图片。
// 类型决定「什么值算合法」，控件决定「用什么方式让人填」。
// 每个 Widget 都对应 console/src/components/form/controls.tsx 里的一个控件，
// 两边对不上时由 cmd/lumo 的契约测试拦下。
type Widget string

// 全部控件。前 11 个是 Console 表单引擎第二版就有的，其余为本次扩充。
//
// 刻意不含富文本：Console 的文章编辑器是「初始内容 + 变化回调」的形态，
// 塞进受控的设置表单会与每次按键的回写互相打架，而设置项里几乎没有长到需要富文本的字段。
// 真要做，得先给编辑器加一个受控模式，那是另一件事。
const (
	WidgetText        Widget = "text"
	WidgetTextarea    Widget = "textarea"
	WidgetCode        Widget = "code"
	WidgetSecret      Widget = "secret"
	WidgetSelect      Widget = "select"
	WidgetRadio       Widget = "radio"
	WidgetMultiselect Widget = "multiselect"
	WidgetColor       Widget = "color"
	WidgetImage       Widget = "image"
	WidgetImages      Widget = "images"
	WidgetDate        Widget = "date"
	WidgetIcon        Widget = "icon"
	WidgetNumber      Widget = "number"
	WidgetSlider      Widget = "slider"
	WidgetSwitch      Widget = "switch"
	WidgetList        Widget = "list"
	WidgetRepeater    Widget = "repeater"
	WidgetGroup       Widget = "group"
)

// Option 是枚举项：值进 schema 的 enum，显示名进 enumNames。
//
// 显示名必须显式给出而不是由值推导——值多半是 local / s3 这种机器标识，
// 直接显示给站长看等于把内部约定漏到界面上。
type Option struct {
	Value string
	Label string
}

// Opt 构造一个枚举项。
func Opt(value, label string) Option { return Option{Value: value, Label: label} }

// Condition 是一条条件依赖，表达「本字段在什么情况下才有意义」。
//
// 序列化为 x-show-if。语义只影响界面：条件不成立时字段不显示、不参与必填校验，
// 但它的值不会被清掉——站长把驱动从 s3 切回 local 再切回来，填过的地址应当还在。
type Condition struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value any    `json:"value,omitempty"`
}

// 条件运算符。取值与 console/src/components/form/schema.ts 的 evaluateShowIf 一致。
const (
	opEq       = "eq"
	opNe       = "ne"
	opIn       = "in"
	opEmpty    = "empty"
	opNotEmpty = "notEmpty"
)

// Eq 表达「另一字段等于某值」。
func Eq(field string, value any) Condition {
	return Condition{Field: field, Op: opEq, Value: value}
}

// Ne 表达「另一字段不等于某值」。
func Ne(field string, value any) Condition {
	return Condition{Field: field, Op: opNe, Value: value}
}

// In 表达「另一字段取值为所列之一」。
func In(field string, values ...any) Condition {
	return Condition{Field: field, Op: opIn, Value: values}
}

// Empty 表达「另一字段为空」。
func Empty(field string) Condition { return Condition{Field: field, Op: opEmpty} }

// NotEmpty 表达「另一字段已填」。
func NotEmpty(field string) Condition { return Condition{Field: field, Op: opNotEmpty} }

// Field 是一个表单字段的声明。
//
// 所有配置方法都返回自身以便链式书写。Field 只在声明期使用，
// 构建完成后即冻进 *Form，运行时不改。
type Field struct {
	key  string
	spec spec
}

// spec 是字段的全部属性。
type spec struct {
	typ         Type
	widget      Widget
	label       string
	help        string
	required    bool
	def         any
	hasDefault  bool
	minimum     *float64
	maximum     *float64
	minLength   *int
	maxLength   *int
	pattern     string
	options     []Option
	items       *Field
	children    []*Field
	showIf      []Condition
	placeholder string
	unit        string
	rows        int
	itemLabel   string
}

// newField 构造一个字段。widget 缺省时由类型推断，此处只记下显式指定的那个。
func newField(key string, typ Type, widget Widget) *Field {
	return &Field{key: key, spec: spec{typ: typ, widget: widget}}
}

// Key 返回字段名。
func (f *Field) Key() string { return f.key }

// ---- 字符串类 ----

// Text 是单行文本。
func Text(key string) *Field { return newField(key, TypeString, WidgetText) }

// Textarea 是多行文本。
func Textarea(key string) *Field { return newField(key, TypeString, WidgetTextarea) }

// Code 是代码，等宽字体、保留缩进。
func Code(key string) *Field { return newField(key, TypeString, WidgetCode) }

// Color 是颜色，值为 #rrggbb 一类字符串。
func Color(key string) *Field { return newField(key, TypeString, WidgetColor) }

// Secret 是需要长期留存的口令或密钥：已存的值不回传、进库前加密、界面上留空即不改动。
//
// 它与 Text 的差别不在形态而在语义，而语义要由接口与存储两侧一起兑现
// （见 internal/settings 的 Group.Mask）。此处只负责把「这是口令」
// 这件事写进声明，让两边都有据可依——没有这个声明，口令只能靠字段名去猜。
func Secret(key string) *Field { return newField(key, TypeString, WidgetSecret) }

// Image 是单张图片的地址。
func Image(key string) *Field { return newField(key, TypeString, WidgetImage) }

// Images 是多张图片的地址列表。
func Images(key string) *Field { return newField(key, TypeArray, WidgetImages) }

// Date 是日期，值为 YYYY-MM-DD；需要时刻时写 YYYY-MM-DDTHH:mm。
func Date(key string) *Field { return newField(key, TypeString, WidgetDate) }

// Icon 是图标名，取值来自 Console 的图标登记表。
func Icon(key string) *Field { return newField(key, TypeString, WidgetIcon) }

// Select 是下拉选择，选项由枚举给出。
func Select(key string, opts ...Option) *Field {
	f := newField(key, TypeString, WidgetSelect)
	f.spec.options = opts
	return f
}

// Radio 是平铺的单选。选项较少时比下拉更好——一眼看全，少一次点击。
func Radio(key string, opts ...Option) *Field {
	f := newField(key, TypeString, WidgetRadio)
	f.spec.options = opts
	return f
}

// MultiSelect 是多选，值为数组。
//
// 候选项记在字段自己身上、序列化时落到 items 里：取值的范围是「条目」的属性，
// 而选项列表在声明与校验两处都要用，放在顶层才不必两边各存一份。
func MultiSelect(key string, opts ...Option) *Field {
	f := newField(key, TypeArray, WidgetMultiselect)
	f.spec.options = opts
	f.spec.items = &Field{spec: spec{typ: TypeString}}
	return f
}

// ---- 数值类 ----

// Int 是整数。
func Int(key string) *Field { return newField(key, TypeInteger, WidgetNumber) }

// Float 是小数。
func Float(key string) *Field { return newField(key, TypeNumber, WidgetNumber) }

// Slider 是拖动条，适用于有明确区间的数值。
func Slider(key string) *Field { return newField(key, TypeInteger, WidgetSlider) }

// ---- 布尔类 ----

// Bool 是开关。
func Bool(key string) *Field { return newField(key, TypeBoolean, WidgetSwitch) }

// ---- 复合类 ----

// List 是字符串列表，条目是自由文本。
func List(key string) *Field {
	f := newField(key, TypeArray, WidgetList)
	f.spec.items = &Field{spec: spec{typ: TypeString, widget: WidgetText}}
	return f
}

// Repeater 是可增删的重复组，每条由若干字段组成。
func Repeater(key string, fields ...*Field) *Field {
	f := newField(key, TypeArray, WidgetRepeater)
	f.spec.items = &Field{spec: spec{typ: TypeObject, children: fields}}
	return f
}

// Group 是嵌套的对象，用于把相关字段收成一层。
func Group(key string, fields ...*Field) *Field {
	f := newField(key, TypeObject, WidgetGroup)
	f.spec.children = fields
	return f
}

// ---- 链式配置 ----

// Label 设置显示名。缺省时表单上会显示原始键名，中文界面里那几乎总是遗漏。
func (f *Field) Label(s string) *Field { f.spec.label = s; return f }

// Help 设置字段说明，显示在标签与控件之间。
func (f *Field) Help(s string) *Field { f.spec.help = s; return f }

// Required 标记为必填。与 ShowIf 同用时是「条件必填」。
func (f *Field) Required() *Field { f.spec.required = true; return f }

// Default 设置缺省值。每个字段都应当有一个——没有缺省值时，
// 表单首次打开该字段是空的，而「空」与「未设置」在多数控件上分不出来。
func (f *Field) Default(v any) *Field {
	f.spec.def = v
	f.spec.hasDefault = true
	return f
}

// Min 设置数值下限。
func (f *Field) Min(n float64) *Field { f.spec.minimum = &n; return f }

// Max 设置数值上限。
func (f *Field) Max(n float64) *Field { f.spec.maximum = &n; return f }

// MinLen 设置最短长度。
func (f *Field) MinLen(n int) *Field { f.spec.minLength = &n; return f }

// MaxLen 设置最长长度。
func (f *Field) MaxLen(n int) *Field { f.spec.maxLength = &n; return f }

// Pattern 设置正则约束。写法必须是 ECMA-262 与 RE2 都认的形态：
// 前端用 new RegExp、后端用 Go 的 regexp，两边都要能编译。
func (f *Field) Pattern(re string) *Field { f.spec.pattern = re; return f }

// Widget 覆盖推断出的控件。类型已由构造函数定下，此处只改呈现方式。
func (f *Field) Widget(w Widget) *Field { f.spec.widget = w; return f }

// Placeholder 设置占位提示。
func (f *Field) Placeholder(s string) *Field { f.spec.placeholder = s; return f }

// Unit 是数值字段后面的单位，如「秒」「条」。
func (f *Field) Unit(s string) *Field { f.spec.unit = s; return f }

// Rows 是文本域可见行数。
func (f *Field) Rows(n int) *Field { f.spec.rows = n; return f }

// ItemLabel 是重复组的条目名，用于「添加一项」的按钮文案。
func (f *Field) ItemLabel(s string) *Field { f.spec.itemLabel = s; return f }

// ShowIf 声明条件依赖：全部条件成立时字段才显示。
//
// 多处传参即「与」。需要「或」时用 In 把取值列进一条条件，
// 而不是再加一个组合算子——设置表单里需要或关系的场景，
// 多半是被声明方式绕进去的。
func (f *Field) ShowIf(conds ...Condition) *Field {
	f.spec.showIf = append(f.spec.showIf, conds...)
	return f
}

// errf 构造带上字段名的声明错误。
func (f *Field) errf(format string, args ...any) error {
	return fmt.Errorf("字段 %q："+format, append([]any{f.key}, args...)...)
}
