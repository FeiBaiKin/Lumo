// Package account 提供前台账户体系：自助注册、邮箱验证、找回密码与账户页。
//
// 它不引入自己的用户表——注册用户落核心的 `users` 表，只授内置角色 `member`。
// 理由是复用：会话、登录限流、argon2 并发闸门、改密踢下线这些都已经是核心的现成实现，
// 另起一张访客表要把它们再写一遍，而且内容与评论的归属要各认一次。
//
// 本模块**不实现 app.RouteProvider**：它产出的是 HTML 表单而不是 REST 接口，
// 进不了三平面（与 theme 同理）。页面由 MountFrontend 直接挂在根路由上。
package account

import (
	"context"
	"embed"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/uptrace/bun"

	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/auth"
	"github.com/FeiBaiKin/lumo/internal/database"
	"github.com/FeiBaiKin/lumo/internal/mail"
	"github.com/FeiBaiKin/lumo/internal/media"
	"github.com/FeiBaiKin/lumo/internal/ratelimit"
	"github.com/FeiBaiKin/lumo/internal/settings"
	"github.com/FeiBaiKin/lumo/internal/theme"
)

// Name 是模块名，也是迁移版本表的后缀（goose_db_version_account）。
const Name = "account"

//go:embed migrations/*.sql
var migrationFS embed.FS

// 令牌有效期。做成代码常量而不是设置项：这两个数字站长改一次就再也不会动，
// 而每多一个设置项就多一处要解释、要翻译、要写文档的地方。
const (
	// VerifyEmailTTL 是邮箱验证链接的有效期。
	//
	// 给足 48 小时是因为验证邮件经常被投进垃圾箱，用户隔一天才想起来翻；
	// 而验证链接只证明邮箱可达，风险与重置链接不在一个量级。
	VerifyEmailTTL = 48 * time.Hour
	// ResetPasswordTTL 是密码重置链接的有效期。
	//
	// 与验证链接差两个数量级，因为重置链接能在不知道原密码的情况下改掉密码——
	// 邮件被别人翻出来时的破坏力大得多。
	ResetPasswordTTL = 2 * time.Hour
)

// tokenSweepInterval 是过期令牌清理循环的周期。
const tokenSweepInterval = time.Hour

// tokenRetention 是过期令牌的保留时长。
//
// 不立即删除：留一周才能回答「链接为什么失效」这类问题（是过期了，还是被用过了）。
const tokenRetention = 7 * 24 * time.Hour

// Module 是前台账户模块。
type Module struct {
	core     *auth.Core
	settings *settings.Service
	mail     *mail.Service
	// media 非 nil 时个人中心才提供头像与封面的上传：前台不另写一条上传管线，
	// 复用附件模块那套（类型嗅探、缩略图、本地或 S3 由存储设置决定）。
	media   *media.Service
	store   *Store
	tokens  *TokenStore
	limiter *Limiter
	csrf    *CSRF
	logger  *slog.Logger
	// db 供周期任务认领执行权。
	db bun.IDB
	// events 是派发给插件的动作总线。
	events app.Events
	// renderer 非 nil 表示可以渲染页面，路由才装得上。
	// migrate 命令走的是同一条注册链但没有认证栈，那时这里是 nil。
	renderer *theme.Renderer
}

// New 构造模块。
func New() *Module { return &Module{} }

// Name 实现 app.Module。
func (m *Module) Name() string { return Name }

