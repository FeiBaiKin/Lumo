package account

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/mail"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/FeiBaiKin/lumo/internal/auth"
	"github.com/FeiBaiKin/lumo/internal/auth/password"
	"github.com/FeiBaiKin/lumo/internal/auth/perm"
	"github.com/FeiBaiKin/lumo/internal/httpx"
	"github.com/FeiBaiKin/lumo/internal/media"
	"github.com/FeiBaiKin/lumo/internal/settings"
	"github.com/FeiBaiKin/lumo/internal/theme"
)

// 注册与找回密码的限流额度。
//
// 前台登录**不在这里限流**：它直接调 auth.Service.Login，与后台共用同一个
// LoginLimiter 与 argon2 并发闸门。另起一套会让前台成为绕过后台限流的旁路。
const (
	registerPerIPLimit = 5
	registerWindow     = 15 * time.Minute

	forgotPerEmailLimit = 3
	forgotPerIPLimit    = 10
	forgotWindow        = time.Hour

	// resetPerIPLimit 挡的是拿一堆令牌来试的枚举行为，故比注册宽松些但更密。
	resetPerIPLimit = 10
	resetWindow     = 15 * time.Minute
)

// 提示文案。失败提示一律只说「不合法」不说具体规则细节的部分，
// 是为了不给试探者更多反馈。
const (
	noticeFormExpired   = "表单已过期，请重新提交"
	noticeTooFrequent   = "操作过于频繁，请稍后再试"
	noticeRegisterOff   = "注册暂未开放"
	noticeNoSiteURL     = "站点尚未配置对外地址，请联系站长"
	noticeAccountTaken  = "用户名或邮箱已被占用"
	noticeTokenExpired  = "链接已失效，请重新注册或联系站长"
	noticeResetExpired  = "链接无效或已过期，请重新申请"
	noticeUnverified    = "邮箱未验证。请查收验证邮件；没收到的话，用同一邮箱重新提交一次注册表单即可重新发送验证邮件"
	noticeRegisterField = "请检查表单里的问题"
	noticeRegisterFail  = "注册失败，请稍后重试"

	// errPasswordMismatch 是「两次输入不一致」的统一文案。
	// 抽成常量不只是为了过 lint：这句话在注册、重置密码、改密码三处出现，
	// 三处说法不一致本身就是一种 bug。
	errPasswordMismatch = "两次输入的密码不一致"
)

// usernamePattern 与 users_username_format 的 CHECK 一致。
//
// 在 Go 侧先查一遍不是为了替代数据库约束（那是兜底），而是为了给出一句
// 人话的错误提示——直接撞数据库约束的话，用户看到的是 500 或一句英文的约束名。
var usernamePattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// notice 是页面级提示。
type notice struct {
	text string
	kind string
}

func okNotice(text string) notice  { return notice{text: text, kind: "ok"} }
func errNotice(text string) notice { return notice{text: text, kind: "error"} }

// apply 把提示写进表单状态；text 为空时不显示。
func (n notice) apply(f *theme.FormState) {
	if n.text == "" {
		return
	}
	f.Notice = n.text
	f.NoticeKind = n.kind
}

// ---------- 渲染 ----------

// page 渲染一个账户页面。
//
// 每次都重新签发表单 CSRF 令牌：GET 时用户还没提交过，POST 失败时旧令牌
// 可能已被消耗或过期——不下发新的，用户就会陷在一个永远提交不了的页面上。
func (m *Module) page(w http.ResponseWriter, r *http.Request, status int,
	template, kind, title string, form *theme.FormState) {
	m.pageWithParams(w, r, status, template, kind, title, form, nil)
}

// pageWithParams 是 page 的扩展版，多一个注入 Context.Params 的口子。
//
// 只有注册页用得上：注册未开放时它仍然渲染注册页模板（告诉来人「这个站现在不收注册」
// 比一个 404 有用），但必须**不渲染表单**——否则用户能把它填完，提交时再撞一次 404。
func (m *Module) pageWithParams(w http.ResponseWriter, r *http.Request, status int,
	template, kind, title string, form *theme.FormState, params map[string]string) {
	if form == nil {
		form = theme.NewFormState()
	}
	form.CSRFToken = m.csrf.Issue(w)

	pageCtx, err := m.renderer.NewContext(r.Context(), r, kind)
	if err != nil {
		// 渲染器自己的 500 兜底页是私有的（renderError），这里不复刻一份：
		// 一个纯文本响应足以说明问题，而编造第二个兜底页只会多一处要维护的 HTML。
		m.logger.Error("组装账户页上下文失败", slog.Any("error", err))
		http.Error(w, "页面暂时无法显示", http.StatusInternalServerError)
		return
	}
	pageCtx.Title = title
	pageCtx.Form = form
	for key, value := range params {
		pageCtx.Params[key] = value
	}
	m.renderer.Render(w, r, status, template, pageCtx)
}

// formValues 从 POST 表单里取出要回填的字段。
//
// 调用方**绝不能**把密码字段传进来：回填密码意味着它会被写进 HTML，
// 而 HTML 会进浏览器缓存、会被「查看源代码」看到、会被截图带出去。
func formValues(r *http.Request, keys ...string) map[string]string {
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		out[key] = r.PostFormValue(key)
	}
	return out
}

// ---------- 登录 ----------

// loginTitle 是登录页标题。
const loginTitle = "登录"

// getLogin 渲染登录表单；已登录则直接进账户页。
func (m *Module) getLogin(w http.ResponseWriter, r *http.Request) {
	if _, ok := auth.FromContext(r.Context()); ok {
		http.Redirect(w, r, PathAccount, http.StatusFound)
		return
	}

	form := theme.NewFormState()
	form.Values["next"] = safeNext(r.URL.Query().Get("next"))
	loginNoticeFor(r.URL.Query()).apply(form)
	m.page(w, r, http.StatusOK, "login.html", theme.KindLogin, loginTitle, form)
}

