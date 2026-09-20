package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/config"
	"github.com/FeiBaiKin/lumo/internal/database"
	"github.com/FeiBaiKin/lumo/internal/install"
	"github.com/FeiBaiKin/lumo/internal/logging"
	"github.com/FeiBaiKin/lumo/internal/server"
	"github.com/FeiBaiKin/lumo/internal/version"
)

// specTimeout 是导出规范的整体超时，实际耗时只有一次数据库连接。
const specTimeout = 30 * time.Second

// runOpenAPI 导出 OpenAPI 规范，供 Console 生成 TypeScript 客户端。
//
// 不监听端口，但仍**需要数据库连接**：各模块的 Routes 在拿不到 DB 时不注册任何接口，
// 没有连接就会导出一份只剩核心端点的空壳规范——那比导出失败更难发现。
// 走的是与 serve 相同的注册路径（core.registerAPI），规范与实际服务的接口逐条一致。
func runOpenAPI(args []string) error {
	fs := flag.NewFlagSet("openapi", flag.ContinueOnError)
	configPath := fs.String("config", "", "配置文件路径，默认按序尝试 ./config.yaml、./config.yml")
	out := fs.String("o", "", "输出文件路径；留空写到标准输出")
	legacy := fs.Bool("3.0", false, "导出 OpenAPI 3.0 降级版本，默认 3.1")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	// 日志一律丢弃：规范可能写到标准输出，混进日志行就不是合法 JSON 了。
	logger := logging.New(io.Discard, logging.Options{Level: cfg.Log.Level, Format: cfg.Log.Format})

	if dsnErr := cfg.RequireDSN(); dsnErr != nil {
		return dsnErr
	}
	ctx, cancel := context.WithTimeout(context.Background(), specTimeout)
	defer cancel()
	db, err := database.Open(ctx, cfg.Database, false)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	core := newCoreStack(db, cfg.Server.SecureCookies, logger)
	_, planes := server.NewRouter(&server.Options{
		Logger:        logger,
		Authenticator: core.Authenticator,
		Version:       version.Get().Version,
		MaxBodySize:   cfg.Server.MaxBodySize,
		MaxUploadSize: cfg.Server.MaxUploadSize,
	})
	application := app.New(&app.Options{Config: cfg, DB: db, Logger: logger, Router: planes})
	// 安装向导的端点也要进规范：Console 的 TS 类型从它生成，缺了就只能手写。
	installer := install.New(install.Options{
		Config:  cfg,
		DataDir: cfg.DataDir,
		Logger:  logger,
		Version: version.Get().Version,
	})
	if regErr := registerAPI(core, planes, application, installer, logger); regErr != nil {
		return regErr
	}

	spec := planes.OpenAPI()
	var raw []byte
	if *legacy {
		raw, err = spec.Downgrade()
	} else {
		raw, err = spec.MarshalJSON()
	}
	if err != nil {
		return fmt.Errorf("序列化 OpenAPI 规范: %w", err)
	}

	// 缩进后再写：生成物要进版本库，逐行 diff 才看得出接口改了什么。
	var pretty bytes.Buffer
	if indentErr := json.Indent(&pretty, raw, "", "  "); indentErr != nil {
		return fmt.Errorf("格式化 OpenAPI 规范: %w", indentErr)
	}
	pretty.WriteByte('\n')

	if *out == "" {
		_, err = os.Stdout.Write(pretty.Bytes())
		return err
	}
	if dirErr := os.MkdirAll(filepath.Dir(*out), 0o755); dirErr != nil {
		return fmt.Errorf("创建输出目录: %w", dirErr)
	}
	if writeErr := os.WriteFile(*out, pretty.Bytes(), 0o600); writeErr != nil {
		return fmt.Errorf("写入 %s: %w", *out, writeErr)
	}
	// 进度写标准错误，标准输出留给规范本身。
	fmt.Fprintf(os.Stderr, "已写出 %s（%d 条路径）\n", *out, len(spec.Paths))
	return nil
}
