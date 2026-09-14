package theme

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"html/template"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/FeiBaiKin/lumo/internal/content"
	"github.com/FeiBaiKin/lumo/internal/taxonomy"
)

// 前台路径前缀，与 internal/seo、internal/menu 的约定保持一致。
const (
	PathPosts      = "/posts/"
	PathCategories = "/categories/"
	PathTags       = "/tags/"
	PathArchives   = "/archives/"
	PathAuthors    = "/authors/"
	PathSearch     = "/search"
	// PathFavorites 是「我的收藏」页。
	//
	// 挂在 /account 下而不是做成顶层的 /favorites：顶层单段路径会遮蔽同名的独立页面
	// （agent.md §3.2 的已知限制，account 的那几条固定路径就是这么来的），
	// 而两段路径与 /{slug} 的兜底完全不相交，不必再往保留字清单里添一个词。
	PathFavorites = "/account/favorites"
)

// 分类与标签在按词筛选时的区分。
const (
	termCategory = "category"
	termTag      = "tag"
)

// Store 提供前台渲染所需的只读查询。
//
// 全部查询都内建「已发布 + 公开」的过滤：前台不存在「忘了加 where」导致
// 草稿泄漏的可能——那是 CMS 最不能犯的错。私密内容对作者本人的可见性
// 由路由层单独处理，不走这里。
type Store struct {
	db        *bun.DB
	searcher  Searcher
	favorites Favoriter
}

// NewStore 构造 Store。
func NewStore(db *bun.DB) *Store { return &Store{db: db} }

// Searcher 是前台搜索页需要的能力：关键词进，按相关度排好序的文章 ID 与总数出。
//
// 只要 ID 与顺序，作者 / 分类 / 标签仍由主题自己的查询补齐——这样搜索引擎换成
// 什么实现都与前台无关（agent.md §2）。
type Searcher interface {
	SearchPostIDs(ctx context.Context, query string, limit, offset int) ([]int64, int, error)
}

// UseSearcher 注入搜索实现；不注入时搜索页退回标题与摘要的模糊匹配。
func (s *Store) UseSearcher(searcher Searcher) { s.searcher = searcher }

// Favoriter 是收藏页与文章页收藏按钮需要的能力，由 internal/favorite 实现。
//
// 与 Searcher 同一种分工：收藏模块只回答「谁收了哪几篇」「这篇被收了多少次」，
// 出的是 ID 与计数；列表上那一列文章由本包自己的查询补齐。
// 主题因此不必知道收藏存在哪张表，收藏模块也不必知道前台怎么展示一篇文章。
type Favoriter interface {
	// FavoritePostIDs 按收藏时间倒序返回某人收藏的、当前仍对访客可见的内容 ID 与总数。
	FavoritePostIDs(ctx context.Context, userID int64, limit, offset int) ([]int64, int, error)
	// HasFavorite 报告某人是否收藏过某篇内容。
	HasFavorite(ctx context.Context, userID, postID int64) (bool, error)
	// CountFavorites 返回一篇内容被收藏的次数。
	CountFavorites(ctx context.Context, postID int64) (int, error)
}

// UseFavorites 注入收藏实现。
//
// 不注入时收藏页是空的、文章页不显示收藏按钮（见 Finder.Favorites 与 routes.go）——
// 收藏模块没装配就等于站点没有这个功能，而不是一个点了报错的按钮。
func (s *Store) UseFavorites(favorites Favoriter) { s.favorites = favorites }

// Favorites 返回已注入的收藏实现；未装配时为 nil。
func (s *Store) Favorites() Favoriter {
	if s == nil {
		return nil
	}
	return s.favorites
}

// FavoritePosts 分页返回某人收藏的内容，按收藏时间倒序。
//
// 未装配收藏模块时返回空列表而不是错误：收藏页在那种装配下仍然打得开，
// 只是永远是空的——这与搜索页在没有 search 模块时退回模糊匹配是同一条思路。
func (s *Store) FavoritePosts(ctx context.Context, userID int64, page, size int) ([]PostView, int, error) {
	if s.favorites == nil || userID <= 0 {
		return []PostView{}, 0, nil
	}
	offset := max(0, (page-1)*size)
	ids, total, err := s.favorites.FavoritePostIDs(ctx, userID, size, offset)
	if err != nil {
		return nil, 0, err
	}
	if len(ids) == 0 {
		return []PostView{}, total, nil
	}
	views, err := s.postsByIDs(ctx, ids)
	if err != nil {
		return nil, 0, err
	}
	return views, total, nil
}

// publicFilter 是所有前台查询共用的可见性条件。
const publicFilter = `p.status = 'published' AND p.visibility = 'public'`

// postColumns 是列表查询要取的列。
//
// 列表不取 content：首页 20 篇文章的正文动辄几百 KB，而列表只用摘要。
const postColumns = `p.id, p.type, p.title, p.slug, p.excerpt, p.cover_url, p.pinned,
	p.template, p.author_id, p.published_at, p.updated_at`

