package auth_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/FeiBaiKin/lumo/internal/auth"
	"github.com/FeiBaiKin/lumo/internal/auth/perm"
	"github.com/FeiBaiKin/lumo/internal/database"
	"github.com/FeiBaiKin/lumo/internal/testsupport"
)

// 集成测试直连本机 PG 的独立测试库（agent.md §12）。
//
// 本包使用独占 schema 做隔离：go test ./... 会并行执行多个包，
// 若共用 public schema，各包的清空操作会互删对方的表。
const testSchema = "lumo_it_auth"

// 测试用常量。口令满足 password.MinLength。
const (
	testPassword = "test-password-123"
	otherPass    = "another-password-456"
)

// env 是一次集成测试的全部依赖。
type env struct {
	db       *database.DB
	users    *auth.Store
	sessions *auth.SessionStore
	tokens   *auth.TokenStore
	service  *auth.Service
	authn    *auth.Authenticator
}

// newEnv 准备干净的独占 schema 与已迁移的表结构，返回装配好的依赖。
func newEnv(t *testing.T) *env {
	t.Helper()

	ctx := context.Background()
	db := testsupport.Open(t, testsupport.Options{Schema: testSchema, Migrate: true})

	e := &env{
		db:       db,
		users:    auth.NewStore(db.DB),
		sessions: auth.NewSessionStore(db.DB, false),
		tokens:   auth.NewTokenStore(db.DB),
	}
	e.service = auth.NewService(e.users, e.sessions, e.tokens, nil)
	e.authn = auth.NewAuthenticator(e.users, e.sessions, e.tokens, nil)

	// 内置角色每次启动都会播种，测试同样需要。
	if err := e.users.SeedRoles(ctx); err != nil {
		t.Fatalf("写入内置角色失败: %v", err)
	}
	return e
}

// createUser 是创建测试用户的便捷封装。
func (e *env) createUser(t *testing.T, username, role string) *auth.User {
	t.Helper()

	user, err := e.users.CreateUser(context.Background(), &auth.CreateUserParams{
		Username: username,
		Email:    username + "@example.com",
		Password: testPassword,
		Roles:    []string{role},
		// 测试用户走「后台建号」这条路：登录闸门要求邮箱已验证，
		// 而信箱验证本身另有专门的用例覆盖（见 TestLoginRejectsUnverifiedEmail）。
		EmailVerified: true,
	})
	if err != nil {
		t.Fatalf("创建用户 %s 失败: %v", username, err)
	}
	return user
}

// ---------- 角色 ----------

func TestSeedRoles(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	roles, err := e.users.ListRoles(ctx)
	if err != nil {
		t.Fatalf("查询角色失败: %v", err)
	}
	if len(roles) != len(perm.BuiltinRoleNames) {
		t.Fatalf("内置角色数量 = %d，期望 %d", len(roles), len(perm.BuiltinRoleNames))
	}

	for _, role := range roles {
		if !role.Builtin {
			t.Errorf("角色 %s 应标记为内置", role.Name)
		}
		// 库中权限必须与代码定义一致。
		want := perm.NewSet(perm.BuiltinRoles[role.Name]...)
		got := perm.NewSet(role.Permissions...)
		if len(want) != len(got) {
			t.Errorf("角色 %s 权限数量 = %d，期望 %d", role.Name, len(got), len(want))
			continue
		}
		for p := range want {
			if !got.Has(p) {
				t.Errorf("角色 %s 缺少权限 %q", role.Name, p)
			}
		}
	}

	// 幂等：重复播种不应报错，也不应产生重复角色。
	if seedErr := e.users.SeedRoles(ctx); seedErr != nil {
		t.Fatalf("重复播种失败: %v", seedErr)
	}
	again, err := e.users.ListRoles(ctx)
	if err != nil {
		t.Fatalf("查询角色失败: %v", err)
	}
	if len(again) != len(perm.BuiltinRoleNames) {
		t.Errorf("重复播种后角色数量 = %d", len(again))
	}
}

// TestSeedRolesOverwritesStalePermissions 验证升级场景：
// 库中被篡改的内置角色权限会被代码定义覆盖。
func TestSeedRolesOverwritesStalePermissions(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	// 模拟旧版本遗留的错误权限集合。
	if _, err := e.db.NewUpdate().
		Model((*auth.Role)(nil)).
		Set("permissions = ?", `["users:manage"]`).
		Where("name = ?", perm.RoleAuthor).
		Exec(ctx); err != nil {
		t.Fatalf("篡改角色失败: %v", err)
	}

	if seedErr := e.users.SeedRoles(ctx); seedErr != nil {
		t.Fatalf("重新播种失败: %v", seedErr)
	}

	role, err := e.users.FindRoleByName(ctx, perm.RoleAuthor)
	if err != nil {
		t.Fatalf("查询角色失败: %v", err)
	}
	if perm.NewSet(role.Permissions...).Has(perm.UsersManage) {
		t.Error("内置角色权限未被代码定义覆盖")
	}
}

// ---------- 用户 ----------

