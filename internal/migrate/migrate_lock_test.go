package migrate_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/FeiBaiKin/lumo/internal/migrate"
)

// blockingDriverName 是下面假驱动的注册名。
const blockingDriverName = "migrate_test_blocking"

var registerBlockingOnce sync.Once

// blockingConn 的连接可用，但所有 ExecContext 都阻塞到 context 结束，
// 用来模拟「advisory lock 被另一个会话占住」时 pg_advisory_lock 的等待行为，
// 无需真实数据库即可验证锁等待期限。
type blockingConn struct{}

func (blockingConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("测试驱动不支持 Prepare")
}
func (blockingConn) Close() error              { return nil }
func (blockingConn) Begin() (driver.Tx, error) { return nil, errors.New("测试驱动不支持事务") }

func (blockingConn) ExecContext(ctx context.Context, _ string, _ []driver.NamedValue) (driver.Result, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

type blockingDriver struct{}

func (blockingDriver) Open(string) (driver.Conn, error) { return blockingConn{}, nil }

// TestLockWaitTimesOutWithClearError 验证锁等待有明确上限：
// 锁被占用时 Up 必须在上限附近返回「等待迁移锁超时」错误，而不是永久挂起。
func TestLockWaitTimesOutWithClearError(t *testing.T) {
	t.Parallel()

	registerBlockingOnce.Do(func() { sql.Register(blockingDriverName, blockingDriver{}) })

	db, err := sql.Open(blockingDriverName, "ignored")
	if err != nil {
		t.Fatalf("打开假数据库失败: %v", err)
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(1)

	fsys := fstest.MapFS{"00001_x.sql": {Data: []byte("-- +goose Up\nSELECT 1;\n")}}
	migrator, err := migrate.New(db, []migrate.Source{{Name: migrate.CoreName, FS: fsys}}, nil,
		migrate.WithLockTimeout(50*time.Millisecond))
	if err != nil {
		t.Fatalf("构造 Migrator 失败: %v", err)
	}

	start := time.Now()
	upErr := migrator.Up(context.Background())
	elapsed := time.Since(start)

	if upErr == nil {
		t.Fatal("锁等待超时应返回错误，实际为 nil")
	}
	if !strings.Contains(upErr.Error(), "等待迁移锁超时") {
		t.Errorf("错误信息未点明锁等待超时: %v", upErr)
	}
	if elapsed > 5*time.Second {
		t.Errorf("超时未在期限内生效，实际耗时 %v", elapsed)
	}
}
