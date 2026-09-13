package perm

import "testing"

// TestAllowsWithBaseAndAny 是回归测试：同时持有 posts:write 与 posts:write_any 的编辑
// 必须能操作他人的对象。旧实现先命中基础权限就返回 owned，导致 editor 改不了 author 的文章。
func TestAllowsWithBaseAndAny(t *testing.T) {
	t.Parallel()

	set := NewSet(PostsWrite, PostsWriteAny)
	if !set.Allows(PostsWrite, false) {
		t.Error("持有 write 与 write_any 时应能操作他人的对象")
	}
	if !set.Allows(PostsWrite, true) {
		t.Error("持有 write 与 write_any 时应能操作自己的对象")
	}

	onlyBase := NewSet(PostsWrite)
	if onlyBase.Allows(PostsWrite, false) {
		t.Error("只持有 write 不应能操作他人的对象")
	}
	if !onlyBase.Allows(PostsWrite, true) {
		t.Error("只持有 write 应能操作自己的对象")
	}

	onlyAny := NewSet(PostsWriteAny)
	if !onlyAny.Allows(PostsWrite, false) {
		t.Error("只持有 write_any 也应能操作他人的对象")
	}

	// 内置角色矩阵：editor 能改他人文章，author 不能。
	if !NewSet(BuiltinRoles[RoleEditor]...).Allows(PostsWrite, false) {
		t.Error("editor 应能修改他人的文章（内容全权）")
	}
	if NewSet(BuiltinRoles[RoleAuthor]...).Allows(PostsWrite, false) {
		t.Error("author 不应能修改他人的文章")
	}
}

// TestMemberRoleIsPresentAndEmpty 是回归测试：member 角色必须存在且权限为**空切片**。
//
// 长度 0 与 nil 在 Go 里几乎处处等价，但在这一处不等价——nil 切片经 bun 序列化成
// JSON null，撞上 roles_permissions_is_array 的 CHECK，表现是启动播种直接失败，
// 而且报的是数据库约束而不是 Go 代码。故把「非 nil」也钉进断言。
func TestMemberRoleIsPresentAndEmpty(t *testing.T) {
	t.Parallel()

	permissions, ok := BuiltinRoles[RoleMember]
	if !ok {
		t.Fatal("内置角色 member 不存在")
	}
	if permissions == nil {
		t.Fatal("member 的权限必须是空切片而非 nil，否则播种时会撞上 jsonb 数组 CHECK")
	}
	if len(permissions) != 0 {
		t.Errorf("member 不应有任何权限，实际 %v", permissions)
	}
	if !IsBuiltin(RoleMember) {
		t.Error("member 应是内置角色")
	}
}

// TestAllPermissionCountUnchanged 钉住权限清单的条数。
//
// 加角色不等于加权限：member 的引入不该让 perm.All 多出任何一条。
// 这条断言是「不改权限清单」这个约定的执行者——清单变了就必须有人来解释为什么。
func TestAllPermissionCountUnchanged(t *testing.T) {
	t.Parallel()

	const expected = 23
	if len(All) != expected {
		t.Errorf("perm.All 长度 = %d，期望 %d（agent.md §7.2）", len(All), expected)
	}
}