func TestCreateUser(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	user, err := e.users.CreateUser(ctx, &auth.CreateUserParams{
		Username:    "Alice",
		Email:       "Alice@Example.COM",
		Password:    testPassword,
		DisplayName: "爱丽丝",
		Roles:       []string{perm.RoleAuthor},
	})
	if err != nil {
		t.Fatalf("创建用户失败: %v", err)
	}

	if user.Username != "alice" {
		t.Errorf("用户名应归一化为小写，实际 %q", user.Username)
	}
	if user.Email != "alice@example.com" {
		t.Errorf("邮箱应归一化为小写，实际 %q", user.Email)
	}
	if len(user.Roles) != 1 || user.Roles[0].Name != perm.RoleAuthor {
		t.Errorf("角色未正确授予: %+v", user.RoleNames())
	}
	// 口令哈希不得等于明文。
	if user.PasswordHash == testPassword || user.PasswordHash == "" {
		t.Error("口令哈希未正确生成")
	}
	if !strings.HasPrefix(user.PasswordHash, "$argon2id$") {
		t.Errorf("口令哈希应为 argon2id PHC 格式，实际 %q", user.PasswordHash)
	}

	// 大小写不敏感查询。
	for _, login := range []string{"alice", "ALICE", "Alice", "alice@example.com", "ALICE@EXAMPLE.COM"} {
		found, err := e.users.FindUserByLogin(ctx, login)
		if err != nil {
			t.Errorf("FindUserByLogin(%q) 失败: %v", login, err)
			continue
		}
		if found.ID != user.ID {
			t.Errorf("FindUserByLogin(%q) 返回了错误的用户", login)
		}
	}

	if _, err := e.users.FindUserByLogin(ctx, "nobody"); !errors.Is(err, auth.ErrNotFound) {
		t.Errorf("不存在的用户应返回 ErrNotFound，实际 %v", err)
	}
}

func TestCreateUserRejectsBadInput(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	t.Run("密码过短", func(t *testing.T) {
		_, err := e.users.CreateUser(ctx, &auth.CreateUserParams{
			Username: "shortpw", Email: "s@example.com", Password: "abc",
			Roles: []string{perm.RoleAuthor},
		})
		if err == nil {
			t.Error("过短密码应被拒绝")
		}
	})

	t.Run("用户名重复", func(t *testing.T) {
		e.createUser(t, "dup", perm.RoleAuthor)
		_, err := e.users.CreateUser(ctx, &auth.CreateUserParams{
			Username: "DUP", Email: "other@example.com", Password: testPassword,
			Roles: []string{perm.RoleAuthor},
		})
		if !errors.Is(err, auth.ErrDuplicate) {
			t.Errorf("重复用户名应返回 ErrDuplicate，实际 %v", err)
		}
	})

	t.Run("邮箱重复", func(t *testing.T) {
		_, err := e.users.CreateUser(ctx, &auth.CreateUserParams{
			Username: "othername", Email: "DUP@example.com", Password: testPassword,
			Roles: []string{perm.RoleAuthor},
		})
		if !errors.Is(err, auth.ErrDuplicate) {
			t.Errorf("重复邮箱应返回 ErrDuplicate，实际 %v", err)
		}
	})

	t.Run("未知角色", func(t *testing.T) {
		_, err := e.users.CreateUser(ctx, &auth.CreateUserParams{
			Username: "badrole", Email: "badrole@example.com", Password: testPassword,
			Roles: []string{"no-such-role"},
		})
		if !errors.Is(err, auth.ErrNotFound) {
			t.Errorf("未知角色应返回 ErrNotFound，实际 %v", err)
		}
	})

	t.Run("非法用户名格式", func(t *testing.T) {
		_, err := e.users.CreateUser(ctx, &auth.CreateUserParams{
			Username: "Bad Name!", Email: "bad@example.com", Password: testPassword,
			Roles: []string{perm.RoleAuthor},
		})
		if err == nil {
			t.Error("违反 DNS-1123 的用户名应被数据库约束拒绝")
		}
	})
}

// TestCreateUserRollsBackOnRoleFailure 验证事务性：
// 角色授予失败时，用户本身也不应留下（否则会留下无角色的孤儿账号）。
func TestCreateUserRollsBackOnRoleFailure(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	_, err := e.users.CreateUser(ctx, &auth.CreateUserParams{
		Username: "orphan", Email: "orphan@example.com", Password: testPassword,
		Roles: []string{perm.RoleAuthor, "no-such-role"},
	})
	if err == nil {
		t.Fatal("含未知角色时创建应失败")
	}

	count, err := e.users.CountUsers(ctx)
	if err != nil {
		t.Fatalf("统计用户失败: %v", err)
	}
	if count != 0 {
		t.Errorf("事务未回滚，残留 %d 个用户", count)
	}
}

// ---------- 登录 ----------

