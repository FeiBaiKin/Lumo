package api

// 分页约定：v1 用 offset 分页，Console 表格需要跳页。
const (
	// DefaultPageSize 是未指定 size 时的每页条数。
	DefaultPageSize = 20
	// MaxPageSize 是每页条数上限，防止一次拉取拖垮数据库。
	MaxPageSize = 100
)

// PageParams 是统一的分页入参，嵌入到列表操作的输入结构体中即可。
type PageParams struct {
	Page int `query:"page" default:"1" minimum:"1" doc:"页码，从 1 开始"`
	Size int `query:"size" default:"20" minimum:"1" maximum:"100" doc:"每页条数，最大 100"`
}

// Normalize 把零值补成默认值，供绕过 huma 校验的调用路径使用。
func (p PageParams) Normalize() PageParams {
	if p.Page < 1 {
		p.Page = 1
	}
	if p.Size < 1 {
		p.Size = DefaultPageSize
	}
	if p.Size > MaxPageSize {
		p.Size = MaxPageSize
	}
	return p
}

// Offset 返回 SQL OFFSET。
func (p PageParams) Offset() int {
	p = p.Normalize()
	return (p.Page - 1) * p.Size
}

// Limit 返回 SQL LIMIT。
func (p PageParams) Limit() int {
	return p.Normalize().Size
}

// Page 是统一的分页响应体。
type Page[T any] struct {
	Items []T `json:"items" doc:"当前页的条目"`
	Page  int `json:"page" doc:"当前页码"`
	Size  int `json:"size" doc:"每页条数"`
	Total int `json:"total" doc:"总条数"`
}

// NewPage 组装分页响应；items 为 nil 时输出空数组而非 null。
func NewPage[T any](items []T, params PageParams, total int) Page[T] {
	params = params.Normalize()
	if items == nil {
		items = []T{}
	}
	return Page[T]{Items: items, Page: params.Page, Size: params.Size, Total: total}
}
