package menu_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/FeiBaiKin/lumo/internal/auth/perm"
	"github.com/FeiBaiKin/lumo/internal/content"
	"github.com/FeiBaiKin/lumo/internal/menu"
	"github.com/FeiBaiKin/lumo/internal/migrate"
	"github.com/FeiBaiKin/lumo/internal/server"
	"github.com/FeiBaiKin/lumo/internal/settings"
	"github.com/FeiBaiKin/lumo/internal/taxonomy"
	"github.com/FeiBaiKin/lumo/internal/testsupport"
)

// 集成测试直连本机 PG 的独立测试库（agent.md §12），本包使用独占 schema。
const testSchema = "lumo_it_menu"

const (
	consolePrefix = server.PrefixConsole
	publicPrefix  = server.PrefixPublic
)

func newStack(t *testing.T) *testsupport.Stack {
	t.Helper()
	set, tax, con, mn := settings.New(), taxonomy.New(), content.New(), menu.New()
	db := testsupport.Open(t, testsupport.Options{
		Schema:  testSchema,
		Migrate: true,
		Sources: []migrate.Source{
			{Name: set.Name(), FS: set.Migrations()},
			{Name: tax.Name(), FS: tax.Migrations()},
			{Name: con.Name(), FS: con.Migrations()},
			{Name: mn.Name(), FS: mn.Migrations()},
		},
	})
	return testsupport.NewStack(t, db, set, tax, con, mn)
}

func mustStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) map[string]any {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("状态码 = %d，期望 %d：%s", rec.Code, want, rec.Body.String())
	}
	if rec.Body.Len() == 0 {
		return nil
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是合法 JSON: %v\n%s", err, rec.Body.String())
	}
	return body
}

func req(t *testing.T, s *testsupport.Stack, method, path, body, authz string) *httptest.ResponseRecorder {
	t.Helper()
	return s.Do(t, &testsupport.Request{Method: method, Path: path, Body: body, Auth: authz})
}

func num(t *testing.T, body map[string]any, key string) int64 {
	t.Helper()
	v, ok := body[key].(float64)
	if !ok {
		t.Fatalf("响应缺少 %s：%v", key, body)
	}
	return int64(v)
}