func TestLogin(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	user := e.createUser(t, "loginuser", perm.RoleAuthor)

	t.Run("凭据正确返回会话", func(t *testing.T) {
		issued, gotUser, err := e.service.Login(ctx, auth.LoginParams{
			Login: "loginuser", Password: testPassword,
			UserAgent: "test-agent", IP: "203.0.113.7",
		})
		if err != nil {
			t.Fatalf("登录失败: %v", err)
		}
		if gotUser.ID != user.ID {
			t.Errorf("返回了错误的用户")
		}
		if issued.Token == "" || issued.CSRFToken == "" {
			t.Error("会话令牌与 CSRF 令牌均不应为空")
		}
		if issued.Token == issued.CSRFToken {
			t.Error("会话令牌与 CSRF 令牌不应相同")
		}
	})

	t.Run("邮箱亦可登录", func(t *testing.T) {
		if _, _, err := e.service.Login(ctx, auth.LoginParams{
			Login: "loginuser@example.com", Password: testPassword,
		}); err != nil {
			t.Errorf("邮箱登录失败: %v", err)
		}
	})

	// 账号不存在与密码错误必须返回同一个错误，避免账号枚举。
	t.Run("密码错误与账号不存在返回同一错误", func(t *testing.T) {
		_, _, errWrongPw := e.service.Login(ctx, auth.LoginParams{
			Login: "loginuser", Password: "wrong-password",
		})
		_, _, errNoUser := e.service.Login(ctx, auth.LoginParams{
			Login: "ghost", Password: "wrong-password",
		})

		if !errors.Is(errWrongPw, auth.ErrInvalidCredentials) {
			t.Errorf("密码错误应返回 ErrInvalidCredentials，实际 %v", errWrongPw)
		}
		if !errors.Is(errNoUser, auth.ErrInvalidCredentials) {
			t.Errorf("账号不存在应返回 ErrInvalidCredentials，实际 %v", errNoUser)
		}
		if errWrongPw.Error() != errNoUser.Error() {
			t.Errorf("两种失败的错误信息必须一致，实际 %q vs %q", errWrongPw, errNoUser)
		}
	})

	t.Run("停用账号拒绝登录", func(t *testing.T) {
		disabled := e.createUser(t, "disableduser", perm.RoleAuthor)
		if err := e.users.SetDisabled(ctx, disabled.ID, true); err != nil {
			t.Fatalf("停用账号失败: %v", err)
		}
		_, _, err := e.service.Login(ctx, auth.LoginParams{
			Login: "disableduser", Password: testPassword,
		})
		if !errors.Is(err, auth.ErrAccountDisabled) {
			t.Errorf("应返回 ErrAccountDisabled，实际 %v", err)
		}
	})
}

// ---------- 会话 ----------

func TestSessionLifecycle(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	user := e.createUser(t, "sessionuser", perm.RoleAuthor)

	issued, err := e.sessions.Create(ctx, user.ID, "test-agent", "203.0.113.7")
	if err != nil {
		t.Fatalf("创建会话失败: %v", err)
	}

	// 库中只应存哈希，不得出现明文令牌。
	var stored string
	if rawErr := e.db.NewRaw("SELECT token_hash FROM sessions LIMIT 1").Scan(ctx, &stored); rawErr != nil {
		t.Fatalf("读取会话失败: %v", rawErr)
	}
	if stored == issued.Token {
		t.Fatal("库中存储了会话令牌明文")
	}
	if stored != auth.HashToken(issued.Token) {
		t.Error("库中存储的不是令牌哈希")
	}

	session, err := e.sessions.Lookup(ctx, issued.Token)
	if err != nil {
		t.Fatalf("查找会话失败: %v", err)
	}
	if session.UserID != user.ID {
		t.Errorf("会话归属错误")
	}
	if session.CSRFToken != issued.CSRFToken {
		t.Error("CSRF 令牌不匹配")
	}

	t.Run("未知令牌被拒绝", func(t *testing.T) {
		if _, err := e.sessions.Lookup(ctx, "not-a-real-token"); !errors.Is(err, auth.ErrInvalidSession) {
			t.Errorf("应返回 ErrInvalidSession，实际 %v", err)
		}
		if _, err := e.sessions.Lookup(ctx, ""); !errors.Is(err, auth.ErrInvalidSession) {
			t.Errorf("空令牌应返回 ErrInvalidSession，实际 %v", err)
		}
	})

	t.Run("过期会话被拒绝并清理", func(t *testing.T) {
		if _, err := e.db.ExecContext(ctx,
			"UPDATE sessions SET expires_at = now() - interval '1 hour' WHERE token_hash = ?",
			auth.HashToken(issued.Token)); err != nil {
			t.Fatalf("设置过期时间失败: %v", err)
		}

		if _, err := e.sessions.Lookup(ctx, issued.Token); !errors.Is(err, auth.ErrInvalidSession) {
			t.Fatalf("过期会话应被拒绝，实际 %v", err)
		}

		// 过期会话应被顺带清理。
		count, err := e.db.NewSelect().Model((*auth.Session)(nil)).Count(ctx)
		if err != nil {
			t.Fatalf("统计会话失败: %v", err)
		}
		if count != 0 {
			t.Errorf("过期会话未被清理，残留 %d 条", count)
		}
	})
}

