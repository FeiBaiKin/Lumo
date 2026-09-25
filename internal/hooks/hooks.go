// Package hooks 定义内核派发给插件的动作与过滤器：名字与数据结构。
//
// 这是内核与插件之间的公开契约，插件 SDK（sdk/go）里有一份同形的类型。
// 改字段就是改接口：只能加，不能改名或删；要大改就起一个新名字。
//
// 本包不依赖任何模块：发出动作的内容、评论、账号模块与接收它们的插件模块都引用它，
// 它自己不能反过来引用任何一方。
package hooks

import "time"

// 动作：事情发生之后异步通知，插件改变不了已经发生的事。
const (
	// PostPublished 是文章或页面发布（含定时发布到点），数据为 Post。
	PostPublished = "post.published"
	// PostUpdated 是文章或页面被保存（任何状态），数据为 Post。
	PostUpdated = "post.updated"
	// PostTrashed 是文章或页面移入回收站，数据为 Post。
	PostTrashed = "post.trashed"
	// PostDeleted 是文章或页面被彻底删除，数据为 Post。
	PostDeleted = "post.deleted"
	// CommentCreated 是收到新评论（含待审与判为垃圾的），数据为 Comment。
	CommentCreated = "comment.created"
	// CommentApproved 是评论通过审核，数据为 Comment。
	CommentApproved = "comment.approved"
	// UserRegistered 是新建了用户（前台注册或后台创建），数据为 User。
	UserRegistered = "user.registered"
)

// 过滤器：把数据交给插件改一遍再用，同步执行、有时限，超时或出错就用原值。
const (
	// CommentJudge 在新评论入库前决定它的状态，值为 CommentJudgement。
	CommentJudge = "comment.judge"
	// ContentRender 在文章与页面正文输出到前台之前改写它，值为 RenderedContent。结果会再净化一遍。
	ContentRender = "content.render"
)

// PostsPrefix 是文章详情页的路径前缀；页面直接挂在站点根下。
const PostsPrefix = "/posts/"

// PostPath 返回文章或页面的前台路径。这条规则内容模块、评论模块与主题都以它为准。
func PostPath(typ, slug string) string {
	if typ == "page" {
		return "/" + slug
	}
	return PostsPrefix + slug
}

// Post 是文章或页面动作的数据。
type Post struct {
	ID    int64  `json:"id"`
	Type  string `json:"type"`
	Title string `json:"title"`
	Slug  string `json:"slug"`
	// Path 是前台路径，拼上站点地址就是完整链接。
	Path     string `json:"path"`
	Status   string `json:"status"`
	AuthorID int64  `json:"authorId"`
	// PublishedAt 是发布时间；从没发布过时为 nil。
	PublishedAt *time.Time `json:"publishedAt,omitempty"`
}

// PostRef 是评论等数据里引用的文章或页面。
type PostRef struct {
	ID    int64  `json:"id"`
	Type  string `json:"type"`
	Title string `json:"title"`
	Path  string `json:"path"`
}

// CommentAuthor 是评论者。邮箱与 IP 只给被授予了「读取站点内容」的插件（反垃圾离不开它们）。
type CommentAuthor struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	URL   string `json:"url"`
	// UserID 是已登录评论者的账号；访客为 nil。
	UserID *int64 `json:"userId,omitempty"`
}

// Comment 是评论动作的数据。
type Comment struct {
	ID       int64         `json:"id"`
	ParentID *int64        `json:"parentId,omitempty"`
	Post     PostRef       `json:"post"`
	Author   CommentAuthor `json:"author"`
	// Content 是评论原文（纯文本）。
	Content   string    `json:"content"`
	Status    string    `json:"status"`
	IP        string    `json:"ip"`
	UserAgent string    `json:"userAgent"`
	CreatedAt time.Time `json:"createdAt"`
}

// CommentJudgement 是 comment.judge 过滤器的值：插件读评论、改 Status。
//
// Status 只认 approved、pending、spam；改成别的值会被忽略。内核自己的频率限制与蜜罐
// 判为垃圾的评论，插件改不回来。
type CommentJudgement struct {
	// Comment 是待判定的评论，此时还没入库，ID 为零。
	Comment Comment `json:"comment"`
	Status  string  `json:"status"`
	// Reason 是插件给出的理由，写进日志，便于站长复查。
	Reason string `json:"reason,omitempty"`
}

// RenderedContent 是 content.render 过滤器的值：插件改 HTML。
type RenderedContent struct {
	HTML string  `json:"html"`
	Post PostRef `json:"post"`
}

// User 是用户动作的数据：只有公开资料。
type User struct {
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
}
