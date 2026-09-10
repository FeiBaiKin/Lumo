package migrate_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/FeiBaiKin/lumo/internal/config"
	"github.com/FeiBaiKin/lumo/internal/database"
	"github.com/FeiBaiKin/lumo/internal/migrate"
	"github.com/FeiBaiKin/lumo/migrations"
)

// 集成测试直连本机 PG 的独立测试库（agent.md §12）。
// 未设置 LUMO_TEST_DSN 时跳过，保证 go test ./... 在无数据库环境下仍可通过。
const testDSNEnv = "LUMO_TEST_DSN"

// forbiddenDatabases 是禁止连接的库（agent.md §13.2）。
// 集成测试会执行 DDL 并清空 schema，误连到这些库会破坏真实数据。
// 库名相近（gocms vs gocms_dev）使误连风险很高，故在代码层面设防而非仅靠文档。
var forbiddenDatabases = []string{"gocms", "gocms_dev", "authcenter", "hone", "xzji", "postgres"}

// openTestDB 建立测试库连接，并在库名不安全时直接让测试失败。
func openTestDB(t *testing.T) *database.DB {
	t.Helper()

	dsn := os.Getenv(testDSNEnv)
	if dsn == "" {
		t.Skipf("未设置 %s，跳过集成测试", testDSNEnv)
	}

	cfg := config.Default()
	cfg.Database.DSN = dsn
	if err := cfg.RequireDSN(); err != nil {
		t.Fatalf("%s 不合法: %v", testDSNEnv, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	db, err := database.Open(ctx, cfg.Database, false)
	if err != nil {
		t.Fatalf("连接测试库失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	name, err := db.CurrentDatabase(ctx)
	if err != nil {
		t.Fatalf("读取当前库名失败: %v", err)
	}
	// 安全护栏：必须连到专用测试库。
	for _, forbidden := range forbiddenDatabases {
		if strings.EqualFold(name, forbidden) {
			t.Fatalf("拒绝在库 %q 上运行集成测试：该库属其他项目或非测试库（agent.md §13.2）", name)
		}
	}
	if !strings.Contains(strings.ToLower(name), "test") {
		t.Fatalf("集成测试库名 %q 未包含 test，拒绝执行以防误连", name)
	}
	return db
}

// resetSchema 把测试库恢复到空白状态。
func resetSchema(t *testing.T, db *database.DB) {
	t.Helper()

	ctx := context.Background()
	if _, err := db.ExecContext(ctx, "DROP SCHEMA public CASCADE; CREATE SCHEMA public;"); err != nil {
		t.Fatalf("重置 schema 失败: %v", err)
	}
}

func TestMigrateUpAndDown(t *testing.T) {
	db := openTestDB(t)
	resetSchema(t, db)
	t.Cleanup(func() { resetSchema(t, db) })

	ctx := context.Background()
	migrator, err := migrate.New(db.SQLDB(), migrations.FS, nil)
	if err != nil {
		t.Fatalf("构造 Migrator 失败: %v", err)
	}

	// 迁移前版本应为 0（版本表尚不存在）。
	v, err := migrator.Version(ctx)
	if err != nil {
		t.Fatalf("读取初始版本失败: %v", err)
	}
	if v != 0 {
		t.Fatalf("初始版本 = %d，期望 0", v)
	}

	if upErr := migrator.Up(ctx); upErr != nil {
		t.Fatalf("Up 失败: %v", upErr)
	}

	v, err = migrator.Version(ctx)
	if err != nil {
		t.Fatalf("读取迁移后版本失败: %v", err)
	}
	if v < 1 {
		t.Fatalf("迁移后版本 = %d，应至少为 1", v)
	}

	// extensions 表及其索引必须存在。
	assertTableExists(t, db, "extensions")
	for _, idx := range []string{"extensions_spec_gin", "extensions_kind_idx", "extensions_unique"} {
		assertIndexExists(t, db, idx)
	}

	// Up 必须幂等：重复执行不应报错，版本不变。
	if repeatErr := migrator.Up(ctx); repeatErr != nil {
		t.Fatalf("重复 Up 应当成功: %v", repeatErr)
	}
	again, err := migrator.Version(ctx)
	if err != nil {
		t.Fatalf("读取版本失败: %v", err)
	}
	if again != v {
		t.Errorf("重复 Up 后版本变化: %d -> %d", v, again)
	}

	if err := migrator.Down(ctx); err != nil {
		t.Fatalf("Down 失败: %v", err)
	}
	if exists := tableExists(t, db, "extensions"); exists {
		t.Error("Down 后 extensions 表应被删除")
	}
}

// TestExtensionsConstraints 验证迁移建立的约束真正生效。
func TestExtensionsConstraints(t *testing.T) {
	db := openTestDB(t)
	resetSchema(t, db)
	t.Cleanup(func() { resetSchema(t, db) })

	ctx := context.Background()
	migrator, err := migrate.New(db.SQLDB(), migrations.FS, nil)
	if err != nil {
		t.Fatalf("构造 Migrator 失败: %v", err)
	}
	if upErr := migrator.Up(ctx); upErr != nil {
		t.Fatalf("Up 失败: %v", upErr)
	}

	const insert = `INSERT INTO extensions (api_group, version, kind, name, spec)
		VALUES (?, ?, ?, ?, ?)`

	// 合法记录应插入成功，并能通过 JSONB 查询取回（含 CJK）。
	if _, insErr := db.ExecContext(ctx, insert,
		"io.github.feibaiikin.lumo", "v1alpha1", "Post", "hello-world",
		`{"title":"你好世界"}`); insErr != nil {
		t.Fatalf("插入合法记录失败: %v", insErr)
	}

	var title string
	err = db.NewRaw(`SELECT spec->>'title' FROM extensions WHERE spec @> ?::jsonb`,
		`{"title":"你好世界"}`).Scan(ctx, &title)
	if err != nil {
		t.Fatalf("JSONB 查询失败: %v", err)
	}
	if title != "你好世界" {
		t.Errorf("CJK 往返失败，得到 %q", title)
	}

	// DNS-1123 约束应拒绝非法 name（agent.md §6.1）。
	badNames := []string{"-leading", "trailing-", "Upper", "has_underscore", "has.dot"}
	for _, name := range badNames {
		if _, err := db.ExecContext(ctx, insert,
			"g", "v1alpha1", "Post", name, `{}`); err == nil {
			t.Errorf("name %q 违反 DNS-1123，应被拒绝", name)
		}
	}

	// 同 group/version/kind 下 name 唯一。
	if _, err := db.ExecContext(ctx, insert,
		"io.github.feibaiikin.lumo", "v1alpha1", "Post", "hello-world", `{}`); err == nil {
		t.Error("重复的 name 应被唯一约束拒绝")
	}
}

// TestConcurrentUpIsSerialized 验证并发迁移被 advisory lock 串行化。
// 这是多实例部署的关键保障：无锁时并发 DDL 会留下半成品 schema。
func TestConcurrentUpIsSerialized(t *testing.T) {
	db := openTestDB(t)
	resetSchema(t, db)
	t.Cleanup(func() { resetSchema(t, db) })

	ctx := context.Background()
	const workers = 4
	errCh := make(chan error, workers)

	for range workers {
		go func() {
			migrator, err := migrate.New(db.SQLDB(), migrations.FS, nil)
			if err != nil {
				errCh <- err
				return
			}
			errCh <- migrator.Up(ctx)
		}()
	}

	for i := range workers {
		if err := <-errCh; err != nil {
			t.Fatalf("第 %d 个并发迁移失败: %v", i, err)
		}
	}

	assertTableExists(t, db, "extensions")
}

func tableExists(t *testing.T, db *database.DB, name string) bool {
	t.Helper()

	var exists bool
	err := db.NewRaw(
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables
			WHERE table_schema = 'public' AND table_name = ?)`, name).
		Scan(context.Background(), &exists)
	if err != nil {
		t.Fatalf("查询表 %s 是否存在失败: %v", name, err)
	}
	return exists
}

func assertTableExists(t *testing.T, db *database.DB, name string) {
	t.Helper()

	if !tableExists(t, db, name) {
		t.Errorf("表 %s 应当存在", name)
	}
}

func assertIndexExists(t *testing.T, db *database.DB, name string) {
	t.Helper()

	var exists bool
	err := db.NewRaw(
		`SELECT EXISTS (SELECT 1 FROM pg_indexes
			WHERE schemaname = 'public' AND indexname = ?)`, name).
		Scan(context.Background(), &exists)
	if err != nil {
		t.Fatalf("查询索引 %s 失败: %v", name, err)
	}
	if !exists {
		t.Errorf("索引 %s 应当存在", name)
	}
}
