package testsupport

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/auth"
	"github.com/FeiBaiKin/lumo/internal/database"
	"github.com/FeiBaiKin/lumo/internal/server"
)

// Stack 是集成测试用的整机装配：真实路由、真实鉴权与已注册并启动的模块，
// 行为与 serve 命令一致但不监听端口。
type Stack struct {
	DB     *database.DB
	Root   http.Handler
	App    *app.App
	Users  *auth.Store
	Tokens *auth.TokenStore
}

// NewStack 装配路由与模块并播种内置角色。调用方须已完成迁移（含模块迁移）。
func NewStack(t *testing.T, db *database.DB, modules ...app.Module) *Stack {
	t.Helper()
	ctx := context.Background()

	users := auth.NewStore(db.DB)
	if err := users.SeedRoles(ctx); err != nil {
		t.Fatalf("写入内置角色失败: %v", err)
	}
	sessions := auth.NewSessionStore(db.DB, false)
	tokens := auth.NewTokenStore(db.DB)
	service := auth.NewService(users, sessions, tokens, nil)
	authn := auth.NewAuthenticator(users, sessions, tokens, nil)

	root, planes := server.NewRouter(&server.Options{Authenticator: authn, Version: "test"})
	auth.NewHandler(service, sessions, tokens, nil).Register(planes.ConsolePublic(), planes.Console())

	application := app.New(&app.Options{DB: db, Router: planes})
	if err := application.Register(modules...); err != nil {
		t.Fatalf("注册模块失败: %v", err)
	}
	// 模块的后台任务随此 ctx 退出，避免 goroutine 泄漏到其他测试。
	runCtx, cancel := context.WithCancel(ctx)
	if err := application.Start(runCtx); err != nil {
		cancel()
		t.Fatalf("启动模块失败: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		_ = application.Close(context.Background())
	})

	return &Stack{DB: db, Root: root, App: application, Users: users, Tokens: tokens}
}

// Bearer 创建拥有指定角色的用户并签发访问令牌，返回可直接放入 Authorization 头的值。
//
// 用 PAT 而非会话：无需 Cookie 与 CSRF 令牌，测试代码更短，且与无头调用路径一致。
func (s *Stack) Bearer(t *testing.T, username, role string) string {
	t.Helper()
	ctx := context.Background()

	user, err := s.Users.CreateUser(ctx, &auth.CreateUserParams{
		Username: username,
		Email:    username + "@example.com",
		Password: "test-password-123",
		Roles:    []string{role},
	})
	if err != nil {
		t.Fatalf("创建用户 %s 失败: %v", username, err)
	}
	issued, err := s.Tokens.Create(ctx, &auth.CreateTokenParams{UserID: user.ID, Name: "test"})
	if err != nil {
		t.Fatalf("签发令牌失败: %v", err)
	}
	return "Bearer " + issued.Plaintext
}

// Request 描述一次 JSON 请求。
type Request struct {
	Method string
	Path   string
	// Body 非空时按 application/json 提交。
	Body string
	// Auth 非空时写入 Authorization 头。
	Auth string
}

// Do 发起请求并返回记录器。
func (s *Stack) Do(t *testing.T, r *Request) *httptest.ResponseRecorder {
	t.Helper()

	var reader io.Reader = http.NoBody
	if r.Body != "" {
		reader = strings.NewReader(r.Body)
	}
	req := httptest.NewRequestWithContext(t.Context(), r.Method, r.Path, reader)
	if r.Body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if r.Auth != "" {
		req.Header.Set("Authorization", r.Auth)
	}
	rec := httptest.NewRecorder()
	s.Root.ServeHTTP(rec, req)
	return rec
}
