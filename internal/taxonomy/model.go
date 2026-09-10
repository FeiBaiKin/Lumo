package taxonomy

import (
	"time"

	"github.com/uptrace/bun"
)

// Category 是分类实体。分类构成一棵森林：ParentID 为 nil 的是根分类。
type Category struct {
	bun.BaseModel `bun:"table:categories,alias:c"`

	ID          int64     `bun:"id,pk,autoincrement" json:"id" doc:"分类 ID"`
	ParentID    *int64    `bun:"parent_id"           json:"parentId" doc:"父分类 ID，根分类为 null"`
	Name        string    `bun:"name,notnull"        json:"name" doc:"名称，同一父分类下唯一"`
	Slug        string    `bun:"slug,notnull"        json:"slug" doc:"URL 片段，全局唯一"`
	Description string    `bun:"description"         json:"description" doc:"描述"`
	CoverURL    string    `bun:"cover_url"           json:"coverUrl" doc:"封面图地址"`
	Position    int       `bun:"position"            json:"position" doc:"同级排序，越小越靠前"`
	CreatedAt   time.Time `bun:"created_at,nullzero" json:"createdAt"`
	UpdatedAt   time.Time `bun:"updated_at,nullzero" json:"updatedAt"`
}

// Tag 是标签实体。
type Tag struct {
	bun.BaseModel `bun:"table:tags,alias:t"`

	ID          int64     `bun:"id,pk,autoincrement" json:"id" doc:"标签 ID"`
	Name        string    `bun:"name,notnull"        json:"name" doc:"名称，大小写不敏感唯一"`
	Slug        string    `bun:"slug,notnull"        json:"slug" doc:"URL 片段，全局唯一"`
	Description string    `bun:"description"         json:"description" doc:"描述"`
	Color       string    `bun:"color"               json:"color" doc:"展示颜色，如 #3b82f6；空串表示主题默认"`
	CreatedAt   time.Time `bun:"created_at,nullzero" json:"createdAt"`
	UpdatedAt   time.Time `bun:"updated_at,nullzero" json:"updatedAt"`
}

// CategoryNode 是分类树的节点：分类本身加上按 position 排序的子分类。
type CategoryNode struct {
	Category
	Children []*CategoryNode `json:"children" doc:"子分类，按 position 排序"`
}
