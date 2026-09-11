package extension

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/uptrace/bun"

	"github.com/FeiBaiKin/lumo/internal/api"
	"github.com/FeiBaiKin/lumo/internal/database"
)

// 错误哨兵。处理器据此映射为 404 / 409 / 422。
var (
	// ErrNotFound 表示记录不存在。
	ErrNotFound = errors.New("扩展记录不存在")
	// ErrNameTaken 表示同一资源下已有同名记录。
	ErrNameTaken = errors.New("同名扩展记录已存在")
	// ErrKindConflict 表示该资源段已被另一种拼法的 kind 占用。
	ErrKindConflict = errors.New("资源段已被另一个 kind 占用")
	// ErrInvalid 表示字段形态不合法或违反了数据库约束。
	ErrInvalid = errors.New("字段不合法")
)

// Ref 是一条记录的寻址四元组，资源段为 URL 里的复数形式。
type Ref struct {
	Group    string
	Version  string
	Resource string
	Name     string
}

// ListParams 是列表查询条件。
type ListParams struct {
	Group    string
	Version  string
	Resource string
	// Match 是对 spec 的包含匹配（jsonb @>），走核心迁移建好的 GIN 索引。
	Match map[string]any
	// Sort 是排序方式，取值见 sortExpr。
	Sort string
	Page api.PageParams
}

// Store 提供扩展记录的持久化操作。
type Store struct {
	db *bun.DB
}

// NewStore 构造 Store。
func NewStore(db *bun.DB) *Store { return &Store{db: db} }

// sortExpr 把排序名翻译成 ORDER BY 表达式；未知值按名称升序。
//
// 一律追加 id 作次序键：名称唯一而创建时间不唯一，缺了它同一毫秒写入的多条记录
// 在翻页时可能重复出现或漏掉。
func sortExpr(sort string) string {
	switch sort {
	case "-name":
		return "ext.name DESC, ext.id DESC"
	case "createdAt":
		return "ext.created_at ASC, ext.id ASC"
	case "-createdAt":
		return "ext.created_at DESC, ext.id DESC"
	default:
		return "ext.name ASC, ext.id ASC"
	}
}

// List 分页返回某个资源下的记录，并给出总数。
func (s *Store) List(ctx context.Context, params *ListParams) ([]Extension, int, error) {
	out := []Extension{}
	query := s.db.NewSelect().Model(&out).
		Where("ext.api_group = ? AND ext.version = ? AND ext.resource = ?",
			params.Group, params.Version, params.Resource).
		OrderExpr(sortExpr(params.Sort))
	if len(params.Match) > 0 {
		encoded, err := json.Marshal(params.Match)
		if err != nil {
			return nil, 0, fmt.Errorf("%w：筛选条件无法序列化", ErrInvalid)
		}
		query = query.Where("ext.spec @> ?::jsonb", string(encoded))
	}

	total, err := query.Limit(params.Page.Limit()).Offset(params.Page.Offset()).ScanAndCount(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("分页查询扩展记录: %w", err)
	}
	for i := range out {
		out[i].fill()
	}
	return out, total, nil
}

// Get 按地址取一条记录。
func (s *Store) Get(ctx context.Context, ref Ref) (*Extension, error) {
	e := new(Extension)
	err := s.db.NewSelect().Model(e).
		Where("ext.api_group = ? AND ext.version = ? AND ext.resource = ? AND ext.name = ?",
			ref.Group, ref.Version, ref.Resource, ref.Name).
		Scan(ctx)
	if err != nil {
		return nil, translate(fmt.Errorf("查询扩展记录: %w", err))
	}
	e.fill()
	return e, nil
}

// KindOf 返回某个资源段上已登记的 kind；该资源段还没有记录时返回空串。
//
// 用于挡住「同一地址两种 kind」：Replicaset 与 ReplicaSet 都映射到 replicasets，
// 先写入的那个即为该资源段的正规拼法。
func (s *Store) KindOf(ctx context.Context, ref Ref) (string, error) {
	var kind string
	err := s.db.NewSelect().Model((*Extension)(nil)).
		Column("kind").
		Where("ext.api_group = ? AND ext.version = ? AND ext.resource = ?",
			ref.Group, ref.Version, ref.Resource).
		Limit(1).
		Scan(ctx, &kind)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("查询资源段的 kind: %w", err)
	}
	return kind, nil
}

// Create 写入一条记录，resource 由 kind 推导。
func (s *Store) Create(ctx context.Context, e *Extension) error {
	e.Resource = Resource(e.Kind)
	if e.Spec == nil {
		e.Spec = map[string]any{}
	}
	now := time.Now().UTC()
	e.CreatedAt, e.UpdatedAt = now, now

	if _, err := s.db.NewInsert().Model(e).Exec(ctx); err != nil {
		return translate(fmt.Errorf("写入扩展记录: %w", err))
	}
	e.fill()
	return nil
}

// Update 整体替换一条记录的 spec；地址与 kind 不可改。
func (s *Store) Update(ctx context.Context, e *Extension) error {
	if e.Spec == nil {
		e.Spec = map[string]any{}
	}
	e.UpdatedAt = time.Now().UTC()

	res, err := s.db.NewUpdate().Model(e).
		Column("spec", "updated_at").
		WherePK().
		Exec(ctx)
	if err != nil {
		return translate(fmt.Errorf("更新扩展记录: %w", err))
	}
	if err := requireOneRow(res); err != nil {
		return err
	}
	e.fill()
	return nil
}

// Delete 按地址删除一条记录。
func (s *Store) Delete(ctx context.Context, ref Ref) error {
	res, err := s.db.NewDelete().Model((*Extension)(nil)).
		Where("ext.api_group = ? AND ext.version = ? AND ext.resource = ? AND ext.name = ?",
			ref.Group, ref.Version, ref.Resource, ref.Name).
		Exec(ctx)
	if err != nil {
		return translate(fmt.Errorf("删除扩展记录: %w", err))
	}
	return requireOneRow(res)
}

// translate 把数据库错误翻译成本包的哨兵错误。
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
		return ErrNameTaken
	case database.CodeCheckViolation:
		return fmt.Errorf("%w：违反约束 %s", ErrInvalid, v.Constraint)
	}
	return err
}

// requireOneRow 校验写操作确实命中了一行，否则视为记录不存在。
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
