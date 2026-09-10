package migrate_test

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/FeiBaiKin/lumo/internal/database"
	"github.com/FeiBaiKin/lumo/internal/migrate"
	"github.com/FeiBaiKin/lumo/internal/testsupport"
	"github.com/FeiBaiKin/lumo/migrations"
)

// 集成测试直连本机 PG 的独立测试库（agent.md §12）。
//
// 本包使用独占 schema 做隔离：go test ./... 会并行执行多个包，
// 若共用 public schema，各包的清空操作会互删对方的表。
const testSchema = "lumo_it_migrate"

// openTestDB 准备一个干净的、独占 schema 的测试库连接（不自动迁移，
// 由各测试自行控制迁移时机）。
func openTestDB(t *testing.T) *database.DB {
	t.Helper()
	return testsupport.Open(t, testsupport.Options{Schema: testSchema})
}

// coreSources 只含核心迁移来源。
func coreSources() []migrate.Source {
	return []migrate.Source{{Name: migrate.CoreName, FS: migrations.FS}}
}

func newMigrator(t *testing.T, db *database.DB, sources []migrate.Source) *migrate.Migrator {
	t.Helper()
	migrator, err := migrate.New(db.SQLDB(), sources, nil)
	if err != nil {
		t.Fatalf("构造 Migrator 失败: %v", err)
	}
	return migrator
}

func TestMigrateUpAndDown(t *testing.T) {
	db := openTestDB(t)

	ctx := context.Background()
	migrator := newMigrator(t, db, coreSources())

	// 迁移前版本应为 0（版本表尚不存在）。
	v, err := migrator.Version(ctx, "")
	if err != nil {
		t.Fatalf("读取初始版本失败: %v", err)
	}
	if v != 0 {
		t.Fatalf("初始版本 = %d，期望 0", v)
	}

	if upErr := migrator.Up(ctx); upErr != nil {
		t.Fatalf("Up 失败: %v", upErr)
	}

	v, err = migrator.Version(ctx, "")
	if err != nil {
		t.Fatalf("读取迁移后版本失败: %v", err)
	}
	if v < 1 {
		t.Fatalf("迁移后版本 = %d，应至少为 1", v)
	}

	// extensions 表及其索引必须存在（00001）。
	assertTableExists(t, db, "extensions")
	for _, idx := range []string{"extensions_spec_gin", "extensions_kind_idx", "extensions_unique"} {
		assertIndexExists(t, db, idx)
	}

	// 认证相关表必须存在（00002）。
	for _, table := range []string{"users", "roles", "user_roles", "sessions", "access_tokens"} {
		assertTableExists(t, db, table)
	}

	// Up 必须幂等：重复执行不应报错，版本不变。
	if repeatErr := migrator.Up(ctx); repeatErr != nil {
		t.Fatalf("重复 Up 应当成功: %v", repeatErr)
	}
	again, err := migrator.Version(ctx, "")
	if err != nil {
		t.Fatalf("读取版本失败: %v", err)
	}
	if again != v {
		t.Errorf("重复 Up 后版本变化: %d -> %d", v, again)
	}

	// Down 只回滚**最后一个**迁移。这是 goose 的语义：早期只有单个迁移时
	// 容易误以为它会清空整个 schema，加了第二个迁移就会暴露这个误解。
	if downErr := migrator.Down(ctx, ""); downErr != nil {
		t.Fatalf("Down 失败: %v", downErr)
	}
	if exists := relExists(t, db, "users"); exists {
		t.Error("Down 后 users 表应被删除")
	}
	if !relExists(t, db, "extensions") {
		t.Error("Down 只应回滚最后一个迁移，extensions 表应保留")
	}

	// 继续回滚直到版本 0，此时全部表都应消失；版本 0 时再 Down 应为无害的空操作。
	for range 10 {
		current, verErr := migrator.Version(ctx, "")
		if verErr != nil {
			t.Fatalf("读取版本失败: %v", verErr)
		}
		if current == 0 {
			break
		}
		if downErr := migrator.Down(ctx, ""); downErr != nil {
			t.Fatalf("回滚到版本 0 失败: %v", downErr)
		}
	}
	if exists := relExists(t, db, "extensions"); exists {
		t.Error("全部回滚后 extensions 表应被删除")
	}
	if err := migrator.Down(ctx, ""); err != nil {
		t.Errorf("版本 0 时 Down 应为空操作，实际报错: %v", err)
	}
}

