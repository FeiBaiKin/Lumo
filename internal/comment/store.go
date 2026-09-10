package comment

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/FeiBaiKin/lumo/internal/api"
	"github.com/FeiBaiKin/lumo/internal/database"
)

// 错误哨兵。处理器据此映射为 400 / 404。
var (
	// ErrNotFound 表示评论不存在。
	ErrNotFound = errors.New("评论不存在")
	// ErrPostNotFound 表示评论的目标内容不存在。
	ErrPostNotFound = errors.New("评论的内容不存在")
	// ErrInvalid 表示字段违反了数据库约束。
	ErrInvalid = errors.New("字段不合法")
)

// Store 提供评论的持久化操作。
//
// 直接用 bun 读 posts 表而不经 content 模块：两者共用同一张表，
// 而模块之间只允许经 App 交互，不值得为一次标题查询引入跨模块依赖。
type Store struct {
	db *bun.DB
}

// NewStore 构造 Store。
func NewStore(db *bun.DB) *Store { return &Store{db: db} }

// Filter 是后台列表的筛选条件。
type Filter struct {
	Status Status
	PostID int64
	// AuthorID 非零时只返回该作者名下内容的评论，用于不带 _any 权限的调用者。
	AuthorID int64
	Q        string
}

// Page 分页返回评论，可按状态、内容与作者筛选。
func (s *Store) Page(ctx context.Context, filter Filter, params api.PageParams) ([]Comment, int, error) {
	out := []Comment{}
	q := s.db.NewSelect().Model(&out).
		Join("JOIN posts AS p ON p.id = cm.post_id").
		Order("cm.created_at DESC", "cm.id DESC")

	if filter.Status != "" {
		q = q.Where("cm.status = ?", string(filter.Status))
	}
	if filter.PostID != 0 {
		q = q.Where("cm.post_id = ?", filter.PostID)
	}
	if filter.AuthorID != 0 {
		q = q.Where("p.author_id = ?", filter.AuthorID)
	}
	if text := strings.TrimSpace(filter.Q); text != "" {
		pattern := "%" + escapeLike(text) + "%"
		q = q.Where("(cm.content ILIKE ? OR cm.author_name ILIKE ?)", pattern, pattern)
	}

	total, err := q.Limit(params.Limit()).Offset(params.Offset()).ScanAndCount(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("分页查询评论: %w", err)
	}
	if err := s.attachPosts(ctx, out); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// ListApproved 返回某篇内容下已通过的**全部**评论，按时间正序。
//
// 不分页：回复树一旦被切开就没法正确组装，而单篇内容的评论量本就有上限。
func (s *Store) ListApproved(ctx context.Context, postID int64) ([]Comment, error) {
	out := []Comment{}
	if err := s.db.NewSelect().Model(&out).
		Where("cm.post_id = ?", postID).
		Where("cm.status = ?", string(StatusApproved)).
		Order("cm.created_at", "cm.id").
		Scan(ctx); err != nil {
		return nil, fmt.Errorf("查询评论: %w", err)
	}
	return out, nil
}

// CountApproved 统计某篇内容已通过的评论数。
func (s *Store) CountApproved(ctx context.Context, postID int64) (int, error) {
	n, err := s.db.NewSelect().Model((*Comment)(nil)).
		Where("cm.post_id = ?", postID).
		Where("cm.status = ?", string(StatusApproved)).
		Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("统计评论数: %w", err)
	}
	return n, nil
}

// Get 按 ID 查评论。
func (s *Store) Get(ctx context.Context, id int64) (*Comment, error) {
	c := new(Comment)
	if err := s.db.NewSelect().Model(c).Where("cm.id = ?", id).Scan(ctx); err != nil {
		return nil, translate(fmt.Errorf("查询评论: %w", err))
	}
	items := []Comment{*c}
	if err := s.attachPosts(ctx, items); err != nil {
		return nil, err
	}
	return &items[0], nil
}

