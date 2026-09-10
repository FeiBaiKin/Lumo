package seo

import (
	"context"
	"fmt"
	"time"

	"github.com/uptrace/bun"
)

// Entry 是一条可供索引或订阅的内容记录。
//
// 直接用 bun 读 posts 表：SEO 只关心标题、摘要、发布时间与更新时间，
// 为此引入对 content 模块的编译期依赖并不划算。
type Entry struct {
	ID          int64     `bun:"id"`
	Type        string    `bun:"type"`
	Title       string    `bun:"title"`
	Slug        string    `bun:"slug"`
	Excerpt     string    `bun:"excerpt"`
	Content     string    `bun:"content"`
	PublishedAt time.Time `bun:"published_at"`
	UpdatedAt   time.Time `bun:"updated_at"`
	AuthorName  string    `bun:"author_name"`

	// URL 由服务层按站点地址与路径前缀补全。
	URL string `bun:"-"`
	// Summary 是摘要；订阅源在非全文模式下用它。
	Summary string `bun:"-"`
	// ContentHTML 是全文；订阅源在全文模式下用它。
	ContentHTML string `bun:"-"`
}

// Store 提供内容元数据的只读查询。
type Store struct {
	db *bun.DB
}

// NewStore 构造 Store。
func NewStore(db *bun.DB) *Store { return &Store{db: db} }

// entrySelect 是索引与订阅共用的查询：公开、已发布、非置顶无关的内容。
//
// 私密内容与未发布内容一律不进 sitemap 与订阅源——把它们列出去，
// 等于把「这里有一篇文章」告诉搜索引擎，而它其实打不开。
const entrySelect = `
SELECT p.id, p.type, p.title, p.slug, p.excerpt, p.content,
       COALESCE(p.published_at, p.updated_at) AS published_at,
       p.updated_at, COALESCE(u.display_name, u.username, '') AS author_name
FROM posts AS p
LEFT JOIN users AS u ON u.id = p.author_id
WHERE p.status = 'published' AND p.visibility = 'public'`

// ListPosts 返回已发布且公开的文章，按发布时间倒序。
func (s *Store) ListPosts(ctx context.Context, limit int) ([]Entry, error) {
	return s.query(ctx, entrySelect+" AND p.type = 'post' ORDER BY published_at DESC, p.id DESC", limit)
}

// ListAll 返回已发布且公开的文章与页面，按发布时间倒序，供 sitemap 使用。
func (s *Store) ListAll(ctx context.Context, limit int) ([]Entry, error) {
	return s.query(ctx, entrySelect+" ORDER BY published_at DESC, p.id DESC", limit)
}

// ListCategories 返回全部分类，供 sitemap 使用。
func (s *Store) ListCategories(ctx context.Context) ([]Entry, error) {
	out := []Entry{}
	err := s.db.NewRaw(
		"SELECT id, slug, name AS title, updated_at FROM categories ORDER BY id").Scan(ctx, &out)
	if err != nil {
		return nil, fmt.Errorf("查询分类: %w", err)
	}
	return out, nil
}

// ListTags 返回全部标签，供 sitemap 使用。
func (s *Store) ListTags(ctx context.Context) ([]Entry, error) {
	out := []Entry{}
	err := s.db.NewRaw(
		"SELECT id, slug, name AS title, updated_at FROM tags ORDER BY id").Scan(ctx, &out)
	if err != nil {
		return nil, fmt.Errorf("查询标签: %w", err)
	}
	return out, nil
}

// GetPost 按 slug 取一条已发布且公开的内容，供生成单页 SEO 元信息。
func (s *Store) GetPost(ctx context.Context, kind, slug string) (*Entry, error) {
	out := []Entry{}
	err := s.db.NewRaw(entrySelect+" AND p.type = ? AND p.slug = ?", kind, slug).Scan(ctx, &out)
	if err != nil {
		return nil, fmt.Errorf("查询内容: %w", err)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return &out[0], nil
}

// query 执行一次内容查询；limit <= 0 表示不限制。
//
// 套一层子查询而不是直接拼 LIMIT：entrySelect 自带 ORDER BY，
// 在外层加限制才能既保住排序又拿到前 N 条。
func (s *Store) query(ctx context.Context, sql string, limit int) ([]Entry, error) {
	out := []Entry{}
	q := s.db.NewRaw(sql)
	if limit > 0 {
		q = s.db.NewRaw("SELECT * FROM ("+sql+") AS entries LIMIT ?", limit)
	}
	if err := q.Scan(ctx, &out); err != nil {
		return nil, fmt.Errorf("查询内容: %w", err)
	}
	return out, nil
}