func TestMenuEndToEnd(t *testing.T) {
	s := newStack(t)
	admin := s.Bearer(t, "admin", perm.RoleAdmin)
	authorRole := s.Bearer(t, "author", perm.RoleAuthor)

	// 造一篇已发布文章与一个分类，供站内条目引用。
	post := mustStatus(t, req(t, s, http.MethodPost, consolePrefix+"/posts",
		`{"title":"菜单目标文章","raw":"正文","rawType":"markdown"}`, admin), http.StatusCreated)
	postID := num(t, post, "id")
	mustStatus(t, req(t, s, http.MethodPost, consolePrefix+"/posts/"+strconv.FormatInt(postID, 10)+"/publish",
		`{}`, admin), http.StatusOK)

	cat := mustStatus(t, req(t, s, http.MethodPost, consolePrefix+"/categories",
		`{"name":"技术分享"}`, admin), http.StatusCreated)
	catID := num(t, cat, "id")

	var menuPath string

	t.Run("创建菜单并由名称生成 slug", func(t *testing.T) {
		body := mustStatus(t, req(t, s, http.MethodPost, consolePrefix+"/menus",
			`{"name":"主导航"}`, admin), http.StatusCreated)
		if body["slug"] != "主导航" {
			t.Errorf("slug 应保留中文，实际 %v", body["slug"])
		}
		menuPath = consolePrefix + "/menus/" + strconv.FormatInt(num(t, body, "id"), 10)
	})

	t.Run("整体保存条目树并解析站内地址", func(t *testing.T) {
		payload := `{"items":[
			{"label":"首页","type":"custom","url":"/"},
			{"label":"文章","type":"post","targetId":` + strconv.FormatInt(postID, 10) + `,
			 "children":[{"label":"分类","type":"category","targetId":` + strconv.FormatInt(catID, 10) + `}]},
			{"label":"关于","type":"custom","url":"/about","target":"_blank","visible":false}
		]}`
		body := mustStatus(t, req(t, s, http.MethodPut, menuPath+"/items", payload, admin), http.StatusOK)
		if body["total"] != float64(4) {
			t.Fatalf("应有 4 个条目，实际 %v", body["total"])
		}

		items, _ := body["items"].([]any)
		first, _ := items[0].(map[string]any)
		if first["url"] != "/" {
			t.Errorf("自定义链接应保留手填地址，实际 %v", first["url"])
		}
		second, _ := items[1].(map[string]any)
		if url, _ := second["url"].(string); !strings.HasPrefix(url, "/posts/") {
			t.Errorf("文章条目应解析出 /posts/ 地址，实际 %v", second["url"])
		}
		children, _ := second["children"].([]any)
		if len(children) != 1 {
			t.Fatalf("文章条目应挂一个子条目，实际 %d 个", len(children))
		}
		child, _ := children[0].(map[string]any)
		if url, _ := child["url"].(string); !strings.HasPrefix(url, "/categories/") {
			t.Errorf("分类条目应解析出 /categories/ 地址，实际 %v", child["url"])
		}
	})

	t.Run("前台只返回可见条目并解析地址", func(t *testing.T) {
		body := mustStatus(t, req(t, s, http.MethodGet, publicPrefix+"/menus/主导航", "", ""), http.StatusOK)
		items, _ := body["items"].([]any)
		if len(items) != 2 {
			t.Fatalf("隐藏的条目不应出现，顶层应有 2 条，实际 %d", len(items))
		}
		if body["total"] != float64(3) {
			t.Errorf("总数应为 3（不含隐藏项），实际 %v", body["total"])
		}
	})

	t.Run("匿名可读，未知 slug 404", func(t *testing.T) {
		mustStatus(t, req(t, s, http.MethodGet, publicPrefix+"/menus/不存在", "", ""), http.StatusNotFound)
	})

	t.Run("权限：只有 menus:manage 能读写", func(t *testing.T) {
		mustStatus(t, req(t, s, http.MethodGet, consolePrefix+"/menus", "", authorRole), http.StatusForbidden)
		mustStatus(t, req(t, s, http.MethodPost, consolePrefix+"/menus", `{"name":"x"}`, authorRole), http.StatusForbidden)
		mustStatus(t, req(t, s, http.MethodGet, consolePrefix+"/menus", "", ""), http.StatusUnauthorized)
	})

	t.Run("校验：层级、空白标题、缺 targetId", func(t *testing.T) {
		// 恰好三级、第三级没有 children 的合法菜单必须能保存：
		// 递归曾经在检查空数组之前就进入下一层，把这种菜单判成四级而拒绝。
		mustStatus(t, req(t, s, http.MethodPut, menuPath+"/items",
			`{"items":[{"label":"一级","type":"custom","url":"/","children":[{"label":"二级","type":"custom","url":"/a",
			   "children":[{"label":"三级","type":"custom","url":"/b"}]}]}]}`,
			admin), http.StatusOK)

		mustStatus(t, req(t, s, http.MethodPut, menuPath+"/items",
			`{"items":[{"label":"一级","type":"custom","url":"/","children":[{"label":"二级","type":"custom","url":"/a",
			   "children":[{"label":"三级","type":"custom","url":"/b",
			   "children":[{"label":"四级","type":"custom","url":"/c"}]}]}]}]}`,
			admin), http.StatusBadRequest)

		mustStatus(t, req(t, s, http.MethodPut, menuPath+"/items",
			`{"items":[{"label":"   ","type":"custom","url":"/"}]}`, admin), http.StatusBadRequest)

		mustStatus(t, req(t, s, http.MethodPut, menuPath+"/items",
			`{"items":[{"label":"缺目标","type":"post"}]}`, admin), http.StatusBadRequest)

		mustStatus(t, req(t, s, http.MethodPut, menuPath+"/items",
			`{"items":[{"label":"缺地址","type":"custom"}]}`, admin), http.StatusBadRequest)
	})

	t.Run("重新保存会替换整棵树", func(t *testing.T) {
		body := mustStatus(t, req(t, s, http.MethodPut, menuPath+"/items",
			`{"items":[{"label":"只剩一条","type":"custom","url":"/"}]}`, admin), http.StatusOK)
		if body["total"] != float64(1) {
			t.Errorf("重新保存后应只剩 1 条，实际 %v", body["total"])
		}
		// 清空也是合法操作。
		body = mustStatus(t, req(t, s, http.MethodPut, menuPath+"/items", `{"items":[]}`, admin), http.StatusOK)
		if body["total"] != float64(0) {
			t.Errorf("清空后应为 0 条，实际 %v", body["total"])
		}
	})

	t.Run("条目指向未发布内容时前台跳过", func(t *testing.T) {
		draft := mustStatus(t, req(t, s, http.MethodPost, consolePrefix+"/posts",
			`{"title":"草稿","raw":"x","rawType":"markdown"}`, admin), http.StatusCreated)
		draftID := num(t, draft, "id")
		mustStatus(t, req(t, s, http.MethodPut, menuPath+"/items",
			`{"items":[{"label":"草稿条目","type":"post","targetId":`+strconv.FormatInt(draftID, 10)+`}]}`, admin), http.StatusOK)

		body := mustStatus(t, req(t, s, http.MethodGet, publicPrefix+"/menus/主导航", "", ""), http.StatusOK)
		if body["total"] != float64(0) {
			t.Errorf("指向未发布内容的条目应被跳过，实际 %v", body["total"])
		}
	})

	t.Run("删除菜单连带删除条目", func(t *testing.T) {
		id := menuPath[len(consolePrefix+"/menus/"):]
		mustStatus(t, req(t, s, http.MethodDelete, consolePrefix+"/menus/"+id, "", admin), http.StatusNoContent)
		mustStatus(t, req(t, s, http.MethodGet, consolePrefix+"/menus/"+id, "", admin), http.StatusNotFound)
	})
}

