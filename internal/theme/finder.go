package theme

import (
	"context"
	"html/template"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Finder 是供模板调用的只读数据访问面。
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

// cached 缓存一次查询结果：本次请求内先查请求级缓存，再查跨请求的共享缓存（见 sharedCache）。
//
// 页头页脚常取同一个菜单或同一批热门标签，请求级缓存省掉重复查询；共享缓存让下一个访客
// 不必再查一遍，它在写入时立刻失效，改了菜单刷新就能看到。
func cached[T any](f *Finder, key string, build func() (T, error)) T {
	return cachedIn(f, key, true, build)
}

// cachedForViewer 只在本次请求内缓存：结果因当前用户而异，不能给下一个访客。
func cachedForViewer[T any](f *Finder, key string, build func() (T, error)) T {
	return cachedIn(f, key, false, build)
}

func cachedIn[T any](f *Finder, key string, shared bool, build func() (T, error)) T {
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
	var value T
	var err error
	if shared {
		value, err = remember(f.store.shared, key, build)
	} else {
		value, err = build()
	}
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

// List 按前台列表的顺序返回前 n 篇：置顶在前，再按发布时间倒序。
//
// 首页「最新文章」这类主列表用它，与 /posts、分类页的顺序一致；侧栏的「最新」仍用 Recent，只看时间。
func (p PostsFinder) List(n int) []PostView {
	n = clamp(n)
	return cached(p.f, "posts.list:"+itoa(n), func() ([]PostView, error) {
		return p.f.store.ListPosts(p.f.ctx, n)
	})
}

// Pinned 返回置顶文章。
func (p PostsFinder) Pinned(n int) []PostView {
	n = clamp(n)
	return cached(p.f, "posts.pinned:"+itoa(n), func() ([]PostView, error) {
		return p.f.store.PinnedPosts(p.f.ctx, n)
	})
}

// Popular 返回按评论数排序的热门文章（判据见 Store.PopularPosts）。
func (p PostsFinder) Popular(n int) []PostView {
	n = clamp(n)
	return cached(p.f, "posts.popular:"+itoa(n), func() ([]PostView, error) {
		return p.f.store.PopularPosts(p.f.ctx, n)
	})
}

// Adjacent 返回上一篇与下一篇，供文章页的翻页链接用。
//
// 返回一个结构体而不是两个值：模板里 {{ $a := .Find.Posts.Adjacent $id }} 之后
// 再 {{ with $a.Prev }} 比解构两值更清楚地表达「可能没有上一篇」。
type Adjacent struct {
	// Prev 是更早发布的一篇。
	Prev *PostView
	// Next 是更晚发布的一篇。
	Next *PostView
}

// Adjacent 返回与给定文章相邻的两篇（按发布时间）。
//
// 文章没有发布时间（草稿不该走到这里，但模板可能被别处调用）时返回两个 nil，
// 模板的 {{ with }} 会自然跳过。
func (p PostsFinder) Adjacent(postID int64, publishedAt time.Time) Adjacent {
	if publishedAt.IsZero() {
		return Adjacent{}
	}
	key := "posts.adjacent:" + itoa(int(postID))
	type pair struct{ prev, next *PostView }
	result := cached(p.f, key, func() (pair, error) {
		prev, next, err := p.f.store.AdjacentPosts(p.f.ctx, postID, publishedAt)
		return pair{prev: prev, next: next}, err
	})
	return Adjacent{Prev: result.prev, Next: result.next}
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
		tags, err := t.f.store.Tags(t.f.ctx, 0)
		if err != nil {
			return nil, err
		}
		return withHues(tags), nil
	})
}

