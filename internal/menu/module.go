// Package menu 提供导航菜单（agent.md §8）。
//
// 菜单分两层：menus 是菜单本身（主题按 slug 引用），menu_items 是它的条目树。
// 条目分两类——自定义链接手填地址，站内条目（文章 / 页面 / 分类 / 标签）指向记录 ID，
// 地址在读取时解析，记录改名或改 slug 后菜单自动跟随。
package menu

import (
	"embed"
	"io/fs"
	"log/slog"

	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/auth/perm"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Name 是模块名，也是迁移版本表的后缀（goose_db_version_menu）。
const Name = "menu"

// Module 是菜单模块。
type Module struct {
	store   *Store
	handler *Handler
}

// New 构造模块。
func New() *Module { return &Module{} }

// Name 实现 app.Module。
func (m *Module) Name() string { return Name }

// Register 实现 app.Module：只做装配，不访问数据库。
func (m *Module) Register(a *app.App) error {
	if db := a.DB(); db != nil {
		m.store = NewStore(db.DB)
		m.handler = NewHandler(m.store, &resolver{db: db.DB}, a.Logger().With(slog.String("module", Name)))
	}
	return nil
}

// Migrations 实现 app.Migrator。
func (m *Module) Migrations() fs.FS {
	sub, err := fs.Sub(migrationFS, "migrations")
	if err != nil {
		panic("menu: 迁移目录不存在: " + err.Error())
	}
	return sub
}

// Permissions 实现 app.PermissionProvider。
func (m *Module) Permissions() []app.Permission {
	return []app.Permission{{
		Key:         perm.MenusManage.String(),
		Label:       "管理菜单",
		Description: "创建、修改与删除导航菜单及其条目",
	}}
}

// Routes 实现 app.RouteProvider。
func (m *Module) Routes(r app.Router) {
	if m.store == nil {
		return
	}
	m.handler.Register(r.Console(), r.Public())
}
