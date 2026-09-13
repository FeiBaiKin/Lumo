package account

import (
	"net/http"

	"github.com/FeiBaiKin/lumo/internal/auth"
)

// 表单 CSRF 的 Cookie 与字段名。
//
// __Host- 前缀要求 Secure + Path=/ 且不带 Domain，能防止子域写入伪造 Cookie；
// 但它同时要求 HTTPS，故仅在 secure 模式下启用（与 auth 的会话 Cookie 同一处置）。
const (
	formCSRFCookieName       = "lumo_form_csrf"
	formCSRFCookieNameSecure = "__Host-lumo_form_csrf"
	// FormCSRFField 是渲染进表单的 hidden 字段名。
	//
	// 带下划线前缀是从 Rails / Django 沿用的惯例，读取表单的中间件与扫描工具都认它。
	FormCSRFField = "_csrf"
)

// formCSRFTTL 是表单令牌的有效期。
//
// 30 分钟：够填完一张注册表，又短到来得及在用户离开电脑后失效。
// 过期不是死路——校验失败时会下发一枚新令牌并提示重新提交。
const formCSRFTTL = 30 * 60

// CSRF 提供前台表单的 CSRF 防护（双提交 Cookie + 隐藏字段）。
//
// 为什么不复用会话 CSRF（auth.Authenticator.CSRF）：那套是「请求头里的令牌必须与
// 会话记录里的一致」，而**匿名访客手里根本没有会话**——登录表单、注册表单、
// 找回密码表单全都发生在这之前。会话 CSRF 在这些页面上无从校验。
//
// 已登录用户的账户页表单也走这一套，不与会话 CSRF 混用：
// 两套机制并存会让模板作者需要判断「这个表单该带哪个令牌」，而判断错了就是
// 一个静默失效的表单。
//
// 已知弱点：双提交无法防住能写 Cookie 的攻击者（同站子域被拿下、
// 或有中间人能力）。生产环境的两条缓解是 __Host- 前缀（禁掉子域写入）
// 与 SameSite=Lax（跨站 POST 不带 Cookie），与 session.go 里的说明一致。
type CSRF struct {
	secure bool
}

// NewCSRF 构造表单 CSRF 管理器；secure 决定是否启用 __Host- 前缀与 Secure 属性。
func NewCSRF(secure bool) *CSRF { return &CSRF{secure: secure} }

// CookieName 返回当前模式下的表单令牌 Cookie 名。
func (c *CSRF) CookieName() string {
	if c.secure {
		return formCSRFCookieNameSecure
	}
	return formCSRFCookieName
}

// Issue 生成一枚新令牌，写进 Cookie 并返回明文（调用方填进表单的隐藏字段）。
//
// 出错时返回空串：调用方会渲染出一张没有令牌的表单，提交时被自己的 CSRF 校验挡下。
// 这是 fail-closed 的方向——宁可让这一次提交失败，也不要放行一个未经校验的请求。
func (c *CSRF) Issue(w http.ResponseWriter) string {
	token, err := randomToken()
	if err != nil {
		return ""
	}
	// HttpOnly：表单里已经有明文，前端 JS 不需要读它（与会话 CSRF 的 Cookie 相反，
	// 那个必须能被 JS 读到才能放进请求头）。
	http.SetCookie(w, &http.Cookie{
		Name:     c.CookieName(),
		Value:    token,
		Path:     "/",
		MaxAge:   formCSRFTTL,
		HttpOnly: true,
		Secure:   c.secure,
		SameSite: http.SameSiteLaxMode,
	})
	return token
}

// Verify 比对 Cookie 值与表单字段值。
//
// 用定时安全比较：普通字符串比较会在第一个不同的字节处提前返回，
// 理论上可按响应时间逐字节还原令牌。
func (c *CSRF) Verify(r *http.Request, formValue string) bool {
	cookie, err := r.Cookie(c.CookieName())
	if err != nil || cookie.Value == "" || formValue == "" {
		return false
	}
	return auth.ConstantTimeEqual(cookie.Value, formValue)
}
