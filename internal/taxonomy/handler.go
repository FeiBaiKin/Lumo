package taxonomy

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/FeiBaiKin/lumo/internal/api"
	"github.com/FeiBaiKin/lumo/internal/auth"
	"github.com/FeiBaiKin/lumo/internal/auth/perm"
	"github.com/FeiBaiKin/lumo/internal/slug"
)

// OpenAPI 分组标签。
var (
	tagCategories = []string{"categories"}
	tagTags       = []string{"tags"}
)

// 路由路径。Console 与 Public 平面共用相对路径，前缀由各平面的分组附加。
const (
	pathCategories   = "/categories"
	pathCategoryTree = "/categories/tree"
	pathCategoryByID = "/categories/{id}"
	pathTags         = "/tags"
	pathTagByID      = "/tags/{id}"
)

// maxSlugRetries 是自动生成的 slug 冲突时追加序号重试的上限。
const maxSlugRetries = 20

// reservedCategorySlugs 是与固定路径冲突、不能用作分类 slug 的值。
var reservedCategorySlugs = map[string]bool{"tree": true}

// Slugger 由文本生成 slug；站点设置决定保留中文还是转拼音。
type Slugger func(ctx context.Context, text string) string

// Handler 提供分类与标签的 Console 与 Public 接口。
type Handler struct {
	store   *Store
	slugify Slugger
}

// NewHandler 构造 Handler；slugify 为 nil 时使用保留中文的缺省策略。
func NewHandler(store *Store, slugify Slugger) *Handler {
	if slugify == nil {
		slugify = func(_ context.Context, text string) string { return slug.Make(text) }
	}
	return &Handler{store: store, slugify: slugify}
}

