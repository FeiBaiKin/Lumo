// Package database 负责建立并持有数据库连接。
//
// 组合方式：pgx（stdlib 适配）提供 *sql.DB，bun 在其上提供查询构建器。
// 选 pgx 而非 bun 自带的 pgdriver，是因为 pgx 是 Go 生态里最完整的 PG 驱动，
// 且 goose 也基于 database/sql，两者共用同一连接池。
package database

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // 注册 pgx 的 database/sql 驱动
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/extra/bundebug"

	"github.com/FeiBaiKin/lumo/internal/config"
)

// driverName 是 pgx 注册到 database/sql 的驱动名。
const driverName = "pgx"

// DB 包装 bun 客户端与底层连接池。
type DB struct {
	*bun.DB
}

// Open 建立连接池并验证连通性。
//
// 调用方负责 Close。debug 为真时打印 SQL，仅用于开发态。
func Open(ctx context.Context, cfg config.DatabaseConfig, debug bool) (*DB, error) {
	sqldb, err := sql.Open(driverName, cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("打开数据库连接: %w", err)
	}

	sqldb.SetMaxOpenConns(cfg.MaxOpenConns)
	sqldb.SetMaxIdleConns(cfg.MaxIdleConns)
	sqldb.SetConnMaxLifetime(cfg.ConnMaxLifetime)

	// 连通性必须在启动阶段确认，否则错误会延迟到第一个请求才暴露。
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := sqldb.PingContext(pingCtx); err != nil {
		_ = sqldb.Close()
		return nil, fmt.Errorf("连接数据库失败: %w", err)
	}

	bundb := bun.NewDB(sqldb, pgdialect.New())
	if debug {
		bundb.AddQueryHook(bundebug.NewQueryHook(bundebug.WithVerbose(true)))
	}
	return &DB{DB: bundb}, nil
}

// SQLDB 暴露底层 *sql.DB，供 goose 等基于 database/sql 的组件使用。
func (db *DB) SQLDB() *sql.DB {
	return db.DB.DB
}

// Version 返回服务端版本串，用于启动日志与诊断。
func (db *DB) Version(ctx context.Context) (string, error) {
	var version string
	if err := db.NewRaw("SELECT version()").Scan(ctx, &version); err != nil {
		return "", fmt.Errorf("查询数据库版本: %w", err)
	}
	return version, nil
}

// CurrentDatabase 返回当前连接的库名。
//
// 存在的意义是安全护栏：同一个 PostgreSQL 实例上往往还躺着别的项目库，库名
// 又常常只差一个后缀，一旦误连，迁移会破坏真实数据。启动时记录实际库名，
// 让误连在第一条日志就暴露。
func (db *DB) CurrentDatabase(ctx context.Context) (string, error) {
	var name string
	if err := db.NewRaw("SELECT current_database()").Scan(ctx, &name); err != nil {
		return "", fmt.Errorf("查询当前数据库名: %w", err)
	}
	return name, nil
}

// LogInfo 输出连接信息，便于确认连到了预期的库。
func (db *DB) LogInfo(ctx context.Context, logger *slog.Logger) {
	if logger == nil {
		return
	}
	name, err := db.CurrentDatabase(ctx)
	if err != nil {
		logger.Warn("无法读取当前数据库名", slog.Any("error", err))
		return
	}
	logger.Info("数据库已连接", slog.String("database", name))
}
