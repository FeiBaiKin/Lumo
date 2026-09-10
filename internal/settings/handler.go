package settings

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/FeiBaiKin/lumo/internal/auth"
	"github.com/FeiBaiKin/lumo/internal/auth/perm"
)

var tagSettings = []string{"settings"}

// Handler 提供设置的 Console 与 Public 接口。
type Handler struct {
	service *Service
}

// NewHandler 构造 Handler。
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// Register 挂载接口：Console 全部需要 settings:manage，Public 只暴露公开字段。
func (h *Handler) Register(console, public huma.API) {
	manage := huma.Middlewares{auth.RequirePermission(perm.SettingsManage)}

	huma.Register(console, huma.Operation{
		OperationID: "settings-list-groups",
		Method:      http.MethodGet,
		Path:        "/settings",
		Summary:     "列出全部设置分组",
		Description: "每个分组携带表单 Schema、缺省值与当前有效值，Console 据此渲染通用表单。",
		Tags:        tagSettings,
		Middlewares: manage,
	}, h.list)
	huma.Register(console, huma.Operation{
		OperationID: "settings-get-group",
		Method:      http.MethodGet,
		Path:        "/settings/{group}",
		Summary:     "获取设置分组",
		Tags:        tagSettings,
		Middlewares: manage,
		Errors:      []int{http.StatusNotFound},
	}, h.get)
	huma.Register(console, huma.Operation{
		OperationID: "settings-update-group",
		Method:      http.MethodPut,
		Path:        "/settings/{group}",
		Summary:     "更新设置分组",
		Description: "请求体为该分组的值对象，与缺省值按顶层键合并后整体校验并保存。",
		Tags:        tagSettings,
		Middlewares: manage,
		Errors:      []int{http.StatusNotFound, http.StatusUnprocessableEntity},
	}, h.update)

	huma.Register(public, huma.Operation{
		OperationID: "settings-public",
		Method:      http.MethodGet,
		Path:        "/settings",
		Summary:     "公开的站点设置",
		Description: "只包含各分组中标记为公开的字段，供主题与登录页使用。",
		Tags:        tagSettings,
	}, h.public)
}

// groupView 是分组的接口视图。
type groupView struct {
	Name        string         `json:"name"`
	Label       string         `json:"label"`
	Description string         `json:"description"`
	Order       int            `json:"order"`
	Public      []string       `json:"public" doc:"可经 Public 平面读取的字段"`
	Schema      map[string]any `json:"schema" doc:"JSON Schema 2020-12 子集 + x-widget"`
	Defaults    map[string]any `json:"defaults"`
	Values      map[string]any `json:"values" doc:"当前有效值：缺省值被已保存值覆盖"`
}

type groupInput struct {
	Group string `path:"group" minLength:"1" maxLength:"64"`
}

type groupUpdateInput struct {
	Group string         `path:"group" minLength:"1" maxLength:"64"`
	Body  map[string]any `doc:"分组的值对象"`
}

type groupOutput struct {
	Body groupView
}

type groupList struct {
	Items []groupView `json:"items"`
}

type groupListOutput struct {
	Body groupList
}

type publicOutput struct {
	Body map[string]map[string]any
}

func (h *Handler) view(ctx context.Context, g *Group) (groupView, error) {
	values, err := h.service.Effective(ctx, g.Name)
	if err != nil {
		return groupView{}, err
	}
	public := g.Public
	if public == nil {
		public = []string{}
	}
	return groupView{
		Name:        g.Name,
		Label:       g.Label,
		Description: g.Description,
		Order:       g.Order,
		Public:      public,
		Schema:      g.SchemaDoc(),
		Defaults:    g.DefaultValues(),
		Values:      values,
	}, nil
}

func (h *Handler) list(ctx context.Context, _ *struct{}) (*groupListOutput, error) {
	groups := h.service.Groups()
	items := make([]groupView, 0, len(groups))
	for _, g := range groups {
		v, err := h.view(ctx, g)
		if err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return &groupListOutput{Body: groupList{Items: items}}, nil
}

func (h *Handler) get(ctx context.Context, in *groupInput) (*groupOutput, error) {
	g, ok := h.service.Group(in.Group)
	if !ok {
		return nil, huma.Error404NotFound(ErrUnknownGroup.Error())
	}
	v, err := h.view(ctx, g)
	if err != nil {
		return nil, err
	}
	return &groupOutput{Body: v}, nil
}

func (h *Handler) update(ctx context.Context, in *groupUpdateInput) (*groupOutput, error) {
	g, ok := h.service.Group(in.Group)
	if !ok {
		return nil, huma.Error404NotFound(ErrUnknownGroup.Error())
	}
	if _, err := h.service.Update(ctx, in.Group, in.Body); err != nil {
		var verr *ValidationError
		if errors.As(err, &verr) {
			details := make([]error, 0, len(verr.Details))
			for i := range verr.Details {
				d := verr.Details[i]
				details = append(details, &huma.ErrorDetail{Message: d.Message, Location: d.Location})
			}
			return nil, huma.Error422UnprocessableEntity("设置校验失败", details...)
		}
		return nil, err
	}
	v, err := h.view(ctx, g)
	if err != nil {
		return nil, err
	}
	return &groupOutput{Body: v}, nil
}

func (h *Handler) public(ctx context.Context, _ *struct{}) (*publicOutput, error) {
	values, err := h.service.Public(ctx)
	if err != nil {
		return nil, err
	}
	return &publicOutput{Body: values}, nil
}
