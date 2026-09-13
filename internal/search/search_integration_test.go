package search

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/uptrace/bun"

	"github.com/FeiBaiKin/lumo/internal/api"
	"github.com/FeiBaiKin/lumo/internal/auth"
	"github.com/FeiBaiKin/lumo/internal/content"
	"github.com/FeiBaiKin/lumo/internal/database"
	"github.com/FeiBaiKin/lumo/internal/migrate"
	"github.com/FeiBaiKin/lumo/internal/taxonomy"
	"github.com/FeiBaiKin/lumo/internal/testsupport"
)

// 集成测试直连本机 PG 的独立测试库（agent.md §12），本包使用独占 schema。
const testSchema = "lumo_it_search"

// seed 是一条测试内容。
type seed struct {
	Title      string
	Slug       string
	Excerpt    string
	Content    string
	Type       string
	Status     string
	Visibility string
}

// newService 准备一个带 content 表的测试库，并返回直连的搜索服务。
//
// 不走整机装配：对账 worker 是后台 goroutine，测试要能一步步驱动它，
// 才看得清「内容改了 → 索引过期 → 重建 → 搜得到」每一步的状态。
func newService(t *testing.T) (*Service, *database.DB, int64) {
	t.Helper()

	db := testsupport.Open(t, testsupport.Options{
		Schema:  testSchema,
		Migrate: true,
		Sources: []migrate.Source{
			{Name: taxonomy.Name, FS: taxonomy.New().Migrations()},
			{Name: content.Name, FS: content.New().Migrations()},
			{Name: Name, FS: New().Migrations()},
		},
	})

	ctx := context.Background()
	users := auth.NewStore(db.DB)
	if err := users.SeedRoles(ctx); err != nil {
		t.Fatalf("写入内置角色失败: %v", err)
	}
	author, err := users.CreateUser(ctx, &auth.CreateUserParams{
		Username: "author",
		Email:    "author@example.com",
		Password: "test-password-123",
		Roles:    []string{"admin"},
		// 等同后台建号：登录闸门要求邮箱已验证，测试账号不该绕开它另开一条路。
		EmailVerified: true,
	})
	if err != nil {
		t.Fatalf("创建作者失败: %v", err)
	}

	return NewService(NewStore(db.DB), slog.New(slog.DiscardHandler)), db, author.ID
}

// insert 写入一条内容并返回 ID。
func insert(t *testing.T, db *bun.DB, authorID int64, s *seed) int64 {
	t.Helper()

	if s.Type == "" {
		s.Type = "post"
	}
	if s.Status == "" {
		s.Status = "published"
	}
	if s.Visibility == "" {
		s.Visibility = "public"
	}

	var id int64
	err := db.NewRaw(
		`INSERT INTO posts (type, title, slug, status, visibility, raw_type, raw, content,
		                    excerpt, author_id, published_at)
		 VALUES (?, ?, ?, ?, ?, 'html', '', ?, ?, ?, now())
		 RETURNING id`,
		s.Type, s.Title, s.Slug, s.Status, s.Visibility, s.Content, s.Excerpt, authorID).
		Scan(context.Background(), &id)
	if err != nil {
		t.Fatalf("写入内容 %q 失败: %v", s.Title, err)
	}
	return id
}

// titles 取出命中结果的标题，便于断言顺序。
func titles(hits []Hit) []string {
	out := make([]string, 0, len(hits))
	for i := range hits {
		out = append(out, hits[i].Title)
	}
	return out
}

// find 执行一次搜索。
func find(t *testing.T, svc *Service, query string, types ...string) *Result {
	t.Helper()
	result, err := svc.Search(context.Background(), &Params{Query: query, Types: types})
	if err != nil {
		t.Fatalf("搜索 %q 失败: %v", query, err)
	}
	return result
}

