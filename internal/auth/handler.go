package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/FeiBaiKin/lumo/internal/auth/password"
	"github.com/FeiBaiKin/lumo/internal/httpx"
)

// tagAuth 是认证操作在 OpenAPI 中的分组标签。
var tagAuth = []string{"auth"}

// Handler 提供 Console 认证端点。
type Handler struct {
	service  *Service
	sessions *SessionStore
	tokens   *TokenStore
	logger   *slog.Logger
}

// NewHandler 构造 Handler。
func NewHandler(service *Service, sessions *SessionStore, tokens *TokenStore, logger *slog.Logger) *Handler {
	return &Handler{service: service, sessions: sessions, tokens: tokens, logger: logger}
}

// Register 把认证端点挂到 Console 平面：登录走免认证注册面，其余走强制认证注册面。
func (h *Handler) Register(consolePublic, console huma.API) {
	huma.Register(consolePublic, huma.Operation{
		OperationID: "auth-login",
		Method:      http.MethodPost,
		Path:        "/auth/login",
		Summary:     "登录",
		Description: "校验用户名或邮箱与密码，签发服务端会话并经 Set-Cookie 下发。" +
			"响应体中的 csrfToken 须在后续非安全方法请求的 X-CSRF-Token 头中回传。" +
			"失败的尝试按账号与客户端 IP 两个维度限流，超出后返回 429。",
		Tags:   tagAuth,
		Errors: []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests, http.StatusServiceUnavailable},
	}, h.login)

	huma.Register(console, huma.Operation{
		OperationID:   "auth-logout",
		Method:        http.MethodPost,
		Path:          "/auth/logout",
		Summary:       "登出",
		Description:   "销毁当前会话并清除 Cookie。令牌调用时只清除 Cookie，令牌本身需经撤销接口作废。",
		Tags:          tagAuth,
		DefaultStatus: http.StatusNoContent,
	}, h.logout)

	huma.Register(console, huma.Operation{
		OperationID: "auth-me",
		Method:      http.MethodGet,
		Path:        "/auth/me",
		Summary:     "当前用户",
		Description: "返回当前调用者及其本次调用的有效权限。",
		Tags:        tagAuth,
	}, h.me)

	huma.Register(console, huma.Operation{
		OperationID:   "auth-change-password",
		Method:        http.MethodPost,
		Path:          "/auth/change-password",
		Summary:       "修改密码",
		Description:   "校验原密码后设置新密码；成功后该用户全部会话与令牌立即失效，需重新登录。",
		Tags:          tagAuth,
		DefaultStatus: http.StatusNoContent,
		Errors:        []int{http.StatusBadRequest, http.StatusUnauthorized},
	}, h.changePassword)

	huma.Register(console, huma.Operation{
		OperationID: "auth-list-tokens",
		Method:      http.MethodGet,
		Path:        "/auth/tokens",
		Summary:     "列出访问令牌",
		Description: "列出当前用户的全部 Personal Access Token，不含令牌明文或哈希。",
		Tags:        tagAuth,
	}, h.listTokens)

	huma.Register(console, huma.Operation{
		OperationID: "auth-create-token",
		Method:      http.MethodPost,
		Path:        "/auth/tokens",
		Summary:     "创建访问令牌",
		Description: "签发 Personal Access Token，**只能用会话登录调用**，且须重新输入当前账号密码。" +
			"明文只在本响应中返回一次。scope 是你当前权限的子集，留空表示继承当前全部权限" +
			"（服务端会展开为显式清单后落库）。",
		Tags:          tagAuth,
		DefaultStatus: http.StatusCreated,
		Errors:        []int{http.StatusBadRequest, http.StatusForbidden},
	}, h.createToken)

	huma.Register(console, huma.Operation{
		OperationID:   "auth-revoke-token",
		Method:        http.MethodDelete,
		Path:          "/auth/tokens/{id}",
		Summary:       "撤销访问令牌",
		Description:   "只能撤销自己的令牌。",
		Tags:          tagAuth,
		DefaultStatus: http.StatusNoContent,
		Errors:        []int{http.StatusNotFound},
	}, h.revokeToken)
}

// ---------- 登录 / 登出 ----------

// loginInput 是登录入参。
type loginInput struct {
	// UserAgent 记入会话用于审计，不进文档。
	UserAgent string `header:"User-Agent" hidden:"true"`
	Body      struct {
		Login    string `json:"login" minLength:"1" maxLength:"254" doc:"用户名或邮箱"`
		Password string `json:"password" minLength:"1" maxLength:"128" doc:"密码"`
	}
}

// sessionView 是登录成功的响应体。
type sessionView struct {
	User      userView  `json:"user"`
	CSRFToken string    `json:"csrfToken" doc:"后续非安全方法请求须放在 X-CSRF-Token 头中回传"`
	ExpiresAt time.Time `json:"expiresAt" doc:"会话过期时间；每次活跃会滑动延长"`
}

