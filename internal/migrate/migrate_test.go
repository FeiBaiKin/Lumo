package migrate_test

import (
	"database/sql"
	"testing"
	"testing/fstest"

	"github.com/FeiBaiKin/lumo/internal/migrate"
)

// TestNewRejectsBadSources 验证构造期校验：来源名会拼进版本表名，必须在此拦住非法值。
func TestNewRejectsBadSources(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{"00001_x.sql": {Data: []byte("-- +goose Up\nSELECT 1;\n")}}
	db := &sql.DB{}

	tests := []struct {
		name    string
		db      *sql.DB
		sources []migrate.Source
	}{
		{"nil db", nil, []migrate.Source{{Name: migrate.CoreName, FS: fsys}}},
		{"无来源", db, nil},
		{"大写来源名", db, []migrate.Source{{Name: "Blog", FS: fsys}}},
		{"数字开头", db, []migrate.Source{{Name: "1blog", FS: fsys}}},
		{"含空格", db, []migrate.Source{{Name: "my blog", FS: fsys}}},
		{"nil 文件系统", db, []migrate.Source{{Name: "blog", FS: nil}}},
		{"重复来源", db, []migrate.Source{{Name: "blog", FS: fsys}, {Name: "blog", FS: fsys}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := migrate.New(tt.db, tt.sources, nil); err == nil {
				t.Fatal("应返回错误")
			}
		})
	}

	if _, err := migrate.New(db, []migrate.Source{
		{Name: migrate.CoreName, FS: fsys},
		{Name: "blog-posts", FS: fsys},
	}, nil); err != nil {
		t.Fatalf("合法来源不应报错: %v", err)
	}
}

// TestTableName 固定版本表命名：核心沿用 goose 默认名以兼容已迁移的库。
func TestTableName(t *testing.T) {
	t.Parallel()

	if got := migrate.TableName(migrate.CoreName); got != "goose_db_version" {
		t.Errorf("核心版本表 = %q", got)
	}
	if got := migrate.TableName("blog-posts"); got != "goose_db_version_blog_posts" {
		t.Errorf("模块版本表 = %q", got)
	}
}