// TestSearchChinese 覆盖中文检索：二元组能匹配子串，且相邻关系不被打乱。
func TestSearchChinese(t *testing.T) {
	svc, db, author := newService(t)
	ctx := context.Background()

	insert(t, db.DB, author, &seed{Title: "全文搜索很难", Slug: "fts"})
	insert(t, db.DB, author, &seed{Title: "东京都会生活", Slug: "tokyo"})
	insert(t, db.DB, author, &seed{Title: "京都大学见闻", Slug: "kyoto"})
	insert(t, db.DB, author, &seed{Title: "读书笔记", Slug: "notes"})
	svc.reconcile(ctx)

	t.Run("子串命中", func(t *testing.T) {
		if got := titles(find(t, svc, "全文搜索").Hits); len(got) != 1 || got[0] != "全文搜索很难" {
			t.Fatalf("结果 = %v", got)
		}
	})

	t.Run("词序颠倒不命中", func(t *testing.T) {
		// 二元组用短语算子相连，等价于子串匹配：「搜索全文」在原文中并不相邻。
		if got := find(t, svc, "搜索全文").Total; got != 0 {
			t.Fatalf("命中 %d 条，期望 0", got)
		}
	})

	t.Run("相邻关系收窄误召回", func(t *testing.T) {
		// 「东京都」切成 东京 / 京都；只有真的挨着才算命中，京都大学不该混进来。
		if got := titles(find(t, svc, "东京都").Hits); len(got) != 1 || got[0] != "东京都会生活" {
			t.Fatalf("结果 = %v", got)
		}
	})

	t.Run("单字按前缀匹配", func(t *testing.T) {
		// 索引里只有二元组，单字查询对不上，故退化为前缀匹配，能命中「书笔」。
		if got := titles(find(t, svc, "书").Hits); len(got) != 1 || got[0] != "读书笔记" {
			t.Fatalf("结果 = %v", got)
		}
	})

	t.Run("切不出词元时返回空结果而不是报错", func(t *testing.T) {
		if got := find(t, svc, "   ！？  ").Total; got != 0 {
			t.Fatalf("命中 %d 条，期望 0", got)
		}
	})
}

// TestSearchFields 覆盖字段权重与正文检索。
func TestSearchFields(t *testing.T) {
	svc, db, author := newService(t)
	ctx := context.Background()

	insert(t, db.DB, author, &seed{
		Title: "无关的标题", Slug: "body-hit",
		Content: `<p>正文里提到了<strong>模板引擎</strong>这个词。</p>`,
	})
	insert(t, db.DB, author, &seed{Title: "模板引擎入门", Slug: "title-hit"})
	insert(t, db.DB, author, &seed{
		Title: "另一篇", Slug: "excerpt-hit", Excerpt: "讲模板引擎的摘要",
	})
	svc.reconcile(ctx)

	result := find(t, svc, "模板引擎")
	if result.Total != 3 {
		t.Fatalf("命中 %d 条，期望 3：%v", result.Total, titles(result.Hits))
	}
	// 标题权重 A、摘要 B、正文 D：标题命中必须排在最前。
	if got := titles(result.Hits); got[0] != "模板引擎入门" {
		t.Fatalf("排序 = %v，标题命中应当排在最前", got)
	}
	if result.Hits[0].Score <= result.Hits[2].Score {
		t.Fatalf("相关度未拉开：%v", result.Hits)
	}
}

// TestSearchStripsHTML 确认正文索引的是文字而不是标签。
func TestSearchStripsHTML(t *testing.T) {
	svc, db, author := newService(t)
	ctx := context.Background()

	insert(t, db.DB, author, &seed{
		Title: "带标签的正文", Slug: "html",
		Content: `<div class="highlight"><a href="https://example.com/strong">链接</a></div>`,
	})
	svc.reconcile(ctx)

	if got := find(t, svc, "highlight").Total; got != 0 {
		t.Fatalf("类名进了索引，命中 %d 条", got)
	}
	if got := find(t, svc, "链接").Total; got != 1 {
		t.Fatalf("正文文字未进索引，命中 %d 条", got)
	}
}

// TestSearchLatin 覆盖拉丁词与数字：整词匹配、大小写不敏感。
func TestSearchLatin(t *testing.T) {
	svc, db, author := newService(t)
	ctx := context.Background()

	insert(t, db.DB, author, &seed{Title: "PostgreSQL 17 上手", Slug: "pg"})
	svc.reconcile(ctx)

	for _, query := range []string{"postgresql", "PostgreSQL", "PostgreSQL 17"} {
		if got := find(t, svc, query).Total; got != 1 {
			t.Errorf("搜索 %q 命中 %d 条，期望 1", query, got)
		}
	}
	if got := find(t, svc, "postgres").Total; got != 0 {
		t.Errorf("拉丁词按整词匹配，前缀不该命中：%d 条", got)
	}
}

