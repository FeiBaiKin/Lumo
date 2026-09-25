package plugin

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/FeiBaiKin/lumo/internal/auth"
	"github.com/FeiBaiKin/lumo/internal/auth/perm"
)

// pageView 是后台自定义页的信息：Console 据此放一个隔离的 iframe。
type pageView struct {
	Plugin      string `json:"plugin"`
	PluginLabel string `json:"pluginLabel"`
	Path        string `json:"path"`
	Label       string `json:"label"`
	Description string `json:"description"`
	// Icon 是页面的图标名，留空时 Console 用拼图。
	Icon string `json:"icon"`
	// Src 是 iframe 的地址。
	Src string `json:"src"`
	// API 是这个插件接口的前缀；页面经后台转发只能调这下面的地址。
	API string `json:"api"`
}

type pageInput struct {
	Name string `path:"name" minLength:"1" maxLength:"64"`
	Page string `path:"page" minLength:"1" maxLength:"64"`
}

type pageOutput struct{ Body pageView }

type widgetListOutput struct {
	Body struct {
		Items []widgetInfo `json:"items"`
	}
}

// registerFrontend 挂载后台自定义页与插件小组件的接口。
func (h *Handler) registerFrontend(console huma.API) {
	huma.Register(console, huma.Operation{
		OperationID: "plugin-page",
		Method:      http.MethodGet,
		Path:        "/plugins/{name}/pages/{page}",
		Summary:     "插件的后台页面",
		Description: "按页面声明的权限串判定；插件须已启用。",
		Tags:        tagPlugins,
		Errors:      []int{http.StatusForbidden, http.StatusNotFound},
	}, h.page)

	huma.Register(console, huma.Operation{
		OperationID: "plugin-widgets",
		Method:      http.MethodGet,
		Path:        "/plugin-widgets",
		Summary:     "插件提供的侧栏小组件",
		Description: "主题设置里「插件小组件」的可选项：启用中、且被授予了前台能力的插件提供的全部小组件。",
		Tags:        tagPlugins,
		Middlewares: huma.Middlewares{auth.RequirePermission(perm.ThemesManage)},
	}, h.listWidgets)
}

func (h *Handler) page(ctx context.Context, in *pageInput) (*pageOutput, error) {
	loaded, err := h.requireEnabled(in.Name)
	if err != nil {
		return nil, err
	}
	decl, ok := loaded.PageByPath(in.Page)
	if !ok {
		return nil, huma.Error404NotFound("插件没有这个页面")
	}
	if !auth.MustFromContext(ctx).Has(decl.RequiredPermission()) {
		return nil, auth.ForbiddenProblem(decl.RequiredPermission())
	}
	return &pageOutput{Body: pageView{
		Plugin: loaded.ID(), PluginLabel: loaded.Manifest.Spec.DisplayName,
		Path: decl.Path, Label: decl.Label, Description: decl.Description, Icon: decl.Icon,
		Src: assetURL(loaded, decl.File), API: RoutesPrefix + "/" + loaded.ID(),
	}}, nil
}

func (h *Handler) listWidgets(_ context.Context, _ *struct{}) (*widgetListOutput, error) {
	out := &widgetListOutput{}
	out.Body.Items = h.module.widgets()
	return out, nil
}
