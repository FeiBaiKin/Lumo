package auth

import (
	"testing"

	"github.com/FeiBaiKin/lumo/internal/auth/perm"
)

// PAT 只能收窄：显式 scope 必须落在调用者权限之内，省略时展开成调用者当前的全部权限。
func TestResolveScopes(t *testing.T) {
	granted := perm.NewSet(perm.PostsWrite, perm.PostsPublish, perm.MediaWrite)

	got, err := resolveScopes([]string{"posts:write", "posts:write", " "}, granted)
	if err != nil || len(got) != 1 || got[0] != perm.PostsWrite {
		t.Fatalf("重复与空白的 scope 应合并成一条，得到 %v, %v", got, err)
	}
	if _, overErr := resolveScopes([]string{"users:manage"}, granted); overErr == nil {
		t.Fatal("超出调用者权限的 scope 应被拒绝，而不是静默忽略")
	}
	if _, badErr := resolveScopes([]string{"posts:everything"}, granted); badErr == nil {
		t.Fatal("不存在的权限串应被拒绝")
	}
	all, err := resolveScopes(nil, granted)
	if err != nil || len(all) != 3 {
		t.Fatalf("省略 scope 应展开成调用者的全部 3 条权限，得到 %v, %v", all, err)
	}
}

// 令牌的有效权限是 scope 与账号当前权限的交集：账号降权后，旧令牌随之失去被收回的权限；
// 空 scope 就是没有任何权限。
func TestTokenPrincipalIntersectsScopes(t *testing.T) {
	user := &User{ID: 1, Roles: []Role{{Name: "editor", Permissions: []perm.Permission{perm.PostsWrite}}}}

	p := NewTokenPrincipal(user, &AccessToken{Scopes: []perm.Permission{perm.PostsWrite, perm.UsersManage}})
	if !p.Permissions().Has(perm.PostsWrite) || p.Permissions().Has(perm.UsersManage) {
		t.Fatalf("有效权限应是交集，得到 %v", p.Permissions().List())
	}
	if empty := NewTokenPrincipal(user, &AccessToken{}); len(empty.Permissions()) != 0 {
		t.Fatalf("空 scope 的令牌应没有任何权限，得到 %v", empty.Permissions().List())
	}
}

// 不带 _any 的权限受所有权约束；_any 必须直接授予，不能由基础权限推导。
func TestPermissionOwnership(t *testing.T) {
	author := perm.NewSet(perm.PostsWrite)
	if !author.Allows(perm.PostsWrite, true) || author.Allows(perm.PostsWrite, false) {
		t.Fatal("posts:write 只能改自己的")
	}
	if author.Allows(perm.PostsWriteAny, true) {
		t.Fatal("posts:write 不能推出 posts:write_any")
	}
	editor := perm.NewSet(perm.PostsWrite, perm.PostsWriteAny)
	if !editor.Allows(perm.PostsWrite, false) {
		t.Fatal("持有 posts:write_any 时应能改别人的")
	}
}
