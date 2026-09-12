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

	"github.com/FeiBaiKin/lumo/internal/api"
	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/config"
	"github.com/FeiBaiKin/lumo/internal/console"
	"github.com/FeiBaiKin/lumo/internal/database"
	"github.com/FeiBaiKin/lumo/internal/httpx"
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
	db, err := database.Open(ctx, cfg.Database, *debugSQL)
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

	// 核心端点与全部功能模块，与 openapi 命令共用同一条注册路径（见 core.go）。
	if regErr := core.registerAPI(planes, application, logger); regErr != nil {
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
	if themes := theme.From(application); themes != nil {
		themes.MountFrontend(root)
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
