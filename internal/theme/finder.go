package theme

import (
	"context"
	"html/template"
	"sync"
	"time"
)

// Finder 是供模板调用的只读数据访问面（agent.md §4.2）。
//
// 刻意**不提供任意查询**：主题能调的每个函数都在这里逐个列出、各自带缓存与条数上限。
// 开放一个 `query` 函数会让主题变成应用——慢查询、N+1、越权读取都会跟着进来，
// 而主题是第三方代码，出了问题排查成本落在站长身上。
//
// 每个 Finder 绑定一次请求：ctx 随请求取消，缓存只在本次渲染内共享。
type Finder struct {
	ctx   context.Context //nolint:containedctx // Finder 生命周期等于一次请求，模板函数无法再传 ctx
	store *Store
	// siteURL 供生成绝对地址。
	siteURL string
	// menus 缓存本次请求内已取过的菜单，页头页脚常取同一个。
	mu    sync.Mutex
	cache map[string]any
}

// finderLimit 是单次 Finder 调用的条数上限。
//
// 主题写 `posts.recent 1000` 不该把首页拖垮；上限之上一律截断。
const finderLimit = 100

// newFinder 构造一次请求用的 Finder。
func newFinder(ctx context.Context, store *Store, siteURL string) *Finder {
	return &Finder{ctx: ctx, store: store, siteURL: siteURL, cache: map[string]any{}}
}

// cached 在本次请求内缓存一次查询结果。
//
// 只缓存到请求结束：页头页脚可能取同一个菜单或同一批热门标签，
// 而跨请求缓存会让「改了菜单要等一分钟才生效」，对编辑很不友好。
func cached[T any](f *Finder, key string, build func() (T, error)) T {
	var zero T
	// 没有数据库时（migrate 命令路径、不连库的模板测试）一律给零值。
	// Finder 的每个方法都会被模板直接调用，这里漏一层判空就是一个 500 页面。
	if f == nil || f.store == nil {
		return zero
	}

	f.mu.Lock()
	if v, ok := f.cache[key]; ok {
		f.mu.Unlock()
		if typed, ok := v.(T); ok {
			return typed
		}
		return zero
	}
	f.mu.Unlock()

	// 查询失败一律返回零值而不是把错误抛给模板：
	// 侧栏取不到热门标签不该让整个页面 500，页面主体仍然有价值。
	value, err := build()
	if err != nil {
		value = zero
	}
	f.mu.Lock()
	f.cache[key] = value
	f.mu.Unlock()
	return value
}

// clamp 把请求条数收敛到 [1, finderLimit]。
func clamp(n int) int {
	if n <= 0 {
		return 1
	}
	return min(n, finderLimit)
}

// ---------- posts ----------

// PostsFinder 是 posts 命名空间。
type PostsFinder struct{ f *Finder }

// Posts 返回 posts 命名空间，模板里写 {{ posts.Recent 5 }}。
func (f *Finder) Posts() PostsFinder { return PostsFinder{f} }

// Recent 返回最近发布的 n 篇文章。
func (p PostsFinder) Recent(n int) []PostView {
	n = clamp(n)
	return cached(p.f, "posts.recent:"+itoa(n), func() ([]PostView, error) {
		return p.f.store.RecentPosts(p.f.ctx, n)
	})
}

// Pinned 返回置顶文章。
func (p PostsFinder) Pinned(n int) []PostView {
	n = clamp(n)
	return cached(p.f, "posts.pinned:"+itoa(n), func() ([]PostView, error) {
		return p.f.store.PinnedPosts(p.f.ctx, n)
	})
}

// Related 返回与给定文章共享分类或标签的其他文章。
func (p PostsFinder) Related(postID int64, n int) []PostView {
	n = clamp(n)
	key := "posts.related:" + itoa(int(postID)) + ":" + itoa(n)
	return cached(p.f, key, func() ([]PostView, error) {
		return p.f.store.RelatedPosts(p.f.ctx, postID, n)
	})
}

// ByCategory 返回某个分类下的文章。
func (p PostsFinder) ByCategory(slug string, n int) []PostView {
	n = clamp(n)
	return cached(p.f, "posts.byCategory:"+slug+":"+itoa(n), func() ([]PostView, error) {
		items, _, err := p.f.store.PostsByTerm(p.f.ctx, termCategory, slug, 1, n)
		return items, err
	})
}

// ByTag 返回某个标签下的文章。
func (p PostsFinder) ByTag(slug string, n int) []PostView {
	n = clamp(n)
	return cached(p.f, "posts.byTag:"+slug+":"+itoa(n), func() ([]PostView, error) {
		items, _, err := p.f.store.PostsByTerm(p.f.ctx, termTag, slug, 1, n)
		return items, err
	})
}

