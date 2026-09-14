// Package settings 提供统一的声明式设置：分组 Schema、值存储、校验与读取（agent.md §5）。
//
// 每个模块经 app.SettingsProvider 声明自己的分组；本模块在 Start 时汇总全部分组并编译 Schema，
// 其他模块经 From(app) 取得 Service 读取有效值。装配顺序上本模块应排在最前。
package settings

import (
	"context"
	"embed"
	"io/fs"

	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/auth/perm"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Name 是模块名，也是迁移版本表后缀与 App.Provide 的键。
const Name = "settings"

// Module 是设置模块。
type Module struct {
	app     *app.App
	service *Service
}

// New 构造模块。
func New() *Module {
	return &Module{}
}

// Name 实现 app.Module。
func (m *Module) Name() string { return Name }

// Register 实现 app.Module：装配存储与服务，并登记为共享服务供后续模块取用。
func (m *Module) Register(a *app.App) error {
	m.app = a
	var store *Store
	if db := a.DB(); db != nil {
		store = NewStore(db.DB)
	}
	m.service = NewService(store)
	a.Provide(Name, m.service)
	return nil
}

// Migrations 实现 app.Migrator。
func (m *Module) Migrations() fs.FS {
	sub, err := fs.Sub(migrationFS, "migrations")
	if err != nil {
		panic("settings: 迁移目录不存在: " + err.Error())
	}
	return sub
}

// Settings 实现 app.SettingsProvider：本模块自带 site 分组。
func (m *Module) Settings() []app.SettingGroup {
	return []app.SettingGroup{siteGroup()}
}

// Permissions 实现 app.PermissionProvider。
func (m *Module) Permissions() []app.Permission {
	return []app.Permission{{
		Key:         perm.SettingsManage.String(),
		Label:       "管理设置",
		Description: "读取与修改全部设置分组",
	}}
}

// Routes 实现 app.RouteProvider。
func (m *Module) Routes(r app.Router) {
	NewHandler(m.service).Register(r.Console(), r.Public())
}

// Start 实现 app.Starter：此时全部模块已注册，汇总它们声明的分组并编译。
func (m *Module) Start(context.Context) error {
	return m.service.RegisterGroups(m.app.Settings())
}

// From 取回设置服务；settings 模块未装配（或尚未注册）时返回 nil。
//
// 调用方须能在 nil 时退化（如 slug 策略退回 unicode），以便单独测试其他模块。
func From(a *app.App) *Service {
	if a == nil {
		return nil
	}
	v, ok := a.Lookup(Name)
	if !ok {
		return nil
	}
	svc, _ := v.(*Service)
	return svc
}

// Navigation 实现 app.NavigationProvider：设置页的侧边栏入口由分组列表推导。
//
// 用推导而不是逐个手写：设置分组可能来自模块，也可能来自主题或插件，
// 手写一份清单就必然有漏。此前 comment 分组在后端存在而在侧边栏没有入口，
// 正是因为那份清单是手写的。
func (m *Module) Navigation() app.Navigation {
	if m.service == nil {
		return app.Navigation{}
	}
	groups := m.service.Groups()
	items := make([]app.NavItem, 0, len(groups))
	for i, group := range groups {
		// 声明为 Hidden 的组有别的页面承载它的表单（见 app.SettingGroup.Hidden）。
		if group.Hidden {
			continue
		}
		items = append(items, app.NavItem{
			Key:         "settings-" + group.Name,
			Label:       group.Label,
			Path:        "/settings/" + group.Name,
			Icon:        group.Icon,
			Group:       app.NavGroupSettings,
			Order:       i,
			Permission:  perm.SettingsManage.String(),
			Description: group.Description,
		})
	}
	return app.Navigation{Items: items}
}
