package extension

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/FeiBaiKin/lumo/internal/api"
	"github.com/FeiBaiKin/lumo/internal/auth"
	"github.com/FeiBaiKin/lumo/internal/auth/perm"
)

// tagExtensions 是 OpenAPI 分组标签。
var tagExtensions = []string{"extensions"}

// 路由路径，前缀 /apis 由 Extension 平面的分组附加。
const (
	pathCollection = "/{group}/{version}/{resource}"
	pathObject     = "/{group}/{version}/{resource}/{name}"
)

// maxWhereClauses 是 where 筛选条件的条数上限。
const maxWhereClauses = 8

// Handler 提供 Extension 平面的通用 CRUD。
type Handler struct {
	store *Store
}

// NewHandler 构造 Handler。
func NewHandler(store *Store) *Handler { return &Handler{store: store} }

// Register 挂载接口。
//
// 五个操作都要求 extensions:manage：扩展记录是站点级数据，不存在「自己的记录」
// 这种所有权概念；读也一样要权限——spec 里可能是任何东西，不该对每个登录用户默认可见。
func (h *Handler) Register(extension huma.API) {
	manage := huma.Middlewares{auth.RequirePermission(perm.ExtensionsManage)}

	huma.Register(extension, huma.Operation{
		OperationID: "extension-list",
		Method:      http.MethodGet,
		Path:        pathCollection,
		Summary:     "列出某个资源下的扩展记录",
		Description: "where 可重复，形如 where=status=draft，对 spec 的顶层字段做等值筛选。",
		Tags:        tagExtensions,
		Middlewares: manage,
	}, h.list)
	huma.Register(extension, huma.Operation{
		OperationID: "extension-get",
		Method:      http.MethodGet,
		Path:        pathObject,
		Summary:     "获取一条扩展记录",
		Tags:        tagExtensions,
		Middlewares: manage,
		Errors:      []int{http.StatusNotFound},
	}, h.get)
	huma.Register(extension, huma.Operation{
		OperationID: "extension-create",
		Method:      http.MethodPost,
		Path:        pathCollection,
		Summary:     "创建一条扩展记录",
		Description: "kind 须为单数 PascalCase，且其复数形式与地址里的资源段一致（Post 对应 posts）。" +
			"同一资源段上的 kind 拼法由第一条记录固定。",
		Tags:          tagExtensions,
		DefaultStatus: http.StatusCreated,
		Middlewares:   manage,
		Errors:        []int{http.StatusConflict, http.StatusUnprocessableEntity},
	}, h.create)
	huma.Register(extension, huma.Operation{
		OperationID: "extension-update",
		Method:      http.MethodPut,
		Path:        pathObject,
		Summary:     "整体替换一条扩展记录的 spec",
		Description: "只替换 spec；请求体里的 kind 与 name 若出现，须与地址一致。" +
			"记录不存在时返回 404，不会顺手创建。",
		Tags:        tagExtensions,
		Middlewares: manage,
		Errors:      []int{http.StatusNotFound, http.StatusUnprocessableEntity},
	}, h.update)
	huma.Register(extension, huma.Operation{
		OperationID:   "extension-delete",
		Method:        http.MethodDelete,
		Path:          pathObject,
		Summary:       "删除一条扩展记录",
		Tags:          tagExtensions,
		DefaultStatus: http.StatusNoContent,
		Middlewares:   manage,
		Errors:        []int{http.StatusNotFound},
	}, h.delete)
}

// ---------- 输入输出 ----------

// ResourceParams 是三段寻址前缀，嵌入到各输入结构体。
//
// 模式同时写进 OpenAPI 的参数声明，形态不合法的地址由 huma 在进入处理器之前拦下。
type ResourceParams struct {
	Group    string `path:"group" maxLength:"253" pattern:"^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)+$" doc:"反向域名形式的 API 分组，如 io.github.feibaiikin.lumo"`
	Version  string `path:"version" maxLength:"32" pattern:"^v[1-9]\\d*((alpha|beta)[1-9]\\d*)?$" doc:"API 版本，如 v1alpha1"`
	Resource string `path:"resource" maxLength:"64" pattern:"^[a-z][a-z0-9]*$" doc:"kind 的小写复数形式，如 posts"`
}

// NameParam 是记录名路径参数。
type NameParam struct {
	Name string `path:"name" maxLength:"253" pattern:"^[a-z0-9]([-a-z0-9]*[a-z0-9])?$" doc:"DNS-1123 名称"`
}

