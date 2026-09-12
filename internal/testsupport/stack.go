package testsupport

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/auth"
	"github.com/FeiBaiKin/lumo/internal/config"
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

// StackOptions 是装配整机测试栈的可选参数。
type StackOptions struct {
	// Config 传给 app.App，模块经 App.Config() 读取（如附件的 DataDir）。
	Config config.Config
	// UploadsDir 非空时把该目录以静态文件形式挂在 /uploads 下。
	UploadsDir string
	// Modules 是要装配的模块，顺序即注册顺序。
	Modules []app.Module
	// AfterStart 在全部模块 Start 之后、返回栈之前调用，可往根路由上再挂东西。
	//
	// 供访客前台使用：它的兜底路由 /{slug} 会吞掉根路径下的一切单段路径，
	// 必须最后注册，与 serve 的顺序保持一致（见 cmd/lumo/serve.go）。
	AfterStart func(root chi.Router, application *app.App)
}

// NewStack 装配路由与模块并播种内置角色。调用方须已完成迁移（含模块迁移）。
func NewStack(t *testing.T, db *database.DB, modules ...app.Module) *Stack {
	t.Helper()
	return NewStackWith(t, db, &StackOptions{Modules: modules})
}

// NewStackWith 是带可选参数的装配入口，供需要工作目录或自定义配置的模块使用。
func NewStackWith(t *testing.T, db *database.DB, opts *StackOptions) *Stack {
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

	root, planes := server.NewRouter(&server.Options{
		Authenticator: authn,
		Version:       "test",
		UploadsDir:    opts.UploadsDir,
	})
	auth.NewHandler(service, sessions, tokens, nil).Register(planes.ConsolePublic(), planes.Console())
	application := app.New(&app.Options{Config: opts.Config, DB: db, Router: planes})
	auth.NewAdminHandler(users, service, func() []auth.PermissionInfo {
		declared := append(app.CorePermissions(), application.Permissions()...)
		out := make([]auth.PermissionInfo, 0, len(declared))
		for _, p := range declared {
			out = append(out, auth.PermissionInfo{Key: p.Key, Label: p.Label, Description: p.Description})
		}
		return out
	}).Register(planes.Console())

	if err := application.Register(opts.Modules...); err != nil {
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

	if opts.AfterStart != nil {
		opts.AfterStart(root, application)
	}

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
		Password: Password,
		Roles:    []string{role},
	})
	if err != nil {
		t.Fatalf("创建用户 %s 失败: %v", username, err)
	}
	// scope 必须显式给出：空 scopes 表示「没有任何权限」，
	// 这里按账号当前权限展开，等价于「全权限令牌」。
	issued, err := s.Tokens.Create(ctx, &auth.CreateTokenParams{
		UserID: user.ID,
		Name:   "test",
		Scopes: user.Permissions().List(),
	})
	if err != nil {
		t.Fatalf("签发令牌失败: %v", err)
	}
	return "Bearer " + issued.Plaintext
}

// Request 描述一次请求。
type Request struct {
	Method string
	Path   string
	// Body 非空时作为请求体提交，默认按 application/json。
	Body string
	// ContentType 覆盖默认的 application/json，供 multipart 一类请求使用。
	ContentType string
	// Auth 非空时写入 Authorization 头。
	Auth string
	// Cookies 随请求发送，供需要会话身份的场景使用（CSRF、已登录访客等）。
	Cookies []*http.Cookie
	// Headers 是额外的请求头。
	Headers map[string]string
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
		contentType := r.ContentType
		if contentType == "" {
			contentType = "application/json"
		}
		req.Header.Set("Content-Type", contentType)
	}
	for _, cookie := range r.Cookies {
		req.AddCookie(cookie)
	}
	for name, value := range r.Headers {
		req.Header.Set(name, value)
	}
	if r.Auth != "" {
		req.Header.Set("Authorization", r.Auth)
	}
	rec := httptest.NewRecorder()
	s.Root.ServeHTTP(rec, req)
	return rec
}

// Password 是测试账号统一使用的口令，与 Bearer 创建用户时一致。
const Password = "test-password-123"

// Session 以给定账号登录，返回会话 Cookie 与 CSRF 令牌。
//
// 供「已登录访客」一类场景使用：Public 平面的写请求要过 CSRF 双提交，
// 光有 Cookie 会被 403 挡下。
func (s *Stack) Session(t *testing.T, username string) (cookies []*http.Cookie, csrfToken string) {
	t.Helper()

	rec := s.Do(t, &Request{
		Method: http.MethodPost,
		Path:   "/api/v1/console/auth/login",
		Body:   `{"login":"` + username + `","password":"` + Password + `"}`,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("登录 %s 失败: %d %s", username, rec.Code, rec.Body.String())
	}
	var body struct {
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("登录响应不是合法 JSON: %v", err)
	}
	if body.CSRFToken == "" {
		t.Fatal("登录响应缺少 csrfToken")
	}
	return rec.Result().Cookies(), body.CSRFToken
}