func TestSessionDeleteAllForUser(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	user := e.createUser(t, "multisession", perm.RoleAuthor)

	for range 3 {
		if _, err := e.sessions.Create(ctx, user.ID, "ua", "127.0.0.1"); err != nil {
			t.Fatalf("创建会话失败: %v", err)
		}
	}

	if err := e.sessions.DeleteAllForUser(ctx, user.ID); err != nil {
		t.Fatalf("删除全部会话失败: %v", err)
	}

	count, err := e.db.NewSelect().Model((*auth.Session)(nil)).Count(ctx)
	if err != nil {
		t.Fatalf("统计会话失败: %v", err)
	}
	if count != 0 {
		t.Errorf("残留 %d 条会话", count)
	}
}

// TestSessionCookieFlags 验证 Cookie 安全属性（agent.md §7.1）。
func TestSessionCookieFlags(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	user := e.createUser(t, "cookieuser", perm.RoleAuthor)

	issued, err := e.sessions.Create(ctx, user.ID, "ua", "127.0.0.1")
	if err != nil {
		t.Fatalf("创建会话失败: %v", err)
	}

	rec := httptest.NewRecorder()
	e.sessions.SetCookies(rec, issued)

	cookies := map[string]*http.Cookie{}
	for _, c := range rec.Result().Cookies() {
		cookies[c.Name] = c
	}

	sessionCookie, ok := cookies[e.sessions.SessionCookieName()]
	if !ok {
		t.Fatal("未写入会话 Cookie")
	}
	if !sessionCookie.HttpOnly {
		t.Error("会话 Cookie 必须为 HttpOnly，防止 XSS 窃取")
	}
	if sessionCookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("会话 Cookie SameSite = %v，期望 Lax", sessionCookie.SameSite)
	}
	if sessionCookie.Value != issued.Token {
		t.Error("会话 Cookie 值不正确")
	}

	csrfCookie, ok := cookies[e.sessions.CSRFCookieName()]
	if !ok {
		t.Fatal("未写入 CSRF Cookie")
	}
	// CSRF Cookie 必须可被前端 JS 读取（双提交模式）。
	if csrfCookie.HttpOnly {
		t.Error("CSRF Cookie 不应为 HttpOnly，否则双提交模式无法工作")
	}

	// 清理 Cookie 应使两者失效。
	clearRec := httptest.NewRecorder()
	e.sessions.ClearCookies(clearRec)
	for _, c := range clearRec.Result().Cookies() {
		if c.MaxAge >= 0 {
			t.Errorf("清理 Cookie %s 的 MaxAge = %d，应为负数", c.Name, c.MaxAge)
		}
	}
}

// TestSecureCookieNaming 验证 Secure 模式下启用 __Host- 前缀。
func TestSecureCookieNaming(t *testing.T) {
	t.Parallel()

	insecure := auth.NewSessionStore(nil, false)
	secure := auth.NewSessionStore(nil, true)

	if strings.HasPrefix(insecure.SessionCookieName(), "__Host-") {
		t.Error("非 Secure 模式不应使用 __Host- 前缀（浏览器会拒绝）")
	}
	if !strings.HasPrefix(secure.SessionCookieName(), "__Host-") {
		t.Error("Secure 模式应使用 __Host- 前缀")
	}
	if !strings.HasPrefix(secure.CSRFCookieName(), "__Host-") {
		t.Error("Secure 模式下 CSRF Cookie 也应使用 __Host- 前缀")
	}
}

// ---------- 访问令牌 ----------

func TestTokenLifecycle(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	user := e.createUser(t, "tokenuser", perm.RoleAuthor)

	issued, err := e.tokens.Create(ctx, &auth.CreateTokenParams{
		UserID: user.ID,
		Name:   "CI 令牌",
	})
	if err != nil {
		t.Fatalf("创建令牌失败: %v", err)
	}

	if !strings.HasPrefix(issued.Plaintext, auth.TokenPrefix) {
		t.Errorf("令牌明文应以 %q 开头，实际 %q", auth.TokenPrefix, issued.Plaintext)
	}
	if issued.Token.TokenHint == "" {
		t.Error("令牌提示不应为空")
	}
	// 提示必须短于密钥本体，否则「仅存哈希」失去意义。
	if strings.Contains(issued.Plaintext, issued.Token.TokenHint) == false {
		t.Error("令牌提示应取自明文前缀")
	}

	// 库中只应存哈希，不得出现明文。
	var storedHash string
	if rawErr := e.db.NewRaw("SELECT token_hash FROM access_tokens LIMIT 1").Scan(ctx, &storedHash); rawErr != nil {
		t.Fatalf("读取令牌失败: %v", rawErr)
	}
	if storedHash == issued.Plaintext {
		t.Fatal("库中存储了令牌明文")
	}
	if storedHash != auth.HashToken(issued.Plaintext) {
		t.Error("库中存储的不是令牌哈希")
	}

	token, err := e.tokens.Lookup(ctx, issued.Plaintext)
	if err != nil {
		t.Fatalf("查找令牌失败: %v", err)
	}
	if token.UserID != user.ID {
		t.Error("令牌归属错误")
	}

	t.Run("非本前缀的令牌被拒绝", func(t *testing.T) {
		for _, bad := range []string{"", "random-string", "Bearer xyz"} {
			if _, err := e.tokens.Lookup(ctx, bad); !errors.Is(err, auth.ErrInvalidToken) {
				t.Errorf("Lookup(%q) 应返回 ErrInvalidToken，实际 %v", bad, err)
			}
		}
	})

	t.Run("未知名牌被拒绝", func(t *testing.T) {
		if _, err := e.tokens.Lookup(ctx, auth.TokenPrefix+"nonexistent"); !errors.Is(err, auth.ErrInvalidToken) {
			t.Errorf("应返回 ErrInvalidToken，实际 %v", err)
		}
	})

	t.Run("过期令牌被拒绝", func(t *testing.T) {
		if _, err := e.db.ExecContext(ctx,
			"UPDATE access_tokens SET expires_at = now() - interval '1 hour' WHERE id = ?",
			issued.Token.ID); err != nil {
			t.Fatalf("设置过期时间失败: %v", err)
		}
		if _, err := e.tokens.Lookup(ctx, issued.Plaintext); !errors.Is(err, auth.ErrInvalidToken) {
			t.Errorf("过期令牌应被拒绝，实际 %v", err)
		}
	})

	t.Run("空名称被拒绝", func(t *testing.T) {
		if _, err := e.tokens.Create(ctx, &auth.CreateTokenParams{
			UserID: user.ID, Name: "   ",
		}); err == nil {
			t.Error("空名称应被拒绝")
		}
	})

	t.Run("非法 scope 被拒绝", func(t *testing.T) {
		if _, err := e.tokens.Create(ctx, &auth.CreateTokenParams{
			UserID: user.ID, Name: "bad-scope",
			Scopes: []perm.Permission{"posts:wirte"},
		}); err == nil {
			t.Error("拼错的 scope 应被拒绝，否则会变成难查的 403")
		}
	})
}

