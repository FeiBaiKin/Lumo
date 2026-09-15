// Package perm 定义权限串、内置角色与所有权规则。
//
// 权限串格式为 <资源>:<动作>，动词统一为 write / write_any / publish / delete_any / manage。
// `_any` 后缀表示「不限所有权」；不带后缀的权限只能操作自己拥有的对象。
package perm

import (
	"fmt"
	"sort"
	"strings"
)

// Permission 是一条权限串。
type Permission string

// v1 权限清单。
const (
	PostsWrite     Permission = "posts:write"
	PostsWriteAny  Permission = "posts:write_any"
	PostsPublish   Permission = "posts:publish"
	PostsDeleteAny Permission = "posts:delete_any"

	PagesWrite     Permission = "pages:write"
	PagesWriteAny  Permission = "pages:write_any"
	PagesPublish   Permission = "pages:publish"
	PagesDeleteAny Permission = "pages:delete_any"

	TaxonomiesManage Permission = "taxonomies:manage"

	CommentsManage    Permission = "comments:manage"
	CommentsManageAny Permission = "comments:manage_any"

	MediaWrite     Permission = "media:write"
	MediaDeleteAny Permission = "media:delete_any"

	// ContentUnsafeHTML 允许正文中的任意 HTML 原样输出到前台（含 iframe 与脚本钩子）。
	//
	// 这是高风险权限，默认只给管理员及以上：未净化的正文会在**站点同源**执行，
	// 而管理 API 与访客页面同源，一段脚本就能以访客（可能是管理员）的身份
	// 调用管理接口。没有这个权限的编辑者，正文在保存时会被允许列表净化。
	ContentUnsafeHTML Permission = "content:unsafe_html"

	MenusManage    Permission = "menus:manage"
	UsersManage    Permission = "users:manage"
	RolesManage    Permission = "roles:manage"
	ThemesManage   Permission = "themes:manage"
	SettingsManage Permission = "settings:manage"

	ExtensionsManage Permission = "extensions:manage"
	PluginsManage    Permission = "plugins:manage"

	SiteDelete   Permission = "site:delete"
	SiteTransfer Permission = "site:transfer"
)

// All 是全部合法权限串，顺序固定以便生成稳定的角色定义与 UI 列表。
var All = []Permission{
	PostsWrite, PostsWriteAny, PostsPublish, PostsDeleteAny,
	PagesWrite, PagesWriteAny, PagesPublish, PagesDeleteAny,
	TaxonomiesManage,
	CommentsManage, CommentsManageAny,
	MediaWrite, MediaDeleteAny,
	ContentUnsafeHTML,
	MenusManage,
	UsersManage,
	RolesManage,
	ThemesManage,
	SettingsManage,
	ExtensionsManage,
	PluginsManage,
	SiteDelete, SiteTransfer,
}

// ResourceLabels 是权限资源段的中文名，供角色编辑器按资源分组时显示标题。
//
// 放在服务端而不是前端写死一份：资源段随模块增加，前端那张表漏掉一个资源时，
// 界面上会以英文标识出现，而没有任何东西提示「这里少了一行」——
// `plugins` 就这么漏过一次（2026-09-15 修）。
var ResourceLabels = map[string]string{
	"posts":      "文章",
	"pages":      "页面",
	"taxonomies": "分类与标签",
	"comments":   "评论",
	"media":      "附件",
	"content":    "正文",
	"menus":      "菜单",
	"users":      "用户",
	"roles":      "角色",
	"themes":     "主题",
	"settings":   "设置",
	"extensions": "扩展记录",
	"plugins":    "插件",
	"site":       "站点",
}

// dangerousPermissions 是会把站点交出去或让脚本在站内执行的权限。
//
// 角色编辑器据此把这几项标红：站长勾选时应当知道它意味着什么，
// 而不是把它当成又一条普通权限。
var dangerousPermissions = map[Permission]bool{
	ContentUnsafeHTML: true,
	SiteDelete:        true,
	SiteTransfer:      true,
}

// Dangerous 报告该权限是否属于高危权限。
func Dangerous(p Permission) bool { return dangerousPermissions[p] }

// anySuffix 标记「不限所有权」的权限。
const anySuffix = "_any"

// String 实现 fmt.Stringer。
func (p Permission) String() string { return string(p) }

// Resource 返回权限串的资源部分，如 posts:write → posts。
func (p Permission) Resource() string {
	resource, _, found := strings.Cut(string(p), ":")
	if !found {
		return ""
	}
	return resource
}

// Action 返回权限串的动作部分，如 posts:write → write。
func (p Permission) Action() string {
	_, action, found := strings.Cut(string(p), ":")
	if !found {
		return ""
	}
	return action
}