// Register 实现 app.Module：只做装配，不访问数据库。
//
// 依赖经 app.Lookup 取：auth.Core（认证实例，必须与后台**同一批**）、
// theme（页面渲染器）、settings 与 mail（站点信息与发信能力）。
func (m *Module) Register(a *app.App) error {
	m.logger = a.Logger().With(slog.String("module", Name))
	m.events = a.Events()
	m.settings = settings.From(a)
	m.mail = mail.From(a)
	// media 排在 account 之前装配（见 cmd/lumo/modules.go），此时它的上传服务已登记。
	// 取不到就没有上传入口，页面照常渲染（见 renderAccount）。
	m.media = media.From(a)

	// 主题模块排在 account 之前装配（见 cmd/lumo/modules.go），此时它的 Renderer
	// 已经构造完毕。取不到就没有页面可渲染，只装配迁移与设置声明。
	if themer := theme.From(a); themer != nil {
		m.renderer = themer.Renderer()
	}

	if v, ok := a.Lookup(auth.CoreKey); ok {
		m.core, _ = v.(*auth.Core)
	}

	if db := a.DB(); db != nil {
		m.store = NewStore(db.DB)
		m.tokens = NewTokenStore(db.DB)
		limits := ratelimit.New(db.DB)
		if m.core != nil {
			limits = m.core.Limits
		}
		m.limiter = NewLimiter(limits, m.logger)
		m.db = db.DB
		m.csrf = NewCSRF(m.core != nil && m.core.Sessions.Secure)
	}

	a.Provide(Name, m)
	return nil
}

// Migrations 实现 app.Migrator。
func (m *Module) Migrations() fs.FS {
	sub, err := fs.Sub(migrationFS, "migrations")
	if err != nil {
		panic("account: 迁移目录不存在: " + err.Error())
	}
	return sub
}

// Settings 实现 app.SettingsProvider。
//
// 不依赖数据库与认证栈，故 migrate 那条路径上也照常声明分组——设置项的定义
// 属于编译期常量，与能不能渲染页面无关。
func (m *Module) Settings() []app.SettingGroup { return []app.SettingGroup{group()} }

// MountFrontend 把前台账户路由挂到根路由上。
//
// 必须在 theme 的 MountFrontend **之前**调用：theme 的 /{slug}（独立页面）
// 与 NotFound 会吞掉根路径下的一切单段路径，挂晚了 /login、/account 都会
// 被当成同名独立页面。
//
// optional 是 auth.Authenticator.Optional：前台不经三平面，登录态只能靠它注入。
func (m *Module) MountFrontend(r chi.Router, optional func(http.Handler) http.Handler) {
	if m.store == nil || m.renderer == nil || m.core == nil {
		return
	}
	r.Group(func(g chi.Router) {
		if optional != nil {
			g.Use(optional)
		}
		// 每个流程都是 GET 渲染表单 + POST 提交，表单走原生提交、不依赖 JavaScript：
		// 登录是基础功能，不该因为一段脚本没加载出来就不可用（定稿决策）。
		//
		// 成功即 302、失败即原地渲染（见各处理器）：成功后重定向才能避免刷新重复提交，
		// 失败原地渲染才能回填用户刚填的值与逐字段错误。
		g.Get(PathLogin, m.getLogin)
		g.Post(PathLogin, m.postLogin)
		g.Get(PathRegister, m.getRegister)
		g.Post(PathRegister, m.postRegister)
		g.Get(PathVerifyEmail, m.getVerifyEmail)
		g.Get(PathForgotPassword, m.getForgotPassword)
		g.Post(PathForgotPassword, m.postForgotPassword)
		g.Get(PathResetPassword, m.getResetPassword)
		g.Post(PathResetPassword, m.postResetPassword)
		g.Get(PathAccount, m.getAccount)
		g.Post(PathAccountProfile, m.postAccountProfile)
		g.Post(PathAccountPassword, m.postAccountPassword)
		g.Post(PathLogout, m.postLogout)
	})
}

// 前台账户路由。
//
// 这些是**被保留的固定路径**：它们会遮蔽同名的独立页面（已知限制）。
// 导出成常量是为了让这份保留清单只有一个出处，将来要改成动态判断也有地方可改。
const (
	PathLogin          = "/login"
	PathRegister       = "/register"
	PathVerifyEmail    = "/verify-email"
	PathForgotPassword = "/forgot-password"
	PathResetPassword  = "/reset-password"
	PathAccount        = "/account"
	// PathAccountProfile 是个人中心里改资料（昵称、简介、头像、封面）的提交地址。
	PathAccountProfile = "/account/profile"
	// PathAccountPassword 是账户页里改密码表单的提交地址。
	PathAccountPassword = "/account/password"
	// PathLogout 是登出表单的提交地址。
	PathLogout = "/logout"
)

