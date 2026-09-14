// Package favorite 提供收藏：已登录用户把一篇内容标记下来，日后在「我的收藏」里找回。
//
// 分工与 search 相同——本模块只回答「谁收了哪几篇」，出的是内容 ID；
// 收藏页上那一列文章由主题自己的查询补齐（见 internal/theme 的 Favoriter 接口）。
// 这样收藏模块不必知道前台怎么展示一篇文章，主题也不必知道收藏是怎么存的。
//
// 依赖 content 模块的 posts 表与核心的 users 表：modules.go 中 content 必须先于本模块注册。
// 本模块又必须先于 theme 注册——主题在装配期就要取到它。
package favorite

import (
	"embed"
	"io/fs"

	"github.com/FeiBaiKin/lumo/internal/app"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Name 是模块名，也是迁移版本表的后缀（goose_db_version_favorite）。
const Name = "favorite"

// Module 是收藏模块。
// 没有 logger 字段：本模块没有任何需要在运行期告警的分支——
// 收藏失败经接口返回给调用者，不是站长要处理的事。
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
	a.Provide(Name, m)
	return nil
}

// Migrations 实现 app.Migrator。
func (m *Module) Migrations() fs.FS {
	sub, err := fs.Sub(migrationFS, "migrations")
	if err != nil {
		panic("favorite: 迁移目录不存在: " + err.Error())
	}
	return sub
}

// Routes 实现 app.RouteProvider。
func (m *Module) Routes(r app.Router) {
	if m.handler == nil {
		return
	}
	m.handler.Register(r.Public())
}

// Permissions 实现 app.PermissionProvider：收藏不需要任何权限。
//
// 明写一个返回 nil 的实现，而不是干脆不实现这个接口：收藏是登录用户人人可做的动作
// （与发评论同级），不该出现在权限清单里让站长以为自己需要给成员授权。
func (m *Module) Permissions() []app.Permission { return nil }

// Store 返回收藏存储，供主题前台取数（见 theme 的 Favoriter 接口）。
//
// 这是本模块唯一对外开放的入口，模块之间仍然不引用彼此的内部实现（agent.md §3.2）。
func (m *Module) Store() *Store { return m.store }

// From 取回收藏存储；模块未装配或没有数据库时返回 nil。
//
// 返回 Store 而不是 Module：调用方（theme）要的就是那几个查询，
// 拿到整个模块只会让它够得着不该碰的东西。
func From(a *app.App) *Store {
	if a == nil {
		return nil
	}
	v, ok := a.Lookup(Name)
	if !ok {
		return nil
	}
	mod, ok := v.(*Module)
	if !ok || mod == nil {
		return nil
	}
	return mod.store
}
