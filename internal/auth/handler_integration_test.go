package auth_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/auth"
	"github.com/FeiBaiKin/lumo/internal/auth/perm"
	"github.com/FeiBaiKin/lumo/internal/httpx"
	"github.com/FeiBaiKin/lumo/internal/server"
)

// 本文件是认证端点的端到端测试：真实路由 + 真实鉴权中间件 + 真实数据库，
// 验证 huma 操作、Cookie 下发、CSRF、令牌与登出在整条链路上的行为。

const consolePrefix = server.PrefixConsole

// newRouter 组装与 serve 命令一致的路由与认证端点。
func newRouter(t *testing.T, e *env) http.Handler {
	t.Helper()
	root, planes := server.NewRouter(&server.Options{Authenticator: e.authn, Version: "test"})
	auth.NewHandler(e.service, e.sessions, e.tokens, nil).Register(planes.ConsolePublic(), planes.Console())
	// 管理端点也一并挂上：与 serve 一致，且管理接口会用到会话与令牌的失效联动。
	auth.NewAdminHandler(e.users, e.service, func() []auth.PermissionInfo {
		declared := app.CorePermissions()
		out := make([]auth.PermissionInfo, 0, len(declared))
		for _, p := range declared {
			out = append(out, auth.PermissionInfo{Key: p.Key, Label: p.Label, Description: p.Description})
		}
		return out
	}).Register(planes.Console())
	return root
}

// call 是端到端请求的参数。
type call struct {
	method  string
	path    string
	body    string
	cookies []*http.Cookie
	headers map[string]string
}

// do 发起请求并返回记录器。
func (e *env) do(t *testing.T, root http.Handler, c *call) *httptest.ResponseRecorder {
	t.Helper()

	var reader io.Reader = http.NoBody
	if c.body != "" {
		reader = strings.NewReader(c.body)
	}
	req := httptest.NewRequestWithContext(t.Context(), c.method, consolePrefix+c.path, reader)
	if c.body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, ck := range c.cookies {
		req.AddCookie(ck)
	}
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	root.ServeHTTP(rec, req)
	return rec
}

// decode 解析 JSON 响应体。
func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是合法 JSON: %v\n%s", err, rec.Body.String())
	}
	return body
}

// login 登录并返回可复用的 Cookie 与 CSRF 令牌。
func (e *env) login(t *testing.T, root http.Handler, login, pass string) (cookies []*http.Cookie, csrf string) {
	t.Helper()

	rec := e.do(t, root, &call{method: http.MethodPost, path: "/auth/login",
		body: `{"login":"` + login + `","password":"` + pass + `"}`})
	if rec.Code != http.StatusOK {
		t.Fatalf("登录状态码 = %d：%s", rec.Code, rec.Body.String())
	}
	body := decode(t, rec)
	csrf, _ = body["csrfToken"].(string)
	if csrf == "" {
		t.Fatal("登录响应缺少 csrfToken")
	}
	return rec.Result().Cookies(), csrf
}

