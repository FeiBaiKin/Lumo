package console

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/FeiBaiKin/lumo/internal/app"
)

// NavPath 是菜单接口的路径，挂在 Console 平面之下。
const NavPath = "/navigation"

// NavGroupView 是菜单分组的接口视图。
type NavGroupView struct {
	Name  string `json:"name" doc:"分组标识"`
	Label string `json:"label" doc:"分组显示名"`
	Order int    `json:"order"`
}

// NavItemView 是菜单项的接口视图。
type NavItemView struct {
	Key         string `json:"key" doc:"菜单项的稳定标识"`
	Label       string `json:"label"`
	Path        string `json:"path" doc:"Console 内的绝对路径"`
	Icon        string `json:"icon" doc:"图标名，取值来自前端的图标登记表"`
	Group       string `json:"group" doc:"所属分组的标识"`
	Order       int    `json:"order"`
	Permission  string `json:"permission" doc:"显示所需的权限串；空串表示所有已登录用户可见"`
	Keywords    string `json:"keywords" doc:"命令面板的检索关键词"`
	Description string `json:"description,omitempty"`
	End         bool   `json:"end" doc:"是否只在路径完全相等时高亮"`
	Hidden      bool   `json:"hidden" doc:"是否不进侧边栏（仍然参与命令面板与标题映射）"`
}

// NavView 是菜单接口的响应体。
type NavView struct {
	Groups []NavGroupView `json:"groups"`
	Items  []NavItemView  `json:"items"`
}

// NavHandler 提供 Console 的菜单。
//
// 菜单为什么由服务端给而不是前端写死：插件的页面要出现在侧边栏里，
// 而前端不可能事先知道有哪些插件。清单一旦写死在前端，
// 它就必须与后端的模块构成保持同步，而那种同步只能靠人记着。
//
// **权限在这里不过滤**：本接口对所有已登录用户开放，携带每一项所需的权限串，
// 由前端决定显示与否。理由是菜单的过滤规则与前端已有的 can() 必须是同一套——
// 服务端过滤一遍、前端再判一遍，两套规则迟早分叉。
// 菜单不是安全边界，真正的拦截在各页面的服务端接口上。
type NavHandler struct {
	navigation func() app.Navigation
}

// NewNavHandler 构造菜单处理器。
//
// 接受一个取菜单的函数而不是一份清单：设置分组的登记要等模块 Start，
// 而设置页的入口是从那份分组列表推导出来的。构造时取一次会漏掉它们。
func NewNavHandler(navigation func() app.Navigation) *NavHandler {
	return &NavHandler{navigation: navigation}
}

type navOutput struct {
	Body NavView
}

// Register 挂载菜单接口。
func (h *NavHandler) Register(console huma.API) {
	huma.Register(console, huma.Operation{
		OperationID: "console-navigation",
		Method:      http.MethodGet,
		Path:        NavPath,
		Summary:     "获取 Console 的侧边栏菜单",
		Description: "返回内置模块与已启用插件声明的全部菜单项与分组，顺序已排好。",
		Tags:        []string{"console"},
	}, h.get)
}

func (h *NavHandler) get(_ context.Context, _ *struct{}) (*navOutput, error) {
	declared := h.navigation()
	view := NavView{
		Groups: make([]NavGroupView, 0, len(declared.Groups)),
		Items:  make([]NavItemView, 0, len(declared.Items)),
	}
	for _, group := range declared.Groups {
		view.Groups = append(view.Groups, NavGroupView{
			Name: group.Name, Label: group.Label, Order: group.Order,
		})
	}
	for i := range declared.Items {
		item := &declared.Items[i]
		view.Items = append(view.Items, NavItemView{
			Key: item.Key, Label: item.Label, Path: item.Path, Icon: item.Icon,
			Group: item.Group, Order: item.Order, Permission: item.Permission,
			Keywords: item.Keywords, Description: item.Description,
			End: item.End, Hidden: item.Hidden,
		})
	}
	return &navOutput{Body: view}, nil
}