// TestRevokeIsScopedToOwner 验证越权防护：
// 用户不能通过猜 ID 撤销他人的令牌。
func TestRevokeIsScopedToOwner(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	owner := e.createUser(t, "tokenowner", perm.RoleAuthor)
	attacker := e.createUser(t, "attacker", perm.RoleAuthor)

	issued, err := e.tokens.Create(ctx, &auth.CreateTokenParams{
		UserID: owner.ID, Name: "victim-token",
	})
	if err != nil {
		t.Fatalf("创建令牌失败: %v", err)
	}

	// 攻击者尝试撤销他人令牌。
	if err := e.tokens.Revoke(ctx, attacker.ID, issued.Token.ID); !errors.Is(err, auth.ErrNotFound) {
		t.Errorf("越权撤销应返回 ErrNotFound，实际 %v", err)
	}

	// 令牌必须仍然有效。
	if _, err := e.tokens.Lookup(ctx, issued.Plaintext); err != nil {
		t.Fatalf("他人令牌被越权撤销: %v", err)
	}

	// 所有者可正常撤销。
	if err := e.tokens.Revoke(ctx, owner.ID, issued.Token.ID); err != nil {
		t.Fatalf("所有者撤销失败: %v", err)
	}
	if _, err := e.tokens.Lookup(ctx, issued.Plaintext); !errors.Is(err, auth.ErrInvalidToken) {
		t.Errorf("撤销后令牌应失效，实际 %v", err)
	}
}

// ---------- Authenticator ----------

func TestAuthenticatorResolveSession(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	user := e.createUser(t, "resolveuser", perm.RoleAuthor)

	issued, err := e.sessions.Create(ctx, user.ID, "ua", "127.0.0.1")
	if err != nil {
		t.Fatalf("创建会话失败: %v", err)
	}

	t.Run("无凭据返回 nil 且不报错", func(t *testing.T) {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody)
		principal, err := e.authn.Resolve(req)
		if err != nil {
			t.Fatalf("无凭据不应报错: %v", err)
		}
		if principal != nil {
			t.Error("无凭据应返回 nil")
		}
	})

	t.Run("有效 Cookie 解析为会话调用者", func(t *testing.T) {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody)
		req.AddCookie(&http.Cookie{Name: e.sessions.SessionCookieName(), Value: issued.Token})

		principal, err := e.authn.Resolve(req)
		if err != nil {
			t.Fatalf("解析失败: %v", err)
		}
		if principal == nil || principal.UserID() != user.ID {
			t.Fatal("未正确解析会话调用者")
		}
		if principal.Method != auth.MethodSession {
			t.Errorf("Method = %q", principal.Method)
		}
	})

	t.Run("无效 Cookie 返回 ErrInvalidSession", func(t *testing.T) {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody)
		req.AddCookie(&http.Cookie{Name: e.sessions.SessionCookieName(), Value: "bogus"})

		if _, err := e.authn.Resolve(req); !errors.Is(err, auth.ErrInvalidSession) {
			t.Errorf("应返回 ErrInvalidSession，实际 %v", err)
		}
	})
}

