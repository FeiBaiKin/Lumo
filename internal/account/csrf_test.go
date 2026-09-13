package account

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCSRFIssueWritesCookieAndReturnsToken 验证令牌同时进 Cookie 与表单。
//
// 双提交的两半必须一致：Cookie 是服务端记的，隐藏字段是页面给的，
// 提交时两者对得上才说明这个请求确实来自我们自己渲染的那个页面。
func TestCSRFIssueWritesCookieAndReturnsToken(t *testing.T) {
	t.Parallel()

	csrf := NewCSRF(false)
	rec := httptest.NewRecorder()
	token := csrf.Issue(rec)

	if token == "" {
		t.Fatal("Issue 应返回令牌明文")
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("应下发一枚 Cookie，实际 %d", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Value != token {
		t.Error("Cookie 值与返回的令牌应一致")
	}
	if !cookie.HttpOnly {
		t.Error("表单 CSRF Cookie 应为 HttpOnly：表单里已有明文，前端 JS 不需要读它")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Error("Cookie 应为 SameSite=Lax")
	}
	if cookie.Secure {
		t.Error("非 HTTPS 模式下不该设 Secure，否则本地开发时 Cookie 存不下来")
	}
	if cookie.MaxAge != formCSRFTTL {
		t.Errorf("MaxAge = %d，期望 %d", cookie.MaxAge, formCSRFTTL)
	}
}

// TestCSRFNameFollowsSecureMode 验证 __Host- 前缀只在 HTTPS 下启用。
//
// __Host- 能挡住子域写入伪造 Cookie，但它同时要求 Secure + Path=/ 且不带 Domain，
// 在本地 HTTP 下浏览器会直接丢弃这枚 Cookie——那会让本地开发时登录表单永远提交不了。
func TestCSRFNameFollowsSecureMode(t *testing.T) {
	t.Parallel()

	if got := NewCSRF(false).CookieName(); got != formCSRFCookieName {
		t.Errorf("非安全模式 Cookie 名 = %q，期望 %q", got, formCSRFCookieName)
	}
	if got := NewCSRF(true).CookieName(); got != formCSRFCookieNameSecure {
		t.Errorf("安全模式 Cookie 名 = %q，期望 %q", got, formCSRFCookieNameSecure)
	}
}

// TestCSRFVerify 验证校验的四种情形。
func TestCSRFVerify(t *testing.T) {
	t.Parallel()

	csrf := NewCSRF(false)
	request := func(cookieValue string, addCookie bool) *http.Request {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/login", http.NoBody)
		if addCookie {
			req.AddCookie(&http.Cookie{Name: csrf.CookieName(), Value: cookieValue})
		}
		return req
	}

	cases := []struct {
		name       string
		cookie     string
		addCookie  bool
		formValue  string
		wantVerify bool
	}{
		{"一致", "tok", true, "tok", true},
		{"不一致", "tok", true, "other", false},
		{"缺 Cookie", "", false, "tok", false},
		{"缺表单字段", "tok", true, "", false},
		{"两边都空", "", true, "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := csrf.Verify(request(tc.cookie, tc.addCookie), tc.formValue); got != tc.wantVerify {
				t.Errorf("Verify = %v，期望 %v", got, tc.wantVerify)
			}
		})
	}
}
