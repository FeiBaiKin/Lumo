package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/config"
	"github.com/FeiBaiKin/lumo/internal/database"
	"github.com/FeiBaiKin/lumo/internal/logging"
	"github.com/FeiBaiKin/lumo/internal/migrate"
)

const migrateUsage = `管理数据库迁移。

用法：
  lumo migrate [up|status|version|down [来源]]

子命令：
  up             应用核心与全部模块的待执行迁移（缺省）
  status         显示各来源每个迁移的应用状态
  version        显示各来源当前的 schema 版本
  down [来源]    回滚指定来源（缺省 core）的最后一个迁移，仅开发排错

核心与每个模块是独立的迁移来源，各有自己的版本表。
`

// runMigrate 执行迁移相关子命令。
func runMigrate(args []string) error {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	configPath := fs.String("config", "", "配置文件路径")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, migrateUsage)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	action := cmdUp
	if fs.NArg() > 0 {
		action = fs.Arg(0)
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	if dsnErr := cfg.RequireDSN(); dsnErr != nil {
		return dsnErr
	}

	logger := logging.New(os.Stdout, logging.Options{
		Level:  cfg.Log.Level,
		Format: cfg.Log.Format,
	})

	ctx := context.Background()
	db, err := database.Open(ctx, cfg.Database, false)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	// 迁移是破坏性操作，先确认连到了哪个库——库名相近极易误连（agent.md §13.2）。
	db.LogInfo(ctx, logger)

	// 走与 serve 相同的模块注册链，以收集各模块的迁移来源；不注册接口。
	application := app.New(&app.Options{Config: cfg, DB: db, Logger: logger})
	if regErr := application.Register(modules()...); regErr != nil {
		return regErr
	}
	migrator, err := migrate.New(db.SQLDB(), migrationSources(application), logger)
	if err != nil {
		return err
	}

	switch action {
	case cmdUp:
		return migrator.Up(ctx)
	case cmdDown:
		source := ""
		if fs.NArg() > 1 {
			source = fs.Arg(1)
		}
		return migrator.Down(ctx, source)
	case keyStatus:
		return printMigrationStatus(ctx, migrator)
	case keyVersion:
		for _, source := range migrator.Sources() {
			v, verErr := migrator.Version(ctx, source)
			if verErr != nil {
				return verErr
			}
			fmt.Printf("%-16s schema 版本：%d\n", source, v)
		}
		return nil
	default:
		fmt.Fprint(os.Stderr, migrateUsage)
		return fmt.Errorf("未知子命令 %q", action)
	}
}

// printMigrationStatus 以表格打印各来源每个迁移的应用状态。
func printMigrationStatus(ctx context.Context, migrator *migrate.Migrator) error {
	statuses, err := migrator.Status(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("%-12s %-8s %-8s %-25s %s\n", "来源", "版本", "状态", "应用时间", "文件")
	for _, st := range statuses {
		state, appliedAt := "pending", "-"
		if st.Applied {
			state = "applied"
			appliedAt = st.AppliedAt.Local().Format(time.RFC3339)
		}
		fmt.Printf("%-12s %-8d %-8s %-25s %s\n", st.Source, st.Version, state, appliedAt, st.Path)
	}
	return nil
}
