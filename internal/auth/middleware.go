package auth

import (
	"errors"
	"log/slog"
	"net/http"

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
	principal.Session = session
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

// Optional 解析会话但**不**强制认证，供访客前台的 HTML 页面使用。
//
// 与 Middleware 的区别只有一条，但很要紧：凭据无效时 Middleware 返回 401
// （API 客户端必须明确知道该重新登录），而这里清掉失效 Cookie 后按匿名继续——
// 访客带着一枚过期 Cookie 打开首页，该看到首页，而不是一段 JSON 错误。
// 解析过程本身出错（数据库故障）同样按匿名继续并记一条 Warn：
// 一次会话查询失败不该让整站不可访问。
//
// 只对「会话本身失效」清 Cookie，对无效的 PAT 不清：两者可能同时存在
// （浏览器里既有会话 Cookie，某个扩展又带了 Authorization 头），
// 为一枚坏令牌把好好的会话 Cookie 抹掉，会把用户从已登录状态踢出去。
func (a *Authenticator) Optional(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, err := a.Resolve(r)
		switch {
		case err == nil:
			if principal != nil {
				r = r.WithContext(WithPrincipal(r.Context(), principal))
			}
		case errors.Is(err, ErrInvalidSession):
			a.sessions.ClearCookies(w)
		case errors.Is(err, ErrInvalidToken):
			// 无效的 PAT 不影响会话身份，什么都不清。
		default:
			if a.logger != nil {
				a.logger.Warn("前台会话解析失败，按匿名继续",
					slog.String("path", r.URL.Path), slog.Any("error", err))
			}
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

		// 鉴权中间件已把会话随 Principal 带来，免去再查一次库；
		// 调用方直接构造 Principal（如测试）时回退到按 Cookie 查库。
		session := principal.Session
		if session == nil {
			looked, err := a.sessions.Lookup(r.Context(), a.sessions.TokenFromRequest(r))
			if err != nil {
				a.writeUnauthorized(w, r, err)
				return
			}
			session = looked
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
	httpx.WriteProblem(w, r, unauthorizedProblem(detail), nil)
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