// postRow 是查询结果的中间形态。
type postRow struct {
	ID          int64        `bun:"id"`
	Type        string       `bun:"type"`
	Title       string       `bun:"title"`
	Slug        string       `bun:"slug"`
	Excerpt     string       `bun:"excerpt"`
	Content     string       `bun:"content"`
	CoverURL    string       `bun:"cover_url"`
	Pinned      bool         `bun:"pinned"`
	Template    string       `bun:"template"`
	AuthorID    int64        `bun:"author_id"`
	PublishedAt sql.NullTime `bun:"published_at"`
	UpdatedAt   time.Time    `bun:"updated_at"`
}

// toView 把查询行转成模板视图。
func (r *postRow) toView(author *AuthorView) PostView {
	view := PostView{
		ID:         r.ID,
		Type:       r.Type,
		Title:      r.Title,
		Slug:       r.Slug,
		URL:        ContentPath(r.Type, r.Slug),
		Content:    r.Content,
		Excerpt:    r.Excerpt,
		CoverURL:   r.CoverURL,
		Pinned:     r.Pinned,
		Template:   r.Template,
		Author:     author,
		Categories: []taxonomy.Category{},
		Tags:       []taxonomy.Tag{},
		UpdatedAt:  r.UpdatedAt,
	}
	if r.PublishedAt.Valid {
		view.PublishedAt = r.PublishedAt.Time
	}
	if r.Content != "" {
		text := content.StripTags(r.Content)
		view.WordCount = countWords(text)
		view.ReadingTime = readingMinutes(text)
		// 代码高亮在读取期做而不是写入期：库里存的始终是语义化的
		// <pre><code class="language-go">，换主题、换配色都不必重存全库
		// （理由见 internal/content/highlight.go 的开头）。着色结果有缓存。
		view.Content = content.Highlight(r.Content)
	}
	return view
}

// ---------- 列表 ----------

// listOrder 是前台列表的固定排序：置顶优先，再按发布时间倒序。
const listOrder = ` ORDER BY p.pinned DESC, p.published_at DESC NULLS LAST, p.id DESC`

// Posts 分页返回已发布且公开的文章。
func (s *Store) Posts(ctx context.Context, page, size int) ([]PostView, int, error) {
	where := publicFilter + ` AND p.type = 'post'`
	return s.pageQuery(ctx, where, nil, page, size)
}

// PostsByTerm 分页返回某个分类或标签下的文章。
func (s *Store) PostsByTerm(ctx context.Context, kind, slug string, page, size int) ([]PostView, int, error) {
	var where string
	switch kind {
	case termCategory:
		where = publicFilter + ` AND p.type = 'post' AND EXISTS (
			SELECT 1 FROM post_categories pc JOIN categories c ON c.id = pc.category_id
			WHERE pc.post_id = p.id AND c.slug = ?)`
	case termTag:
		where = publicFilter + ` AND p.type = 'post' AND EXISTS (
			SELECT 1 FROM post_tags pt JOIN tags t ON t.id = pt.tag_id
			WHERE pt.post_id = p.id AND t.slug = ?)`
	default:
		return nil, 0, fmt.Errorf("未知的分类维度 %q", kind)
	}
	return s.pageQuery(ctx, where, []any{slug}, page, size)
}

// PostsByAuthor 分页返回某作者的文章。
func (s *Store) PostsByAuthor(ctx context.Context, authorID int64, page, size int) ([]PostView, int, error) {
	where := publicFilter + ` AND p.type = 'post' AND p.author_id = ?`
	return s.pageQuery(ctx, where, []any{authorID}, page, size)
}

// PostsByArchive 分页返回某年（月）的文章。month 为 0 表示整年。
func (s *Store) PostsByArchive(ctx context.Context, year, month, page, size int) ([]PostView, int, error) {
	where := publicFilter + ` AND p.type = 'post' AND EXTRACT(YEAR FROM p.published_at) = ?`
	args := []any{year}
	if month > 0 {
		where += ` AND EXTRACT(MONTH FROM p.published_at) = ?`
		args = append(args, month)
	}
	return s.pageQuery(ctx, where, args, page, size)
}

// SearchPosts 分页返回命中关键词的文章，按相关度排序。
//
// 装配了 search 模块时走全文索引；否则退回标题与摘要的 ILIKE 模糊匹配，
// 保证主题在最小装配下仍有一个能用的搜索页。
func (s *Store) SearchPosts(ctx context.Context, query string, page, size int) ([]PostView, int, error) {
	query = strings.TrimSpace(query)
	if s.searcher == nil {
		pattern := "%" + escapeLike(query) + "%"
		where := publicFilter + ` AND p.type = 'post' AND (p.title ILIKE ? OR p.excerpt ILIKE ?)`
		return s.pageQuery(ctx, where, []any{pattern, pattern}, page, size)
	}

	offset := max(0, (page-1)*size)
	ids, total, err := s.searcher.SearchPostIDs(ctx, query, size, offset)
	if err != nil {
		return nil, 0, err
	}
	if len(ids) == 0 {
		return []PostView{}, total, nil
	}
	views, err := s.postsByIDs(ctx, ids)
	if err != nil {
		return nil, 0, err
	}
	return views, total, nil
}

