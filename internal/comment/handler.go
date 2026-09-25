package comment

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/danielgtaylor/huma/v2"

	"github.com/FeiBaiKin/lumo/internal/api"
	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/auth"
	"github.com/FeiBaiKin/lumo/internal/auth/perm"
	"github.com/FeiBaiKin/lumo/internal/hooks"
	"github.com/FeiBaiKin/lumo/internal/httpx"
)

// tagComments 是 OpenAPI 分组标签。
var tagComments = []string{"comments"}

// 路由路径。Console 与 Public 平面共用相对路径，前缀由各平面的分组附加。
const (
	pathComments    = "/comments"
	pathCommentByID = "/comments/{id}"
	// pathPostComments 挂在内容路径下，Public 用它按内容取评论。
	pathPostComments = "/posts/{postId}/comments"
)

// 访客字段的长度上限，与库中的 CHECK 约束一致。
const (
	maxAuthorName  = 64
	maxAuthorEmail = 254
)

// Handler 提供评论的 Console 与 Public 接口。
type Handler struct {
	store  *Store
	cfg    ConfigFunc
	spam   SpamChecker
	notify Notifier
	events app.Events
	logger *slog.Logger
}

// NewHandler 构造 Handler；notify 与 events 可为 nil，spam 为 nil 时用内置判定器。
func NewHandler(store *Store, cfg ConfigFunc, spam SpamChecker, notify Notifier, events app.Events, logger *slog.Logger) *Handler {
	if spam == nil {
		spam = NewSpamChecker()
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{store: store, cfg: cfg, spam: spam, notify: notify, events: events, logger: logger}
}

// emit 在评论变动之后通知插件。
func (h *Handler) emit(ctx context.Context, action string, c *Comment, post *PostRef) {
	if h.events != nil {
		h.events.Emit(ctx, action, hookComment(c, post))
	}
}

// hookComment 把评论转成派给插件的数据。
func hookComment(c *Comment, post *PostRef) hooks.Comment {
	out := hooks.Comment{
		ID:       c.ID,
		ParentID: c.ParentID,
		Author: hooks.CommentAuthor{
			Name: c.AuthorName, Email: c.AuthorEmail, URL: c.AuthorURL, UserID: c.UserID,
		},
		Content:   c.Content,
		Status:    string(c.Status),
		IP:        c.IP,
		UserAgent: c.UserAgent,
		CreatedAt: c.CreatedAt,
	}
	if post != nil {
		out.Post = hooks.PostRef{ID: post.ID, Type: post.Type, Title: post.Title, Path: hooks.PostPath(post.Type, post.Slug)}
	}
	return out
}

// Register 挂载接口。
func (h *Handler) Register(console, public huma.API) {
	manager := huma.Middlewares{auth.RequirePermission(perm.CommentsManage, perm.CommentsManageAny)}

	// ---- Console ----
	huma.Register(console, huma.Operation{
		OperationID: "comment-page",
		Method:      http.MethodGet,
		Path:        pathComments,
		Summary:     "分页列出评论",
		Description: "按发表时间倒序。没有 " + perm.CommentsManageAny.String() + " 权限的调用者只看到自己内容下的评论。",
		Tags:        tagComments,
		Middlewares: manager,
	}, h.page)
	huma.Register(console, huma.Operation{
		OperationID: "comment-get",
		Method:      http.MethodGet,
		Path:        pathCommentByID,
		Summary:     "获取评论",
		Tags:        tagComments,
		Middlewares: manager,
		Errors:      []int{http.StatusForbidden, http.StatusNotFound},
	}, h.get)
	huma.Register(console, huma.Operation{
		OperationID: "comment-approve",
		Method:      http.MethodPost,
		Path:        pathCommentByID + "/approve",
		Summary:     "通过审核",
		Tags:        tagComments,
		Middlewares: manager,
		Errors:      []int{http.StatusForbidden, http.StatusNotFound},
	}, h.approve)
	huma.Register(console, huma.Operation{
		OperationID: "comment-spam",
		Method:      http.MethodPost,
		Path:        pathCommentByID + "/spam",
		Summary:     "标记为垃圾",
		Description: "留档但不展示，便于日后申诉与调规则。",
		Tags:        tagComments,
		Middlewares: manager,
		Errors:      []int{http.StatusForbidden, http.StatusNotFound},
	}, h.markSpam)
	huma.Register(console, huma.Operation{
		OperationID:   "comment-delete",
		Method:        http.MethodDelete,
		Path:          pathCommentByID,
		Summary:       "删除评论",
		Description:   "其下回复一并删除，不可恢复。",
		Tags:          tagComments,
		DefaultStatus: http.StatusNoContent,
		Middlewares:   manager,
		Errors:        []int{http.StatusForbidden, http.StatusNotFound},
	}, h.delete)
	huma.Register(console, huma.Operation{
		OperationID:   "comment-reply",
		Method:        http.MethodPost,
		Path:          pathCommentByID + "/replies",
		Summary:       "以管理员身份回复",
		Description:   "回复一经创建即为已通过状态。",
		Tags:          tagComments,
		DefaultStatus: http.StatusCreated,
		Middlewares:   manager,
		Errors:        []int{http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound},
	}, h.reply)

	// ---- Public ----
	huma.Register(public, huma.Operation{
		OperationID: "comment-public-list",
		Method:      http.MethodGet,
		Path:        pathPostComments,
		Summary:     "获取内容的评论树",
		Description: "只返回已通过的评论。响应不含邮箱与 IP 等个人信息。",
		Tags:        tagComments,
	}, h.publicList)
	huma.Register(public, huma.Operation{
		OperationID:   "comment-public-create",
		Method:        http.MethodPost,
		Path:          pathPostComments,
		Summary:       "发表评论",
		Description:   "匿名亦可发表（受站点设置约束）。内容按纯文本处理，其中的 HTML 会被转义。",
		Tags:          tagComments,
		DefaultStatus: http.StatusCreated,
		Errors:        []int{http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound, http.StatusTooManyRequests},
	}, h.publicCreate)
}

// ---------- 输入输出 ----------

type idInput struct {
	ID int64 `path:"id" minimum:"1"`
}

type postInput struct {
	PostID int64 `path:"postId" minimum:"1"`
}

type pageInput struct {
	api.PageParams
	Status string `query:"status" enum:"pending,approved,spam" doc:"按审核状态筛选"`
	PostID int64  `query:"postId" minimum:"1" doc:"按内容筛选"`
	Q      string `query:"q" maxLength:"200" doc:"按正文或评论者名模糊筛选"`
}

// commentBody 是 Console 回复的请求体。
type commentBody struct {
	Content string `json:"content" minLength:"1" maxLength:"10000" doc:"纯文本内容"`
}

type commentInput struct {
	ID   int64 `path:"id" minimum:"1"`
	Body commentBody
}

type commentOutput struct {
	Body Comment
}

type pageOutput struct {
	Body api.Page[Comment]
}

// createBody 是访客发表评论的请求体。
type createBody struct {
	Content string `json:"content" minLength:"1" maxLength:"10000" doc:"纯文本内容，其中的 HTML 会被转义"`
	// ParentID 用于回复。
	ParentID *int64 `json:"parentId,omitempty" minimum:"1" doc:"被回复的评论 ID；顶层评论留空"`
	// 以下三项对已登录用户可省略：服务端改用账号信息。
	Name  string `json:"name,omitempty" maxLength:"64" doc:"访客昵称"`
	Email string `json:"email,omitempty" format:"email" doc:"访客邮箱，不会公开"`
	URL   string `json:"url,omitempty" maxLength:"512" doc:"访客主页地址"`
	// Honeypot 是对机器人设的陷阱，正常访客不会填。
	Honeypot string `json:"website2,omitempty" doc:"请勿填写"`
}

type createInput struct {
	PostID int64 `path:"postId" minimum:"1"`
	Body   createBody
}

type createOutput struct {
	Body struct {
		Comment Comment `json:"comment"`
		// Status 让前端知道该显示「已发表」还是「等待审核」。
		Status Status `json:"status"`
		Note   string `json:"note"`
	}
}

type treeOutput struct {
	Body struct {
		Items []*PublicView `json:"items"`
		Total int           `json:"total" doc:"树中的评论总数，含回复"`
	}
}

// ---------- Console 处理器 ----------

func (h *Handler) page(ctx context.Context, in *pageInput) (*pageOutput, error) {
	p := auth.MustFromContext(ctx)
	filter := Filter{Status: Status(in.Status), PostID: in.PostID, Q: in.Q}
	// 没有 _any 权限就只看自己内容下的评论。
	if !p.Has(perm.CommentsManageAny) {
		filter.AuthorID = p.UserID()
	}

	items, total, err := h.store.Page(ctx, filter, in.PageParams)
	if err != nil {
		return nil, err
	}
	return &pageOutput{Body: api.NewPage(items, in.PageParams, total)}, nil
}

func (h *Handler) get(ctx context.Context, in *idInput) (*commentOutput, error) {
	c, err := h.load(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	return &commentOutput{Body: *c}, nil
}

func (h *Handler) approve(ctx context.Context, in *idInput) (*commentOutput, error) {
	return h.setStatus(ctx, in.ID, StatusApproved)
}

func (h *Handler) markSpam(ctx context.Context, in *idInput) (*commentOutput, error) {
	return h.setStatus(ctx, in.ID, StatusSpam)
}

func (h *Handler) setStatus(ctx context.Context, id int64, status Status) (*commentOutput, error) {
	c, err := h.load(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := h.store.UpdateStatus(ctx, id, status); err != nil {
		return nil, mapError(err)
	}
	previous := c.Status
	c.Status = status
	if status == StatusApproved && previous != StatusApproved {
		if post, postErr := h.store.PostRef(ctx, c.PostID); postErr == nil {
			h.emit(ctx, hooks.CommentApproved, c, post)
		}
	}
	return &commentOutput{Body: *c}, nil
}

func (h *Handler) delete(ctx context.Context, in *idInput) (*struct{}, error) {
	if _, err := h.load(ctx, in.ID); err != nil {
		return nil, err
	}
	if err := h.store.Delete(ctx, in.ID); err != nil {
		return nil, mapError(err)
	}
	return nil, nil //nolint:nilnil // 无响应体，huma 按 DefaultStatus 返回 204
}

func (h *Handler) reply(ctx context.Context, in *commentInput) (*commentOutput, error) {
	parent, err := h.load(ctx, in.ID)
	if err != nil {
		return nil, err
	}

	content, err := cleanContent(in.Body.Content, 0) // 管理员回复只受硬上限约束
	if err != nil {
		return nil, err
	}
	principal := auth.MustFromContext(ctx)

	c := &Comment{
		PostID:      parent.PostID,
		ParentID:    &parent.ID,
		UserID:      &principal.User.ID,
		AuthorName:  principal.User.Name(),
		AuthorEmail: principal.User.Email,
		Content:     content,
		ContentHTML: Render(content),
		// 管理员自己发的一律直接通过：让站长审自己写的东西没有意义。
		Status: StatusApproved,
	}
	if err := h.store.Create(ctx, c); err != nil {
		return nil, mapError(err)
	}
	if post, postErr := h.store.PostRef(ctx, c.PostID); postErr == nil {
		h.emit(ctx, hooks.CommentCreated, c, post)
	}
	return &commentOutput{Body: *c}, nil
}

// load 取评论并在必要时校验所有权。
func (h *Handler) load(ctx context.Context, id int64) (*Comment, error) {
	c, err := h.store.Get(ctx, id)
	if err != nil {
		return nil, mapError(err)
	}
	if !h.canManage(ctx, c) {
		return nil, auth.ForbiddenProblem(perm.CommentsManageAny)
	}
	return c, nil
}

// canManage 报告调用者能否管理这条评论。
//
// 「自己内容下的评论」用内容作者判定，而不是评论作者：author 角色拿到的是
// 对**自己文章**的评论管理权，评论是谁写的并不重要。
func (h *Handler) canManage(ctx context.Context, c *Comment) bool {
	p := auth.MustFromContext(ctx)
	if p.Has(perm.CommentsManageAny) {
		return true
	}
	if !p.Has(perm.CommentsManage) {
		return false
	}
	return c.Post != nil && c.Post.AuthorID == p.UserID()
}

// ---------- Public 处理器 ----------

func (h *Handler) publicList(ctx context.Context, in *postInput) (*treeOutput, error) {
	post, err := h.publicPost(ctx, in.PostID)
	if err != nil {
		return nil, err
	}
	items, err := h.store.ListApproved(ctx, in.PostID)
	if err != nil {
		return nil, err
	}
	out := &treeOutput{}
	out.Body.Items = BuildTree(items, post.AuthorID)
	out.Body.Total = Count(out.Body.Items)
	return out, nil
}

// publicPost 取内容摘要并执行**与正文前台一致的**可见性校验。
//
// 评论接口过去只检查内容是否存在，于是文章一旦撤回或转为私密：
// 正文 404，评论列表却仍返回 200、评论也仍能被匿名写入——
// 已下线内容的信息就这样从评论接口漏了出去。
func (h *Handler) publicPost(ctx context.Context, id int64) (*PostRef, error) {
	post, err := h.store.PostRef(ctx, id)
	if err != nil {
		return nil, mapError(err)
	}
	if !post.PubliclyVisible() {
		// 与正文一样用 404 而不是 403：不暴露该内容是否存在。
		return nil, huma.Error404NotFound(ErrPostNotFound.Error())
	}
	return post, nil
}

func (h *Handler) publicCreate(ctx context.Context, in *createInput) (*createOutput, error) {
	cfg := h.settings(ctx)
	if !cfg.Enabled {
		return nil, huma.Error403Forbidden("本站已关闭评论")
	}

	post, err := h.publicPost(ctx, in.PostID)
	if err != nil {
		return nil, err
	}

	principal, authenticated := auth.FromContext(ctx)
	if !authenticated && !cfg.AllowAnonymous {
		return nil, huma.Error403Forbidden("本站仅允许已登录用户发表评论")
	}

	content, err := cleanContent(in.Body.Content, cfg.MaxLength)
	if err != nil {
		return nil, err
	}
	author, err := h.resolveAuthor(ctx, &in.Body, principal, authenticated, cfg)
	if err != nil {
		return nil, err
	}

	var parent *Comment
	if in.Body.ParentID != nil {
		parent, err = h.checkParent(ctx, *in.Body.ParentID, post.ID)
		if err != nil {
			return nil, err
		}
	}

	ip := httpx.ClientIPFromContext(ctx)
	c := &Comment{
		PostID:      post.ID,
		ParentID:    in.Body.ParentID,
		UserID:      author.userID,
		AuthorName:  author.name,
		AuthorEmail: author.email,
		AuthorURL:   author.url,
		Content:     content,
		ContentHTML: Render(content),
		IP:          ip,
		UserAgent:   userAgent(ctx),
	}
	// 插件判定在事务之外：反垃圾插件可能要调几秒外部服务，不能占着按 IP 的锁和数据库连接等它。
	verdict := h.pluginJudge(ctx, c, post, h.moderate(ctx, cfg, post))
	err = h.store.SerializeByIP(ctx, ip, func(ctx context.Context, tx *Store) error {
		// 内置的频率限制与蜜罐说是垃圾，插件改不回来
		c.Status = verdict
		if h.isSpam(ctx, tx, cfg, &in.Body, content) {
			c.Status = StatusSpam
		}
		return tx.Create(ctx, c)
	})
	if err != nil {
		return nil, mapError(err)
	}
	status := c.Status
	h.emit(ctx, hooks.CommentCreated, c, post)

	if h.notify != nil {
		h.notify.CommentCreated(ctx, &NotifyEvent{
			Comment: c, Post: post, Settings: cfg, Parent: parent,
			AutoApproved: status == StatusApproved,
		})
	}

	out := &createOutput{}
	out.Body.Comment = *c
	out.Body.Status = status
	if status == StatusApproved {
		out.Body.Note = "评论已发表"
	} else {
		out.Body.Note = "评论已提交，待审核后显示"
	}
	return out, nil
}

// author 是解析后的评论者信息。
type author struct {
	userID *int64
	name   string
	email  string
	url    string
}

// resolveAuthor 决定评论者是谁：已登录用户一律以账号为准，忽略表单里的字段。
//
// 否则任何人都能冒充站长发言。
func (h *Handler) resolveAuthor(
	_ context.Context, body *createBody, p *auth.Principal, authenticated bool, cfg Settings,
) (author, error) {
	if authenticated && p != nil && p.User != nil {
		return author{
			userID: &p.User.ID,
			name:   p.User.Name(),
			email:  p.User.Email,
			url:    strings.TrimSpace(body.URL),
		}, nil
	}

	name := strings.TrimSpace(body.Name)
	if name == "" {
		return author{}, huma.Error400BadRequest("请填写昵称")
	}
	if utf8.RuneCountInString(name) > maxAuthorName {
		return author{}, huma.Error400BadRequest("昵称过长")
	}
	email := strings.TrimSpace(body.Email)
	if cfg.RequireEmail && email == "" {
		return author{}, huma.Error400BadRequest("请填写邮箱")
	}
	if utf8.RuneCountInString(email) > maxAuthorEmail {
		return author{}, huma.Error400BadRequest("邮箱过长")
	}
	url, ok := sanitizeAuthorURL(body.URL)
	if !ok {
		return author{}, huma.Error400BadRequest("主页地址须为含 http 或 https 协议的绝对地址")
	}
	return author{name: name, email: email, url: url}, nil
}

// settings 读取评论设置；未装配设置模块时用缺省值。
func (h *Handler) settings(ctx context.Context) Settings {
	if h.cfg == nil {
		return defaultSettings
	}
	return h.cfg(ctx)
}

// moderate 决定新评论在不看反垃圾时该是什么状态：内容作者本人直接通过，其余按「需要审核」设置。
//
// 内容作者自己的评论直接通过：站长在自己的文章下回复访客是高频场景，走一遍审核只会让对话断掉。
func (h *Handler) moderate(ctx context.Context, cfg Settings, post *PostRef) Status {
	if p, ok := auth.FromContext(ctx); ok && p.User != nil && p.User.ID == post.AuthorID {
		return StatusApproved
	}
	if cfg.RequireApproval {
		return StatusPending
	}
	return StatusApproved
}

// pluginJudge 把评论交给订阅了 comment.judge 的插件，返回它们给出的状态；没人订阅时原样返回。
func (h *Handler) pluginJudge(ctx context.Context, c *Comment, post *PostRef, status Status) Status {
	if h.events == nil || !h.events.Subscribed(hooks.CommentJudge) {
		return status
	}
	in := hooks.CommentJudgement{Comment: hookComment(c, post), Status: string(status)}
	out := app.ApplyFilter(ctx, h.events, hooks.CommentJudge, in)
	switch next := Status(out.Status); next {
	case StatusApproved, StatusPending, StatusSpam:
		if next != status {
			h.logger.Info("插件改变了新评论的状态",
				slog.String("from", string(status)), slog.String("to", string(next)), slog.String("reason", out.Reason))
		}
		return next
	default:
		return status
	}
}

// isSpam 跑内置的反垃圾判定。要读同 IP 最近一条评论的时间，故在 SerializeByIP 的事务里调用。
func (h *Handler) isSpam(ctx context.Context, store *Store, cfg Settings, body *createBody, content string) bool {
	var last time.Time
	if at, err := store.LastByIP(ctx, httpx.ClientIPFromContext(ctx)); err == nil {
		last = at
	}
	// 查不到最近记录不该让评论发不出去：err 非 nil 时 last 保持零值，
	// 判定器据此视为「没有历史」继续往下走。
	return h.spam.Check(&cfg, &SpamInput{
		AuthorName: body.Name,
		AuthorURL:  body.URL,
		Content:    content,
		Honeypot:   body.Honeypot,
		LastFromIP: last,
		Now:        nowFunc(),
	}).Spam
}

// checkParent 校验被回复的评论存在、属于同一篇内容且已通过，并返回它供通知使用。
func (h *Handler) checkParent(ctx context.Context, parentID, postID int64) (*Comment, error) {
	parent, err := h.store.Get(ctx, parentID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, huma.Error400BadRequest("被回复的评论不存在")
		}
		return nil, err
	}
	if parent.PostID != postID {
		return nil, huma.Error400BadRequest("被回复的评论不属于该内容")
	}
	if parent.Status != StatusApproved {
		return nil, huma.Error400BadRequest("被回复的评论尚未通过审核")
	}
	return parent, nil
}

// ---------- 工具 ----------

// cleanContent 规范化评论正文：去首尾空白、拒绝空白内容、按上限限制长度。
//
// limit 是站点设置里的有效上限（Unicode 字符数），非正数或超过硬上限时退回硬上限——
// 设置本身已被表单声明约束在 1..10000，这里再兜一层，避免一条脏数据让评论功能全挂。
//
// 这里只做形态检查，转义交给 Render——两者分开，是为了让「存什么」与
// 「怎么显示」各自独立可测。
func cleanContent(content string, limit int) (string, error) {
	content = strings.TrimSpace(strings.ReplaceAll(content, "\r\n", "\n"))
	if content == "" {
		return "", huma.Error400BadRequest("评论内容不能为空白")
	}
	if limit <= 0 || limit > maxContentLength {
		limit = maxContentLength
	}
	if utf8.RuneCountInString(content) > limit {
		return "", huma.Error400BadRequest(fmt.Sprintf("评论内容过长，最多 %d 字", limit))
	}
	return content, nil
}

// maxContentLength 是正文的硬上限，与库中的 CHECK 约束一致。
// 站点设置里的上限只能比它更严。
const maxContentLength = 10000

// userAgent 取请求头里的 UA，超长时截断。
func userAgent(ctx context.Context) string {
	ua := httpx.UserAgentFromContext(ctx)
	if len(ua) > maxUserAgent {
		return ua[:maxUserAgent]
	}
	return ua
}

// maxUserAgent 是留存的 UA 长度上限，与库中的列宽无关，只是防止个别客户端塞进来一整篇文本。
const maxUserAgent = 512

// mapError 把存储层的哨兵错误映射为 HTTP 错误；其余错误交给 huma 按 500 处理。
func mapError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrNotFound):
		return huma.Error404NotFound(err.Error())
	case errors.Is(err, ErrPostNotFound):
		return huma.Error404NotFound(err.Error())
	case errors.Is(err, ErrInvalid):
		return huma.Error400BadRequest(err.Error())
	}
	return err
}
