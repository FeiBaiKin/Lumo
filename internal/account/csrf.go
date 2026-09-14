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
	c.set(w, token)
	return token
}

// Ensure 返回本次请求可用的令牌：请求已带着一枚合法的就沿用，否则签发新的。
//
// 与 Issue 的差别只在「换不换值」，而这一条是为页眉里的退出登录表单加的：
// 那张表单出现在**每一个**前台页面上，若每次渲染都换发新令牌，
// 用户在另一个标签页里开着的注册表单会在切回去提交时被自己的 CSRF 挡下——
// 那张表还好好地摆在眼前，提交却说「表单已过期」。
//
// 沿用旧值不削弱双提交：它赌的是攻击者读不到也写不了这个 Cookie，与令牌换不换无关。
// 每次仍重设一遍 Cookie 是为了续期——人停在一个页面上超过 formCSRFTTL 之后，
// 页面里那枚令牌还在，Cookie 却已经过期，退出登录会莫名其妙地失败一次。
func (c *CSRF) Ensure(w http.ResponseWriter, r *http.Request) string {
	cookie, err := r.Cookie(c.CookieName())
	if err != nil || !validToken(cookie.Value) {
		return c.Issue(w)
	}
	c.set(w, cookie.Value)
	return cookie.Value
}

// set 写入令牌 Cookie。
//
// HttpOnly：表单里已经有明文，前端 JS 不需要读它（与会话 CSRF 的 Cookie 相反，
// 那个必须能被 JS 读到才能放进请求头）。
func (c *CSRF) set(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     c.CookieName(),
		Value:    token,
		Path:     "/",
		MaxAge:   formCSRFTTL,
		HttpOnly: true,
		Secure:   c.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// validToken 报告一个值是否长得像本模块签发的令牌（32 字节的 base64url）。
//
// Ensure 会把 Cookie 里的值原样渲染进表单，故先过一道形状检查：
// 能写 Cookie 的攻击者本就绕得过双提交（见 CSRF 的已知弱点），
// 这道检查挡的不是他，而是别处写坏的 Cookie 被当成令牌一路带进 HTML。
func validToken(value string) bool {
	// 32 字节随机数按 base64url 无填充编码后的长度。
	const want = 43
	if len(value) != want {
		return false
	}
	for i := range len(value) {
		ch := value[i]
		switch {
		case ch >= 'A' && ch <= 'Z', ch >= 'a' && ch <= 'z', ch >= '0' && ch <= '9', ch == '-', ch == '_':
		default:
			return false
		}
	}
	return true
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
