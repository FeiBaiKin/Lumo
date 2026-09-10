package auth_test

import (
	"context"
	"testing"

	"github.com/FeiBaiKin/lumo/internal/auth"
	"github.com/FeiBaiKin/lumo/internal/auth/perm"
)

// testUser 构造带指定权限的用户，用于纯逻辑测试（不触库）。
func testUser(id int64, roleName string, permissions ...perm.Permission) *auth.User {
	return &auth.User{
		ID:       id,
		Username: "tester",
		Roles: []auth.Role{
			{Name: roleName, Permissions: permissions},
		},
	}
}

func TestSessionPrincipalUsesUserPermissions(t *testing.T) {
	t.Parallel()

	user := testUser(1, "author", perm.PostsWrite, perm.MediaWrite)
	p := auth.NewSessionPrincipal(user)

	if p.Method != auth.MethodSession {
		t.Errorf("Method = %q，期望 %q", p.Method, auth.MethodSession)
	}
	if p.UserID() != 1 {
		t.Errorf("UserID = %d", p.UserID())
	}
	if !p.Has(perm.PostsWrite) || !p.Has(perm.MediaWrite) {
		t.Error("会话调用者应持有用户的全部权限")
	}
	if p.Has(perm.UsersManage) {
		t.Error("不应持有未授予的权限")
	}
}

// TestTokenPrincipalCannotEscalate 是本文件最关键的安全测试：
// 令牌 scope 只能收窄权限，绝不能放大。若这里回归，
// 一个 author 的令牌只要写上 users:manage 就能越权管理用户。
func TestTokenPrincipalCannotEscalate(t *testing.T) {
	t.Parallel()

	user := testUser(1, "author", perm.PostsWrite, perm.MediaWrite)

	t.Run("scope 为子集时收窄", func(t *testing.T) {
		t.Parallel()
		p := auth.NewTokenPrincipal(user, &auth.AccessToken{
			ID:     7,
			UserID: 1,
			Scopes: []perm.Permission{perm.PostsWrite},
		})

		if p.Method != auth.MethodToken {
			t.Errorf("Method = %q", p.Method)
		}
		if p.TokenID != 7 {
			t.Errorf("TokenID = %d", p.TokenID)
		}
		if !p.Has(perm.PostsWrite) {
			t.Error("应保留 scope 内的权限")
		}
		if p.Has(perm.MediaWrite) {
			t.Error("scope 外的用户权限应被收窄掉")
		}
	})

	t.Run("scope 超出用户权限时不得放大", func(t *testing.T) {
		t.Parallel()
		p := auth.NewTokenPrincipal(user, &auth.AccessToken{
			ID:     8,
			UserID: 1,
			Scopes: []perm.Permission{perm.UsersManage, perm.SiteDelete},
		})

		if p.Has(perm.UsersManage) || p.Has(perm.SiteDelete) {
			t.Fatal("令牌 scope 放大了用户权限——越权漏洞")
		}
		if len(p.Permissions()) != 0 {
			t.Errorf("有效权限应为空，实际 %v", p.Permissions().List())
		}
	})

	t.Run("scope 为空表示继承用户全部权限", func(t *testing.T) {
		t.Parallel()
		p := auth.NewTokenPrincipal(user, &auth.AccessToken{ID: 9, UserID: 1})

		if !p.Has(perm.PostsWrite) || !p.Has(perm.MediaWrite) {
			t.Error("空 scope 应继承用户全部权限")
		}
		if p.Has(perm.UsersManage) {
			t.Error("继承不应超出用户自身权限")
		}
	})
}

func TestPrincipalAllowsOwnership(t *testing.T) {
	t.Parallel()

	// author 只有 posts:write（不带 _any），受所有权约束。
	author := auth.NewSessionPrincipal(testUser(10, "author", perm.PostsWrite))

	if !author.Allows(perm.PostsWrite, 10) {
		t.Error("应能操作自己的对象")
	}
	if author.Allows(perm.PostsWrite, 99) {
		t.Error("不应能操作他人的对象")
	}
	// ownerID 为 0 表示对象无所有者，视为非本人。
	if author.Allows(perm.PostsWrite, 0) {
		t.Error("无所有者对象不应视为本人所有")
	}

	// editor 有 posts:delete_any，可越过所有权。
	editor := auth.NewSessionPrincipal(testUser(11, "editor",
		perm.PostsWrite, perm.PostsDeleteAny))

	if !editor.Allows(perm.PostsDeleteAny, 99) {
		t.Error("持有 _any 权限应能操作他人对象")
	}
	// _any 权限本身也不受所有权限制，ownerID 为 0 同样允许。
	if !editor.Allows(perm.PostsDeleteAny, 0) {
		t.Error("_any 权限不应受所有权约束")
	}
}

func TestPrincipalIsSuperAdmin(t *testing.T) {
	t.Parallel()

	admin := auth.NewSessionPrincipal(testUser(1, perm.RoleSuperAdmin, perm.All...))
	if !admin.IsSuperAdmin() {
		t.Error("应识别为超级管理员")
	}

	editor := auth.NewSessionPrincipal(testUser(2, perm.RoleEditor, perm.PostsWrite))
	if editor.IsSuperAdmin() {
		t.Error("editor 不应被识别为超级管理员")
	}
}

// TestZeroPrincipalIsSafe 验证未认证时所有权限判定均为 false。
func TestZeroPrincipalIsSafe(t *testing.T) {
	t.Parallel()

	p := &auth.Principal{}
	if p.UserID() != 0 {
		t.Errorf("UserID = %d", p.UserID())
	}
	if p.Has(perm.PostsWrite) {
		t.Error("零值 Principal 不应持有任何权限")
	}
	if p.Allows(perm.PostsWrite, 0) {
		t.Error("零值 Principal 不应允许任何操作")
	}
	if p.IsSuperAdmin() {
		t.Error("零值 Principal 不应是超级管理员")
	}
	if len(p.Permissions()) != 0 {
		t.Error("零值 Principal 权限集合应为空")
	}
}

func TestPrincipalContext(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	if _, ok := auth.FromContext(ctx); ok {
		t.Error("空 context 不应取出调用者")
	}

	// MustFromContext 在未认证时返回安全的零值，而不是 nil 或 panic。
	zero := auth.MustFromContext(ctx)
	if zero == nil {
		t.Fatal("MustFromContext 不应返回 nil")
	}
	if zero.Has(perm.PostsWrite) {
		t.Error("未认证时不应持有权限")
	}

	user := testUser(5, "editor", perm.PostsWrite)
	want := auth.NewSessionPrincipal(user)
	ctx = auth.WithPrincipal(ctx, want)

	got, ok := auth.FromContext(ctx)
	if !ok {
		t.Fatal("应能从 context 取出调用者")
	}
	if got.UserID() != 5 {
		t.Errorf("UserID = %d，期望 5", got.UserID())
	}
}

func TestUserHelpers(t *testing.T) {
	t.Parallel()

	u := &auth.User{ID: 1, Username: "alice", DisplayName: "爱丽丝"}
	if u.Name() != "爱丽丝" {
		t.Errorf("Name() = %q，应优先用显示名", u.Name())
	}

	u.DisplayName = ""
	if u.Name() != "alice" {
		t.Errorf("Name() = %q，显示名缺失时应回退到用户名", u.Name())
	}

	u.Roles = []auth.Role{{Name: "editor"}, {Name: "author"}}
	names := u.RoleNames()
	if len(names) != 2 || names[0] != "editor" || names[1] != "author" {
		t.Errorf("RoleNames() = %v", names)
	}
}