func TestLoginEndToEnd(t *testing.T) {
	e := newEnv(t)
	root := newRouter(t, e)
	e.createUser(t, "alice", perm.RoleEditor)

	t.Run("登录成功下发会话与 CSRF Cookie", func(t *testing.T) {
		cookies, csrf := e.login(t, root, "alice", testPassword)

		var sessionCookie, csrfCookie *http.Cookie
		for _, ck := range cookies {
			switch ck.Name {
			case e.sessions.SessionCookieName():
				sessionCookie = ck
			case e.sessions.CSRFCookieName():
				csrfCookie = ck
			}
		}
		if sessionCookie == nil || csrfCookie == nil {
			t.Fatalf("Cookie 不完整: %v", cookies)
		}
		if !sessionCookie.HttpOnly {
			t.Error("会话 Cookie 必须为 HttpOnly")
		}
		if csrfCookie.HttpOnly {
			t.Error("CSRF Cookie 须可被前端读取（双提交）")
		}
		if sessionCookie.SameSite != http.SameSiteLaxMode || csrfCookie.SameSite != http.SameSiteLaxMode {
			t.Error("Cookie 应为 SameSite=Lax")
		}
		if csrfCookie.Value != csrf {
			t.Error("CSRF Cookie 与响应体中的 csrfToken 应一致")
		}
	})

	t.Run("错误密码与不存在账号的响应逐字节相同", func(t *testing.T) {
		wrong := e.do(t, root, &call{method: http.MethodPost, path: "/auth/login",
			body: `{"login":"alice","password":"definitely-wrong"}`})
		missing := e.do(t, root, &call{method: http.MethodPost, path: "/auth/login",
			body: `{"login":"nobody","password":"definitely-wrong"}`})
		if wrong.Code != http.StatusUnauthorized || missing.Code != http.StatusUnauthorized {
			t.Fatalf("状态码 = %d / %d，期望均为 401", wrong.Code, missing.Code)
		}
		if wrong.Body.String() != missing.Body.String() {
			t.Errorf("响应体不一致，可用于枚举账号：\n%s\n%s", wrong.Body.String(), missing.Body.String())
		}
		if ct := wrong.Header().Get("Content-Type"); ct != httpx.ContentTypeProblem {
			t.Errorf("Content-Type = %q", ct)
		}
	})

	t.Run("请求体校验失败返回 422 且不回显请求体", func(t *testing.T) {
		rec := e.do(t, root, &call{method: http.MethodPost, path: "/auth/login",
			body: `{"login":"alice","password":"hunter2-secret","extra":1}`})
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("状态码 = %d：%s", rec.Code, rec.Body.String())
		}
		if details, ok := decode(t, rec)["errors"].([]any); !ok || len(details) == 0 {
			t.Error("应返回逐条校验明细")
		}
		// 未知字段属对象级错误，huma 默认会把整个请求体（含密码）放进 value，必须被剥掉。
		if strings.Contains(rec.Body.String(), "hunter2-secret") {
			t.Errorf("校验错误回显了请求体中的密码: %s", rec.Body.String())
		}
	})

	t.Run("停用账号返回 403", func(t *testing.T) {
		user := e.createUser(t, "sleepy", perm.RoleAuthor)
		if err := e.service.Disable(t.Context(), user.ID); err != nil {
			t.Fatalf("停用失败: %v", err)
		}
		rec := e.do(t, root, &call{method: http.MethodPost, path: "/auth/login",
			body: `{"login":"sleepy","password":"` + testPassword + `"}`})
		if rec.Code != http.StatusForbidden {
			t.Fatalf("状态码 = %d，期望 403", rec.Code)
		}
	})
}

