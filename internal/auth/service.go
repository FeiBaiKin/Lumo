package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/FeiBaiKin/lumo/internal/auth/password"
	"github.com/FeiBaiKin/lumo/internal/auth/perm"
)

// ErrInvalidCredentials 表示登录凭据错误。
//
// 用户不存在与密码错误都返回同一个错误，避免通过响应差异枚举账号。
var ErrInvalidCredentials = errors.New("用户名或密码错误")

// ErrAccountDisabled 表示账号已被停用。
var ErrAccountDisabled = errors.New("账号已被停用")

// Service 编排登录、登出与密码变更。
type Service struct {
	users    *Store
	sessions *SessionStore
	tokens   *TokenStore
	logger   *slog.Logger
}

// NewService 构造 Service。
func NewService(users *Store, sessions *SessionStore, tokens *TokenStore, logger *slog.Logger) *Service {
	return &Service{users: users, sessions: sessions, tokens: tokens, logger: logger}
}

// dummyHash 用于在账号不存在时仍执行一次哈希校验。
//
// 目的是消除时间差：若直接返回，攻击者可通过响应时间区分
// 「账号不存在」与「密码错误」，从而枚举有效账号。
var dummyHash = func() string {
	h, err := password.HashWithParams("dummy-password-for-timing", password.Params{
		Memory: 64 * 1024, Iterations: 3, Parallelism: 1, SaltLength: 16, KeyLength: 32,
	})
	if err != nil {
		return ""
	}
	return h
}()

// LoginParams 是登录入参。
type LoginParams struct {
	// Login 可以是用户名或邮箱。
	Login     string
	Password  string
	UserAgent string
	IP        string
}

// Login 校验凭据并签发会话。
func (s *Service) Login(ctx context.Context, params LoginParams) (*IssuedSession, *User, error) {
	user, err := s.users.FindUserByLogin(ctx, params.Login)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			// 走一次等价开销的哈希校验，抹平时间差。
			if dummyHash != "" {
				_ = password.Verify(params.Password, dummyHash)
			}
			return nil, nil, ErrInvalidCredentials
		}
		return nil, nil, err
	}

	if verifyErr := password.Verify(params.Password, user.PasswordHash); verifyErr != nil {
		if errors.Is(verifyErr, password.ErrMismatch) || errors.Is(verifyErr, password.ErrInvalidHash) {
			return nil, nil, ErrInvalidCredentials
		}
		return nil, nil, verifyErr
	}

	// 密码正确后才检查停用状态：先检查会让攻击者用错误密码探知账号是否存在。
	if user.Disabled {
		return nil, nil, ErrAccountDisabled
	}

	// 明文在手，是唯一能升级哈希强度的时机。
	if password.NeedsRehash(user.PasswordHash) {
		if newHash, hashErr := password.Hash(params.Password); hashErr == nil {
			if setErr := s.users.setPasswordHash(ctx, user.ID, newHash); setErr != nil && s.logger != nil {
				s.logger.Warn("升级密码哈希失败", slog.Any("error", setErr))
			}
		}
	}

	issued, err := s.sessions.Create(ctx, user.ID, params.UserAgent, params.IP)
	if err != nil {
		return nil, nil, err
	}
	if err := s.users.TouchLastLogin(ctx, user.ID); err != nil && s.logger != nil {
		s.logger.Warn("更新登录时间失败", slog.Any("error", err))
	}

	return issued, user, nil
}

// Logout 销毁指定会话。
func (s *Service) Logout(ctx context.Context, sessionToken string) error {
	if sessionToken == "" {
		return nil
	}
	return s.sessions.Delete(ctx, sessionToken)
}

// LogoutSession 销毁已解析的会话；session 为 nil（如令牌调用）时不做任何事。
func (s *Service) LogoutSession(ctx context.Context, session *Session) error {
	if session == nil {
		return nil
	}
	return s.sessions.DeleteByHash(ctx, session.TokenHash)
}

// ChangePassword 校验旧密码后设置新密码。
//
// 成功后销毁该用户的全部会话与令牌：改密码必须使所有既有凭据失效，
// 否则「改了密码却没踢下线」会成为严重的安全缺陷。
func (s *Service) ChangePassword(ctx context.Context, userID int64, oldPlain, newPlain string) error {
	user, err := s.users.FindUserByID(ctx, userID)
	if err != nil {
		return err
	}
	if err := password.Verify(oldPlain, user.PasswordHash); err != nil {
		if errors.Is(err, password.ErrMismatch) {
			return ErrInvalidCredentials
		}
		return err
	}
	return s.ResetPassword(ctx, userID, newPlain)
}

// ResetPassword 强制设置新密码并使既有凭据全部失效。
//
// 供 `lumo admin reset-password` 与管理员操作使用，不校验旧密码。
func (s *Service) ResetPassword(ctx context.Context, userID int64, newPlain string) error {
	if err := s.users.UpdatePassword(ctx, userID, newPlain); err != nil {
		return err
	}
	if err := s.sessions.DeleteAllForUser(ctx, userID); err != nil {
		return err
	}
	if err := s.tokens.RevokeAllForUser(ctx, userID); err != nil {
		return err
	}
	return nil
}

// Disable 停用账号并清除其全部凭据。
func (s *Service) Disable(ctx context.Context, userID int64) error {
	if err := s.users.SetDisabled(ctx, userID, true); err != nil {
		return err
	}
	if err := s.sessions.DeleteAllForUser(ctx, userID); err != nil {
		return err
	}
	return s.tokens.RevokeAllForUser(ctx, userID)
}

// Bootstrap 在无任何用户时创建首个超级管理员。
//
// 返回创建的用户；若已存在用户则返回 nil。
func (s *Service) Bootstrap(ctx context.Context, params *CreateUserParams) (*User, error) {
	count, err := s.users.CountUsers(ctx)
	if err != nil {
		return nil, err
	}
	if count > 0 {
		return nil, nil
	}
	params.Roles = []string{perm.RoleSuperAdmin}
	user, err := s.users.CreateUser(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("创建初始管理员: %w", err)
	}
	return user, nil
}

// CleanupExpiredSessions 清理过期会话，供定期任务调用。
func (s *Service) CleanupExpiredSessions(ctx context.Context) error {
	removed, err := s.sessions.DeleteExpired(ctx)
	if err != nil {
		return err
	}
	if removed > 0 && s.logger != nil {
		s.logger.Info("已清理过期会话", slog.Int64("count", removed))
	}
	return nil
}

// StartSessionCleanup 启动后台会话清理循环，ctx 取消时退出。
func (s *Service) StartSessionCleanup(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Hour
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.CleanupExpiredSessions(ctx); err != nil && s.logger != nil {
				s.logger.Warn("清理过期会话失败", slog.Any("error", err))
			}
		}
	}
}
