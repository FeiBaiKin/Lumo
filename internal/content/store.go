package content

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/FeiBaiKin/lumo/internal/api"
	"github.com/FeiBaiKin/lumo/internal/auth"
	"github.com/FeiBaiKin/lumo/internal/database"
	"github.com/FeiBaiKin/lumo/internal/taxonomy"
)

// 错误哨兵。处理器据此映射为 400 / 404 / 409。
var (
	// ErrNotFound 表示内容或修订不存在。
	ErrNotFound = errors.New("对象不存在")
	// ErrSlugTaken 表示同类型下 slug 已被占用。
	ErrSlugTaken = errors.New("slug 已被占用")
	// ErrTermNotFound 表示引用的分类或标签不存在。
	ErrTermNotFound = errors.New("分类或标签不存在")
	// ErrNotTrashed 表示内容不在回收站，无法彻底删除。
	ErrNotTrashed = errors.New("内容不在回收站")
	// ErrInvalid 表示字段违反了数据库约束。
	ErrInvalid = errors.New("字段不合法")
)

// RevisionsKept 是每篇内容保留的修订数上限，超出后最旧的被清理。
const RevisionsKept = 30

// Store 提供内容的持久化操作。
type Store struct {
	db    *bun.DB
	users *auth.Store
}

// NewStore 构造 Store。
func NewStore(db *bun.DB, users *auth.Store) *Store {
	return &Store{db: db, users: users}
}

// Filter 是列表筛选条件。
type Filter struct {
	Type Type
	// Status 为空表示回收站之外的全部状态。
	Status Status
	// AuthorID 非 0 时只取该作者的内容。
	AuthorID int64
	// CategorySlug / TagSlug 非空时按分类或标签筛选。
	CategorySlug string
	TagSlug      string
	// Query 非空时按标题模糊匹配。
	Query string
	// PublicOnly 为真时只取已发布内容：公开的，或属于 ViewerID 的私密内容。
	PublicOnly bool
	ViewerID   int64
}

// Page 分页返回内容并附上作者、分类与标签。
//
// 前台按置顶与发布时间排序，后台按更新时间排序——编辑者关心的是最近动过的内容。
func (s *Store) Page(ctx context.Context, f *Filter, params api.PageParams) ([]Post, int, error) {
	posts := []Post{}
	q := s.db.NewSelect().Model(&posts).Where("p.type = ?", f.Type)

	switch {
	case f.PublicOnly:
		q = q.Where("p.status = ?", StatusPublished)
		if f.ViewerID == 0 {
			q = q.Where("p.visibility = ?", VisibilityPublic)
		} else {
			q = q.Where("(p.visibility = ? OR p.author_id = ?)", VisibilityPublic, f.ViewerID)
		}
	case f.Status != "":
		q = q.Where("p.status = ?", f.Status)
	default:
		q = q.Where("p.status <> ?", StatusTrashed)
	}
	if f.AuthorID != 0 {
		q = q.Where("p.author_id = ?", f.AuthorID)
	}
	if f.CategorySlug != "" {
		q = q.Where("EXISTS (SELECT 1 FROM post_categories pc JOIN categories c ON c.id = pc.category_id "+
			"WHERE pc.post_id = p.id AND c.slug = ?)", f.CategorySlug)
	}
	if f.TagSlug != "" {
		q = q.Where("EXISTS (SELECT 1 FROM post_tags pt JOIN tags t ON t.id = pt.tag_id "+
			"WHERE pt.post_id = p.id AND t.slug = ?)", f.TagSlug)
	}
	if query := strings.TrimSpace(f.Query); query != "" {
		q = q.Where("p.title ILIKE ?", "%"+escapeLike(query)+"%")
	}
	if f.PublicOnly {
		q = q.OrderExpr("p.pinned DESC, p.published_at DESC NULLS LAST, p.id DESC")
	} else {
		q = q.OrderExpr("p.updated_at DESC, p.id DESC")
	}

	total, err := q.Limit(params.Limit()).Offset(params.Offset()).ScanAndCount(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("分页查询内容: %w", err)
	}
	if err := s.attach(ctx, posts); err != nil {
		return nil, 0, err
	}
	return posts, total, nil
}

// Get 按 ID 取内容（含关联）。
func (s *Store) Get(ctx context.Context, typ Type, id int64) (*Post, error) {
	p := new(Post)
	err := s.db.NewSelect().Model(p).Where("p.type = ? AND p.id = ?", typ, id).Scan(ctx)
	if err != nil {
		return nil, translate(fmt.Errorf("查询内容: %w", err))
	}
	return s.withRelations(ctx, p)
}