// loginNoticeFor 把 query 参数翻译成提示。
//
// 提示走 query 参数而不是 flash cookie：为五句提示语开一个服务端状态是过度设计，
// 而 query 参数刷新一次就没了，正好符合「提示只说一次」的语义。
func loginNoticeFor(query url.Values) notice {
	switch {
	case query.Get("registered") != "":
		return okNotice("注册成功。验证邮件已经发出，请查收并点开链接后再登录。")
	case query.Get("verified") != "":
		return okNotice("邮箱验证成功，现在可以登录了。")
	case query.Get("reset") != "":
		return okNotice("密码已重设，请用新密码登录。")
	case query.Get("changed") != "":
		return okNotice("密码已修改，请用新密码重新登录。")
	default:
		return notice{}
	}
}

// postLogin 处理登录提交。
func (m *Module) postLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		m.renderLoginFailure(w, r, http.StatusBadRequest, errNotice("请求格式不正确"))
		return
	}
	if !m.csrf.Verify(r, r.PostFormValue(FormCSRFField)) {
		m.renderLoginFailure(w, r, http.StatusBadRequest, errNotice(noticeFormExpired))
		return
	}

	login := strings.TrimSpace(r.PostFormValue("login"))
	issued, _, err := m.core.Service.Login(r.Context(), auth.LoginParams{
		Login:     login,
		Password:  r.PostFormValue("password"),
		UserAgent: r.UserAgent(),
		IP:        httpx.ClientIPFrom(r),
	})
	if err != nil {
		m.renderLoginFailure(w, r, loginFailureStatus(err), loginFailureNotice(err))
		return
	}

	m.core.Sessions.SetCookies(w, issued)
	next := safeNext(r.PostFormValue("next"))
	if next == "" {
		next = "/"
	}
	http.Redirect(w, r, next, http.StatusFound)
}

// renderLoginFailure 原地重渲染登录页：失败时回填刚填的值（密码除外），
// 否则用户要重新输一遍用户名才知道自己错在哪。
func (m *Module) renderLoginFailure(w http.ResponseWriter, r *http.Request, status int, n notice) {
	form := theme.NewFormState()
	form.Values["login"] = r.PostFormValue("login")
	form.Values["next"] = safeNext(r.PostFormValue("next"))
	n.apply(form)
	m.page(w, r, status, "login.html", theme.KindLogin, loginTitle, form)
}

// loginFailureStatus 把登录错误映射成 HTTP 状态码。
func loginFailureStatus(err error) int {
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		return http.StatusUnauthorized
	case errors.Is(err, auth.ErrAccountDisabled), errors.Is(err, auth.ErrEmailUnverified):
		return http.StatusForbidden
	case errors.Is(err, auth.ErrTooManyAttempts):
		return http.StatusTooManyRequests
	case errors.Is(err, password.ErrBusy):
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

// loginFailureNotice 把登录错误映射成给用户看的一句话。
//
// 凭据错误不区分「账号不存在」与「密码错误」：服务端两侧的响应本来就该逐字节相同，
// 这里也不该把这个信息漏回去（防账号枚举）。
func loginFailureNotice(err error) notice {
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		return errNotice("用户名或密码错误")
	case errors.Is(err, auth.ErrAccountDisabled):
		return errNotice("账号已被停用")
	case errors.Is(err, auth.ErrEmailUnverified):
		return errNotice(noticeUnverified)
	case errors.Is(err, auth.ErrTooManyAttempts):
		return errNotice("登录尝试过于频繁，请稍后再试")
	case errors.Is(err, password.ErrBusy):
		return errNotice("服务器繁忙，请稍后重试")
	default:
		return errNotice("登录失败，请稍后重试")
	}
}

// safeNext 校验登录后的跳转目标，只接受站内绝对路径。
//
// 这是开放重定向的唯一防线：把 ?next=https://evil.example 原样跳过去，
// 攻击者就能拿本站域名做钓鱼跳板——用户看到的是本站地址，落地的却是别处。
// 判定条件刻意收得很紧：必须以单个 / 开头（//evil.example 会被浏览器当协议相对地址），
// 且不含反斜杠与任何控制字符（部分浏览器把 /\evil.example 也当 //）。
func safeNext(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") {
		return ""
	}
	if strings.ContainsAny(raw, "\\\r\n\t") {
		return ""
	}
	return raw
}

// ---------- 注册 ----------

// registerTitle 是注册页标题。
const registerTitle = "创建账户"

// registrationOpen 报告此刻注册是否真的可用。
//
// 三个条件缺一不可：开关打开、能发信、站点配了对外地址。
// 最后一条不是多余的谨慎——验证链接必须是能点开的绝对地址，
// 没有对外地址时注册出来的账号永远验证不了，连登录都不行。
func (m *Module) registrationOpen(ctx context.Context) bool {
	if !m.settingsOf(ctx).AllowRegistration || !m.mailAvailable(ctx) {
		return false
	}
	// 站点对外地址也在这里兜：验证链接必须是能点开的绝对地址，
	// 没有它注册出来的账号永远验证不了，连登录都不行。
	_, baseURL := m.siteInfo(ctx)
	return baseURL != ""
}

// getRegister 渲染注册表单；注册未开放时按 404 处理。
func (m *Module) getRegister(w http.ResponseWriter, r *http.Request) {
	if !m.registrationOpen(r.Context()) {
		// 对外等于这个路径不存在：不是「有个页面说不能注册」。
		m.renderRegisterClosed(w, r)
		return
	}
	if _, ok := auth.FromContext(r.Context()); ok {
		http.Redirect(w, r, PathAccount, http.StatusFound)
		return
	}

	m.renderRegister(w, r, http.StatusOK, theme.NewFormState(), notice{}, true)
}

// renderRegisterClosed 渲染「注册暂未开放」。
//
// 仍用注册页模板而不是 404 模板：站在这里的人是从某个「注册」链接过来的，
// 告诉他这个站现在不收注册比一个「这里没有内容」的 404 有用得多；
// 状态码取 404 是为了对爬虫和探测脚本如实表明这个路径没有内容。
func (m *Module) renderRegisterClosed(w http.ResponseWriter, r *http.Request) {
	m.renderRegister(w, r, http.StatusNotFound, theme.NewFormState(), errNotice(noticeRegisterOff), false)
}

