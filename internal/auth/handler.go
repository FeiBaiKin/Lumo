package auth

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/FeiBaiKin/lumo/internal/httpx"
)

// maxBodyBytes 是认证类请求体的上限，防止超大 JSON 消耗内存。
const maxBodyBytes = 64 << 10

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

// RegisterPublic 挂载无需认证的端点（登录）。
func (h *Handler) RegisterPublic(r chi.Router) {
	r.Post("/auth/login", h.login)
}

// RegisterAuthenticated 挂载需要认证的端点。
func (h *Handler) RegisterAuthenticated(r chi.Router) {
	r.Post("/auth/logout", h.logout)
	r.Get("/auth/me", h.me)
	r.Post("/auth/change-password", h.changePassword)

	r.Get("/auth/tokens", h.listTokens)
	r.Post("/auth/tokens", h.createToken)
	r.Delete("/auth/tokens/{id}", h.revokeToken)
}

// loginRequest 是登录入参。
type loginRequest struct {
	// Login 可以是用户名或邮箱。
	Login    string `json:"login"`
	Password string `json:"password"`
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	issued, user, err := h.service.Login(r.Context(), LoginParams{
		Login:     req.Login,
		Password:  req.Password,
		UserAgent: r.UserAgent(),
		IP:        httpx.ClientIPFrom(r),
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidCredentials):
			// 统一提示，不区分账号不存在与密码错误。
			httpx.Unauthorized(w, r, "用户名或密码错误")
		case errors.Is(err, ErrAccountDisabled):
			httpx.Forbidden(w, r, "账号已被停用")
		default:
			httpx.WriteError(w, r, err, h.logger)
		}
		return
	}

	h.sessions.SetCookies(w, issued)
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{
		"user":      newUserView(user),
		"csrfToken": issued.CSRFToken,
		"expiresAt": issued.Session.ExpiresAt,
	})
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	token := h.sessions.TokenFromRequest(r)
	if err := h.service.Logout(r.Context(), token); err != nil {
		httpx.WriteError(w, r, err, h.logger)
		return
	}
	h.sessions.ClearCookies(w)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	principal := MustFromContext(r.Context())
	if principal.User == nil {
		httpx.Error(w, r, http.StatusUnauthorized, "需要登录")
		return
	}

	// 返回有效权限而非角色权限并集：令牌调用时前者已按 scope 收窄，
	// 前端据此渲染 UI 才不会显示实际不可用的操作。
	permissions := principal.Permissions().List()
	names := make([]string, 0, len(permissions))
	for _, p := range permissions {
		names = append(names, p.String())
	}

	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{
		"user":        newUserView(principal.User),
		"permissions": names,
		"authMethod":  principal.Method,
	})
}

// changePasswordRequest 是修改密码入参。
type changePasswordRequest struct {
	OldPassword string `json:"oldPassword"`
	NewPassword string `json:"newPassword"`
}

func (h *Handler) changePassword(w http.ResponseWriter, r *http.Request) {
	principal := MustFromContext(r.Context())
	if principal.UserID() == 0 {
		httpx.Error(w, r, http.StatusUnauthorized, "需要登录")
		return
	}

	var req changePasswordRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	err := h.service.ChangePassword(r.Context(), principal.UserID(), req.OldPassword, req.NewPassword)
	if err != nil {
		if errors.Is(err, ErrInvalidCredentials) {
			httpx.Unauthorized(w, r, "原密码错误")
			return
		}
		// 密码强度不足属于客户端错误，回传具体原因以便用户修正。
		httpx.BadRequest(w, r, err.Error())
		return
	}

	// 全部会话已失效，清除当前 Cookie 并要求重新登录。
	h.sessions.ClearCookies(w)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) listTokens(w http.ResponseWriter, r *http.Request) {
	principal := MustFromContext(r.Context())
	tokens, err := h.tokens.ListForUser(r.Context(), principal.UserID())
	if err != nil {
		httpx.WriteError(w, r, err, h.logger)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": tokens})
}

// createTokenRequest 是创建 PAT 的入参。
type createTokenRequest struct {
	Name   string   `json:"name"`
	Scopes []string `json:"scopes"`
}

func (h *Handler) createToken(w http.ResponseWriter, r *http.Request) {
	principal := MustFromContext(r.Context())

	var req createTokenRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	scopes, err := parseScopes(req.Scopes)
	if err != nil {
		httpx.BadRequest(w, r, err.Error())
		return
	}

	issued, err := h.tokens.Create(r.Context(), &CreateTokenParams{
		UserID: principal.UserID(),
		Name:   req.Name,
		Scopes: scopes,
	})
	if err != nil {
		httpx.BadRequest(w, r, err.Error())
		return
	}

	// 明文只在此刻返回一次，之后无法找回。
	httpx.WriteJSON(w, r, http.StatusCreated, map[string]any{
		"token":     issued.Token,
		"plaintext": issued.Plaintext,
	})
}

func (h *Handler) revokeToken(w http.ResponseWriter, r *http.Request) {
	principal := MustFromContext(r.Context())

	id, err := parseInt64(chi.URLParam(r, "id"))
	if err != nil {
		httpx.BadRequest(w, r, "令牌 ID 非法")
		return
	}

	if err := h.tokens.Revoke(r.Context(), principal.UserID(), id); err != nil {
		if errors.Is(err, ErrNotFound) {
			httpx.NotFound(w, r, "令牌不存在")
			return
		}
		httpx.WriteError(w, r, err, h.logger)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// userView 是对外暴露的用户视图，显式列出字段以避免误传敏感数据。
type userView struct {
	ID          int64    `json:"id"`
	Username    string   `json:"username"`
	Email       string   `json:"email"`
	DisplayName string   `json:"displayName"`
	AvatarURL   string   `json:"avatarUrl"`
	Roles       []string `json:"roles"`
}

func newUserView(u *User) userView {
	return userView{
		ID:          u.ID,
		Username:    u.Username,
		Email:       u.Email,
		DisplayName: u.Name(),
		AvatarURL:   u.AvatarURL,
		Roles:       u.RoleNames(),
	}
}

// decodeJSON 解析请求体；失败时已写出 400，返回 false。
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	decoder := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes))
	// 拒绝未知字段：拼错的字段名应当报错，而不是被静默忽略。
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		httpx.BadRequest(w, r, "请求体不是合法的 JSON 或包含未知字段")
		return false
	}
	return true
}