// TestExtensionsConstraints 验证迁移建立的约束真正生效。
func TestExtensionsConstraints(t *testing.T) {
	db := openTestDB(t)

	ctx := context.Background()
	migrator := newMigrator(t, db, coreSources())
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
	err := db.NewRaw(`SELECT spec->>'title' FROM extensions WHERE spec @> ?::jsonb`,
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
//
// 每个 goroutine 各自构造 Migrator：迁移器基于 goose Provider，无包级全局状态，
// 这样并发构造也不会触发数据竞态（阶段 2 遗留的 CI 竞态即由此修复）。
func TestConcurrentUpIsSerialized(t *testing.T) {
	db := openTestDB(t)

	ctx := context.Background()
	const workers = 4
	errCh := make(chan error, workers)

	for range workers {
		go func() {
			migrator, err := migrate.New(db.SQLDB(), coreSources(), nil)
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

// blogModuleFS 模拟一个自带迁移的模块，编号从 1 开始、与核心无关。
var blogModuleFS = fstest.MapFS{
	"00001_blog.sql": &fstest.MapFile{Data: []byte(`-- +goose Up
CREATE TABLE blog_things (id bigserial PRIMARY KEY, title text NOT NULL);
-- +goose Down
DROP TABLE blog_things;
`)},
}

// TestModuleSourcesHaveIndependentVersionTables 验证核心与模块各有版本表、互不干扰。
//
// 这是模块迁移接入的核心契约：模块编号与核心重叠（都有 00001）也不冲突，
// 回滚一个模块不影响另一个来源。
func TestModuleSourcesHaveIndependentVersionTables(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	sources := append(coreSources(), migrate.Source{Name: "blog", FS: blogModuleFS})
	migrator := newMigrator(t, db, sources)

	if err := migrator.Up(ctx); err != nil {
		t.Fatalf("Up 失败: %v", err)
	}

	assertTableExists(t, db, "extensions")
	assertTableExists(t, db, "blog_things")
	assertTableExists(t, db, migrate.TableName(migrate.CoreName))
	assertTableExists(t, db, migrate.TableName("blog"))

	coreVersion, err := migrator.Version(ctx, migrate.CoreName)
	if err != nil {
		t.Fatalf("读取核心版本失败: %v", err)
	}
	if coreVersion < 2 {
		t.Errorf("核心版本 = %d，应至少为 2", coreVersion)
	}
	blogVersion, err := migrator.Version(ctx, "blog")
	if err != nil {
		t.Fatalf("读取模块版本失败: %v", err)
	}
	if blogVersion != 1 {
		t.Errorf("模块版本 = %d，期望 1", blogVersion)
	}

	// Status 应列出两个来源，且全部已应用。
	statuses, err := migrator.Status(ctx)
	if err != nil {
		t.Fatalf("Status 失败: %v", err)
	}
	seen := map[string]int{}
	for _, st := range statuses {
		seen[st.Source]++
		if !st.Applied {
			t.Errorf("迁移 %s/%d 应已应用", st.Source, st.Version)
		}
	}
	if seen[migrate.CoreName] < 2 || seen["blog"] != 1 {
		t.Errorf("Status 来源分布 = %v", seen)
	}

	// 回滚模块不影响核心。
	if err := migrator.Down(ctx, "blog"); err != nil {
		t.Fatalf("回滚模块失败: %v", err)
	}
	if relExists(t, db, "blog_things") {
		t.Error("回滚后 blog_things 应被删除")
	}
	assertTableExists(t, db, "extensions")
	if v, _ := migrator.Version(ctx, "blog"); v != 0 {
		t.Errorf("回滚后模块版本 = %d，期望 0", v)
	}
	if v, _ := migrator.Version(ctx, migrate.CoreName); v != coreVersion {
		t.Errorf("回滚模块后核心版本变化: %d -> %d", coreVersion, v)
	}

	// 未知来源应报错而非静默忽略。
	if err := migrator.Down(ctx, "nope"); err == nil {
		t.Error("未知来源应报错")
	}
}

// relExists 报告表或索引是否存在。
//
// 用 to_regclass 而非 information_schema 按 schema 名过滤：前者按当前
// search_path 解析，因此查的是本包独占 schema 里的对象。若写死 'public'，
// 一旦 public 中残留同名对象，断言就会对着错误的表给出结论。
func relExists(t *testing.T, db *database.DB, name string) bool {
	t.Helper()

	var exists bool
	err := db.NewRaw("SELECT to_regclass(?) IS NOT NULL", name).
		Scan(context.Background(), &exists)
	if err != nil {
		t.Fatalf("查询对象 %s 是否存在失败: %v", name, err)
	}
	return exists
}

func assertTableExists(t *testing.T, db *database.DB, name string) {
	t.Helper()

	if !relExists(t, db, name) {
		t.Errorf("表 %s 应当存在", name)
	}
}

func assertIndexExists(t *testing.T, db *database.DB, name string) {
	t.Helper()

	if !relExists(t, db, name) {
		t.Errorf("索引 %s 应当存在", name)
	}
}
