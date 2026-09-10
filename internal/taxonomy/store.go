package taxonomy

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

// 错误哨兵。处理器据此映射为 400 / 404 / 409。
var (
	// ErrNotFound 表示分类或标签不存在。
	ErrNotFound = errors.New("对象不存在")
	// ErrSlugTaken 表示 slug 已被占用。
	ErrSlugTaken = errors.New("slug 已被占用")
	// ErrNameTaken 表示名称已被占用（分类为同级、标签为全局）。
	ErrNameTaken = errors.New("名称已被占用")
	// ErrParentNotFound 表示父分类不存在。
	ErrParentNotFound = errors.New("父分类不存在")
	// ErrCycle 表示移动会让分类树成环。
	ErrCycle = errors.New("父分类不能是自身或其后代")
	// ErrInvalid 表示字段违反了数据库约束。
	ErrInvalid = errors.New("字段不合法")
)

// Store 提供分类与标签的持久化操作。
type Store struct {
	db *bun.DB
}

// NewStore 构造 Store。
func NewStore(db *bun.DB) *Store {
	return &Store{db: db}
}

// categoryOrder 是分类的固定排序：根在前，同级按 position、name、id。
const categoryOrder = "c.parent_id NULLS FIRST, c.position, c.name, c.id"

// ---------- 分类 ----------

// ListCategories 返回全部分类。
func (s *Store) ListCategories(ctx context.Context) ([]Category, error) {
	out := []Category{}
	if err := s.db.NewSelect().Model(&out).OrderExpr(categoryOrder).Scan(ctx); err != nil {
		return nil, fmt.Errorf("查询分类列表: %w", err)
	}
	return out, nil
}

// PageCategories 分页返回分类，并给出总数。
func (s *Store) PageCategories(ctx context.Context, params api.PageParams) ([]Category, int, error) {
	out := []Category{}
	total, err := s.db.NewSelect().Model(&out).
		OrderExpr(categoryOrder).
		Limit(params.Limit()).
		Offset(params.Offset()).
		ScanAndCount(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("分页查询分类: %w", err)
	}
	return out, total, nil
}

// GetCategory 按 ID 查分类。
func (s *Store) GetCategory(ctx context.Context, id int64) (*Category, error) {
	c := new(Category)
	if err := s.db.NewSelect().Model(c).Where("c.id = ?", id).Scan(ctx); err != nil {
		return nil, translate(fmt.Errorf("查询分类: %w", err))
	}
	return c, nil
}

// GetCategoryBySlug 按 slug 查分类。
func (s *Store) GetCategoryBySlug(ctx context.Context, slug string) (*Category, error) {
	c := new(Category)
	if err := s.db.NewSelect().Model(c).Where("c.slug = ?", slug).Scan(ctx); err != nil {
		return nil, translate(fmt.Errorf("查询分类: %w", err))
	}
	return c, nil
}

// NextPosition 返回给定父分类下新分类应取的排序值（末尾）。
func (s *Store) NextPosition(ctx context.Context, parentID *int64) (int, error) {
	q := s.db.NewSelect().Model((*Category)(nil)).ColumnExpr("COALESCE(MAX(c.position), -1) + 1")
	if parentID == nil {
		q = q.Where("c.parent_id IS NULL")
	} else {
		q = q.Where("c.parent_id = ?", *parentID)
	}
	var next int
	if err := q.Scan(ctx, &next); err != nil {
		return 0, fmt.Errorf("计算分类排序: %w", err)
	}
	return next, nil
}

// CreateCategory 创建分类；成功后回填 ID 与时间戳。
func (s *Store) CreateCategory(ctx context.Context, c *Category) error {
	now := time.Now()
	c.CreatedAt, c.UpdatedAt = now, now
	if _, err := s.db.NewInsert().Model(c).Returning("*").Exec(ctx); err != nil {
		return translate(fmt.Errorf("创建分类: %w", err))
	}
	return nil
}

// UpdateCategory 更新分类的可编辑字段，并在事务内校验父分类存在且不成环。
func (s *Store) UpdateCategory(ctx context.Context, c *Category) error {
	return s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if c.ParentID != nil {
			var all []Category
			if err := tx.NewSelect().Model(&all).Scan(ctx); err != nil {
				return fmt.Errorf("加载分类树: %w", err)
			}
			byID := indexByID(all)
			if _, ok := byID[*c.ParentID]; !ok {
				return ErrParentNotFound
			}
			if wouldCycle(byID, c.ID, *c.ParentID) {
				return ErrCycle
			}
		}

		c.UpdatedAt = time.Now()
		res, err := tx.NewUpdate().Model(c).
			Column("parent_id", "name", "slug", "description", "cover_url", "position", "updated_at").
			WherePK().
			Exec(ctx)
		if err != nil {
			return translate(fmt.Errorf("更新分类: %w", err))
		}
		return requireOneRow(res)
	})
}

