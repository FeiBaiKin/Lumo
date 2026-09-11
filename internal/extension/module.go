// Package extension 实现 Extension 平面的通用 CRUD（agent.md §6.1）。
//
// Extension 是给插件预留的自定义模型：核心不解释 spec 里有什么，只负责寻址、
// 校验命名与持久化。一条记录由 (apiGroup, version, kind, name) 唯一确定，
// 地址形如 /apis/io.github.feibaiikin.lumo/v1alpha1/posts/hello。
//
// 表 extensions 在核心迁移里就建好了，本模块只补一列 resource 与按它寻址的唯一索引：
// 地址里是复数段而表里存单数 kind，映射规则在 Go 侧且不可逆，故把结果存下来。
package extension

import (
	"embed"
	"io/fs"

	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/auth/perm"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Name 是模块名，也是迁移版本表的后缀（goose_db_version_extension）。
const Name = "extension"

// Module 是扩展记录模块。
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
		m.handler = NewHandler(m.store)
	}
	return nil
}

// Migrations 实现 app.Migrator。
func (m *Module) Migrations() fs.FS {
	sub, err := fs.Sub(migrationFS, "migrations")
	if err != nil {
		panic("extension: 迁移目录不存在: " + err.Error())
	}
	return sub
}

// Permissions 实现 app.PermissionProvider。
func (m *Module) Permissions() []app.Permission {
	return []app.Permission{{
		Key:         perm.ExtensionsManage.String(),
		Label:       "管理扩展记录",
		Description: "读写 Extension 平面上的自定义记录",
	}}
}

// Routes 实现 app.RouteProvider。
func (m *Module) Routes(r app.Router) {
	if m.store == nil {
		return
	}
	m.handler.Register(r.Extension())
}
