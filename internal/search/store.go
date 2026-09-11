package search

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/FeiBaiKin/lumo/internal/content"
)

// publicFilter 是前台可见性条件，与主题前台保持逐字一致：
// 两处若各写各的，搜索就可能漏出草稿或私密内容。
const publicFilter = `p.status = 'published' AND p.visibility = 'public'`

// staleFilter 是「索引落后于内容」的判定：没有索引行，或索引所依据的版本已经过时。
const staleFilter = `s.post_id IS NULL OR s.indexed_at < p.updated_at`

// rankWeights 是 ts_rank_cd 的 {D,C,B,A} 权重：标题命中远重于正文命中。
const rankWeights = `{0.1, 0.2, 0.4, 1.0}`

// rankNormalization 是 ts_rank_cd 的归一化位掩码，直接拼进 SQL。
//
// 32 即 rank/(rank+1)，把分值压进 (0,1)：长文天然含更多词元，不做归一化时
// 一篇长文会仅仅因为「长」而压过标题精确命中的短文。
const rankNormalization = "32"

// Store 负责索引的读写。
type Store struct {
	db *bun.DB
}

// NewStore 构造 Store。
func NewStore(db *bun.DB) *Store { return &Store{db: db} }

// Doc 是一条待索引的内容。
type Doc struct {
	ID        int64     `bun:"id"`
	Title     string    `bun:"title"`
	Excerpt   string    `bun:"excerpt"`
	Content   string    `bun:"content"`
	UpdatedAt time.Time `bun:"updated_at"`
}

// Hit 是一条搜索结果。
type Hit struct {
	ID          int64      `bun:"id"           json:"id"`
	Type        string     `bun:"type"         json:"type" enum:"post,page"`
	Title       string     `bun:"title"        json:"title"`
	Slug        string     `bun:"slug"         json:"slug"`
	Excerpt     string     `bun:"excerpt"      json:"excerpt"`
	PublishedAt *time.Time `bun:"published_at" json:"publishedAt"`
	Score       float64    `bun:"score"        json:"score" doc:"相关度，只在同一次查询内可比"`
}

// Stats 是索引的进度快照。
type Stats struct {
	Total   int `json:"total"   doc:"内容总数"`
	Indexed int `json:"indexed" doc:"已建索引的条数"`
	Pending int `json:"pending" doc:"索引落后于内容、等待重建的条数"`
}

// Stale 取出索引落后于内容的行，最多 limit 条。
//
// 不排序：调用方会一批批取到取空为止，先处理哪些并不重要，而 ORDER BY 会平白
// 多一次排序——这条查询每两秒就要跑一次。
func (s *Store) Stale(ctx context.Context, limit int) ([]Doc, error) {
	rows := []Doc{}
	err := s.db.NewRaw(
		`SELECT p.id, p.title, p.excerpt, p.content, p.updated_at
		   FROM posts AS p
		   LEFT JOIN post_search AS s ON s.post_id = p.id
		  WHERE `+staleFilter+`
		  LIMIT ?`, limit).Scan(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("查询待索引内容: %w", err)
	}
	return rows, nil
}

// Index 为一条内容写入索引。
//
// indexed_at 写的是**这次读到的** updated_at 而不是 now()：若内容在读出之后又被改过，
// 它的 updated_at 会更大，这一行仍然算过期，下一轮自然会重建。写 now() 则会把
// 那次修改吞掉，索引从此停在旧内容上。
func (s *Store) Index(ctx context.Context, row *Doc) error {
	title := strings.Join(Tokenize(row.Title), " ")
	excerpt := strings.Join(Tokenize(row.Excerpt), " ")
	// 正文存的是渲染后的 HTML，直接切词会把标签名与类名一起收进索引。
	body := strings.Join(Tokenize(content.StripTags(row.Content)), " ")

	_, err := s.db.NewRaw(
		`INSERT INTO post_search (post_id, tsv, indexed_at)
		 VALUES (?, setweight(to_tsvector('simple', ?), 'A')
		          || setweight(to_tsvector('simple', ?), 'B')
		          || setweight(to_tsvector('simple', ?), 'D'), ?)
		 ON CONFLICT (post_id) DO UPDATE
		    SET tsv = EXCLUDED.tsv, indexed_at = EXCLUDED.indexed_at`,
		row.ID, title, excerpt, body, row.UpdatedAt).Exec(ctx)
	if err != nil {
		return fmt.Errorf("写入索引 (id=%d): %w", row.ID, err)
	}
	return nil
}

// MarkAllStale 清空索引，返回待重建的条数。切词规则改动后用它整体重建。
func (s *Store) MarkAllStale(ctx context.Context) (int, error) {
	if _, err := s.db.NewRaw(`DELETE FROM post_search`).Exec(ctx); err != nil {
		return 0, fmt.Errorf("清空索引: %w", err)
	}
	// 待重建的是内容条数，不是刚删掉的索引行数：从没索引过的内容也要算进去。
	var pending int
	if err := s.db.NewRaw(`SELECT count(*) FROM posts`).Scan(ctx, &pending); err != nil {
		return 0, fmt.Errorf("统计待重建条数: %w", err)
	}
	return pending, nil
}

// Stats 返回索引进度。
func (s *Store) Stats(ctx context.Context) (Stats, error) {
	var out Stats
	err := s.db.NewRaw(
		`SELECT count(*) AS total,
		        count(s.post_id) AS indexed,
		        count(*) FILTER (WHERE `+staleFilter+`) AS pending
		   FROM posts AS p
		   LEFT JOIN post_search AS s ON s.post_id = p.id`).
		Scan(ctx, &out.Total, &out.Indexed, &out.Pending)
	if err != nil {
		return Stats{}, fmt.Errorf("统计索引进度: %w", err)
	}
	return out, nil
}

// Search 按相关度分页返回命中的内容，只包含前台可见的条目。
//
// tsquery 由调用方给出（见 TSQuery），空串时不应走到这里。
func (s *Store) Search(ctx context.Context, tsquery string, types []string, limit, offset int) (
	[]Hit, int, error,
) {
	from := `FROM post_search AS s
	          JOIN posts AS p ON p.id = s.post_id
	          CROSS JOIN to_tsquery('simple', ?) AS q`
	where := `WHERE s.tsv @@ q AND ` + publicFilter
	args := []any{tsquery}
	if len(types) > 0 {
		where += ` AND p.type IN (?)`
		args = append(args, bun.List(types))
	}

	var total int
	if err := s.db.NewRaw(`SELECT count(*) `+from+` `+where, args...).Scan(ctx, &total); err != nil {
		return nil, 0, fmt.Errorf("统计搜索结果: %w", err)
	}
	if total == 0 {
		return []Hit{}, 0, nil
	}

	hits := []Hit{}
	listSQL := `SELECT p.id, p.type, p.title, p.slug, p.excerpt, p.published_at,
	                   ts_rank_cd('` + rankWeights + `', s.tsv, q, ` + rankNormalization + `) AS score
	            ` + from + ` ` + where + `
	             ORDER BY score DESC, p.published_at DESC NULLS LAST, p.id DESC
	             LIMIT ? OFFSET ?`
	if err := s.db.NewRaw(listSQL, append(append([]any{}, args...), limit, offset)...).
		Scan(ctx, &hits); err != nil {
		return nil, 0, fmt.Errorf("查询搜索结果: %w", err)
	}
	return hits, total, nil
}
