// Package content 提供文章与独立页面。
//
// 两者共用一张 posts 表，以 type 区分；发布、修订、定时发布与可见性逻辑只写一份，
// 接口按类型分别挂在 /posts 与 /pages 下，权限分别走 posts:* 与 pages:*。
// 依赖 taxonomy 模块（分类与标签关联），装配时必须排在它之后。
package content

import (
	"context"
	"embed"
	"io/fs"
	"log/slog"
	"time"

	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/auth"
	"github.com/FeiBaiKin/lumo/internal/auth/perm"
	"github.com/FeiBaiKin/lumo/internal/settings"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Name 是模块名，也是迁移版本表的后缀。
const Name = "content"

// scheduleInterval 是定时发布扫描的间隔。
const scheduleInterval = 30 * time.Second

// Module 是内容模块。
type Module struct {
	store   *Store
	logger  *slog.Logger
	slugify Slugger
}

// New 构造模块。
func New() *Module {
	return &Module{}
}

// Name 实现 app.Module。
func (m *Module) Name() string { return Name }

// Register 实现 app.Module：只做装配，不访问数据库。
//
// 若 settings 模块先于本模块装配，slug 生成策略跟随站点设置；否则退回保留中文的缺省策略。
func (m *Module) Register(a *app.App) error {
	m.logger = a.Logger()
	if db := a.DB(); db != nil {
		m.store = NewStore(db.DB, auth.NewStore(db.DB))
	}
	if svc := settings.From(a); svc != nil {
		m.slugify = svc.Slug
	}
	return nil
}

// Migrations 实现 app.Migrator。
func (m *Module) Migrations() fs.FS {
	sub, err := fs.Sub(migrationFS, "migrations")
	if err != nil {
		panic("content: 迁移目录不存在: " + err.Error())
	}
	return sub
}

// Routes 实现 app.RouteProvider。
func (m *Module) Routes(r app.Router) {
	if m.store == nil {
		return
	}
	NewHandler(m.store, m.slugify).Register(r.Console(), r.Public())
}

// Permissions 实现 app.PermissionProvider。
func (m *Module) Permissions() []app.Permission {
	return []app.Permission{
		{Key: perm.PostsWrite.String(), Label: "撰写文章", Description: "创建与修改自己的文章"},
		{Key: perm.PostsWriteAny.String(), Label: "修改任何文章", Description: "修改他人的文章"},
		{Key: perm.PostsPublish.String(), Label: "发布文章", Description: "发布、定时发布与撤回文章"},
		{Key: perm.PostsDeleteAny.String(), Label: "删除任何文章", Description: "删除他人的文章"},
		{Key: perm.PagesWrite.String(), Label: "撰写页面", Description: "创建与修改自己的页面"},
		{Key: perm.PagesWriteAny.String(), Label: "修改任何页面", Description: "修改他人的页面"},
		{Key: perm.PagesPublish.String(), Label: "发布页面", Description: "发布、定时发布与撤回页面"},
		{Key: perm.PagesDeleteAny.String(), Label: "删除任何页面", Description: "删除他人的页面"},
	}
}

// Start 实现 app.Starter：启动定时发布扫描，ctx 取消时退出。
//
// 扫描是幂等的单条 UPDATE，多实例部署时各实例都跑也不会重复发布。
func (m *Module) Start(ctx context.Context) error {
	if m.store == nil {
		return nil
	}
	go func() {
		ticker := time.NewTicker(scheduleInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := m.PublishDue(ctx); err != nil && ctx.Err() == nil && m.logger != nil {
					m.logger.Warn("推进定时发布失败", slog.Any("error", err))
				}
			}
		}
	}()
	return nil
}

// PublishDue 立即执行一次定时发布扫描，返回推进条数；供后台任务与测试调用。
func (m *Module) PublishDue(ctx context.Context) (int64, error) {
	count, err := m.store.PublishDue(ctx)
	if err != nil {
		return 0, err
	}
	if count > 0 && m.logger != nil {
		m.logger.Info("定时内容已发布", slog.Int64("count", count))
	}
	return count, nil
}

// Navigation 实现 app.NavigationProvider。
func (m *Module) Navigation() app.Navigation {
	return app.Navigation{Items: []app.NavItem{
		{
			Key: "posts", Label: "文章", Path: "/posts", Icon: "book-open",
			Group: app.NavGroupContent, Order: 10, Keywords: "posts wenzhang",
		},
		{
			Key: "pages", Label: "页面", Path: "/pages", Icon: "file-text",
			Group: app.NavGroupContent, Order: 20, Keywords: "pages yemian",
		},
	}}
}