// renderRegister 渲染注册页。
//
// 站长的「注册页说明」不在这里塞进 Form.Notice：它是公开设置项，
// 主题从 .Setting "account" "registrationNotice" 直接读，页眉注册入口的判断同理。
// 两条路径读同一份声明，才不会出现「界面显示注册开着、页眉却没有入口」。
//
// registrationOpen 这个参数决定模板渲不渲染表单：未开放时给一句说明 + 去登录页的出口，
// 而不是一张填完注定 404 的表单。
func (m *Module) renderRegister(w http.ResponseWriter, r *http.Request, status int,
	form *theme.FormState, n notice, open bool) {
	n.apply(form)
	params := map[string]string{}
	if open {
		params["registrationOpen"] = "1"
	}
	m.pageWithParams(w, r, status, "register.html", theme.KindRegister, registerTitle, form, params)
}

// postRegister 处理注册提交。
func (m *Module) postRegister(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := r.ParseForm(); err != nil {
		m.renderRegisterFailure(w, r, http.StatusBadRequest, errNotice("请求格式不正确"), nil)
		return
	}
	if !m.csrf.Verify(r, r.PostFormValue(FormCSRFField)) {
		m.renderRegisterFailure(w, r, http.StatusBadRequest, errNotice(noticeFormExpired), nil)
		return
	}

	// 蜜罐：正常访客看不见这个字段（.comment-hp 把它移出视口），填了必是机器人。
	// 这里**假装成功**而不是报错：告诉机器人「你被识别了」只会让它换个填法。
	// 不留任何痕迹——评论那边的蜜罐把内容落库标成 spam 是为了留证，注册这边
	// 连用户都不该创建。
	if strings.TrimSpace(r.PostFormValue("website2")) != "" {
		m.logger.Info("注册表单蜜罐被填写，已静默丢弃", slog.String("ip", httpx.ClientIPFrom(r)))
		http.Redirect(w, r, PathLogin+"?registered=1", http.StatusFound)
		return
	}

	if !m.registrationOpen(ctx) {
		m.renderRegisterFailure(w, r, http.StatusNotFound, errNotice(noticeRegisterOff), nil)
		return
	}
	if !m.limiter.Allow("register:ip:"+clientIP(r), registerPerIPLimit, registerWindow) {
		m.renderRegisterFailure(w, r, http.StatusTooManyRequests, errNotice(noticeTooFrequent), nil)
		return
	}

	values, fieldErrors := validateRegistration(r)
	if len(fieldErrors) > 0 {
		form := theme.NewFormState()
		form.Values = values
		form.Errors = fieldErrors
		m.renderRegisterFailure(w, r, http.StatusBadRequest, errNotice(noticeRegisterField), form)
		return
	}

	email := strings.ToLower(strings.TrimSpace(values["email"]))
	plain := r.PostFormValue("password")
	siteTitle, baseURL := m.siteInfo(ctx)

	// 邮箱已被注册：已验证的按占用处理；未验证的把密码换成这次提交的，
	// 重新发一封验证邮件。
	//
	// 「未验证就重新发」这条路径是必需的——否则一个填错邮箱的人再也收不到信，
	// 也永远登录不了，只能找站长改库。但**必须连密码一起换**：
	// 只重发不换密码的话，先注册的人（可能是拿别人邮箱注册的攻击者）
	// 就坐等邮箱主人点开链接把账号验证掉，然后用自己设的密码登进去。
	// 换成密码之后，账号的凭据始终属于最后一次提交的人，而能把它验证掉的只有信箱主人。
	existing, lookupErr := m.store.FindByEmail(ctx, email)
	switch {
	case lookupErr == nil && existing.Verified():
		m.renderRegisterFailure(w, r, http.StatusConflict, errNotice(noticeAccountTaken), filledForm(values))
		return

	case lookupErr == nil:
		if err := m.core.Service.ResetPassword(ctx, existing.ID, plain); err != nil {
			m.logger.Error("重发验证邮件时更新密码失败", slog.Any("error", err))
			m.renderRegisterFailure(w, r, http.StatusInternalServerError, errNotice(noticeRegisterFail), filledForm(values))
			return
		}
		if err := m.sendVerification(ctx, existing.ID, email, siteTitle, baseURL); err != nil {
			m.logger.Error("重发验证邮件失败", slog.Any("error", err))
			m.renderRegisterFailure(w, r, http.StatusInternalServerError, errNotice(noticeRegisterFail), filledForm(values))
			return
		}
		http.Redirect(w, r, PathLogin+"?registered=1", http.StatusFound)
		return

	case !errors.Is(lookupErr, auth.ErrNotFound):
		m.logger.Error("查询邮箱失败", slog.Any("error", lookupErr))
		m.renderRegisterFailure(w, r, http.StatusInternalServerError, errNotice(noticeRegisterFail), filledForm(values))
		return
	}

	user, err := m.core.Users.CreateUser(ctx, &auth.CreateUserParams{
		Username: values["username"],
		Email:    email,
		Password: plain,
		Roles:    []string{perm.RoleMember},
		// 自助注册是唯一传 false 的路径：这条路上的账号必须自己证明邮箱可达。
		EmailVerified: false,
	})
	if err != nil {
		if errors.Is(err, auth.ErrDuplicate) {
			// 合并成一句，不告诉对方究竟哪一个被占——那同样是枚举面。
			m.renderRegisterFailure(w, r, http.StatusConflict, errNotice(noticeAccountTaken), filledForm(values))
			return
		}
		if errors.Is(err, password.ErrEmptyPassword) ||
			strings.Contains(err.Error(), "密码") {
			form := filledForm(values)
			form.Errors["password"] = err.Error()
			m.renderRegisterFailure(w, r, http.StatusBadRequest, errNotice(noticeRegisterField), form)
			return
		}
		m.logger.Error("创建注册用户失败", slog.Any("error", err))
		m.renderRegisterFailure(w, r, http.StatusInternalServerError, errNotice(noticeRegisterFail), filledForm(values))
		return
	}

	if err := m.sendVerification(ctx, user.ID, email, siteTitle, baseURL); err != nil {
		m.logger.Error("发送验证邮件失败", slog.Any("error", err))
		m.renderRegisterFailure(w, r, http.StatusInternalServerError, errNotice(noticeRegisterFail), filledForm(values))
		return
	}
	http.Redirect(w, r, PathLogin+"?registered=1", http.StatusFound)
}