// LastByIP 返回某 IP 最近一条评论的时间，供频率限制使用。
//
// 只算「已存在」的评论：把待审的也算上，同一 IP 反复提交就会被自己挡住，
// 而正常访客并不知道自己的评论在待审。
func (s *Store) LastByIP(ctx context.Context, ip string) (time.Time, error) {
	var at time.Time
	err := s.db.NewSelect().Model((*Comment)(nil)).
		Column("cm.created_at").
		Where("cm.ip = ?", ip).
		Order("cm.created_at DESC").
		Limit(1).
		Scan(ctx, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("查询最近评论: %w", err)
	}
	return at, nil
}

// PostRef 按 ID 查内容摘要，同时给出作者 ID 供所有权判定。
func (s *Store) PostRef(ctx context.Context, id int64) (*PostRef, error) {
	ref := new(PostRef)
	err := s.db.NewRaw(
		"SELECT id, type, title, slug, author_id FROM posts WHERE id = ?", id).Scan(ctx, ref)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrPostNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("查询评论目标: %w", err)
	}
	return ref, nil
}

// Create 写入评论；成功后回填 ID 与时间戳。
func (s *Store) Create(ctx context.Context, c *Comment) error {
	now := time.Now()
	c.CreatedAt, c.UpdatedAt = now, now
	if _, err := s.db.NewInsert().Model(c).Returning("*").Exec(ctx); err != nil {
		return translate(fmt.Errorf("创建评论: %w", err))
	}
	return nil
}

// UpdateStatus 修改审核状态。
func (s *Store) UpdateStatus(ctx context.Context, id int64, status Status) error {
	res, err := s.db.NewUpdate().Model((*Comment)(nil)).
		Set("status = ?", string(status)).
		Set("updated_at = now()").
		Where("id = ?", id).
		Exec(ctx)
	if err != nil {
		return translate(fmt.Errorf("更新评论状态: %w", err))
	}
	return requireOneRow(res)
}

// Delete 删除评论；其回复由外键级联删除。
func (s *Store) Delete(ctx context.Context, id int64) error {
	res, err := s.db.NewDelete().Model((*Comment)(nil)).Where("id = ?", id).Exec(ctx)
	if err != nil {
		return fmt.Errorf("删除评论: %w", err)
	}
	return requireOneRow(res)
}

// CountPending 统计待审评论数，供 Console 侧显示待办角标。
func (s *Store) CountPending(ctx context.Context, authorID int64) (int, error) {
	q := s.db.NewSelect().Model((*Comment)(nil)).
		Where("cm.status = ?", string(StatusPending))
	if authorID != 0 {
		q = q.Join("JOIN posts AS p ON p.id = cm.post_id").Where("p.author_id = ?", authorID)
	}
	n, err := q.Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("统计待审评论: %w", err)
	}
	return n, nil
}

// attachPosts 批量填充评论所属的内容摘要，避免列表接口的 N+1 查询。
func (s *Store) attachPosts(ctx context.Context, items []Comment) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(items))
	for i := range items {
		ids = append(ids, items[i].PostID)
	}

	var refs []PostRef
	if err := s.db.NewRaw(
		"SELECT id, type, title, slug, author_id FROM posts WHERE id IN (?)", bun.List(uniqueIDs(ids)),
	).Scan(ctx, &refs); err != nil {
		return fmt.Errorf("查询评论目标: %w", err)
	}
	byID := make(map[int64]PostRef, len(refs))
	for _, ref := range refs {
		byID[ref.ID] = ref
	}
	for i := range items {
		if ref, ok := byID[items[i].PostID]; ok {
			ref := ref
			items[i].Post = &ref
		}
	}
	return nil
}

// uniqueIDs 去重，保持首次出现的顺序。
func uniqueIDs(ids []int64) []int64 {
	seen := make(map[int64]bool, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id == 0 || seen[id] {
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
	case database.CodeForeignKeyViolation:
		return ErrPostNotFound
	case database.CodeCheckViolation, database.CodeUniqueViolation:
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
