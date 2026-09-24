package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/FeiBaiKin/lumo/internal/auth"
	"github.com/FeiBaiKin/lumo/internal/config"
	"github.com/FeiBaiKin/lumo/internal/database"
	"github.com/FeiBaiKin/lumo/internal/testdb"
	"github.com/FeiBaiKin/lumo/internal/version"
	"github.com/FeiBaiKin/lumo/internal/workdir"
)

// testSite 是按 serve 同一条装配路径搭起来的整站，跑在独立 schema 上。
type testSite struct {
	url  string
	site *assembledSite
}

func newTestSite(t *testing.T) *testSite {
	t.Helper()
	cfg := config.Default()
	cfg.Database.DSN = testdb.DSN(t)
	cfg.DataDir = t.TempDir()
	cfg.Update.Enabled = false
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	ctx, cancel := context.WithCancel(context.Background())
	db, err := database.Open(ctx, cfg.Database, false)
	if err != nil {
		t.Fatal(err)
	}
	dataDir, err := workdir.Init(cfg.DataDir, logger)
	if err != nil {
		t.Fatal(err)
	}
	site, err := assembleSite(ctx, cfg, db, dataDir, version.Get(), logger)
	if err != nil {
		t.Fatalf("装配整站: %v", err)
	}
	srv := httptest.NewServer(site.root)
	t.Cleanup(func() {
		srv.Close()
		cancel()
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer closeCancel()
		_ = site.application.Close(closeCtx)
		_ = db.Close()
	})
	return &testSite{url: srv.URL, site: site}
}

