package auth_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/FeiBaiKin/lumo/internal/auth"
	"github.com/FeiBaiKin/lumo/internal/auth/perm"
)

// 本文件是用户与角色管理端点的端到端测试：复用本包已有的整机装配
// （newEnv + newRouter），路由与 serve 命令一致。

// bearer 创建一个用户并签发访问令牌，返回可直接放入 Authorization 头的值。
func (e *env) bearer(t *testing.T, username, role string) string {
	t.Helper()
	user := e.createUser(t, username, role)
	issued, err := e.tokens.Create(t.Context(), &auth.CreateTokenParams{UserID: user.ID, Name: "test"})
	if err != nil {
		t.Fatalf("签发令牌失败: %v", err)
	}
	return "Bearer " + issued.Plaintext
}

// doRaw 与 env.do 的区别是路径不再拼前缀，供已带前缀的调用使用。
func doRaw(t *testing.T, root http.Handler, method, path, body, authz string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if authz != "" {
		req.Header.Set("Authorization", authz)
	}
	rec := httptest.NewRecorder()
	root.ServeHTTP(rec, req)
	return rec
}

func expect(t *testing.T, rec *httptest.ResponseRecorder, want int) map[string]any {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("状态码 = %d，期望 %d：%s", rec.Code, want, rec.Body.String())
	}
	if rec.Body.Len() == 0 {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("响应不是合法 JSON: %v | %s", err, rec.Body.String())
	}
	return out
}

