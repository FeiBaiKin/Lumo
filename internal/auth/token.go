package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/FeiBaiKin/lumo/internal/auth/perm"
)

// TokenPrefix 是 PAT 明文的固定前缀。
//
// 有了它，令牌一旦被误贴到公开场所（issue、日志、代码库），
// 可以被 secret 扫描工具按前缀识别出来。
const TokenPrefix = "lumo_pat_"

// tokenHintLength 是存入 token_hint 的明文前缀长度（不含 TokenPrefix）。
// 仅用于 UI 区分展示，太长会削弱「仅存哈希」的意义。
const tokenHintLength = 6

// ErrInvalidToken 表示令牌不存在、已过期或所属账号已停用。
var ErrInvalidToken = errors.New("访问令牌无效")

// TokenStore 管理 Personal Access Token。
type TokenStore struct {
	db bun.IDB
}

// NewTokenStore 构造 TokenStore。
func NewTokenStore(db bun.IDB) *TokenStore {
	return &TokenStore{db: db}
}

// IssuedToken 是新建令牌的结果，Plaintext 仅在此刻可见。
type IssuedToken struct {
	Token *AccessToken
	// Plaintext 是令牌明文，**只在创建时返回一次**，此后无法找回。
	Plaintext string
}

// CreateTokenParams 是创建令牌的入参。
type CreateTokenParams struct {
	UserID int64
	Name   string
	// Scopes 是令牌的权限清单，**必须是已经解析好的显式列表**：
	// 空列表表示这枚令牌没有任何权限，而不是「继承全部」。
	// 「继承账号全部权限」由 handlers 的 resolveScopes 展开后再传进来。
	Scopes []perm.Permission
	// ExpiresAt 为 nil 表示永不过期。
	ExpiresAt *time.Time
}

// Create 为用户签发新的访问令牌。
func (s *TokenStore) Create(ctx context.Context, params *CreateTokenParams) (*IssuedToken, error) {
	name := strings.TrimSpace(params.Name)
	if name == "" {
		return nil, errors.New("令牌名称不能为空")
	}

	// 校验 scope 必须是已知权限串，拼错的 scope 会表现为难查的 403。
	for _, scope := range params.Scopes {
		if !scope.Valid() {
			return nil, fmt.Errorf("未知权限串 %q", scope)
		}
	}

	secret, err := randomToken()
	if err != nil {
		return nil, err
	}
	plaintext := TokenPrefix + secret

	// 只存显式清单：scopes 列的 NULL/空数组在鉴权时表示「无权限」（fail-closed），
	// 不像过去那样表示「账号全部权限」。
	scopes := params.Scopes
	if scopes == nil {
		scopes = []perm.Permission{}
	}

	token := &AccessToken{
		TokenHash: HashToken(plaintext),
		TokenHint: secret[:min(tokenHintLength, len(secret))],
		UserID:    params.UserID,
		Name:      name,
		Scopes:    scopes,
		ExpiresAt: params.ExpiresAt,
		CreatedAt: time.Now(),
	}
	if _, err := s.db.NewInsert().Model(token).Exec(ctx); err != nil {
		return nil, fmt.Errorf("创建访问令牌: %w", err)
	}

	return &IssuedToken{Token: token, Plaintext: plaintext}, nil
}

// Lookup 按令牌明文查找有效令牌。
func (s *TokenStore) Lookup(ctx context.Context, plaintext string) (*AccessToken, error) {
	if plaintext == "" || !strings.HasPrefix(plaintext, TokenPrefix) {
		return nil, ErrInvalidToken
	}

	token := new(AccessToken)
	err := s.db.NewSelect().
		Model(token).
		Where("at.token_hash = ?", HashToken(plaintext)).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrInvalidToken
		}
		return nil, fmt.Errorf("查询访问令牌: %w", err)
	}
	if token.Expired(time.Now()) {
		return nil, ErrInvalidToken
	}
	return token, nil
}

// TouchLastUsed 记录令牌最近使用时间。
//
// 失败不应影响请求：这只是审计信息。
func (s *TokenStore) TouchLastUsed(ctx context.Context, id int64) error {
	_, err := s.db.NewUpdate().
		Model((*AccessToken)(nil)).
		Set("last_used_at = now()").
		Where("id = ?", id).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("更新令牌使用时间: %w", err)
	}
	return nil
}

// ListForUser 返回用户的令牌列表（不含明文与哈希）。
func (s *TokenStore) ListForUser(ctx context.Context, userID int64) ([]AccessToken, error) {
	var tokens []AccessToken
	err := s.db.NewSelect().
		Model(&tokens).
		Where("at.user_id = ?", userID).
		Order("at.created_at DESC").
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("查询令牌列表: %w", err)
	}
	return tokens, nil
}

// Revoke 删除用户的某个令牌。
//
// 以 user_id 一并过滤，防止越权删除他人令牌。
func (s *TokenStore) Revoke(ctx context.Context, userID, tokenID int64) error {
	res, err := s.db.NewDelete().
		Model((*AccessToken)(nil)).
		Where("id = ? AND user_id = ?", tokenID, userID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("撤销令牌: %w", err)
	}
	return requireOneRow(res, "访问令牌")
}

// RevokeAllForUser 删除用户的全部令牌，用于停用账号或重设密码。
func (s *TokenStore) RevokeAllForUser(ctx context.Context, userID int64) error {
	_, err := s.db.NewDelete().
		Model((*AccessToken)(nil)).
		Where("user_id = ?", userID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("撤销用户全部令牌: %w", err)
	}
	return nil
}

// BearerToken 从 Authorization 头中提取 Bearer 令牌。
func BearerToken(header string) string {
	const prefix = "Bearer "
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(header[len(prefix):])
}
