// Package settings 提供统一的声明式设置：分组 Schema、值存储、校验与读取（agent.md §5）。
//
// 每个模块经 app.SettingsProvider 声明自己的分组；本模块在 Start 时汇总全部分组并编译 Schema，
// 其他模块经 From(app) 取得 Service 读取有效值。装配顺序上本模块应排在最前。
package settings

import (
	"context"
	"embed"
	"fmt"
	"io/fs"

	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/auth/perm"
	"github.com/FeiBaiKin/lumo/internal/secret"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Name 是模块名，也是迁移版本表后缀与 App.Provide 的键。
const Name = "settings"

// Path 是设置页在 Console 内的路径，也是本模块接口在 Console 平面下的前缀。
//
// 页面与接口共用这一段是刻意的：`/settings/<分组>` 既是接口地址，也是
// 「展开那一块」的页面地址，两者写成两个常量迟早会漂开。
const Path = "/" + Name

// Icon 是设置在侧边栏与页内的图标名（见 console/src/lib/icons.ts）。
const Icon = "settings"

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
	// 声明成接口类型而不是 *Store：nil 的 *Store 装进接口之后不等于 nil，
	// 于是 s.store != nil 会成立，接着就是一次空指针解引用。
	var store ValueStore
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
//
// 主密钥在编译之后才加载：只有真有分组声明了口令字段时才需要碰密钥文件。
// 反过来的话，每个只装了本站的目录里都会多出一个 secret.key——多一个「这文件要不要
// 跟着备份」的问题，而多数站点根本没有任何需要加密的口令。
func (m *Module) Start(context.Context) error {
	if err := m.service.RegisterGroups(m.app.Settings()); err != nil {
		return err
	}
	m.service.SetLogger(m.app.Logger())
	if !m.service.HasSecrets() {
		return nil
	}
	keyring, err := secret.Open(m.app.Config().DataDir)
	if err != nil {
		return fmt.Errorf("settings: 加载主密钥: %w", err)
	}
	m.service.SetKeyring(keyring)
	return nil
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

// Navigation 实现 app.NavigationProvider：侧边栏一个入口，分组做命令面板的直达项。
//
// 侧边栏只有「站点设置」一条：所有分组现在铺在同一页上（见 console 的 settings 页），
// 再给每个分组画一条侧栏菜单，等于让站长先选一个分组才能开始配置，
// 而他要改的两项很可能分属两组。
//
// 分组仍各自下发一条 Hidden 的菜单项。Hidden 的项不进侧边栏，但仍进命令面板与标题映射
// （见 app.NavItem.Hidden）：Ctrl+K 里搜「邮件」应当能直接跳到邮件那一块，
// 而这正是把入口收成一个之后唯一会丢掉的东西。
//
// 用推导而不是逐个手写：设置分组可能来自模块，也可能来自主题或插件，
// 手写一份清单就必然有漏。此前 comment 分组在后端存在而在侧边栏没有入口，
// 正是因为那份清单是手写的。
func (m *Module) Navigation() app.Navigation {
	if m.service == nil {
		return app.Navigation{}
	}
	groups := m.service.Groups()
	items := make([]app.NavItem, 0, len(groups)+1)
	items = append(items, app.NavItem{
		Key:         Name,
		Label:       "站点设置",
		Path:        Path,
		Icon:        Icon,
		Group:       app.NavGroupSettings,
		Order:       0,
		Permission:  perm.SettingsManage.String(),
		Description: "站点、存储、邮件、评论、注册与 SEO 的全部配置",
		Keywords:    "settings shezhi peizhi zhandian",
		End:         true,
	})
	for i, group := range groups {
		items = append(items, app.NavItem{
			Key:         "settings-" + group.Name,
			Label:       group.Label,
			Path:        Path + "/" + group.Name,
			Icon:        group.Icon,
			Group:       app.NavGroupSettings,
			Order:       i + 1,
			Permission:  perm.SettingsManage.String(),
			Description: group.Description,
			Hidden:      true,
		})
	}
	return app.Navigation{Items: items}
}
