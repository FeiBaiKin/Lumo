// Package update 提供从 GitHub Releases 就地升级的能力：检查新版本、
// 下载并核对校验和、备份当前二进制、替换并重启。
//
// 这是整个程序里唯一一处会改写「自己」的代码，故每一步都按「失败了也要留个能跑的站点」
// 来设计：校验和不对不装、新二进制跑不起来不装、替换之前先备份、替换失败原样退回。
//
// 不在容器里做就地升级：镜像是只读的，换掉的文件会在下次重建容器时消失，
// 站长会得到一次「升级成功但版本没变」的诡异经历。容器部署该换镜像标签。
package update

import (
	"context"

	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/version"
)

// Name 是模块名，也是 App.Provide 的键。
const Name = "update"

// Module 是在线升级模块。
//
// 无数据库表，故不实现 Migrator：升级状态是「这台机器此刻的事」，
// 重启之后本就该重新探测，落库只会留下一堆过期的判断。
// 也不声明权限串（见 Handler 的说明）与菜单项——升级界面在关于页里，
// 一年用不到几次的功能不该在侧边栏常驻一个入口。
type Module struct {
	service *Service
	handler *Handler
}

// New 构造模块。
func New() *Module { return &Module{} }

// Name 实现 app.Module。
func (m *Module) Name() string { return Name }

// Register 实现 app.Module：只做装配，不访问数据库，也不碰网络。
func (m *Module) Register(a *app.App) error {
	m.service = NewService(a.Config(), version.Get(), a.Logger())
	m.handler = NewHandler(m.service)
	a.Provide(Name, m)
	return nil
}

// Routes 实现 app.RouteProvider。
func (m *Module) Routes(r app.Router) {
	if m.handler == nil {
		return
	}
	m.handler.Register(r.Console())
}

// Permissions 实现 app.PermissionProvider：本模块不新增权限串。
func (m *Module) Permissions() []app.Permission { return nil }

// Start 实现 app.Starter：清理上次升级的残留，并起后台检查循环。
func (m *Module) Start(ctx context.Context) error {
	if m.service == nil {
		return nil
	}
	// Windows 上换不掉正在运行的文件，上一次升级会留下一个 lumo.exe.old。
	// 现在那个进程已经没了，删得掉。
	m.service.CleanupStale()
	m.service.StartAutoCheck(ctx)
	return nil
}

// Service 返回升级服务，供 cmd/lumo 接线停机与重启。
func (m *Module) Service() *Service { return m.service }

// From 从 App 取回本模块；未装配时返回 nil。
func From(a *app.App) *Module {
	value, ok := a.Lookup(Name)
	if !ok {
		return nil
	}
	module, _ := value.(*Module)
	return module
}