// postsByIDs 按给定顺序取回文章视图。
//
// 顺序由搜索给出，SQL 的 IN 不保证返回次序，故在 Go 侧按 ids 重排；
// 期间消失的内容（刚被删或撤回）直接跳过，不占位。
func (s *Store) postsByIDs(ctx context.Context, ids []int64) ([]PostView, error) {
	sqlText := `SELECT ` + postColumns + ` FROM posts AS p WHERE ` + publicFilter + ` AND p.id IN (?)`
	rows := []postRow{}
	if err := s.db.NewRaw(sqlText, bun.List(ids)).Scan(ctx, &rows); err != nil {
		return nil, fmt.Errorf("按 ID 取回文章: %w", err)
	}

	views, err := s.attach(ctx, rows)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]PostView, len(views))
	for i := range views {
		byID[views[i].ID] = views[i]
	}
	out := make([]PostView, 0, len(ids))
	for _, id := range ids {
		if view, ok := byID[id]; ok {
			out = append(out, view)
		}
	}
	return out, nil
}

// pageQuery 执行一次分页列表查询并补上作者、分类与标签。
func (s *Store) pageQuery(ctx context.Context, where string, args []any, page, size int) (
	[]PostView, int, error,
) {
	total, err := s.count(ctx, where, args)
	if err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []PostView{}, 0, nil
	}

	offset := max(0, (page-1)*size)
	sqlText := `SELECT ` + postColumns + ` FROM posts AS p WHERE ` + where + listOrder +
		` LIMIT ? OFFSET ?`
	rows := []postRow{}
	if scanErr := s.db.NewRaw(sqlText, append(append([]any{}, args...), size, offset)...).
		Scan(ctx, &rows); scanErr != nil {
		return nil, 0, fmt.Errorf("查询内容列表: %w", scanErr)
	}

	views, err := s.attach(ctx, rows)
	if err != nil {
		return nil, 0, err
	}
	return views, total, nil
}

// count 统计满足条件的内容数。
func (s *Store) count(ctx context.Context, where string, args []any) (int, error) {
	var total int
	if err := s.db.NewRaw(`SELECT count(*) FROM posts AS p WHERE `+where, args...).
		Scan(ctx, &total); err != nil {
		return 0, fmt.Errorf("统计内容: %w", err)
	}
	return total, nil
}

// CountPosts 返回已发布且公开的文章总数。
func (s *Store) CountPosts(ctx context.Context) (int, error) {
	return s.count(ctx, publicFilter+` AND p.type = 'post'`, nil)
}

// ---------- 单条 ----------

// GetContent 按类型与 slug 取一条内容（含正文）。
//
// viewerID 非 0 时额外放行该用户自己的私密内容——作者预览自己的私密文章
// 不该看到 404。草稿与回收站内容一律不可见，预览走 Console。
func (s *Store) GetContent(ctx context.Context, kind, slug string, viewerID int64) (*PostView, error) {
	where := `p.type = ? AND p.slug = ? AND p.status = 'published'`
	args := []any{kind, slug}
	if viewerID == 0 {
		where += ` AND p.visibility = 'public'`
	} else {
		where += ` AND (p.visibility = 'public' OR p.author_id = ?)`
		args = append(args, viewerID)
	}

	rows := []postRow{}
	sqlText := `SELECT ` + postColumns + `, p.content FROM posts AS p WHERE ` + where + ` LIMIT 1`
	if err := s.db.NewRaw(sqlText, args...).Scan(ctx, &rows); err != nil {
		return nil, fmt.Errorf("查询内容: %w", err)
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}

	views, err := s.attach(ctx, rows)
	if err != nil {
		return nil, err
	}
	return &views[0], nil
}

// attach 批量补上作者、分类与标签，避免列表页的 N+1 查询。
func (s *Store) attach(ctx context.Context, rows []postRow) ([]PostView, error) {
	if len(rows) == 0 {
		return []PostView{}, nil
	}

	postIDs := make([]int64, 0, len(rows))
	authorIDs := make([]int64, 0, len(rows))
	for i := range rows {
		postIDs = append(postIDs, rows[i].ID)
		authorIDs = append(authorIDs, rows[i].AuthorID)
	}

	authors, err := s.authorsByIDs(ctx, authorIDs)
	if err != nil {
		return nil, err
	}
	categories, err := s.termsByPost(ctx, postIDs, termCategory)
	if err != nil {
		return nil, err
	}
	tags, err := s.termsByPost(ctx, postIDs, termTag)
	if err != nil {
		return nil, err
	}

	out := make([]PostView, 0, len(rows))
	for i := range rows {
		view := rows[i].toView(authors[rows[i].AuthorID])
		for _, t := range categories[rows[i].ID] {
			view.Categories = append(view.Categories, taxonomy.Category{
				ID: t.ID, Name: t.Name, Slug: t.Slug, Description: t.Description, CoverURL: t.CoverURL,
			})
		}
		for _, t := range tags[rows[i].ID] {
			view.Tags = append(view.Tags, taxonomy.Tag{
				ID: t.ID, Name: t.Name, Slug: t.Slug, Description: t.Description, Color: t.Color,
			})
		}
		out = append(out, view)
	}
	return out, nil
}

