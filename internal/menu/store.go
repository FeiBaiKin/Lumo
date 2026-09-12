package menu

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/uptrace/bun"

	"github.com/FeiBaiKin/lumo/internal/database"
)

// 错误哨兵。处理器据此映射为 400 / 404 / 409。
var (
	// ErrNotFound 表示菜单不存在。
	ErrNotFound = errors.New("菜单不存在")
	// ErrSlugTaken 表示菜单 slug 已被占用。
	ErrSlugTaken = errors.New("菜单标识已被占用")
	// ErrInvalid 表示字段违反了数据库约束。
	ErrInvalid = errors.New("字段不合法")
	// ErrCycle 表示条目树的父子关系成环。
	ErrCycle = errors.New("父条目不能是自身或其后代")
	// ErrTargetNotFound 表示条目指向的站内记录不存在。
	ErrTargetNotFound = errors.New("条目指向的内容不存在")
)

// Store 提供菜单与条目的持久化操作。
type Store struct {
	db *bun.DB
}

// NewStore 构造 Store。
func NewStore(db *bun.DB) *Store { return &Store{db: db} }

// List 返回全部菜单，按名称排序，并带上条目数。
func (s *Store) List(ctx context.Context) ([]Menu, error) {
	out := []Menu{}
	if err := s.db.NewSelect().Model(&out).Order("mn.name", "mn.id").Scan(ctx); err != nil {
		return nil, fmt.Errorf("查询菜单列表: %w", err)
	}
	if len(out) == 0 {
		return out, nil
	}

	ids := make([]int64, 0, len(out))
	for i := range out {
		ids = append(ids, out[i].ID)
	}
	var counts []struct {
		MenuID int64 `bun:"menu_id"`
		Total  int   `bun:"total"`
	}
	if err := s.db.NewRaw(
		"SELECT menu_id, count(*) AS total FROM menu_items WHERE menu_id IN (?) GROUP BY menu_id",
		bun.List(ids)).Scan(ctx, &counts); err != nil {
		return nil, fmt.Errorf("统计菜单条目: %w", err)
	}
	byID := make(map[int64]int, len(counts))
	for _, c := range counts {
		byID[c.MenuID] = c.Total
	}
	for i := range out {
		out[i].ItemCount = byID[out[i].ID]
	}
	return out, nil
}

// Get 按 ID 查菜单。
func (s *Store) Get(ctx context.Context, id int64) (*Menu, error) {
	m := new(Menu)
	if err := s.db.NewSelect().Model(m).Where("mn.id = ?", id).Scan(ctx); err != nil {
		return nil, translate(fmt.Errorf("查询菜单: %w", err))
	}
	return m, nil
}

// GetBySlug 按 slug 查菜单。
func (s *Store) GetBySlug(ctx context.Context, slug string) (*Menu, error) {
	m := new(Menu)
	if err := s.db.NewSelect().Model(m).Where("mn.slug = ?", slug).Scan(ctx); err != nil {
		return nil, translate(fmt.Errorf("查询菜单: %w", err))
	}
	return m, nil
}

// Create 创建菜单；成功后回填 ID 与时间戳。
func (s *Store) Create(ctx context.Context, m *Menu) error {
	now := time.Now()
	m.CreatedAt, m.UpdatedAt = now, now
	if _, err := s.db.NewInsert().Model(m).Returning("*").Exec(ctx); err != nil {
		return translate(fmt.Errorf("创建菜单: %w", err))
	}
	return nil
}

// Update 更新菜单的基本信息。
func (s *Store) Update(ctx context.Context, m *Menu) error {
	m.UpdatedAt = time.Now()
	res, err := s.db.NewUpdate().Model(m).
		Column("name", "slug", "description", "updated_at").
		WherePK().
		Exec(ctx)
	if err != nil {
		return translate(fmt.Errorf("更新菜单: %w", err))
	}
	return requireOneRow(res)
}

