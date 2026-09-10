package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/FeiBaiKin/lumo/internal/config"
	"github.com/FeiBaiKin/lumo/internal/database"
	"github.com/FeiBaiKin/lumo/internal/logging"
	"github.com/FeiBaiKin/lumo/internal/migrate"
	"github.com/FeiBaiKin/lumo/migrations"
)

const migrateUsage = `管理数据库迁移。

用法：
  lumo migrate [up|status|version|down]

子命令：
  up        应用所有待执行的迁移（缺省）
  status    显示各迁移的应用状态
  version   显示当前 schema 版本
  down      回滚最后一个迁移（仅开发排错）
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

	migrator, err := migrate.New(db.SQLDB(), migrations.FS, logger)
	if err != nil {
		return err
	}

	switch action {
	case cmdUp:
		return migrator.Up(ctx)
	case cmdDown:
		return migrator.Down(ctx)
	case keyStatus:
		return migrator.Status(ctx)
	case keyVersion:
		v, verErr := migrator.Version(ctx)
		if verErr != nil {
			return verErr
		}
		fmt.Printf("当前 schema 版本：%d\n", v)
		return nil
	default:
		fmt.Fprint(os.Stderr, migrateUsage)
		return fmt.Errorf("未知子命令 %q", action)
	}
}