// termsByPost 批量取内容关联的分类或标签。
func (s *Store) termsByPost(ctx context.Context, postIDs []int64, kind string) (map[int64][]TermView, error) {
	var sqlText string
	switch kind {
	case termCategory:
		sqlText = `SELECT pc.post_id, c.id, c.name, c.slug, c.description, c.cover_url, '' AS color
			FROM post_categories pc JOIN categories c ON c.id = pc.category_id
			WHERE pc.post_id IN (?) ORDER BY c.position, c.name, c.id`
	case termTag:
		sqlText = `SELECT pt.post_id, t.id, t.name, t.slug, t.description, '' AS cover_url, t.color
			FROM post_tags pt JOIN tags t ON t.id = pt.tag_id
			WHERE pt.post_id IN (?) ORDER BY t.name, t.id`
	default:
		return nil, fmt.Errorf("未知的分类维度 %q", kind)
	}

	var rows []struct {
		PostID      int64  `bun:"post_id"`
		ID          int64  `bun:"id"`
		Name        string `bun:"name"`
		Slug        string `bun:"slug"`
		Description string `bun:"description"`
		CoverURL    string `bun:"cover_url"`
		Color       string `bun:"color"`
	}
	if err := s.db.NewRaw(sqlText, bun.List(postIDs)).Scan(ctx, &rows); err != nil {
		return nil, fmt.Errorf("查询内容关联: %w", err)
	}

	prefix := PathCategories
	if kind == termTag {
		prefix = PathTags
	}
	out := make(map[int64][]TermView, len(postIDs))
	for _, r := range rows {
		out[r.PostID] = append(out[r.PostID], TermView{
			ID: r.ID, Name: r.Name, Slug: r.Slug, Description: r.Description,
			CoverURL: r.CoverURL, Color: r.Color, URL: prefix + r.Slug,
		})
	}
	return out, nil
}

// ---------- 作者 ----------

// authorsByIDs 批量取作者的公开信息。
func (s *Store) authorsByIDs(ctx context.Context, ids []int64) (map[int64]*AuthorView, error) {
	out := map[int64]*AuthorView{}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []struct {
		ID          int64  `bun:"id"`
		Username    string `bun:"username"`
		DisplayName string `bun:"display_name"`
		AvatarURL   string `bun:"avatar_url"`
		Bio         string `bun:"bio"`
	}
	if err := s.db.NewRaw(
		`SELECT id, username, display_name, avatar_url, bio FROM users WHERE id IN (?)`,
		bun.List(ids)).Scan(ctx, &rows); err != nil {
		return nil, fmt.Errorf("查询作者: %w", err)
	}
	for i := range rows {
		r := rows[i]
		name := r.DisplayName
		if strings.TrimSpace(name) == "" {
			name = r.Username
		}
		out[r.ID] = &AuthorView{
			ID: r.ID, Username: r.Username, DisplayName: name,
			AvatarURL: r.AvatarURL, Bio: r.Bio, URL: PathAuthors + r.Username,
		}
	}
	return out, nil
}

