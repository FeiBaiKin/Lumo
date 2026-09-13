package theme_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/auth"
	"github.com/FeiBaiKin/lumo/internal/config"
	"github.com/FeiBaiKin/lumo/internal/content"
	"github.com/FeiBaiKin/lumo/internal/migrate"
	"github.com/FeiBaiKin/lumo/internal/search"
	"github.com/FeiBaiKin/lumo/internal/settings"
	"github.com/FeiBaiKin/lumo/internal/taxonomy"
	"github.com/FeiBaiKin/lumo/internal/testsupport"
	"github.com/FeiBaiKin/lumo/internal/theme"
)

// indexWait 是等待后台对账把索引补齐的上限；search 的 worker 每 2 秒一轮。
const indexWait = 20 * time.Second

// newSearchFrontendStack 装配一个「带全文搜索」的前台栈。
//
// 其余前台测试不装 search 模块，走的是标题模糊匹配的兜底路径；这里专门验证
// 装上之后前台确实换用了索引（见 theme.Store.UseSearcher）。
func newSearchFrontendStack(t *testing.T) *testsupport.Stack {
	t.Helper()

	db := testsupport.Open(t, testsupport.Options{
		Schema:  "lumo_it_theme_search",
		Migrate: true,
		Sources: []migrate.Source{
			{Name: settings.Name, FS: settings.New().Migrations()},
			{Name: taxonomy.Name, FS: taxonomy.New().Migrations()},
			{Name: content.Name, FS: content.New().Migrations()},
			{Name: search.Name, FS: search.New().Migrations()},
			{Name: theme.Name, FS: theme.New().Migrations()},
		},
	})

	cfg := config.Default()
	cfg.DataDir = t.TempDir()

	return testsupport.NewStackWith(t, db, &testsupport.StackOptions{
		Config: cfg,
		Modules: []app.Module{
			settings.New(), taxonomy.New(), content.New(), search.New(), theme.New(),
		},
		AfterStart: func(root chi.Router, application *app.App) {
			if mod := theme.From(application); mod != nil {
				core, ok := application.Lookup(auth.CoreKey)
				if !ok {
					t.Fatal("认证栈未登记为共享服务")
				}
				mod.MountFrontend(root, core.(*auth.Core).Authenticator.Optional)
			}
		},
	})
}

// waitForFrontend 等到搜索页出现（或消失）指定文字。
func waitForFrontend(t *testing.T, s *testsupport.Stack, path, want string) string {
	t.Helper()

	deadline := time.Now().Add(indexWait)
	var body string
	for time.Now().Before(deadline) {
		code, page := get(t, s, path)
		if code != http.StatusOK {
			t.Fatalf("%s 状态码 = %d", path, code)
		}
		body = page
		if strings.Contains(body, want) {
			return body
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("等待索引超时：%s 始终不含 %q", path, want)
	return ""
}

// TestFrontendSearchUsesIndex 确认装上 search 模块后，前台搜索页走的是全文索引。
//
// 判据是「正文命中」：兜底路径只匹配标题与摘要，只有真的用上索引才搜得到正文里的词。
func TestFrontendSearchUsesIndex(t *testing.T) {
	stack := newSearchFrontendStack(t)
	editor := stack.Bearer(t, "editor-index", "editor")

	seedPost(t, stack, editor, "一篇普通的标题", "indexed-body",
		"正文里藏着**苔原驯鹿**这个词，标题与摘要都没有它。")
	seedPost(t, stack, editor, "另一篇文章", "other", "无关内容")

	body := waitForFrontend(t, stack, "/search?q=苔原驯鹿", "一篇普通的标题")
	if strings.Contains(body, "另一篇文章") {
		t.Error("不相关的文章不该出现在搜索结果里")
	}

	// 搜不到的词仍应正常渲染空结果页，而不是报错。
	code, empty := get(t, stack, "/search?q=绝对不存在的词")
	if code != http.StatusOK {
		t.Fatalf("空结果页状态码 = %d", code)
	}
	if strings.Contains(empty, "一篇普通的标题") {
		t.Error("空结果页不该列出文章")
	}
}
