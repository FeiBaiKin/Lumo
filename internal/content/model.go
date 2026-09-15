package content

import (
	"time"

	"github.com/uptrace/bun"

	"github.com/FeiBaiKin/lumo/internal/taxonomy"
)

// Type 区分文章与独立页面，两者共用一张表。
type Type string

// 内容类型。
const (
	TypePost Type = "post"
	TypePage Type = "page"
)

// Status 是内容的生命周期状态。
type Status string

// 内容状态。
const (
	// StatusDraft 是草稿：仅作者与有权限者可见。
	StatusDraft Status = "draft"
	// StatusPublished 已发布。
	StatusPublished Status = "published"
	// StatusScheduled 定时发布：到 PublishedAt 时由后台任务推进为已发布。
	StatusScheduled Status = "scheduled"
	// StatusTrashed 在回收站：可恢复，也可彻底删除。
	StatusTrashed Status = "trashed"
)

// Visibility 是内容对前台的可见性。
type Visibility string

// 可见性。
const (
	VisibilityPublic  Visibility = "public"
	VisibilityPrivate Visibility = "private"
)

// RawType 是原稿格式。
type RawType string

// 原稿格式。
const (
	RawHTML     RawType = "html"
	RawMarkdown RawType = "markdown"
)

// Post 是文章或页面实体。
type Post struct {
	bun.BaseModel `bun:"table:posts,alias:p"`

	ID          int64          `bun:"id,pk,autoincrement" json:"id"`
	Type        Type           `bun:"type,notnull"        json:"type" enum:"post,page"`
	Title       string         `bun:"title,notnull"       json:"title"`
	Slug        string         `bun:"slug,notnull"        json:"slug" doc:"URL 片段，同类型内唯一"`
	Status      Status         `bun:"status,notnull"      json:"status" enum:"draft,published,scheduled,trashed"`
	Visibility  Visibility     `bun:"visibility,notnull"  json:"visibility" enum:"public,private"`
	RawType     RawType        `bun:"raw_type,notnull"    json:"rawType" enum:"html,markdown" doc:"原稿格式"`
	Raw         string         `bun:"raw"                 json:"raw" doc:"原稿：Markdown 源码或规范 HTML"`
	Content     string         `bun:"content"             json:"content" doc:"渲染后的 HTML，主题只消费此字段"`
	Excerpt     string         `bun:"excerpt"             json:"excerpt"`
	ExcerptAuto bool           `bun:"excerpt_auto"        json:"excerptAuto" doc:"摘要是否由正文自动生成"`
	CoverURL    string         `bun:"cover_url"           json:"coverUrl"`
	Pinned      bool           `bun:"pinned"              json:"pinned"`
	Template    string         `bun:"template"            json:"template" doc:"页面模板名，如 page-about；空为默认"`
	AuthorID    int64          `bun:"author_id,notnull"   json:"authorId"`
	PublishedAt *time.Time     `bun:"published_at"        json:"publishedAt" doc:"发布时间；定时发布时为计划时间"`
	TrashedAt   *time.Time     `bun:"trashed_at"          json:"trashedAt"`
	Meta        map[string]any `bun:"meta,type:jsonb"     json:"meta" doc:"扩展元数据"`
	CreatedAt   time.Time      `bun:"created_at,nullzero" json:"createdAt"`
	UpdatedAt   time.Time      `bun:"updated_at,nullzero" json:"updatedAt"`

	// 以下为关联数据，由存储层按需填充，不是数据库列。
	Author     *AuthorView         `bun:"-" json:"author,omitempty"`
	Categories []taxonomy.Category `bun:"-" json:"categories"`
	Tags       []taxonomy.Tag      `bun:"-" json:"tags"`
}

// IsPublic 报告内容是否对匿名访客可见。
func (p *Post) IsPublic() bool {
	return p.Status == StatusPublished && p.Visibility == VisibilityPublic
}

// AuthorView 是嵌入内容响应的作者摘要，只含可公开的字段。
type AuthorView struct {
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	AvatarURL   string `json:"avatarUrl"`
}

// PostCategory 是文章与分类的关联。
type PostCategory struct {
	bun.BaseModel `bun:"table:post_categories,alias:pc"`

	PostID     int64 `bun:"post_id,pk"`
	CategoryID int64 `bun:"category_id,pk"`
}

// PostTag 是文章与标签的关联。
type PostTag struct {
	bun.BaseModel `bun:"table:post_tags,alias:pt"`

	PostID int64 `bun:"post_id,pk"`
	TagID  int64 `bun:"tag_id,pk"`
}

// Revision 是一次内容快照。
type Revision struct {
	bun.BaseModel `bun:"table:post_revisions,alias:rv"`

	ID        int64     `bun:"id,pk,autoincrement" json:"id"`
	PostID    int64     `bun:"post_id,notnull"     json:"postId"`
	AuthorID  *int64    `bun:"author_id"           json:"authorId" doc:"保存该版本的用户；用户已删除时为 null"`
	Title     string    `bun:"title,notnull"       json:"title"`
	RawType   RawType   `bun:"raw_type,notnull"    json:"rawType" enum:"html,markdown"`
	Raw       string    `bun:"raw"                 json:"raw"`
	Content   string    `bun:"content"             json:"content"`
	Excerpt   string    `bun:"excerpt"             json:"excerpt"`
	CreatedAt time.Time `bun:"created_at,nullzero" json:"createdAt"`
}

// RevisionSummary 是修订列表项，不含正文以免列表过大。
type RevisionSummary struct {
	bun.BaseModel `bun:"table:post_revisions,alias:rv"`

	ID        int64     `bun:"id,pk"            json:"id"`
	PostID    int64     `bun:"post_id"          json:"postId"`
	AuthorID  *int64    `bun:"author_id"        json:"authorId"`
	Title     string    `bun:"title"            json:"title"`
	RawType   RawType   `bun:"raw_type"         json:"rawType" enum:"html,markdown"`
	CreatedAt time.Time `bun:"created_at,nullzero" json:"createdAt"`
}
