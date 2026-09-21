package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/FeiBaiKin/lumo/internal/account"
	"github.com/FeiBaiKin/lumo/internal/api"
	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/auth"
	"github.com/FeiBaiKin/lumo/internal/config"
	"github.com/FeiBaiKin/lumo/internal/console"
	"github.com/FeiBaiKin/lumo/internal/database"
	"github.com/FeiBaiKin/lumo/internal/httpx"
	"github.com/FeiBaiKin/lumo/internal/install"
	"github.com/FeiBaiKin/lumo/internal/logging"
	"github.com/FeiBaiKin/lumo/internal/media"
	"github.com/FeiBaiKin/lumo/internal/server"
	"github.com/FeiBaiKin/lumo/internal/theme"
	"github.com/FeiBaiKin/lumo/internal/version"
	"github.com/FeiBaiKin/lumo/internal/workdir"
)

// runServe 启动 HTTP 服务。
//
// 生命周期：配置 → 数据库 → 路由与认证栈 → 模块装配 → 迁移 → 播种与模块启动 → 对外服务。
// 迁移必须在模块启动之前：Start 是模块第一次被允许访问数据库的时机。
//
// 没有数据库连接信息时先走安装向导（见 install.go），装完带着写好的配置回到这里
// 重来一轮 —— 站长不必再手动重启进程。
func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	configPath := fs.String("config", "", "配置文件路径，默认按序尝试 ./config.yaml、./config.yml")
	addr := fs.String("addr", "", "监听地址，覆盖配置与环境变量")
	noMigrate := fs.Bool("no-migrate", false, "跳过启动时自动迁移")
	debugSQL := fs.Bool("debug-sql", false, "打印 SQL 语句（仅开发用）")
	if err := fs.Parse(args); err != nil {
		return err
	}

	for {
		cfg, err := config.Load(*configPath)
		if err != nil {
			return err
		}
		// 命令行参数优先级最高，覆盖配置文件与环境变量。
		if *addr != "" {
			cfg.Server.Addr = *addr
		}
		if *noMigrate {
			cfg.Database.AutoMigrate = false
		}

		logger, closeLog := newLogger(cfg)

		if cfg.Database.DSN == "" {
			installed, installErr := runInstallServe(cfg, logger)
			closeLog()
			if installErr != nil {
				return installErr
			}
			if !installed {
				return nil
			}
			continue
		}
		err = runSite(cfg, logger, *debugSQL)
		closeLog()
		return err
	}
}

// newLogger 构造日志器，并返回退出前要调用的关闭函数。
//
// 控制台那一路始终在：Docker 用户看 docker logs、systemd 用户看 journalctl，
// 断掉它等于断掉这两种部署方式的排障入口。文件那一路是后台日志页的数据来源，
// 由 log.file 控制，默认开。
//
// 日志文件打不开（目录只读、磁盘满）不让启动失败：能对外服务比能记日志更要紧，
// 退回只写控制台并把原因说清楚。
func newLogger(cfg config.Config) (logger *slog.Logger, closeLog func()) {
	opts := logging.Options{Level: cfg.Log.Level, Format: cfg.Log.Format}
	if !cfg.Log.File {
		return logging.New(os.Stdout, opts), func() {}
	}

	dir := filepath.Join(cfg.DataDir, workdir.LogsDirName)
	rotator, err := logging.NewRotatingFile(logging.RotateOptions{
		Dir:        dir,
		RetainDays: cfg.Log.RetainDays,
		MaxSizeMB:  cfg.Log.MaxSizeMB,
	})
	if err != nil {
		logger := logging.New(os.Stdout, opts)
		logger.Warn("日志文件不可用，本次只输出到控制台（后台日志页会是空的）",
			slog.String("dir", dir), slog.Any("error", err))
		return logger, func() {}
	}
	return logging.NewTee(os.Stdout, rotator, opts), func() { _ = rotator.Close() }
}

