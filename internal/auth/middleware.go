package auth

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/FeiBaiKin/lumo/internal/auth/perm"
	"github.com/FeiBaiKin/lumo/internal/httpx"
)

// Authenticator 把请求解析为调用者。
type Authenticator struct {
	users    *Store
	sessions *SessionStore
	tokens   *TokenStore
	logger   *slog.Logger
}

// NewAuthenticator 构造 Authenticator。
func NewAuthenticator(users *Store, sessions *SessionStore, tokens *TokenStore, logger *slog.Logger) *Authenticator {
	return &Authenticator{users: users, sessions: sessions, tokens: tokens, logger: logger}
}

// Resolve 解析请求中的凭据。
//
// 优先 Authorization: Bearer（无头调用），其次会话 Cookie（Console）。
// 无凭据时返回 (nil, nil)：区分「未认证」与「凭据无效」，
// 前者对公共端点是合法状态。
func (a *Authenticator) Resolve(r *http.Request) (*Principal, error) {
	ctx := r.Context()

	if bearer := BearerToken(r.Header.Get("Authorization")); bearer != "" {
		token, err := a.tokens.Lookup(ctx, bearer)
		if err != nil {
			return nil, err
		}
		user, err := a.users.FindUserByID(ctx, token.UserID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return nil, ErrInvalidToken
			}
			return nil, err
		}
		if user.Disabled {
			return nil, ErrInvalidToken
		}
		// 使用时间是审计信息，写失败不应让请求失败。
		if err := a.tokens.TouchLastUsed(ctx, token.ID); err != nil && a.logger != nil {
			a.logger.Warn("更新令牌使用时间失败", slog.Any("error", err))
		}
		return NewTokenPrincipal(user, token), nil
	}

	sessionToken := a.sessions.TokenFromRequest(r)
	if sessionToken == "" {
		return nil, nil
	}

	session, err := a.sessions.Lookup(ctx, sessionToken)
	if err != nil {
		return nil, err
	}
	user, err := a.users.FindUserByID(ctx, session.UserID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrInvalidSession
		}
		return nil, err
	}
	if user.Disabled {
		// 停用账号的会话立即失效，并顺手清除。
		_ = a.sessions.Delete(ctx, sessionToken)
		return nil, ErrInvalidSession
	}
	if err := a.sessions.Touch(ctx, session); err != nil && a.logger != nil {
		a.logger.Warn("更新会话活跃时间失败", slog.Any("error", err))
	}

	principal := NewSessionPrincipal(user)
	return principal, nil
}

// Middleware 解析凭据并注入 context，但**不**强制要求已认证。
//
// 凭据无效时（过期会话、被撤销的令牌）直接返回 401，而不是当作匿名继续——
// 否则客户端会拿到令人困惑的 403 而不知道该重新登录。
func (a *Authenticator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, err := a.Resolve(r)
		if err != nil {
			a.writeUnauthorized(w, r, err)
			return
		}
		if principal != nil {
			r = r.WithContext(WithPrincipal(r.Context(), principal))
		}
		next.ServeHTTP(w, r)
	})
}

// RequireAuth 要求请求已认证。
func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := FromContext(r.Context()); !ok {
			unauthorized(w, r, "需要登录")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequirePermission 要求调用者直接持有指定权限之一。
//
// 适用于与所有权无关的权限（如 users:manage）。涉及所有权的检查
// 必须在处理器内用 Principal.Allows 完成——中间件此时还不知道目标对象归属谁。
func RequirePermission(permissions ...perm.Permission) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, ok := FromContext(r.Context())
			if !ok {
				unauthorized(w, r, "需要登录")
				return
			}
			if !principal.Permissions().HasAny(permissions...) {
				forbidden(w, r, permissions)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// CSRF 校验非安全方法的 CSRF 令牌（双提交模式）。
//
// 仅对会话认证生效：PAT 调用不带 Cookie，天然无 CSRF 风险，
// 若一并要求 CSRF 头会让无头客户端无法使用。
func (a *Authenticator) CSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isSafeMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}

		principal, ok := FromContext(r.Context())
		if !ok || principal.Method != MethodSession {
			next.ServeHTTP(w, r)
			return
		}

		sessionToken := a.sessions.TokenFromRequest(r)
		session, err := a.sessions.Lookup(r.Context(), sessionToken)
		if err != nil {
			a.writeUnauthorized(w, r, err)
			return
		}

		// 双提交：请求头中的令牌必须与会话绑定的令牌一致。
		// 只比对头与库，不比对 Cookie —— 攻击者可写入 Cookie 但读不到会话记录。
		provided := r.Header.Get(CSRFHeaderName)
		if provided == "" || !ConstantTimeEqual(provided, session.CSRFToken) {
			httpx.WriteProblem(w, r, &httpx.Problem{
				Status: http.StatusForbidden,
				Title:  "CSRF 校验失败",
				Detail: "缺少或不匹配的 " + CSRFHeaderName + " 请求头",
			}, nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// writeUnauthorized 把凭据错误转成 401，其余错误转成 500。
func (a *Authenticator) writeUnauthorized(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrInvalidSession):
		a.clearAndUnauthorized(w, r, "会话已失效，请重新登录")
	case errors.Is(err, ErrInvalidToken):
		unauthorized(w, r, "访问令牌无效或已过期")
	default:
		if a.logger != nil {
			a.logger.Error("鉴权失败", slog.Any("error", err))
		}
		httpx.WriteProblem(w, r, httpx.NewProblem(http.StatusInternalServerError, ""), nil)
	}
}

// clearAndUnauthorized 清除失效 Cookie 后返回 401，避免浏览器反复带上无效凭据。
func (a *Authenticator) clearAndUnauthorized(w http.ResponseWriter, r *http.Request, detail string) {
	a.sessions.ClearCookies(w)
	unauthorized(w, r, detail)
}

// unauthorized 输出 401。
func unauthorized(w http.ResponseWriter, r *http.Request, detail string) {
	httpx.WriteProblem(w, r, &httpx.Problem{
		Status: http.StatusUnauthorized,
		Title:  "Unauthorized",
		Detail: detail,
	}, nil)
}

// forbidden 输出 403，并在扩展成员中列出所需权限，便于前端提示与排错。
func forbidden(w http.ResponseWriter, r *http.Request, required []perm.Permission) {
	names := make([]string, 0, len(required))
	for _, p := range required {
		names = append(names, p.String())
	}
	httpx.WriteProblem(w, r, &httpx.Problem{
		Status:     http.StatusForbidden,
		Title:      "Forbidden",
		Detail:     "权限不足",
		Extensions: map[string]any{"requiredPermissions": names},
	}, nil)
}

// isSafeMethod 报告方法是否为不改变状态的安全方法。
func isSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return true
	default:
		return false
	}
}
