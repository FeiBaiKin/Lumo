// Package testdb 给集成测试提供一个用完即删的独立 schema。
//
// 连接串只从 LUMO_TEST_DSN 取，没设时跳过测试，所以不带数据库也能跑 go test。
// 库名必须含 test：同一实例上还有开发库和别的项目的库，连错一次就是一次事故，
// 这道闸门不能靠人记。每次调用建一个随机命名的 schema，测试结束连同里面的表一起删掉，
// 多个测试包并行跑也互不干扰。
package testdb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/FeiBaiKin/lumo/internal/config"
	"github.com/FeiBaiKin/lumo/internal/database"
	"github.com/FeiBaiKin/lumo/internal/migrate"
	"github.com/FeiBaiKin/lumo/migrations"
)

// EnvDSN 是测试库连接串所在的环境变量。
const EnvDSN = "LUMO_TEST_DSN"

// DSN 建一个新 schema，返回把 search_path 指向它的连接串；测试结束时删除该 schema。
func DSN(t testing.TB) string {
	t.Helper()
	raw := os.Getenv(EnvDSN)
	if raw == "" {
		t.Skip("未设置 " + EnvDSN + "，跳过集成测试")
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("%s 不是合法的连接串: %v", EnvDSN, err)
	}
	if name := strings.TrimPrefix(u.Path, "/"); !strings.Contains(strings.ToLower(name), "test") {
		t.Fatalf("%s 指向的库 %q 名字里没有 test，拒绝在它上面跑测试", EnvDSN, name)
	}

	suffix := make([]byte, 6)
	if _, randErr := rand.Read(suffix); randErr != nil {
		t.Fatal(randErr)
	}
	schema := "t_" + hex.EncodeToString(suffix)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := database.Open(ctx, config.DatabaseConfig{DSN: raw, MaxOpenConns: 2}, false)
	if err != nil {
		t.Fatalf("连接测试库: %v", err)
	}
	if _, err := admin.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		_ = admin.Close()
		t.Fatalf("建 schema: %v", err)
	}
	t.Cleanup(func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer dropCancel()
		if _, err := admin.ExecContext(dropCtx, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Logf("删除 schema %s 失败: %v", schema, err)
		}
		_ = admin.Close()
	})

	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String()
}

// Open 在新 schema 上开一个连接池，测试结束时关闭。
func Open(t testing.TB) *database.DB {
	t.Helper()
	dsn := DSN(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := database.Open(ctx, config.DatabaseConfig{DSN: dsn, MaxOpenConns: 8}, false)
	if err != nil {
		t.Fatalf("连接测试 schema: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// OpenCore 在新 schema 上开连接池并跑完核心迁移，给只用到核心表的测试用。
func OpenCore(t testing.TB) *database.DB {
	t.Helper()
	db := Open(t)
	migrator, err := migrate.New(db.SQLDB(), []migrate.Source{{Name: migrate.CoreName, FS: migrations.FS}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := migrator.Up(ctx); err != nil {
		t.Fatalf("核心迁移: %v", err)
	}
	return db
}