// Total 返回已发布且公开的文章总数。
func (p PostsFinder) Total() int {
	return cached(p.f, "posts.total", func() (int, error) {
		return p.f.store.CountPosts(p.f.ctx)
	})
}

// ---------- categories ----------

// CategoriesFinder 是 categories 命名空间。
type CategoriesFinder struct{ f *Finder }

// Categories 返回 categories 命名空间。
func (f *Finder) Categories() CategoriesFinder { return CategoriesFinder{f} }

// Tree 返回分类树，每个节点带文章数。
func (c CategoriesFinder) Tree() []*CategoryNode {
	return cached(c.f, "categories.tree", func() ([]*CategoryNode, error) {
		return c.f.store.CategoryTree(c.f.ctx)
	})
}

// All 返回平铺的分类列表，每个带文章数。
func (c CategoriesFinder) All() []TermView {
	return cached(c.f, "categories.all", func() ([]TermView, error) {
		return c.f.store.Categories(c.f.ctx)
	})
}

// ---------- tags ----------

// TagsFinder 是 tags 命名空间。
type TagsFinder struct{ f *Finder }

// Tags 返回 tags 命名空间。
func (f *Finder) Tags() TagsFinder { return TagsFinder{f} }

// All 返回全部标签，按名称排序。
func (t TagsFinder) All() []TermView {
	return cached(t.f, "tags.all", func() ([]TermView, error) {
		return t.f.store.Tags(t.f.ctx, 0)
	})
}

// Cloud 返回按文章数倒序的前 n 个标签，每个带 Weight（1–5）供渲染字号。
func (t TagsFinder) Cloud(n int) []TermView {
	n = clamp(n)
	return cached(t.f, "tags.cloud:"+itoa(n), func() ([]TermView, error) {
		tags, err := t.f.store.Tags(t.f.ctx, n)
		if err != nil {
			return nil, err
		}
		return withWeights(tags), nil
	})
}

// ---------- archives ----------

// ArchivesFinder 是 archives 命名空间。
type ArchivesFinder struct{ f *Finder }

// Archives 返回 archives 命名空间。
func (f *Finder) Archives() ArchivesFinder { return ArchivesFinder{f} }

// ByMonth 返回按月归档的条目，新的在前。
func (a ArchivesFinder) ByMonth() []ArchiveEntry {
	return cached(a.f, "archives.byMonth", func() ([]ArchiveEntry, error) {
		return a.f.store.ArchivesByMonth(a.f.ctx)
	})
}

// ByYear 返回按年归档的条目，新的在前。
func (a ArchivesFinder) ByYear() []ArchiveEntry {
	return cached(a.f, "archives.byYear", func() ([]ArchiveEntry, error) {
		return a.f.store.ArchivesByYear(a.f.ctx)
	})
}

// ---------- menus ----------

// MenusFinder 是 menus 命名空间。
type MenusFinder struct{ f *Finder }

// Menus 返回 menus 命名空间。
func (f *Finder) Menus() MenusFinder { return MenusFinder{f} }

// Get 按 slug 取菜单树，只含可见且目标存在的条目。
//
// 菜单不存在时返回空切片而不是报错：主题引用了站长还没建的菜单是常见情形，
// 该显示的是「没有导航」，不是一个 500 页面。
func (m MenusFinder) Get(slug string) []MenuItemView {
	return cached(m.f, "menus:"+slug, func() ([]MenuItemView, error) {
		return m.f.store.Menu(m.f.ctx, slug)
	})
}

// ---------- comments ----------

// CommentsFinder 是 comments 命名空间。
type CommentsFinder struct{ f *Finder }

// Comments 返回 comments 命名空间。
func (f *Finder) Comments() CommentsFinder { return CommentsFinder{f} }

// Count 返回某条内容下已通过审核的评论数。
func (c CommentsFinder) Count(postID int64) int {
	return cached(c.f, "comments.count:"+itoa(int(postID)), func() (int, error) {
		return c.f.store.CountComments(c.f.ctx, postID)
	})
}

// Recent 返回站点最近通过审核的评论。
func (c CommentsFinder) Recent(n int) []CommentView {
	n = clamp(n)
	return cached(c.f, "comments.recent:"+itoa(n), func() ([]CommentView, error) {
		return c.f.store.RecentComments(c.f.ctx, n)
	})
}

// Tree 返回某条内容下已通过审核的评论树。
func (c CommentsFinder) Tree(postID int64) []*CommentNode {
	return cached(c.f, "comments.tree:"+itoa(int(postID)), func() ([]*CommentNode, error) {
		return c.f.store.CommentTree(c.f.ctx, postID)
	})
}