// Cloud 返回按文章数倒序的前 n 个标签。
//
// 顺序就是热度的唯一表达：字号不参与编码（见 withHues）。
func (t TagsFinder) Cloud(n int) []TermView {
	n = clamp(n)
	return cached(t.f, "tags.cloud:"+itoa(n), func() ([]TermView, error) {
		tags, err := t.f.store.Tags(t.f.ctx, n)
		if err != nil {
			return nil, err
		}
		return withHues(tags), nil
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
	return cachedForViewer(v.f, "favorites.has:"+itoa(int(postID)), func() (bool, error) {
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
	// Hue 是这枚标签的色相（0–359），CSS 用它拼出底色与文字色。
	// 站长设过颜色就用那个颜色的色相，没设过由标识稳定算出（见 tagHue）。
	// 分类不用它，为零值。
	Hue int
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
// 一旦给了它就等于给了所有装这个主题的站点一个数据泄漏口子。
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

// withHues 给每枚标签算一个色相。
//
// 2026-09-15 改：此前标签云按文章数分 1–5 档字号，同一屏里的标签因此高低不齐——
// 站长要的是「大小统一、形状统一、文字居中，颜色可以各不相同」。热度改由**顺序**
// 表达（Cloud 仍按文章数倒序取前 n 个），不再占用字号这个通道。
//
// 色相的计算见 tagHue。
func withHues(tags []TermView) []TermView {
	for i := range tags {
		tags[i].Hue = tagHue(&tags[i])
	}
	return tags
}

// tagHue 返回一枚标签的色相（0–359）。
//
// 两条来源，站长优先：
//
//  1. 后台给这个标签设过颜色 → 用它。**只取色相**，深浅由主题按明暗模式定：
//     一排标签的对比度必须一致，而站长给的颜色（比如浅黄）直接拿来当文字色会读不清。
//  2. 没设过 → 由标签标识稳定地算一个。稳定是硬要求：同一个标签在列表、文章页、
//     标签云里必须是同一个颜色，换一页就换色的话，颜色就不成其为标识了。
//
// 算法是「哈希 → 有限调色板」而不是「哈希 → 0–359 连续取值」：连续取值下，
// 一屏里出现两枚几乎同色的标签是常态（十六枚里撞色是概率问题，不是运气问题），
// 而那比「两枚标签同色」更难看。
func tagHue(t *TermView) int {
	if hue, ok := hexHue(t.Color); ok {
		return hue
	}
	// 用 slug 而不是显示名：站长改标签名不该换颜色。
	// 没有 slug 时退回名字，至少还是个稳定值。
	seed := t.Slug
	if seed == "" {
		seed = t.Name
	}
	return paletteHues[int(hash32(seed)%uint32(len(paletteHues)))]
}

// paletteHues 是没设颜色时用的调色板，16 个大致等距的色相。
//
// 等距不是必需，但「大致均匀铺开」能让一屏标签看起来是调过的而不是随机的。
var paletteHues = []int{
	22, 44, 67, 89, 112, 134, 157, 179,
	202, 224, 247, 269, 292, 314, 337, 359,
}

// hash32 是 FNV-1a 32 位加一轮 murmur3 的收尾混合。
//
// 单用 FNV-1a 对本场景够用（它的低位是真正被搅动过的），
// 收尾混合是为了让「前缀相同的一串中文标签」也散得开——
// 中文标签常有「前端 / 前端工程 / 前端工程化」这种前缀族。
func hash32(s string) uint32 {
	const (
		offset = 2166136261
		prime  = 16777619
	)
	h := uint32(offset)
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= prime
	}
	// murmur3 的 fmix32
	h ^= h >> 16
	h *= 0x85ebca6b
	h ^= h >> 13
	h *= 0xc2b2ae35
	h ^= h >> 16
	return h
}

// hexHue 把站长设的 #rrggbb 转成 OKLCH 的色相角。
//
// 转 OKLCH 而不是 HSL：主题的标签色是用 oklch() 拼出来的，两者的色相角不是同一个刻度
// （同一个红色，HSL 是 0°、OKLCH 约 29°）。用 HSL 的色相角去喂 oklch() 会得到一个
// 完全不是站长选的那个颜色。
//
// 解析失败（空串、写坏的十六进制）返回 false，调用方退回按名字生成。
func hexHue(color string) (int, bool) {
	hex := strings.TrimPrefix(strings.TrimSpace(color), "#")
	if len(hex) != 6 {
		return 0, false
	}
	value, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		return 0, false
	}
	r := float64((value>>16)&0xff) / 255
	g := float64((value>>8)&0xff) / 255
	b := float64(value&0xff) / 255

	// sRGB → 线性（与 theme.css 里那套一致：阈值 0.04045，见 WCAG 的相对亮度定义）
	lin := func(c float64) float64 {
		if c <= 0.04045 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	r, g, b = lin(r), lin(g), lin(b)

	// 线性 sRGB → OKLab（Björn Ottosson 的矩阵）
	l := 0.4122214708*r + 0.5363325363*g + 0.0514459929*b
	m := 0.2119034982*r + 0.6806995451*g + 0.1073969566*b
	s := 0.0883024619*r + 0.2817188376*g + 0.6299787005*b

	l, m, s = math.Cbrt(l), math.Cbrt(m), math.Cbrt(s)

	// 只要色相，故只算 a 与 b 两个对立轴（L 用不上）。
	// 写成三行而不是挤成一行：这三行是三个不同的轴，混起来正是最容易犯的错。
	a := 1.9779984951*l - 2.4285922050*m + 0.4505937099*s
	bb := 0.0259040371*l + 0.7827717662*m - 0.8086757660*s

	hue := math.Atan2(bb, a) * 180 / math.Pi
	if hue < 0 {
		hue += 360
	}
	return int(math.Round(hue)) % 360, true
}