// GetBySlug 按 slug 取内容（含关联）。
func (s *Store) GetBySlug(ctx context.Context, typ Type, slug string) (*Post, error) {
	p := new(Post)
	err := s.db.NewSelect().Model(p).Where("p.type = ? AND p.slug = ?", typ, slug).Scan(ctx)
	if err != nil {
		return nil, translate(fmt.Errorf("查询内容: %w", err))
	}
	return s.withRelations(ctx, p)
}

// Create 创建内容、写入分类与标签关联，并记录首个修订；成功后回填 ID、时间戳与关联。
func (s *Store) Create(ctx context.Context, p *Post, categoryIDs, tagIDs []int64) error {
	now := time.Now()
	p.CreatedAt, p.UpdatedAt = now, now
	if p.Meta == nil {
		p.Meta = map[string]any{}
	}
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewInsert().Model(p).Returning("*").Exec(ctx); err != nil {
			return translate(fmt.Errorf("创建内容: %w", err))
		}
		if err := setTerms(ctx, tx, p, categoryIDs, tagIDs); err != nil {
			return err
		}
		return addRevision(ctx, tx, p, p.AuthorID)
	})
	if err != nil {
		return err
	}
	_, err = s.withRelations(ctx, p)
	return err
}

// Update 更新内容的可编辑字段与关联；snapshot 为真时追加修订并清理超出上限的旧修订。
func (s *Store) Update(ctx context.Context, p *Post, categoryIDs, tagIDs []int64, snapshot bool, editorID int64) error {
	p.UpdatedAt = time.Now()
	if p.Meta == nil {
		p.Meta = map[string]any{}
	}
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		res, err := tx.NewUpdate().Model(p).
			Column("title", "slug", "visibility", "raw_type", "raw", "content", "excerpt", "excerpt_auto",
				"cover_url", "pinned", "template", "meta", "updated_at").
			WherePK().
			Exec(ctx)
		if err != nil {
			return translate(fmt.Errorf("更新内容: %w", err))
		}
		if err := requireOneRow(res); err != nil {
			return err
		}
		if err := setTerms(ctx, tx, p, categoryIDs, tagIDs); err != nil {
			return err
		}
		if snapshot {
			return addRevision(ctx, tx, p, editorID)
		}
		return nil
	})
	if err != nil {
		return err
	}
	_, err = s.withRelations(ctx, p)
	return err
}

// UpdateStatus 只写入状态相关列：status、published_at、trashed_at 与 updated_at。
func (s *Store) UpdateStatus(ctx context.Context, p *Post) error {
	p.UpdatedAt = time.Now()
	res, err := s.db.NewUpdate().Model(p).
		Column("status", "published_at", "trashed_at", "updated_at").
		WherePK().
		Exec(ctx)
	if err != nil {
		return translate(fmt.Errorf("更新内容状态: %w", err))
	}
	return requireOneRow(res)
}

// DeletePermanently 彻底删除内容，关联与修订随外键级联消失。
//
// 删除条件里带上 status = 'trashed'，让「检查是否在回收站」与「删除」成为同一条语句：
// 先读后删之间存在窗口，用户在这期间点了「恢复」，刚恢复的内容就会被永久删除，
// 连同它的修订与评论一起不可逆地消失。条件不满足时返回 ErrNotTrashed。
func (s *Store) DeletePermanently(ctx context.Context, typ Type, id int64) error {
	res, err := s.db.NewDelete().Model((*Post)(nil)).
		Where("type = ? AND id = ? AND status = ?", typ, id, StatusTrashed).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("删除内容: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return nil //nolint:nilerr // 驱动不支持计数时视为成功
	}
	if affected == 0 {
		return ErrNotTrashed
	}
	return nil
}

// PublishDue 把计划时间已到的定时内容推进为已发布，返回被推进的那些。
//
// 单条幂等 UPDATE：多实例同时执行也不会重复发布或互相干扰；只有真正改到行的那个实例
// 拿得到 RETURNING 的结果，post.published 因此只发一次。
func (s *Store) PublishDue(ctx context.Context) ([]Post, error) {
	var out []Post
	err := s.db.NewUpdate().Model(&out).
		Set("status = ?", StatusPublished).
		Set("updated_at = now()").
		Where("status = ? AND published_at <= now()", StatusScheduled).
		Returning("id, type, title, slug, status, author_id, published_at").
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("推进定时发布: %w", err)
	}
	return out, nil
}

// ---------- 修订 ----------

