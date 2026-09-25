package lumo

import "encoding/json"

// ---------- 前台（capabilities.frontend 与 spec.frontend） ----------

// 前台插槽。主题在页面的固定位置留口子，插件在 spec.frontend.slots 里声明要往哪几个里放东西。
const (
	// SlotHead 在 </head> 之前，只收 meta 与少数几种 link 标签（样式表请写进 spec.frontend.styles）。
	SlotHead = "head"
	// SlotFooter 在 </body> 之前。
	SlotFooter = "footer"
	// SlotContentBefore 在文章与页面的正文之前。
	SlotContentBefore = "content.before"
	// SlotContentAfter 在文章与页面的正文之后。
	SlotContentAfter = "content.after"
	// SlotCommentsAfter 在评论区之后。
	SlotCommentsAfter = "comments.after"
)

// PageInfo 是访客正在看的页面。
type PageInfo struct {
	// Kind 是页面种类：index、post、page、category、tag、archive、search、author、posts、404 等。
	Kind  string `json:"kind"`
	Path  string `json:"path"`
	Title string `json:"title"`
	// Post 在文章与页面上是当前内容，其余为 nil。
	Post *PostRef `json:"post,omitempty"`
}

// Shortcode 是正文里的一个短代码：[名字 参数="值"]。
type Shortcode struct {
	Name  string            `json:"name"`
	Attrs map[string]string `json:"attrs"`
	Page  *PageInfo         `json:"page"`
}

// Attr 取一个参数，没写时返回 fallback。
func (s *Shortcode) Attr(name, fallback string) string {
	if v, ok := s.Attrs[name]; ok {
		return v
	}
	return fallback
}

type fragmentWire struct {
	Page *PageInfo `json:"page"`
}

// onFragment 登记一个渲染前台片段的处理函数：返回 HTML，空串表示这次不显示。
func onFragment(kind, name string, fn func(ctx *Context, page *PageInfo) (string, error)) {
	register(kind, name, func(ctx *Context, payload json.RawMessage) (any, error) {
		var in fragmentWire
		if err := json.Unmarshal(payload, &in); err != nil {
			return nil, err
		}
		if in.Page == nil {
			in.Page = &PageInfo{}
		}
		return fn(ctx, in.Page)
	})
}

// OnSlot 登记一个插槽的渲染函数，返回要放进去的 HTML。
//
// 访客每打开一页都会调一次，时限 200 毫秒，超时或出错这一处就空着。HTML 会按正文的规则净化，
// 另外放行表单控件；事件属性与内联脚本不收，要交互就在 spec.frontend.scripts 里带脚本文件。
// 宿主把每个插件的内容包在 <div class="plugin-slot" data-plugin="插件名" data-slot="插槽名"> 里。
func OnSlot(slot string, fn func(ctx *Context, page *PageInfo) (string, error)) {
	onFragment("slot", slot, fn)
}

// OnWidget 登记一个侧栏小组件，名字对应 spec.frontend.widgets 的 name。规则同 OnSlot。
func OnWidget(name string, fn func(ctx *Context, page *PageInfo) (string, error)) {
	onFragment("widget", name, fn)
}

// OnShortcode 登记一个短代码，名字对应 spec.frontend.shortcodes 的 name。
//
// 只在文章与页面的详情页展开，一页最多 20 个；代码块里的不展开，写成 [[名字]] 原样显示。规则同 OnSlot。
func OnShortcode(name string, fn func(ctx *Context, sc *Shortcode) (string, error)) {
	register("shortcode", name, func(ctx *Context, payload json.RawMessage) (any, error) {
		sc := &Shortcode{}
		if err := json.Unmarshal(payload, sc); err != nil {
			return nil, err
		}
		if sc.Attrs == nil {
			sc.Attrs = map[string]string{}
		}
		if sc.Page == nil {
			sc.Page = &PageInfo{}
		}
		return fn(ctx, sc)
	})
}
