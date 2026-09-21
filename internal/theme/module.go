// Package theme 提供主题系统：模板引擎、主题包加载、前台路由与主题设置。
//
// 主题是一个 zip 包（theme.yaml + settings.yaml + templates/ + static/），后台上传即可切换。
// 模板引擎用 html/template——主题是第三方代码，XSS 面就在主题里，而它是唯一做
// 上下文感知转义的引擎；模板作者体验由 Hugo 风格的 layout / partial 约定与函数库补齐。
//
// 内置默认主题「墨 Ink」编译进二进制，既是开箱即用的外观，也是所有主题的回退：
// 第三方主题缺失可选模板时自动回退到它的同名模板。
package theme

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/auth/perm"
	"github.com/FeiBaiKin/lumo/internal/favorite"
	"github.com/FeiBaiKin/lumo/internal/search"
	"github.com/FeiBaiKin/lumo/internal/settings"
	"github.com/FeiBaiKin/lumo/internal/workdir"
)

// devModeEnabled 读取开发模式开关。
//
// 走环境变量而非配置文件：它只在开发机上开，写进 config.yaml 容易被误提交到生产。
func devModeEnabled() bool {
	enabled, err := strconv.ParseBool(os.Getenv(EnvDevMode))
	return err == nil && enabled
}

//go:embed migrations/*.sql
var migrationFS embed.FS

// builtinFS 是内置默认主题，编译进二进制。
//
// all: 前缀确保包含以点开头的文件；主题包里有 .gitkeep 一类占位文件时不会漏。
//
//go:embed all:builtin
var builtinFS embed.FS

// Name 是模块名，也是迁移版本表的后缀（goose_db_version_theme）。
const Name = "theme"

// EnvDevMode 开启主题开发模式：模板改动自动重载、静态资源不缓存。
const EnvDevMode = "LUMO_THEME_DEV"

// Module 是主题模块。
type Module struct {
	registry *Registry
	store    *Store
	settings *SettingsStore
	state    *StateStore
	renderer *Renderer
	frontend *Frontend
	logger   *slog.Logger
	root     string
	devMode  bool
	// builtin 是二进制里的内置主题（已剥到主题包根），用于落盘与「恢复出厂」。
	builtin fs.FS
}

// New 构造模块。
func New() *Module { return &Module{} }

// Name 实现 app.Module。
func (m *Module) Name() string { return Name }

// Register 实现 app.Module：只做装配，不访问数据库。
//
// 注意主题注册表在此构造：它要加载内置主题并解析模板，是纯文件系统操作，
// 与数据库无关。启用哪个主题要读库，那在 Start 里做。
func (m *Module) Register(a *app.App) error {
	cfg := a.Config()
	m.logger = a.Logger().With(slog.String("module", Name))
	m.root = filepath.Join(cfg.DataDir, workdir.ThemesDirName)
	m.devMode = devModeEnabled()

	builtin, err := fs.Sub(builtinFS, "builtin/"+BuiltinName)
	if err != nil {
		return err
	}
	m.builtin = builtin
	registry, err := NewRegistry(&RegistryOptions{
		Root:    m.root,
		Builtin: builtin,
		Logger:  m.logger,
		DevMode: m.devMode,
	})
	if err != nil {
		return err
	}
	m.registry = registry

	if db := a.DB(); db != nil {
		m.store = NewStore(db.DB)
		m.settings = NewSettingsStore(db.DB)
		m.state = NewStateStore(db.DB)
		// 装配了 search 模块就用全文索引，否则搜索页退回标题模糊匹配。
		if searcher := search.From(a); searcher != nil {
			m.store.UseSearcher(searcher)
		}
		// 装配了 favorite 模块才有收藏页与收藏按钮；没有它时前台不显示这个功能。
		if favorites := favorite.From(a); favorites != nil {
			m.store.UseFavorites(favorites)
		}
	}
	m.renderer = NewRenderer(&RendererOptions{
		Registry: registry,
		Store:    m.store,
		Settings: settings.From(a),
		Themes:   m.settings,
		Logger:   m.logger,
	})
	// 内容时间要按站点时区渲染，时区来自站点设置，因此得等 renderer 装好再回注。
	if m.store != nil {
		m.store.UseTimezone(m.renderer.Location)
	}
	m.frontend = NewFrontend(m.renderer, m.store)

	a.Provide(Name, m)
	return nil
}

// Migrations 实现 app.Migrator。
func (m *Module) Migrations() fs.FS {
	sub, err := fs.Sub(migrationFS, "migrations")
	if err != nil {
		panic("theme: 迁移目录不存在: " + err.Error())
	}
	return sub
}

// Permissions 实现 app.PermissionProvider。
//
// themes:manage 已由核心声明（app.CorePermissions），此处不重复声明，
// 否则权限清单里会出现两条同名项。
func (m *Module) Permissions() []app.Permission { return nil }

// Routes 实现 app.RouteProvider：挂载 Console 管理接口。
//
// 前台路由不在这里：它产出 HTML 而非 JSON，也不走三平面的鉴权模型，
// 由 serve 直接挂在根路由上（见 MountFrontend）。
func (m *Module) Routes(r app.Router) {
	if m.store == nil {
		return
	}
	NewHandler(m).Register(r.Console(), r.Public())
}