// sendVerification 签发一枚验证令牌并把邮件放进队列。
func (m *Module) sendVerification(ctx context.Context, userID int64, email, siteTitle, baseURL string) error {
	token, err := m.tokens.Issue(ctx, userID, PurposeVerifyEmail, VerifyEmailTTL)
	if err != nil {
		return err
	}
	link := mailLink(baseURL, PathVerifyEmail, map[string]string{"token": token})
	if link == "" {
		// 如实报错而不是发一封链接是相对地址的信：用户点开只会得到一个打不开的页面。
		m.logger.Error("站点未配置对外地址，验证邮件未发送", slog.String("email", email))
		return errors.New(noticeNoSiteURL)
	}
	m.enqueue(ctx, verificationMail(email, siteTitle, link))
	return nil
}

// filledForm 构造一个带回填值的表单状态。
func filledForm(values map[string]string) *theme.FormState {
	form := theme.NewFormState()
	form.Values = values
	return form
}

// renderRegisterFailure 原地重渲染注册页。
func (m *Module) renderRegisterFailure(w http.ResponseWriter, r *http.Request, status int,
	n notice, form *theme.FormState) {
	if form == nil {
		form = theme.NewFormState()
	}
	// 走到这里说明注册是开着的（处理器在更早处已经把未开放的情形挡掉了），
	// 故照常渲染表单，让用户改完就能重交。
	m.renderRegister(w, r, status, form, n, true)
}

// validateRegistration 校验注册表单，返回回填值与逐字段错误。
func validateRegistration(r *http.Request) (values, errorsByField map[string]string) {
	values = formValues(r, "username", "email")
	values["username"] = auth.NormalizeUsername(values["username"])
	values["email"] = strings.TrimSpace(strings.ToLower(values["email"]))

	errorsByField = map[string]string{}
	if !usernamePattern.MatchString(values["username"]) ||
		len(values["username"]) < 2 || len(values["username"]) > 64 {
		errorsByField["username"] = "2–64 位小写字母、数字与连字符，首尾须为字母或数字"
	}
	if _, err := mail.ParseAddress(values["email"]); err != nil || len(values["email"]) > 254 {
		errorsByField["email"] = "请填一个有效的邮箱地址"
	}

	// 密码不回填，故这里读的是原始表单值而不是 values。
	plain := r.PostFormValue("password")
	switch err := password.Validate(plain); {
	case err != nil && errors.Is(err, password.ErrEmptyPassword):
		errorsByField["password"] = "请设置密码"
	case err != nil:
		errorsByField["password"] = err.Error()
	case plain != r.PostFormValue("passwordConfirm"):
		errorsByField["passwordConfirm"] = errPasswordMismatch
	}
	return values, errorsByField
}

// ---------- 邮箱验证 ----------

// getVerifyEmail 消费验证令牌并标记邮箱已验证。
//
// 用 GET 而不是让用户再点一次按钮：验证链接从邮件客户端点开必然是一次 GET，
// 硬要做成 POST 就得在页面上再放一个「确认」按钮，多一道手续换不来任何安全
// ——这枚令牌只出现在用户的信箱里，而它做的事（把邮箱标记为可达）本身不危险。
func (m *Module) getVerifyEmail(w http.ResponseWriter, r *http.Request) {
	userID, err := m.tokens.Consume(r.Context(), r.URL.Query().Get("token"), PurposeVerifyEmail)
	if err != nil {
		if !errors.Is(err, ErrInvalidToken) {
			m.logger.Warn("消费验证令牌失败", slog.Any("error", err))
		}
		m.renderLoginFailure(w, r, http.StatusBadRequest, errNotice(noticeTokenExpired))
		return
	}
	if err := m.store.MarkEmailVerified(r.Context(), userID); err != nil {
		m.logger.Error("标记邮箱已验证失败", slog.Any("error", err))
		m.renderLoginFailure(w, r, http.StatusInternalServerError, errNotice("验证失败，请稍后重试"))
		return
	}
	http.Redirect(w, r, PathLogin+"?verified=1", http.StatusFound)
}

// ---------- 找回密码 ----------

// forgotTitle 是找回密码页标题。
const forgotTitle = "重置密码"

// getForgotPassword 渲染找回密码表单。
func (m *Module) getForgotPassword(w http.ResponseWriter, r *http.Request) {
	form := theme.NewFormState()
	if r.URL.Query().Get("sent") != "" {
		okNotice("如果这个邮箱在站上且已验证，重置链接已经发出，请查收邮件。").apply(form)
	}
	m.page(w, r, http.StatusOK, "forgot-password.html", theme.KindForgotPassword, forgotTitle, form)
}

// postForgotPassword 处理找回密码提交。
//
// **无论邮箱是否存在都返回同一个结果**：响应一旦区分，这个端点就变成了
// 一个账号枚举接口——拿一份邮箱列表刷一遍就知道谁在站上注册过。
func (m *Module) postForgotPassword(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := r.ParseForm(); err != nil {
		m.renderForgotFailure(w, r, http.StatusBadRequest, errNotice("请求格式不正确"))
		return
	}
	if !m.csrf.Verify(r, r.PostFormValue(FormCSRFField)) {
		m.renderForgotFailure(w, r, http.StatusBadRequest, errNotice(noticeFormExpired))
		return
	}

	email := strings.ToLower(strings.TrimSpace(r.PostFormValue("email")))
	ip := clientIP(r)
	if !m.limiter.Allow("forgot:email:"+email, forgotPerEmailLimit, forgotWindow) ||
		!m.limiter.Allow("forgot:ip:"+ip, forgotPerIPLimit, forgotWindow) {
		m.renderForgotFailure(w, r, http.StatusTooManyRequests, errNotice(noticeTooFrequent))
		return
	}

	// 限流判定在前、查库在后：被限流的请求不该再花一次数据库往返。
	user, err := m.store.FindVerifiedByEmail(ctx, email)
	switch {
	case err == nil:
		siteTitle, baseURL := m.siteInfo(ctx)
		if baseURL == "" {
			// 这里**不能**改成给用户报错：响应一旦随邮箱是否存在而变化，
			// 枚举接口就成立了。只能记日志，让站长自己去补配置。
			m.logger.Error("站点未配置对外地址，密码重置邮件未发送", slog.String("email", email))
		} else {
			m.sendReset(ctx, user.ID, email, siteTitle, baseURL)
		}
	case errors.Is(err, auth.ErrNotFound):
		// 邮箱不存在或未验证：什么都不做，响应与成功时完全一致。
	default:
		m.logger.Error("查询邮箱失败", slog.Any("error", err))
	}

	http.Redirect(w, r, PathForgotPassword+"?sent=1", http.StatusFound)
}

