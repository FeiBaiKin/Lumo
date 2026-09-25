// Package comment 提供评论与回复树。
//
// 评论是本项目里唯一由**匿名访客**写入的用户内容，安全模型因此与文章正文相反：
// 正文信任已认证用户、原样保留 HTML；评论一律先全文转义再做有限的富化（见 render.go）。
//
// 依赖 content 模块的 posts 表与 mail 模块的发信服务：modules.go 中两者都必须先于本模块注册。
package comment

import (
	"context"
	"embed"
	"io/fs"
	"log/slog"

	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/auth/perm"
	"github.com/FeiBaiKin/lumo/internal/mail"
	"github.com/FeiBaiKin/lumo/internal/settings"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Name 是模块名，也是迁移版本表的后缀（goose_db_version_comment）。
const Name = "comment"

// Module 是评论模块。
type Module struct {
	store   *Store
	handler *Handler
	logger  *slog.Logger
}

// New 构造模块。
func New() *Module { return &Module{} }

// Name 实现 app.Module。
func (m *Module) Name() string { return Name }

// Register 实现 app.Module：只做装配，不访问数据库。
func (m *Module) Register(a *app.App) error {
	m.logger = a.Logger()

	if db := a.DB(); db != nil {
		m.store = NewStore(db.DB)
	}
	m.handler = NewHandler(m.store, configFunc(a, m.logger), NewSpamChecker(),
		&notifier{mail: mail.From(a), logger: m.logger}, a.Events(), m.logger)
	return nil
}

// Migrations 实现 app.Migrator。
func (m *Module) Migrations() fs.FS {
	sub, err := fs.Sub(migrationFS, "migrations")
	if err != nil {
		panic("comment: 迁移目录不存在: " + err.Error())
	}
	return sub
}

// Settings 实现 app.SettingsProvider。
func (m *Module) Settings() []app.SettingGroup {
	return []app.SettingGroup{settingsGroup()}
}

// Permissions 实现 app.PermissionProvider。
func (m *Module) Permissions() []app.Permission {
	return []app.Permission{
		{Key: perm.CommentsManage.String(), Label: "管理自己内容的评论", Description: "审核、回复与删除自己文章或页面下的评论"},
		{Key: perm.CommentsManageAny.String(), Label: "管理任何评论", Description: "审核、回复与删除任意内容下的评论"},
	}
}

// Routes 实现 app.RouteProvider。
func (m *Module) Routes(r app.Router) {
	if m.store == nil {
		return
	}
	m.handler.Register(r.Console(), r.Public())
}

// configFunc 构造读取评论设置的函数；设置模块未装配或读取失败时退回缺省值。
func configFunc(a *app.App, logger *slog.Logger) ConfigFunc {
	return func(ctx context.Context) Settings {
		svc := settings.From(a)
		if svc == nil {
			return defaultSettings
		}
		var out Settings
		if err := svc.Get(ctx, GroupComment, &out); err != nil {
			if logger != nil {
				logger.Warn("读取评论设置失败，使用缺省值", slog.Any("error", err))
			}
			return defaultSettings
		}
		return out
	}
}

// ---------- 通知 ----------

// notifier 把评论事件转成邮件塞进 mail 模块的队列。
//
// 它跑在请求路径上，所以只做「组装 + 入队」，真正的发送由 mail 的后台协程负责。
type notifier struct {
	mail   *mail.Service
	logger *slog.Logger
}

// CommentCreated 实现 Notifier。
func (n *notifier) CommentCreated(ctx context.Context, event *NotifyEvent) {
	if n.mail == nil || event.Comment == nil || event.Post == nil {
		return
	}
	cfg := event.Settings

	// 标为垃圾的不通知：垃圾评论量最大，通知它们等于给自己发垃圾。
	if event.Comment.Status != StatusSpam && cfg.NotifyNew && cfg.NotifyTo != "" {
		n.mail.Enqueue(ctx, &mail.Message{
			To:      []string{cfg.NotifyTo},
			Subject: "有新评论：" + event.Post.Title,
			Text: "《" + event.Post.Title + "》收到一条来自 " + event.Comment.AuthorName + " 的评论。\n\n" +
				event.Comment.Content + "\n\n在后台查看：/console/comments",
		})
	}

	if cfg.NotifyReply && event.Parent != nil {
		n.notifyParent(ctx, event)
	}
}

// notifyParent 通知被回复者。
//
// 不给自己回自己发，也不给没留邮箱的评论发：访客不填邮箱是常态，
// 回落到站长的地址只会制造一堆无关邮件。
func (n *notifier) notifyParent(ctx context.Context, event *NotifyEvent) {
	parent := event.Parent
	if parent.AuthorEmail == "" || parent.AuthorEmail == event.Comment.AuthorEmail {
		return
	}
	n.mail.Enqueue(ctx, &mail.Message{
		To:      []string{parent.AuthorEmail},
		Subject: "有人在《" + event.Post.Title + "》回复了你",
		Text: event.Comment.AuthorName + " 回复了你在《" + event.Post.Title + "》下的评论。\n\n" +
			event.Comment.Content + "\n\n原评论：\n" + parent.Content,
	})
}

// Navigation 实现 app.NavigationProvider。
func (m *Module) Navigation() app.Navigation {
	return app.Navigation{Items: []app.NavItem{{
		Key: "comments", Label: "评论", Path: "/comments", Icon: "message-square",
		Group: app.NavGroupContent, Order: 50, Keywords: "comments pinglun",
	}}}
}