// IsAny 报告该权限是否不限所有权。
func (p Permission) IsAny() bool {
	return strings.HasSuffix(p.Action(), anySuffix)
}

// Valid 报告该权限串是否在 v1 清单内。
//
// 只接受清单内的权限，避免拼错的权限串被静默当作「无权限」——
// 那会让配置错误表现为难以排查的 403。
func (p Permission) Valid() bool {
	return validSet[p]
}

var validSet = func() map[Permission]bool {
	m := make(map[Permission]bool, len(All))
	for _, p := range All {
		m[p] = true
	}
	return m
}()

// Parse 校验并返回权限串。
func Parse(s string) (Permission, error) {
	p := Permission(strings.TrimSpace(s))
	if !p.Valid() {
		return "", fmt.Errorf("未知权限串 %q", s)
	}
	return p, nil
}

// 内置角色名。
//
// 2026-09-15 站长把内置角色收成三挡：用户 / 编辑 / 管理员，外加不对站长开放的
// super-admin。原先的 author（只能写自己的、不能发布）取消——它的活由自定义角色承担，
// 命令行的 create-user 也不再默认落在这个角色上。
const (
	RoleSuperAdmin = "super-admin"
	RoleAdmin      = "admin"
	RoleEditor     = "editor"
	// RoleMember 是前台自助注册账号的角色，见 BuiltinRoles 中的说明。
	RoleMember = "member"
)

// RoleMeta 是内置角色的展示信息。
type RoleMeta struct {
	Label       string
	Description string
}

// BuiltinRoleMeta 是内置角色的显示名与描述。
//
// 显示名与描述**随代码走**（每次启动覆盖），权限集合**归站长**（只在角色首次创建时写入）。
// 两者分开的理由：措辞是产品的一部分，升级就该更新；而权限是站长在新版权限清单下的
// 选择，一次升级把它覆盖回去，就会在无声中放宽或收紧一个正在用的角色。
var BuiltinRoleMeta = map[string]RoleMeta{
	RoleSuperAdmin: {
		Label:       "超级管理员",
		Description: "站点的最高权限，含删除站点与转移所有权。不对外开放：不可修改、不可删除，也不出现在角色管理页",
	},
	RoleAdmin: {
		Label:       "管理员",
		Description: "除删除站点与转移所有权外的全部权限，含用户、角色、设置、主题与插件",
	},
	RoleEditor: {
		Label:       "编辑",
		Description: "内容全权：可改可发他人内容、删任何人的文章，并管理分类标签、评论与附件；不碰用户、角色、设置、主题、菜单与插件",
	},
	RoleMember: {
		Label:       "用户",
		Description: "前台注册账号：能登录前台、能被内容与评论归属，但没有任何后台权限",
	},
}

// Locked 报告该角色是否不对站长开放。
//
// 锁定的角色既不可改也不可删，且不在角色管理页出现（仍可分配给用户）：
// 把 super-admin 的权限改坏或把它删掉，等于让站点失去唯一的后门。
func Locked(name string) bool { return name == RoleSuperAdmin }

// BuiltinRoles 是内置角色到其权限集合的映射。
//
// 角色即权限串的集合，允许自定义角色，故此处只是「预设」而非硬编码逻辑。
// 除 super-admin 外，这里给出的只是**首次创建时**的默认值，之后以库中的记录为准
// （见 Store.SeedRoles）；改回默认值走角色管理页的「恢复默认」。
var BuiltinRoles = map[string][]Permission{
	// 全部权限，含删除站点与转移所有权。
	//
	// 唯一一个每次启动都被代码覆盖的角色：它不对站长开放，没有「站长的选择」需要保留。
	RoleSuperAdmin: All,

	// 除站点级危险操作外的全部权限。
	RoleAdmin: {
		PostsWrite, PostsWriteAny, PostsPublish, PostsDeleteAny,
		PagesWrite, PagesWriteAny, PagesPublish, PagesDeleteAny,
		TaxonomiesManage,
		CommentsManage, CommentsManageAny,
		MediaWrite, MediaDeleteAny,
		ContentUnsafeHTML,
		MenusManage,
		UsersManage,
		RolesManage,
		ThemesManage,
		SettingsManage,
		ExtensionsManage,
		PluginsManage,
	},

	// 内容全权（含发布、删任何人的内容）；不碰用户/角色/设置/主题/菜单。
	//
	// **不含 ContentUnsafeHTML**：编辑与管理员是不同的信任级别。
	// 正文原样输出等于在站点同源执行任意脚本，拥有它就能以管理员的身份
	// 调用管理 API —— 那正是「编辑」这个角色不该有的能力。
	RoleEditor: {
		PostsWrite, PostsWriteAny, PostsPublish, PostsDeleteAny,
		PagesWrite, PagesWriteAny, PagesPublish, PagesDeleteAny,
		TaxonomiesManage,
		CommentsManage, CommentsManageAny,
		MediaWrite, MediaDeleteAny,
	},

	// 前台自助注册的账号落在这里（2026-09-14）。
	//
	// 没有任何权限：它的意义是「能登录前台、能被内容与评论归属，但进不了后台」。
	// 必须写成空切片而不是 nil —— nil 会被序列化成 JSON null，撞上
	// roles_permissions_is_array 的 CHECK，表现是启动播种直接失败。
	RoleMember: {},
}