type listInput struct {
	ResourceParams
	api.PageParams
	Sort string `query:"sort" enum:"name,-name,createdAt,-createdAt" doc:"排序方式，缺省按 name 升序"`
	// explode 让 where 按重复参数解析；huma 对切片默认按逗号切分，而筛选值里可能带逗号。
	Where []string `query:"where,explode" doc:"对 spec 顶层字段的等值筛选，形如 key=value，可重复；值写 true / false 或数字时按该类型比较"`
}

type objectInput struct {
	ResourceParams
	NameParam
}

type createInput struct {
	ResourceParams
	Body extensionCreateBody
}

type extensionCreateBody struct {
	Kind string         `json:"kind" minLength:"1" maxLength:"63" pattern:"^[A-Z][A-Za-z0-9]*$" doc:"单数 PascalCase 的资源种类，如 Post"`
	Name string         `json:"name" minLength:"1" maxLength:"253" pattern:"^[a-z0-9]([-a-z0-9]*[a-z0-9])?$" doc:"DNS-1123 名称，在同一资源下唯一"`
	Spec map[string]any `json:"spec,omitempty" doc:"自定义字段，服务端不解释其内容"`
}

type updateInput struct {
	ResourceParams
	NameParam
	Body extensionUpdateBody
}

// extensionUpdateBody 与创建体同形，但 kind 与 name 只参与一致性校验、不参与写入：
// 客户端可以拿它断言「我改的确实是我以为的那条」，改地址得走删了重建。
//
// 只收这三个字段，而不是让客户端把 GET 到的整条记录原样 PUT 回来——请求体的未知字段
// 一律拒绝，响应里的 id / selfLink / 时间戳回传过来只会得到 422。
type extensionUpdateBody struct {
	Kind string         `json:"kind,omitempty" maxLength:"63" doc:"若出现，须与该记录一致"`
	Name string         `json:"name,omitempty" maxLength:"253" doc:"若出现，须与地址一致"`
	Spec map[string]any `json:"spec" doc:"整体替换的自定义字段"`
}

type extensionOutput struct {
	Body Extension
}

type pageOutput struct {
	Body api.Page[Extension]
}

// ---------- 处理器 ----------

func (h *Handler) list(ctx context.Context, in *listInput) (*pageOutput, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}
	match, err := parseWhere(in.Where)
	if err != nil {
		return nil, err
	}

	items, total, err := h.store.List(ctx, &ListParams{
		Group:    in.Group,
		Version:  in.Version,
		Resource: in.Resource,
		Match:    match,
		Sort:     in.Sort,
		Page:     in.PageParams,
	})
	if err != nil {
		return nil, mapError(err)
	}
	return &pageOutput{Body: api.NewPage(items, in.PageParams, total)}, nil
}

func (h *Handler) get(ctx context.Context, in *objectInput) (*extensionOutput, error) {
	ref, err := in.ref()
	if err != nil {
		return nil, err
	}
	e, err := h.store.Get(ctx, ref)
	if err != nil {
		return nil, mapError(err)
	}
	return &extensionOutput{Body: *e}, nil
}

func (h *Handler) create(ctx context.Context, in *createInput) (*extensionOutput, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}
	kind := strings.TrimSpace(in.Body.Kind)
	if err := ValidateKind(kind); err != nil {
		return nil, invalid(err, "body.kind")
	}
	// 资源段由 kind 推导而来，两者不一致意味着这条记录会落在一个取不回来的地址上。
	if got := Resource(kind); got != in.Resource {
		return nil, invalid(
			fmt.Errorf("kind %q 对应的资源段是 %q，与地址里的 %q 不一致", kind, got, in.Resource),
			"body.kind")
	}
	name := strings.TrimSpace(in.Body.Name)
	if err := ValidateName(name); err != nil {
		return nil, invalid(err, "body.name")
	}

	ref := Ref{Group: in.Group, Version: in.Version, Resource: in.Resource, Name: name}
	registered, err := h.store.KindOf(ctx, ref)
	if err != nil {
		return nil, err
	}
	if registered != "" && registered != kind {
		return nil, huma.Error409Conflict(
			fmt.Sprintf("%s：%s 已登记为 %q", ErrKindConflict, in.Resource, registered))
	}

	e := &Extension{
		APIGroup: in.Group,
		Version:  in.Version,
		Kind:     kind,
		Name:     name,
		Spec:     in.Body.Spec,
	}
	if createErr := h.store.Create(ctx, e); createErr != nil {
		return nil, mapError(createErr)
	}
	return &extensionOutput{Body: *e}, nil
}

