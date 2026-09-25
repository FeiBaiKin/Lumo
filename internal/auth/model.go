// Package auth 提供用户、角色、会话与访问令牌的存储与鉴权逻辑。
package auth

import (
	"context"
	"time"

	"github.com/uptrace/bun"

	"github.com/FeiBaiKin/lumo/internal/auth/perm"
	"github.com/FeiBaiKin/lumo/internal/hooks"
)

// User 是用户实体。
type User struct {
	bun.BaseModel `bun:"table:users,alias:u"`

	ID       int64  `bun:"id,pk,autoincrement" json:"id"`
	Username string `bun:"username,notnull"    json:"username"`
	Email    string `bun:"email,notnull"       json:"email"`
	// EmailVerifiedAt 为 nil 表示邮箱未验证，该账号不能登录（见 Service.Login）。
	// 后台创建的账号在创建时即置为已验证：管理员当面给的号，再让他去收信毫无意义。
	//
	// json:"-" 而不是导出这个时间值：管理接口直接序列化 User，
	// 而「哪一秒验证的」除了拼出一条精确到秒的账号活动轨迹之外没有任何用处。
	// Console 需要的是「这个号能不能登录」，那是下面 Verified 那个布尔值的事。
	EmailVerifiedAt *time.Time `bun:"email_verified_at" json:"-"`
	// Verified 由 EmailVerifiedAt 推出，扫描每一行后填充（见 AfterScanRow），不是数据库列。
	Verified bool `bun:"-" json:"emailVerified"`
	// PasswordHash 绝不出现在 JSON 中：json:"-" 是防止口令哈希经 API 泄漏的第一道防线。
	PasswordHash string `bun:"password_hash,notnull" json:"-"`
	DisplayName  string `bun:"display_name"          json:"displayName"`
	AvatarURL    string `bun:"avatar_url"            json:"avatarUrl"`
	Bio          string `bun:"bio"                   json:"bio"`
	// BannerURL 是个人中心的封面图（前台自己传，后台不管）。
	// 与 AvatarURL 同源：都指向媒体库里的一条记录，都只存地址不存文件。
	BannerURL   string     `bun:"banner_url"            json:"bannerUrl"`
	Disabled    bool       `bun:"disabled"              json:"disabled"`
	LastLoginAt *time.Time `bun:"last_login_at"         json:"lastLoginAt,omitempty"`
	CreatedAt   time.Time  `bun:"created_at,nullzero"   json:"createdAt"`
	UpdatedAt   time.Time  `bun:"updated_at,nullzero"   json:"updatedAt"`

	// Roles 由 LoadRoles 填充，不是数据库列。
	Roles []Role `bun:"-" json:"roles,omitempty"`
}

// Name 返回用于展示的名称，显示名缺失时回退到用户名。
// HookUser 把用户转成派给插件的动作数据：只有公开资料。
func HookUser(u *User) hooks.User {
	return hooks.User{ID: u.ID, Username: u.Username, DisplayName: u.DisplayName}
}

func (u *User) Name() string {
	if u.DisplayName != "" {
		return u.DisplayName
	}
	return u.Username
}

// EmailVerified 报告账号的邮箱是否已验证。
//
// 判定写在这里而不是散落各处：登录闸门、Console 视图与账户页读的必须是同一个判据。
func (u *User) EmailVerified() bool { return u != nil && u.EmailVerifiedAt != nil }

var _ bun.AfterScanRowHook = (*User)(nil)

// AfterScanRow 实现 bun.AfterScanRowHook：每读出一行就同步 Verified，
// 接口序列化 User 时这个字段因此永远与数据库一致。
func (u *User) AfterScanRow(context.Context) error {
	u.Verified = u.EmailVerifiedAt != nil
	return nil
}

// RoleNames 返回用户的角色名列表。
func (u *User) RoleNames() []string {
	names := make([]string, 0, len(u.Roles))
	for i := range u.Roles {
		names = append(names, u.Roles[i].Name)
	}
	return names
}

// Permissions 汇总用户所有角色的权限并集。
func (u *User) Permissions() perm.Set {
	set := make(perm.Set)
	for i := range u.Roles {
		set.Add(u.Roles[i].Permissions...)
	}
	return set
}

