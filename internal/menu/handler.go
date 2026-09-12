package menu

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/FeiBaiKin/lumo/internal/auth"
	"github.com/FeiBaiKin/lumo/internal/auth/perm"
	"github.com/FeiBaiKin/lumo/internal/slug"
)

// tagMenus 是 OpenAPI 分组标签。
var tagMenus = []string{"menus"}

// 路由路径。Console 与 Public 平面共用相对路径，前缀由各平面的分组附加。
const (
	pathMenus     = "/menus"
	pathMenuByID  = "/menus/{id}"
	pathMenuItems = "/menus/{id}/items"
	// pathMenuBySlug 是前台按主题引用名取菜单的入口。
	pathMenuBySlug = "/menus/{slug}"
)

// maxItemDepth 是允许的条目层级，与 store 侧一致。
const maxItemDepth = 3

// maxItems 是单个菜单的条目数上限，防止一次提交把整棵树撑爆。
const maxItems = 200

// Handler 提供菜单的 Console 与 Public 接口。
type Handler struct {
	store    *Store
	resolver *resolver
	logger   logger
}

// logger 是本包用到的最小日志能力。
type logger interface {
	Warn(msg string, args ...any)
}

// NewHandler 构造 Handler。
func NewHandler(store *Store, resolver *resolver, log logger) *Handler {
	return &Handler{store: store, resolver: resolver, logger: log}
}

// Register 挂载接口。
//
// 读写都要求 menus:manage：菜单是站点级配置，不存在「自己的菜单」这种所有权概念。
func (h *Handler) Register(console, public huma.API) {
	manage := huma.Middlewares{auth.RequirePermission(perm.MenusManage)}

	huma.Register(console, huma.Operation{
		OperationID: "menu-list",
		Method:      http.MethodGet,
		Path:        pathMenus,
		Summary:     "列出全部菜单",
		Tags:        tagMenus,
		Middlewares: manage,
	}, h.list)
	huma.Register(console, huma.Operation{
		OperationID: "menu-get",
		Method:      http.MethodGet,
		Path:        pathMenuByID,
		Summary:     "获取菜单及其条目树",
		Tags:        tagMenus,
		Middlewares: manage,
		Errors:      []int{http.StatusNotFound},
	}, h.get)
	huma.Register(console, huma.Operation{
		OperationID:   "menu-create",
		Method:        http.MethodPost,
		Path:          pathMenus,
		Summary:       "创建菜单",
		Description:   "slug 留空则由名称生成；主题按 slug 引用菜单。",
		Tags:          tagMenus,
		DefaultStatus: http.StatusCreated,
		Middlewares:   manage,
		Errors:        []int{http.StatusBadRequest, http.StatusConflict},
	}, h.create)
	huma.Register(console, huma.Operation{
		OperationID: "menu-update",
		Method:      http.MethodPut,
		Path:        pathMenuByID,
		Summary:     "更新菜单",
		Description: "slug 留空则保留原值（改 slug 会让主题引用失效）。",
		Tags:        tagMenus,
		Middlewares: manage,
		Errors:      []int{http.StatusBadRequest, http.StatusConflict, http.StatusNotFound},
	}, h.update)
	huma.Register(console, huma.Operation{
		OperationID:   "menu-delete",
		Method:        http.MethodDelete,
		Path:          pathMenuByID,
		Summary:       "删除菜单",
		Description:   "其条目一并删除。",
		Tags:          tagMenus,
		DefaultStatus: http.StatusNoContent,
		Middlewares:   manage,
		Errors:        []int{http.StatusNotFound},
	}, h.delete)
	huma.Register(console, huma.Operation{
		OperationID: "menu-get-items",
		Method:      http.MethodGet,
		Path:        pathMenuItems,
		Summary:     "获取条目树",
		Tags:        tagMenus,
		Middlewares: manage,
		Errors:      []int{http.StatusNotFound},
	}, h.getItems)
	huma.Register(console, huma.Operation{
		OperationID: "menu-replace-items",
		Method:      http.MethodPut,
		Path:        pathMenuItems,
		Summary:     "整体替换条目树",
		Description: "请求体是嵌套的条目树，服务端按深度优先序重建。层级最多 " +
			"3 级，条目最多 200 个。站内条目的地址由记录解析，此处提交的 url 会被忽略。",
		Tags:        tagMenus,
		Middlewares: manage,
		Errors:      []int{http.StatusBadRequest, http.StatusNotFound},
	}, h.replaceItems)

	huma.Register(public, huma.Operation{
		OperationID: "menu-public-get",
		Method:      http.MethodGet,
		Path:        pathMenuBySlug,
		Summary:     "按 slug 获取菜单树",
		Description: "只返回可见条目，且会自动剔除指向已删除或未发布记录的条目。",
		Tags:        tagMenus,
		Errors:      []int{http.StatusNotFound},
	}, h.publicGet)
}