// Revisions 返回内容的修订列表，新的在前。
func (s *Store) Revisions(ctx context.Context, postID int64) ([]RevisionSummary, error) {
	out := []RevisionSummary{}
	err := s.db.NewSelect().Model(&out).Where("rv.post_id = ?", postID).Order("rv.id DESC").Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("查询修订列表: %w", err)
	}
	return out, nil
}

// Revision 取指定修订的完整内容。
func (s *Store) Revision(ctx context.Context, postID, revisionID int64) (*Revision, error) {
	rev := new(Revision)
	err := s.db.NewSelect().Model(rev).Where("rv.post_id = ? AND rv.id = ?", postID, revisionID).Scan(ctx)
	if err != nil {
		return nil, translate(fmt.Errorf("查询修订: %w", err))
	}
	return rev, nil
}

// addRevision 记录当前内容为一次修订，并清理超出上限的旧修订。
func addRevision(ctx context.Context, tx bun.Tx, p *Post, authorID int64) error {
	rev := &Revision{
		PostID:    p.ID,
		Title:     p.Title,
		RawType:   p.RawType,
		Raw:       p.Raw,
		Content:   p.Content,
		Excerpt:   p.Excerpt,
		CreatedAt: time.Now(),
	}
	if authorID != 0 {
		rev.AuthorID = &authorID
	}
	if _, err := tx.NewInsert().Model(rev).Exec(ctx); err != nil {
		return fmt.Errorf("记录修订: %w", err)
	}

	// 保留最近 RevisionsKept 版；用子查询定位需要保留的 ID。
	keep := tx.NewSelect().Model((*Revision)(nil)).Column("rv.id").
		Where("rv.post_id = ?", p.ID).Order("rv.id DESC").Limit(RevisionsKept)
	if _, err := tx.NewDelete().Model((*Revision)(nil)).
		Where("post_id = ?", p.ID).
		Where("id NOT IN (?)", keep).
		Exec(ctx); err != nil {
		return fmt.Errorf("清理旧修订: %w", err)
	}
	return nil
}

// ---------- 关联 ----------

// setTerms 覆盖式写入分类与标签关联；页面不参与分类，传入的列表被忽略。
func setTerms(ctx context.Context, tx bun.Tx, p *Post, categoryIDs, tagIDs []int64) error {
	if p.Type != TypePost {
		return nil
	}
	categoryIDs, tagIDs = uniqueIDs(categoryIDs), uniqueIDs(tagIDs)

	if err := requireAllExist(ctx, tx, (*taxonomy.Category)(nil), "c.id", categoryIDs); err != nil {
		return err
	}
	if err := requireAllExist(ctx, tx, (*taxonomy.Tag)(nil), "t.id", tagIDs); err != nil {
		return err
	}

	if _, err := tx.NewDelete().Model((*PostCategory)(nil)).Where("post_id = ?", p.ID).Exec(ctx); err != nil {
		return fmt.Errorf("清除分类关联: %w", err)
	}
	if _, err := tx.NewDelete().Model((*PostTag)(nil)).Where("post_id = ?", p.ID).Exec(ctx); err != nil {
		return fmt.Errorf("清除标签关联: %w", err)
	}
	if len(categoryIDs) > 0 {
		links := make([]PostCategory, 0, len(categoryIDs))
		for _, id := range categoryIDs {
			links = append(links, PostCategory{PostID: p.ID, CategoryID: id})
		}
		if _, err := tx.NewInsert().Model(&links).Exec(ctx); err != nil {
			return translate(fmt.Errorf("写入分类关联: %w", err))
		}
	}
	if len(tagIDs) > 0 {
		links := make([]PostTag, 0, len(tagIDs))
		for _, id := range tagIDs {
			links = append(links, PostTag{PostID: p.ID, TagID: id})
		}
		if _, err := tx.NewInsert().Model(&links).Exec(ctx); err != nil {
			return translate(fmt.Errorf("写入标签关联: %w", err))
		}
	}
	return nil
}

// requireAllExist 校验给定 ID 全部存在，否则返回 ErrTermNotFound。
func requireAllExist(ctx context.Context, tx bun.Tx, model any, column string, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	count, err := tx.NewSelect().Model(model).Where(column+" IN (?)", bun.List(ids)).Count(ctx)
	if err != nil {
		return fmt.Errorf("校验分类或标签: %w", err)
	}
	if count != len(ids) {
		return ErrTermNotFound
	}
	return nil
}

// withRelations 给单条内容附上关联并返回它自身。
func (s *Store) withRelations(ctx context.Context, p *Post) (*Post, error) {
	batch := []Post{*p}
	if err := s.attach(ctx, batch); err != nil {
		return nil, err
	}
	*p = batch[0]
	return p, nil
}