func TestAuthenticatorResolveToken(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	user := e.createUser(t, "beareruser", perm.RoleAuthor)

	issued, err := e.tokens.Create(ctx, &auth.CreateTokenParams{
		UserID: user.ID, Name: "bearer",
	})
	if err != nil {
		t.Fatalf("创建令牌失败: %v", err)
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+issued.Plaintext)

	principal, err := e.authn.Resolve(req)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if principal == nil || principal.UserID() != user.ID {
		t.Fatal("未正确解析令牌调用者")
	}
	if principal.Method != auth.MethodToken {
		t.Errorf("Method = %q，期望 %q", principal.Method, auth.MethodToken)
	}
	if principal.TokenID != issued.Token.ID {
		t.Errorf("TokenID = %d", principal.TokenID)
	}

	t.Run("无效令牌返回 ErrInvalidToken", func(t *testing.T) {
		badReq := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody)
		badReq.Header.Set("Authorization", "Bearer "+auth.TokenPrefix+"bogus")

		if _, err := e.authn.Resolve(badReq); !errors.Is(err, auth.ErrInvalidToken) {
			t.Errorf("应返回 ErrInvalidToken，实际 %v", err)
		}
	})

	t.Run("令牌优先于会话", func(t *testing.T) {
		// 同时带 Cookie 与 Bearer 时应走令牌认证（无头调用优先）。
		session, err := e.sessions.Create(ctx, user.ID, "ua", "127.0.0.1")
		if err != nil {
			t.Fatalf("创建会话失败: %v", err)
		}
		mixed := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody)
		mixed.AddCookie(&http.Cookie{Name: e.sessions.SessionCookieName(), Value: session.Token})
		mixed.Header.Set("Authorization", "Bearer "+issued.Plaintext)

		principal, err := e.authn.Resolve(mixed)
		if err != nil {
			t.Fatalf("解析失败: %v", err)
		}
		if principal.Method != auth.MethodToken {
			t.Errorf("Method = %q，期望令牌优先", principal.Method)
		}
	})
}

// TestDisabledUserCredentialsRejected 验证停用账号的既有凭据立即失效。
func TestDisabledUserCredentialsRejected(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	user := e.createUser(t, "disabledcreds", perm.RoleAuthor)

	session, err := e.sessions.Create(ctx, user.ID, "ua", "127.0.0.1")
	if err != nil {
		t.Fatalf("创建会话失败: %v", err)
	}
	token, err := e.tokens.Create(ctx, &auth.CreateTokenParams{UserID: user.ID, Name: "t"})
	if err != nil {
		t.Fatalf("创建令牌失败: %v", err)
	}

	if err := e.users.SetDisabled(ctx, user.ID, true); err != nil {
		t.Fatalf("停用账号失败: %v", err)
	}

	sessionReq := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody)
	sessionReq.AddCookie(&http.Cookie{Name: e.sessions.SessionCookieName(), Value: session.Token})
	if _, err := e.authn.Resolve(sessionReq); !errors.Is(err, auth.ErrInvalidSession) {
		t.Errorf("停用账号的会话应失效，实际 %v", err)
	}

	tokenReq := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody)
	tokenReq.Header.Set("Authorization", "Bearer "+token.Plaintext)
	if _, err := e.authn.Resolve(tokenReq); !errors.Is(err, auth.ErrInvalidToken) {
		t.Errorf("停用账号的令牌应失效，实际 %v", err)
	}
}

// ---------- CSRF ----------

func TestCSRFMiddleware(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	user := e.createUser(t, "csrfuser", perm.RoleAuthor)

	issued, err := e.sessions.Create(ctx, user.ID, "ua", "127.0.0.1")
	if err != nil {
		t.Fatalf("创建会话失败: %v", err)
	}

	okHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// do 构造带会话 Cookie 的请求，principal 由调用方决定。
	do := func(method, csrfHeader string, withPrincipal bool) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(t.Context(), method, "/api/v1/console/posts", http.NoBody)
		req.AddCookie(&http.Cookie{Name: e.sessions.SessionCookieName(), Value: issued.Token})
		if csrfHeader != "" {
			req.Header.Set(auth.CSRFHeaderName, csrfHeader)
		}
		if withPrincipal {
			principal := auth.NewSessionPrincipal(user)
			req = req.WithContext(auth.WithPrincipal(req.Context(), principal))
		}
		rec := httptest.NewRecorder()
		e.authn.CSRF(okHandler).ServeHTTP(rec, req)
		return rec
	}

	t.Run("安全方法免校验", func(t *testing.T) {
		for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
			if rec := do(method, "", true); rec.Code != http.StatusOK {
				t.Errorf("%s 应免 CSRF 校验，实际 %d", method, rec.Code)
			}
		}
	})

	t.Run("写方法缺少 CSRF 头返回 403", func(t *testing.T) {
		rec := do(http.MethodPost, "", true)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("状态码 = %d，期望 403", rec.Code)
		}
	})

	t.Run("CSRF 令牌错误返回 403", func(t *testing.T) {
		rec := do(http.MethodPost, "wrong-token", true)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("状态码 = %d，期望 403", rec.Code)
		}
	})

	t.Run("CSRF 令牌正确则放行", func(t *testing.T) {
		rec := do(http.MethodPost, issued.CSRFToken, true)
		if rec.Code != http.StatusOK {
			t.Fatalf("状态码 = %d，期望 200", rec.Code)
		}
	})

	t.Run("匿名请求不做 CSRF 校验", func(t *testing.T) {
		// 未认证请求由 RequireAuth 处理，CSRF 中间件不应抢先返回 403。
		rec := do(http.MethodPost, "", false)
		if rec.Code != http.StatusOK {
			t.Errorf("匿名请求应交给后续鉴权中间件，实际 %d", rec.Code)
		}
	})

	t.Run("令牌认证跳过 CSRF", func(t *testing.T) {
		// PAT 调用不带 Cookie，天然无 CSRF 风险。
		token, err := e.tokens.Create(ctx, &auth.CreateTokenParams{UserID: user.ID, Name: "csrf-skip"})
		if err != nil {
			t.Fatalf("创建令牌失败: %v", err)
		}
		principal := auth.NewTokenPrincipal(user, token.Token)

		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/console/posts", http.NoBody)
		req = req.WithContext(auth.WithPrincipal(req.Context(), principal))
		rec := httptest.NewRecorder()
		e.authn.CSRF(okHandler).ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("令牌认证应跳过 CSRF，实际 %d", rec.Code)
		}
	})
}