// ---------- 输入输出 ----------

type idInput struct {
	ID int64 `path:"id" minimum:"1"`
}

type slugInput struct {
	Slug string `path:"slug" minLength:"1" maxLength:"128"`
}

type menuBody struct {
	Name        string `json:"name" minLength:"1" maxLength:"64"`
	Slug        string `json:"slug,omitempty" maxLength:"128" doc:"主题引用名；留空则由名称生成"`
	Description string `json:"description,omitempty" maxLength:"500"`
}

type menuInput struct {
	Body menuBody
}

type menuUpdateInput struct {
	ID   int64 `path:"id" minimum:"1"`
	Body menuBody
}

type menuOutput struct {
	Body Menu
}

type menuList struct {
	Items []Menu `json:"items"`
}

type menuListOutput struct {
	Body menuList
}

// menuItemInput 是请求体里的一个条目。
//
// 名字不能叫 itemNode：与响应侧的 ItemNode 只差大小写，而 huma 的 Schema 注册表
// 按不区分大小写的名字索引，两者会撞名并直接 panic。
type menuItemInput struct {
	Label    string `json:"label" minLength:"1" maxLength:"128"`
	Type     string `json:"type" enum:"custom,post,page,category,tag"`
	TargetID *int64 `json:"targetId,omitempty" minimum:"1" doc:"站内记录 ID；自定义链接留空"`
	URL      string `json:"url,omitempty" maxLength:"1024" doc:"仅自定义链接使用"`
	Target   string `json:"target,omitempty" enum:",_blank"`
	Rel      string `json:"rel,omitempty" maxLength:"255"`
	Visible  *bool  `json:"visible,omitempty" doc:"留空视为可见"`
	// Children 是子条目，嵌套表达层级。
	Children []menuItemInput `json:"children,omitempty"`
}

// itemsBody 用对象而不是裸数组包一层：huma 无法为顶层数组生成请求体 Schema。
type itemsBody struct {
	Items []menuItemInput `json:"items" doc:"嵌套的条目树"`
}

type itemsInput struct {
	ID   int64 `path:"id" minimum:"1"`
	Body itemsBody
}

type itemsOutput struct {
	Body struct {
		Items []*ItemNode `json:"items"`
		Total int         `json:"total"`
	}
}

// ---------- 菜单处理器 ----------

func (h *Handler) list(ctx context.Context, _ *struct{}) (*menuListOutput, error) {
	items, err := h.store.List(ctx)
	if err != nil {
		return nil, err
	}
	return &menuListOutput{Body: menuList{Items: items}}, nil
}

func (h *Handler) get(ctx context.Context, in *idInput) (*menuOutput, error) {
	m, err := h.store.Get(ctx, in.ID)
	if err != nil {
		return nil, mapError(err)
	}
	items, err := h.store.Items(ctx, m.ID)
	if err != nil {
		return nil, err
	}
	m.Items = BuildTree(items)
	return &menuOutput{Body: *m}, nil
}