// Register 挂载接口：Console 平面读操作对任何已认证用户开放（作者选分类需要），
// 写操作要求 taxonomies:manage；Public 平面匿名只读。
func (h *Handler) Register(console, public huma.API) {
	manage := huma.Middlewares{auth.RequirePermission(perm.TaxonomiesManage)}

	// ---- Console · 分类 ----
	huma.Register(console, huma.Operation{
		OperationID: "taxonomy-page-categories",
		Method:      http.MethodGet,
		Path:        pathCategories,
		Summary:     "分页列出分类",
		Description: "平铺列表，根分类在前，同级按 position 排序。需要树形结构请用 /categories/tree。",
		Tags:        tagCategories,
	}, h.pageCategories)
	huma.Register(console, huma.Operation{
		OperationID: "taxonomy-category-tree",
		Method:      http.MethodGet,
		Path:        pathCategoryTree,
		Summary:     "分类树",
		Tags:        tagCategories,
	}, h.categoryTree)
	huma.Register(console, huma.Operation{
		OperationID: "taxonomy-get-category",
		Method:      http.MethodGet,
		Path:        pathCategoryByID,
		Summary:     "获取分类",
		Tags:        tagCategories,
		Errors:      []int{http.StatusNotFound},
	}, h.getCategory)
	huma.Register(console, huma.Operation{
		OperationID:   "taxonomy-create-category",
		Method:        http.MethodPost,
		Path:          pathCategories,
		Summary:       "创建分类",
		Description:   "slug 留空则由名称生成（保留中文）；position 留空则排在同级末尾。",
		Tags:          tagCategories,
		DefaultStatus: http.StatusCreated,
		Middlewares:   manage,
		Errors:        []int{http.StatusBadRequest, http.StatusForbidden, http.StatusConflict},
	}, h.createCategory)
	huma.Register(console, huma.Operation{
		OperationID: "taxonomy-update-category",
		Method:      http.MethodPut,
		Path:        pathCategoryByID,
		Summary:     "更新分类",
		Description: "整体替换可编辑字段。slug 留空则保留原值（改 slug 会让既有链接失效），" +
			"position 留空则保留原值；parentId 留空表示移到根。",
		Tags:        tagCategories,
		Middlewares: manage,
		Errors:      []int{http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound, http.StatusConflict},
	}, h.updateCategory)
	huma.Register(console, huma.Operation{
		OperationID:   "taxonomy-delete-category",
		Method:        http.MethodDelete,
		Path:          pathCategoryByID,
		Summary:       "删除分类",
		Description:   "子分类会挂到被删分类的父分类下，不做级联删除。",
		Tags:          tagCategories,
		DefaultStatus: http.StatusNoContent,
		Middlewares:   manage,
		Errors:        []int{http.StatusForbidden, http.StatusNotFound},
	}, h.deleteCategory)

	// ---- Console · 标签 ----
	huma.Register(console, huma.Operation{
		OperationID: "taxonomy-page-tags",
		Method:      http.MethodGet,
		Path:        pathTags,
		Summary:     "分页列出标签",
		Tags:        tagTags,
	}, h.pageTags)
	huma.Register(console, huma.Operation{
		OperationID: "taxonomy-get-tag",
		Method:      http.MethodGet,
		Path:        pathTagByID,
		Summary:     "获取标签",
		Tags:        tagTags,
		Errors:      []int{http.StatusNotFound},
	}, h.getTag)
	huma.Register(console, huma.Operation{
		OperationID:   "taxonomy-create-tag",
		Method:        http.MethodPost,
		Path:          pathTags,
		Summary:       "创建标签",
		Description:   "slug 留空则由名称生成（保留中文）。",
		Tags:          tagTags,
		DefaultStatus: http.StatusCreated,
		Middlewares:   manage,
		Errors:        []int{http.StatusBadRequest, http.StatusForbidden, http.StatusConflict},
	}, h.createTag)
	huma.Register(console, huma.Operation{
		OperationID: "taxonomy-update-tag",
		Method:      http.MethodPut,
		Path:        pathTagByID,
		Summary:     "更新标签",
		Description: "整体替换可编辑字段；slug 留空则保留原值。",
		Tags:        tagTags,
		Middlewares: manage,
		Errors:      []int{http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound, http.StatusConflict},
	}, h.updateTag)
	huma.Register(console, huma.Operation{
		OperationID:   "taxonomy-delete-tag",
		Method:        http.MethodDelete,
		Path:          pathTagByID,
		Summary:       "删除标签",
		Tags:          tagTags,
		DefaultStatus: http.StatusNoContent,
		Middlewares:   manage,
		Errors:        []int{http.StatusForbidden, http.StatusNotFound},
	}, h.deleteTag)

	// ---- Public ----
	huma.Register(public, huma.Operation{
		OperationID: "taxonomy-public-list-categories",
		Method:      http.MethodGet,
		Path:        pathCategories,
		Summary:     "全部分类（平铺）",
		Tags:        tagCategories,
	}, h.publicCategories)
	huma.Register(public, huma.Operation{
		OperationID: "taxonomy-public-category-tree",
		Method:      http.MethodGet,
		Path:        pathCategoryTree,
		Summary:     "分类树",
		Tags:        tagCategories,
	}, h.categoryTree)
	huma.Register(public, huma.Operation{
		OperationID: "taxonomy-public-get-category",
		Method:      http.MethodGet,
		Path:        "/categories/{slug}",
		Summary:     "按 slug 获取分类",
		Tags:        tagCategories,
		Errors:      []int{http.StatusNotFound},
	}, h.publicCategoryBySlug)
	huma.Register(public, huma.Operation{
		OperationID: "taxonomy-public-page-tags",
		Method:      http.MethodGet,
		Path:        pathTags,
		Summary:     "分页列出标签",
		Tags:        tagTags,
	}, h.pageTags)
	huma.Register(public, huma.Operation{
		OperationID: "taxonomy-public-get-tag",
		Method:      http.MethodGet,
		Path:        "/tags/{slug}",
		Summary:     "按 slug 获取标签",
		Tags:        tagTags,
		Errors:      []int{http.StatusNotFound},
	}, h.publicTagBySlug)
}

// ---------- 输入输出 ----------

type idInput struct {
	ID int64 `path:"id" minimum:"1"`
}