// EnsureFormCSRF 为本次响应准备一枚表单 CSRF 令牌，供主题页眉里的退出登录表单使用。
//
// 导出给 theme：页眉出现在**每一个**前台页面上，而签发令牌要写 Cookie，
// 只有拿得到 ResponseWriter 的地方做得了。serve 在两个模块都装配完之后把它接上去
// （见 cmd/lumo/serve.go 与 theme.Renderer.UseFormCSRF），
// theme 因此不必反过来依赖 account——那会是一个导入环。
//
// 用 Ensure 而不是 Issue：换发新令牌会把访客在别的标签页里开着的表单作废。
func (m *Module) EnsureFormCSRF(w http.ResponseWriter, r *http.Request) string {
	if m.csrf == nil {
		return ""
	}
	return m.csrf.Ensure(w, r)
}

// ReservedPaths 返回本模块保留的固定路径，供文档与将来的保留字校验使用。
func ReservedPaths() []string {
	return []string{
		PathLogin, PathRegister, PathVerifyEmail,
		PathForgotPassword, PathResetPassword,
		PathAccount, PathAccountPassword, PathLogout,
	}
}

// Start 实现 app.Starter：开关开着但发不出信时告警，并启动令牌清理循环。
func (m *Module) Start(ctx context.Context) error {
	if m.store == nil {
		return nil
	}

	// 「开关为真但邮件未配置」这条校验放不进设置分组的 Check（它只拿到 values，
	// 读不到 mail 设置），故在这里兜一次：让站长在日志里看得见，
	// 而不是等访客反馈「注册页打不开」。
	if m.settings != nil {
		var cfg Settings
		if err := m.settings.Get(ctx, GroupAccount, &cfg); err == nil && cfg.AllowRegistration {
			if m.mail == nil || !m.mail.Enabled(ctx) {
				m.logger.Warn("已开放注册但邮件未配置，注册页会返回 404：请先在邮件设置里配好 SMTP")
			}
		}
	}

	go m.sweepTokens(ctx)
	return nil
}

// sweepTokens 定期清理过期已久的令牌，ctx 取消即退出。
//
// 照 auth.Service.StartCleanup 的写法：循环跑在自己的 goroutine 上，认领到本轮才执行，
// 出错只记日志不中断——清理失败不该让进程退出。
func (m *Module) sweepTokens(ctx context.Context) {
	ticker := time.NewTicker(tokenSweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			claimed, err := database.ClaimRun(ctx, m.db, "account.tokens.sweep", tokenSweepInterval)
			if err != nil {
				m.logger.Warn("清理过期账户令牌失败", slog.Any("error", err))
				continue
			}
			if !claimed {
				continue
			}
			removed, err := m.tokens.DeleteExpired(ctx, tokenRetention)
			if err != nil {
				m.logger.Warn("清理过期账户令牌失败", slog.Any("error", err))
				continue
			}
			if removed > 0 {
				m.logger.Info("已清理过期账户令牌", slog.Int64("count", removed))
			}
		}
	}
}

// Store 返回账户数据访问对象，供测试取用。
func (m *Module) Store() *Store { return m.store }

// Tokens 返回令牌存储，供测试取用。
func (m *Module) Tokens() *TokenStore { return m.tokens }

// From 取回账户模块；未装配时返回 nil。
func From(a *app.App) *Module {
	if a == nil {
		return nil
	}
	v, ok := a.Lookup(Name)
	if !ok {
		return nil
	}
	mod, _ := v.(*Module)
	return mod
}