// sendReset 签发重置令牌并投递邮件。失败只记日志——路径不返回错误，
// 因为返回错误就得把错误显示出来，而显示的差异本身就是信息泄漏。
func (m *Module) sendReset(ctx context.Context, userID int64, email, siteTitle, baseURL string) {
	token, err := m.tokens.Issue(ctx, userID, PurposeResetPassword, ResetPasswordTTL)
	if err != nil {
		m.logger.Error("签发重置令牌失败", slog.String("email", email), slog.Any("error", err))
		return
	}
	link := mailLink(baseURL, PathResetPassword, map[string]string{"token": token})
	if link == "" {
		return
	}
	m.enqueue(ctx, resetMail(email, siteTitle, link))
}

// renderForgotFailure 原地重渲染找回密码页。
func (m *Module) renderForgotFailure(w http.ResponseWriter, r *http.Request, status int, n notice) {
	form := theme.NewFormState()
	form.Values["email"] = r.PostFormValue("email")
	n.apply(form)
	m.page(w, r, status, "forgot-password.html", theme.KindForgotPassword, forgotTitle, form)
}

// ---------- 重置密码 ----------

// resetTitle 是重置密码页标题。
const resetTitle = "设置新密码"

// getResetPassword 校验令牌并渲染设置新密码的表单。
//
// 只查验**不消费**：用户还没设新密码，消费掉令牌等于让一次误刷新就把链接烧掉。
// 真正的消费发生在 POST，那一步才是原子的。
func (m *Module) getResetPassword(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	form := theme.NewFormState()
	if _, err := m.tokens.Check(r.Context(), token, PurposeResetPassword); err != nil {
		if !errors.Is(err, ErrInvalidToken) {
			m.logger.Warn("查验重置令牌失败", slog.Any("error", err))
		}
		errNotice(noticeResetExpired).apply(form)
		m.page(w, r, http.StatusBadRequest, "reset-password.html", theme.KindResetPassword, resetTitle, form)
		return
	}
	form.Token = token
	m.page(w, r, http.StatusOK, "reset-password.html", theme.KindResetPassword, resetTitle, form)
}

// postResetPassword 处理设置新密码的提交。
func (m *Module) postResetPassword(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := r.ParseForm(); err != nil {
		m.renderResetFailure(w, r, http.StatusBadRequest, errNotice("请求格式不正确"), "")
		return
	}
	if !m.csrf.Verify(r, r.PostFormValue(FormCSRFField)) {
		// 令牌本身还是好的（还没被消费），故带上它重新渲染，用户不必再去翻邮件。
		m.renderResetFailure(w, r, http.StatusBadRequest, errNotice(noticeFormExpired), r.PostFormValue("token"))
		return
	}
	if !m.limiter.Allow("reset:ip:"+clientIP(r), resetPerIPLimit, resetWindow) {
		m.renderResetFailure(w, r, http.StatusTooManyRequests, errNotice(noticeTooFrequent), r.PostFormValue("token"))
		return
	}

	token := r.PostFormValue("token")
	plain := r.PostFormValue("newPassword")

	// 先校验表单再消费令牌。顺序与「消费了再说」相反，因为消费是不可逆的：
	// 两次输入不一致这种纯手误不该把用户手里唯一的那枚链接烧掉。
	// 这不削弱原子性——真正需要原子的是「同一封邮件被并发提交时只能生效一次」，
	// 而那条保证落在下面的 Consume 上，它仍然只可能有一个调用者成功。
	if err := password.Validate(plain); err != nil {
		form := theme.NewFormState()
		form.Token = token
		form.Errors["newPassword"] = err.Error()
		errNotice(noticeRegisterField).apply(form)
		m.page(w, r, http.StatusBadRequest, "reset-password.html", theme.KindResetPassword, resetTitle, form)
		return
	}
	if plain != r.PostFormValue("newPasswordConfirm") {
		form := theme.NewFormState()
		form.Token = token
		form.Errors["newPasswordConfirm"] = errPasswordMismatch
		errNotice(noticeRegisterField).apply(form)
		m.page(w, r, http.StatusBadRequest, "reset-password.html", theme.KindResetPassword, resetTitle, form)
		return
	}

	userID, err := m.tokens.Consume(ctx, token, PurposeResetPassword)
	if err != nil {
		if !errors.Is(err, ErrInvalidToken) {
			m.logger.Warn("消费重置令牌失败", slog.Any("error", err))
		}
		m.renderResetFailure(w, r, http.StatusBadRequest, errNotice(noticeResetExpired), "")
		return
	}
	// ResetPassword 会一并清掉该用户的全部会话与访问令牌：
	// 改密码必须使所有既有凭据失效，否则「改了密码却没踢下线」是严重的安全缺陷。
	if err := m.core.Service.ResetPassword(ctx, userID, plain); err != nil {
		m.logger.Error("重置密码失败", slog.Any("error", err))
		m.renderResetFailure(w, r, http.StatusInternalServerError, errNotice("重置失败，请稍后重试"), "")
		return
	}
	http.Redirect(w, r, PathLogin+"?reset=1", http.StatusFound)
}

// renderResetFailure 原地重渲染重置密码页；token 为空表示这枚链接已经不能用了。
func (m *Module) renderResetFailure(w http.ResponseWriter, r *http.Request, status int, n notice, token string) {
	form := theme.NewFormState()
	form.Token = token
	n.apply(form)
	m.page(w, r, status, "reset-password.html", theme.KindResetPassword, resetTitle, form)
}

// ---------- 账户页 ----------