type slugInput struct {
	Slug string `path:"slug" minLength:"1" maxLength:"128"`
}

// categoryBody 是创建与更新分类共用的请求体。
type categoryBody struct {
	Name        string `json:"name" minLength:"1" maxLength:"64" doc:"名称，同一父分类下唯一"`
	Slug        string `json:"slug,omitempty" maxLength:"128" doc:"URL 片段；留空则由名称生成"`
	ParentID    *int64 `json:"parentId,omitempty" minimum:"1" doc:"父分类 ID；留空为根分类"`
	Description string `json:"description,omitempty" maxLength:"1000" doc:"描述"`
	CoverURL    string `json:"coverUrl,omitempty" maxLength:"1024" doc:"封面图地址"`
	Position    *int   `json:"position,omitempty" doc:"同级排序；创建时留空排在末尾，更新时留空保留原值"`
}

type categoryInput struct {
	Body categoryBody
}

type categoryUpdateInput struct {
	ID   int64 `path:"id" minimum:"1"`
	Body categoryBody
}

type categoryOutput struct {
	Body Category
}

type categoryPageOutput struct {
	Body api.Page[Category]
}

// categoryList 是不分页的分类列表。
type categoryList struct {
	Items []Category `json:"items"`
}

type categoryListOutput struct {
	Body categoryList
}

// categoryTree 是分类森林。
type categoryTree struct {
	Items []*CategoryNode `json:"items" doc:"根分类，按 position 排序；每个节点携带其子树"`
}

type categoryTreeOutput struct {
	Body categoryTree
}

// tagBody 是创建与更新标签共用的请求体。
type tagBody struct {
	Name        string `json:"name" minLength:"1" maxLength:"64" doc:"名称，大小写不敏感唯一"`
	Slug        string `json:"slug,omitempty" maxLength:"128" doc:"URL 片段；留空则由名称生成"`
	Description string `json:"description,omitempty" maxLength:"1000" doc:"描述"`
	Color       string `json:"color,omitempty" pattern:"^#[0-9a-fA-F]{6}$" doc:"展示颜色，如 #3b82f6"`
}

type tagInput struct {
	Body tagBody
}

type tagUpdateInput struct {
	ID   int64 `path:"id" minimum:"1"`
	Body tagBody
}

type tagOutput struct {
	Body Tag
}

type tagPageInput struct {
	api.PageParams
	Q string `query:"q" maxLength:"64" doc:"按名称或 slug 模糊筛选"`
}

type tagPageOutput struct {
	Body api.Page[Tag]
}

// ---------- 分类处理器 ----------

func (h *Handler) pageCategories(ctx context.Context, in *api.PageParams) (*categoryPageOutput, error) {
	items, total, err := h.store.PageCategories(ctx, *in)
	if err != nil {
		return nil, err
	}
	return &categoryPageOutput{Body: api.NewPage(items, *in, total)}, nil
}

func (h *Handler) categoryTree(ctx context.Context, _ *struct{}) (*categoryTreeOutput, error) {
	all, err := h.store.ListCategories(ctx)
	if err != nil {
		return nil, err
	}
	return &categoryTreeOutput{Body: categoryTree{Items: BuildTree(all)}}, nil
}

func (h *Handler) publicCategories(ctx context.Context, _ *struct{}) (*categoryListOutput, error) {
	all, err := h.store.ListCategories(ctx)
	if err != nil {
		return nil, err
	}
	return &categoryListOutput{Body: categoryList{Items: all}}, nil
}

func (h *Handler) getCategory(ctx context.Context, in *idInput) (*categoryOutput, error) {
	c, err := h.store.GetCategory(ctx, in.ID)
	if err != nil {
		return nil, mapError(err)
	}
	return &categoryOutput{Body: *c}, nil
}

func (h *Handler) publicCategoryBySlug(ctx context.Context, in *slugInput) (*categoryOutput, error) {
	c, err := h.store.GetCategoryBySlug(ctx, in.Slug)
	if err != nil {
		return nil, mapError(err)
	}
	return &categoryOutput{Body: *c}, nil
}

