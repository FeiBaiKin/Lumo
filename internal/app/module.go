// Package app 定义模块契约与应用注册器。
//
// v1 不做运行时插件加载，但所有功能模块必须以「编译期插件」形态组织：
// 核心不得直接依赖模块内部实现（agent.md §3.2）。这样 v2 接入 WASM 或
// gRPC 宿主时，核心无需重写。
package app

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

// Module 是每个功能模块必须实现的最小接口。
type Module interface {
	// Name 返回模块的唯一标识，用于日志、迁移版本表名与依赖诊断。
	// 须为小写字母开头的短标识（字母、数字、下划线、连字符）。
	Name() string
	// Register 把模块的能力挂载到应用上。
	//
	// 只做装配，不得访问数据库：migrate 命令也会走同一条注册链，而那时业务表
	// 可能尚不存在。需要读写库的启动逻辑放到 Starter.Start。
	Register(app *App) error
}

// 以下为可选能力接口：模块按需实现，核心用类型断言检测。
//
// 采用「最小接口 + 可选能力」而非单一宽接口，是 Go 的惯例做法：
// 加新模块不必写一堆空方法，模块能力仍可被静态检查。

// Migrator 声明模块自带数据库迁移。
//
// 返回的文件系统根目录直接包含 goose SQL 文件；每个模块拥有独立的版本表，
// 迁移编号只需在模块内递增（见 internal/migrate）。
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

// RouteProvider 声明模块挂载 HTTP 接口。
//
// 接口注册通过三平面注册面进行，模块不能直接接触根路由，
// 以保证鉴权与前缀约定不被绕过（agent.md §6）。
type RouteProvider interface {
	Routes(r Router)
}

// Starter 声明模块需要在迁移完成之后、对外服务之前执行启动逻辑，
// 如播种内置数据、启动后台任务。ctx 在应用关闭时取消，后台 goroutine 应据此退出。
type Starter interface {
	Start(ctx context.Context) error
}

// Closer 是需要在应用关闭时释放资源的模块可选能力。
type Closer interface {
	Close(ctx context.Context) error
}

// Router 是暴露给模块的接口注册面（agent.md §6）。
//
// 四个注册面都是 huma.API：模块用 huma.Register 声明操作，请求校验、错误格式与
// OpenAPI 文档随之自动生成。前缀与鉴权中间件挂在各平面的分组上，模块无需也不能自行处理。
type Router interface {
	// Console 挂载 /api/v1/console/** ：已解析凭据、CSRF 校验、强制已认证；
	// 细粒度权限由各操作用 auth.RequirePermission 或 Principal.Allows 声明。
	Console() huma.API
	// ConsolePublic 挂载 Console 平面下**免认证**的端点，仅供登录一类极少数场景。
	ConsolePublic() huma.API
	// Public 挂载 /api/v1/public/** ：解析凭据但不强制，匿名可读已发布内容。
	Public() huma.API
	// Extension 挂载 /apis/{group}/{version}/{kind} ：强制已认证。
	Extension() huma.API
}

// SettingGroup 是一组设置项声明，走统一表单 Schema（agent.md §5）。
//
// 分组是设置的读写单位：Console 按分组渲染表单，接口按分组整体替换值。
type SettingGroup struct {
	// Name 是分组标识（DNS-1123），如 site、seo、mail。
	Name string
	// Label 是分组显示名。
	Label string
	// Description 是分组说明。
	Description string
	// Order 决定 Console 中的显示顺序，数值小者在前。
	Order int
	// Schema 是 JSON Schema 2020-12 子集 + x-widget 的声明，须为 object 类型并逐项声明 properties。
	Schema json.RawMessage
	// Defaults 是缺省值对象，须能通过 Schema 校验；有效值 = Defaults 被已保存值按顶层键覆盖。
	Defaults json.RawMessage
	// Public 列出可经 Public 平面读取的字段名（如站点标题）；其余字段仅 Console 可见。
	Public []string
	// Check 是 Schema 之外的 Go 侧校验（如时区名是否真实存在），入参为合并默认值后的有效值；可为 nil。
	Check func(values map[string]any) error
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
