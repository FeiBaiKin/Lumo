package perm

import "testing"

func TestPermissionParts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		perm     Permission
		resource string
		action   string
		isAny    bool
	}{
		{PostsWrite, "posts", "write", false},
		{PostsPublish, "posts", "publish", false},
		{PostsDeleteAny, "posts", "delete_any", true},
		{MediaDeleteAny, "media", "delete_any", true},
		{UsersManage, "users", "manage", false},
		{Permission("malformed"), "", "", false},
	}

	for _, tt := range tests {
		t.Run(string(tt.perm), func(t *testing.T) {
			t.Parallel()
			if got := tt.perm.Resource(); got != tt.resource {
				t.Errorf("Resource() = %q，期望 %q", got, tt.resource)
			}
			if got := tt.perm.Action(); got != tt.action {
				t.Errorf("Action() = %q，期望 %q", got, tt.action)
			}
			if got := tt.perm.IsAny(); got != tt.isAny {
				t.Errorf("IsAny() = %v，期望 %v", got, tt.isAny)
			}
		})
	}
}

func TestParseRejectsUnknown(t *testing.T) {
	t.Parallel()

	if _, err := Parse("posts:write"); err != nil {
		t.Errorf("合法权限串应解析成功: %v", err)
	}
	// 拼错的权限串必须报错，否则会表现为难查的 403。
	for _, bad := range []string{"posts:wirte", "post:write", "", "posts:*", "users:delete"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("非法权限串 %q 应被拒绝", bad)
		}
	}
}

// TestAllowsOwnershipRule 是本包最关键的测试：
// 所有权规则决定了 author 能否改别人的文章（agent.md §7.2）。
func TestAllowsOwnershipRule(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		held  []Permission
		want  Permission
		owned bool
		allow bool
	}{
		{
			name:  "持有基础权限且对象属于自己",
			held:  []Permission{PostsWrite},
			want:  PostsWrite,
			owned: true,
			allow: true,
		},
		{
			name:  "持有基础权限但对象属于他人",
			held:  []Permission{PostsWrite},
			want:  PostsWrite,
			owned: false,
			allow: false,
		},
		{
			name:  "持有 _any 权限可越过所有权",
			held:  []Permission{PostsDeleteAny},
			want:  PostsDeleteAny,
			owned: false,
			allow: true,
		},
		{
			name:  "基础权限不能推导出 _any 权限",
			held:  []Permission{PostsWrite},
			want:  PostsDeleteAny,
			owned: true,
			allow: false,
		},
		{
			name:  "未持有任何相关权限",
			held:  []Permission{MediaWrite},
			want:  PostsWrite,
			owned: true,
			allow: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			set := NewSet(tt.held...)
			if got := set.Allows(tt.want, tt.owned); got != tt.allow {
				t.Errorf("Allows(%q, owned=%v) = %v，期望 %v", tt.want, tt.owned, got, tt.allow)
			}
		})
	}
}

// TestBuiltinRoleMatrix 逐条核对内置角色矩阵与 agent.md §7.2 一致。
func TestBuiltinRoleMatrix(t *testing.T) {
	t.Parallel()

	t.Run("super-admin 拥有全部权限", func(t *testing.T) {
		t.Parallel()
		set := NewSet(BuiltinRoles[RoleSuperAdmin]...)
		for _, p := range All {
			if !set.Has(p) {
				t.Errorf("super-admin 缺少权限 %q", p)
			}
		}
	})

	t.Run("admin 不含站点级危险操作", func(t *testing.T) {
		t.Parallel()
		set := NewSet(BuiltinRoles[RoleAdmin]...)
		if set.Has(SiteDelete) || set.Has(SiteTransfer) {
			t.Error("admin 不应持有 site:delete 或 site:transfer")
		}
		// 其余权限都应具备。
		for _, p := range All {
			if p == SiteDelete || p == SiteTransfer {
				continue
			}
			if !set.Has(p) {
				t.Errorf("admin 缺少权限 %q", p)
			}
		}
	})

	t.Run("editor 不碰用户/角色/设置/主题/菜单", func(t *testing.T) {
		t.Parallel()
		set := NewSet(BuiltinRoles[RoleEditor]...)
		forbidden := []Permission{
			UsersManage, RolesManage, SettingsManage, ThemesManage, MenusManage,
			SiteDelete, SiteTransfer,
		}
		for _, p := range forbidden {
			if set.Has(p) {
				t.Errorf("editor 不应持有 %q", p)
			}
		}
		// 内容全权，含发布与删除他人内容。
		for _, p := range []Permission{PostsWrite, PostsPublish, PostsDeleteAny, CommentsManage, TaxonomiesManage} {
			if !set.Has(p) {
				t.Errorf("editor 缺少权限 %q", p)
			}
		}
	})

	t.Run("author 不能发布且全部权限受所有权约束", func(t *testing.T) {
		t.Parallel()
		set := NewSet(BuiltinRoles[RoleAuthor]...)
		if set.Has(PostsPublish) {
			t.Error("author 不应持有 posts:publish（需审核）")
		}
		// author 的每条权限都不得带 _any，否则所有权规则失效。
		for _, p := range BuiltinRoles[RoleAuthor] {
			if p.IsAny() {
				t.Errorf("author 不应持有不限所有权的权限 %q", p)
			}
		}
		// 只能改自己的文章。
		if set.Allows(PostsWrite, false) {
			t.Error("author 不应能修改他人文章")
		}
		if !set.Allows(PostsWrite, true) {
			t.Error("author 应能修改自己的文章")
		}
	})
}

func TestBuiltinRolePermissionsAreValid(t *testing.T) {
	t.Parallel()

	for name, permissions := range BuiltinRoles {
		for _, p := range permissions {
			if !p.Valid() {
				t.Errorf("角色 %s 含非法权限串 %q", name, p)
			}
		}
	}
}

func TestIsBuiltin(t *testing.T) {
	t.Parallel()

	for _, name := range BuiltinRoleNames {
		if !IsBuiltin(name) {
			t.Errorf("%q 应被识别为内置角色", name)
		}
	}
	if IsBuiltin("my-custom-role") {
		t.Error("自定义角色不应被识别为内置角色")
	}
}

func TestRolePermissionsUnion(t *testing.T) {
	t.Parallel()

	custom := map[string][]Permission{
		"reviewer": {CommentsManage},
	}
	set := RolePermissions([]string{RoleAuthor, "reviewer", "不存在的角色"}, custom)

	if !set.Has(PostsWrite) {
		t.Error("应含 author 的 posts:write")
	}
	if !set.Has(CommentsManage) {
		t.Error("应含自定义角色的 comments:manage")
	}
	// 未知角色被忽略而非报错，避免升级删角色后请求 500。
	if set.Has(UsersManage) {
		t.Error("不应凭空获得 users:manage")
	}
}

func TestSetList(t *testing.T) {
	t.Parallel()

	set := NewSet(UsersManage, PostsWrite, MediaWrite)
	list := set.List()
	if len(list) != 3 {
		t.Fatalf("List() 长度 = %d", len(list))
	}
	// 必须有序，便于稳定输出与断言。
	for i := 1; i < len(list); i++ {
		if list[i-1] > list[i] {
			t.Errorf("List() 未排序: %v", list)
			break
		}
	}
}

func TestSetHasAny(t *testing.T) {
	t.Parallel()

	set := NewSet(PostsWrite)
	if !set.HasAny(UsersManage, PostsWrite) {
		t.Error("应命中 posts:write")
	}
	if set.HasAny(UsersManage, RolesManage) {
		t.Error("不应命中任何权限")
	}
}