func TestSessionFlowEndToEnd(t *testing.T) {
	e := newEnv(t)
	root := newRouter(t, e)
	e.createUser(t, "bob", perm.RoleAuthor)
	cookies, csrf := e.login(t, root, "bob", testPassword)

	t.Run("匿名访问受保护端点返回 401", func(t *testing.T) {
		rec := e.do(t, root, &call{method: http.MethodGet, path: "/auth/me"})
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("状态码 = %d，期望 401", rec.Code)
		}
	})

	t.Run("会话可读取当前用户与有效权限", func(t *testing.T) {
		rec := e.do(t, root, &call{method: http.MethodGet, path: "/auth/me", cookies: cookies})
		if rec.Code != http.StatusOK {
			t.Fatalf("状态码 = %d：%s", rec.Code, rec.Body.String())
		}
		body := decode(t, rec)
		if body["authMethod"] != "session" {
			t.Errorf("authMethod = %v", body["authMethod"])
		}
		perms, _ := body["permissions"].([]any)
		if len(perms) != 3 {
			t.Errorf("author 权限数量 = %d，期望 3：%v", len(perms), perms)
		}
		if strings.Contains(rec.Body.String(), "passwordHash") {
			t.Error("响应不得包含口令哈希")
		}
	})

	t.Run("写请求缺少或错误的 CSRF 头返回 403", func(t *testing.T) {
		for _, header := range []string{"", "wrong"} {
			headers := map[string]string{}
			if header != "" {
				headers[auth.CSRFHeaderName] = header
			}
			rec := e.do(t, root, &call{method: http.MethodPost, path: "/auth/tokens",
				body: `{"name":"x","password":"` + testPassword + `"}`, cookies: cookies, headers: headers})
			if rec.Code != http.StatusForbidden {
				t.Errorf("CSRF 头 %q 时状态码 = %d，期望 403", header, rec.Code)
			}
		}
	})

	var plaintext string
	var tokenID float64
	t.Run("携带 CSRF 头可创建令牌且明文只返回一次", func(t *testing.T) {
		rec := e.do(t, root, &call{method: http.MethodPost, path: "/auth/tokens",
			body: `{"name":"ci","password":"` + testPassword + `","scopes":["posts:write"]}`, cookies: cookies,
			headers: map[string]string{auth.CSRFHeaderName: csrf}})
		if rec.Code != http.StatusCreated {
			t.Fatalf("状态码 = %d：%s", rec.Code, rec.Body.String())
		}
		body := decode(t, rec)
		plaintext, _ = body["plaintext"].(string)
		if !strings.HasPrefix(plaintext, auth.TokenPrefix) {
			t.Fatalf("令牌明文 = %q，应以 %s 开头", plaintext, auth.TokenPrefix)
		}
		token, _ := body["token"].(map[string]any)
		tokenID, _ = token["id"].(float64)
		if tokenID == 0 {
			t.Fatalf("令牌 ID 缺失: %v", body)
		}
		if _, leaked := token["tokenHash"]; leaked {
			t.Error("响应不得包含令牌哈希")
		}

		list := e.do(t, root, &call{method: http.MethodGet, path: "/auth/tokens", cookies: cookies})
		if list.Code != http.StatusOK || strings.Contains(list.Body.String(), plaintext) {
			t.Errorf("列表状态码 = %d，且不得再次出现明文", list.Code)
		}
	})

	t.Run("未知 scope 返回 400、未知字段返回 422", func(t *testing.T) {
		bad := e.do(t, root, &call{method: http.MethodPost, path: "/auth/tokens",
			body: `{"name":"x","password":"` + testPassword + `","scopes":["posts:fly"]}`, cookies: cookies,
			headers: map[string]string{auth.CSRFHeaderName: csrf}})
		if bad.Code != http.StatusBadRequest {
			t.Errorf("未知 scope 状态码 = %d，期望 400", bad.Code)
		}
		unknown := e.do(t, root, &call{method: http.MethodPost, path: "/auth/tokens",
			body: `{"name":"x","password":"` + testPassword + `","bogus":true}`, cookies: cookies,
			headers: map[string]string{auth.CSRFHeaderName: csrf}})
		if unknown.Code != http.StatusUnprocessableEntity {
			t.Errorf("未知字段状态码 = %d，期望 422", unknown.Code)
		}
	})

	t.Run("令牌可无 Cookie 调用且不需 CSRF", func(t *testing.T) {
		bearer := map[string]string{"Authorization": "Bearer " + plaintext}
		rec := e.do(t, root, &call{method: http.MethodGet, path: "/auth/me", headers: bearer})
		if rec.Code != http.StatusOK {
			t.Fatalf("状态码 = %d：%s", rec.Code, rec.Body.String())
		}
		body := decode(t, rec)
		if body["authMethod"] != "token" {
			t.Errorf("authMethod = %v", body["authMethod"])
		}
		// scope 已收窄到 posts:write。
		if perms, _ := body["permissions"].([]any); len(perms) != 1 || perms[0] != "posts:write" {
			t.Errorf("令牌有效权限 = %v，期望 [posts:write]", perms)
		}

		del := e.do(t, root, &call{method: http.MethodDelete,
			path:    "/auth/tokens/" + strconv.FormatInt(int64(tokenID), 10),
			headers: bearer})
		if del.Code != http.StatusNoContent {
			t.Fatalf("撤销状态码 = %d：%s", del.Code, del.Body.String())
		}
		again := e.do(t, root, &call{method: http.MethodGet, path: "/auth/me", headers: bearer})
		if again.Code != http.StatusUnauthorized {
			t.Errorf("已撤销令牌仍可用：状态码 = %d", again.Code)
		}
		missing := e.do(t, root, &call{method: http.MethodDelete, path: "/auth/tokens/999999",
			cookies: cookies, headers: map[string]string{auth.CSRFHeaderName: csrf}})
		if missing.Code != http.StatusNotFound {
			t.Errorf("撤销不存在的令牌状态码 = %d，期望 404", missing.Code)
		}
	})

	t.Run("登出后会话失效并清除 Cookie", func(t *testing.T) {
		rec := e.do(t, root, &call{method: http.MethodPost, path: "/auth/logout",
			cookies: cookies, headers: map[string]string{auth.CSRFHeaderName: csrf}})
		if rec.Code != http.StatusNoContent {
			t.Fatalf("状态码 = %d：%s", rec.Code, rec.Body.String())
		}
		cleared := false
		for _, ck := range rec.Result().Cookies() {
			if ck.Name == e.sessions.SessionCookieName() && ck.MaxAge < 0 {
				cleared = true
			}
		}
		if !cleared {
			t.Error("登出应清除会话 Cookie")
		}
		after := e.do(t, root, &call{method: http.MethodGet, path: "/auth/me", cookies: cookies})
		if after.Code != http.StatusUnauthorized {
			t.Errorf("登出后旧会话仍可用：状态码 = %d", after.Code)
		}
	})
}

