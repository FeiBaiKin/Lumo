// Package plugin 提供插件系统：插件包加载、生命周期、设置声明与后端代码的运行。
//
// 插件分两种：纯声明式的只有清单、设置与静态资源；带后端的另有一个 plugin.wasm，
// 在 wazero 沙箱里运行（internal/plugin/wasm），能做什么全看宿主开放了什么、
// 站长授予了什么（capabilities.go、host.go）。
package plugin

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/uptrace/bun"

	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/auth/perm"
	"github.com/FeiBaiKin/lumo/internal/comment"
	"github.com/FeiBaiKin/lumo/internal/content"
	"github.com/FeiBaiKin/lumo/internal/database"
	"github.com/FeiBaiKin/lumo/internal/mail"
	"github.com/FeiBaiKin/lumo/internal/plugin/wasm"
	"github.com/FeiBaiKin/lumo/internal/settings"
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

// defaultIcon 是插件与它的资源页、后台页面没写图标时用的拼图。
const defaultIcon = "puzzle"

// maxConsecutiveCrashes 是自动停用前允许的连续崩溃次数（超时、panic、违反调用约定）。
const maxConsecutiveCrashes = 5

// Module 是插件模块。
type Module struct {
	app      *app.App
	registry *Registry
	store    *Store
	data     *DataStore
	db       *bun.DB
	settings *settings.Service
	// content 与 comments 是宿主写内容时转交的模块，mail 是发邮件用的服务；Start 时从 App 上取。
	content  *content.Module
	comments *comment.Module
	mail     *mail.Service
	logger   *slog.Logger
	root     string
	cacheDir string
	engine   *wasm.Engine

	crashMu sync.Mutex
	crashes map[string]int

	// jobs 是待派发给插件的动作，见 hooks.go。
	jobs chan actionJob
	// cronRunning 是本实例上还没跑完的定时任务，见 cron.go。
	cronRunning *runningJobs
	// transport 替换对外请求的传输层，只在测试里用；nil 时用带地址核对的 fetchTransport。
	transport http.RoundTripper
}

// UseFetchTransport 替换插件对外请求的传输层。测试用：把请求路由到本地的假服务器，
// 域名白名单照常核对（那一步在传输层之前）。
func (m *Module) UseFetchTransport(rt http.RoundTripper) { m.transport = rt }

// New 构造模块。
func New() *Module { return &Module{} }

// Name 实现 app.Module。
func (m *Module) Name() string { return Name }

