package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"

	"github.com/FeiBaiKin/lumo/internal/config"
	"github.com/FeiBaiKin/lumo/internal/database"
)

// App 是模块注册与共享依赖的中心。
//
// 模块只通过 App 获取依赖并注册能力，不直接互相引用——这是编译期插件形态
// 的关键约束，也是 v2 换成进程外插件时唯一需要替换的接缝。
type App struct {
	cfg    config.Config
	db     *database.DB
	logger *slog.Logger
	router Router

	modules []Module

	// 以下为模块注册期收集的能力。
	migrations  []NamedFS
	settings    []SettingGroup
	permissions []Permission
	hooks       []Hook
}

// NamedFS 是带来源模块名的迁移文件系统，便于错误定位。
type NamedFS struct {
	Module string
	FS     fs.FS
}

// Options 是构造 App 的依赖。
type Options struct {
	Config config.Config
	// DB 可为 nil：不需要数据库的命令（如仅渲染帮助）也应能构造 App。
	DB     *database.DB
	Logger *slog.Logger
	Router Router
}

// New 构造 App。
func New(opts *Options) *App {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &App{
		cfg:    opts.Config,
		db:     opts.DB,
		logger: logger,
		router: opts.Router,
	}
}

// Config 返回运行配置。
func (a *App) Config() config.Config { return a.cfg }

// DB 返回数据库客户端，可能为 nil。
func (a *App) DB() *database.DB { return a.db }

// Logger 返回应用日志器。
func (a *App) Logger() *slog.Logger { return a.logger }

// Router 返回三平面路由注册面，可能为 nil（非 HTTP 场景）。
func (a *App) Router() Router { return a.router }

// Modules 返回已注册模块的名称，顺序即注册顺序。
func (a *App) Modules() []string {
	names := make([]string, 0, len(a.modules))
	for _, m := range a.modules {
		names = append(names, m.Name())
	}
	return names
}

// Migrations 返回所有模块贡献的迁移文件系统。
func (a *App) Migrations() []NamedFS { return a.migrations }

// Settings 返回所有模块注册的设置分组。
func (a *App) Settings() []SettingGroup { return a.settings }

// Permissions 返回所有模块声明的权限串。
func (a *App) Permissions() []Permission { return a.permissions }

// Hooks 返回按优先级升序排列的钩子。
func (a *App) Hooks() []Hook {
	sorted := make([]Hook, len(a.hooks))
	copy(sorted, a.hooks)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Priority < sorted[j].Priority
	})
	return sorted
}

// Register 依次注册模块，并用类型断言收集可选能力。
//
// 任一模块注册失败即整体失败：核心基座不接受「部分可用」的启动状态。
func (a *App) Register(modules ...Module) error {
	registered := make(map[string]bool, len(a.modules))
	for _, m := range a.modules {
		registered[m.Name()] = true
	}

	for _, module := range modules {
		if module == nil {
			return errors.New("app: 不能注册 nil 模块")
		}
		name := module.Name()
		if name == "" {
			return errors.New("app: 模块名不能为空")
		}
		if registered[name] {
			return fmt.Errorf("app: 模块 %q 重复注册", name)
		}

		if err := module.Register(a); err != nil {
			return fmt.Errorf("注册模块 %q: %w", name, err)
		}

		a.collectCapabilities(module)
		a.modules = append(a.modules, module)
		registered[name] = true
		a.logger.Debug("模块已注册", slog.String("module", name))
	}
	return nil
}

// collectCapabilities 用类型断言检测并收集模块的可选能力。
func (a *App) collectCapabilities(module Module) {
	name := module.Name()

	if m, ok := module.(Migrator); ok {
		if fsys := m.Migrations(); fsys != nil {
			a.migrations = append(a.migrations, NamedFS{Module: name, FS: fsys})
		}
	}
	if m, ok := module.(SettingsProvider); ok {
		a.settings = append(a.settings, m.Settings()...)
	}
	if m, ok := module.(PermissionProvider); ok {
		a.permissions = append(a.permissions, m.Permissions()...)
	}
	if m, ok := module.(HookProvider); ok {
		a.hooks = append(a.hooks, m.Hooks()...)
	}
	if m, ok := module.(RouteProvider); ok && a.router != nil {
		m.Routes(a.router)
	}
}

// Closer 是需要在应用关闭时释放资源的模块可选能力。
type Closer interface {
	Close(ctx context.Context) error
}

// Close 逆序关闭实现了 Closer 的模块。
//
// 逆序是因为后注册的模块可能依赖先注册者；错误全部收集后一并返回，
// 不因单个模块失败而跳过其余模块的清理。
func (a *App) Close(ctx context.Context) error {
	var errs []error
	for i := len(a.modules) - 1; i >= 0; i-- {
		closer, ok := a.modules[i].(Closer)
		if !ok {
			continue
		}
		if err := closer.Close(ctx); err != nil {
			errs = append(errs, fmt.Errorf("关闭模块 %q: %w", a.modules[i].Name(), err))
		}
	}
	return errors.Join(errs...)
}