// getAccount 渲染账户页；未登录则跳登录页并带上回跳地址。
func (m *Module) getAccount(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || principal.User == nil {
		http.Redirect(w, r, PathLogin+"?next="+PathAccount, http.StatusFound)
		return
	}
	// 保存成功走的是重定向（PRG），提示只能借查询串带回来。
	// 与登录页的 ?changed=1 同一手法，只是这里的话短，直接写在调用处。
	n := notice{}
	if r.URL.Query().Get("saved") == "1" {
		n = okNotice("资料已保存")
	}
	m.renderAccount(w, r, http.StatusOK, theme.NewFormState(), principal.User, n)
}

// renderAccount 渲染账户页。
//
// 账户页要显示邮箱、注册时间与角色，这些不在 CurrentUserView 里（那是页眉用的视图，
// 刻意只放全站都该看到的字段）。它们经 Context.Params 传给模板——那正是
// 「路由未消费的额外参数」这个槽位的用途。日期在这里就格式化好，
// 模板不必知道时区与格式串，也就不会各写各的。
//
// 个人资料的四项（昵称、简介、头像、封面）同样走 Params：它们只在**这一页**上出现，
// 塞进 CurrentUserView 等于让每个页面的上下文都背上它们。
func (m *Module) renderAccount(w http.ResponseWriter, r *http.Request, status int,
	form *theme.FormState, user *auth.User, n notice) {
	n.apply(form)

	// 资料表单的初值：GET 时是库里的现值；POST 失败时调用方已经把刚填的值放进
	// form.Values 了，那时不能覆盖——否则用户填错一次，回来看到的还是旧昵称。
	if _, ok := form.Values["displayName"]; !ok {
		form.Values["displayName"] = user.Name()
	}
	if _, ok := form.Values["bio"]; !ok {
		form.Values["bio"] = user.Bio
	}

	pageCtx, err := m.renderer.NewContext(r.Context(), r, theme.KindAccount)
	if err != nil {
		m.logger.Error("组装账户页上下文失败", slog.Any("error", err))
		http.Error(w, "页面暂时无法显示", http.StatusInternalServerError)
		return
	}
	form.CSRFToken = m.csrf.Issue(w)
	pageCtx.Title = user.Name()
	pageCtx.Form = form
	pageCtx.Params["email"] = user.Email
	pageCtx.Params["joinedAt"] = m.chineseDate(r.Context(), user.CreatedAt)
	pageCtx.Params["roles"] = roleLabels(user)
	pageCtx.Params["username"] = user.Username
	pageCtx.Params["avatar"] = user.AvatarURL
	pageCtx.Params["banner"] = user.BannerURL
	// 媒体模块没装配时不渲染上传控件：一个选了文件却传不上去的表单，
	// 比没有这个入口更让人费解（与页眉「注册」入口的三条判据同一条理由）。
	if m.media != nil {
		pageCtx.Params["uploads"] = "1"
	}
	m.renderer.Render(w, r, status, "account.html", pageCtx)
}

// chineseDate 按站点时区把时间格式化成中文日期。
//
// 与主题的 date "chinese" 布局一致（2006年1月2日）：注册时间该按站点时区显示，
// 而不是按服务器所在时区——一个跨时区部署的站，站长与访客看到的会是两个日期。
func (m *Module) chineseDate(ctx context.Context, t time.Time) string {
	loc := time.Local
	if m.settings != nil {
		var site settings.Site
		if err := m.settings.Get(ctx, settings.GroupSite, &site); err == nil {
			if parsed, err := time.LoadLocation(site.Timezone); err == nil {
				loc = parsed
			}
		}
	}
	return t.In(loc).Format("2006年1月2日")
}

// roleLabels 把用户的角色拼成一行标签。
//
// 优先用角色的显示名（「管理员」），没有时退回角色名。用「、」连接而不是中点：
// 中点是本主题明确拒绝的那类元信息写法。
func roleLabels(user *auth.User) string {
	labels := make([]string, 0, len(user.Roles))
	for i := range user.Roles {
		label := strings.TrimSpace(user.Roles[i].Label)
		if label == "" {
			label = user.Roles[i].Name
		}
		labels = append(labels, label)
	}
	return strings.Join(labels, "、")
}

// postAccountPassword 处理账户页的改密码提交。
func (m *Module) postAccountPassword(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || principal.User == nil {
		http.Redirect(w, r, PathLogin+"?next="+PathAccount, http.StatusFound)
		return
	}
	user := principal.User

	if err := r.ParseForm(); err != nil {
		m.renderAccountPasswordFailure(w, r, http.StatusBadRequest, errNotice("请求格式不正确"), user)
		return
	}
	if !m.csrf.Verify(r, r.PostFormValue(FormCSRFField)) {
		m.renderAccountPasswordFailure(w, r, http.StatusBadRequest, errNotice(noticeFormExpired), user)
		return
	}

	old := r.PostFormValue("oldPassword")
	next := r.PostFormValue("newPassword")
	if next != r.PostFormValue("newPasswordConfirm") {
		form := theme.NewFormState()
		form.Errors["newPasswordConfirm"] = errPasswordMismatch
		m.renderAccount(w, r, http.StatusBadRequest, form, user, errNotice(noticeRegisterField))
		return
	}

	if err := m.core.Service.ChangePassword(r.Context(), user.ID, old, next); err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidCredentials):
			form := theme.NewFormState()
			form.Errors["oldPassword"] = "原密码不正确"
			m.renderAccount(w, r, http.StatusUnauthorized, form, user, errNotice(noticeRegisterField))
		case strings.Contains(err.Error(), "密码"):
			form := theme.NewFormState()
			form.Errors["newPassword"] = err.Error()
			m.renderAccount(w, r, http.StatusBadRequest, form, user, errNotice(noticeRegisterField))
		default:
			m.logger.Error("修改密码失败", slog.Any("error", err))
			m.renderAccount(w, r, http.StatusInternalServerError, theme.NewFormState(), user, errNotice("修改失败，请稍后重试"))
		}
		return
	}

	// 改密码会清掉该用户的全部会话，当前这条也在内——浏览器手里的 Cookie 已经
	// 指向一条不存在的会话，清除它才能让下一次请求干净地按匿名处理。
	m.core.Sessions.ClearCookies(w)
	http.Redirect(w, r, PathLogin+"?changed=1", http.StatusFound)
}

