package plugin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/uptrace/bun"
)

// Record 是 plugins 表的一行。
type Record struct {
	bun.BaseModel `bun:"table:plugins,alias:p"`

	Name        string `bun:"name,pk"`
	Version     string `bun:"version,notnull"`
	DisplayName string `bun:"display_name,notnull"`
	Description string `bun:"description,notnull"`
	Author      string `bun:"author,notnull"`
	Enabled     bool   `bun:"enabled,notnull"`
	Manifest    []byte `bun:"manifest,type:jsonb,notnull"`
	// Granted 是站长授予过的能力；nil 表示从没授予过。
	Granted *Capabilities `bun:"granted,type:jsonb,nullzero"`
	// DisabledReason 是系统停用插件的原因，手动停用时为空串。
	DisabledReason string    `bun:"disabled_reason,notnull"`
	InstalledAt    time.Time `bun:"installed_at,nullzero,notnull,default:now()"`
	UpdatedAt      time.Time `bun:"updated_at,nullzero,notnull,default:now()"`
}

// Store 持久化插件状态与设置值。
type Store struct {
	db *bun.DB
}

// NewStore 构造 Store。
func NewStore(db *bun.DB) *Store { return &Store{db: db} }

// List 返回全部已安装插件，按名称排序。
func (s *Store) List(ctx context.Context) ([]Record, error) {
	var rows []Record
	if err := s.db.NewSelect().Model(&rows).Order("p.name ASC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("读取插件列表: %w", err)
	}
	return rows, nil
}

// Get 读取一个插件；不存在时返回 ErrNotFound。
func (s *Store) Get(ctx context.Context, name string) (*Record, error) {
	row := new(Record)
	err := s.db.NewSelect().Model(row).Where("p.name = ?", name).Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w：%s", ErrNotFound, name)
		}
		return nil, fmt.Errorf("读取插件 %s: %w", name, err)
	}
	return row, nil
}

// Put 写入或整体覆盖一个插件的元信息。
//
// 安装与升级共用这一条：两者的差别只在 overwrite，而那是文件系统层面的事。
func (s *Store) Put(ctx context.Context, manifest *Manifest, raw []byte) error {
	row := &Record{
		Name:        manifest.Metadata.Name,
		Version:     manifest.Spec.Version,
		DisplayName: manifest.Spec.DisplayName,
		Description: manifest.Spec.Description,
		Author:      manifest.Spec.Author.Name,
		Manifest:    raw,
	}
	_, err := s.db.NewInsert().Model(row).
		On("CONFLICT (name) DO UPDATE").
		// enabled 不在覆盖之列：升级一个已启用的插件不该顺手把它关掉。
		Set("version = EXCLUDED.version").
		Set("display_name = EXCLUDED.display_name").
		Set("description = EXCLUDED.description").
		Set("author = EXCLUDED.author").
		Set("manifest = EXCLUDED.manifest").
		Set("updated_at = now()").
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("写入插件 %s: %w", row.Name, err)
	}
	return nil
}

// State 是插件的启用状态。
type State struct {
	Enabled bool
	// Reason 是系统停用的原因，手动启停时为空串。
	Reason string
	// Granted 是授予过的能力；nil 表示不改动库里已有的值。
	Granted *Capabilities
}

// SetState 写入插件的启用状态。
func (s *Store) SetState(ctx context.Context, name string, st State) error {
	q := s.db.NewUpdate().Model((*Record)(nil)).
		Set("enabled = ?", st.Enabled).
		Set("disabled_reason = ?", st.Reason).
		Set("updated_at = now()").
		Where("p.name = ?", name)
	if st.Granted != nil {
		raw, err := json.Marshal(st.Granted)
		if err != nil {
			return fmt.Errorf("编码插件 %s 授予的能力: %w", name, err)
		}
		q = q.Set("granted = ?::jsonb", string(raw))
	}
	res, err := q.Exec(ctx)
	if err != nil {
		return fmt.Errorf("更新插件 %s 的启用状态: %w", name, err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return fmt.Errorf("%w：%s", ErrNotFound, name)
	}
	return nil
}

// Delete 删除一个插件的状态；它的设置值由外键级联一并清除。
func (s *Store) Delete(ctx context.Context, name string) error {
	if _, err := s.db.NewDelete().Model((*Record)(nil)).Where("p.name = ?", name).Exec(ctx); err != nil {
		return fmt.Errorf("删除插件 %s: %w", name, err)
	}
	return nil
}

// settingsRecord 是 plugin_settings 表的一行。
type settingsRecord struct {
	bun.BaseModel `bun:"table:plugin_settings,alias:ps"`

	Plugin string         `bun:"plugin,pk"`
	Group  string         `bun:"group_name,pk"`
	Values map[string]any `bun:"values,type:jsonb"`
}

// LoadSettings 读取某个插件全部分组的已保存值。
func (s *Store) LoadSettings(ctx context.Context, name string) (map[string]map[string]any, error) {
	var rows []settingsRecord
	if err := s.db.NewSelect().Model(&rows).Where("ps.plugin = ?", name).Scan(ctx); err != nil {
		return nil, fmt.Errorf("读取插件设置: %w", err)
	}
	out := make(map[string]map[string]any, len(rows))
	for i := range rows {
		values := rows[i].Values
		if values == nil {
			values = map[string]any{}
		}
		out[rows[i].Group] = values
	}
	return out, nil
}

// SaveSettings 覆盖式写入某个插件某个分组的值。
func (s *Store) SaveSettings(ctx context.Context, name, group string, values map[string]any) error {
	if values == nil {
		values = map[string]any{}
	}
	row := &settingsRecord{Plugin: name, Group: group, Values: values}
	_, err := s.db.NewInsert().Model(row).
		On("CONFLICT (plugin, group_name) DO UPDATE").
		Set("values = EXCLUDED.values").
		Set("updated_at = now()").
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("写入插件设置: %w", err)
	}
	return nil
}
