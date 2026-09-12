package comment

import (
	"time"

	"github.com/uptrace/bun"
)

// Status 是评论的审核状态。
type Status string

// 评论状态。删除即删行，故没有回收站态。
const (
	// StatusPending 待审核：仅后台可见。
	StatusPending Status = "pending"
	// StatusApproved 已通过：前台可见。
	StatusApproved Status = "approved"
	// StatusSpam 垃圾：留档以便训练与申诉，前台不可见。
	StatusSpam Status = "spam"
)

// Comment 是一条评论。
//
// 字段分两类：带 json 标签的可以出现在 Console 响应里；
// 邮箱、IP 与 UA 是个人信息，只在 Console 平面出现，前台视图另有一套（见 PublicView）。
type Comment struct {
	bun.BaseModel `bun:"table:comments,alias:cm"`

	ID          int64     `bun:"id,pk,autoincrement" json:"id"`
	PostID      int64     `bun:"post_id,notnull"     json:"postId"`
	ParentID    *int64    `bun:"parent_id"           json:"parentId" doc:"父评论 ID，顶层评论为 null"`
	UserID      *int64    `bun:"user_id"             json:"userId" doc:"已登录用户的 ID；访客评论为 null"`
	AuthorName  string    `bun:"author_name,notnull" json:"authorName"`
	AuthorEmail string    `bun:"author_email"        json:"authorEmail" doc:"仅 Console 可见"`
	AuthorURL   string    `bun:"author_url"          json:"authorUrl"`
	Content     string    `bun:"content,notnull"     json:"content" doc:"访客提交的原文，纯文本"`
	ContentHTML string    `bun:"content_html"        json:"contentHtml" doc:"转义并线性化后的展示版本"`
	Status      Status    `bun:"status,notnull"      json:"status" enum:"pending,approved,spam"`
	IP          string    `bun:"ip"                  json:"ip" doc:"仅 Console 可见"`
	UserAgent   string    `bun:"user_agent"          json:"userAgent" doc:"仅 Console 可见"`
	CreatedAt   time.Time `bun:"created_at,nullzero" json:"createdAt"`
	UpdatedAt   time.Time `bun:"updated_at,nullzero" json:"updatedAt"`

	// 以下为关联数据，由存储层按需填充，不是数据库列。
	Post *PostRef `bun:"-" json:"post,omitempty"`
}

// PostRef 是评论所属内容的摘要，供后台列表直接显示与跳转。
type PostRef struct {
	ID       int64  `json:"id"`
	Type     string `json:"type"`
	Title    string `json:"title"`
	Slug     string `json:"slug"`
	AuthorID int64  `json:"authorId"`
	// Status 与 Visibility 供前台接口复刻正文的可见性规则。
	Status     string `json:"-"`
	Visibility string `json:"-"`
}

// 内容状态与可见性的取值。与 content 模块的常量同义，但这里不引那个包：
// 模块之间只允许经 App 交互（见 store.go 的说明），两个字符串不值得开一个依赖。
const (
	postStatusPublished  = "published"
	postVisibilityPublic = "public"
)

// PubliclyVisible 报告内容是否对访客可见，判据与主题前台的 publicFilter 完全一致：
// 已发布且公开。
//
// 这里刻意不接受「作者可见自己的私密内容」：Public 平面是访客平面，
// 草稿、私密与回收站中的内容一律不对外提供服务，其存在性也不该被探知。
// 作者要看自己未公开内容的评论，走 Console。
func (r *PostRef) PubliclyVisible() bool {
	return r.Status == postStatusPublished && r.Visibility == postVisibilityPublic
}

// PublicView 是前台可见的评论，**不含**邮箱、IP 与 UA。
//
// 单独建一个类型而不是给字段加 omitempty：前台响应少一个字段是功能问题，
// 多一个字段是数据泄漏，两者的严重程度不对等，故用类型把边界钉死。
type PublicView struct {
	ID          int64         `json:"id"`
	ParentID    *int64        `json:"parentId"`
	AuthorName  string        `json:"authorName"`
	AuthorURL   string        `json:"authorUrl"`
	ContentHTML string        `json:"contentHtml" doc:"已转义的展示 HTML，主题直出即可"`
	IsAuthor    bool          `json:"isAuthor" doc:"是否为该内容的作者本人所发"`
	CreatedAt   time.Time     `json:"createdAt"`
	Children    []*PublicView `json:"children" doc:"回复，按时间正序"`
}

// toPublic 把一条评论转成前台视图。
func toPublic(c *Comment, postAuthorID int64) *PublicView {
	return &PublicView{
		ID:          c.ID,
		ParentID:    c.ParentID,
		AuthorName:  c.AuthorName,
		AuthorURL:   c.AuthorURL,
		ContentHTML: c.ContentHTML,
		IsAuthor:    c.UserID != nil && *c.UserID == postAuthorID,
		CreatedAt:   c.CreatedAt,
		Children:    []*PublicView{},
	}
}

// BuildTree 把平铺的评论按 parent_id 组装成回复树。
//
// 只保留能追溯到顶层的节点：父评论被删或被标垃圾时，其回复不该跑到顶层去展示。
func BuildTree(items []Comment, postAuthorID int64) []*PublicView {
	nodes := make(map[int64]*PublicView, len(items))
	for i := range items {
		nodes[items[i].ID] = toPublic(&items[i], postAuthorID)
	}

	roots := make([]*PublicView, 0, len(items))
	for i := range items {
		node := nodes[items[i].ID]
		if items[i].ParentID == nil {
			roots = append(roots, node)
			continue
		}
		if parent, ok := nodes[*items[i].ParentID]; ok {
			parent.Children = append(parent.Children, node)
		}
		// 找不到父节点即整支丢弃：父评论待审或已被标垃圾。
	}
	return roots
}

// Count 统计一棵回复树里的节点数，供分页元数据使用。
func Count(nodes []*PublicView) int {
	total := 0
	for _, node := range nodes {
		total += 1 + Count(node.Children)
	}
	return total
}