// GetAuthor 按用户名取作者的公开信息。
//
// 停用的账号仍可访问：作者离职不代表他写过的文章要连同署名页一起消失。
func (s *Store) GetAuthor(ctx context.Context, username string) (*AuthorView, error) {
	var rows []struct {
		ID          int64  `bun:"id"`
		Username    string `bun:"username"`
		DisplayName string `bun:"display_name"`
		AvatarURL   string `bun:"avatar_url"`
		Bio         string `bun:"bio"`
	}
	if err := s.db.NewRaw(
		`SELECT id, username, display_name, avatar_url, bio FROM users WHERE username = ? LIMIT 1`,
		username).Scan(ctx, &rows); err != nil {
		return nil, fmt.Errorf("查询作者: %w", err)
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	r := rows[0]
	name := r.DisplayName
	if strings.TrimSpace(name) == "" {
		name = r.Username
	}
	return &AuthorView{
		ID: r.ID, Username: r.Username, DisplayName: name,
		AvatarURL: r.AvatarURL, Bio: r.Bio, URL: PathAuthors + r.Username,
	}, nil
}

// ---------- Finder 支撑查询 ----------

// RecentPosts 返回最近发布的文章。
func (s *Store) RecentPosts(ctx context.Context, n int) ([]PostView, error) {
	rows := []postRow{}
	sqlText := `SELECT ` + postColumns + ` FROM posts AS p WHERE ` + publicFilter +
		` AND p.type = 'post' ORDER BY p.published_at DESC NULLS LAST, p.id DESC LIMIT ?`
	if err := s.db.NewRaw(sqlText, n).Scan(ctx, &rows); err != nil {
		return nil, fmt.Errorf("查询最新文章: %w", err)
	}
	return s.attach(ctx, rows)
}

// PinnedPosts 返回置顶文章。
func (s *Store) PinnedPosts(ctx context.Context, n int) ([]PostView, error) {
	rows := []postRow{}
	sqlText := `SELECT ` + postColumns + ` FROM posts AS p WHERE ` + publicFilter +
		` AND p.type = 'post' AND p.pinned ORDER BY p.published_at DESC NULLS LAST, p.id DESC LIMIT ?`
	if err := s.db.NewRaw(sqlText, n).Scan(ctx, &rows); err != nil {
		return nil, fmt.Errorf("查询置顶文章: %w", err)
	}
	return s.attach(ctx, rows)
}

// RelatedPosts 返回与给定文章共享分类或标签最多的其他文章。
//
// 按共享词条数排序而非随机：共享 3 个标签的文章显然比共享 1 个的更相关。
func (s *Store) RelatedPosts(ctx context.Context, postID int64, n int) ([]PostView, error) {
	const sqlText = `
WITH terms AS (
    SELECT 'c' AS kind, category_id AS term_id FROM post_categories WHERE post_id = ?
    UNION ALL
    SELECT 't', tag_id FROM post_tags WHERE post_id = ?
),
related AS (
    SELECT pc.post_id, count(*) AS shared FROM post_categories pc
    JOIN terms ON terms.kind = 'c' AND terms.term_id = pc.category_id
    WHERE pc.post_id <> ? GROUP BY pc.post_id
    UNION ALL
    SELECT pt.post_id, count(*) FROM post_tags pt
    JOIN terms ON terms.kind = 't' AND terms.term_id = pt.tag_id
    WHERE pt.post_id <> ? GROUP BY pt.post_id
),
ranked AS (
    SELECT post_id, sum(shared) AS score FROM related GROUP BY post_id
)
SELECT p.id, p.type, p.title, p.slug, p.excerpt, p.cover_url, p.pinned,
       p.template, p.author_id, p.published_at, p.updated_at
FROM posts AS p JOIN ranked ON ranked.post_id = p.id
WHERE ` + publicFilter + ` AND p.type = 'post'
ORDER BY ranked.score DESC, p.published_at DESC NULLS LAST, p.id DESC
LIMIT ?`

	rows := []postRow{}
	if err := s.db.NewRaw(sqlText, postID, postID, postID, postID, n).Scan(ctx, &rows); err != nil {
		return nil, fmt.Errorf("查询相关文章: %w", err)
	}
	return s.attach(ctx, rows)
}

// termCountSelect 统计每个分类或标签下已发布且公开的文章数。
const categoryCountSelect = `
SELECT c.id, c.parent_id, c.name, c.slug, c.description, c.cover_url, c.position,
       COALESCE(n.total, 0) AS total
FROM categories c
LEFT JOIN (
    SELECT pc.category_id, count(*) AS total FROM post_categories pc
    JOIN posts p ON p.id = pc.post_id
    WHERE ` + publicFilter + ` AND p.type = 'post'
    GROUP BY pc.category_id
) n ON n.category_id = c.id
ORDER BY c.parent_id NULLS FIRST, c.position, c.name, c.id`

// categoryRow 是分类查询的中间形态。
type categoryRow struct {
	ID          int64         `bun:"id"`
	ParentID    sql.NullInt64 `bun:"parent_id"`
	Name        string        `bun:"name"`
	Slug        string        `bun:"slug"`
	Description string        `bun:"description"`
	CoverURL    string        `bun:"cover_url"`
	Position    int           `bun:"position"`
	Total       int           `bun:"total"`
}

// Categories 返回平铺的分类列表，每个带文章数。
func (s *Store) Categories(ctx context.Context) ([]TermView, error) {
	rows, err := s.categoryRows(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]TermView, 0, len(rows))
	for i := range rows {
		out = append(out, rows[i].toTerm())
	}
	return out, nil
}

// CategoryTree 返回带文章数的分类树。
func (s *Store) CategoryTree(ctx context.Context) ([]*CategoryNode, error) {
	rows, err := s.categoryRows(ctx)
	if err != nil {
		return nil, err
	}

	nodes := make(map[int64]*CategoryNode, len(rows))
	for i := range rows {
		nodes[rows[i].ID] = &CategoryNode{TermView: rows[i].toTerm(), Children: []*CategoryNode{}}
	}
	roots := make([]*CategoryNode, 0, len(rows))
	for i := range rows {
		node := nodes[rows[i].ID]
		if !rows[i].ParentID.Valid {
			roots = append(roots, node)
			continue
		}
		// 父分类不存在时把节点提为根，而不是整支丢掉：
		// 站长在前台看不到某个分类远比看不出原因更难排查。
		parent, ok := nodes[rows[i].ParentID.Int64]
		if !ok {
			roots = append(roots, node)
			continue
		}
		parent.Children = append(parent.Children, node)
	}
	return roots, nil
}

// categoryRows 查询分类及其文章数。
func (s *Store) categoryRows(ctx context.Context) ([]categoryRow, error) {
	rows := []categoryRow{}
	if err := s.db.NewRaw(categoryCountSelect).Scan(ctx, &rows); err != nil {
		return nil, fmt.Errorf("查询分类: %w", err)
	}
	return rows, nil
}