// TestUserAdminEndToEnd 覆盖用户与角色管理端点的关键路径与几条自锁防护。
func TestUserAdminEndToEnd(t *testing.T) {
	e := newEnv(t)
	root := newRouter(t, e)
	admin := e.bearer(t, "admin", perm.RoleAdmin)
	editor := e.bearer(t, "editor", perm.RoleEditor)

	var userID string

	t.Run("创建用户不复述口令", func(t *testing.T) {
		body := expect(t, doRaw(t, root, http.MethodPost, consolePrefix+"/users",
			`{"username":"writer","email":"writer@example.com","password":"writer-pass-123","roles":["author"],"displayName":"写手"}`,
			admin), http.StatusCreated)

		userID = strconv.FormatInt(int64(body["id"].(float64)), 10)
		if body["username"] != "writer" || body["displayName"] != "写手" {
			t.Errorf("字段未落库：%v", body)
		}
		// 口令与哈希都不该出现在响应里。
		if _, leaked := body["password"]; leaked {
			t.Error("响应不应含 password")
		}
		if _, leaked := body["passwordHash"]; leaked {
			t.Error("响应不应含 passwordHash")
		}
		roles, _ := body["roles"].([]any)
		if len(roles) != 1 {
			t.Fatalf("应带一个角色，实际 %v", body["roles"])
		}
		if r, _ := roles[0].(map[string]any); r["name"] != "author" {
			t.Errorf("角色应为 author，实际 %v", r["name"])
		}
	})

	t.Run("校验：重复用户名、非法角色、口令过短", func(t *testing.T) {
		expect(t, doRaw(t, root, http.MethodPost, consolePrefix+"/users",
			`{"username":"writer","email":"other@example.com","password":"another-pass-123","roles":["author"]}`,
			admin), http.StatusConflict)
		expect(t, doRaw(t, root, http.MethodPost, consolePrefix+"/users",
			`{"username":"ghost","email":"ghost@example.com","password":"ghost-pass-123","roles":["nope"]}`,
			admin), http.StatusBadRequest)
		expect(t, doRaw(t, root, http.MethodPost, consolePrefix+"/users",
			`{"username":"short","email":"s@example.com","password":"abc","roles":["author"]}`,
			admin), http.StatusUnprocessableEntity)
		expect(t, doRaw(t, root, http.MethodPost, consolePrefix+"/users",
			`{"username":"norole","email":"n@example.com","password":"no-role-pass-123","roles":[]}`,
			admin), http.StatusBadRequest)
	})

	t.Run("列表可按角色与关键词筛选", func(t *testing.T) {
		all := expect(t, doRaw(t, root, http.MethodGet, consolePrefix+"/users", "", admin), http.StatusOK)
		if all["total"] != float64(3) {
			t.Errorf("应有 3 名用户，实际 %v", all["total"])
		}
		authors := expect(t, doRaw(t, root, http.MethodGet, consolePrefix+"/users?role=author", "", admin), http.StatusOK)
		if authors["total"] != float64(1) {
			t.Errorf("author 角色应有 1 人，实际 %v", authors["total"])
		}
		hits := expect(t, doRaw(t, root, http.MethodGet, consolePrefix+"/users?q=writer", "", admin), http.StatusOK)
		if hits["total"] != float64(1) {
			t.Errorf("按用户名筛选应命中 1 人，实际 %v", hits["total"])
		}
		// 列表里同样不含口令哈希。
		raw, _ := json.Marshal(all)
		if strings.Contains(string(raw), "passwordHash") || strings.Contains(string(raw), "$argon2id$") {
			t.Errorf("列表响应泄漏了口令哈希：%s", raw)
		}
	})

	t.Run("改资料、停用与恢复", func(t *testing.T) {
		body := expect(t, doRaw(t, root, http.MethodPut, consolePrefix+"/users/"+userID,
			`{"email":"writer2@example.com","displayName":"改过的名字","bio":"简介"}`, admin), http.StatusOK)
		if body["displayName"] != "改过的名字" {
			t.Errorf("显示名未更新：%v", body["displayName"])
		}

		expect(t, doRaw(t, root, http.MethodPut, consolePrefix+"/users/"+userID+"/status",
			`{"disabled":true}`, admin), http.StatusOK)
		disabled := expect(t, doRaw(t, root, http.MethodGet, consolePrefix+"/users?status=disabled", "", admin), http.StatusOK)
		if disabled["total"] != float64(1) {
			t.Errorf("停用筛选应命中 1 人，实际 %v", disabled["total"])
		}
		expect(t, doRaw(t, root, http.MethodPut, consolePrefix+"/users/"+userID+"/status",
			`{"disabled":false}`, admin), http.StatusOK)
	})

	t.Run("重置口令后旧凭据立即失效", func(t *testing.T) {
		// 给 writer 签发一个令牌，重置口令后它必须失效。
		writer, err := e.users.FindUserByLogin(t.Context(), "writer")
		if err != nil {
			t.Fatalf("查用户失败: %v", err)
		}
		issued, err := e.tokens.Create(t.Context(), &auth.CreateTokenParams{UserID: writer.ID, Name: "probe"})
		if err != nil {
			t.Fatalf("签发令牌失败: %v", err)
		}
		writerToken := "Bearer " + issued.Plaintext
		if rec := doRaw(t, root, http.MethodGet, consolePrefix+"/auth/me", "", writerToken); rec.Code != http.StatusOK {
			t.Fatalf("重置前令牌应可用，实际 %d", rec.Code)
		}

		expect(t, doRaw(t, root, http.MethodPut, consolePrefix+"/users/"+userID+"/password",
			`{"password":"brand-new-pass-456"}`, admin), http.StatusOK)

		if rec := doRaw(t, root, http.MethodGet, consolePrefix+"/auth/me", "", writerToken); rec.Code != http.StatusUnauthorized {
			t.Errorf("重置后旧令牌应失效，实际 %d", rec.Code)
		}
	})

	t.Run("自锁防护：不能停用/删除自己，不能摘掉最后一名管理员", func(t *testing.T) {
		p, _ := e.users.FindUserByLogin(t.Context(), "admin")
		selfID := strconv.FormatInt(p.ID, 10)

		expect(t, doRaw(t, root, http.MethodPut, consolePrefix+"/users/"+selfID+"/status",
			`{"disabled":true}`, admin), http.StatusConflict)
		expect(t, doRaw(t, root, http.MethodDelete, consolePrefix+"/users/"+selfID, "", admin), http.StatusConflict)
		expect(t, doRaw(t, root, http.MethodPut, consolePrefix+"/users/"+selfID+"/roles",
			`{"roles":["author"]}`, admin), http.StatusConflict)
	})

	t.Run("权限：只有 users:manage 能管用户", func(t *testing.T) {
		expect(t, doRaw(t, root, http.MethodGet, consolePrefix+"/users", "", editor), http.StatusForbidden)
		expect(t, doRaw(t, root, http.MethodGet, consolePrefix+"/users", "", ""), http.StatusUnauthorized)
	})

	t.Run("角色：内置不可改删，自定义可增删改", func(t *testing.T) {
		list := expect(t, doRaw(t, root, http.MethodGet, consolePrefix+"/roles", "", admin), http.StatusOK)
		items, _ := list["items"].([]any)
		if len(items) != 4 {
			t.Fatalf("应有 4 个内置角色，实际 %d", len(items))
		}

		created := expect(t, doRaw(t, root, http.MethodPost, consolePrefix+"/roles",
			`{"name":"reviewer","label":"审核员","permissions":["posts:write","comments:manage"]}`,
			admin), http.StatusCreated)
		roleID := strconv.FormatInt(int64(created["id"].(float64)), 10)
		if created["builtin"] != false {
			t.Error("自定义角色的 builtin 应为 false")
		}

		// 权限串拼错要报错，不能静默变成无效权限。
		expect(t, doRaw(t, root, http.MethodPost, consolePrefix+"/roles",
			`{"name":"typo","permissions":["posts:write_anyy"]}`, admin), http.StatusBadRequest)
		expect(t, doRaw(t, root, http.MethodPost, consolePrefix+"/roles",
			`{"name":"reviewer","permissions":[]}`, admin), http.StatusConflict)

		// 内置角色不可改、不可删。
		var editorRoleID string
		for _, raw := range items {
			item, _ := raw.(map[string]any)
			if item["name"] == "editor" {
				editorRoleID = strconv.FormatInt(int64(item["id"].(float64)), 10)
			}
		}
		if editorRoleID == "" {
			t.Fatal("未找到 editor 角色")
		}
		expect(t, doRaw(t, root, http.MethodPut, consolePrefix+"/roles/"+editorRoleID,
			`{"label":"x","permissions":[]}`, admin), http.StatusConflict)
		expect(t, doRaw(t, root, http.MethodDelete, consolePrefix+"/roles/"+editorRoleID, "", admin), http.StatusConflict)

		// 自定义角色可改。
		expect(t, doRaw(t, root, http.MethodPut, consolePrefix+"/roles/"+roleID,
			`{"label":"高级审核员","permissions":["posts:write","posts:publish"]}`, admin), http.StatusOK)

		// 有人持有时不可删。
		expect(t, doRaw(t, root, http.MethodPut, consolePrefix+"/users/"+userID+"/roles",
			`{"roles":["reviewer"]}`, admin), http.StatusOK)
		expect(t, doRaw(t, root, http.MethodDelete, consolePrefix+"/roles/"+roleID, "", admin), http.StatusConflict)

		// 摘掉持有者后即可删除。
		expect(t, doRaw(t, root, http.MethodPut, consolePrefix+"/users/"+userID+"/roles",
			`{"roles":["author"]}`, admin), http.StatusOK)
		expect(t, doRaw(t, root, http.MethodDelete, consolePrefix+"/roles/"+roleID, "", admin), http.StatusNoContent)
	})

	t.Run("权限清单含模块声明的中文名", func(t *testing.T) {
		body := expect(t, doRaw(t, root, http.MethodGet, consolePrefix+"/permissions", "", admin), http.StatusOK)
		items, _ := body["items"].([]any)
		if len(items) != len(perm.All) {
			t.Fatalf("应列出全部权限，期望 %d 条，实际 %d", len(perm.All), len(items))
		}
		found := map[string]string{}
		for _, raw := range items {
			item, _ := raw.(map[string]any)
			key, _ := item["key"].(string)
			label, _ := item["label"].(string)
			found[key] = label
		}
		// 核心自己引入的权限（用户、角色、主题、站点）有中文名；
		// 本包没有装配业务模块，模块权限未声明，故退回权限串本身——这比隐藏它安全。
		if found["users:manage"] != "管理用户" {
			t.Errorf("users:manage 的中文名不对：%q", found["users:manage"])
		}
		if found["roles:manage"] != "管理角色" {
			t.Errorf("roles:manage 的中文名不对：%q", found["roles:manage"])
		}
		if found["posts:write"] == "" {
			t.Error("未声明的权限也应以权限串本身出现在清单里")
		}
	})

	t.Run("删除用户", func(t *testing.T) {
		expect(t, doRaw(t, root, http.MethodDelete, consolePrefix+"/users/"+userID, "", admin), http.StatusNoContent)
		expect(t, doRaw(t, root, http.MethodGet, consolePrefix+"/users/"+userID, "", admin), http.StatusNotFound)
	})
}