// TestReplaceItemsIsSerialized 覆盖并发整树替换：两个事务若都先 DELETE 再各自 INSERT，
// 结果表里可能同时留下两棵树，而两个请求都返回成功。
//
// 起点选空菜单是有意的：对空表的 DELETE 不产生行锁，两个事务因此不会互相阻塞，
// 修复前它们各自的 INSERT 都会落库。修复后事务开头会锁住 menus 父行，
// 同一菜单的替换被串行化，最终只会留下其中一棵树。
func TestReplaceItemsIsSerialized(t *testing.T) {
	s := newStack(t)
	store := menu.NewStore(s.DB.DB)
	ctx := t.Context()

	created := &menu.Menu{Name: "并发菜单", Slug: "并发菜单"}
	if err := store.Create(ctx, created); err != nil {
		t.Fatalf("创建菜单失败: %v", err)
	}

	const writers = 4
	var wg sync.WaitGroup
	errs := make([]error, writers)
	start := make(chan struct{})
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs[i] = store.ReplaceItems(ctx, created.ID, []menu.Item{
				{Label: "唯一一条", Type: menu.TypeCustom, URL: "/", Visible: true},
			}, []int{-1})
		}()
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("第 %d 个写入失败: %v", i, err)
		}
	}

	items, err := store.Items(ctx, created.ID)
	if err != nil {
		t.Fatalf("查询条目失败: %v", err)
	}
	if len(items) != 1 {
		t.Errorf("整树替换必须串行化，条目数 = %d，期望 1（多出来的说明两棵树合并了）", len(items))
	}
}