// toTerm 把分类行转成模板视图。
func (r *categoryRow) toTerm() TermView {
	return TermView{
		ID: r.ID, Name: r.Name, Slug: r.Slug, Description: r.Description,
		CoverURL: r.CoverURL, URL: PathCategories + r.Slug, Count: r.Total,
	}
}

// Tags 返回标签列表；limit > 0 时按文章数倒序取前 limit 个，否则按名称排序取全部。
func (s *Store) Tags(ctx context.Context, limit int) ([]TermView, error) {
	sqlText := `
SELECT t.id, t.name, t.slug, t.description, t.color, COALESCE(n.total, 0) AS total
FROM tags t
LEFT JOIN (
    SELECT pt.tag_id, count(*) AS total FROM post_tags pt
    JOIN posts p ON p.id = pt.post_id
    WHERE ` + publicFilter + ` AND p.type = 'post'
    GROUP BY pt.tag_id
) n ON n.tag_id = t.id`
	args := []any{}
	if limit > 0 {
		sqlText += ` WHERE COALESCE(n.total, 0) > 0 ORDER BY total DESC, t.name, t.id LIMIT ?`
		args = append(args, limit)
	} else {
		sqlText += ` ORDER BY t.name, t.id`
	}

	var rows []struct {
		ID          int64  `bun:"id"`
		Name        string `bun:"name"`
		Slug        string `bun:"slug"`
		Description string `bun:"description"`
		Color       string `bun:"color"`
		Total       int    `bun:"total"`
	}
	if err := s.db.NewRaw(sqlText, args...).Scan(ctx, &rows); err != nil {
		return nil, fmt.Errorf("查询标签: %w", err)
	}
	out := make([]TermView, 0, len(rows))
	for _, r := range rows {
		out = append(out, TermView{
			ID: r.ID, Name: r.Name, Slug: r.Slug, Description: r.Description,
			Color: r.Color, URL: PathTags + r.Slug, Count: r.Total,
		})
	}
	return out, nil
}

// GetCategory 按 slug 取分类。
func (s *Store) GetCategory(ctx context.Context, slug string) (*taxonomy.Category, error) {
	c := new(taxonomy.Category)
	if err := s.db.NewSelect().Model(c).Where("c.slug = ?", slug).Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("查询分类: %w", err)
	}
	return c, nil
}

// GetTag 按 slug 取标签。
func (s *Store) GetTag(ctx context.Context, slug string) (*taxonomy.Tag, error) {
	t := new(taxonomy.Tag)
	if err := s.db.NewSelect().Model(t).Where("t.slug = ?", slug).Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("查询标签: %w", err)
	}
	return t, nil
}

// ArchivesByMonth 返回按月归档，新的在前。
func (s *Store) ArchivesByMonth(ctx context.Context) ([]ArchiveEntry, error) {
	const sqlText = `
SELECT EXTRACT(YEAR FROM p.published_at)::int AS year,
       EXTRACT(MONTH FROM p.published_at)::int AS month,
       count(*) AS total
FROM posts AS p
WHERE ` + publicFilter + ` AND p.type = 'post' AND p.published_at IS NOT NULL
GROUP BY year, month ORDER BY year DESC, month DESC`
	return s.archives(ctx, sqlText, true)
}

// ArchivesByYear 返回按年归档，新的在前。
func (s *Store) ArchivesByYear(ctx context.Context) ([]ArchiveEntry, error) {
	const sqlText = `
SELECT EXTRACT(YEAR FROM p.published_at)::int AS year, 0 AS month, count(*) AS total
FROM posts AS p
WHERE ` + publicFilter + ` AND p.type = 'post' AND p.published_at IS NOT NULL
GROUP BY year ORDER BY year DESC`
	return s.archives(ctx, sqlText, false)
}

// archives 执行归档统计查询。
func (s *Store) archives(ctx context.Context, sqlText string, withMonth bool) ([]ArchiveEntry, error) {
	var rows []struct {
		Year  int `bun:"year"`
		Month int `bun:"month"`
		Total int `bun:"total"`
	}
	if err := s.db.NewRaw(sqlText).Scan(ctx, &rows); err != nil {
		return nil, fmt.Errorf("统计归档: %w", err)
	}
	out := make([]ArchiveEntry, 0, len(rows))
	for _, r := range rows {
		entry := ArchiveEntry{Year: r.Year, Count: r.Total}
		if withMonth {
			entry.Month = r.Month
			entry.Label = strconv.Itoa(r.Year) + " 年 " + strconv.Itoa(r.Month) + " 月"
			entry.URL = PathArchives + strconv.Itoa(r.Year) + "/" + twoDigits(r.Month)
		} else {
			entry.Label = strconv.Itoa(r.Year) + " 年"
			entry.URL = PathArchives + strconv.Itoa(r.Year)
		}
		out = append(out, entry)
	}
	return out, nil
}

// ---------- 菜单 ----------