// Register 实现 app.Module：只做装配，不访问数据库。
func (m *Module) Register(a *app.App) error {
	cfg := a.Config()
	m.app = a
	m.logger = a.Logger().With(slog.String("module", Name))
	m.root = filepath.Join(cfg.DataDir, workdir.PluginsDirName)
	m.cacheDir = filepath.Join(cfg.DataDir, workdir.CacheDirName, "plugins")
	m.crashes = map[string]int{}
	m.jobs = make(chan actionJob, actionQueueSize)
	m.cronRunning = &runningJobs{keys: map[string]bool{}}
	// 内核经 App 的事件总线发动作、跑过滤器，主题经前台入口拿插槽、小组件与短代码，插件模块是两者的实现
	a.SetEvents(m)
	a.SetFrontend(m)
	if db := a.DB(); db != nil {
		m.db = db.DB
		m.store = NewStore(db.DB)
		m.data = NewDataStore(db.DB)
	}
	m.registry = NewRegistry(&RegistryOptions{
		Root:   m.root,
		Store:  m.store,
		Data:   m.data,
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

// Start 实现 app.Starter：备好运行时，扫描已安装插件、与库里的状态对齐，启动启用中插件的后端。
//
// 这是模块第一次被允许访问数据库的时机。运行时起不来不算启动失败：
// 纯声明式插件照常可用，带后端的插件启用时会得到明确的错误。
func (m *Module) Start(ctx context.Context) error {
	if m.store == nil {
		return nil
	}
	m.settings = settings.From(m.app)
	m.content = content.From(m.app)
	m.comments = comment.From(m.app)
	m.mail = mail.From(m.app)
	engine, err := wasm.NewEngine(ctx, wasm.Options{CacheDir: m.cacheDir, Host: m.host, Logger: m.logger})
	if err != nil {
		m.logger.Error("插件运行时启动失败，带后端的插件暂时用不了", slog.Any("error", err))
	} else {
		m.engine = engine
		m.registry.SetEngine(engine)
	}
	for range actionWorkers {
		go m.runActions(ctx)
	}
	go m.sweepKV(ctx)
	go m.runCron(ctx)
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

// kvSweepInterval 是清理过期键值的间隔。
const kvSweepInterval = time.Hour

// sweepKV 定时清理过期的键值；多实例时经 ClaimRun 认领，每轮只有一个实例执行。
func (m *Module) sweepKV(ctx context.Context) {
	ticker := time.NewTicker(kvSweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			claimed, err := database.ClaimRun(ctx, m.db, "plugin.kv.sweep", kvSweepInterval)
			if err == nil && claimed {
				_, err = m.data.SweepKV(ctx)
			}
			if err != nil && ctx.Err() == nil {
				m.logger.Warn("清理过期的插件数据失败", slog.Any("error", err))
			}
		}
	}
}

// MountRoutes 把插件的接口与静态文件挂到根路由上，由 serve 在模块启动之后调用。
//
// 插件接口不经 huma：路径由插件在运行时声明，进不了启动期生成的接口规范。resolve 解析凭据但不强制
// （失效的会话按匿名处理，不回 401）；CSRF 由 serveRoute 按接口是否公开分别处置。
// 静态文件不套会话解析，理由同主题静态资源。
func (m *Module) MountRoutes(r chi.Router, resolve func(http.Handler) http.Handler) {
	if m.store == nil {
		return
	}
	r.Mount(AssetsPath, m.AssetsHandler())
	var handler http.Handler = http.HandlerFunc(m.serveRoute)
	if resolve != nil {
		handler = resolve(handler)
	}
	r.Handle(RoutesPrefix+"/{plugin}/*", handler)
	r.Handle(RoutesPrefix+"/{plugin}", handler)
}

// Close 实现 app.Closer：停掉全部后端并关闭运行时。
func (m *Module) Close(ctx context.Context) error {
	if m.registry != nil {
		m.registry.Close()
	}
	if m.engine != nil {
		return m.engine.Close(ctx)
	}
	return nil
}

// Registry 返回插件注册表，供其他模块与测试取用。
func (m *Module) Registry() *Registry { return m.registry }

// Invoke 调用一个运行中插件的后端，并累计连续崩溃：到了上限就自动停用它。
func (m *Module) Invoke(ctx context.Context, name string, req wasm.Request, timeout time.Duration) (json.RawMessage, error) {
	backend, ok := m.registry.Backend(name)
	if !ok {
		return nil, wasm.ErrClosed
	}
	out, err := backend.Call(ctx, req, timeout)
	m.recordOutcome(ctx, name, err)
	return out, err
}

// recordOutcome 记下一次调用的结果。只有崩溃算数：处理函数自己返回的错误不累计。
func (m *Module) recordOutcome(ctx context.Context, name string, err error) {
	crashed := wasm.IsCrash(err)
	m.crashMu.Lock()
	if !crashed {
		delete(m.crashes, name)
		m.crashMu.Unlock()
		return
	}
	m.crashes[name]++
	count := m.crashes[name]
	if count >= maxConsecutiveCrashes {
		delete(m.crashes, name)
	}
	m.crashMu.Unlock()

	m.logger.Warn("插件运行出错", slog.String("plugin", name), slog.Int("consecutive", count), slog.Any("error", err))
	if count >= maxConsecutiveCrashes {
		reason := fmt.Sprintf("连续 %d 次运行出错，已自动停用。最后一次：%v", count, err)
		m.registry.Suspend(context.WithoutCancel(ctx), name, reason)
	}
}

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
//
// 启用中的插件各自的资源页也在这里：每个插件一组，排在「系统」之前（WordPress 里插件的顶级菜单），
// 每次取菜单时现算，启停插件后侧栏立即跟着变。
func (m *Module) Navigation() app.Navigation {
	nav := app.Navigation{Items: []app.NavItem{{
		Key: "plugins", Label: "插件", Path: PathPlugins, Icon: defaultIcon,
		Group: app.NavGroupSystem, Order: 30, Permission: perm.PluginsManage.String(),
		Keywords: "plugins chajian kuozhan",
		// 插件的资源页也在 /plugins/ 之下，前缀匹配会让「插件」跟着一起亮
		End: true,
	}}}
	if m.registry == nil {
		return nav
	}
	for _, loaded := range m.registry.Enabled() {
		group := "plugin-" + loaded.ID()
		added := false
		for j := range loaded.Resources {
			res := &loaded.Resources[j]
			if !res.Menu {
				continue
			}
			icon := res.Icon
			if icon == "" {
				icon = defaultIcon
			}
			nav.Items = append(nav.Items, app.NavItem{
				Key: group + "-" + res.Path, Label: res.Label, Path: PathPlugins + "/" + loaded.ID() + "/" + res.Path,
				Icon: icon, Group: group, Order: j, Permission: res.Permission.String(),
				Keywords: loaded.ID() + " " + res.Path, Description: res.Description,
			})
			added = true
		}
		for j := range loaded.Manifest.Spec.Pages {
			page := &loaded.Manifest.Spec.Pages[j]
			if !page.InMenu() {
				continue
			}
			icon := page.Icon
			if icon == "" {
				icon = defaultIcon
			}
			nav.Items = append(nav.Items, app.NavItem{
				Key: group + "-p-" + page.Path, Label: page.Label, Path: PathPlugins + "/" + loaded.ID() + "/p/" + page.Path,
				Icon: icon, Group: group, Order: len(loaded.Resources) + j, Permission: page.RequiredPermission().String(),
				Keywords: loaded.ID() + " " + page.Path, Description: page.Description,
			})
			added = true
		}
		if added {
			nav.Groups = append(nav.Groups, app.NavGroup{Name: group, Label: loaded.Manifest.Spec.DisplayName, Order: 55})
		}
	}
	return nav
}
