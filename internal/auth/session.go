package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/uptrace/bun"
)

// 会话与 CSRF 的 Cookie 名。
//
// __Host- 前缀要求 Secure + Path=/ 且不带 Domain，能防止子域写入伪造 Cookie；
// 但它同时要求 HTTPS，故仅在 secure 模式下启用（见 cookieName）。
const (
	sessionCookieName       = "lumo_session"
	sessionCookieNameSecure = "__Host-lumo_session"
	csrfCookieName          = "lumo_csrf"
	csrfCookieNameSecure    = "__Host-lumo_csrf"

	// CSRFHeaderName 是前端提交 CSRF 令牌的请求头。
	CSRFHeaderName = "X-CSRF-Token"
)

// 会话时长。
const (
	// SessionTTL 是会话有效期。
	SessionTTL = 7 * 24 * time.Hour
	// sessionRefreshThreshold 是滑动续期阈值：距上次活跃超过该时长才写库，
	// 避免每个请求都产生一次 UPDATE。
	sessionRefreshThreshold = time.Hour
)

// tokenBytes 是会话令牌与 CSRF 令牌的随机字节数。
// 32 字节 = 256 位熵，远超暴力枚举可行范围。
const tokenBytes = 32

// ErrInvalidSession 表示会话不存在、已过期或所属账号已停用。
var ErrInvalidSession = errors.New("会话无效")

// SessionStore 管理服务端会话。
type SessionStore struct {
	db bun.IDB
	// Secure 决定是否设置 Secure 属性与 __Host- 前缀，生产环境必须为真。
	Secure bool
}

// NewSessionStore 构造 SessionStore。
func NewSessionStore(db bun.IDB, secure bool) *SessionStore {
	return &SessionStore{db: db, Secure: secure}
}

// IssuedSession 是新建会话的结果，含仅此一次可见的令牌明文。
type IssuedSession struct {
	Session   *Session
	Token     string
	CSRFToken string
}

// Create 为用户签发新会话。
func (s *SessionStore) Create(ctx context.Context, userID int64, userAgent, ip string) (*IssuedSession, error) {
	token, err := randomToken()
	if err != nil {
		return nil, err
	}
	csrfToken, err := randomToken()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	session := &Session{
		TokenHash:  HashToken(token),
		UserID:     userID,
		CSRFToken:  csrfToken,
		UserAgent:  truncate(userAgent, 512),
		IP:         ip,
		ExpiresAt:  now.Add(SessionTTL),
		CreatedAt:  now,
		LastSeenAt: now,
	}
	if _, err := s.db.NewInsert().Model(session).Exec(ctx); err != nil {
		return nil, fmt.Errorf("创建会话: %w", err)
	}

	return &IssuedSession{Session: session, Token: token, CSRFToken: csrfToken}, nil
}

// Lookup 按令牌明文查找有效会话，并返回其所属用户。
//
// 过期会话会被顺带删除，避免表无限增长。
func (s *SessionStore) Lookup(ctx context.Context, token string) (*Session, error) {
	if token == "" {
		return nil, ErrInvalidSession
	}

	session := new(Session)
	err := s.db.NewSelect().
		Model(session).
		Where("s.token_hash = ?", HashToken(token)).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrInvalidSession
		}
		return nil, fmt.Errorf("查询会话: %w", err)
	}

	if session.Expired(time.Now()) {
		// 顺手清理，忽略删除错误：这只是机会性清理，不该影响鉴权结果。
		_ = s.Delete(ctx, token)
		return nil, ErrInvalidSession
	}
	return session, nil
}

// Touch 在超过阈值时更新会话的最近活跃时间并滑动延长有效期。
func (s *SessionStore) Touch(ctx context.Context, session *Session) error {
	now := time.Now()
	if now.Sub(session.LastSeenAt) < sessionRefreshThreshold {
		return nil
	}
	_, err := s.db.NewUpdate().
		Model((*Session)(nil)).
		Set("last_seen_at = now()").
		Set("expires_at = ?", now.Add(SessionTTL)).
		Where("token_hash = ?", session.TokenHash).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("更新会话活跃时间: %w", err)
	}
	return nil
}