// Delete 删除菜单；其条目由外键级联删除。
func (s *Store) Delete(ctx context.Context, id int64) error {
	res, err := s.db.NewDelete().Model((*Menu)(nil)).Where("id = ?", id).Exec(ctx)
	if err != nil {
		return fmt.Errorf("删除菜单: %w", err)
	}
	return requireOneRow(res)
}

// Items 返回某菜单的全部条目，按父项与 position 排序。
func (s *Store) Items(ctx context.Context, menuID int64) ([]Item, error) {
	out := []Item{}
	if err := s.db.NewSelect().Model(&out).
		Where("mi.menu_id = ?", menuID).
		OrderExpr("mi.parent_id NULLS FIRST, mi.position, mi.id").
		Scan(ctx); err != nil {
		return nil, fmt.Errorf("查询菜单条目: %w", err)
	}
	return out, nil
}

// ReplaceItems 用给定条目整体替换菜单的条目树。
//
// 整体替换而不是逐条 diff：菜单是「一次性编辑、一次性保存」的表单，
// 逐条同步要处理移动、重排、删除与重建的交叉情形，出错的概率远大于收益。
// 代价是条目 ID 每次保存都会变——主题按 label 与 url 渲染，不依赖 ID。
//
// items 须为深度优先序（父项一定排在其子项之前），parents[i] 给出第 i 项在
// items 中的父项下标，-1 表示一级条目。父项的行号只有在插入后才能拿到，
// 所以按下标而不是按客户端给的 ID 建立父子关系。
//
// 事务开头先对菜单父行加行锁（SELECT ... FOR UPDATE），把同一菜单的整树替换串行化：
// 两个并发事务若都先 DELETE 再各自 INSERT，最终表里会同时留下两棵树
// （DELETE 看不见对方尚未提交的插入），而两个请求都返回成功。
func (s *Store) ReplaceItems(ctx context.Context, menuID int64, items []Item, parents []int) error {
	if len(items) != len(parents) {
		return fmt.Errorf("%w：条目数与父项数不一致", ErrInvalid)
	}
	return s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// 锁必须在 DELETE 之前拿到；行不存在时返回 ErrNotFound，
		// 与处理器先做的存在性检查互为兜底。
		var locked int64
		if err := tx.NewRaw("SELECT id FROM menus WHERE id = ? FOR UPDATE", menuID).
			Scan(ctx, &locked); err != nil {
			return translate(fmt.Errorf("锁定菜单: %w", err))
		}

		if _, err := tx.NewDelete().Model((*Item)(nil)).Where("menu_id = ?", menuID).Exec(ctx); err != nil {
			return fmt.Errorf("清空菜单条目: %w", err)
		}
		if len(items) == 0 {
			return nil
		}

		now := time.Now()
		newIDs := make([]int64, len(items))
		for i := range items {
			row := items[i]
			row.ID = 0
			row.MenuID = menuID
			row.ParentID = nil
			row.CreatedAt, row.UpdatedAt = now, now
			if _, err := tx.NewInsert().Model(&row).Returning("id").Exec(ctx); err != nil {
				return translate(fmt.Errorf("写入菜单条目: %w", err))
			}
			newIDs[i] = row.ID

			if parent := parents[i]; parent >= 0 {
				if parent >= i {
					// 深度优先序保证父项在前，越界即调用方传错了顺序。
					return fmt.Errorf("%w：父条目必须排在其子条目之前", ErrInvalid)
				}
				if _, err := tx.NewUpdate().Model((*Item)(nil)).
					Set("parent_id = ?", newIDs[parent]).
					Where("id = ?", row.ID).
					Exec(ctx); err != nil {
					return fmt.Errorf("设置菜单层级: %w", err)
				}
			}
		}
		return nil
	})
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
		if v.Constraint == "menus_slug_key" {
			return ErrSlugTaken
		}
		return fmt.Errorf("%w：唯一约束 %s 冲突", ErrInvalid, v.Constraint)
	case database.CodeForeignKeyViolation:
		return ErrTargetNotFound
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