// TestSearchVisibility 确认搜索不会漏出草稿与私密内容。
func TestSearchVisibility(t *testing.T) {
	svc, db, author := newService(t)
	ctx := context.Background()

	insert(t, db.DB, author, &seed{Title: "公开的密钥说明", Slug: "public"})
	insert(t, db.DB, author, &seed{Title: "草稿里的密钥", Slug: "draft", Status: "draft"})
	insert(t, db.DB, author, &seed{Title: "私密的密钥", Slug: "private", Visibility: "private"})
	insert(t, db.DB, author, &seed{Title: "回收站的密钥", Slug: "trashed", Status: "trashed"})
	svc.reconcile(ctx)

	result := find(t, svc, "密钥")
	if got := titles(result.Hits); len(got) != 1 || got[0] != "公开的密钥说明" {
		t.Fatalf("结果 = %v，只应返回已发布且公开的内容", got)
	}
}

// TestSearchTypes 覆盖按内容类型筛选。
func TestSearchTypes(t *testing.T) {
	svc, db, author := newService(t)
	ctx := context.Background()

	insert(t, db.DB, author, &seed{Title: "关于本站", Slug: "about", Type: "page"})
	insert(t, db.DB, author, &seed{Title: "关于搜索的文章", Slug: "about-search"})
	svc.reconcile(ctx)

	if got := find(t, svc, "关于").Total; got != 2 {
		t.Fatalf("不限类型应命中 2 条，实际 %d", got)
	}
	if got := titles(find(t, svc, "关于", "page").Hits); len(got) != 1 || got[0] != "关于本站" {
		t.Fatalf("按 page 筛选 = %v", got)
	}
	if got := titles(find(t, svc, "关于", "post").Hits); len(got) != 1 || got[0] != "关于搜索的文章" {
		t.Fatalf("按 post 筛选 = %v", got)
	}
}

// TestSearchPaging 覆盖分页：总数是全部命中数，条目按页切分。
func TestSearchPaging(t *testing.T) {
	svc, db, author := newService(t)
	ctx := context.Background()

	for _, slug := range []string{"a", "b", "c"} {
		insert(t, db.DB, author, &seed{Title: "分页测试 " + slug, Slug: slug})
	}
	svc.reconcile(ctx)

	result, err := svc.Search(ctx, &Params{Query: "分页测试", Page: api.PageParams{Page: 2, Size: 2}})
	if err != nil {
		t.Fatalf("搜索失败: %v", err)
	}
	if result.Total != 3 {
		t.Fatalf("total = %d，期望 3", result.Total)
	}
	if len(result.Hits) != 1 {
		t.Fatalf("第二页 = %v，期望 1 条", titles(result.Hits))
	}
}

// TestReconcileFollowsEdits 覆盖对账：内容改了，索引跟着改。
//
// 这是不挂写钩子、改用 updated_at 对账的核心假设，必须钉死。
func TestReconcileFollowsEdits(t *testing.T) {
	svc, db, author := newService(t)
	ctx := context.Background()

	id := insert(t, db.DB, author, &seed{Title: "旧标题里的鲸鱼", Slug: "whale"})
	svc.reconcile(ctx)
	if got := find(t, svc, "鲸鱼").Total; got != 1 {
		t.Fatalf("初次索引未生效，命中 %d 条", got)
	}

	// 模拟内容模块的写路径：任何一条写路径都会更新 updated_at。
	if _, err := db.DB.NewRaw(
		`UPDATE posts SET title = ?, updated_at = now() WHERE id = ?`,
		"新标题里的海豚", id).Exec(ctx); err != nil {
		t.Fatalf("更新内容失败: %v", err)
	}

	stats, err := svc.Stats(ctx)
	if err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if stats.Pending != 1 {
		t.Fatalf("pending = %d，改过的内容应当被认成过期", stats.Pending)
	}

	svc.reconcile(ctx)
	if got := find(t, svc, "海豚").Total; got != 1 {
		t.Fatalf("新标题搜不到，命中 %d 条", got)
	}
	if got := find(t, svc, "鲸鱼").Total; got != 0 {
		t.Fatalf("旧标题仍能搜到，命中 %d 条", got)
	}

	// 删除后索引随行消失，不留孤儿。
	if _, err := db.DB.NewRaw(`DELETE FROM posts WHERE id = ?`, id).Exec(ctx); err != nil {
		t.Fatalf("删除内容失败: %v", err)
	}
	if got := find(t, svc, "海豚").Total; got != 0 {
		t.Fatalf("内容已删除但仍能搜到，命中 %d 条", got)
	}
}