// Delete 按令牌明文删除会话（登出）。
func (s *SessionStore) Delete(ctx context.Context, token string) error {
	_, err := s.db.NewDelete().
		Model((*Session)(nil)).
		Where("token_hash = ?", HashToken(token)).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("删除会话: %w", err)
	}
	return nil
}

// DeleteAllForUser 删除某用户的全部会话。
//
// 改密码、停用账号、撤销授权时必须调用：否则旧会话仍然有效，
// 「改了密码却没踢下线」是常见且严重的安全缺陷。
func (s *SessionStore) DeleteAllForUser(ctx context.Context, userID int64) error {
	_, err := s.db.NewDelete().
		Model((*Session)(nil)).
		Where("user_id = ?", userID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("删除用户会话: %w", err)
	}
	return nil
}

// DeleteExpired 清理过期会话，返回删除条数。
func (s *SessionStore) DeleteExpired(ctx context.Context) (int64, error) {
	res, err := s.db.NewDelete().
		Model((*Session)(nil)).
		Where("expires_at <= now()").
		Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("清理过期会话: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, nil //nolint:nilerr // 驱动不支持计数时不视为失败
	}
	return affected, nil
}

// ---------- Cookie 读写 ----------

// SessionCookieName 返回当前模式下的会话 Cookie 名。
func (s *SessionStore) SessionCookieName() string {
	if s.Secure {
		return sessionCookieNameSecure
	}
	return sessionCookieName
}

// CSRFCookieName 返回当前模式下的 CSRF Cookie 名。
func (s *SessionStore) CSRFCookieName() string {
	if s.Secure {
		return csrfCookieNameSecure
	}
	return csrfCookieName
}

// SetCookies 写入会话与 CSRF Cookie。
//
// 会话 Cookie 为 HttpOnly，前端 JS 读不到，防止 XSS 直接窃取会话；
// CSRF Cookie 必须可被 JS 读取（双提交模式），故不设 HttpOnly。
// 两者均为 SameSite=Lax（agent.md §7.1）。
func (s *SessionStore) SetCookies(w http.ResponseWriter, issued *IssuedSession) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.SessionCookieName(),
		Value:    issued.Token,
		Path:     "/",
		Expires:  issued.Session.ExpiresAt,
		MaxAge:   int(SessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   s.Secure,
		SameSite: http.SameSiteLaxMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     s.CSRFCookieName(),
		Value:    issued.CSRFToken,
		Path:     "/",
		Expires:  issued.Session.ExpiresAt,
		MaxAge:   int(SessionTTL.Seconds()),
		HttpOnly: false, // 双提交模式要求前端能读取
		Secure:   s.Secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearCookies 清除会话与 CSRF Cookie。
func (s *SessionStore) ClearCookies(w http.ResponseWriter) {
	for _, name := range []string{s.SessionCookieName(), s.CSRFCookieName()} {
		http.SetCookie(w, &http.Cookie{
			Name:     name,
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: name == s.SessionCookieName(),
			Secure:   s.Secure,
			SameSite: http.SameSiteLaxMode,
		})
	}
}

// TokenFromRequest 从请求 Cookie 中取出会话令牌明文。
func (s *SessionStore) TokenFromRequest(r *http.Request) string {
	cookie, err := r.Cookie(s.SessionCookieName())
	if err != nil {
		return ""
	}
	return cookie.Value
}

// ---------- 令牌工具 ----------

// HashToken 返回令牌的 SHA-256 十六进制哈希。
//
// 会话与 PAT 均只存哈希：库泄漏时无法反推出可用凭据。
// 这里不需要加盐或慢哈希——令牌是 256 位随机值，不存在字典攻击面。
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// randomToken 生成 URL 安全的随机令牌。
func randomToken() (string, error) {
	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成随机令牌: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// ConstantTimeEqual 是定长字符串比较，避免通过响应时间推断令牌内容。
func ConstantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// truncate 按字节截断字符串，避免超长 User-Agent 撑爆列。
func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit]
}