// renderAccountPasswordFailure 是改密码失败的简写入口。
func (m *Module) renderAccountPasswordFailure(w http.ResponseWriter, r *http.Request, status int, n notice, user *auth.User) {
	m.renderAccount(w, r, status, theme.NewFormState(), user, n)
}

// ---------- 个人资料 ----------

/*
 * 个人中心能改什么，是这一版最需要说清的一件事。
 *
 * 能改：昵称、简介、头像、封面。四样都是**这个人在这个站上的公开形象**。
 * 不能改：用户名（改它等于换一个身份，而站内的 @ 提及与外链都指着它）、
 * 邮箱（它是账号凭证，改邮箱要走验证信，不是一张表单能办的事）、角色（前台管不着）。
 * 后两项在页面上以只读形式呈现，不做成灰掉的输入框——灰掉的输入框暗示「本来能改，只是现在不行」。
 */
const (
	// profileNameMax 与后台创建用户时的限制同源。按**字符数**算，不按字节：
	// 中文昵称按字节算会在第 10 个字上被判超长。
	profileNameMax = 32
	profileBioMax  = 200
	// maxProfileImage 是头像与封面的单文件上限，比附件默认上限严。
	// 头像最终显示在 32px 的方印里、封面也就一千来像素宽，几十兆的原图没有意义。
	maxProfileImage = 4 << 20
	// 上传要写文件、要生成缩略图，比改密码更重。给一个宽松但存在的上限。
	profilePerUserLimit = 20
	profileWindow       = time.Hour
)

// errNotProfileImage 表示传上来的不是一张能当头像用的位图。
var errNotProfileImage = errors.New("不是可用的图片格式")

// postAccountProfile 保存个人资料。
//
// 一个 multipart 表单管四件事（昵称、简介、头像、封面）。合成一张表而不是两张：
// 站长改资料时往往是「换个头像顺便改个昵称」，拆成两个入口就要提交两次、
// 而两次提交之间还会出现「头像换了但昵称没换」的中间状态。
//
// 表单里没出现的文件字段 = 不动那张图；勾了「删除」才清空。
// 「没选文件」与「要删掉」在浏览器里是两件不同的事，但都表现为没有文件，
// 所以删除必须由显式的一个复选框表达（见模板）。
func (m *Module) postAccountProfile(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || principal.User == nil {
		http.Redirect(w, r, PathLogin+"?next="+PathAccount, http.StatusFound)
		return
	}
	user := principal.User

	if !m.limiter.Allow("profile:"+strconv.FormatInt(user.ID, 10), profilePerUserLimit, profileWindow) {
		m.renderAccount(w, r, http.StatusTooManyRequests, theme.NewFormState(), user, errNotice(noticeTooFrequent))
		return
	}

	// 上限给到 maxProfileImage：更大的请求在解析阶段就被切断，
	// 免得把一个 200MB 的请求先落到临时文件再告诉用户「太大了」。
	if err := r.ParseMultipartForm(maxProfileImage); err != nil {
		m.renderAccount(w, r, http.StatusBadRequest, theme.NewFormState(), user, errNotice("请求格式不正确"))
		return
	}
	if !m.csrf.Verify(r, r.FormValue(FormCSRFField)) {
		m.renderAccount(w, r, http.StatusBadRequest, theme.NewFormState(), user, errNotice(noticeFormExpired))
		return
	}

	values := map[string]string{
		"displayName": strings.TrimSpace(r.FormValue("displayName")),
		"bio":         strings.TrimSpace(r.FormValue("bio")),
	}
	form := theme.NewFormState()
	form.Values = values

	if problem := validateProfile(values); problem != "" {
		m.renderAccount(w, r, http.StatusBadRequest, form, user, errNotice(problem))
		return
	}

	avatar, banner, err := m.profileImages(r, user.ID)
	if err != nil {
		m.logger.Warn("个人资料图片上传失败", slog.Int64("user", user.ID), slog.Any("error", err))
		// 原因挂在提示条上而不是字段错误里：这两张图的错误没有对应的输入框
		// （file 字段本身不会「填错」），落在没人渲染的 Errors 键上等于没提示。
		m.renderAccount(w, r, http.StatusBadRequest, form, user,
			errNotice("图片没能保存："+uploadReason(err)))
		return
	}

	if err := m.store.UpdateProfile(r.Context(), user.ID, values["displayName"], values["bio"]); err != nil {
		m.logger.Error("更新个人资料失败", slog.Any("error", err))
		m.renderAccount(w, r, http.StatusInternalServerError, form, user, errNotice("保存失败，请稍后重试"))
		return
	}
	if avatar.set {
		if err := m.store.UpdateAvatar(r.Context(), user.ID, avatar.url); err != nil {
			m.logger.Error("更新头像失败", slog.Any("error", err))
			m.renderAccount(w, r, http.StatusInternalServerError, form, user, errNotice("保存失败，请稍后重试"))
			return
		}
	}
	if banner.set {
		if err := m.store.UpdateBanner(r.Context(), user.ID, banner.url); err != nil {
			m.logger.Error("更新封面失败", slog.Any("error", err))
			m.renderAccount(w, r, http.StatusInternalServerError, form, user, errNotice("保存失败，请稍后重试"))
			return
		}
	}

	/* 保存成功重定向（PRG）。

	   与改密码不同，这里**不换发会话、也不清 Cookie**：昵称与头像不是凭证。
	   重定向之后页面重新从库里读一遍，用户看到的就是刚落库的样子——
	   原地渲染的成功态反而要自己拼一遍「保存后长什么样」，那是两份会走样的真相。 */
	http.Redirect(w, r, PathAccount+"?saved=1", http.StatusFound)
}

// imageChange 描述一张图这次要不要动、动成什么。
type imageChange struct {
	// set 为真才写库：没选文件又没勾删除时，这次提交与那张图无关。
	set bool
	url string
}