// DeleteCategory 删除分类，并把它的子分类挂到它的父分类下。
//
// 不做级联删除：误删一个顶层分类不该连带抹掉整棵子树。
func (s *Store) DeleteCategory(ctx context.Context, id int64) error {
	return s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		target := new(Category)
		if err := tx.NewSelect().Model(target).Where("c.id = ?", id).For("UPDATE").Scan(ctx); err != nil {
			return translate(fmt.Errorf("查询分类: %w", err))
		}

		if _, err := tx.NewUpdate().Model((*Category)(nil)).
			Set("parent_id = ?", target.ParentID).
			Set("updated_at = now()").
			Where("parent_id = ?", id).
			Exec(ctx); err != nil {
			return fmt.Errorf("迁移子分类: %w", err)
		}

		if _, err := tx.NewDelete().Model((*Category)(nil)).Where("id = ?", id).Exec(ctx); err != nil {
			return fmt.Errorf("删除分类: %w", err)
		}
		return nil
	})
}

// ---------- 标签 ----------

// ListTags 返回全部标签，按名称排序。
func (s *Store) ListTags(ctx context.Context) ([]Tag, error) {
	out := []Tag{}
	if err := s.db.NewSelect().Model(&out).Order("t.name", "t.id").Scan(ctx); err != nil {
		return nil, fmt.Errorf("查询标签列表: %w", err)
	}
	return out, nil
}

// PageTags 分页返回标签；q 非空时按名称或 slug 模糊筛选。
func (s *Store) PageTags(ctx context.Context, q string, params api.PageParams) ([]Tag, int, error) {
	out := []Tag{}
	query := s.db.NewSelect().Model(&out).Order("t.name", "t.id")
	if q = strings.TrimSpace(q); q != "" {
		pattern := "%" + escapeLike(q) + "%"
		query = query.Where("(t.name ILIKE ? OR t.slug ILIKE ?)", pattern, pattern)
	}
	total, err := query.Limit(params.Limit()).Offset(params.Offset()).ScanAndCount(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("分页查询标签: %w", err)
	}
	return out, total, nil
}

// GetTag 按 ID 查标签。
func (s *Store) GetTag(ctx context.Context, id int64) (*Tag, error) {
	tag := new(Tag)
	if err := s.db.NewSelect().Model(tag).Where("t.id = ?", id).Scan(ctx); err != nil {
		return nil, translate(fmt.Errorf("查询标签: %w", err))
	}
	return tag, nil
}

// GetTagBySlug 按 slug 查标签。
func (s *Store) GetTagBySlug(ctx context.Context, slug string) (*Tag, error) {
	tag := new(Tag)
	if err := s.db.NewSelect().Model(tag).Where("t.slug = ?", slug).Scan(ctx); err != nil {
		return nil, translate(fmt.Errorf("查询标签: %w", err))
	}
	return tag, nil
}

// CreateTag 创建标签；成功后回填 ID 与时间戳。
func (s *Store) CreateTag(ctx context.Context, tag *Tag) error {
	now := time.Now()
	tag.CreatedAt, tag.UpdatedAt = now, now
	if _, err := s.db.NewInsert().Model(tag).Returning("*").Exec(ctx); err != nil {
		return translate(fmt.Errorf("创建标签: %w", err))
	}
	return nil
}

// UpdateTag 更新标签的可编辑字段。
func (s *Store) UpdateTag(ctx context.Context, tag *Tag) error {
	tag.UpdatedAt = time.Now()
	res, err := s.db.NewUpdate().Model(tag).
		Column("name", "slug", "description", "color", "updated_at").
		WherePK().
		Exec(ctx)
	if err != nil {
		return translate(fmt.Errorf("更新标签: %w", err))
	}
	return requireOneRow(res)
}

// DeleteTag 删除标签。
func (s *Store) DeleteTag(ctx context.Context, id int64) error {
	res, err := s.db.NewDelete().Model((*Tag)(nil)).Where("id = ?", id).Exec(ctx)
	if err != nil {
		return fmt.Errorf("删除标签: %w", err)
	}
	return requireOneRow(res)
}

// ---------- 工具 ----------

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
		switch v.Constraint {
		case "categories_slug_key", "tags_slug_key":
			return ErrSlugTaken
		case "categories_sibling_name_key", "tags_name_key":
			return ErrNameTaken
		}
		return fmt.Errorf("%w：唯一约束 %s 冲突", ErrInvalid, v.Constraint)
	case database.CodeForeignKeyViolation:
		return ErrParentNotFound
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