// loginOutput 是登录响应：Cookie 经响应头下发。
type loginOutput struct {
	SetCookie []string `header:"Set-Cookie"`
	Body      sessionView
}

func (h *Handler) login(ctx context.Context, in *loginInput) (*loginOutput, error) {
	issued, user, err := h.service.Login(ctx, LoginParams{
		Login:     in.Body.Login,
		Password:  in.Body.Password,
		UserAgent: in.UserAgent,
		IP:        httpx.ClientIPFromContext(ctx),
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidCredentials):
			// 统一提示，不区分账号不存在与密码错误。
			return nil, huma.Error401Unauthorized("用户名或密码错误")
		case errors.Is(err, ErrAccountDisabled):
			return nil, huma.Error403Forbidden("账号已被停用")
		case errors.Is(err, ErrEmailUnverified):
			return nil, huma.Error403Forbidden("邮箱未验证，请查收验证邮件")
		case errors.Is(err, ErrTooManyAttempts):
			return nil, huma.Error429TooManyRequests(
				"登录尝试过于频繁，请稍后再试。若忘记密码，可用 lumo admin reset-password 重置。")
		case errors.Is(err, password.ErrBusy):
			// 并发哈希额度用满：这是临时的容量问题，不是凭据问题。
			return nil, huma.Error503ServiceUnavailable("服务器繁忙，请稍后重试")
		default:
			return nil, err
		}
	}

	return &loginOutput{
		SetCookie: h.sessions.Cookies(issued),
		Body: sessionView{
			User:      newUserView(user),
			CSRFToken: issued.CSRFToken,
			ExpiresAt: issued.Session.ExpiresAt,
		},
	}, nil
}

// clearCookiesOutput 是只清除 Cookie、无响应体的输出。
type clearCookiesOutput struct {
	SetCookie []string `header:"Set-Cookie"`
}

func (h *Handler) logout(ctx context.Context, _ *struct{}) (*clearCookiesOutput, error) {
	if err := h.service.LogoutSession(ctx, MustFromContext(ctx).Session); err != nil {
		return nil, err
	}
	return &clearCookiesOutput{SetCookie: h.sessions.ClearedCookies()}, nil
}

// ---------- 当前用户 ----------

// meView 是当前用户的响应体。
type meView struct {
	User        userView   `json:"user"`
	Permissions []string   `json:"permissions" doc:"本次调用的有效权限；令牌调用时已按 scope 收窄"`
	AuthMethod  AuthMethod `json:"authMethod" enum:"session,token" doc:"认证方式"`
}

type meOutput struct {
	Body meView
}

func (h *Handler) me(ctx context.Context, _ *struct{}) (*meOutput, error) {
	principal := MustFromContext(ctx)
	if principal.User == nil {
		return nil, huma.Error401Unauthorized("需要登录")
	}

	// 返回有效权限而非角色权限并集：令牌调用时前者已按 scope 收窄，
	// 前端据此渲染 UI 才不会显示实际不可用的操作。
	permissions := principal.Permissions().List()
	names := make([]string, 0, len(permissions))
	for _, p := range permissions {
		names = append(names, p.String())
	}

	return &meOutput{Body: meView{
		User:        newUserView(principal.User),
		Permissions: names,
		AuthMethod:  principal.Method,
	}}, nil
}

// ---------- 修改密码 ----------

type changePasswordInput struct {
	Body struct {
		OldPassword string `json:"oldPassword" minLength:"1" maxLength:"128" doc:"原密码"`
		NewPassword string `json:"newPassword" minLength:"8" maxLength:"128" doc:"新密码，8–128 字节"`
	}
}

func (h *Handler) changePassword(ctx context.Context, in *changePasswordInput) (*clearCookiesOutput, error) {
	principal := MustFromContext(ctx)
	if principal.UserID() == 0 {
		return nil, huma.Error401Unauthorized("需要登录")
	}
	// 口令策略先于任何数据库访问校验：其错误文本面向用户，可以直接回传。
	if err := password.Validate(in.Body.NewPassword); err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}

	err := h.service.ChangePassword(ctx, principal.UserID(), in.Body.OldPassword, in.Body.NewPassword)
	switch {
	case err == nil:
	case errors.Is(err, ErrInvalidCredentials):
		return nil, huma.Error401Unauthorized("原密码错误")
	case errors.Is(err, ErrNotFound):
		return nil, huma.Error401Unauthorized("需要登录")
	default:
		return nil, err
	}

	// 全部会话已失效，清除当前 Cookie 并要求重新登录。
	return &clearCookiesOutput{SetCookie: h.sessions.ClearedCookies()}, nil
}

// ---------- 访问令牌 ----------

// tokenList 是令牌列表响应体。
type tokenList struct {
	Items []AccessToken `json:"items"`
}

type tokenListOutput struct {
	Body tokenList
}