// runSite 是配置齐备时的正常启动路径。
func runSite(cfg config.Config, logger *slog.Logger, debugSQL bool) error {
	info := version.Get()
	logger.Info("启动 Lumo",
		slog.String("version", info.Version),
		slog.String("commit", info.Commit),
		slog.String("addr", cfg.Server.Addr),
		slog.Bool("consoleEmbedded", console.Built()),
	)
	if !console.Built() {
		logger.Warn("Console 前端未嵌入，后台界面不可用；执行 task console:build 后重新编译")
	}

	dataDir, err := workdir.Init(cfg.DataDir, logger)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 数据库是核心依赖：DSN 缺失或连接失败都应让启动失败，
	// 而不是带着半残状态对外服务。
	if dsnErr := cfg.RequireDSN(); dsnErr != nil {
		return dsnErr
	}
	db, err := database.Open(ctx, cfg.Database, debugSQL)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	logger.Info("数据库已连接", slog.String("dsn", cfg.RedactedDSN()))
	db.LogInfo(ctx, logger)

	// 认证栈。构造不触库，可以在迁移之前完成。
	core := newCoreStack(db, cfg.Server.SecureCookies, logger)

	if !cfg.Server.SecureCookies {
		logger.Warn("会话 Cookie 未启用 Secure，仅适用于本地 HTTP 开发；生产环境请设 LUMO_SECURE_COOKIES=true")
	}

	clientIP, err := httpx.NewClientIPResolver(cfg.Server.TrustedProxies)
	if err != nil {
		return err
	}
	if len(cfg.Server.TrustedProxies) == 0 {
		logger.Info("未配置可信代理，将忽略 X-Forwarded-For 并使用直连地址")
	}

	root, planes := server.NewRouter(&server.Options{
		Logger:        logger,
		Authenticator: core.Authenticator,
		ClientIP:      clientIP,
		Version:       info.Version,
		MaxBodySize:   cfg.Server.MaxBodySize,
		MaxUploadSize: cfg.Server.MaxUploadSize,
		// 本地存储的附件由核心以静态文件提供；换成 S3 后这个目录只是空着，无害。
		UploadsDir: filepath.Join(dataDir, media.UploadsDirName),
	})

	// 功能模块装配。此时只注册能力与接口，不访问数据库。
	// 必须先于下面的处理器构造：权限清单由模块声明，处理器要在请求时读得到。
	application := app.New(&app.Options{
		Config: cfg,
		DB:     db,
		Logger: logger,
		Router: planes,
	})
	// account 模块经此取用**同一批**认证实例（见 auth.Core 的说明）。
	// 这一行只在 serve 里：migrate 命令也走同一条注册链，但它不需要认证栈，
	// 而 account 在取不到 core 时会自行退化为「只装配迁移与设置声明」。
	application.Provide(auth.CoreKey, core)

	// 正常模式下安装向导已经关闭，但仍要注册那组端点：一是规范里得有它们
	// （Console 的类型从规范生成），二是给「已安装」一个明确的 409 而不是 404。
	installer := install.New(install.Options{
		Config:  cfg,
		DataDir: dataDir,
		Logger:  logger,
		Version: info.Version,
	})

	// 核心端点与全部功能模块，与 openapi 命令共用同一条注册路径（见 core.go）。
	if regErr := registerAPI(core, planes, application, installer, logger); regErr != nil {
		return regErr
	}

	if cfg.Database.AutoMigrate {
		if upErr := runMigrations(ctx, db, migrationSources(application), cfg.Database, logger); upErr != nil {
			return upErr
		}
	} else {
		logger.Warn("已跳过自动迁移，schema 可能落后于当前版本")
	}

	// 内置角色每次启动都以代码为准同步，确保升级后新增权限生效。
	if seedErr := core.Users.SeedRoles(ctx); seedErr != nil {
		return seedErr
	}
	if count, cErr := core.Users.CountUsers(ctx); cErr == nil && count == 0 {
		logger.Warn("尚无任何用户，请执行 lumo admin create-user 创建初始管理员")
	}

	// 迁移完成，模块可以开始播种数据、启动后台任务。
	if startErr := application.Start(ctx); startErr != nil {
		return startErr
	}

	// 访客前台必须最后挂载：它的兜底路由 /{slug}（独立页面）与 NotFound
	// 会吞掉根路径下的一切单段路径，排在 /console/、/uploads/ 与 SEO 文档之前
	// 就会把它们全部遮蔽。
	//
	// 前台不经三平面，故登录态要靠 auth 的 Optional 中间件注入；
	// account 必须先于 theme 挂载，否则它的固定路径（/login 等）会被
	// theme 的 /{slug} 当成独立页面吞掉。
	accounts := account.From(application)
	themes := theme.From(application)
	// 页眉账户菜单里的退出登录是一张表单，令牌由 account 的双提交签发。
	// 在这里接线而不是让 theme 去引 account：account 依赖 theme 渲染页面，
	// 反向引用就是一个导入环，而装配点本来就是解这种环的地方。
	if accounts != nil && themes != nil {
		themes.Renderer().UseFormCSRF(accounts.EnsureFormCSRF)
	}
	if accounts != nil {
		accounts.MountFrontend(root, core.Authenticator.Optional)
	}
	if themes != nil {
		themes.MountFrontend(root, core.Authenticator.Optional)
		logger.Info("访客前台已挂载", slog.String("theme", themes.Registry().ActiveName()))
	}
	// 停机顺序：srv.Run 收到信号后先停止接收新请求并排空 HTTP（最多
	// ShutdownTimeout），返回后才执行本 defer 的模块关闭；邮件队列等模块
	// 到这里才停止入队并做限时排空。deploy/docker-compose.yml 的
	// stop_grace_period 按「HTTP 排空 + 模块清理」的总预算取值。
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
		defer cancel()
		if err := application.Close(closeCtx); err != nil {
			logger.Error("关闭模块失败", slog.Any("error", err))
		}
	}()

	// 后台定期清理过期会话。
	go core.Service.StartSessionCleanup(ctx, time.Hour)

	registerHealth(root, db, &info, readinessProbeTimeout)
	logger.Info("API 规范与文档已就绪",
		slog.String("openapi", api.OpenAPIPath+".json"),
		slog.String("docs", api.DocsPath))

	srv := server.New(root, &cfg.Server, logger)
	if err := srv.Run(ctx); err != nil {
		return err
	}
	logger.Info("已退出")
	return nil
}