func (h *Handler) create(ctx context.Context, in *menuInput) (*menuOutput, error) {
	name, err := cleanName(in.Body.Name)
	if err != nil {
		return nil, err
	}
	s, err := resolveSlug(in.Body.Slug, name)
	if err != nil {
		return nil, err
	}

	m := &Menu{Name: name, Slug: s, Description: strings.TrimSpace(in.Body.Description)}
	if err := h.store.Create(ctx, m); err != nil {
		return nil, mapError(err)
	}
	m.Items = []*ItemNode{}
	return &menuOutput{Body: *m}, nil
}

func (h *Handler) update(ctx context.Context, in *menuUpdateInput) (*menuOutput, error) {
	m, err := h.store.Get(ctx, in.ID)
	if err != nil {
		return nil, mapError(err)
	}
	name, err := cleanName(in.Body.Name)
	if err != nil {
		return nil, err
	}
	// slug 留空保留原值：主题按它引用菜单，改了会让前台菜单消失。
	if s := strings.TrimSpace(in.Body.Slug); s != "" {
		m.Slug, err = resolveSlug(s, name)
		if err != nil {
			return nil, err
		}
	}

	m.Name = name
	m.Description = strings.TrimSpace(in.Body.Description)
	if updateErr := h.store.Update(ctx, m); updateErr != nil {
		return nil, mapError(updateErr)
	}
	items, err := h.store.Items(ctx, m.ID)
	if err != nil {
		return nil, err
	}
	m.Items = BuildTree(items)
	return &menuOutput{Body: *m}, nil
}

func (h *Handler) delete(ctx context.Context, in *idInput) (*struct{}, error) {
	if err := h.store.Delete(ctx, in.ID); err != nil {
		return nil, mapError(err)
	}
	return nil, nil //nolint:nilnil // 无响应体，huma 按 DefaultStatus 返回 204
}

