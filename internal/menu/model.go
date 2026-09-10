package menu

import (
	"time"

	"github.com/uptrace/bun"
)

// ItemType 是菜单条目的类型，决定 URL 从哪来。
type ItemType string

// 条目类型。
const (
	// TypeCustom 是手填地址的条目。
	TypeCustom ItemType = "custom"
	// 以下四种指向站内已有记录，URL 由服务端解析，记录改名或改 slug 后菜单自动跟随。
	TypePost     ItemType = "post"
	TypePage     ItemType = "page"
	TypeCategory ItemType = "category"
	TypeTag      ItemType = "tag"
)

// Menu 是一组菜单。
type Menu struct {
	bun.BaseModel `bun:"table:menus,alias:mn"`

	ID          int64     `bun:"id,pk,autoincrement" json:"id"`
	Name        string    `bun:"name,notnull"        json:"name"`
	Slug        string    `bun:"slug,notnull"        json:"slug" doc:"主题按此名取菜单，全局唯一"`
	Description string    `bun:"description"         json:"description"`
	CreatedAt   time.Time `bun:"created_at,nullzero" json:"createdAt"`
	UpdatedAt   time.Time `bun:"updated_at,nullzero" json:"updatedAt"`

	// Items 仅在按 slug 取整棵树时填充，不是数据库列。
	Items []*ItemNode `bun:"-" json:"items,omitempty"`
	// ItemCount 是条目数，供后台列表显示。
	ItemCount int `bun:"-" json:"itemCount"`
}

// Item 是菜单里的一个条目。
type Item struct {
	bun.BaseModel `bun:"table:menu_items,alias:mi"`

	ID       int64    `bun:"id,pk,autoincrement" json:"id"`
	MenuID   int64    `bun:"menu_id,notnull"     json:"menuId"`
	ParentID *int64   `bun:"parent_id"           json:"parentId" doc:"父条目 ID，一级菜单为 null"`
	Position int      `bun:"position"            json:"position" doc:"同级排序，越小越靠前"`
	Label    string   `bun:"label,notnull"       json:"label"`
	Type     ItemType `bun:"type,notnull"        json:"type" enum:"custom,post,page,category,tag"`
	// TargetID 是站内记录的 ID，仅 post / page / category / tag 类型有值。
	TargetID *int64 `bun:"target_id" json:"targetId" doc:"站内记录 ID，自定义链接为 null"`
	// URL 对自定义链接是手填值；对站内条目是由记录解析出的地址。
	URL    string `bun:"url"     json:"url"`
	Target string `bun:"target"  json:"target" enum:",_blank" doc:"空串为当前窗口，_blank 为新窗口"`
	Rel    string `bun:"rel"     json:"rel" doc:"额外的 rel 属性，如 nofollow"`
	// Visible 为假时前台不渲染该条目，但后台仍保留。
	Visible   bool      `bun:"visible"            json:"visible"`
	CreatedAt time.Time `bun:"created_at,nullzero" json:"createdAt"`
	UpdatedAt time.Time `bun:"updated_at,nullzero" json:"updatedAt"`
}

// ItemNode 是菜单树的节点：条目本身加上按 position 排序的子条目。
type ItemNode struct {
	Item
	Children []*ItemNode `json:"children" doc:"子条目，按 position 排序"`
}

// BuildTree 把平铺的条目组装成树。
//
// 只保留能追溯到根节点的条目：父项被删时子项已由外键级联删除，
// 但脏数据仍可能存在，这里再兜一层。
func BuildTree(items []Item) []*ItemNode {
	nodes := make(map[int64]*ItemNode, len(items))
	for i := range items {
		nodes[items[i].ID] = &ItemNode{Item: items[i], Children: []*ItemNode{}}
	}

	roots := make([]*ItemNode, 0, len(items))
	for i := range items {
		node := nodes[items[i].ID]
		if items[i].ParentID == nil {
			roots = append(roots, node)
			continue
		}
		parent, ok := nodes[*items[i].ParentID]
		if !ok {
			continue
		}
		parent.Children = append(parent.Children, node)
	}
	return roots
}

// VisibleTree 返回只含可见条目的树，供前台渲染。
//
// 父项隐藏时整支不渲染：让一个隐藏的父项还留着可见的子项，
// 会在前台产生一串没有归属的链接。
func VisibleTree(nodes []*ItemNode) []*ItemNode {
	out := make([]*ItemNode, 0, len(nodes))
	for _, node := range nodes {
		if !node.Visible {
			continue
		}
		clone := *node
		clone.Children = VisibleTree(node.Children)
		out = append(out, &clone)
	}
	return out
}

// CountItems 统计树中的条目数。
func CountItems(nodes []*ItemNode) int {
	total := 0
	for _, node := range nodes {
		total += 1 + CountItems(node.Children)
	}
	return total
}