func (h *Handler) listTokens(ctx context.Context, _ *struct{}) (*tokenListOutput, error) {
	principal := MustFromContext(ctx)
	tokens, err := h.tokens.ListForUser(ctx, principal.UserID())
	if err != nil {
		return nil, err
	}
	if tokens == nil {
		tokens = []AccessToken{}
	}
	return &tokenListOutput{Body: tokenList{Items: tokens}}, nil
}

// tokenRequest 是创建令牌的入参。
type tokenRequest struct {
	Name string `json:"name" minLength:"1" maxLength:"128" doc:"令牌名称，仅用于区分"`
	// Password 是当前账号密码，用于二次确认。
	Password string `json:"password" minLength:"1" maxLength:"128" doc:"当前账号密码，用于二次确认"`
	// Scopes 留空表示继承调用者当前的全部权限。
	Scopes    []string   `json:"scopes,omitempty" doc:"权限串子集；留空表示继承调用者当前的全部权限"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty" doc:"过期时间；留空表示永不过期"`
}

type createTokenInput struct {
	Body tokenRequest
}

// issuedTokenView 是创建令牌的响应体。
type issuedTokenView struct {
	Token     AccessToken `json:"token"`
	Plaintext string      `json:"plaintext" doc:"令牌明文，只在此刻返回一次，此后无法找回"`
}

type createTokenOutput struct {
	Body issuedTokenView
}

// createToken 签发访问令牌。
//
// 只允许会话调用，并要求重新验证密码，理由是令牌比会话危险得多：
// 它能脱离浏览器长期使用，且不受 CSRF 与会话撤销的约束。
// 若放开给 PAT 调用，「只读令牌 → 派生一枚全权限令牌」就是一条完整的提权路径
// （空 scopes 在过去恰好表示继承账号全部权限）。
func (h *Handler) createToken(ctx context.Context, in *createTokenInput) (*createTokenOutput, error) {
	principal := MustFromContext(ctx)
	if principal.Method != MethodSession {
		return nil, huma.Error403Forbidden("签发访问令牌必须用会话登录调用，不能用令牌调用")
	}
	if err := h.service.VerifyPassword(ctx, principal.UserID(), in.Body.Password); err != nil {
		switch {
		case errors.Is(err, ErrInvalidCredentials):
			return nil, huma.Error403Forbidden("密码错误，无法签发令牌")
		case errors.Is(err, ErrNotFound):
			return nil, huma.Error401Unauthorized("需要登录")
		default:
			return nil, err
		}
	}

	scopes, err := resolveScopes(in.Body.Scopes, principal.Permissions())
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	if in.Body.ExpiresAt != nil && !in.Body.ExpiresAt.After(time.Now()) {
		return nil, huma.Error400BadRequest("过期时间必须晚于当前时间")
	}

	issued, err := h.tokens.Create(ctx, &CreateTokenParams{
		UserID:    principal.UserID(),
		Name:      in.Body.Name,
		Scopes:    scopes,
		ExpiresAt: in.Body.ExpiresAt,
	})
	if err != nil {
		return nil, err
	}

	// 签发长期凭据是安全审计必须留痕的事件。
	if h.logger != nil {
		h.logger.Info("签发访问令牌",
			slog.Int64("userId", principal.UserID()),
			slog.Int64("tokenId", issued.Token.ID),
			slog.Int("scopes", len(issued.Token.Scopes)),
		)
	}

	// 明文只在此刻返回一次，之后无法找回。
	return &createTokenOutput{Body: issuedTokenView{
		Token:     *issued.Token,
		Plaintext: issued.Plaintext,
	}}, nil
}

type tokenIDInput struct {
	ID int64 `path:"id" minimum:"1" doc:"令牌 ID"`
}

func (h *Handler) revokeToken(ctx context.Context, in *tokenIDInput) (*struct{}, error) {
	principal := MustFromContext(ctx)
	if err := h.tokens.Revoke(ctx, principal.UserID(), in.ID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, huma.Error404NotFound("令牌不存在")
		}
		return nil, err
	}
	return nil, nil //nolint:nilnil // 无响应体，huma 按 DefaultStatus 返回 204
}

// ---------- 视图 ----------

// userView 是对外暴露的用户视图，显式列出字段以避免误传敏感数据。
type userView struct {
	ID          int64    `json:"id"`
	Username    string   `json:"username"`
	Email       string   `json:"email"`
	DisplayName string   `json:"displayName"`
	AvatarURL   string   `json:"avatarUrl"`
	Roles       []string `json:"roles"`
	// EmailVerified 只暴露布尔值，不暴露验证时间：后台需要知道「这个号能不能登录」，
	// 但那个时间戳除了精确到秒的账号活动轨迹之外没有任何用处。
	EmailVerified bool `json:"emailVerified"`
}

func newUserView(u *User) userView {
	return userView{
		ID:            u.ID,
		Username:      u.Username,
		Email:         u.Email,
		DisplayName:   u.Name(),
		AvatarURL:     u.AvatarURL,
		Roles:         u.RoleNames(),
		EmailVerified: u.EmailVerified(),
	}
}
