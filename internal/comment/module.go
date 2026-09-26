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
	"fmt"
	"io/fs"
	"log/slog"
	"strings"

	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/auth/perm"
	"github.com/FeiBaiKin/lumo/internal/hooks"
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
	notify := &notifier{mail: mail.From(a), logger: m.logger, siteURL: siteURLFunc(a)}
	if m.store != nil {
		notify.authorEmail = m.store.AuthorEmail
	}
	m.handler = NewHandler(m.store, configFunc(a, m.logger), NewSpamChecker(), notify, a.Events(), m.logger)
	a.Provide(Name, m)
	return nil
}

// From 取回评论模块；未装配时返回 nil。
func From(a *app.App) *Module {
	v, ok := a.Lookup(Name)
	if !ok {
		return nil
	}
	mod, _ := v.(*Module)
	return mod
}

// Moderate 改一条评论的状态，给插件这类内部调用方用：权限由调用方判定。
// 从别的状态改成通过时派发 comment.approved，与后台审核一致。
func (m *Module) Moderate(ctx context.Context, id int64, status Status) error {
	switch status {
	case StatusApproved, StatusPending, StatusSpam:
	default:
		return fmt.Errorf("评论状态只能是 approved、pending 或 spam，实际 %q", status)
	}
	if m.store == nil {
		return ErrNotFound
	}
	c, err := m.store.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := m.store.UpdateStatus(ctx, id, status); err != nil {
		return err
	}
	if status == StatusApproved && c.Status != StatusApproved {
		c.Status = status
		if post, postErr := m.store.PostRef(ctx, c.PostID); postErr == nil {
			m.handler.emit(ctx, hooks.CommentApproved, c, post)
		}
	}
	return nil
}

// Delete 删一条评论，给插件这类内部调用方用：权限由调用方判定。
func (m *Module) Delete(ctx context.Context, id int64) error {
	if m.store == nil {
		return ErrNotFound
	}
	return m.store.Delete(ctx, id)
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

// siteURLFunc 取站点对外地址（去掉末尾斜杠）；没配或读不到时为空串。
func siteURLFunc(a *app.App) func(ctx context.Context) string {
	return func(ctx context.Context) string {
		svc := settings.From(a)
		if svc == nil {
			return ""
		}
		var site settings.Site
		if err := svc.Get(ctx, settings.GroupSite, &site); err != nil {
			return ""
		}
		return strings.TrimSuffix(strings.TrimSpace(site.URL), "/")
	}
}

// ---------- 通知 ----------

// notifier 把评论事件转成邮件塞进 mail 模块的队列。
//
// 它跑在请求路径上，所以只做「组装 + 入队」，真正的发送由 mail 的后台协程负责。
type notifier struct {
	mail   *mail.Service
	logger *slog.Logger
	// authorEmail 取内容作者的邮箱；为 nil 时（没有库）收件地址留空就不发。
	authorEmail func(ctx context.Context, userID int64) (string, error)
	// siteURL 取站点对外地址，拼邮件里的后台链接。
	siteURL func(ctx context.Context) string
}

// CommentCreated 实现 Notifier。
func (n *notifier) CommentCreated(ctx context.Context, event *NotifyEvent) {
	if n.mail == nil || event.Comment == nil || event.Post == nil {
		return
	}
	cfg := event.Settings

	// 标为垃圾的不通知：垃圾评论量最大，通知它们等于给自己发垃圾。
	if event.Comment.Status != StatusSpam && cfg.NotifyNew {
		if to := n.recipient(ctx, cfg, event); to != "" {
			n.mail.Enqueue(ctx, &mail.Message{
				To:      []string{to},
				Subject: "有新评论：" + event.Post.Title,
				Text: "《" + event.Post.Title + "》收到一条来自 " + event.Comment.AuthorName + " 的评论。\n\n" +
					event.Comment.Content + "\n\n" + n.consoleHint(ctx),
			})
		}
	}

	if cfg.NotifyReply && event.Parent != nil {
		n.notifyParent(ctx, event)
	}
}

// recipient 决定新评论通知发给谁：填了收件地址就发给它，留空则发给这篇内容的作者。
// 作者在自己的内容下留言不通知他自己。
func (n *notifier) recipient(ctx context.Context, cfg Settings, event *NotifyEvent) string {
	if to := strings.TrimSpace(cfg.NotifyTo); to != "" {
		return to
	}
	if n.authorEmail == nil || event.Post.AuthorID == 0 {
		return ""
	}
	if uid := event.Comment.UserID; uid != nil && *uid == event.Post.AuthorID {
		return ""
	}
	email, err := n.authorEmail(ctx, event.Post.AuthorID)
	if err != nil {
		n.logger.Warn("取内容作者的邮箱失败，这条新评论不通知", slog.Any("error", err))
		return ""
	}
	return email
}

// consoleHint 是邮件末尾指向后台评论页的一句话。站点没配对外地址时不给链接：相对地址在邮件里点不开。
func (n *notifier) consoleHint(ctx context.Context) string {
	if n.siteURL != nil {
		if base := n.siteURL(ctx); base != "" {
			return "在后台查看：" + base + "/console/comments"
		}
	}
	return "到后台的「评论」里查看。"
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