// menuRow 是菜单条目查询的中间形态。
type menuRow struct {
	ID       int64         `bun:"id"`
	ParentID sql.NullInt64 `bun:"parent_id"`
	Position int           `bun:"position"`
	Label    string        `bun:"label"`
	Type     string        `bun:"type"`
	TargetID sql.NullInt64 `bun:"target_id"`
	URL      string        `bun:"url"`
	Target   string        `bun:"target"`
	Rel      string        `bun:"rel"`
}

// Menu 按 slug 取菜单树，只含可见且目标仍存在的条目。
func (s *Store) Menu(ctx context.Context, slug string) ([]MenuItemView, error) {
	const sqlText = `
SELECT mi.id, mi.parent_id, mi.position, mi.label, mi.type, mi.target_id, mi.url, mi.target, mi.rel
FROM menu_items mi JOIN menus mn ON mn.id = mi.menu_id
WHERE mn.slug = ? AND mi.visible
ORDER BY mi.parent_id NULLS FIRST, mi.position, mi.id`

	rows := []menuRow{}
	if err := s.db.NewRaw(sqlText, slug).Scan(ctx, &rows); err != nil {
		return nil, fmt.Errorf("查询菜单: %w", err)
	}
	if len(rows) == 0 {
		return []MenuItemView{}, nil
	}
	return s.buildMenuTree(ctx, rows), nil
}

// buildMenuTree 把平铺条目组装成树，并就地解析站内条目的地址。
func (s *Store) buildMenuTree(ctx context.Context, rows []menuRow) []MenuItemView {
	// 站内条目的地址在读取时解析，记录改名或改 slug 后菜单自动跟随。
	// 解析不出来（记录已删或未发布）的条目在前台跳过，不留死链。
	kept := make([]menuRow, 0, len(rows))
	urls := make(map[int64]string, len(rows))
	for i := range rows {
		url, ok := s.resolveItemURL(ctx, &rows[i])
		if !ok {
			continue
		}
		urls[rows[i].ID] = url
		kept = append(kept, rows[i])
	}

	children := make(map[int64][]int64, len(kept))
	present := make(map[int64]bool, len(kept))
	byID := make(map[int64]*menuRow, len(kept))
	for i := range kept {
		present[kept[i].ID] = true
		byID[kept[i].ID] = &kept[i]
	}
	for i := range kept {
		if pid := kept[i].ParentID; pid.Valid && present[pid.Int64] {
			children[pid.Int64] = append(children[pid.Int64], kept[i].ID)
		}
	}

	var build func(id int64) MenuItemView
	build = func(id int64) MenuItemView {
		row := byID[id]
		view := MenuItemView{
			Label: row.Label, URL: urls[id], Target: row.Target, Rel: row.Rel,
			Children: []MenuItemView{},
		}
		for _, childID := range children[id] {
			view.Children = append(view.Children, build(childID))
		}
		return view
	}

	roots := make([]MenuItemView, 0, len(kept))
	for i := range kept {
		// 父项被跳过时把子项提为顶级，否则整支导航会凭空消失。
		if pid := kept[i].ParentID; !pid.Valid || !present[pid.Int64] {
			roots = append(roots, build(kept[i].ID))
		}
	}
	return roots
}

// resolveItemURL 解析一个菜单条目的地址；无法解析时返回 false 表示该条目应被跳过。
func (s *Store) resolveItemURL(ctx context.Context, row *menuRow) (string, bool) {
	if row.Type == "custom" {
		return row.URL, true
	}
	if !row.TargetID.Valid {
		return "", false
	}
	url, err := s.resolveTarget(ctx, row.Type, row.TargetID.Int64)
	if err != nil {
		return "", false
	}
	return url, true
}

// resolveTarget 解析站内条目的地址。
func (s *Store) resolveTarget(ctx context.Context, kind string, targetID int64) (string, error) {
	switch kind {
	case "post", "page":
		var rows []struct {
			Slug   string `bun:"slug"`
			Type   string `bun:"type"`
			Status string `bun:"status"`
		}
		if err := s.db.NewRaw(
			`SELECT slug, type, status FROM posts WHERE id = ?`, targetID).Scan(ctx, &rows); err != nil {
			return "", fmt.Errorf("查询菜单目标: %w", err)
		}
		if len(rows) == 0 || rows[0].Status != "published" {
			return "", ErrNotFound
		}
		return ContentPath(rows[0].Type, rows[0].Slug), nil
	case termCategory:
		return s.slugPath(ctx, "categories", PathCategories, targetID)
	case termTag:
		return s.slugPath(ctx, "tags", PathTags, targetID)
	}
	return "", ErrNotFound
}

// slugPath 取某张表的 slug 并拼成前台路径。
//
// table 只来自本文件内的固定字面量，不含用户输入，不存在注入面。
func (s *Store) slugPath(ctx context.Context, table, prefix string, id int64) (string, error) {
	var slugs []string
	if err := s.db.NewRaw(`SELECT slug FROM `+table+` WHERE id = ?`, id).Scan(ctx, &slugs); err != nil {
		return "", fmt.Errorf("查询菜单目标 slug: %w", err)
	}
	if len(slugs) == 0 {
		return "", ErrNotFound
	}
	return prefix + slugs[0], nil
}

// ---------- 评论 ----------