func (h *Handler) update(ctx context.Context, in *updateInput) (*extensionOutput, error) {
	ref, err := in.ref()
	if err != nil {
		return nil, err
	}
	if name := strings.TrimSpace(in.Body.Name); name != "" && name != ref.Name {
		return nil, invalid(fmt.Errorf("请求体里的 name %q 与地址里的 %q 不一致", name, ref.Name), "body.name")
	}

	e, err := h.store.Get(ctx, ref)
	if err != nil {
		return nil, mapError(err)
	}
	if kind := strings.TrimSpace(in.Body.Kind); kind != "" && kind != e.Kind {
		return nil, invalid(fmt.Errorf("请求体里的 kind %q 与该记录的 %q 不一致", kind, e.Kind), "body.kind")
	}

	e.Spec = in.Body.Spec
	if updateErr := h.store.Update(ctx, e); updateErr != nil {
		return nil, mapError(updateErr)
	}
	return &extensionOutput{Body: *e}, nil
}

func (h *Handler) delete(ctx context.Context, in *objectInput) (*struct{}, error) {
	ref, err := in.ref()
	if err != nil {
		return nil, err
	}
	if err := h.store.Delete(ctx, ref); err != nil {
		return nil, mapError(err)
	}
	return nil, nil //nolint:nilnil // 无响应体，huma 按 DefaultStatus 返回 204
}

// ---------- 工具 ----------

// validate 复核三段寻址前缀。huma 已按 pattern 拦过一道，这里是同一套规则的
// 权威实现，也覆盖不经 huma 的调用路径。
func (p *ResourceParams) validate() error {
	if err := ValidateGroup(p.Group); err != nil {
		return invalid(err, "path.group")
	}
	if err := ValidateVersion(p.Version); err != nil {
		return invalid(err, "path.version")
	}
	if err := ValidateResource(p.Resource); err != nil {
		return invalid(err, "path.resource")
	}
	return nil
}

// ref 校验并组装寻址四元组。
func (in *objectInput) ref() (Ref, error) { return makeRef(&in.ResourceParams, in.Name) }

func (in *updateInput) ref() (Ref, error) { return makeRef(&in.ResourceParams, in.Name) }

func makeRef(p *ResourceParams, name string) (Ref, error) {
	if err := p.validate(); err != nil {
		return Ref{}, err
	}
	if err := ValidateName(name); err != nil {
		return Ref{}, invalid(err, "path.name")
	}
	return Ref{Group: p.Group, Version: p.Version, Resource: p.Resource, Name: name}, nil
}

// parseWhere 把 where=key=value 形式的条件解析成对 spec 的包含匹配。
//
// 只支持顶层字段的等值比较：jsonb 的 @> 正好是这个语义，且能走核心迁移建好的 GIN 索引。
// 值按 JSON 字面量解释——true / false / null 与数字各按其类型比较，其余一律当字符串。
func parseWhere(clauses []string) (map[string]any, error) {
	if len(clauses) == 0 {
		return nil, nil
	}
	if len(clauses) > maxWhereClauses {
		return nil, invalid(fmt.Errorf("筛选条件最多 %d 条", maxWhereClauses), "query.where")
	}

	out := make(map[string]any, len(clauses))
	for _, clause := range clauses {
		key, value, ok := strings.Cut(clause, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			return nil, invalid(fmt.Errorf("筛选条件 %q 须形如 key=value", clause), "query.where")
		}
		out[key] = jsonLiteral(value)
	}
	return out, nil
}

// jsonLiteral 把查询串里的值还原成对应的 JSON 类型。
func jsonLiteral(value string) any {
	switch value {
	case "true":
		return true
	case "false":
		return false
	case "null":
		return nil
	}
	if f, err := strconv.ParseFloat(value, 64); err == nil {
		return f
	}
	return value
}

// invalid 把一条校验失败包成带位置的 422，与设置、主题两处的字段级报错形态一致。
func invalid(err error, location string) error {
	return huma.Error422UnprocessableEntity("扩展记录校验失败",
		&huma.ErrorDetail{Message: err.Error(), Location: location})
}

// mapError 把存储层的哨兵错误映射成 HTTP 状态。
func mapError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrNotFound):
		return huma.Error404NotFound(err.Error())
	case errors.Is(err, ErrNameTaken), errors.Is(err, ErrKindConflict):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, ErrInvalid):
		return huma.Error422UnprocessableEntity(err.Error())
	}
	return err
}
