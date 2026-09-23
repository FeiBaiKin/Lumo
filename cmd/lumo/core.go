package main

import (
	"log/slog"

	"github.com/FeiBaiKin/lumo/internal/api"
	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/auth"
	"github.com/FeiBaiKin/lumo/internal/console"
	"github.com/FeiBaiKin/lumo/internal/database"
	"github.com/FeiBaiKin/lumo/internal/install"
	"github.com/FeiBaiKin/lumo/internal/version"
)

// coreStack 是核心自带的认证与管理能力。
//
// 定义本身已挪到 internal/auth（auth.Core），这里只留一个别名：
// 功能模块要能经 app.Lookup 取到**同一批**认证实例（见 auth.Core 的说明），
// 而登记名与类型必须同处一地才不会走散。
type coreStack = auth.Core

// newCoreStack 构造认证栈。只做装配、不访问数据库，可在迁移之前调用。
func newCoreStack(db *database.DB, secureCookies bool, logger *slog.Logger) *coreStack {
	return auth.NewCore(db.DB, secureCookies, logger)
}

// registerAPI 挂上核心端点，再装配全部功能模块。
//
// 顺序有讲究：核心的处理器要先于模块构造，而权限清单必须延迟到请求时才读——
// 声明权限的模块要到本函数最后一行才注册。
//
// 是自由函数而非 coreStack 的方法：coreStack 现在只是 auth.Core 的别名，
// Go 不允许为别名定义方法（方法必须与被别名的类型同包）。
func registerAPI(c *auth.Core, planes *api.Planes, application *app.App, installer *install.Service, logger *slog.Logger) error {
	// 认证端点：登录走免认证注册面，其余走强制认证注册面。
	auth.NewHandler(c.Service, c.Sessions, c.Tokens, logger).
		Register(planes.ConsolePublic(), planes.Console())
	auth.NewAdminHandler(c.Users, c.Service, func() []auth.PermissionInfo {
		// 核心自身引入的权限与各模块声明的合并：前者没有对应的功能模块。
		declared := append(app.CorePermissions(), application.Permissions()...)
		out := make([]auth.PermissionInfo, 0, len(declared))
		for _, p := range declared {
			out = append(out, auth.PermissionInfo{Key: p.Key, Label: p.Label, Description: p.Description})
		}
		return out
	}).Register(planes.Console())

	// 侧边栏菜单。挂在核心而不是某个模块下：它汇总的是全部模块的声明，
	// 而模块清单要到下一行才装配完，故这里传的是取值函数而不是一份快照。
	console.NewNavHandler(application.Navigation).Register(planes.Console())
	console.NewBuildHandler(version.Get()).Register(planes.Console())

	// 安装向导的端点挂在根 API 上（无前缀、无鉴权中间件）：它要在数据库可用之前就能应答。
	// 正常模式下这几个端点同样注册，只是一律返回「站点已完成安装」—— 规范的稳定性优先于
	// 端点的存在性，Console 的 TS 类型正是从这份规范生成的。
	if installer != nil {
		install.NewHandler(installer, logger).Register(planes.API())
	}

	return application.Register(modules()...)
}