func (h *Handler) createCategory(ctx context.Context, in *categoryInput) (*categoryOutput, error) {
	name, err := cleanName(in.Body.Name)
	if err != nil {
		return nil, err
	}
	s, err := h.resolveSlug(ctx, in.Body.Slug, name, "", reservedCategorySlugs)
	if err != nil {
		return nil, err
	}

	position := 0
	if in.Body.Position != nil {
		position = *in.Body.Position
	} else {
		next, posErr := h.store.NextPosition(ctx, in.Body.ParentID)
		if posErr != nil {
			return nil, posErr
		}
		position = next
	}

	c := &Category{
		ParentID:    in.Body.ParentID,
		Name:        name,
		Slug:        s,
		Description: in.Body.Description,
		CoverURL:    in.Body.CoverURL,
		Position:    position,
	}
	err = withSlugRetry(isGenerated(in.Body.Slug), s, func(candidate string) error {
		c.Slug = candidate
		return h.store.CreateCategory(ctx, c)
	})
	if err != nil {
		return nil, mapError(err)
	}
	return &categoryOutput{Body: *c}, nil
}

func (h *Handler) updateCategory(ctx context.Context, in *categoryUpdateInput) (*categoryOutput, error) {
	current, err := h.store.GetCategory(ctx, in.ID)
	if err != nil {
		return nil, mapError(err)
	}
	name, err := cleanName(in.Body.Name)
	if err != nil {
		return nil, err
	}
	s, err := h.resolveSlug(ctx, in.Body.Slug, name, current.Slug, reservedCategorySlugs)
	if err != nil {
		return nil, err
	}

	current.ParentID = in.Body.ParentID
	current.Name = name
	current.Slug = s
	current.Description = in.Body.Description
	current.CoverURL = in.Body.CoverURL
	if in.Body.Position != nil {
		current.Position = *in.Body.Position
	}
	if err := h.store.UpdateCategory(ctx, current); err != nil {
		return nil, mapError(err)
	}
	return &categoryOutput{Body: *current}, nil
}

func (h *Handler) deleteCategory(ctx context.Context, in *idInput) (*struct{}, error) {
	if err := h.store.DeleteCategory(ctx, in.ID); err != nil {
		return nil, mapError(err)
	}
	return nil, nil //nolint:nilnil // 无响应体，huma 按 DefaultStatus 返回 204
}

// ---------- 标签处理器 ----------

func (h *Handler) pageTags(ctx context.Context, in *tagPageInput) (*tagPageOutput, error) {
	items, total, err := h.store.PageTags(ctx, in.Q, in.PageParams)
	if err != nil {
		return nil, err
	}
	return &tagPageOutput{Body: api.NewPage(items, in.PageParams, total)}, nil
}

func (h *Handler) getTag(ctx context.Context, in *idInput) (*tagOutput, error) {
	tag, err := h.store.GetTag(ctx, in.ID)
	if err != nil {
		return nil, mapError(err)
	}
	return &tagOutput{Body: *tag}, nil
}

func (h *Handler) publicTagBySlug(ctx context.Context, in *slugInput) (*tagOutput, error) {
	tag, err := h.store.GetTagBySlug(ctx, in.Slug)
	if err != nil {
		return nil, mapError(err)
	}
	return &tagOutput{Body: *tag}, nil
}

func (h *Handler) createTag(ctx context.Context, in *tagInput) (*tagOutput, error) {
	name, err := cleanName(in.Body.Name)
	if err != nil {
		return nil, err
	}
	s, err := h.resolveSlug(ctx, in.Body.Slug, name, "", nil)
	if err != nil {
		return nil, err
	}
	tag := &Tag{
		Name:        name,
		Slug:        s,
		Description: in.Body.Description,
		Color:       strings.ToLower(in.Body.Color),
	}
	err = withSlugRetry(isGenerated(in.Body.Slug), s, func(candidate string) error {
		tag.Slug = candidate
		return h.store.CreateTag(ctx, tag)
	})
	if err != nil {
		return nil, mapError(err)
	}
	return &tagOutput{Body: *tag}, nil
}

