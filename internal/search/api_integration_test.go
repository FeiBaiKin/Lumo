package search_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/FeiBaiKin/lumo/internal/auth/perm"
	"github.com/FeiBaiKin/lumo/internal/content"
	"github.com/FeiBaiKin/lumo/internal/migrate"
	"github.com/FeiBaiKin/lumo/internal/search"
	"github.com/FeiBaiKin/lumo/internal/server"
	"github.com/FeiBaiKin/lumo/internal/settings"
	"github.com/FeiBaiKin/lumo/internal/taxonomy"
	"github.com/FeiBaiKin/lumo/internal/testsupport"
)

// 本文件走整机装配，与白盒测试分用不同 schema，避免同一个测试二进制里互相清空。
const apiSchema = "lumo_it_search_api"

const (
	consolePrefix = server.PrefixConsole
	publicPrefix  = server.PrefixPublic
)

// indexWait 是等待后台对账完成的上限；worker 每 2 秒一轮。
const indexWait = 20 * time.Second

func newStack(t *testing.T) *testsupport.Stack {
	t.Helper()

	set, tax, con, sea := settings.New(), taxonomy.New(), content.New(), search.New()
	db := testsupport.Open(t, testsupport.Options{
		Schema:  apiSchema,
		Migrate: true,
		Sources: []migrate.Source{
			{Name: set.Name(), FS: set.Migrations()},
			{Name: tax.Name(), FS: tax.Migrations()},
			{Name: con.Name(), FS: con.Migrations()},
			{Name: sea.Name(), FS: sea.Migrations()},
		},
	})
	return testsupport.NewStack(t, db, set, tax, con, sea)
}

func req(t *testing.T, s *testsupport.Stack, method, path, body, authz string) *httptest.ResponseRecorder {
	t.Helper()
	return s.Do(t, &testsupport.Request{Method: method, Path: path, Body: body, Auth: authz})
}

func mustStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) map[string]any {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("状态码 = %d，期望 %d：%s", rec.Code, want, rec.Body.String())
	}
	if rec.Body.Len() == 0 {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("响应不是合法 JSON: %v\n%s", err, rec.Body.String())
	}
	return out
}

func total(t *testing.T, body map[string]any) int {
	t.Helper()
	v, ok := body["total"].(float64)
	if !ok {
		t.Fatalf("响应缺少 total：%v", body)
	}
	return int(v)
}

// publishPost 创建并发布一篇文章，返回 ID。
func publishPost(t *testing.T, s *testsupport.Stack, authz, title, slug string) int64 {
	t.Helper()

	created := mustStatus(t, req(t, s, http.MethodPost, consolePrefix+"/posts",
		`{"title":"`+title+`","slug":"`+slug+`","raw":"<p>`+title+`</p>"}`, authz), http.StatusCreated)
	id, ok := created["id"].(float64)
	if !ok {
		t.Fatalf("创建响应缺少 id：%v", created)
	}
	mustStatus(t, req(t, s, http.MethodPost,
		consolePrefix+"/posts/"+strconv.FormatInt(int64(id), 10)+"/publish", `{}`, authz), http.StatusOK)
	return int64(id)
}