// profileImages 处理表单里的头像与封面。
//
// 顺序上先传文件再写库：文件传不上去就不该留下一个指向空地址的 URL。
// 旧图不删——它还在附件库里，删掉会让「我的附件」里凭空少一条，
// 而用户完全可能只是换回上一张。
func (m *Module) profileImages(r *http.Request, userID int64) (avatar, banner imageChange, err error) {
	avatar, err = m.profileImage(r, userID, "avatar", "removeAvatar")
	if err != nil {
		return avatar, banner, err
	}
	banner, err = m.profileImage(r, userID, "banner", "removeBanner")
	return avatar, banner, err
}

// profileImageExts 是头像与封面允许的扩展名。
//
// 比附件那张白名单**窄**：那边收文档、音频、压缩包，这里要的只是一张能当头像显示的位图。
// 尤其是 SVG——它是 XML 文档，媒体模块自己就把它排除在图片处理之外；拿它当头像，
// 等于把一个可执行文档挂到每个页面的页眉上。
//
// 扩展名只挡第一道；内容对不对由媒体模块嗅探（扩展名与嗅探结果必须彼此印证）。
var profileImageExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true,
	".webp": true, ".bmp": true, ".ico": true,
}

// profileImage 处理其中一张图。
func (m *Module) profileImage(r *http.Request, userID int64, field, removeField string) (imageChange, error) {
	if r.FormValue(removeField) != "" {
		return imageChange{set: true, url: ""}, nil
	}

	file, header, err := r.FormFile(field)
	if errors.Is(err, http.ErrMissingFile) {
		// 没选文件（input 是空的）：这次提交不动这张图。
		return imageChange{}, nil
	}
	if err != nil {
		return imageChange{}, err
	}
	defer func() { _ = file.Close() }()

	if header.Size > maxProfileImage {
		return imageChange{}, fmt.Errorf("%w：超过 %d MB", media.ErrTooLarge, maxProfileImage>>20)
	}
	if ext := strings.ToLower(filepath.Ext(header.Filename)); !profileImageExts[ext] {
		return imageChange{}, fmt.Errorf("%w: %s", errNotProfileImage, ext)
	}
	// 媒体模块没装配时前台不提供上传入口（见 renderAccount 的 Params），
	// 真到了这里说明配置不对，给一句人话而不是一个 nil 解引用。
	if m.media == nil {
		return imageChange{}, errors.New("media 模块未装配")
	}

	item, err := m.media.Upload(r.Context(), &media.UploadParams{
		OriginalName: header.Filename,
		Size:         header.Size,
		Content:      file,
		UploaderID:   userID,
		Title:        "个人资料图片",
		Alt:          "用户上传的图片",
	})
	if err != nil {
		return imageChange{}, err
	}
	return imageChange{set: true, url: item.URL}, nil
}

// validateProfile 校验昵称与简介，返回一句给用户看的话（空串表示通过）。
func validateProfile(values map[string]string) string {
	name := []rune(values["displayName"])
	if len(name) == 0 {
		return "昵称不能为空"
	}
	if len(name) > profileNameMax {
		return fmt.Sprintf("昵称最多 %d 个字", profileNameMax)
	}
	if len([]rune(values["bio"])) > profileBioMax {
		return fmt.Sprintf("简介最多 %d 个字", profileBioMax)
	}
	return ""
}

// uploadReason 把上传失败的原因翻成一句能读的话。
//
// 只区分用户能自己解决的两类（太大、格式不支持），其余归到「请稍后重试」：
// 把底层错误原样吐给用户，既帮不上忙，也把服务端的实现细节说了出去。
func uploadReason(err error) string {
	if errors.Is(err, media.ErrTooLarge) {
		return fmt.Sprintf("图片不能超过 %d MB", maxProfileImage>>20)
	}
	if errors.Is(err, errNotProfileImage) {
		return "只支持 JPEG / PNG / GIF / WebP / BMP / ICO 这几种图片"
	}
	// 类型不支持是个结构体错误（带着扩展名），只能 errors.As。
	var unsupported *media.ErrUnsupportedType
	if errors.As(err, &unsupported) {
		return "只支持 JPEG / PNG / GIF / WebP / BMP / ICO 这几种图片"
	}
	return "请稍后重试"
}

// ---------- 登出 ----------

// postLogout 处理登出。
//
// 是 POST 而不是 GET 链接：登出是状态变更，GET 会被浏览器的预取器、
// 邮件客户端的安全扫描器当普通链接抓一遍——用户会莫名其妙地被登出。
func (m *Module) postLogout(w http.ResponseWriter, r *http.Request) {
	principal, authenticated := auth.FromContext(r.Context())

	if err := r.ParseForm(); err != nil {
		m.renderLogoutFailure(w, r, principal, errNotice("请求格式不正确"))
		return
	}
	if !m.csrf.Verify(r, r.PostFormValue(FormCSRFField)) {
		m.renderLogoutFailure(w, r, principal, errNotice(noticeFormExpired))
		return
	}

	if authenticated {
		if err := m.core.Service.LogoutSession(r.Context(), principal.Session); err != nil {
			m.logger.Warn("销毁会话失败", slog.Any("error", err))
		}
	}
	// 无论有没有会话都清一遍 Cookie：令牌调用（PAT）不写 Cookie，
	// 但同一个浏览器里可能残留着一条已失效的会话 Cookie。
	m.core.Sessions.ClearCookies(w)
	http.Redirect(w, r, "/", http.StatusFound)
}

// renderLogoutFailure 渲染登出失败：已登录时回到账户页，匿名时回登录页。
func (m *Module) renderLogoutFailure(w http.ResponseWriter, r *http.Request, principal *auth.Principal, n notice) {
	if principal == nil || principal.User == nil {
		m.renderLoginFailure(w, r, http.StatusBadRequest, n)
		return
	}
	m.renderAccount(w, r, http.StatusBadRequest, theme.NewFormState(), principal.User, n)
}

// clientIP 返回客户端地址，取不到时用占位串。
//
// 限流键不能为空串：所有取不到 IP 的请求会共用同一个计数器，
// 那会让一台代理后的站点在几个人同时注册后整体被锁。
func clientIP(r *http.Request) string {
	if ip := httpx.ClientIPFrom(r); ip != "" {
		return ip
	}
	return "unknown"
}
