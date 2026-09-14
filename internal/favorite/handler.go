package favorite

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/FeiBaiKin/lumo/internal/auth"
)

// tagFavorites 是 OpenAPI 分组标签。
var tagFavorites = []string{"favorites"}

// pathPostFavorite 是收藏标记的路径。
//
// 单数而不是 /favorites：这个资源是「当前调用者对这篇内容的那一个收藏标记」，
// 每人每篇至多一个，故它是单例子资源——PUT 置上、DELETE 取消、GET 读状态，
// 三个动词各自幂等。评论那边是集合（一篇内容有很多条），所以是复数。
const pathPostFavorite = "/posts/{postId}/favorite"

// Handler 提供收藏的 Public 接口。
//
// 没有 Console 接口：后台管不了别人收藏了什么，那是访客的私人清单，
// 站长要看的「这篇被收了多少次」在文章页上就有。
type Handler struct {
	store *Store
}

// NewHandler 构造 Handler。
func NewHandler(store *Store) *Handler { return &Handler{store: store} }

// Register 挂载接口。只有 Public 平面。
func (h *Handler) Register(public huma.API) {
	huma.Register(public, huma.Operation{
		OperationID: "favorite-state",
		Method:      http.MethodGet,
		Path:        pathPostFavorite,
		Summary:     "获取收藏状态",
		Description: "返回这篇内容的收藏总数，以及当前调用者是否收藏过；匿名调用时后者恒为 false。",
		Tags:        tagFavorites,
		Errors:      []int{http.StatusNotFound},
	}, h.state)
	huma.Register(public, huma.Operation{
		OperationID: "favorite-add",
		Method:      http.MethodPut,
		Path:        pathPostFavorite,
		Summary:     "收藏内容",
		Description: "需要登录。重复收藏不报错，响应里的状态即为最终状态。",
		Tags:        tagFavorites,
		Errors:      []int{http.StatusUnauthorized, http.StatusNotFound},
	}, h.add)
	huma.Register(public, huma.Operation{
		OperationID: "favorite-remove",
		Method:      http.MethodDelete,
		Path:        pathPostFavorite,
		Summary:     "取消收藏",
		Description: "需要登录。未收藏过也不报错。",
		Tags:        tagFavorites,
		Errors:      []int{http.StatusUnauthorized, http.StatusNotFound},
	}, h.remove)
}

// ---------- 输入输出 ----------

type postInput struct {
	PostID int64 `path:"postId" minimum:"1"`
}

// stateOutput 是三个操作共用的响应：一次调用之后的最终状态。
//
// 三者同形是有意的：前端拿到响应就能直接把按钮画对，
// 不必再发一次 GET 去问「那现在到底是什么状态」。
type stateOutput struct {
	Body struct {
		Favorited bool `json:"favorited" doc:"当前调用者是否已收藏；匿名调用恒为 false"`
		Count     int  `json:"count" doc:"这篇内容被收藏的次数"`
	}
}

// ---------- 处理器 ----------

func (h *Handler) state(ctx context.Context, in *postInput) (*stateOutput, error) {
	if err := h.store.VisiblePost(ctx, in.PostID); err != nil {
		return nil, mapError(err)
	}
	return h.result(ctx, viewerID(ctx), in.PostID)
}

func (h *Handler) add(ctx context.Context, in *postInput) (*stateOutput, error) {
	userID, err := requireViewer(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.store.VisiblePost(ctx, in.PostID); err != nil {
		return nil, mapError(err)
	}
	if _, err := h.store.Add(ctx, userID, in.PostID); err != nil {
		return nil, mapError(err)
	}
	return h.result(ctx, userID, in.PostID)
}

func (h *Handler) remove(ctx context.Context, in *postInput) (*stateOutput, error) {
	userID, err := requireViewer(ctx)
	if err != nil {
		return nil, err
	}
	// 取消收藏**不**校验内容可见性：文章被撤回之后，用户仍然该能把它从
	// 自己的收藏里划掉。这与收藏（写入）的方向相反，那里要拦住新的引用。
	if _, err := h.store.Remove(ctx, userID, in.PostID); err != nil {
		return nil, mapError(err)
	}
	return h.result(ctx, userID, in.PostID)
}

// result 读出这篇内容当前的收藏状态。
func (h *Handler) result(ctx context.Context, userID, postID int64) (*stateOutput, error) {
	count, err := h.store.CountFavorites(ctx, postID)
	if err != nil {
		return nil, err
	}
	favorited := false
	if userID > 0 {
		if favorited, err = h.store.HasFavorite(ctx, userID, postID); err != nil {
			return nil, err
		}
	}
	out := &stateOutput{}
	out.Body.Favorited = favorited
	out.Body.Count = count
	return out, nil
}

// viewerID 返回调用者的用户 ID；匿名为 0。
func viewerID(ctx context.Context) int64 {
	principal, ok := auth.FromContext(ctx)
	if !ok {
		return 0
	}
	return principal.UserID()
}

// requireViewer 取调用者的用户 ID，匿名时返回 401。
//
// 不用 auth.RequireAuth 之类的中间件：Public 平面整体是「解析凭据但不强制」，
// 而本模块三个操作里只有两个需要登录，逐个操作自己判断比给平面加一层例外清楚。
func requireViewer(ctx context.Context) (int64, error) {
	if id := viewerID(ctx); id > 0 {
		return id, nil
	}
	return 0, huma.Error401Unauthorized("请先登录后再收藏")
}

// mapError 把存储层的哨兵错误映射为 HTTP 状态。
func mapError(err error) error {
	if errors.Is(err, ErrPostNotFound) {
		// 与正文一样用 404 而不是 403：不暴露该内容是否存在。
		return huma.Error404NotFound(err.Error())
	}
	return err
}
