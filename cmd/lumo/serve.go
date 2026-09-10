package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/auth"
	"github.com/FeiBaiKin/lumo/internal/config"
	"github.com/FeiBaiKin/lumo/internal/console"
	"github.com/FeiBaiKin/lumo/internal/database"
	"github.com/FeiBaiKin/lumo/internal/httpx"
	"github.com/FeiBaiKin/lumo/internal/logging"
	"github.com/FeiBaiKin/lumo/internal/migrate"
	"github.com/FeiBaiKin/lumo/internal/server"
	"github.com/FeiBaiKin/lumo/internal/version"
	"github.com/FeiBaiKin/lumo/internal/workdir"
	"github.com/FeiBaiKin/lumo/migrations"
)

// runServe 启动 HTTP 服务。
func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	configPath := fs.String("config", "", "配置文件路径，默认按序尝试 ./config.yaml、./config.yml")
	addr := fs.String("addr", "", "监听地址，覆盖配置与环境变量")
	noMigrate := fs.Bool("no-migrate", false, "跳过启动时自动迁移")
	debugSQL := fs.Bool("debug-sql", false, "打印 SQL 语句（仅开发用）")
	if err := fs.Parse(args); err != nil {
		return err
	}

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

	logger := logging.New(os.Stdout, logging.Options{
		Level:  cfg.Log.Level,
		Format: cfg.Log.Format,
	})

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

	if _, err = workdir.Init(cfg.DataDir, logger); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 数据库是核心依赖：DSN 缺失或连接失败都应让启动失败，
	// 而不是带着半残状态对外服务。
	if dsnErr := cfg.RequireDSN(); dsnErr != nil {
		return dsnErr
	}
	db, err := database.Open(ctx, cfg.Database, *debugSQL)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	logger.Info("数据库已连接", slog.String("dsn", cfg.RedactedDSN()))
	db.LogInfo(ctx, logger)

	if cfg.Database.AutoMigrate {
		migrator, migErr := migrate.New(db.SQLDB(), migrations.FS, logger)
		if migErr != nil {
			return migErr
		}
		if upErr := migrator.Up(ctx); upErr != nil {
			return upErr
		}
	} else {
		logger.Warn("已跳过自动迁移，schema 可能落后于当前版本")
	}

	// 认证栈。内置角色每次启动都以代码为准同步，确保升级后新增权限生效。
	users := auth.NewStore(db.DB)
	if seedErr := users.SeedRoles(ctx); seedErr != nil {
		return seedErr
	}
	sessions := auth.NewSessionStore(db.DB, cfg.Server.SecureCookies)
	tokens := auth.NewTokenStore(db.DB)
	authService := auth.NewService(users, sessions, tokens, logger)
	authenticator := auth.NewAuthenticator(users, sessions, tokens, logger)

	if !cfg.Server.SecureCookies {
		logger.Warn("会话 Cookie 未启用 Secure，仅适用于本地 HTTP 开发；生产环境请设 LUMO_SECURE_COOKIES=true")
	}
	if count, cErr := users.CountUsers(ctx); cErr == nil && count == 0 {
		logger.Warn("尚无任何用户，请执行 lumo admin create-user 创建初始管理员")
	}

	clientIP, err := httpx.NewClientIPResolver(cfg.Server.TrustedProxies)
	if err != nil {
		return err
	}
	if len(cfg.Server.TrustedProxies) == 0 {
		logger.Info("未配置可信代理，将忽略 X-Forwarded-For 并使用直连地址")
	}

	// 认证端点的处理器。
	authHandler := auth.NewHandler(authService, sessions, tokens, logger)

	root, planes := server.NewRouter(&server.Options{
		Logger:        logger,
		Authenticator: authenticator,
		ClientIP:      clientIP,
		// 登录是 Console 平面下唯一无需认证的端点。
		PublicConsoleRoutes: func(r chi.Router) {
			authHandler.RegisterPublic(r)
		},
	})

	// Console 平面默认要求已认证，此处只需注册路由本身。
	planes.Console(func(r chi.Router) {
		authHandler.RegisterAuthenticated(r)
	})

	// 后台定期清理过期会话。
	go authService.StartSessionCleanup(ctx, time.Hour)

	application := app.New(&app.Options{
		Config: cfg,
		DB:     db,
		Logger: logger,
		Router: planes,
	})
	// 阶段 2 的认证能力直接由核心提供；功能模块从阶段 3 起在此注册。
	if regErr := application.Register(); regErr != nil {
		return regErr
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
		defer cancel()
		if err := application.Close(closeCtx); err != nil {
			logger.Error("关闭模块失败", slog.Any("error", err))
		}
	}()

	registerHealth(root, db, &info)

	srv := server.New(root, &cfg.Server, logger)
	if err := srv.Run(ctx); err != nil {
		return err
	}
	logger.Info("已退出")
	return nil
}

// registerHealth 挂载健康检查端点。
//
// /healthz 只报进程存活；/readyz 额外探测数据库，用于负载均衡摘流判断。
func registerHealth(root chi.Router, db *database.DB, info *version.Info) {
	root.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteJSON(w, r, http.StatusOK, map[string]any{
			keyStatus:  "ok",
			keyVersion: info.Version,
		})
	})

	root.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := db.PingContext(r.Context()); err != nil {
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
