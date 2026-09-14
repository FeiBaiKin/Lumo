package theme

import (
	"context"
	"fmt"
	"maps"
	"sort"

	"github.com/uptrace/bun"

	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/settings"
)

// compiledGroup 是一个已编译的主题设置分组。
type compiledGroup struct {
	app.SettingGroup
	validator *settings.Validator
	defaults  map[string]any
	// intFields 是 Schema 中声明为 integer 的顶层字段名。
	intFields map[string]bool
}

// integerFields 从 Schema 文档里挑出声明为 integer 的顶层属性。
//
// 为什么需要它：设置值经 JSON 往返存取，而 encoding/json 把所有数字解成 float64。
// 模板里 {{ .Find.Posts.Related $id .Theme.Settings.content.relatedCount }} 会因此
// 报「expected int; got float64」——这是主题作者无从预料也无从修复的坑，
// 必须在服务端消化掉。Schema 已经声明了哪些字段是整数，据此还原即可。
func integerFields(doc map[string]any) map[string]bool {
	out := map[string]bool{}
	props, ok := doc["properties"].(map[string]any)
	if !ok {
		return out
	}
	for name, raw := range props {
		prop, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if typ, _ := prop["type"].(string); typ == "integer" {
			out[name] = true
		}
	}
	return out
}

// coerceInts 就地把声明为 integer 的字段从浮点还原成 int。
func (g *compiledGroup) coerceInts(values map[string]any) map[string]any {
	for name := range g.intFields {
		switch v := values[name].(type) {
		case float64:
			values[name] = int(v)
		case float32:
			values[name] = int(v)
		case int64:
			values[name] = int(v)
		}
	}
	return values
}

// compiledSettings 是一个主题的全部设置分组，按声明顺序编译。
type compiledSettings struct {
	groups []*compiledGroup
	byName map[string]*compiledGroup
}

// compileSettings 编译主题的设置声明。
//
// 复用 settings.Validator：主题设置与站点设置走同一个校验器与同一套错误明细格式，
// Console 的表单引擎因此不必区分两者（agent.md §5）。
func compileSettings(themeName string, decl *SettingsDecl) (*compiledSettings, error) {
	groups, err := decl.SettingGroups()
	if err != nil {
		return nil, err
	}

	out := &compiledSettings{byName: make(map[string]*compiledGroup, len(groups))}
	for i := range groups {
		g := groups[i]
		// 主题设置走的是 theme_settings 表，加解密没接进去。放行 Secret 字段会得到
		// 一个「界面上写着不回传、库里其实躺着明文」的假象，比直接不支持更糟。
		if err := settings.RejectSecrets("主题 "+themeName+" 的设置", g.Form); err != nil {
			return nil, fmt.Errorf("%w：%w", ErrInvalidPackage, err)
		}
		validator, err := settings.NewValidator(themeName+"."+g.Name, g.Form)
		if err != nil {
			return nil, fmt.Errorf("%w：%w", ErrInvalidPackage, err)
		}

		defaults := validator.DefaultValues()
		// 缺省值必须自洽：主题作者把 defaults 写得不符合自己的 schema 时，
		// 站长打开设置页会看到一堆无法保存的初始值。
		if err := validator.Validate(defaults); err != nil {
			return nil, fmt.Errorf("%w：分组 %q 的 defaults 未通过自身 schema：%w",
				ErrInvalidPackage, g.Name, err)
		}

		compiled := &compiledGroup{
			SettingGroup: g,
			validator:    validator,
			defaults:     defaults,
			intFields:    integerFields(validator.Doc()),
		}
		compiled.coerceInts(compiled.defaults)
		out.groups = append(out.groups, compiled)
		out.byName[g.Name] = compiled
	}
	sort.SliceStable(out.groups, func(i, j int) bool {
		if out.groups[i].Order != out.groups[j].Order {
			return out.groups[i].Order < out.groups[j].Order
		}
		return out.groups[i].Name < out.groups[j].Name
	})
	return out, nil
}

