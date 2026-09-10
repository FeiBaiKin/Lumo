// Package testsupport 提供集成测试共用的数据库准备逻辑。
//
// 核心问题是隔离：集成测试需要清空并重建对象，而 `go test ./...` 会在
// **多个包之间并行**执行。若各包共用 public schema，一个包的 DROP SCHEMA
// 会把另一个包正在使用的表删掉，表现为随机失败。
//
// 因此每个测试包使用**独占 schema**：连接串带 search_path，
// 未限定的 DDL 与查询都落在自己的 schema 内，互不干扰。
package testsupport

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/FeiBaiKin/lumo/internal/config"
	"github.com/FeiBaiKin/lumo/internal/database"
	"github.com/FeiBaiKin/lumo/internal/migrate"
	"github.com/FeiBaiKin/lumo/migrations"
)

// DSNEnv 是测试库连接串所在的环境变量名（agent.md §12）。
const DSNEnv = "LUMO_TEST_DSN"

// forbiddenDatabases 是禁止连接的库（agent.md §13.2）。
//
// 本包会删除并重建 schema，误连到这些库会破坏真实数据。
// 库名相近（gocms vs gocms_dev）使误连风险很高，故在代码层面设防。
var forbiddenDatabases = []string{"gocms", "gocms_dev", "authcenter", "hone", "xzji", "postgres"}

// schemaNamePattern 限定 schema 名形态，避免标识符拼接引入注入风险。
var schemaNamePattern = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)

// Options 描述一个测试包的数据库隔离需求。
type Options struct {
	// Schema 是本包独占的 schema 名，各测试包必须互不相同。
	Schema string
	// Migrate 为真时在重置 schema 后执行全部迁移。
	Migrate bool
	// Sources 是核心之外的额外迁移来源（模块迁移），按顺序在核心之后执行；仅在 Migrate 为真时生效。
	Sources []migrate.Source
}

// Open 准备一个干净的、独占 schema 的测试库连接。
//
// 未设置 LUMO_TEST_DSN 时跳过测试，保证无数据库环境下 go test ./... 仍可通过。
func Open(t *testing.T, opts Options) *database.DB {
	t.Helper()

	if !schemaNamePattern.MatchString(opts.Schema) {
		t.Fatalf("testsupport: 非法 schema 名 %q", opts.Schema)
	}

	dsn := os.Getenv(DSNEnv)
	if dsn == "" {
		t.Skipf("未设置 %s，跳过集成测试", DSNEnv)
	}

	cfg := config.Default()
	cfg.Database.DSN = dsn
	if err := cfg.RequireDSN(); err != nil {
		t.Fatalf("%s 不合法: %v", DSNEnv, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// 管理连接不带 search_path，用于校验目标库并维护 schema 本身。
	adminCfg := cfg.Database
	admin, err := database.Open(ctx, adminCfg, false)
	if err != nil {
		t.Fatalf("连接测试库失败: %v", err)
	}
	t.Cleanup(func() { _ = admin.Close() })

	if guardErr := guardDatabase(ctx, admin); guardErr != nil {
		t.Fatal(guardErr)
	}

	quoted := quoteIdentifier(opts.Schema)
	if _, dropErr := admin.ExecContext(ctx, "DROP SCHEMA IF EXISTS "+quoted+" CASCADE"); dropErr != nil {
		t.Fatalf("清理 schema %s 失败: %v", opts.Schema, dropErr)
	}
	if _, createErr := admin.ExecContext(ctx, "CREATE SCHEMA "+quoted); createErr != nil {
		t.Fatalf("创建 schema %s 失败: %v", opts.Schema, createErr)
	}

	// 业务连接把 search_path 指向独占 schema。
	scopedCfg := cfg.Database
	scopedCfg.DSN = withSearchPath(dsn, opts.Schema)
	db, err := database.Open(ctx, scopedCfg, false)
	if err != nil {
		t.Fatalf("连接 schema %s 失败: %v", opts.Schema, err)
	}

	// 校验隔离确实生效：若 search_path 未生效，后续操作会落到 public，
	// 隔离静默失效并再次引发跨包互删，必须在此刻就失败。
	var current string
	if err := db.NewRaw("SELECT current_schema()").Scan(ctx, &current); err != nil {
		t.Fatalf("读取 current_schema 失败: %v", err)
	}
	if current != opts.Schema {
		t.Fatalf("schema 隔离未生效：current_schema = %q，期望 %q", current, opts.Schema)
	}

	if opts.Migrate {
		sources := []migrate.Source{{Name: migrate.CoreName, FS: migrations.FS}}
		sources = append(sources, opts.Sources...)
		migrator, migErr := migrate.New(db.SQLDB(), sources, nil)
		if migErr != nil {
			t.Fatalf("构造 Migrator 失败: %v", migErr)
		}
		if upErr := migrator.Up(ctx); upErr != nil {
			t.Fatalf("执行迁移失败: %v", upErr)
		}
	}

	t.Cleanup(func() {
		_ = db.Close()
		// 清理放在最后：DROP SCHEMA 需要 admin 连接仍然存活。
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if _, err := admin.ExecContext(cleanupCtx, "DROP SCHEMA IF EXISTS "+quoted+" CASCADE"); err != nil {
			t.Logf("清理 schema %s 失败: %v", opts.Schema, err)
		}
	})

	return db
}

// guardDatabase 校验连接的是专用测试库。
//
// 两重检查：黑名单排除已知的他项目库，白名单要求库名含 test。
func guardDatabase(ctx context.Context, db *database.DB) error {
	name, err := db.CurrentDatabase(ctx)
	if err != nil {
		return fmt.Errorf("读取当前库名失败: %w", err)
	}
	for _, forbidden := range forbiddenDatabases {
		if strings.EqualFold(name, forbidden) {
			return fmt.Errorf(
				"拒绝在库 %q 上运行集成测试：该库属其他项目或非测试库（agent.md §13.2）", name)
		}
	}
	if !strings.Contains(strings.ToLower(name), "test") {
		return fmt.Errorf("集成测试库名 %q 未包含 test，拒绝执行以防误连", name)
	}
	return nil
}

// withSearchPath 在 DSN 上追加 search_path 运行时参数。
func withSearchPath(dsn, schema string) string {
	separator := "?"
	if strings.Contains(dsn, "?") {
		separator = "&"
	}
	return dsn + separator + "search_path=" + schema
}

// quoteIdentifier 用双引号包裹标识符，并转义内部的双引号。
func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