// ---------- 密码变更与凭据失效 ----------

// TestResetPasswordInvalidatesAllCredentials 是本文件的安全核心测试之一：
// 改密码后所有既有会话与令牌必须失效，否则「改了密码却没踢下线」
// 会成为严重的安全缺陷。
func TestResetPasswordInvalidatesAllCredentials(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	user := e.createUser(t, "resetpw", perm.RoleAuthor)

	for range 2 {
		if _, err := e.sessions.Create(ctx, user.ID, "ua", "127.0.0.1"); err != nil {
			t.Fatalf("创建会话失败: %v", err)
		}
	}
	for range 2 {
		if _, err := e.tokens.Create(ctx, &auth.CreateTokenParams{UserID: user.ID, Name: "t"}); err != nil {
			t.Fatalf("创建令牌失败: %v", err)
		}
	}

	if err := e.service.ResetPassword(ctx, user.ID, otherPass); err != nil {
		t.Fatalf("重置密码失败: %v", err)
	}

	sessionCount, err := e.db.NewSelect().Model((*auth.Session)(nil)).Count(ctx)
	if err != nil {
		t.Fatalf("统计会话失败: %v", err)
	}
	if sessionCount != 0 {
		t.Errorf("改密码后残留 %d 条会话", sessionCount)
	}

	tokenCount, err := e.db.NewSelect().Model((*auth.AccessToken)(nil)).Count(ctx)
	if err != nil {
		t.Fatalf("统计令牌失败: %v", err)
	}
	if tokenCount != 0 {
		t.Errorf("改密码后残留 %d 个令牌", tokenCount)
	}

	// 旧密码失效、新密码可用。
	if _, _, err := e.service.Login(ctx, auth.LoginParams{
		Login: "resetpw", Password: testPassword,
	}); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Errorf("旧密码应失效，实际 %v", err)
	}
	if _, _, err := e.service.Login(ctx, auth.LoginParams{
		Login: "resetpw", Password: otherPass,
	}); err != nil {
		t.Errorf("新密码应可登录，实际 %v", err)
	}
}

func TestChangePasswordRequiresOldPassword(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	user := e.createUser(t, "changepw", perm.RoleAuthor)

	// 原密码错误应被拒绝。
	if err := e.service.ChangePassword(ctx, user.ID, "wrong-old", otherPass); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Errorf("原密码错误应返回 ErrInvalidCredentials，实际 %v", err)
	}

	// 原密码正确则成功。
	if err := e.service.ChangePassword(ctx, user.ID, testPassword, otherPass); err != nil {
		t.Fatalf("修改密码失败: %v", err)
	}

	if _, _, err := e.service.Login(ctx, auth.LoginParams{
		Login: "changepw", Password: otherPass,
	}); err != nil {
		t.Errorf("新密码应可登录，实际 %v", err)
	}
}

// ---------- 自定义角色 ----------

func TestCustomRole(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	role := &auth.Role{
		Name:        "reviewer",
		Label:       "审核员",
		Permissions: []perm.Permission{perm.PostsWrite, perm.CommentsManage},
		Builtin:     false,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if _, err := e.db.NewInsert().Model(role).Exec(ctx); err != nil {
		t.Fatalf("创建自定义角色失败: %v", err)
	}

	user := e.createUser(t, "customrole", "reviewer")

	set := user.Permissions()
	if !set.Has(perm.PostsWrite) || !set.Has(perm.CommentsManage) {
		t.Error("自定义角色的权限未生效")
	}
	if set.Has(perm.UsersManage) {
		t.Error("不应获得未授予的权限")
	}

	// 内置角色不可被误认为自定义角色。
	if perm.IsBuiltin("reviewer") {
		t.Error("自定义角色不应被识别为内置角色")
	}

	custom, err := e.users.CustomRolePermissions(ctx)
	if err != nil {
		t.Fatalf("查询自定义角色失败: %v", err)
	}
	if _, ok := custom["reviewer"]; !ok {
		t.Error("自定义角色未出现在映射中")
	}
	if _, ok := custom[perm.RoleAuthor]; ok {
		t.Error("内置角色不应出现在自定义角色映射中")
	}
}

// TestAssignRoles 验证覆盖式设置角色。
func TestAssignRoles(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	user := e.createUser(t, "rolechange", perm.RoleAuthor)

	if err := e.users.AssignRoles(ctx, user.ID, []string{perm.RoleEditor}); err != nil {
		t.Fatalf("设置角色失败: %v", err)
	}

	updated, err := e.users.FindUserByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("查询用户失败: %v", err)
	}
	if len(updated.Roles) != 1 || updated.Roles[0].Name != perm.RoleEditor {
		t.Errorf("角色未覆盖，实际 %v", updated.RoleNames())
	}
	// editor 的能力应生效，author 的不应残留。
	if !updated.Permissions().Has(perm.PostsPublish) {
		t.Error("editor 应具备 posts:publish")
	}
}