// readinessProbeTimeout 是 /readyz 数据库探测的独立期限（审查建议 2–3 秒，取中值）。
//
// 不能只依赖请求 context 或 HTTP WriteTimeout：数据库黑洞或连接池耗尽时
// PingContext 会一直等待，探针永远不返回 503，负载均衡无法摘流，
// 停机排空也会被拖长。超时按数据库不可用处理（503）。
const readinessProbeTimeout = 3 * time.Second

// pinger 是健康探测所需的数据库能力。抽象成接口便于测试注入阻塞探针，
// 验证超时路径；生产装配传 *database.DB。
type pinger interface {
	PingContext(ctx context.Context) error
}

// registerHealth 挂载健康检查端点。
//
// /healthz 只报进程存活，不触库；/readyz 额外探测数据库，用于负载均衡摘流判断。
// 它们是运维探针而非业务接口，故不进 OpenAPI 文档。
// readyTimeout 独立于请求 context，测试可传短期限验证超时行为。
func registerHealth(root chi.Router, db pinger, info *version.Info, readyTimeout time.Duration) {
	root.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteJSON(w, r, http.StatusOK, map[string]any{
			keyStatus:  "ok",
			keyVersion: info.Version,
		})
	})

	root.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		// 在请求 context 之上再叠一层独立期限：请求取消能提前结束探测，
		// 但探测本身绝不会超过 readyTimeout。
		ctx, cancel := context.WithTimeout(r.Context(), readyTimeout)
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			httpx.WriteProblem(w, r, &httpx.Problem{
				Status: http.StatusServiceUnavailable,
				Title:  "Service Unavailable",
				Detail: "数据库不可用",
			}, nil)
			return
		}
		httpx.WriteJSON(w, r, http.StatusOK, map[string]any{
			keyStatus:  "ready",
			keyVersion: info.Version,
		})
	})
}
