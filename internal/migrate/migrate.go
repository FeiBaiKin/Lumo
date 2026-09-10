// Package migrate 封装 goose 迁移的执行与并发保护。
//
// 关键设计：启动自动迁移必须带锁（agent.md §9）。多实例同时启动时，
// 无锁会让两个进程并发执行同一批 DDL，轻则报错、重则留下半成品 schema。
// 这里用 PostgreSQL 的会话级 advisory lock 做互斥：拿不到锁的实例等待，
// 而不是跳过迁移（跳过会让实例在旧 schema 上运行，问题更隐蔽）。
package migrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"time"

	"github.com/pressly/goose/v3"
)

// advisoryLockID 是迁移互斥锁的标识，取自 "lumo.migrate" 的任意稳定常量。
// 同一实例内所有 Lumo 进程共用它，故不可与其他项目冲突——取值刻意选得足够特异。
const advisoryLockID int64 = 0x4C554D4F4D4947 // "LUMOMIG"

// dialect 固定为 postgres：agent.md §2 明确只支持 PostgreSQL。
const dialect = "postgres"

// Migrator 执行迁移。
type Migrator struct {
	db     *sql.DB
	fsys   fs.FS
	logger *slog.Logger
}

// New 构造 Migrator。fsys 为携带 SQL 文件的文件系统，logger 可为 nil。
func New(db *sql.DB, fsys fs.FS, logger *slog.Logger) (*Migrator, error) {
	if db == nil {
		return nil, errors.New("migrate: db 不能为 nil")
	}
	if fsys == nil {
		return nil, errors.New("migrate: fsys 不能为 nil")
	}
	if err := goose.SetDialect(dialect); err != nil {
		return nil, fmt.Errorf("设置 goose 方言: %w", err)
	}
	return &Migrator{db: db, fsys: fsys, logger: logger}, nil
}

// Up 执行所有待应用的迁移，全程持有互斥锁。
func (m *Migrator) Up(ctx context.Context) error {
	return m.withLock(ctx, func(conn *sql.Conn) error {
		before, err := m.version(ctx)
		if err != nil {
			return err
		}

		goose.SetBaseFS(m.fsys)
		defer goose.SetBaseFS(nil)
		if upErr := goose.UpContext(ctx, m.db, "."); upErr != nil {
			return fmt.Errorf("执行迁移: %w", upErr)
		}

		after, err := m.version(ctx)
		if err != nil {
			return err
		}
		if m.logger != nil {
			if before == after {
				m.logger.Info("数据库 schema 已是最新", slog.Int64("version", after))
			} else {
				m.logger.Info("迁移完成",
					slog.Int64("from", before),
					slog.Int64("to", after))
			}
		}
		return nil
	})
}

// Down 回滚最后一个迁移。仅供开发排错，生产不应使用。
func (m *Migrator) Down(ctx context.Context) error {
	return m.withLock(ctx, func(conn *sql.Conn) error {
		goose.SetBaseFS(m.fsys)
		defer goose.SetBaseFS(nil)
		if err := goose.DownContext(ctx, m.db, "."); err != nil {
			return fmt.Errorf("回滚迁移: %w", err)
		}
		return nil
	})
}

// Status 打印各迁移的应用状态。
func (m *Migrator) Status(ctx context.Context) error {
	goose.SetBaseFS(m.fsys)
	defer goose.SetBaseFS(nil)
	if err := goose.StatusContext(ctx, m.db, "."); err != nil {
		return fmt.Errorf("查询迁移状态: %w", err)
	}
	return nil
}

// Version 返回当前已应用的最新迁移版本号。
func (m *Migrator) Version(ctx context.Context) (int64, error) {
	return m.version(ctx)
}

// version 读取当前 schema 版本；迁移表尚不存在时视为 0。
func (m *Migrator) version(ctx context.Context) (int64, error) {
	goose.SetBaseFS(m.fsys)
	defer goose.SetBaseFS(nil)

	v, err := goose.GetDBVersionContext(ctx, m.db)
	if err != nil {
		// 首次运行时 goose 版本表不存在，视为版本 0 而非失败。
		return 0, nil //nolint:nilerr // 首次迁移前无版本表属正常状态
	}
	return v, nil
}

// withLock 在持有 advisory lock 的独占连接上执行 fn。
//
// 用 pg_advisory_lock 而非 pg_try_advisory_lock：拿不到锁时应当等待其他实例
// 完成迁移，而不是放弃后在旧 schema 上启动。等待上限由 ctx 控制。
func (m *Migrator) withLock(ctx context.Context, fn func(conn *sql.Conn) error) error {
	// advisory lock 是会话级的，必须锁定到同一条连接上，否则解锁会打在别的会话。
	conn, err := m.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("获取迁移专用连接: %w", err)
	}
	defer func() { _ = conn.Close() }()

	if m.logger != nil {
		m.logger.Debug("正在获取迁移锁", slog.Int64("lockId", advisoryLockID))
	}
	start := time.Now()
	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", advisoryLockID); err != nil {
		return fmt.Errorf("获取迁移锁: %w", err)
	}
	if waited := time.Since(start); waited > time.Second && m.logger != nil {
		m.logger.Info("等待其他实例完成迁移", slog.Duration("waited", waited))
	}

	defer func() {
		// 解锁用独立的 context：即使 ctx 已取消，锁也必须释放，
		// 否则该锁会一直挂到连接断开，阻塞后续所有实例启动。
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := conn.ExecContext(unlockCtx, "SELECT pg_advisory_unlock($1)", advisoryLockID); err != nil {
			if m.logger != nil {
				m.logger.Error("释放迁移锁失败", slog.Any("error", err))
			}
		}
	}()

	return fn(conn)
}