// ---------- favorites ----------

// FavoritesFinder 是 favorites 命名空间。
type FavoritesFinder struct{ f *Finder }

// Favorites 返回 favorites 命名空间。
func (f *Finder) Favorites() FavoritesFinder { return FavoritesFinder{f} }

// Enabled 报告站点是否装配了收藏功能。
//
// 主题据此决定画不画收藏按钮：没装配收藏模块时，一个点了必然报错的按钮
// 比没有这个按钮更糟。
func (v FavoritesFinder) Enabled() bool {
	return v.f != nil && v.f.store != nil && v.f.store.Favorites() != nil
}

// Count 返回一篇内容被收藏的次数。
func (v FavoritesFinder) Count(postID int64) int {
	if !v.Enabled() {
		return 0
	}
	return cached(v.f, "favorites.count:"+itoa(int(postID)), func() (int, error) {
		return v.f.store.Favorites().CountFavorites(v.f.ctx, postID)
	})
}

// Has 报告**当前登录用户**是否收藏过这篇内容；匿名访客恒为 false。
//
// 用户 ID 从请求上下文里取而不是让模板传：模板里根本没有可信的用户 ID 可传，
// 传进来的任何值都等于让主题替别人查收藏状态。
func (v FavoritesFinder) Has(postID int64) bool {
	if !v.Enabled() {
		return false
	}
	userID := viewerIDFrom(v.f.ctx)
	if userID <= 0 {
		return false
	}
	return cached(v.f, "favorites.has:"+itoa(int(postID)), func() (bool, error) {
		return v.f.store.Favorites().HasFavorite(v.f.ctx, userID, postID)
	})
}

// ---------- 视图类型 ----------

// TermView 是分类或标签在模板中的视图，附带文章数。
type TermView struct {
	ID          int64
	Name        string
	Slug        string
	Description string
	// Color 仅标签有值。
	Color string
	// CoverURL 仅分类有值。
	CoverURL string
	// URL 是该分类或标签的归档页地址。
	URL string
	// Count 是其下已发布且公开的文章数。
	Count int
	// Weight 是标签云的字号权重，取值 1–5；非标签云场景为 0。
	Weight int
}

// CategoryNode 是带文章数的分类树节点。
type CategoryNode struct {
	TermView
	Children []*CategoryNode
}

// ArchiveEntry 是一个归档区间。
type ArchiveEntry struct {
	Year int
	// Month 为 0 表示按年归档。
	Month int
	Count int
	// Label 是可读标题，如「2026 年 9 月」。
	Label string
	// URL 是归档页地址。
	URL string
}

// MenuItemView 是菜单条目在模板中的视图。
type MenuItemView struct {
	Label string
	URL   string
	// Target 为 _blank 时在新窗口打开。
	Target string
	Rel    string
	// Children 是子条目。
	Children []MenuItemView
}

// CommentView 是评论在模板中的视图。
//
// 字段刻意受限：邮箱、IP 与 UA 一律不进模板——主题是第三方代码，
// 一旦给了它就等于给了所有装这个主题的站点一个数据泄漏口子（agent.md §8）。
type CommentView struct {
	ID         int64
	AuthorName string
	// AuthorURL 是访客填的主页，已校验过协议。
	AuthorURL string
	// ContentHTML 是转义并富化后的内容，模板里须用 safeHTML 输出。
	ContentHTML template.HTML
	CreatedAt   time.Time
	// IsAuthor 为真表示这条评论来自内容作者本人。
	IsAuthor bool
	// PostTitle 与 PostURL 仅在「最近评论」里填充。
	PostTitle string
	PostURL   string
}

// CommentNode 是评论树节点。
type CommentNode struct {
	CommentView
	Children []*CommentNode
}

// withWeights 给标签云的条目按文章数分配 1–5 的权重。
//
// 按名次而非绝对数量分档：一个站点可能最热标签 500 篇、次热 3 篇，
// 按数量线性映射会让除最热之外的全挤在最小号。
func withWeights(tags []TermView) []TermView {
	if len(tags) == 0 {
		return tags
	}
	maxCount := 0
	minCount := tags[0].Count
	for _, t := range tags {
		maxCount = max(maxCount, t.Count)
		minCount = min(minCount, t.Count)
	}
	span := maxCount - minCount
	for i := range tags {
		if span == 0 {
			tags[i].Weight = 3
			continue
		}
		// 5 档，向上取整保证最热的一定是 5。
		tags[i].Weight = 1 + (tags[i].Count-minCount)*4/span
	}
	return tags
}