// createUser 直接经存储建号，等同于 lumo admin create-user。
func (s *testSite) createUser(t *testing.T, username, role string) *auth.User {
	t.Helper()
	u, err := s.site.core.Users.CreateUser(context.Background(), &auth.CreateUserParams{
		Username: username, Email: username + "@example.com", Password: "password-" + username,
		Roles: []string{role}, EmailVerified: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// client 带着 Cookie 与 CSRF 令牌，像 Console 一样调接口。
type client struct {
	t      *testing.T
	base   string
	http   *http.Client
	csrf   string
	bearer string
}

func (s *testSite) client(t *testing.T) *client {
	jar, _ := cookiejar.New(nil)
	return &client{t: t, base: s.url, http: &http.Client{Jar: jar}}
}

func (c *client) do(method, path string, body any) (status int, out map[string]any) {
	c.t.Helper()
	var reader io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		reader = bytes.NewReader(data)
	}
	req, _ := http.NewRequestWithContext(context.Background(), method, c.base+path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.csrf != "" {
		req.Header.Set(auth.CSRFHeaderName, c.csrf)
	}
	if c.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearer)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

func (c *client) login(login, password string) int {
	c.t.Helper()
	status, out := c.do(http.MethodPost, "/api/v1/console/auth/login", map[string]string{"login": login, "password": password})
	if token, ok := out["csrfToken"].(string); ok {
		c.csrf = token
	}
	return status
}

func mustStatus(t *testing.T, what string, got, want int, body map[string]any) {
	t.Helper()
	if got != want {
		t.Fatalf("%s：状态码 %d，应为 %d（%v）", what, got, want, body)
	}
}

func TestSiteFlows(t *testing.T) {
	s := newTestSite(t)
	s.createUser(t, "admin", "super-admin")
	editorUser := s.createUser(t, "editor", "editor")

	t.Run("登录失败计数到上限后返回 429", func(t *testing.T) {
		c := s.client(t)
		for i := range 8 {
			if status := c.login("no-such-user", "wrong-password"); status != http.StatusUnauthorized {
				t.Fatalf("第 %d 次失败登录返回 %d，应为 401", i+1, status)
			}
		}
		if status := c.login("no-such-user", "wrong-password"); status != http.StatusTooManyRequests {
			t.Fatalf("第 9 次返回 %d，应为 429", status)
		}
	})

	admin := s.client(t)
	mustStatus(t, "管理员登录", admin.login("admin", "password-admin"), http.StatusOK, nil)
	editor := s.client(t)
	mustStatus(t, "编辑登录", editor.login("editor", "password-editor"), http.StatusOK, nil)

	t.Run("访问令牌只能收窄", func(t *testing.T) {
		status, out := editor.do(http.MethodPost, "/api/v1/console/auth/tokens", map[string]any{
			"name": "越权", "password": "password-editor", "scopes": []string{"users:manage"},
		})
		mustStatus(t, "申请超出自身权限的 scope", status, http.StatusBadRequest, out)

		status, out = admin.do(http.MethodPost, "/api/v1/console/auth/tokens", map[string]any{
			"name": "只写文章", "password": "password-admin", "scopes": []string{"posts:write"},
		})
		mustStatus(t, "签发令牌", status, http.StatusCreated, out)
		plaintext, _ := out["plaintext"].(string)
		if !strings.HasPrefix(plaintext, "lumo_pat_") {
			t.Fatalf("没有拿到令牌明文：%v", out)
		}

		pat := s.client(t)
		pat.bearer = plaintext
		status, out = pat.do(http.MethodGet, "/api/v1/console/users", nil)
		mustStatus(t, "只有 posts:write 的令牌列用户", status, http.StatusForbidden, out)
		status, out = pat.do(http.MethodPost, "/api/v1/console/auth/tokens", map[string]any{
			"name": "派生", "password": "password-admin",
		})
		mustStatus(t, "用令牌签发令牌", status, http.StatusForbidden, out)
	})

	t.Run("编辑进不了用户管理", func(t *testing.T) {
		status, out := editor.do(http.MethodGet, "/api/v1/console/users", nil)
		mustStatus(t, "编辑列用户", status, http.StatusForbidden, out)
	})

	var postID float64
	t.Run("编辑的正文保存时被净化，发布后前台可见", func(t *testing.T) {
		status, out := editor.do(http.MethodPost, "/api/v1/console/posts", map[string]any{
			"title": "净化检查", "slug": "sanitize-check", "rawType": "html",
			"raw": `<p>正文</p><script>alert(1)</script><img src="/a.png" onerror="alert(1)">`,
		})
		mustStatus(t, "编辑发文", status, http.StatusCreated, out)
		postID, _ = out["id"].(float64)
		if content, _ := out["content"].(string); strings.Contains(content, "<script") || strings.Contains(content, "onerror") {
			t.Fatalf("没有 content:unsafe_html 的作者，正文应被净化：%s", content)
		}

		anon := s.client(t)
		status, out = anon.do(http.MethodGet, "/api/v1/public/posts/sanitize-check", nil)
		mustStatus(t, "草稿对访客", status, http.StatusNotFound, out)

		status, out = editor.do(http.MethodPost, "/api/v1/console/posts/"+jsonID(postID)+"/publish", map[string]any{})
		mustStatus(t, "发布", status, http.StatusOK, out)
		status, out = anon.do(http.MethodGet, "/api/v1/public/posts/sanitize-check", nil)
		mustStatus(t, "已发布的文章对访客", status, http.StatusOK, out)
	})

	t.Run("同一来源连发评论，第二条判为垃圾", func(t *testing.T) {
		path := "/api/v1/public/posts/" + jsonID(postID) + "/comments"
		status, out := admin.do(http.MethodPost, path, map[string]any{"content": "第一条"})
		mustStatus(t, "第一条评论", status, http.StatusCreated, out)
		if out["status"] == "spam" {
			t.Fatalf("第一条不该是垃圾：%v", out)
		}
		status, out = admin.do(http.MethodPost, path, map[string]any{"content": "紧接着的第二条"})
		mustStatus(t, "第二条评论", status, http.StatusCreated, out)
		if out["status"] != "spam" {
			t.Fatalf("发表间隔内的第二条应判为垃圾，得到 %v", out["status"])
		}
	})

	t.Run("邮箱未验证不能登录，后台标记后可以", func(t *testing.T) {
		if _, err := s.site.application.DB().NewUpdate().Table("users").
			Set("email_verified_at = NULL").Where("id = ?", editorUser.ID).
			Exec(context.Background()); err != nil {
			t.Fatal(err)
		}
		c := s.client(t)
		mustStatus(t, "未验证时登录", c.login("editor", "password-editor"), http.StatusForbidden, nil)

		status, out := admin.do(http.MethodPut, "/api/v1/console/users/"+jsonID(float64(editorUser.ID))+"/email-verified", nil)
		mustStatus(t, "标记为已验证", status, http.StatusOK, out)
		if out["emailVerified"] != true {
			t.Fatalf("返回的用户应已验证：%v", out)
		}
		mustStatus(t, "标记后登录", c.login("editor", "password-editor"), http.StatusOK, nil)
	})

	t.Run("装好之后安装接口锁定", func(t *testing.T) {
		status, out := s.client(t).do(http.MethodPost, "/api/v1/install/apply", map[string]any{
			"database": map[string]any{"host": "127.0.0.1"},
			"site":     map[string]any{"title": "x"},
			"admin":    map[string]any{"username": "evil", "email": "evil@example.com", "password": "password-evil"},
		})
		mustStatus(t, "已安装时调用安装接口", status, http.StatusConflict, out)
	})
}

func jsonID(id float64) string {
	data, _ := json.Marshal(int64(id))
	return string(data)
}
