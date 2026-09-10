package settings

import (
	"context"
	"fmt"
	"time"

	"github.com/uptrace/bun"
)

// record 是 settings 表的一行。
type record struct {
	bun.BaseModel `bun:"table:settings,alias:st"`

	Name      string         `bun:"name,pk"`
	Values    map[string]any `bun:"values,type:jsonb"`
	UpdatedAt time.Time      `bun:"updated_at,nullzero"`
}

// Store 提供设置值的持久化操作。
type Store struct {
	db *bun.DB
}

// NewStore 构造 Store。
func NewStore(db *bun.DB) *Store {
	return &Store{db: db}
}

// Load 读取分组的已保存值；从未保存过时返回空对象。
func (s *Store) Load(ctx context.Context, name string) (map[string]any, error) {
	var rows []record
	if err := s.db.NewSelect().Model(&rows).Where("st.name = ?", name).Scan(ctx); err != nil {
		return nil, fmt.Errorf("读取设置 %s: %w", name, err)
	}
	if len(rows) == 0 || rows[0].Values == nil {
		return map[string]any{}, nil
	}
	return rows[0].Values, nil
}

// Save 覆盖式写入分组的值。
func (s *Store) Save(ctx context.Context, name string, values map[string]any) error {
	if values == nil {
		values = map[string]any{}
	}
	row := &record{Name: name, Values: values, UpdatedAt: time.Now()}
	_, err := s.db.NewInsert().Model(row).
		On("CONFLICT (name) DO UPDATE").
		Set("values = EXCLUDED.values").
		Set("updated_at = EXCLUDED.updated_at").
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("写入设置 %s: %w", name, err)
	}
	return nil
}
