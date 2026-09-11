// Package search 提供全文搜索（agent.md §2）。
//
// 切词在 Go 侧完成（中日韩按二元组，见 tokenize.go），结果存进独立的 post_search 表，
// 检索走 GIN 索引——全程不依赖任何 PostgreSQL 扩展，装机即用。
//
// 索引由后台对账维护：按 posts.updated_at 找出落后的行重建，不在内容模块的写路径上挂钩子。
// 对外只暴露 Searcher 接口，将来换成 meilisearch 一类外部引擎时调用方不动。
package search

import (
	"context"
	"embed"
	"io/fs"
	"log/slog"

	"github.com/FeiBaiKin/lumo/internal/app"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Name 是模块名，也是迁移版本表的后缀（goose_db_version_search）。
const Name = "search"

// Module 是搜索模块。
type Module struct {
	service *Service
	logger  *slog.Logger
}

// New 构造模块。
func New() *Module { return &Module{} }

// Name 实现 app.Module。
func (m *Module) Name() string { return Name }

// Register 实现 app.Module：只做装配，不访问数据库。
func (m *Module) Register(a *app.App) error {
	m.logger = a.Logger().With(slog.String("module", Name))
	if db := a.DB(); db != nil {
		m.service = NewService(NewStore(db.DB), m.logger)
		// 主题前台按名字取用；未装配本模块时前台自动退回标题模糊匹配。
		a.Provide(Name, m.service)
	}
	return nil
}

// Migrations 实现 app.Migrator。
func (m *Module) Migrations() fs.FS {
	sub, err := fs.Sub(migrationFS, "migrations")
	if err != nil {
		panic("search: 迁移目录不存在: " + err.Error())
	}
	return sub
}

// Routes 实现 app.RouteProvider。
func (m *Module) Routes(r app.Router) {
	if m.service == nil {
		return
	}
	NewHandler(m.service).Register(r.Console(), r.Public())
}

// Start 实现 app.Starter：启动索引对账，ctx 取消时退出。
func (m *Module) Start(ctx context.Context) error {
	if m.service == nil {
		return nil
	}
	go m.service.Run(ctx)
	return nil
}

// From 取回已装配的搜索服务；未装配时返回 nil。
func From(a *app.App) *Service {
	v, ok := a.Lookup(Name)
	if !ok {
		return nil
	}
	service, _ := v.(*Service)
	return service
}
