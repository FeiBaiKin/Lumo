package favorite_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/FeiBaiKin/lumo/internal/auth/perm"
	"github.com/FeiBaiKin/lumo/internal/content"
	"github.com/FeiBaiKin/lumo/internal/favorite"
	"github.com/FeiBaiKin/lumo/internal/migrate"
	"github.com/FeiBaiKin/lumo/internal/server"
	"github.com/FeiBaiKin/lumo/internal/settings"
	"github.com/FeiBaiKin/lumo/internal/taxonomy"
	"github.com/FeiBaiKin/lumo/internal/testsupport"
)

// 集成测试直连本机 PG 的独立测试库（agent.md §12），本包使用独占 schema。
const testSchema = "lumo_it_favorite"

const (
	consolePrefix = server.PrefixConsole
	publicPrefix  = server.PrefixPublic
)

// newStack 按生产装配顺序装配 settings + taxonomy + content + favorite。
func newStack(t *testing.T) *testsupport.Stack {
	t.Helper()
	set, tax, con, fav := settings.New(), taxonomy.New(), content.New(), favorite.New()
	db := testsupport.Open(t, testsupport.Options{
		Schema:  testSchema,
		Migrate: true,
		Sources: []migrate.Source{
			{Name: set.Name(), FS: set.Migrations()},
			{Name: tax.Name(), FS: tax.Migrations()},
			{Name: con.Name(), FS: con.Migrations()},
			{Name: fav.Name(), FS: fav.Migrations()},
		},
	})
	return testsupport.NewStack(t, db, set, tax, con, fav)
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

func req(t *testing.T, s *testsupport.Stack, method, path, authz string) *httptest.ResponseRecorder {
	t.Helper()
	return s.Do(t, &testsupport.Request{Method: method, Path: path, Auth: authz})
}

// createPost 用 editor 建一篇文章；publish 为真时顺带发布，返回其 ID。
func createPost(t *testing.T, s *testsupport.Stack, bearer, title string, publish bool) int64 {
	t.Helper()
	body := mustStatus(t, s.Do(t, &testsupport.Request{
		Method: http.MethodPost, Path: consolePrefix + "/posts", Auth: bearer,
		Body: `{"title":"` + title + `","raw":"正文","rawType":"markdown"}`,
	}), http.StatusCreated)
	raw, ok := body["id"].(float64)
	if !ok {
		t.Fatalf("建文章的响应里没有 id：%v", body)
	}
	id := int64(raw)
	if publish {
		mustStatus(t, s.Do(t, &testsupport.Request{
			Method: http.MethodPost, Path: postPath(id) + "/publish", Auth: bearer, Body: `{}`,
		}), http.StatusOK)
	}
	return id
}

func postPath(id int64) string {
	return consolePrefix + "/posts/" + strconv.FormatInt(id, 10)
}

func favoritePath(id int64) string {
	return publicPrefix + "/posts/" + strconv.FormatInt(id, 10) + "/favorite"
}

// state 断言一次收藏调用的返回：是否已收藏、总共多少人收藏。
func state(t *testing.T, body map[string]any, favorited bool, count int) {
	t.Helper()
	if body["favorited"] != favorited {
		t.Errorf("favorited = %v，期望 %v", body["favorited"], favorited)
	}
	if body["count"] != float64(count) {
		t.Errorf("count = %v，期望 %d", body["count"], count)
	}
}

func TestFavoriteEndToEnd(t *testing.T) {
	s := newStack(t)
	editor := s.Bearer(t, "editor", perm.RoleEditor)
	member := s.Bearer(t, "member", perm.RoleMember)
	other := s.Bearer(t, "other", perm.RoleMember)

	postID := createPost(t, s, editor, "收藏测试文章", true)
	path := favoritePath(postID)

	t.Run("匿名读得到收藏数但收藏不了", func(t *testing.T) {
		body := mustStatus(t, req(t, s, http.MethodGet, path, ""), http.StatusOK)
		state(t, body, false, 0)

		// 匿名收藏是 401 而不是 403：问题出在「你是谁」，不是「你能不能」。
		mustStatus(t, req(t, s, http.MethodPut, path, ""), http.StatusUnauthorized)
	})

	t.Run("零权限的成员也能收藏", func(t *testing.T) {
		// member 角色一条权限都没有。收藏与发评论同级，是登录用户人人可做的动作，
		// 若这里要求任何权限串，自助注册的账号就全都用不了这个功能。
		body := mustStatus(t, req(t, s, http.MethodPut, path, member), http.StatusOK)
		state(t, body, true, 1)
	})

	t.Run("重复收藏不叠加", func(t *testing.T) {
		body := mustStatus(t, req(t, s, http.MethodPut, path, member), http.StatusOK)
		state(t, body, true, 1)
	})

	t.Run("各人的收藏各自计数", func(t *testing.T) {
		body := mustStatus(t, req(t, s, http.MethodPut, path, other), http.StatusOK)
		state(t, body, true, 2)

		// 另一个人收藏了，不该影响本人的「收没收」。
		body = mustStatus(t, req(t, s, http.MethodGet, path, member), http.StatusOK)
		state(t, body, true, 2)
	})

	t.Run("取消收藏是幂等的", func(t *testing.T) {
		body := mustStatus(t, req(t, s, http.MethodDelete, path, member), http.StatusOK)
		state(t, body, false, 1)

		body = mustStatus(t, req(t, s, http.MethodDelete, path, member), http.StatusOK)
		state(t, body, false, 1)
	})
}

// TestFavoriteHidesUnpublished 验证未发布内容既收藏不了，也不会出现在收藏列表里。
//
// 两条都要：写入时拦住，是不让收藏接口变成一个「探测草稿是否存在」的探针；
// 读出时再过一遍，是因为文章在被收藏**之后**仍然可能撤回——
// 那之后收藏页就成了一扇能看见已下线内容标题的窗。
func TestFavoriteHidesUnpublished(t *testing.T) {
	s := newStack(t)
	editor := s.Bearer(t, "editor", perm.RoleEditor)
	member := s.Bearer(t, "member", perm.RoleMember)

	draftID := createPost(t, s, editor, "草稿", false)
	liveID := createPost(t, s, editor, "已发布", true)

	t.Run("草稿收藏不了且回答与不存在一致", func(t *testing.T) {
		// 404 而不是 403：403 等于承认「有这么一篇，只是你看不到」。
		mustStatus(t, req(t, s, http.MethodPut, favoritePath(draftID), member), http.StatusNotFound)
		mustStatus(t, req(t, s, http.MethodPut, favoritePath(999999), member), http.StatusNotFound)
	})

	t.Run("撤回之后从收藏列表里消失", func(t *testing.T) {
		mustStatus(t, req(t, s, http.MethodPut, favoritePath(liveID), member), http.StatusOK)

		store := favorite.NewStore(s.DB.DB)
		userID := favoriteUserID(t, s, member, liveID)

		ids, total, err := store.FavoritePostIDs(t.Context(), userID, 10, 0)
		if err != nil {
			t.Fatalf("取收藏列表失败: %v", err)
		}
		if total != 1 || len(ids) != 1 || ids[0] != liveID {
			t.Fatalf("收藏列表 = %v（共 %d），期望只有 %d", ids, total, liveID)
		}

		mustStatus(t, s.Do(t, &testsupport.Request{
			Method: http.MethodPost, Path: postPath(liveID) + "/unpublish", Auth: editor, Body: `{}`,
		}), http.StatusOK)

		ids, total, err = store.FavoritePostIDs(t.Context(), userID, 10, 0)
		if err != nil {
			t.Fatalf("取收藏列表失败: %v", err)
		}
		// 总数与列表必须同时归零：显示 0 条却说「共 1 篇」是同一个 WHERE 写漏一处的典型症状。
		if total != 0 || len(ids) != 0 {
			t.Errorf("撤回后仍能看到 %v（共 %d）", ids, total)
		}

		// 但取消收藏仍然要能做：文章下线了，用户还是该能把它从自己的清单里划掉。
		mustStatus(t, req(t, s, http.MethodDelete, favoritePath(liveID), member), http.StatusOK)
	})
}

// TestFavoriteOrdersByRecent 验证收藏列表按收藏时间倒序，而不是按文章发表时间。
//
// 这一页回答的是「我什么时候留下的它」，最近收的该在最前面——
// 按发表时间排的话，收藏一篇三年前的旧文，它会沉到列表底部再也找不到。
func TestFavoriteOrdersByRecent(t *testing.T) {
	s := newStack(t)
	editor := s.Bearer(t, "editor", perm.RoleEditor)
	member := s.Bearer(t, "member", perm.RoleMember)

	first := createPost(t, s, editor, "先发的", true)
	second := createPost(t, s, editor, "后发的", true)

	// 先收后发的那篇，再收先发的那篇：两种排序在这组数据上结果相反。
	mustStatus(t, req(t, s, http.MethodPut, favoritePath(second), member), http.StatusOK)
	mustStatus(t, req(t, s, http.MethodPut, favoritePath(first), member), http.StatusOK)

	store := favorite.NewStore(s.DB.DB)
	userID := favoriteUserID(t, s, member, first)
	ids, total, err := store.FavoritePostIDs(t.Context(), userID, 10, 0)
	if err != nil {
		t.Fatalf("取收藏列表失败: %v", err)
	}
	if total != 2 || len(ids) != 2 {
		t.Fatalf("收藏列表 = %v（共 %d），期望两条", ids, total)
	}
	if ids[0] != first || ids[1] != second {
		t.Errorf("收藏列表 = %v，期望最近收藏的 %d 在前", ids, first)
	}

	t.Run("分页", func(t *testing.T) {
		ids, total, err := store.FavoritePostIDs(t.Context(), userID, 1, 1)
		if err != nil {
			t.Fatalf("取收藏列表失败: %v", err)
		}
		if total != 2 {
			t.Errorf("总数 = %d，期望 2——总数说的是全部，不是本页", total)
		}
		if len(ids) != 1 || ids[0] != second {
			t.Errorf("第二页 = %v，期望 [%d]", ids, second)
		}
	})
}

// favoriteUserID 用「谁收藏了这篇」反查用户 ID。
//
// Stack.Bearer 只给令牌不给用户 ID，而这几条用例要直接调存储层核对排序与可见性。
// 从收藏表反查比再开一条查用户的路子短，且必然对得上——这一行就是那个人写的。
func favoriteUserID(t *testing.T, s *testsupport.Stack, bearer string, postID int64) int64 {
	t.Helper()
	// 先确认这个令牌确实收藏过这一篇，否则下面查出来的是别人的行。
	body := mustStatus(t, req(t, s, http.MethodGet, favoritePath(postID), bearer), http.StatusOK)
	if body["favorited"] != true {
		t.Fatalf("该令牌并未收藏 %d：%v", postID, body)
	}

	var userID int64
	err := s.DB.DB.NewRaw(
		"SELECT user_id FROM favorites WHERE post_id = ? ORDER BY id DESC LIMIT 1", postID,
	).Scan(t.Context(), &userID)
	if err != nil {
		t.Fatalf("反查收藏者失败: %v", err)
	}
	return userID
}
