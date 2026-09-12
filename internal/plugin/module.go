// Package plugin 提供插件系统：插件包加载、生命周期与设置声明（agent.md §14）。
//
// v1 的插件是**纯声明式**的：包内只有清单、设置声明与展示图，没有可执行的代码。
// 这一期的目标是先把插件契约立起来——包的格式、安装与启停的语义、设置如何生效。
// 缺的从来不是加载器；契约定不下来，加载器做出来也无处可挂（§14.2）。
//
// 后端代码的执行（wazero + host function 白名单）排在后面几期，
// 届时 plugin.yaml 会多一个 runtime 字段，包内会多出 .wasm，
// 而本包的结构不必推倒重来。
package plugin

import (
	"context"
	"embed"
	"io/fs"
	"log/slog"
	"path/filepath"

	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/auth/perm"
	"github.com/FeiBaiKin/lumo/internal/workdir"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Name 是模块名，也是迁移版本表的后缀（goose_db_version_plugin）。
const Name = "plugin"

// PathPlugins 是插件管理页在 Console 中的路径。
//
// 与接口的 /plugins 恰好同名，但两者是不同的东西：这一条是前端路由。
// 写成一个常量是为了让「它们恰好一样」这件事是显式的，而不是靠人记得两边一起改。
const PathPlugins = "/plugins"

// Module 是插件模块。
type Module struct {
	registry *Registry
	store    *Store
	logger   *slog.Logger
	root     string
}

// New 构造模块。
func New() *Module { return &Module{} }

// Name 实现 app.Module。
func (m *Module) Name() string { return Name }

// Register 实现 app.Module：只做装配，不访问数据库。
func (m *Module) Register(a *app.App) error {
	cfg := a.Config()
	m.logger = a.Logger().With(slog.String("module", Name))
	m.root = filepath.Join(cfg.DataDir, workdir.PluginsDirName)
	if db := a.DB(); db != nil {
		m.store = NewStore(db.DB)
	}
	m.registry = NewRegistry(&RegistryOptions{
		Root:   m.root,
		Store:  m.store,
		Logger: m.logger,
	})
	a.Provide(Name, m)
	return nil
}

// Migrations 实现 app.Migrator。
func (m *Module) Migrations() fs.FS {
	sub, err := fs.Sub(migrationFS, "migrations")
	if err != nil {
		panic("plugin: 迁移目录不存在: " + err.Error())
	}
	return sub
}

// Permissions 实现 app.PermissionProvider。
func (m *Module) Permissions() []app.Permission {
	return []app.Permission{{
		Key:         perm.PluginsManage.String(),
		Label:       "管理插件",
		Description: "安装、启用、停用与卸载插件",
	}}
}

// Routes 实现 app.RouteProvider。
func (m *Module) Routes(r app.Router) {
	if m.store == nil {
		return
	}
	NewHandler(m).Register(r.Console())
}

// Start 实现 app.Starter：扫描已安装插件并与库里的状态对齐。
//
// 这是模块第一次被允许访问数据库的时机（agent.md §3.2）。
func (m *Module) Start(ctx context.Context) error {
	if m.store == nil {
		return nil
	}
	if err := m.registry.Load(ctx); err != nil {
		return err
	}
	for name, reason := range m.registry.Broken() {
		m.logger.Warn("插件不可用", slog.String("plugin", name), slog.String("reason", reason))
	}
	m.logger.Info("插件系统就绪",
		slog.Int("installed", len(m.registry.List())),
		slog.Int("enabled", len(m.registry.Enabled())))
	return nil
}

// Registry 返回插件注册表，供其他模块与测试取用。
func (m *Module) Registry() *Registry { return m.registry }

// From 取回插件模块；未装配时返回 nil。
//
// 调用方须能在 nil 时退化——插件是可选能力，核心功能不该因为它而不可用。
func From(a *app.App) *Module {
	if a == nil {
		return nil
	}
	v, ok := a.Lookup(Name)
	if !ok {
		return nil
	}
	mod, _ := v.(*Module)
	return mod
}

// Navigation 实现 app.NavigationProvider。
//
// 插件能改后台的页面与设置，门槛与主题同级，故整项按 plugins:manage 收起。
func (m *Module) Navigation() app.Navigation {
	return app.Navigation{Items: []app.NavItem{{
		Key: "plugins", Label: "插件", Path: PathPlugins, Icon: "puzzle",
		Group: app.NavGroupSystem, Order: 30, Permission: perm.PluginsManage.String(),
		Keywords: "plugins chajian kuozhan",
	}}}
}
