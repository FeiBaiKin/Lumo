package account

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/auth"
	"github.com/FeiBaiKin/lumo/internal/auth/perm"
)

// 后台管理邮箱验证的接口。放在本模块而不是核心的用户管理里：
// 验证令牌、验证邮件与「此刻能不能发信」的判断都归本模块。
const (
	pathVerificationMail     = "/account/verification-mail"
	pathUserVerificationMail = "/users/{id}/verification-mail"
	pathUserEmailVerified    = "/users/{id}/email-verified"
)

// 同一账号补发验证邮件的频率上限：误点或脚本不该把对方的信箱塞满。
const (
	resendPerUserLimit = 5
	resendWindow       = time.Hour
)

// Routes 实现 app.RouteProvider。
func (m *Module) Routes(r app.Router) {
	if m.store == nil || m.core == nil {
		return
	}
	users := huma.Middlewares{auth.RequirePermission(perm.UsersManage)}
	tags := []string{"users"}

	huma.Register(r.Console(), huma.Operation{
		OperationID: "account-verification-mail-status",
		Method:      http.MethodGet,
		Path:        pathVerificationMail,
		Summary:     "查询此刻能否发送验证邮件",
		Description: "后台据此决定「重发验证邮件」是否可用，不可用时给出原因。",
		Tags:        tags,
		Middlewares: users,
	}, m.verificationMailStatus)
	huma.Register(r.Console(), huma.Operation{
		OperationID:   "user-resend-verification",
		Method:        http.MethodPost,
		Path:          pathUserVerificationMail,
		Summary:       "重发验证邮件",
		Description:   "给未验证邮箱的账号重新签发验证链接并发信。同一账号每小时最多 5 次。",
		Tags:          tags,
		DefaultStatus: http.StatusNoContent,
		Middlewares:   users,
		Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusTooManyRequests,
			http.StatusServiceUnavailable},
	}, m.resendVerification)
	huma.Register(r.Console(), huma.Operation{
		OperationID: "user-mark-email-verified",
		Method:      http.MethodPut,
		Path:        pathUserEmailVerified,
		Summary:     "把邮箱标记为已验证",
		Description: "站长确认过这个邮箱、而对方收不到验证信时手动放行。已验证的账号保持原验证时间不变。",
		Tags:        tags,
		Middlewares: users,
		Errors:      []int{http.StatusNotFound},
	}, m.markEmailVerified)
}

type verificationMailStatusOutput struct {
	Body struct {
		Available bool `json:"available"`
		// Reason 在不可用时说明原因，可用时为空串。
		Reason string `json:"reason"`
	}
}

type consoleUserIDInput struct {
	ID int64 `path:"id" minimum:"1"`
}

type consoleUserOutput struct {
	Body auth.User
}

// verificationBlocker 返回此刻发不出验证邮件的原因；能发时为空串。
func (m *Module) verificationBlocker(ctx context.Context) string {
	if !m.mailAvailable(ctx) {
		return "邮件发送未开启或 SMTP 未配置，请先在「设置 → 邮件发送」里配好"
	}
	if _, baseURL := m.siteInfo(ctx); baseURL == "" {
		return "站点还没有配置对外地址，验证链接无法生成，请先在「设置 → 站点」里填写"
	}
	return ""
}

func (m *Module) verificationMailStatus(ctx context.Context, _ *struct{}) (*verificationMailStatusOutput, error) {
	out := &verificationMailStatusOutput{}
	out.Body.Reason = m.verificationBlocker(ctx)
	out.Body.Available = out.Body.Reason == ""
	return out, nil
}

func (m *Module) resendVerification(ctx context.Context, in *consoleUserIDInput) (*struct{}, error) {
	user, err := m.consoleUser(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	if user.EmailVerified() {
		return nil, huma.Error409Conflict("这个账号的邮箱已经验证过了")
	}
	if reason := m.verificationBlocker(ctx); reason != "" {
		return nil, huma.Error503ServiceUnavailable(reason)
	}
	if !m.limiter.Allow(ctx, "resend:user:"+strconv.FormatInt(user.ID, 10), resendPerUserLimit, resendWindow) {
		return nil, huma.Error429TooManyRequests("给这个账号发得太频繁了，请一小时后再试")
	}
	title, baseURL := m.siteInfo(ctx)
	if err := m.sendVerification(ctx, user.ID, user.Email, title, baseURL); err != nil {
		m.logger.Error("重发验证邮件失败", slog.Int64("user", user.ID), slog.Any("error", err))
		return nil, huma.Error503ServiceUnavailable("验证邮件没能发出，请稍后重试")
	}
	return nil, nil
}

func (m *Module) markEmailVerified(ctx context.Context, in *consoleUserIDInput) (*consoleUserOutput, error) {
	if _, err := m.consoleUser(ctx, in.ID); err != nil {
		return nil, err
	}
	if err := m.store.MarkEmailVerified(ctx, in.ID); err != nil {
		return nil, err
	}
	user, err := m.consoleUser(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	return &consoleUserOutput{Body: *user}, nil
}

// consoleUser 取用户；不存在时映射为 404。
func (m *Module) consoleUser(ctx context.Context, id int64) (*auth.User, error) {
	user, err := m.core.Users.GetUser(ctx, id)
	if errors.Is(err, auth.ErrUserNotFound) {
		return nil, huma.Error404NotFound(err.Error())
	}
	return user, err
}
