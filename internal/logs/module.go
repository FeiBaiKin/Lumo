package logs

import (
	"path/filepath"

	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/workdir"
)

// Name 是模块名。
const Name = "logs"

// Module 是日志模块。
//
// 无数据库表，故不实现 Migrator：运行日志落在文件里，不落库。
// 理由是数据库出问题的时候恰恰最需要日志——写库意味着 DB 一挂就什么也查不到，
// 而且日志是高频写入，会和业务查询抢连接池。
//
// 不声明权限：接口沿用 settings:manage（见 Handler 的说明），
// 不往权限清单里添一条让站长以为需要额外授权的东西。
type Module struct {
	reader  *Reader
	handler *Handler
}

// New 构造模块。
func New() *Module { return &Module{} }

// Name 实现 app.Module。
func (m *Module) Name() string { return Name }

// Register 实现 app.Module：只做装配，不访问数据库。
//
// 日志目录的算法必须与 cmd/lumo 里构造写入器时一致，否则页面会去一个空目录里找日志。
func (m *Module) Register(a *app.App) error {
	cfg := a.Config()
	m.reader = NewReader(filepath.Join(cfg.DataDir, workdir.LogsDirName))
	m.handler = NewHandler(m.reader, cfg.Log.File)
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
//
// 明写一个返回 nil 的实现而不是不实现这个接口，是为了让「为什么没有 logs:read」
// 这个问题在代码里有个落点。
func (m *Module) Permissions() []app.Permission { return nil }

// Reader 返回日志读取器，供将来可能的复用（如诊断信息导出）。
func (m *Module) Reader() *Reader { return m.reader }