// Role 是角色实体，本质是一组权限串。
type Role struct {
	bun.BaseModel `bun:"table:roles,alias:r"`

	ID          int64             `bun:"id,pk,autoincrement" json:"id"`
	Name        string            `bun:"name,notnull"        json:"name"`
	Label       string            `bun:"label"               json:"label"`
	Description string            `bun:"description"         json:"description"`
	Permissions []perm.Permission `bun:"permissions,type:jsonb" json:"permissions"`
	Builtin     bool              `bun:"builtin"             json:"builtin"`
	// Locked 为真表示该角色不对站长开放：不可改、不可删，也不在角色管理页出现
	// （仍可分配给用户）。目前只有 super-admin 是锁定的。
	Locked bool `bun:"-" json:"locked"`
	// Customized 为真表示内置角色的权限已被站长改过，与代码里的默认值不同。
	// 界面据此提示「已自定义」并给出「恢复默认」；自定义角色恒为假。
	Customized bool      `bun:"-" json:"customized"`
	CreatedAt  time.Time `bun:"created_at,nullzero" json:"createdAt"`
	UpdatedAt  time.Time `bun:"updated_at,nullzero" json:"updatedAt"`
}

// fillMeta 填上两个派生字段。它们不是数据库列，只在输出前算一次。
func (r *Role) fillMeta() {
	r.Locked = r.Builtin && perm.Locked(r.Name)
	if defaults, ok := perm.BuiltinRoles[r.Name]; ok {
		r.Customized = r.Builtin && !perm.EqualSets(r.Permissions, defaults)
	}
}

// DisplayLabel 返回用于展示的角色名，显示名缺失时回退到标识。
//
// 显示名可以由站长改（内置角色的名字也随代码更新），故它只能从服务端取，
// 前端写死一张表就会在下一次改档时漂掉。
func (r *Role) DisplayLabel() string {
	if r.Label != "" {
		return r.Label
	}
	return r.Name
}

// UserRole 是用户与角色的关联。
type UserRole struct {
	bun.BaseModel `bun:"table:user_roles,alias:ur"`

	UserID    int64     `bun:"user_id,pk"`
	RoleID    int64     `bun:"role_id,pk"`
	GrantedAt time.Time `bun:"granted_at,nullzero"`
}

// Session 是服务端会话。
//
// TokenHash 是会话令牌的哈希；令牌明文只存在于客户端 Cookie 中。
type Session struct {
	bun.BaseModel `bun:"table:sessions,alias:s"`

	TokenHash  string    `bun:"token_hash,pk"`
	UserID     int64     `bun:"user_id,notnull"`
	CSRFToken  string    `bun:"csrf_token,notnull"`
	UserAgent  string    `bun:"user_agent"`
	IP         string    `bun:"ip"`
	ExpiresAt  time.Time `bun:"expires_at,notnull"`
	CreatedAt  time.Time `bun:"created_at,nullzero"`
	LastSeenAt time.Time `bun:"last_seen_at,nullzero"`
}

// Expired 报告会话是否已过期。
func (s *Session) Expired(now time.Time) bool {
	return !now.Before(s.ExpiresAt)
}

// AccessToken 是 Personal Access Token（仅存哈希）。
type AccessToken struct {
	bun.BaseModel `bun:"table:access_tokens,alias:at"`

	ID        int64  `bun:"id,pk,autoincrement" json:"id"`
	TokenHash string `bun:"token_hash,notnull"  json:"-"`
	// TokenHint 是令牌前缀，仅用于 UI 区分，不足以用于认证。
	TokenHint string `bun:"token_hint" json:"tokenHint"`
	UserID    int64  `bun:"user_id,notnull" json:"userId"`
	Name      string `bun:"name,notnull"    json:"name"`
	// Scopes 为空表示没有任何权限；签发时省略 scope 会展开成调用者当时的全部权限再落库。
	Scopes     []perm.Permission `bun:"scopes,type:jsonb" json:"scopes"`
	ExpiresAt  *time.Time        `bun:"expires_at"        json:"expiresAt,omitempty"`
	LastUsedAt *time.Time        `bun:"last_used_at"      json:"lastUsedAt,omitempty"`
	CreatedAt  time.Time         `bun:"created_at,nullzero" json:"createdAt"`
}

// Expired 报告令牌是否已过期。ExpiresAt 为 nil 表示永不过期。
func (t *AccessToken) Expired(now time.Time) bool {
	if t.ExpiresAt == nil {
		return false
	}
	return !now.Before(*t.ExpiresAt)
}