// waitForHits 等到搜索命中指定条数，或超时失败。
//
// 索引由后台对账维护，写完内容不会当场可搜；这正是该方案的取舍，
// 测试等它一轮，也顺带证明 worker 确实被 Start 起来了。
func waitForHits(t *testing.T, s *testsupport.Stack, query string, want int) map[string]any {
	t.Helper()

	deadline := time.Now().Add(indexWait)
	var last map[string]any
	for time.Now().Before(deadline) {
		body := mustStatus(t, req(t, s, http.MethodGet,
			publicPrefix+"/search?q="+query, "", ""), http.StatusOK)
		last = body
		if total(t, body) == want {
			return body
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("等待索引超时：搜索 %q 得到 %v，期望 %d 条", query, last, want)
	return nil
}

// TestSearchEndToEnd 走完整链路：发文 → 后台索引 → 匿名搜到。
func TestSearchEndToEnd(t *testing.T) {
	s := newStack(t)
	admin := s.Bearer(t, "admin", perm.RoleAdmin)

	publishPost(t, s, admin, "海豚观察笔记", "dolphin")
	publishPost(t, s, admin, "鲸鱼观察笔记", "whale")

	body := waitForHits(t, s, "海豚", 1)
	items, ok := body["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("items = %v", body["items"])
	}
	hit, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("条目不是对象：%v", items[0])
	}
	if hit["title"] != "海豚观察笔记" || hit["slug"] != "dolphin" || hit["type"] != "post" {
		t.Fatalf("命中内容不对：%v", hit)
	}
	if score, isNum := hit["score"].(float64); !isNum || score <= 0 {
		t.Fatalf("缺少相关度：%v", hit)
	}

	// 两篇都含「观察笔记」，说明索引覆盖到了后写入的那篇。
	waitForHits(t, s, "观察笔记", 2)
}

// TestSearchUnpublishedNotIndexed 确认草稿不进搜索结果。
func TestSearchUnpublishedNotIndexed(t *testing.T) {
	s := newStack(t)
	admin := s.Bearer(t, "admin", perm.RoleAdmin)

	// 只创建不发布：内容仍会被索引，但前台过滤把它挡在外面。
	mustStatus(t, req(t, s, http.MethodPost, consolePrefix+"/posts",
		`{"title":"未发布的穿山甲","slug":"pangolin","raw":"<p>草稿</p>"}`, admin), http.StatusCreated)
	publishPost(t, s, admin, "已发布的穿山甲", "pangolin-published")

	waitForHits(t, s, "穿山甲", 1)
}

// TestSearchQueryParams 覆盖入参校验与匿名可用。
func TestSearchQueryParams(t *testing.T) {
	s := newStack(t)

	t.Run("匿名可搜", func(t *testing.T) {
		mustStatus(t, req(t, s, http.MethodGet, publicPrefix+"/search?q=test", "", ""), http.StatusOK)
	})

	t.Run("空关键词返回空结果", func(t *testing.T) {
		body := mustStatus(t, req(t, s, http.MethodGet, publicPrefix+"/search?q=", "", ""), http.StatusOK)
		if got := total(t, body); got != 0 {
			t.Fatalf("total = %d，期望 0", got)
		}
	})

	t.Run("内容类型不合法", func(t *testing.T) {
		mustStatus(t, req(t, s, http.MethodGet,
			publicPrefix+"/search?q=x&type=comment", "", ""), http.StatusUnprocessableEntity)
	})
}

// TestSearchAdminEndpoints 覆盖索引进度与重建的权限门禁。
func TestSearchAdminEndpoints(t *testing.T) {
	s := newStack(t)
	admin := s.Bearer(t, "admin", perm.RoleAdmin)
	author := s.Bearer(t, "author", perm.RoleAuthor)

	publishPost(t, s, admin, "索引进度测试", "stats")
	waitForHits(t, s, "索引进度", 1)

	stats := mustStatus(t, req(t, s, http.MethodGet, consolePrefix+"/search/status", "", admin), http.StatusOK)
	if stats["total"].(float64) != 1 || stats["indexed"].(float64) != 1 {
		t.Fatalf("索引进度 = %v", stats)
	}

	rebuilt := mustStatus(t, req(t, s, http.MethodPost, consolePrefix+"/search/reindex", "", admin), http.StatusOK)
	if rebuilt["pending"].(float64) != 1 {
		t.Fatalf("重建响应 = %v", rebuilt)
	}
	// 重建后后台会把索引补回来，内容依旧搜得到。
	waitForHits(t, s, "索引进度", 1)

	t.Run("维护端点需要 settings:manage", func(t *testing.T) {
		mustStatus(t, req(t, s, http.MethodGet, consolePrefix+"/search/status", "", author), http.StatusForbidden)
		mustStatus(t, req(t, s, http.MethodPost, consolePrefix+"/search/reindex", "", author), http.StatusForbidden)
		mustStatus(t, req(t, s, http.MethodGet, consolePrefix+"/search/status", "", ""), http.StatusUnauthorized)
	})
}
