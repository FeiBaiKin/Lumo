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