// BuiltinRoleNames 是内置角色名，按权限从大到小排列。
var BuiltinRoleNames = []string{RoleSuperAdmin, RoleAdmin, RoleEditor, RoleMember}

// IsBuiltin 报告角色名是否为内置角色。
//
// 内置角色的**名字**不可更改、角色不可删除：名字是权限判定与播种用的键，
// 删掉它下一次启动又会补回来，只会让人以为删除没生效。权限集合自 2026-09-15 起可改，
// 但 super-admin 例外——它整条都不对外开放（见 Locked）。
func IsBuiltin(name string) bool {
	_, ok := BuiltinRoles[name]
	return ok
}

// Set 是一组权限，用于高效判定。
type Set map[Permission]struct{}

// NewSet 由权限列表构造集合，自动忽略重复项。
func NewSet(permissions ...Permission) Set {
	set := make(Set, len(permissions))
	for _, p := range permissions {
		set[p] = struct{}{}
	}
	return set
}

// Add 并入若干权限。
func (s Set) Add(permissions ...Permission) {
	for _, p := range permissions {
		s[p] = struct{}{}
	}
}

// Has 报告集合是否直接包含该权限。
func (s Set) Has(p Permission) bool {
	_, ok := s[p]
	return ok
}

// List 返回排序后的权限列表，便于稳定输出与断言。
func (s Set) List() []Permission {
	out := make([]Permission, 0, len(s))
	for p := range s {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Allows 判定在给定所有权前提下，集合是否允许该权限。
//
// 规则：
//   - 直接持有 `<资源>:<动作>_any` ⇒ 无条件允许
//   - 直接持有不带 `_any` 的权限 ⇒ 仅当 owned 为真时允许
//   - 请求的权限本身带 `_any` ⇒ 必须直接持有，不能由基础权限升级得来
//
// owned 表示目标对象是否属于当前用户；与所有权无关的权限（如 users:manage）
// 传任意值均可，因为它们只会走「直接持有」这条路径。
func (s Set) Allows(p Permission, owned bool) bool {
	if p.IsAny() {
		// _any 权限不可由基础权限推导，必须显式授予。
		return s.Has(p)
	}
	// 先查不限所有权的版本：同时持有 posts:write 与 posts:write_any 的编辑
	// 必须能操作他人的对象，不能因为先命中基础权限就被所有权规则挡住。
	if s.Has(Permission(string(p) + anySuffix)) {
		return true
	}
	// 不带 _any 的权限受所有权约束。
	return s.Has(p) && owned
}

// HasAny 报告集合是否包含任一给定权限，用于「满足其一即可」的场景。
func (s Set) HasAny(permissions ...Permission) bool {
	for _, p := range permissions {
		if s.Has(p) {
			return true
		}
	}
	return false
}

// EqualSets 报告两组权限（忽略顺序与重复）是否相同。
//
// 用来判断一个内置角色的权限是否已被站长改过，从而决定界面上要不要提示
// 「已自定义」并给出「恢复默认」。与 len(a) == len(b) 这种写法相比，
// 它对重复项和顺序都不敏感。
func EqualSets(a, b []Permission) bool {
	setA, setB := NewSet(a...), NewSet(b...)
	if len(setA) != len(setB) {
		return false
	}
	for p := range setA {
		if !setB.Has(p) {
			return false
		}
	}
	return true
}

// RolePermissions 汇总多个角色的权限并集。
// 未知角色被忽略：角色可能在升级过程中被删除，不应因此让请求 500。
func RolePermissions(roleNames []string, custom map[string][]Permission) Set {
	set := make(Set)
	for _, name := range roleNames {
		if permissions, ok := BuiltinRoles[name]; ok {
			set.Add(permissions...)
		}
		if custom != nil {
			if permissions, ok := custom[name]; ok {
				set.Add(permissions...)
			}
		}
	}
	return set
}
