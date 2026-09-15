package search

import (
	"context"
	"net/http"
	"slices"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/FeiBaiKin/lumo/internal/api"
	"github.com/FeiBaiKin/lumo/internal/auth"
	"github.com/FeiBaiKin/lumo/internal/auth/perm"
)

// tagSearch 是 OpenAPI 分组标签。
var tagSearch = []string{"search"}

// 路由路径，前缀由各平面的分组附加。
const (
	pathSearch  = "/search"
	pathStatus  = "/search/status"
	pathReindex = "/search/reindex"
)

// maxQueryRunes 是关键词长度上限，与主题前台一致。
const maxQueryRunes = 128

// contentTypes 是可搜索的内容类型。
var contentTypes = []string{"post", "page"}

// Handler 提供搜索接口。
type Handler struct {
	service *Service
}

// NewHandler 构造 Handler。
func NewHandler(service *Service) *Handler { return &Handler{service: service} }

// Register 挂载接口。
//
// 搜索是匿名可用的公开能力，只返回已发布且公开的内容；
// 索引状态与重建属站点维护，走 Console 并复用 settings:manage——
// 为一个维护端点单开一条权限串，只会让角色编辑器多一个没人看得懂的选项。
func (h *Handler) Register(console, public huma.API) {
	huma.Register(public, huma.Operation{
		OperationID: "search",
		Method:      http.MethodGet,
		Path:        pathSearch,
		Summary:     "全文搜索",
		Description: "按相关度返回已发布且公开的内容。中文按二元组匹配，" +
			"多个关键词之间取交集；关键词切不出词元时返回空结果。",
		Tags:   tagSearch,
		Errors: []int{http.StatusUnprocessableEntity},
	}, h.search)

	manage := huma.Middlewares{auth.RequirePermission(perm.SettingsManage)}
	huma.Register(console, huma.Operation{
		OperationID: "search-status",
		Method:      http.MethodGet,
		Path:        pathStatus,
		Summary:     "查看索引进度",
		Description: "pending 为索引落后于内容的条数，后台每 2 秒对账一次，正常应当很快归零。",
		Tags:        tagSearch,
		Middlewares: manage,
	}, h.status)
	huma.Register(console, huma.Operation{
		OperationID: "search-reindex",
		Method:      http.MethodPost,
		Path:        pathReindex,
		Summary:     "重建全部索引",
		Description: "把全部内容标记为待索引，由后台逐批重建；接口立即返回，不等待重建完成。" +
			"切词规则升级后需要执行一次，否则只有此后被编辑过的内容才会用上新规则。",
		Tags:        tagSearch,
		Middlewares: manage,
	}, h.reindex)
}

// ---------- 输入输出 ----------

type searchInput struct {
	api.PageParams
	Q string `query:"q" maxLength:"128" doc:"关键词"`
	// 不给 enum 标签：huma 会把它加在数组本身而不是元素上，生成的规范反而是错的。
	Types []string `query:"type,explode" doc:"限定内容类型，可重复，取值 post 或 page；留空为不限"`
}

type searchOutput struct {
	Body api.Page[Hit]
}

type statusOutput struct {
	Body Stats
}

type reindexOutput struct {
	Body struct {
		Pending int `json:"pending" doc:"已标记为待索引的条数"`
	}
}

// ---------- 处理器 ----------

func (h *Handler) search(ctx context.Context, in *searchInput) (*searchOutput, error) {
	types, err := cleanTypes(in.Types)
	if err != nil {
		return nil, err
	}

	query := strings.TrimSpace(in.Q)
	if runes := []rune(query); len(runes) > maxQueryRunes {
		query = string(runes[:maxQueryRunes])
	}

	result, err := h.service.Search(ctx, &Params{Query: query, Types: types, Page: in.PageParams})
	if err != nil {
		return nil, err
	}
	return &searchOutput{Body: api.NewPage(result.Hits, in.PageParams, result.Total)}, nil
}

func (h *Handler) status(ctx context.Context, _ *struct{}) (*statusOutput, error) {
	stats, err := h.service.Stats(ctx)
	if err != nil {
		return nil, err
	}
	return &statusOutput{Body: stats}, nil
}

func (h *Handler) reindex(ctx context.Context, _ *struct{}) (*reindexOutput, error) {
	pending, err := h.service.Reindex(ctx)
	if err != nil {
		return nil, err
	}
	out := &reindexOutput{}
	out.Body.Pending = pending
	return out, nil
}

// cleanTypes 校验并去重内容类型。
func cleanTypes(types []string) ([]string, error) {
	if len(types) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(types))
	for _, t := range types {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if !slices.Contains(contentTypes, t) {
			return nil, huma.Error422UnprocessableEntity("搜索条件不合法",
				&huma.ErrorDetail{
					Message:  "内容类型只能是 post 或 page",
					Location: "query.type",
					Value:    t,
				})
		}
		if !slices.Contains(out, t) {
			out = append(out, t)
		}
	}
	return out, nil
}
