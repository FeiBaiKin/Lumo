package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/auth"
	"github.com/FeiBaiKin/lumo/internal/config"
	"github.com/FeiBaiKin/lumo/internal/console"
	"github.com/FeiBaiKin/lumo/internal/database"
	"github.com/FeiBaiKin/lumo/internal/httpx"
	"github.com/FeiBaiKin/lumo/internal/install"
	"github.com/FeiBaiKin/lumo/internal/server"
	"github.com/FeiBaiKin/lumo/internal/settings"
	"github.com/FeiBaiKin/lumo/internal/version"
	"github.com/FeiBaiKin/lumo/internal/workdir"
)

// runInstallServe 在还没有数据库连接信息时提供服务：只挂安装向导与 Console 静态资源。
//
// 返回 true 表示安装已经完成，调用方应重新加载配置再来一轮（那时就是正常站点）；
// 返回 false 表示进程收到退出信号，本次启动就此结束。
func runInstallServe(cfg config.Config, logger *slog.Logger) (bool, error) {
	info := version.Get()

	dataDir, err := workdir.Init(cfg.DataDir, logger)
	if err != nil {
		return false, err
	}

	svc := install.New(install.Options{
		Config:  cfg,
		DataDir: dataDir,
		Logger:  logger,
		Version: info.Version,
		Provision: func(ctx context.Context, db *database.DB, c config.Config, in install.Params) (install.ProvisionResult, error) {
			return provisionForInstall(ctx, db, c, in, logger)
		},
	})

	root, planes := server.NewRouter(&server.Options{
		Logger:  logger,
		Version: info.Version,
		// 向导模式下站点入口（/ 与 /console）都指向向导本身；后台静态资源照常提供。
		InstallRedirect: console.MountPath + "install",
	})
	install.NewHandler(svc, logger).Register(planes.API())
	registerInstallHealth(root, &info)

	// 这条日志是站长第一次运行程序时唯一的线索，地址要给全。
	logger.Warn("尚未配置数据库，已进入安装向导模式",
		slog.String("向导", wizardURL(cfg.Server.Addr)),
		slog.String("工作目录", dataDir),
		slog.String("也可以", "直接设 LUMO_DATABASE_DSN 环境变量跳过向导"))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 安装成功后让 Run 返回：取消 ctx 触发优雅关闭，在途的响应会先写完再退出。
	go func() {
		select {
		case <-svc.Done():
			logger.Info("安装完成，正在以正常模式重新启动")
			stop()
		case <-ctx.Done():
		}
	}()

	srv := server.New(root, &cfg.Server, logger)
	if err := srv.Run(ctx); err != nil {
		return false, err
	}
	return svc.Installed(), nil
}

// registerInstallHealth 挂安装模式下的健康检查。
//
// 不复用 registerHealth：它要求一个可用的数据库句柄，而这里根本没有。
// /readyz 一律 503 —— 站点还没装好，负载均衡不该把流量放进来。
func registerInstallHealth(root chi.Router, info *version.Info) {
	root.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteJSON(w, r, http.StatusOK, map[string]any{
			keyStatus:  "ok",
			keyVersion: info.Version,
		})
	})
	root.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteProblem(w, r, &httpx.Problem{
			Status: http.StatusServiceUnavailable,
			Title:  "Service Unavailable",
			Detail: "站点尚未安装",
		}, nil)
	})
}

// wizardURL 把监听地址翻译成站长能直接点开的向导地址。
//
// 监听在 0.0.0.0 或空主机上时给 127.0.0.1：日志是给坐在那台机器前的人看的。
func wizardURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://" + addr + console.MountPath + "install"
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return fmt.Sprintf("http://%s:%s%sinstall", host, port, console.MountPath)
}

// provisionForInstall 在目标库上完成安装的实质工作。
//
// 走的是与 serve 完全相同的模块注册链与迁移来源，所以「走向导装出来的库」与
// 「先配好 DSN 再启动」没有任何区别 —— 站长日后照着文档手动装也能得到同一个结果。
//
// 本函数返回的错误一律标记成「站长可自行修正」：安装阶段失败的原因几乎都是
// 数据库权限、口令或参数，原样回显比一句「服务器内部错误」有用得多。
func provisionForInstall(
	ctx context.Context,
	db *database.DB,
	cfg config.Config,
	in install.Params,
	logger *slog.Logger,
) (install.ProvisionResult, error) {
	var result install.ProvisionResult

	application := app.New(&app.Options{Config: cfg, DB: db, Logger: logger})
	if err := application.Register(modules()...); err != nil {
		return result, err
	}

	if err := migrateForInstall(ctx, db, cfg, application, logger); err != nil {
		return result, install.WrapBadRequest(err)
	}

	// 内置角色必须先落库，Bootstrap 授予 super-admin 时才找得到这个角色。
	core := auth.NewCore(db.DB, cfg.Server.SecureCookies, logger)
	if err := core.Users.SeedRoles(ctx); err != nil {
		return result, install.WrapBadRequest(fmt.Errorf("创建内置角色: %w", err))
	}
	user, err := core.Service.Bootstrap(ctx, &auth.CreateUserParams{
		Username:    in.Admin.Username,
		Email:       in.Admin.Email,
		Password:    in.Admin.Password,
		DisplayName: in.Admin.Username,
	})
	if err != nil {
		return result, install.WrapBadRequest(err)
	}
	// Bootstrap 在库里已有用户时返回 nil：那是「接管已有站点」，不是失败。
	result.AdminCreated = user != nil

	// 站点设置要等模块 Start 之后才写得进去：设置分组是那时登记的。
	if err := application.Start(ctx); err != nil {
		return result, install.WrapBadRequest(err)
	}
	if err := applySiteSettings(ctx, application, in.Site); err != nil {
		// 站点名写不进去不该让整次安装失败：表与管理员都已建好，站长可在后台补填。
		logger.Warn("写入站点信息失败，可在后台设置里补填", slog.Any("error", err))
	}

	// 这里的模块实例只为安装而生，随即关掉：接下来 serve 会带着新配置重新装配一套。
	closeCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := application.Close(closeCtx); err != nil {
		logger.Warn("关闭安装期模块失败", slog.Any("error", err))
	}
	return result, nil
}

// migrateForInstall 在安装连上跑一遍迁移。
//
// 与 migrate 命令共用 runMigrations 与迁移来源；区别只是这里的库多半是空的。
func migrateForInstall(ctx context.Context, db *database.DB, cfg config.Config, application *app.App, logger *slog.Logger) error {
	lockDB, err := openMigrationLockDB(ctx, cfg.Database)
	if err != nil {
		return err
	}
	defer func() { _ = lockDB.Close() }()

	return runMigrations(ctx, db, migrationSources(application), cfg.Database, logger)
}

// applySiteSettings 把向导里填的站点信息写进设置。
func applySiteSettings(ctx context.Context, application *app.App, site install.SiteInput) error {
	values := map[string]any{}
	if title := strings.TrimSpace(site.Title); title != "" {
		values["title"] = title
	}
	if raw := strings.TrimSpace(site.URL); raw != "" {
		values["url"] = raw
	}
	if len(values) == 0 {
		return nil
	}

	service := settings.From(application)
	if service == nil {
		return errors.New("设置模块未装配")
	}
	return service.Update(ctx, settings.GroupSite, values)
}