// CountComments 返回某条内容下已通过审核的评论数。
func (s *Store) CountComments(ctx context.Context, postID int64) (int, error) {
	var total int
	if err := s.db.NewRaw(
		`SELECT count(*) FROM comments WHERE post_id = ? AND status = 'approved'`,
		postID).Scan(ctx, &total); err != nil {
		return 0, fmt.Errorf("统计评论: %w", err)
	}
	return total, nil
}

// commentRow 是评论查询的中间形态。
//
// 只取前台该看到的列：邮箱、IP 与 UA 连查都不查，避免日后有人顺手加进视图。
type commentRow struct {
	ID          int64         `bun:"id"`
	ParentID    sql.NullInt64 `bun:"parent_id"`
	PostID      int64         `bun:"post_id"`
	AuthorName  string        `bun:"author_name"`
	AuthorURL   string        `bun:"author_url"`
	ContentHTML string        `bun:"content_html"`
	CreatedAt   time.Time     `bun:"created_at"`
	UserID      sql.NullInt64 `bun:"user_id"`
	PostTitle   string        `bun:"post_title"`
	PostType    string        `bun:"post_type"`
	PostSlug    string        `bun:"post_slug"`
	PostAuthor  sql.NullInt64 `bun:"post_author_id"`
}

// toView 把评论行转成模板视图。
func (r *commentRow) toView() CommentView {
	view := CommentView{
		ID:          r.ID,
		AuthorName:  r.AuthorName,
		AuthorURL:   r.AuthorURL,
		ContentHTML: template.HTML(r.ContentHTML), //nolint:gosec // 入库前已全文转义并有限富化
		CreatedAt:   r.CreatedAt,
	}
	if r.UserID.Valid && r.PostAuthor.Valid && r.UserID.Int64 == r.PostAuthor.Int64 {
		view.IsAuthor = true
	}
	if r.PostSlug != "" {
		view.PostTitle = r.PostTitle
		view.PostURL = ContentPath(r.PostType, r.PostSlug)
	}
	return view
}

// commentSelect 是评论查询的公共部分。
const commentSelect = `
SELECT cm.id, cm.parent_id, cm.post_id, cm.author_name, cm.author_url, cm.content_html,
       cm.created_at, cm.user_id, p.title AS post_title, p.type AS post_type,
       p.slug AS post_slug, p.author_id AS post_author_id
FROM comments cm JOIN posts p ON p.id = cm.post_id
WHERE cm.status = 'approved'`

// CommentTree 返回某条内容下已通过审核的评论树。
func (s *Store) CommentTree(ctx context.Context, postID int64) ([]*CommentNode, error) {
	rows := []commentRow{}
	if err := s.db.NewRaw(commentSelect+` AND cm.post_id = ? ORDER BY cm.created_at, cm.id`,
		postID).Scan(ctx, &rows); err != nil {
		return nil, fmt.Errorf("查询评论: %w", err)
	}

	nodes := make(map[int64]*CommentNode, len(rows))
	for i := range rows {
		nodes[rows[i].ID] = &CommentNode{CommentView: rows[i].toView(), Children: []*CommentNode{}}
	}
	roots := make([]*CommentNode, 0, len(rows))
	for i := range rows {
		node := nodes[rows[i].ID]
		if !rows[i].ParentID.Valid {
			roots = append(roots, node)
			continue
		}
		// 父评论不在结果里（未过审或已删）时整支丢弃：
		// 一条脱离上下文的回复对读者没有意义。
		parent, ok := nodes[rows[i].ParentID.Int64]
		if !ok {
			continue
		}
		parent.Children = append(parent.Children, node)
	}
	return roots, nil
}

// RecentComments 返回站点最近通过审核的评论。
func (s *Store) RecentComments(ctx context.Context, n int) ([]CommentView, error) {
	rows := []commentRow{}
	// 附带 publicFilter：挂在草稿或私密内容下的评论不该出现在侧栏，
	// 那等于把还没发布的内容标题泄漏出去。
	if err := s.db.NewRaw(commentSelect+` AND `+publicFilter+
		` ORDER BY cm.created_at DESC, cm.id DESC LIMIT ?`, n).Scan(ctx, &rows); err != nil {
		return nil, fmt.Errorf("查询最新评论: %w", err)
	}
	out := make([]CommentView, 0, len(rows))
	for i := range rows {
		out = append(out, rows[i].toView())
	}
	return out, nil
}

// ---------- 工具 ----------

// PageTemplatesInUse 返回内容表里实际用到的页面模板名，供后台校验。
func (s *Store) PageTemplatesInUse(ctx context.Context) ([]string, error) {
	var names []string
	if err := s.db.NewRaw(
		`SELECT DISTINCT template FROM posts WHERE type = 'page' AND template <> ''`).
		Scan(ctx, &names); err != nil {
		return nil, fmt.Errorf("查询页面模板: %w", err)
	}
	sort.Strings(names)
	return names, nil
}

// twoDigits 把月份补成两位。
func twoDigits(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

// escapeLike 转义 LIKE 模式中的通配符，让用户输入按字面匹配。
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