func (h *Handler) getItems(ctx context.Context, in *idInput) (*itemsOutput, error) {
	if _, err := h.store.Get(ctx, in.ID); err != nil {
		return nil, mapError(err)
	}
	items, err := h.store.Items(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	h.resolver.resolveBestEffort(ctx, items)
	out := &itemsOutput{}
	out.Body.Items = BuildTree(items)
	out.Body.Total = CountItems(out.Body.Items)
	return out, nil
}

func (h *Handler) replaceItems(ctx context.Context, in *itemsInput) (*itemsOutput, error) {
	if _, err := h.store.Get(ctx, in.ID); err != nil {
		return nil, mapError(err)
	}

	items, parents, err := flatten(in.Body.Items)
	if err != nil {
		return nil, err
	}
	if err := h.store.ReplaceItems(ctx, in.ID, items, parents); err != nil {
		return nil, mapError(err)
	}

	return h.getItems(ctx, &idInput{ID: in.ID})
}

// ---------- Public 处理器 ----------

func (h *Handler) publicGet(ctx context.Context, in *slugInput) (*itemsOutput, error) {
	m, err := h.store.GetBySlug(ctx, in.Slug)
	if err != nil {
		return nil, mapError(err)
	}
	items, err := h.store.Items(ctx, m.ID)
	if err != nil {
		return nil, err
	}

	// 解析站内条目的地址，剔除指向已删除或未发布记录的那些。
	kept, dropped, err := h.resolver.resolveAll(ctx, items)
	if err != nil {
		return nil, err
	}
	if dropped > 0 && h.logger != nil {
		h.logger.Warn("菜单中存在指向无效记录的条目，已在渲染时跳过",
			"menu", m.Slug, "dropped", dropped)
	}

	tree := VisibleTree(BuildTree(kept))
	out := &itemsOutput{}
	out.Body.Items = tree
	out.Body.Total = CountItems(tree)
	return out, nil
}

// ---------- 工具 ----------

// flatten 把嵌套的条目树摊平成深度优先序的条目与父项下标。
//
// 父项下标用切片位置表达，而不是客户端的条目 ID：条目在保存时会被整体重建，
// 客户端手里的 ID 保存完就作废，用它们表达父子关系会在第二次保存时错位。
func flatten(nodes []menuItemInput) ([]Item, []int, error) {
	if len(nodes) > maxItems {
		return nil, nil, huma.Error400BadRequest("条目数超过上限 " + strconv.Itoa(maxItems))
	}

	items := make([]Item, 0, len(nodes))
	parents := make([]int, 0, len(nodes))
	var walk func(nodes []menuItemInput, parent int, depth int) error

	walk = func(nodes []menuItemInput, parent, depth int) error {
		// 空数组必须**先**返回：调用方在这一层没有子节点，不代表层级超限。
		// 少了这一句，恰好三级、且第三级没有 children 的合法菜单会以 depth=4
		// 递归进来，被下面的检查判成四级而拒绝。
		if len(nodes) == 0 {
			return nil
		}
		if depth > maxItemDepth {
			return huma.Error400BadRequest("菜单层级最多 " + strconv.Itoa(maxItemDepth) + " 级")
		}
		for i := range nodes {
			item, err := toItem(&nodes[i])
			if err != nil {
				return err
			}
			if len(items) >= maxItems {
				return huma.Error400BadRequest("条目数超过上限 " + strconv.Itoa(maxItems))
			}
			// DFS 序保证父项总是先于子项出现，故父项下标可以直接写位置值。
			items = append(items, item)
			parents = append(parents, parent)
			index := len(items) - 1
			if err := walk(nodes[i].Children, index, depth+1); err != nil {
				return err
			}
		}
		return nil
	}

	if err := walk(nodes, -1, 1); err != nil {
		return nil, nil, err
	}
	return items, parents, nil
}

// toItem 把请求体里的一个条目转成实体，并做形态校验。
func toItem(node *menuItemInput) (Item, error) {
	label := strings.TrimSpace(node.Label)
	if label == "" {
		return Item{}, huma.Error400BadRequest("条目标题不能为空白")
	}

	item := Item{
		Label:   label,
		Type:    ItemType(node.Type),
		Target:  node.Target,
		Rel:     strings.TrimSpace(node.Rel),
		Visible: node.Visible == nil || *node.Visible,
	}

	switch item.Type {
	case TypeCustom:
		url := strings.TrimSpace(node.URL)
		if url == "" {
			return Item{}, huma.Error400BadRequest("自定义链接必须填写地址")
		}
		item.URL = url
	default:
		if node.TargetID == nil {
			return Item{}, huma.Error400BadRequest("站内条目必须指定 targetId")
		}
		item.TargetID = node.TargetID
	}
	return item, nil
}

// cleanName 去除首尾空白并拒绝空白名称。
func cleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", huma.Error400BadRequest("菜单名称不能为空白")
	}
	return name, nil
}

// resolveSlug 把用户给定的 slug 规范化，或由名称生成。
func resolveSlug(provided, name string) (string, error) {
	var s string
	if strings.TrimSpace(provided) != "" {
		s = slug.Make(provided)
	} else {
		s = slug.Make(name)
	}
	if s == "" {
		return "", huma.Error400BadRequest("无法生成菜单标识，请手动指定")
	}
	return s, nil
}

// mapError 把存储层的哨兵错误映射为 HTTP 错误；其余错误交给 huma 按 500 处理。
func mapError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrNotFound):
		return huma.Error404NotFound(err.Error())
	case errors.Is(err, ErrSlugTaken):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, ErrInvalid), errors.Is(err, ErrCycle), errors.Is(err, ErrTargetNotFound):
		return huma.Error400BadRequest(err.Error())
	}
	return err
}