// TestReconcileRaceWithEdit 确认对账期间的修改不会被吞掉。
//
// 索引写回的是读出时那一刻的 updated_at；若写 now()，这次修改会被判成「已索引」，
// 索引从此永远停在旧内容上——这是整个对账方案最容易写错的一处。
func TestReconcileRaceWithEdit(t *testing.T) {
	svc, db, author := newService(t)
	ctx := context.Background()

	id := insert(t, db.DB, author, &seed{Title: "初版", Slug: "race"})
	rows, err := svc.store.Stale(ctx, 10)
	if err != nil || len(rows) != 1 {
		t.Fatalf("取待索引内容失败: %v（%d 条）", err, len(rows))
	}

	// 读出之后、写回之前，内容又被改了一次。
	time.Sleep(2 * time.Millisecond)
	if _, execErr := db.DB.NewRaw(
		`UPDATE posts SET title = ?, updated_at = now() WHERE id = ?`,
		"改版之后的猎豹", id).Exec(ctx); execErr != nil {
		t.Fatalf("更新内容失败: %v", execErr)
	}
	if indexErr := svc.store.Index(ctx, &rows[0]); indexErr != nil {
		t.Fatalf("写索引失败: %v", indexErr)
	}

	stats, err := svc.Stats(ctx)
	if err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if stats.Pending != 1 {
		t.Fatalf("pending = %d，期间的修改应当仍算过期", stats.Pending)
	}
	svc.reconcile(ctx)
	if got := find(t, svc, "猎豹").Total; got != 1 {
		t.Fatalf("修改被吞掉了，命中 %d 条", got)
	}
}

// TestReindex 覆盖整体重建：切词规则升级后要能让存量内容跟上。
func TestReindex(t *testing.T) {
	svc, db, author := newService(t)
	ctx := context.Background()

	insert(t, db.DB, author, &seed{Title: "重建索引测试", Slug: "reindex"})
	svc.reconcile(ctx)

	stats, err := svc.Stats(ctx)
	if err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if stats.Total != 1 || stats.Indexed != 1 || stats.Pending != 0 {
		t.Fatalf("索引进度 = %+v", stats)
	}

	pending, err := svc.Reindex(ctx)
	if err != nil {
		t.Fatalf("重建索引失败: %v", err)
	}
	if pending != 1 {
		t.Fatalf("待重建 = %d，期望 1", pending)
	}
	stats, err = svc.Stats(ctx)
	if err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if stats.Pending != 1 {
		t.Fatalf("重建后 pending = %d，期望 1", stats.Pending)
	}

	svc.reconcile(ctx)
	if got := find(t, svc, "重建索引").Total; got != 1 {
		t.Fatalf("重建后搜不到，命中 %d 条", got)
	}
}

// TestSearchPostIDs 覆盖主题前台用的窄接口：只出文章，且带分页。
func TestSearchPostIDs(t *testing.T) {
	svc, db, author := newService(t)
	ctx := context.Background()

	insert(t, db.DB, author, &seed{Title: "关于本站", Slug: "about", Type: "page"})
	postID := insert(t, db.DB, author, &seed{Title: "关于搜索", Slug: "about-search"})
	svc.reconcile(ctx)

	ids, total, err := svc.SearchPostIDs(ctx, "关于", 10, 0)
	if err != nil {
		t.Fatalf("搜索失败: %v", err)
	}
	if total != 1 || len(ids) != 1 || ids[0] != postID {
		t.Fatalf("ids = %v，total = %d，期望只返回文章 %d", ids, total, postID)
	}

	// 关键词切不出词元时不查库，直接空结果。
	ids, total, err = svc.SearchPostIDs(ctx, "  ", 10, 0)
	if err != nil || len(ids) != 0 || total != 0 {
		t.Fatalf("空关键词 = %v / %d / %v", ids, total, err)
	}
}
