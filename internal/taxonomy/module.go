// Package taxonomy 提供分类（树形）与标签（agent.md §8）。
//
// 这是第一个业务模块，也是模块形态的样板：自带迁移、经三平面注册面挂接口、
// 声明所用权限；核心对它一无所知，仅在 cmd/lumo/modules.go 装配。
package taxonomy

import (
	"embed"
	"io/fs"

	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/auth/perm"
	"github.com/FeiBaiKin/lumo/internal/settings"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Name 是模块名，也是迁移版本表的后缀（goose_db_version_taxonomy）。
const Name = "taxonomy"

// Module 是分类与标签模块。
type Module struct {
	store   *Store
	slugify Slugger
}

// New 构造模块。
func New() *Module {
	return &Module{}
}

// Name 实现 app.Module。
func (m *Module) Name() string { return Name }

// Register 实现 app.Module：只做装配，不访问数据库。
//
// 若 settings 模块先于本模块装配，slug 生成策略跟随站点设置；否则退回保留中文的缺省策略。
func (m *Module) Register(a *app.App) error {
	if db := a.DB(); db != nil {
		m.store = NewStore(db.DB)
	}
	if svc := settings.From(a); svc != nil {
		m.slugify = svc.Slug
	}
	return nil
}

// Migrations 实现 app.Migrator。
func (m *Module) Migrations() fs.FS {
	sub, err := fs.Sub(migrationFS, "migrations")
	if err != nil {
		// 路径是编译期常量，出错只可能是代码本身写错。
		panic("taxonomy: 迁移目录不存在: " + err.Error())
	}
	return sub
}

// Routes 实现 app.RouteProvider。
func (m *Module) Routes(r app.Router) {
	if m.store == nil {
		// 无数据库的场景（如只渲染帮助）不挂接口。
		return
	}
	NewHandler(m.store, m.slugify).Register(r.Console(), r.Public())
}

// Permissions 实现 app.PermissionProvider。
func (m *Module) Permissions() []app.Permission {
	return []app.Permission{{
		Key:         perm.TaxonomiesManage.String(),
		Label:       "管理分类与标签",
		Description: "创建、修改、删除分类与标签",
	}}
}
