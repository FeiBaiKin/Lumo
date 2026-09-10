// Command lumo 是 Lumo CMS 的唯一入口。
//
// 用法：lumo serve | migrate | admin | version
//
// 阶段 0 只搭起可编译、可运行的骨架：serve 能起 HTTP 服务并挂载 Console，
// migrate 与 admin 是显式的未实现占位，由阶段 1、2 落地。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/FeiBaiKin/lumo/internal/console"
	"github.com/FeiBaiKin/lumo/internal/version"
)

const usage = `Lumo — 用 Go 编写的现代化开源 CMS

用法：
  lumo <命令> [参数]

命令：
  serve                  启动 HTTP 服务
  migrate                执行数据库迁移（阶段 1 实现）
  admin reset-password   重置管理员密码（阶段 2 实现）
  version                输出版本信息

用 "lumo <命令> -h" 查看具体命令的参数。
`

func main() {
	// 退出码集中在 main 处理，便于 run 内部统一用 error 返回。
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintf(os.Stderr, "lumo: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return errors.New("缺少命令")
	}

	switch args[0] {
	case "serve":
		return runServe(args[1:])
	case "migrate":
		return runMigrate(args[1:])
	case "admin":
		return runAdmin(args[1:])
	case "version":
		return runVersion(args[1:])
	case "-h", "--help", "help":
		fmt.Print(usage)
		return nil
	default:
		fmt.Fprint(os.Stderr, usage)
		return fmt.Errorf("未知命令 %q", args[0])
	}
}

// runVersion 输出版本信息，支持 -json 便于脚本消费。
func runVersion(args []string) error {
	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "以 JSON 格式输出")
	if err := fs.Parse(args); err != nil {
		return err
	}

	info := version.Get()
	if *asJSON {
		fmt.Printf("{\"version\":%q,\"commit\":%q,\"date\":%q,\"goVersion\":%q,\"platform\":%q}\n",
			info.Version, info.Commit, info.Date, info.GoVersion, info.Platform)
		return nil
	}
	fmt.Println(info)
	return nil
}

// runMigrate 是迁移命令占位。迁移框架（goose + embed SQL）在阶段 1 落地。
func runMigrate(args []string) error {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	return errors.New("migrate 尚未实现（阶段 1 · 后端核心基座）")
}

// runAdmin 是管理命令占位。用户体系与密码哈希在阶段 2 落地。
func runAdmin(args []string) error {
	fs := flag.NewFlagSet("admin", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return errors.New("用法：lumo admin reset-password")
	}
	switch fs.Arg(0) {
	case "reset-password":
		return errors.New("admin reset-password 尚未实现（阶段 2 · 认证与权限）")
	default:
		return fmt.Errorf("未知子命令 %q，可用：reset-password", fs.Arg(0))
	}
}

// runServe 启动 HTTP 服务。
//
// 阶段 0 只挂载健康检查与 Console 静态资源；三平面 API 路由（Chi v5）
// 与配置文件加载在阶段 1 接入，此处暂用命令行参数 + 环境变量。
func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	addr := fs.String("addr", envOr("LUMO_ADDR", ":8080"), "监听地址（环境变量 LUMO_ADDR）")
	// --no-migrate 已在此声明以固定 CLI 契约，实际行为随阶段 1 的迁移框架生效。
	noMigrate := fs.Bool("no-migrate", false, "跳过启动时自动迁移（阶段 1 生效）")
	if err := fs.Parse(args); err != nil {
		return err
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	info := version.Get()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "{\"status\":\"ok\",\"version\":%q}\n", info.Version)
	})
	// Console 挂载在 /console/ 下；根路径重定向过去，方便直接访问站点地址。
	mux.Handle(console.MountPath, console.Handler())
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, console.MountPath, http.StatusFound)
	})

	srv := &http.Server{
		Addr:              *addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	logger.Info("启动 Lumo",
		slog.String("version", info.Version),
		slog.String("commit", info.Commit),
		slog.String("addr", *addr),
		slog.Bool("consoleEmbedded", console.Built()),
		slog.Bool("noMigrate", *noMigrate),
	)
	if !console.Built() {
		logger.Warn("Console 前端未嵌入，后台界面不可用；执行 task console:build 后重新编译")
	}

	// 收到中断信号后停止接收新连接，并给在途请求留出收尾时间。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("HTTP 服务异常退出: %w", err)
		}
		return nil
	case <-ctx.Done():
		logger.Info("收到退出信号，正在关闭")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("优雅关闭失败: %w", err)
	}
	logger.Info("已退出")
	return nil
}

// envOr 返回环境变量值，为空时回退到默认值。
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