// TestBootstrapOnlyOnce 验证首个管理员只在无用户时创建。
func TestBootstrapOnlyOnce(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	params := &auth.CreateUserParams{
		Username: "root", Email: "root@example.com", Password: testPassword,
	}
	user, err := e.service.Bootstrap(ctx, params)
	if err != nil {
		t.Fatalf("Bootstrap 失败: %v", err)
	}
	if user == nil {
		t.Fatal("首次 Bootstrap 应创建管理员")
	}
	if len(user.Roles) != 1 || user.Roles[0].Name != perm.RoleSuperAdmin {
		t.Errorf("初始账号应为超级管理员，实际 %v", user.RoleNames())
	}

	// 已有用户时不应再创建。
	again, err := e.service.Bootstrap(ctx, &auth.CreateUserParams{
		Username: "root2", Email: "root2@example.com", Password: testPassword,
	})
	if err != nil {
		t.Fatalf("第二次 Bootstrap 不应报错: %v", err)
	}
	if again != nil {
		t.Error("已有用户时不应再次创建管理员")
	}
}

// TestSeedRolesPreservesCustomRoles 验证播种不误删自定义角色。
func TestSeedRolesPreservesCustomRoles(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	role := &auth.Role{
		Name: "custom-keep", Label: "保留", Permissions: []perm.Permission{perm.PostsWrite},
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if _, err := e.db.NewInsert().Model(role).Exec(ctx); err != nil {
		t.Fatalf("创建自定义角色失败: %v", err)
	}

	if err := e.users.SeedRoles(ctx); err != nil {
		t.Fatalf("播种失败: %v", err)
	}

	if _, err := e.users.FindRoleByName(ctx, "custom-keep"); err != nil {
		t.Errorf("播种不应删除自定义角色: %v", err)
	}
}

// TestLoginRejectsUnverifiedEmail 验证邮箱验证闸门（agent.md §7.1）。
//
// 闸门放在 Service.Login 里而不是某个前台处理器里，理由就是这条用例的形态：
// Console 的 POST /console/auth/login 走的是另一条路径，而它调用的也是这个方法。
// 只拦前台等于留一扇后门——会话 Cookie 一旦签出，前台认的是 Cookie 而不是「从哪登的」。
func TestLoginRejectsUnverifiedEmail(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	user, err := e.users.CreateUser(ctx, &auth.CreateUserParams{
		Username: "unverified",
		Email:    "unverified@example.com",
		Password: testPassword,
		Roles:    []string{perm.RoleMember},
		// 自助注册走的就是这条：EmailVerified 保持零值。
	})
	if err != nil {
		t.Fatalf("创建用户失败: %v", err)
	}
	if user.EmailVerified() {
		t.Fatal("新创建的用户不应被视为已验证")
	}

	_, _, err = e.service.Login(ctx, auth.LoginParams{
		Login: "unverified", Password: testPassword, IP: "127.0.0.1",
	})
	if !errors.Is(err, auth.ErrEmailUnverified) {
		t.Fatalf("未验证账号登录应被拒，实际 %v", err)
	}
}

// TestLoginSucceedsAfterEmailVerified 验证同一账号在验证之后可以正常登录。
//
// 与上一条成对：只测「被拒」的话，把闸门写成无条件拒绝也能过。
func TestLoginSucceedsAfterEmailVerified(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	user, err := e.users.CreateUser(ctx, &auth.CreateUserParams{
		Username: "verifies",
		Email:    "verifies@example.com",
		Password: testPassword,
		Roles:    []string{perm.RoleMember},
	})
	if err != nil {
		t.Fatalf("创建用户失败: %v", err)
	}

	if _, execErr := e.db.ExecContext(ctx,
		"UPDATE users SET email_verified_at = now() WHERE id = ?", user.ID); execErr != nil {
		t.Fatalf("标记邮箱已验证失败: %v", execErr)
	}

	issued, logged, err := e.service.Login(ctx, auth.LoginParams{
		Login: "verifies@example.com", Password: testPassword, IP: "127.0.0.1",
	})
	if err != nil {
		t.Fatalf("已验证账号应能登录，实际 %v", err)
	}
	if issued == nil || issued.Token == "" {
		t.Error("登录成功应签发会话")
	}
	if logged.ID != user.ID {
		t.Errorf("登录返回的用户 ID = %d，期望 %d", logged.ID, user.ID)
	}
}
