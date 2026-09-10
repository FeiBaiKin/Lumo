package media

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
)

// 错误哨兵。处理器据此映射为 404 / 409。
var (
	// ErrNotFound 表示附件不存在。
	ErrNotFound = errors.New("附件不存在")
	// ErrKeyTaken 表示对象键已被占用。
	ErrKeyTaken = errors.New("对象键已被占用")
	// ErrInvalid 表示字段违反了数据库约束。
	ErrInvalid = errors.New("字段不合法")
)

// Store 提供附件的持久化操作。
type Store struct {
	db    *bun.DB
	users *auth.Store
}

// NewStore 构造 Store。
func NewStore(db *bun.DB, users *auth.Store) *Store {
	return &Store{db: db, users: users}
}

// Filter 是附件列表的筛选条件。
type Filter struct {
	// Kind 非空时只返回该分类的附件。
	Kind Kind
	// UploaderID 非零时只返回该用户上传的附件。
	UploaderID int64
	// Q 非空时按原始文件名模糊筛选。
	Q string
}

// Page 分页返回附件，按上传时间倒序。
func (s *Store) Page(ctx context.Context, filter Filter, params api.PageParams) ([]Media, int, error) {
	out := []Media{}
	q := s.db.NewSelect().Model(&out).Order("m.created_at DESC", "m.id DESC")
	if filter.Kind != "" {
		q = q.Where("m.kind = ?", string(filter.Kind))
	}
	if filter.UploaderID != 0 {
		q = q.Where("m.uploader_id = ?", filter.UploaderID)
	}
	if text := strings.TrimSpace(filter.Q); text != "" {
		q = q.Where("m.original_name ILIKE ?", "%"+escapeLike(text)+"%")
	}

	total, err := q.Limit(params.Limit()).Offset(params.Offset()).ScanAndCount(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("分页查询附件: %w", err)
	}
	if err := s.attach(ctx, out); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// Get 按 ID 查附件。
func (s *Store) Get(ctx context.Context, id int64) (*Media, error) {
	m := new(Media)
	if err := s.db.NewSelect().Model(m).Where("m.id = ?", id).Scan(ctx); err != nil {
		return nil, translate(fmt.Errorf("查询附件: %w", err))
	}
	// attach 作用在切片元素上，故先装入切片再取回。
	items := []Media{*m}
	if err := s.attach(ctx, items); err != nil {
		return nil, err
	}
	return &items[0], nil
}

// Create 写入附件记录；成功后回填 ID 与时间戳。
func (s *Store) Create(ctx context.Context, m *Media) error {
	now := time.Now()
	m.CreatedAt, m.UpdatedAt = now, now
	if m.Thumbnails == nil {
		m.Thumbnails = []Thumbnail{}
	}
	if _, err := s.db.NewInsert().Model(m).Returning("*").Exec(ctx); err != nil {
		return translate(fmt.Errorf("创建附件: %w", err))
	}
	return nil
}

// UpdateMeta 更新附件的展示信息（alt 与标题），不动文件本身。
func (s *Store) UpdateMeta(ctx context.Context, m *Media) error {
	m.UpdatedAt = time.Now()
	res, err := s.db.NewUpdate().Model(m).
		Column("alt", "title", "updated_at").
		WherePK().
		Exec(ctx)
	if err != nil {
		return translate(fmt.Errorf("更新附件: %w", err))
	}
	return requireOneRow(res)
}

// Delete 删除附件记录。文件本身由服务层在记录删除成功后清理。
func (s *Store) Delete(ctx context.Context, id int64) error {
	res, err := s.db.NewDelete().Model((*Media)(nil)).Where("id = ?", id).Exec(ctx)
	if err != nil {
		return fmt.Errorf("删除附件: %w", err)
	}
	return requireOneRow(res)
}

// attach 批量填充上传者，避免列表接口的 N+1 查询。
func (s *Store) attach(ctx context.Context, items []Media) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(items))
	for i := range items {
		ids = append(ids, items[i].UploaderID)
		if items[i].Thumbnails == nil {
			items[i].Thumbnails = []Thumbnail{}
		}
	}
	if s.users == nil {
		return nil
	}

	uploaders, err := s.users.FindUsersByIDs(ctx, uniqueIDs(ids))
	if err != nil {
		return err
	}
	for i := range items {
		if u, ok := uploaders[items[i].UploaderID]; ok {
			items[i].Uploader = &UploaderView{
				ID: u.ID, Username: u.Username, DisplayName: u.Name(), AvatarURL: u.AvatarURL,
			}
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
	case database.CodeUniqueViolation:
		if v.Constraint == "media_storage_key_key" {
			return ErrKeyTaken
		}
		return fmt.Errorf("%w：唯一约束 %s 冲突", ErrInvalid, v.Constraint)
	case database.CodeCheckViolation:
		return fmt.Errorf("%w：违反约束 %s", ErrInvalid, v.Constraint)
	case database.CodeForeignKeyViolation:
		return fmt.Errorf("%w：外键 %s 不满足", ErrInvalid, v.Constraint)
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
	return strings.NewReplacer(`\`, `\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