func (h *Handler) updateTag(ctx context.Context, in *tagUpdateInput) (*tagOutput, error) {
	current, err := h.store.GetTag(ctx, in.ID)
	if err != nil {
		return nil, mapError(err)
	}
	name, err := cleanName(in.Body.Name)
	if err != nil {
		return nil, err
	}
	s, err := h.resolveSlug(ctx, in.Body.Slug, name, current.Slug, nil)
	if err != nil {
		return nil, err
	}

	current.Name = name
	current.Slug = s
	current.Description = in.Body.Description
	current.Color = strings.ToLower(in.Body.Color)
	if err := h.store.UpdateTag(ctx, current); err != nil {
		return nil, mapError(err)
	}
	return &tagOutput{Body: *current}, nil
}

func (h *Handler) deleteTag(ctx context.Context, in *idInput) (*struct{}, error) {
	if err := h.store.DeleteTag(ctx, in.ID); err != nil {
		return nil, mapError(err)
	}
	return nil, nil //nolint:nilnil // 无响应体，huma 按 DefaultStatus 返回 204
}

// ---------- 工具 ----------

// cleanName 去除首尾空白并拒绝空白名称：minLength 校验挡不住纯空格。
func cleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", huma.Error400BadRequest("名称不能为空白")
	}
	return name, nil
}

// resolveSlug 决定最终 slug：
//   - 调用方给了 slug：按统一规则规范化，无法规范化则报错；
//   - 没给但对象已有 slug（更新）：保留原值，避免既有链接失效；
//   - 都没有（创建）：按站点策略由名称生成，生成不出则要求手动指定。
func (h *Handler) resolveSlug(ctx context.Context, provided, name, current string, reserved map[string]bool) (string, error) {
	var s string
	switch {
	case strings.TrimSpace(provided) != "":
		s = slug.Make(provided)
		if s == "" {
			return "", huma.Error400BadRequest("slug 不合法：须包含字母或数字")
		}
	case current != "":
		return current, nil
	default:
		s = h.slugify(ctx, name)
		if s == "" {
			return "", huma.Error400BadRequest("无法从名称生成 slug，请手动指定")
		}
	}
	if reserved[s] {
		return "", huma.Error400BadRequest("slug 与保留路径冲突：" + s)
	}
	return s, nil
}

// isGenerated 报告 slug 是否将由名称自动生成（调用方未显式指定）。
func isGenerated(provided string) bool {
	return strings.TrimSpace(provided) == ""
}

// withSlugRetry 执行 create；当 slug 为自动生成且与既有对象冲突时，
// 依次追加 -2、-3…… 重试（WordPress 的做法）。用户显式指定的 slug 冲突直接报错，
// 不替用户改地址。
func withSlugRetry(generated bool, base string, create func(candidate string) error) error {
	err := create(base)
	if !generated || !errors.Is(err, ErrSlugTaken) {
		return err
	}
	for i := 2; i <= maxSlugRetries; i++ {
		err = create(withSuffix(base, "-"+strconv.Itoa(i)))
		if !errors.Is(err, ErrSlugTaken) {
			return err
		}
	}
	return err
}

// withSuffix 给 slug 追加后缀，超长时截短主体以保持在长度上限内。
func withSuffix(base, suffix string) string {
	runes := []rune(base)
	if keep := slug.MaxLength - len(suffix); len(runes) > keep {
		runes = runes[:keep]
	}
	return strings.TrimRight(string(runes), "-") + suffix
}

// mapError 把存储层的哨兵错误映射为 HTTP 错误；其余错误交给 huma 按 500 处理。
func mapError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrNotFound):
		return huma.Error404NotFound(err.Error())
	case errors.Is(err, ErrSlugTaken), errors.Is(err, ErrNameTaken):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, ErrParentNotFound), errors.Is(err, ErrCycle), errors.Is(err, ErrInvalid):
		return huma.Error400BadRequest(err.Error())
	}
	return err
}
