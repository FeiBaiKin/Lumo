package lumo

import (
	"encoding/json"
	"time"
)

// 宿主派发的动作。订阅之前要在 plugin.yaml 的 spec.hooks.actions 里声明，
// 并声明 capabilities.content.read（这些动作的数据都属于站点内容）。
const (
	// ActionPostPublished 是文章或页面发布（含定时发布到点），数据为 Post。
	ActionPostPublished = "post.published"
	// ActionPostUpdated 是文章或页面被保存（任何状态），数据为 Post。
	ActionPostUpdated = "post.updated"
	// ActionPostTrashed 是文章或页面移入回收站，数据为 Post。
	ActionPostTrashed = "post.trashed"
	// ActionPostDeleted 是文章或页面被彻底删除，数据为 Post。
	ActionPostDeleted = "post.deleted"
	// ActionCommentCreated 是收到新评论（含待审与判为垃圾的），数据为 Comment。
	ActionCommentCreated = "comment.created"
	// ActionCommentApproved 是评论通过审核，数据为 Comment。
	ActionCommentApproved = "comment.approved"
	// ActionUserRegistered 是新建了用户，数据为 User。
	ActionUserRegistered = "user.registered"
)

// 宿主提供的过滤器。订阅之前要在 spec.hooks.filters 里声明。
const (
	// FilterCommentJudge 在新评论入库前决定它的状态，时限 3 秒。需要 content.read。
	FilterCommentJudge = "comment.judge"
	// FilterContentRender 在正文输出到前台前改写它，时限 200 毫秒，结果会再净化一遍。
	// 需要 content.read 与 frontend。
	FilterContentRender = "content.render"
)

// 评论状态。
const (
	CommentApproved = "approved"
	CommentPending  = "pending"
	CommentSpam     = "spam"
)

// Post 是文章或页面动作的数据。
type Post struct {
	ID    int64  `json:"id"`
	Type  string `json:"type"`
	Title string `json:"title"`
	Slug  string `json:"slug"`
	// Path 是前台路径，拼上站点地址（Self().Site）就是完整链接。
	Path        string     `json:"path"`
	Status      string     `json:"status"`
	AuthorID    int64      `json:"authorId"`
	PublishedAt *time.Time `json:"publishedAt,omitempty"`
}

// PostRef 是评论等数据里引用的文章或页面。
type PostRef struct {
	ID    int64  `json:"id"`
	Type  string `json:"type"`
	Title string `json:"title"`
	Path  string `json:"path"`
}

// CommentAuthor 是评论者。
type CommentAuthor struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	URL   string `json:"url"`
	// UserID 是已登录评论者的账号；访客为 nil。
	UserID *int64 `json:"userId,omitempty"`
}

// Comment 是评论动作的数据。
type Comment struct {
	ID        int64         `json:"id"`
	ParentID  *int64        `json:"parentId,omitempty"`
	Post      PostRef       `json:"post"`
	Author    CommentAuthor `json:"author"`
	Content   string        `json:"content"`
	Status    string        `json:"status"`
	IP        string        `json:"ip"`
	UserAgent string        `json:"userAgent"`
	CreatedAt time.Time     `json:"createdAt"`
}

// CommentJudgement 是 comment.judge 的值：读 Comment，改 Status（approved、pending、spam）。
// 内核自己的频率限制与蜜罐判为垃圾的评论，插件改不回来。
type CommentJudgement struct {
	// Comment 是待判定的评论，此时还没入库，ID 为零。
	Comment Comment `json:"comment"`
	Status  string  `json:"status"`
	// Reason 是给出这个结论的理由，写进日志。
	Reason string `json:"reason,omitempty"`
}

// RenderedContent 是 content.render 的值：改 HTML。
type RenderedContent struct {
	HTML string  `json:"html"`
	Post PostRef `json:"post"`
}

// User 是用户动作的数据，只有公开资料。
type User struct {
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
}

// OnFilter 登记一个过滤器：收到当前值，返回改过的值；返回 nil 表示不改。
//
// 过滤器同步执行、有时限，出错或超时宿主就沿用上一环的值。能用下面的类型化版本就用它们。
func OnFilter(name string, fn func(ctx *Context, value json.RawMessage) (any, error)) {
	register("filter", name, func(ctx *Context, payload json.RawMessage) (any, error) {
		return fn(ctx, payload)
	})
}

// onTyped 登记一个类型化的过滤器：把值解成 T，交给 fn 就地修改，再原样交回。
func onTyped[T any](name string, fn func(ctx *Context, v *T) error) {
	register("filter", name, func(ctx *Context, payload json.RawMessage) (any, error) {
		v := new(T)
		if err := json.Unmarshal(payload, v); err != nil {
			return nil, err
		}
		if err := fn(ctx, v); err != nil {
			return nil, err
		}
		return v, nil
	})
}

// OnCommentJudge 登记 comment.judge：改 j.Status 决定新评论的去向。
func OnCommentJudge(fn func(ctx *Context, j *CommentJudgement) error) {
	onTyped(FilterCommentJudge, fn)
}

// OnContentRender 登记 content.render：改 c.HTML。
func OnContentRender(fn func(ctx *Context, c *RenderedContent) error) {
	onTyped(FilterContentRender, fn)
}

// onEvent 登记一个类型化的动作处理函数。
func onEvent[T any](name string, fn func(ctx *Context, v *T) error) {
	OnAction(name, func(ctx *Context, e *Event) error {
		v := new(T)
		if err := e.Decode(v); err != nil {
			return err
		}
		return fn(ctx, v)
	})
}

// OnPost 登记文章或页面的动作：post.published、post.updated、post.trashed、post.deleted。
func OnPost(action string, fn func(ctx *Context, p *Post) error) { onEvent(action, fn) }

// OnComment 登记评论的动作：comment.created、comment.approved。
func OnComment(action string, fn func(ctx *Context, c *Comment) error) { onEvent(action, fn) }

// OnUserRegistered 登记 user.registered。
func OnUserRegistered(fn func(ctx *Context, u *User) error) { onEvent(ActionUserRegistered, fn) }