// TestTokenIssuanceIsNotPrivilegeEscalation 是审查发现的提权路径的回归测试：
// 一枚只有 posts:write 的受限 PAT 曾经可以调用签发接口，创建一枚空 scopes 的新令牌，
// 而空 scopes 在当时被解释为「继承账号全部权限」——于是受限令牌一步变成全权限令牌。
func TestTokenIssuanceIsNotPrivilegeEscalation(t *testing.T) {
	e := newEnv(t)
	root := newRouter(t, e)
	user := e.createUser(t, "dave", perm.RoleAuthor)
	cookies, csrf := e.login(t, root, "dave", testPassword)
	sessionHeaders := map[string]string{auth.CSRFHeaderName: csrf}

	// 只有 posts:write 的受限令牌。
	restricted, err := e.tokens.Create(t.Context(), &auth.CreateTokenParams{
		UserID: user.ID,
		Name:   "restricted",
		Scopes: []perm.Permission{perm.PostsWrite},
	})
	if err != nil {
		t.Fatalf("签发受限令牌失败: %v", err)
	}
	bearer := map[string]string{"Authorization": "Bearer " + restricted.Plaintext}

	t.Run("受限 PAT 不能签发新令牌", func(t *testing.T) {
		rec := e.do(t, root, &call{method: http.MethodPost, path: "/auth/tokens",
			body: `{"name":"escalated","password":"` + testPassword + `"}`, headers: bearer})
		if rec.Code != http.StatusForbidden {
			t.Fatalf("状态码 = %d，期望 403：%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("空 scopes 的令牌没有任何权限", func(t *testing.T) {
		// 绕过处理器直接落库，覆盖「历史数据或直接被改空」的情形：
		// 空 scopes 必须按无权限解释，而不是账号全权限。
		empty, createErr := e.tokens.Create(t.Context(), &auth.CreateTokenParams{
			UserID: user.ID,
			Name:   "legacy-empty",
		})
		if createErr != nil {
			t.Fatalf("签发空 scopes 令牌失败: %v", createErr)
		}
		rec := e.do(t, root, &call{method: http.MethodGet, path: "/auth/me",
			headers: map[string]string{"Authorization": "Bearer " + empty.Plaintext}})
		if rec.Code != http.StatusOK {
			t.Fatalf("状态码 = %d：%s", rec.Code, rec.Body.String())
		}
		if perms, _ := decode(t, rec)["permissions"].([]any); len(perms) != 0 {
			t.Errorf("空 scopes 令牌的有效权限 = %v，期望为空", perms)
		}
	})

	t.Run("签发令牌须重新校验密码", func(t *testing.T) {
		rec := e.do(t, root, &call{method: http.MethodPost, path: "/auth/tokens",
			body: `{"name":"x","password":"wrong-password"}`, cookies: cookies, headers: sessionHeaders})
		if rec.Code != http.StatusForbidden {
			t.Fatalf("密码错误时状态码 = %d，期望 403：%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("scope 不得超出账号权限", func(t *testing.T) {
		rec := e.do(t, root, &call{method: http.MethodPost, path: "/auth/tokens",
			body:    `{"name":"x","password":"` + testPassword + `","scopes":["users:manage"]}`,
			cookies: cookies, headers: sessionHeaders})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("越权 scope 状态码 = %d，期望 400：%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("省略 scopes 时展开为账号权限的显式清单", func(t *testing.T) {
		rec := e.do(t, root, &call{method: http.MethodPost, path: "/auth/tokens",
			body: `{"name":"full","password":"` + testPassword + `"}`, cookies: cookies, headers: sessionHeaders})
		if rec.Code != http.StatusCreated {
			t.Fatalf("状态码 = %d：%s", rec.Code, rec.Body.String())
		}
		body := decode(t, rec)
		token, _ := body["token"].(map[string]any)
		scopes, _ := token["scopes"].([]any)
		if len(scopes) != len(perm.BuiltinRoles[perm.RoleAuthor]) {
			t.Errorf("落库 scope = %v，期望与 author 的权限清单等长", scopes)
		}
		// 空 scopes 不再落库：这条不变量保证鉴权侧「空即无权限」永远安全。
		if len(scopes) == 0 {
			t.Error("scope 不应以空数组落库")
		}
	})
}

func TestChangePasswordEndToEnd(t *testing.T) {
	e := newEnv(t)
	root := newRouter(t, e)
	e.createUser(t, "carol", perm.RoleEditor)
	cookies, csrf := e.login(t, root, "carol", testPassword)
	headers := map[string]string{auth.CSRFHeaderName: csrf}

	t.Run("原密码错误返回 401", func(t *testing.T) {
		rec := e.do(t, root, &call{method: http.MethodPost, path: "/auth/change-password",
			body:    `{"oldPassword":"nope-nope-nope","newPassword":"` + otherPass + `"}`,
			cookies: cookies, headers: headers})
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("状态码 = %d，期望 401", rec.Code)
		}
	})

	t.Run("新密码过短返回 422", func(t *testing.T) {
		rec := e.do(t, root, &call{method: http.MethodPost, path: "/auth/change-password",
			body:    `{"oldPassword":"` + testPassword + `","newPassword":"short"}`,
			cookies: cookies, headers: headers})
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("状态码 = %d，期望 422", rec.Code)
		}
	})

	t.Run("修改成功后旧会话失效且新密码可登录", func(t *testing.T) {
		rec := e.do(t, root, &call{method: http.MethodPost, path: "/auth/change-password",
			body:    `{"oldPassword":"` + testPassword + `","newPassword":"` + otherPass + `"}`,
			cookies: cookies, headers: headers})
		if rec.Code != http.StatusNoContent {
			t.Fatalf("状态码 = %d：%s", rec.Code, rec.Body.String())
		}
		stale := e.do(t, root, &call{method: http.MethodGet, path: "/auth/me", cookies: cookies})
		if stale.Code != http.StatusUnauthorized {
			t.Errorf("改密后旧会话仍可用：状态码 = %d", stale.Code)
		}
		e.login(t, root, "carol", otherPass)
	})
}

// TestConsoleLoginRejectsUnverifiedEmail 验证 Console 登录同样被邮箱验证闸门拦下。
//
// 这是闸门放在 Service.Login 而不是前台处理器里的**关键验收**：如果只拦前台，
// 一个未验证的账号可以改从 /console/auth/login 登录，拿到会话 Cookie 之后
// 前台照样认它——闸门就成了摆设。
func TestConsoleLoginRejectsUnverifiedEmail(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	root := newRouter(t, e)

	if _, err := e.users.CreateUser(ctx, &auth.CreateUserParams{
		Username: "pending",
		Email:    "pending@example.com",
		Password: testPassword,
		Roles:    []string{perm.RoleMember},
	}); err != nil {
		t.Fatalf("创建用户失败: %v", err)
	}

	rec := e.do(t, root, &call{
		method: http.MethodPost,
		path:   "/auth/login",
		body:   `{"login":"pending","password":"` + testPassword + `"}`,
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("未验证账号登录 Console 状态码 = %d，期望 403：%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Header().Get("Set-Cookie"), e.sessions.SessionCookieName()) {
		t.Error("被拒的登录不该下发会话 Cookie")
	}
}