// attach 批量加载作者、分类与标签，避免列表接口的 N+1 查询。
func (s *Store) attach(ctx context.Context, posts []Post) error {
	if len(posts) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(posts))
	authorIDs := make([]int64, 0, len(posts))
	for i := range posts {
		ids = append(ids, posts[i].ID)
		authorIDs = append(authorIDs, posts[i].AuthorID)
		posts[i].Categories = []taxonomy.Category{}
		posts[i].Tags = []taxonomy.Tag{}
		if posts[i].Meta == nil {
			posts[i].Meta = map[string]any{}
		}
	}

	authors, err := s.users.FindUsersByIDs(ctx, uniqueIDs(authorIDs))
	if err != nil {
		return err
	}
	for i := range posts {
		if u, ok := authors[posts[i].AuthorID]; ok {
			posts[i].Author = &AuthorView{ID: u.ID, Username: u.Username, DisplayName: u.Name(), AvatarURL: u.AvatarURL}
		}
	}

	var categoryLinks []PostCategory
	if err := s.db.NewSelect().Model(&categoryLinks).Where("pc.post_id IN (?)", bun.List(ids)).Scan(ctx); err != nil {
		return fmt.Errorf("查询分类关联: %w", err)
	}
	var tagLinks []PostTag
	if err := s.db.NewSelect().Model(&tagLinks).Where("pt.post_id IN (?)", bun.List(ids)).Scan(ctx); err != nil {
		return fmt.Errorf("查询标签关联: %w", err)
	}

	categories := map[int64]taxonomy.Category{}
	if len(categoryLinks) > 0 {
		catIDs := make([]int64, 0, len(categoryLinks))
		for _, l := range categoryLinks {
			catIDs = append(catIDs, l.CategoryID)
		}
		var rows []taxonomy.Category
		if err := s.db.NewSelect().Model(&rows).Where("c.id IN (?)", bun.List(uniqueIDs(catIDs))).
			Order("c.name", "c.id").Scan(ctx); err != nil {
			return fmt.Errorf("查询分类: %w", err)
		}
		for i := range rows {
			categories[rows[i].ID] = rows[i]
		}
	}
	tags := map[int64]taxonomy.Tag{}
	if len(tagLinks) > 0 {
		tagIDs := make([]int64, 0, len(tagLinks))
		for _, l := range tagLinks {
			tagIDs = append(tagIDs, l.TagID)
		}
		var rows []taxonomy.Tag
		if err := s.db.NewSelect().Model(&rows).Where("t.id IN (?)", bun.List(uniqueIDs(tagIDs))).
			Order("t.name", "t.id").Scan(ctx); err != nil {
			return fmt.Errorf("查询标签: %w", err)
		}
		for i := range rows {
			tags[rows[i].ID] = rows[i]
		}
	}

	index := make(map[int64]int, len(posts))
	for i := range posts {
		index[posts[i].ID] = i
	}
	for _, l := range categoryLinks {
		if c, ok := categories[l.CategoryID]; ok {
			i := index[l.PostID]
			posts[i].Categories = append(posts[i].Categories, c)
		}
	}
	for _, l := range tagLinks {
		if t, ok := tags[l.TagID]; ok {
			i := index[l.PostID]
			posts[i].Tags = append(posts[i].Tags, t)
		}
	}
	return nil
}

// ---------- 工具 ----------

// uniqueIDs 去重并保持顺序，忽略非正数。
func uniqueIDs(ids []int64) []int64 {
	seen := make(map[int64]bool, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// translate 把驱动错误翻译成本包的哨兵错误；无法识别的错误原样返回。
func translate(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	v, ok := database.AsConstraintViolation(err)
	if !ok {
		return err
	}
	switch v.Code {
	case database.CodeUniqueViolation:
		if v.Constraint == "posts_slug_key" {
			return ErrSlugTaken
		}
		return fmt.Errorf("%w：唯一约束 %s 冲突", ErrInvalid, v.Constraint)
	case database.CodeForeignKeyViolation:
		return ErrTermNotFound
	case database.CodeCheckViolation:
		return fmt.Errorf("%w：违反约束 %s", ErrInvalid, v.Constraint)
	}
	return err
}

// requireOneRow 校验写操作确实命中了一行，否则视为对象不存在。
func requireOneRow(res sql.Result) error {
	affected, err := res.RowsAffected()
	if err != nil {
		return nil //nolint:nilerr // 驱动不支持计数时视为成功
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// escapeLike 转义 LIKE 模式中的通配符，让用户输入按字面匹配。
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