// Start 实现 app.Starter：加载已安装主题并恢复启用项。
//
// 这是模块第一次被允许访问数据库的时机。
func (m *Module) Start(ctx context.Context) error {
	// 先把内置主题解压到 data/themes（已存在则不动），再扫目录 ——
	// 顺序不能反：LoadInstalled 正是靠磁盘上那份把注册表里的内置主题顶掉。
	if m.builtin != nil {
		created, err := MaterializeBuiltin(m.root, m.builtin)
		if err != nil {
			// 落盘失败不拦启动：二进制里那份照样服务，只是站长这次在
			// data/themes 下看不到内置主题。
			m.logger.Warn("内置主题落盘失败，本次仍用二进制里那份", slog.Any("error", err))
		} else if created {
			m.logger.Info("内置主题已解压到 data/themes",
				slog.String("dir", filepath.Join(m.root, BuiltinName)))
		}
	}

	if err := m.registry.LoadInstalled(); err != nil {
		return err
	}
	if broken := m.registry.Broken(); len(broken) > 0 {
		for name, reason := range broken {
			m.logger.Warn("主题不可用", slog.String("theme", name), slog.String("reason", reason))
		}
	}

	if m.state != nil {
		active, err := m.state.Active(ctx)
		if err != nil {
			return err
		}
		if active != "" {
			if err := m.registry.Activate(active); err != nil {
				// 启用的主题被删或加载失败时退回内置主题，站点继续可访问。
				m.logger.Warn("启用的主题不可用，已回退到内置主题",
					slog.String("theme", active), slog.String("fallback", BuiltinName))
			}
		}
	}

	active := m.registry.Active()
	m.logger.Info("主题系统就绪",
		slog.String("active", m.registry.ActiveName()),
		slog.String("source", active.Source),
		slog.Int("installed", len(m.registry.List())),
		slog.Bool("devMode", m.devMode))

	if m.devMode {
		go m.registry.Watch(ctx)
	}
	return nil
}

// MountFrontend 把访客前台路由与主题静态资源挂到根路由上。
//
// 由 serve 在全部模块注册之后调用：前台的兜底路由 /{slug} 会吞掉根路径下的
// 一切单段路径，必须排在 /console/、/uploads/ 与 SEO 文档之后注册。
//
// 直接接受 chi.Router 而非自定义接口：前台要用 chi.URLParam 读路径参数，
// 本就与 chi 的路由树绑定，抽象一层只会掩盖这个事实。
//
// optional 是「解析会话但不强制认证」的中间件（auth.Authenticator.Optional）。
// 页面的登录态要靠它才进得来——unlike 三平面，根路由上没有鉴权中间件，
// 没有它时 routes.go 的 viewerID 恒为 0，作者点自己文章的链接会看到 404。
// 传 nil 表示不解析会话（测试里的纯渲染场景）。
func (m *Module) MountFrontend(r chi.Router, optional func(http.Handler) http.Handler) {
	if m.store == nil {
		return
	}
	// 静态资源不套会话解析：每一个 CSS / 字体切片都要付一次会话查询是纯浪费，
	// 而静态资源本身与登录态无关。
	r.Mount(AssetsPath, m.registry.AssetsHandler())

	r.Group(func(g chi.Router) {
		if optional != nil {
			g.Use(optional)
		}
		m.frontend.Mount(g)
	})

	// 404 页同样要显示登录态，故也要过一遍会话解析。
	// 它是根路由的兜底处理器，只能挂在 r 上——chi 的 Group 与父路由共用路由树，
	// 在组内调 NotFound 不会生效。
	notFound := http.Handler(m.frontend.NotFoundHandler())
	if optional != nil {
		notFound = optional(notFound)
	}
	r.NotFound(notFound.ServeHTTP)
}

// Registry 返回主题注册表，供其他模块与测试取用。
func (m *Module) Registry() *Registry { return m.registry }

// RestoreBuiltin 把内置主题恢复成出厂状态（重新解压）并立即重新加载。
//
// 站长改坏了模板时这是回到原版的唯一入口 —— 没有它，就只能去翻发行包。
// 调用方必须先确认：站长对内置主题的改动会全部丢失。
func (m *Module) RestoreBuiltin() error {
	if m.builtin == nil {
		return errors.New("内置主题的文件系统不可用")
	}
	if err := RestoreBuiltin(m.root, m.builtin); err != nil {
		return err
	}
	return m.registry.loadBuiltinFromDisk()
}

// Renderer 返回页面渲染器。
//
// 导出给 account 模块：前台账户页与全站页面必须用同一套模板引擎、同一套回退规则与
// 同一份主题设置，否则账户页会在换主题时独自错位。它是本模块唯一对外开放的
// 渲染入口，模块之间仍然不引用彼此的内部实现。
func (m *Module) Renderer() *Renderer { return m.renderer }

// Frontend 返回前台处理器。
func (m *Module) Frontend() *Frontend { return m.frontend }

// From 取回主题模块；未装配时返回 nil。
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

// Permission 是本模块相关操作所需的权限。
const Permission = perm.ThemesManage

// Navigation 实现 app.NavigationProvider。
//
// 主题的**全部**操作都要求 themes:manage（含列表）——主题能执行任意模板逻辑并决定
// 整站外观，门槛与设置同级，故未持有时整项隐藏，点进去也只会得到 403。
func (m *Module) Navigation() app.Navigation {
	return app.Navigation{Items: []app.NavItem{{
		Key: "themes", Label: "主题", Path: "/themes", Icon: "palette",
		Group: app.NavGroupAppearance, Order: 10, Permission: Permission.String(),
		Keywords: "themes zhuti",
	}}}
}
