// Package app 定义模块契约与应用注册器。
//
// v1 不做运行时插件加载，但所有功能模块必须以「编译期插件」形态组织：
// 核心不得直接依赖模块内部实现（agent.md §3.2）。这样 v2 接入 WASM 或
// gRPC 宿主时，核心无需重写。
package app

import (
	"io/fs"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// Module 是每个功能模块必须实现的最小接口。
type Module interface {
	// Name 返回模块的唯一标识，用于日志与依赖诊断。
	Name() string
	// Register 把模块的能力挂载到应用上。
	Register(app *App) error
}

// 以下为可选能力接口：模块按需实现，核心用类型断言检测。
//
// 采用「最小接口 + 可选能力」而非单一宽接口，是 Go 的惯例做法：
// 加新模块不必写一堆空方法，模块能力仍可被静态检查。

// Migrator 声明模块自带数据库迁移。
type Migrator interface {
	Migrations() fs.FS
}

// SettingsProvider 声明模块注册设置项分组，走统一表单 Schema（agent.md §5）。
type SettingsProvider interface {
	Settings() []SettingGroup
}

// PermissionProvider 声明模块引入的权限串（agent.md §7.2）。
type PermissionProvider interface {
	Permissions() []Permission
}

// HookProvider 声明模块注册的钩子。
type HookProvider interface {
	Hooks() []Hook
}

// RouteProvider 声明模块挂载 HTTP 路由。
//
// 路由注册通过三平面注册器进行，模块不能直接接触根路由，
// 以保证鉴权与前缀约定不被绕过（agent.md §6）。
type RouteProvider interface {
	Routes(r Router)
}

// Router 是暴露给模块的路由注册面。
//
// 三个平面各自对应 agent.md §6 的鉴权策略，模块只能在这三者之内挂载路由。
type Router interface {
	// Console 挂载 /api/v1/console/** ，需会话或 PAT + 权限校验。
	Console(fn func(r chi.Router))
	// Public 挂载 /api/v1/public/** ，匿名只读。
	Public(fn func(r chi.Router))
	// Extension 挂载 /apis/{group}/{version}/{kind} 。
	Extension(fn func(r chi.Router))
}

// SettingGroup 是一组设置项声明，字段随阶段 3 的表单 Schema 落地后细化。
type SettingGroup struct {
	// Name 是分组标识。
	Name string
	// Label 是分组显示名，走 i18n key。
	Label string
	// Schema 是 JSON Schema 子集 + x-widget 的声明（agent.md §5）。
	Schema []byte
}

// Permission 是一条权限声明，格式为 <资源>:<动作>（agent.md §7.2）。
type Permission struct {
	// Key 是权限串，如 posts:write。
	Key string
	// Label 是显示名，走 i18n key。
	Label string
	// Description 是权限说明。
	Description string
}

// HookFunc 是钩子的处理函数，签名随阶段 3 的具体钩子点细化。
type HookFunc func(http.Handler) http.Handler

// Hook 是一条钩子声明。
type Hook struct {
	// Name 是钩子点标识。
	Name string
	// Priority 决定执行顺序，数值小者先执行。
	Priority int
	// Handler 是钩子实现。
	Handler HookFunc
}