// settingsRecord 是 theme_settings 表的一行。
type settingsRecord struct {
	bun.BaseModel `bun:"table:theme_settings,alias:ts"`

	Theme  string         `bun:"theme,pk"`
	Group  string         `bun:"group_name,pk"`
	Values map[string]any `bun:"values,type:jsonb"`
}

// SettingsStore 持久化主题设置值。
//
// 与站点设置分表：主题设置随主题走，卸载主题时要能连同它的设置一起清掉，
// 而站点设置是核心资产。混在一张表里迟早会误删。
type SettingsStore struct {
	db *bun.DB
}

// NewSettingsStore 构造 SettingsStore。
func NewSettingsStore(db *bun.DB) *SettingsStore { return &SettingsStore{db: db} }

// Load 读取某主题全部分组的已保存值。
func (s *SettingsStore) Load(ctx context.Context, theme string) (map[string]map[string]any, error) {
	var rows []settingsRecord
	if err := s.db.NewSelect().Model(&rows).Where("ts.theme = ?", theme).Scan(ctx); err != nil {
		return nil, fmt.Errorf("读取主题设置: %w", err)
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

// Save 覆盖式写入某主题某分组的值。
func (s *SettingsStore) Save(ctx context.Context, theme, group string, values map[string]any) error {
	if values == nil {
		values = map[string]any{}
	}
	row := &settingsRecord{Theme: theme, Group: group, Values: values}
	_, err := s.db.NewInsert().Model(row).
		On("CONFLICT (theme, group_name) DO UPDATE").
		Set("values = EXCLUDED.values").
		Set("updated_at = now()").
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("写入主题设置: %w", err)
	}
	return nil
}

// Purge 删除某主题的全部设置值，供卸载主题时调用。
func (s *SettingsStore) Purge(ctx context.Context, theme string) error {
	if _, err := s.db.NewDelete().Model((*settingsRecord)(nil)).
		Where("theme = ?", theme).Exec(ctx); err != nil {
		return fmt.Errorf("清除主题设置: %w", err)
	}
	return nil
}

// activeRecord 是 theme_state 表的一行，全表只有一行。
type activeRecord struct {
	bun.BaseModel `bun:"table:theme_state,alias:tst"`

	ID     int    `bun:"id,pk"`
	Active string `bun:"active"`
}

// StateStore 持久化「当前启用的是哪个主题」。
type StateStore struct {
	db *bun.DB
}

// NewStateStore 构造 StateStore。
func NewStateStore(db *bun.DB) *StateStore { return &StateStore{db: db} }

// Active 读取当前启用的主题名；从未设置过时返回空串。
func (s *StateStore) Active(ctx context.Context) (string, error) {
	var rows []activeRecord
	if err := s.db.NewSelect().Model(&rows).Where("tst.id = 1").Scan(ctx); err != nil {
		return "", fmt.Errorf("读取启用主题: %w", err)
	}
	if len(rows) == 0 {
		return "", nil
	}
	return rows[0].Active, nil
}

// SetActive 写入当前启用的主题名。
func (s *StateStore) SetActive(ctx context.Context, name string) error {
	row := &activeRecord{ID: 1, Active: name}
	_, err := s.db.NewInsert().Model(row).
		On("CONFLICT (id) DO UPDATE").
		Set("active = EXCLUDED.active").
		Set("updated_at = now()").
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("写入启用主题: %w", err)
	}
	return nil
}

// effectiveSettings 把已保存值与缺省值合并成完整的设置对象。
//
// 合并后统一把 integer 字段还原成 int：从库里读出的值经过 JSON 往返，
// 整数会变成 float64，而模板把它当整数用时会报类型错。
func (c *compiledSettings) effectiveSettings(stored map[string]map[string]any) map[string]map[string]any {
	out := make(map[string]map[string]any, len(c.groups))
	for _, g := range c.groups {
		out[g.Name] = g.coerceInts(settings.Merge(g.defaults, stored[g.Name]))
	}
	return out
}

// group 按名称取分组。
func (c *compiledSettings) group(name string) (*compiledGroup, bool) {
	g, ok := c.byName[name]
	return g, ok
}

// defaultValues 返回分组缺省值的副本。
func (g *compiledGroup) defaultValues() map[string]any { return maps.Clone(g.defaults) }
