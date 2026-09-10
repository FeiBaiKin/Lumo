package auth

import (
	"context"

	"github.com/FeiBaiKin/lumo/internal/auth/perm"
)

// AuthMethod 标识请求的认证方式。
type AuthMethod string

const (
	// MethodSession 表示经由会话 Cookie 认证（Console）。
	MethodSession AuthMethod = "session"
	// MethodToken 表示经由 Personal Access Token 认证（无头调用）。
	MethodToken AuthMethod = "token"
)

// Principal 是已认证的调用者。
type Principal struct {
	User   *User
	Method AuthMethod
	// TokenID 仅在 Method 为 MethodToken 时有值。
	TokenID int64

	// permissions 是本次调用的**有效**权限：
	// 会话认证时等于用户权限；令牌认证时为用户权限与令牌 scope 的交集。
	permissions perm.Set
}

// NewSessionPrincipal 构造会话调用者。
func NewSessionPrincipal(user *User) *Principal {
	return &Principal{
		User:        user,
		Method:      MethodSession,
		permissions: user.Permissions(),
	}
}

// NewTokenPrincipal 构造令牌调用者。
//
// 令牌 scope 只能收窄权限、不能放大：有效权限取用户权限与 scope 的交集。
// 否则一个 author 的令牌若被写入 users:manage，就会凭空获得越权能力。
func NewTokenPrincipal(user *User, token *AccessToken) *Principal {
	effective := user.Permissions()
	if len(token.Scopes) > 0 {
		narrowed := make(perm.Set, len(token.Scopes))
		for _, scope := range token.Scopes {
			if effective.Has(scope) {
				narrowed[scope] = struct{}{}
			}
		}
		effective = narrowed
	}
	return &Principal{
		User:        user,
		Method:      MethodToken,
		TokenID:     token.ID,
		permissions: effective,
	}
}

// UserID 返回调用者的用户 ID，未认证时返回 0。
func (p *Principal) UserID() int64 {
	if p == nil || p.User == nil {
		return 0
	}
	return p.User.ID
}

// Permissions 返回本次调用的有效权限集合。
func (p *Principal) Permissions() perm.Set {
	if p == nil {
		return perm.Set{}
	}
	return p.permissions
}

// Has 报告调用者是否直接持有该权限（不考虑所有权）。
func (p *Principal) Has(permission perm.Permission) bool {
	if p == nil {
		return false
	}
	return p.permissions.Has(permission)
}

// Allows 在给定所有权前提下判定权限。
//
// ownerID 为目标对象的所有者用户 ID；传 0 表示对象无所有者概念。
func (p *Principal) Allows(permission perm.Permission, ownerID int64) bool {
	if p == nil {
		return false
	}
	owned := ownerID != 0 && ownerID == p.UserID()
	return p.permissions.Allows(permission, owned)
}

// IsSuperAdmin 报告调用者是否为超级管理员。
func (p *Principal) IsSuperAdmin() bool {
	if p == nil || p.User == nil {
		return false
	}
	for i := range p.User.Roles {
		if p.User.Roles[i].Name == perm.RoleSuperAdmin {
			return true
		}
	}
	return false
}

// ---------- 请求上下文 ----------

// contextKey 是本包私有的 context 键类型，避免与其他包冲突。
type contextKey struct{ name string }

var principalKey = &contextKey{name: "auth.principal"}

// WithPrincipal 把调用者放入 context。
func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, principalKey, p)
}

// FromContext 取出调用者，未认证时返回 nil 与 false。
func FromContext(ctx context.Context) (*Principal, bool) {
	p, ok := ctx.Value(principalKey).(*Principal)
	if !ok || p == nil {
		return nil, false
	}
	return p, true
}

// MustFromContext 取出调用者，未认证时返回零值 Principal。
//
// 供已在鉴权中间件之后的处理器使用，避免每处都写 ok 判断；
// 零值 Principal 的所有权限判定均为 false，故失败是安全的。
func MustFromContext(ctx context.Context) *Principal {
	if p, ok := FromContext(ctx); ok {
		return p
	}
	return &Principal{permissions: perm.Set{}}
}
