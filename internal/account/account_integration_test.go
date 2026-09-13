package account

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/auth"
	"github.com/FeiBaiKin/lumo/internal/auth/perm"
	"github.com/FeiBaiKin/lumo/internal/config"
	"github.com/FeiBaiKin/lumo/internal/mail"
	"github.com/FeiBaiKin/lumo/internal/migrate"
	"github.com/FeiBaiKin/lumo/internal/settings"
	"github.com/FeiBaiKin/lumo/internal/testsupport"
	"github.com/FeiBaiKin/lumo/internal/theme"
)

// 集成测试直连本机 PG 的独立测试库（agent.md §12），用独占 schema 与其他包隔离。
const testSchema = "lumo_it_account"

// 测试用的口令与邮箱。
const (
	testPassword  = "test-password-123"
	otherPassword = "another-password-456"
	testSiteURL   = "https://example.com"
)

// newAccountStack 装配一个带前台账户路由的整机测试栈。
//
// 挂载顺序与 serve.go 完全一致：先 account 再 theme —— 后者注册的 /{slug}
// 会吞掉根路径下的一切单段路径，顺序反了 /login 就成了一个名为 login 的独立页面。
func newAccountStack(t *testing.T) (*testsupport.Stack, *Module) {
	t.Helper()

	db := testsupport.Open(t, testsupport.Options{
		Schema:  testSchema,
		Migrate: true,
		Sources: []migrate.Source{
			{Name: settings.Name, FS: settings.New().Migrations()},
			{Name: theme.Name, FS: theme.New().Migrations()},
			{Name: Name, FS: New().Migrations()},
		},
	})

	cfg := config.Default()
	cfg.DataDir = t.TempDir()

	var mod *Module
	stack := testsupport.NewStackWith(t, db, &testsupport.StackOptions{
		Config:  cfg,
		Modules: []app.Module{settings.New(), mail.New(), theme.New(), New()},
		AfterStart: func(root chi.Router, application *app.App) {
			mod = From(application)
			if mod == nil {
				t.Fatal("账户模块未装配")
			}
			value, ok := application.Lookup(auth.CoreKey)
			if !ok {
				t.Fatal("认证栈未登记为共享服务；account 需要它来复用同一套会话与限流")
			}
			core, ok := value.(*auth.Core)
			if !ok {
				t.Fatalf("auth.CoreKey 登记的不是 *auth.Core，而是 %T", value)
			}
			mod.MountFrontend(root, core.Authenticator.Optional)
			if themes := theme.From(application); themes != nil {
				themes.MountFrontend(root, core.Authenticator.Optional)
			}
		},
	})
	return stack, mod
}

// configure 更新一个设置分组。
func configure(t *testing.T, stack *testsupport.Stack, group string, values map[string]any) {
	t.Helper()
	svc := settings.From(stack.App)
	if svc == nil {
		t.Fatal("设置服务未装配")
	}
	if _, err := svc.Update(context.Background(), group, values); err != nil {
		t.Fatalf("更新设置分组 %s 失败: %v", group, err)
	}
}

// enableRegistration 把站点配成「可以注册」：填对外地址、开 SMTP、开注册开关。
//
// 三样缺一不可，这正是 registrationOpen 的三个条件——注册要发验证邮件，
// 而验证邮件里的链接必须是能点开的绝对地址。
func enableRegistration(t *testing.T, stack *testsupport.Stack) {
	t.Helper()
	configure(t, stack, settings.GroupSite, map[string]any{"url": testSiteURL})
	configure(t, stack, mail.GroupMail, map[string]any{
		"enabled": true, "host": "127.0.0.1", "port": 587,
		"username": "", "encryption": "starttls",
		"fromAddress": "noreply@example.com", "fromName": "Lumo",
	})
	configure(t, stack, GroupAccount, map[string]any{
		"allowRegistration": true, "registrationNotice": "请先读一遍站规。",
	})
}

// get 发起一次 GET，返回记录器、**合并后的 Cookie 集合**与表单 CSRF 令牌。
//
// 返回的 Cookie 集合是要带去下一次请求的那一份：请求时带的（例如已登录页面的会话）
// 与响应新下发的（表单 CSRF）合并，同名以响应为准——浏览器就是这么做的，
// 不合并的话「带会话打开账户页再改密码」这条链路在测试里会缺一半 Cookie 而失败。
//
// 表单 CSRF Cookie 是 HttpOnly，测试只能从响应头里读它，正如表单字段里那份一样。
func get(t *testing.T, stack *testsupport.Stack, path string, cookies []*http.Cookie) (*httptest.ResponseRecorder, []*http.Cookie, string) {
	t.Helper()
	rec := stack.Do(t, &testsupport.Request{Method: http.MethodGet, Path: path, Cookies: cookies})
	return rec, mergeCookies(cookies, rec), cookieValue(mergeCookies(cookies, rec), formCSRFCookieName)
}

// mergeCookies 把请求时带的 Cookie 与响应新下发的合并，同名以响应为准。
func mergeCookies(sent []*http.Cookie, rec *httptest.ResponseRecorder) []*http.Cookie {
	issued := rec.Result().Cookies()
	if len(issued) == 0 {
		return sent
	}
	latest := make(map[string]string, len(issued))
	for _, c := range issued {
		latest[c.Name] = c.Value
	}

	out := make([]*http.Cookie, 0, len(sent)+len(issued))
	seen := make(map[string]bool, len(latest))
	for _, c := range sent {
		if value, ok := latest[c.Name]; ok {
			out = append(out, &http.Cookie{Name: c.Name, Value: value})
			seen[c.Name] = true
			continue
		}
		out = append(out, c)
	}
	for _, c := range issued {
		if !seen[c.Name] {
			out = append(out, c)
		}
	}
	return out
}

// postForm 以原生表单的编码方式提交。
func postForm(t *testing.T, stack *testsupport.Stack, path string, cookies []*http.Cookie, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	return stack.Do(t, &testsupport.Request{
		Method:      http.MethodPost,
		Path:        path,
		Body:        form.Encode(),
		ContentType: "application/x-www-form-urlencoded",
		Cookies:     cookies,
	})
}

// cookieValue 从 Cookie 列表里按名字取值。
func cookieValue(cookies []*http.Cookie, name string) string {
	for _, c := range cookies {
		if c.Name == name {
			return c.Value
		}
	}
	return ""
}

// ---------- 注册开关 ----------

// TestRegisterClosedReturns404 验证关着注册时 /register 对外等于不存在。
func TestRegisterClosedReturns404(t *testing.T) {
	stack, _ := newAccountStack(t)

	rec, _, _ := get(t, stack, PathRegister, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("状态码 = %d，期望 404（注册未开放时这个路径不该存在）", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), noticeRegisterOff) {
		t.Errorf("页面应说明注册暂未开放: %.300s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `action="/register"`) {
		t.Error("未开放时不该渲染出可提交的注册表单")
	}
}

// TestRegisterRequiresMailAndSiteURL 验证邮件与对外地址缺一不可。
//
// 少了任何一样，注册出来的账号都永远验证不了、也永远登录不了——
// 与其放行一个必然死掉的流程，不如让这个路径不存在。
func TestRegisterRequiresMailAndSiteURL(t *testing.T) {
	stack, _ := newAccountStack(t)
	configure(t, stack, settings.GroupSite, map[string]any{"url": testSiteURL})
	configure(t, stack, GroupAccount, map[string]any{"allowRegistration": true})

	rec, _, _ := get(t, stack, PathRegister, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("邮件未配置时状态码 = %d，期望 404", rec.Code)
	}
}

// ---------- 全流程 ----------

// TestRegistrationToLoginFlow 走一遍注册 → 未验证被拒 → 验证 → 登录 → 账户页。
func TestRegistrationToLoginFlow(t *testing.T) {
	stack, mod := newAccountStack(t)
	enableRegistration(t, stack)
	ctx := context.Background()

	// GET /register：应拿到可提交的表单与一枚表单 CSRF Cookie。
	rec, cookies, csrf := get(t, stack, PathRegister, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("注册页状态码 = %d：%s", rec.Code, rec.Body.String())
	}
	if csrf == "" {
		t.Fatal("注册页应下发表单 CSRF Cookie")
	}
	if !strings.Contains(rec.Body.String(), "请先读一遍站规。") {
		t.Error("注册页应显示站长写的说明")
	}

	// POST /register
	email := "newbie@example.com"
	rec = postForm(t, stack, PathRegister, cookies, url.Values{
		"username":        {"newbie"},
		"email":           {email},
		"password":        {testPassword},
		"passwordConfirm": {testPassword},
		FormCSRFField:     {csrf},
	})
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != PathLogin+"?registered=1" {
		t.Fatalf("注册状态码 = %d，Location = %q：%s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}

	user, err := mod.Store().FindByEmail(ctx, email)
	if err != nil {
		t.Fatalf("注册后应能查到用户: %v", err)
	}
	if user.Verified() {
		t.Error("自助注册的账号在点开验证链接之前不该是已验证状态")
	}

	// 未验证就登录：必须被拒，且提示里给出「重新发送验证邮件」这条路。
	loginRec, loginCookies, loginCSRF := get(t, stack, PathLogin, nil)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("登录页状态码 = %d", loginRec.Code)
	}
	rec = postForm(t, stack, PathLogin, loginCookies, url.Values{
		"login": {email}, "password": {testPassword}, FormCSRFField: {loginCSRF},
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("未验证账号登录状态码 = %d，期望 403", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "重新发送验证邮件") {
		t.Error("被拒页面应告诉用户怎么重新拿到验证邮件")
	}

	// 走一遍验证链接。令牌明文只在那封邮件里，这里直接签一枚——
	// 邮件正文的组装由 mail.go 的单元测试覆盖，本用例要验的是端点行为。
	token, err := mod.Tokens().Issue(ctx, user.ID, PurposeVerifyEmail, VerifyEmailTTL)
	if err != nil {
		t.Fatalf("签发验证令牌失败: %v", err)
	}
	rec, _, _ = get(t, stack, PathVerifyEmail+"?token="+url.QueryEscape(token), nil)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != PathLogin+"?verified=1" {
		t.Fatalf("验证状态码 = %d，Location = %q", rec.Code, rec.Header().Get("Location"))
	}

	// 验证之后登录成功。
	_, loginCookies, loginCSRF = get(t, stack, PathLogin, nil)
	rec = postForm(t, stack, PathLogin, loginCookies, url.Values{
		"login": {email}, "password": {testPassword}, FormCSRFField: {loginCSRF},
	})
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/" {
		t.Fatalf("登录状态码 = %d，Location = %q：%s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	session := rec.Result().Cookies()
	if cookieValue(session, "lumo_session") == "" {
		t.Fatalf("登录成功应下发会话 Cookie，实际 %v", session)
	}

	// 带着会话打开账户页。
	rec, _, _ = get(t, stack, PathAccount, session)
	if rec.Code != http.StatusOK {
		t.Fatalf("账户页状态码 = %d：%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, email) {
		t.Error("账户页应显示注册邮箱")
	}
	if !strings.Contains(body, "会员") {
		t.Error("账户页应显示角色（member 的显示名是「会员」）")
	}
	if strings.Contains(body, "进入后台") {
		t.Error("零权限账号不该看到「进入后台」入口——点进去只会被拦")
	}
}

// TestAccountRequiresLogin 验证未登录访问账户页会被送去登录页并带回跳地址。
func TestAccountRequiresLogin(t *testing.T) {
	stack, _ := newAccountStack(t)

	rec, _, _ := get(t, stack, PathAccount, nil)
	if rec.Code != http.StatusFound {
		t.Fatalf("状态码 = %d，期望 302", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != PathLogin+"?next="+PathAccount {
		t.Errorf("Location = %q，期望带回跳地址", got)
	}
}

// TestLoginRejectsOffsiteNext 验证开放重定向的唯一防线。
//
// 把 ?next=https://evil.example 原样跳过去，攻击者就能拿本站域名做钓鱼跳板：
// 用户看到的是本站地址，落地的却是别处。
func TestLoginRejectsOffsiteNext(t *testing.T) {
	stack, _ := newAccountStack(t)

	for _, next := range []string{
		"https://evil.example/phish",
		"//evil.example/phish",
		"/\\evil.example",
	} {
		rec, _, _ := get(t, stack, PathLogin+"?next="+url.QueryEscape(next), nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("登录页状态码 = %d", rec.Code)
		}
		if strings.Contains(rec.Body.String(), "evil.example") {
			t.Errorf("站外地址 %q 不该被带进表单", next)
		}
	}

	// 站内路径照常保留。
	rec, _, _ := get(t, stack, PathLogin+"?next="+url.QueryEscape("/account"), nil)
	if !strings.Contains(rec.Body.String(), `name="next" value="/account"`) {
		t.Errorf("站内路径应保留: %.400s", rec.Body.String())
	}
}

// TestReRegisterUnverifiedReplacesPassword 验证「同一邮箱再注册一次」的安全性。
//
// 这条路径必须存在：否则填错邮箱的人再也收不到验证邮件，也永远登录不了。
// 但它**必须连密码一起换**——只重发不换密码的话，先注册的人（可能是拿别人邮箱
// 注册的攻击者）就坐等信箱主人点开链接把账号验证掉，然后用自己设的密码登进去。
func TestReRegisterUnverifiedReplacesPassword(t *testing.T) {
	stack, mod := newAccountStack(t)
	enableRegistration(t, stack)
	ctx := context.Background()
	email := "repeat@example.com"

	register := func(t *testing.T, password string) {
		t.Helper()
		_, cookies, csrf := get(t, stack, PathRegister, nil)
		rec := postForm(t, stack, PathRegister, cookies, url.Values{
			"username":        {"repeat"},
			"email":           {email},
			"password":        {password},
			"passwordConfirm": {password},
			FormCSRFField:     {csrf},
		})
		if rec.Code != http.StatusFound {
			t.Fatalf("注册状态码 = %d：%s", rec.Code, rec.Body.String())
		}
	}

	register(t, testPassword)
	register(t, otherPassword)

	user, err := mod.Store().FindByEmail(ctx, email)
	if err != nil {
		t.Fatalf("应能查到用户: %v", err)
	}
	if _, err := stack.DB.ExecContext(ctx,
		"UPDATE users SET email_verified_at = now() WHERE id = ?", user.ID); err != nil {
		t.Fatalf("标记已验证失败: %v", err)
	}

	// 最后一次提交的口令才是有效的；第一次的那枚必须已经作废。
	login := func(t *testing.T, password string) int {
		t.Helper()
		_, cookies, csrf := get(t, stack, PathLogin, nil)
		rec := postForm(t, stack, PathLogin, cookies, url.Values{
			"login": {email}, "password": {password}, FormCSRFField: {csrf},
		})
		return rec.Code
	}
	if code := login(t, otherPassword); code != http.StatusFound {
		t.Errorf("最后注册时设的口令应能登录，状态码 = %d", code)
	}
	if code := login(t, testPassword); code != http.StatusUnauthorized {
		t.Errorf("被覆盖掉的口令不该还能登录，状态码 = %d", code)
	}
}

// TestRegisterHoneypotIsSilentlyDropped 验证蜜罐字段被填时不创建任何用户。
//
// 假装成功而不是报错：告诉机器人「你被识别了」只会让它换个填法。
func TestRegisterHoneypotIsSilentlyDropped(t *testing.T) {
	stack, mod := newAccountStack(t)
	enableRegistration(t, stack)
	ctx := context.Background()

	_, cookies, csrf := get(t, stack, PathRegister, nil)
	rec := postForm(t, stack, PathRegister, cookies, url.Values{
		"username":        {"robot"},
		"email":           {"robot@example.com"},
		"password":        {testPassword},
		"passwordConfirm": {testPassword},
		"website2":        {"http://spam.example"},
		FormCSRFField:     {csrf},
	})
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != PathLogin+"?registered=1" {
		t.Fatalf("蜜罐被填时应假装成功，实际 %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if _, err := mod.Store().FindByEmail(ctx, "robot@example.com"); err == nil {
		t.Error("蜜罐被填时不该创建用户")
	}
}

// TestRegisterRejectsDuplicateUsername 验证用户名冲突被合并成一句提示。
func TestRegisterRejectsDuplicateUsername(t *testing.T) {
	stack, _ := newAccountStack(t)
	enableRegistration(t, stack)

	if _, err := stack.Users.CreateUser(context.Background(), &auth.CreateUserParams{
		Username: "taken", Email: "taken@example.com",
		Password: testPassword, Roles: []string{perm.RoleMember}, EmailVerified: true,
	}); err != nil {
		t.Fatalf("创建用户失败: %v", err)
	}

	_, cookies, csrf := get(t, stack, PathRegister, nil)
	rec := postForm(t, stack, PathRegister, cookies, url.Values{
		"username":        {"taken"},
		"email":           {"someone-else@example.com"},
		"password":        {testPassword},
		"passwordConfirm": {testPassword},
		FormCSRFField:     {csrf},
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("状态码 = %d，期望 409", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), noticeAccountTaken) {
		t.Error("提示应说明用户名或邮箱已被占用")
	}
}

// TestRegisterRejectsMismatchedPassword 验证两次输入不一致时原地重渲染并逐字段报错。
func TestRegisterRejectsMismatchedPassword(t *testing.T) {
	stack, _ := newAccountStack(t)
	enableRegistration(t, stack)

	_, cookies, csrf := get(t, stack, PathRegister, nil)
	rec := postForm(t, stack, PathRegister, cookies, url.Values{
		"username":        {"mismatch"},
		"email":           {"mismatch@example.com"},
		"password":        {testPassword},
		"passwordConfirm": {otherPassword},
		FormCSRFField:     {csrf},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("状态码 = %d，期望 400", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "两次输入的密码不一致") {
		t.Error("应逐字段提示两次输入不一致")
	}
	// 失败时回填刚填的值，用户不必重输。
	if !strings.Contains(body, `value="mismatch"`) {
		t.Error("失败时应回填用户名")
	}
	// 密码永不回填。
	if strings.Contains(body, testPassword) || strings.Contains(body, otherPassword) {
		t.Error("密码字段绝不能回填进 HTML")
	}
}

// ---------- 找回密码 ----------

// TestForgotPasswordDoesNotRevealExistence 验证这个端点不是账号枚举接口。
func TestForgotPasswordDoesNotRevealExistence(t *testing.T) {
	stack, _ := newAccountStack(t)
	enableRegistration(t, stack)

	if _, err := stack.Users.CreateUser(context.Background(), &auth.CreateUserParams{
		Username: "known", Email: "known@example.com",
		Password: testPassword, Roles: []string{perm.RoleMember}, EmailVerified: true,
	}); err != nil {
		t.Fatalf("创建用户失败: %v", err)
	}

	submit := func(t *testing.T, email string) (int, string, string) {
		t.Helper()
		_, cookies, csrf := get(t, stack, PathForgotPassword, nil)
		rec := postForm(t, stack, PathForgotPassword, cookies, url.Values{
			"email": {email}, FormCSRFField: {csrf},
		})
		return rec.Code, rec.Header().Get("Location"), rec.Body.String()
	}

	existCode, existLoc, existBody := submit(t, "known@example.com")
	missCode, missLoc, missBody := submit(t, "nobody@example.com")

	if existCode != missCode || existLoc != missLoc || existBody != missBody {
		t.Fatalf("存在与不存在的邮箱必须返回逐字节相同的响应：\n存在 %d %q\n不存在 %d %q",
			existCode, existLoc, missCode, missLoc)
	}
	if existLoc != PathForgotPassword+"?sent=1" {
		t.Fatalf("Location = %q", existLoc)
	}
}

// TestResetPasswordConsumesTokenOnce 验证重置令牌只能消费一次。
func TestResetPasswordConsumesTokenOnce(t *testing.T) {
	stack, mod := newAccountStack(t)
	ctx := context.Background()

	user, err := stack.Users.CreateUser(ctx, &auth.CreateUserParams{
		Username: "forgetful", Email: "forgetful@example.com",
		Password: testPassword, Roles: []string{perm.RoleMember}, EmailVerified: true,
	})
	if err != nil {
		t.Fatalf("创建用户失败: %v", err)
	}
	token, err := mod.Tokens().Issue(ctx, user.ID, PurposeResetPassword, ResetPasswordTTL)
	if err != nil {
		t.Fatalf("签发重置令牌失败: %v", err)
	}

	// GET：只查验不消费，令牌仍要能被带回表单。
	rec, jar, csrf := get(t, stack, PathResetPassword+"?token="+url.QueryEscape(token), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("重置页状态码 = %d：%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `name="token"`) {
		t.Error("重置页应把令牌放进隐藏字段")
	}

	reset := func(t *testing.T, password string) *httptest.ResponseRecorder {
		t.Helper()
		return postForm(t, stack, PathResetPassword, jar, url.Values{
			"token": {token}, "newPassword": {password},
			"newPasswordConfirm": {password}, FormCSRFField: {csrf},
		})
	}

	// 两次输入不一致时不该把令牌烧掉——那是纯手误，而用户手里只有这一枚链接。
	rec = postForm(t, stack, PathResetPassword, jar, url.Values{
		"token": {token}, "newPassword": {testPassword},
		"newPasswordConfirm": {otherPassword}, FormCSRFField: {csrf},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("两次输入不一致应报 400，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "两次输入的密码不一致") {
		t.Error("应逐字段提示两次输入不一致")
	}
	if _, err := mod.Tokens().Check(ctx, token, PurposeResetPassword); err != nil {
		t.Fatalf("校验失败不该消费令牌: %v", err)
	}

	rec = reset(t, otherPassword)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != PathLogin+"?reset=1" {
		t.Fatalf("重置状态码 = %d，Location = %q：%s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}

	// 同一枚链接点第二次必须明确报失效。
	rec = reset(t, testPassword)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("重复使用令牌状态码 = %d，期望 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), noticeResetExpired) {
		t.Error("重复使用应明确提示链接已失效")
	}

	// 新口令生效，旧口令作废。
	login := func(t *testing.T, password string) int {
		t.Helper()
		_, cookies, csrf := get(t, stack, PathLogin, nil)
		rec := postForm(t, stack, PathLogin, cookies, url.Values{
			"login": {"forgetful"}, "password": {password}, FormCSRFField: {csrf},
		})
		return rec.Code
	}
	if code := login(t, otherPassword); code != http.StatusFound {
		t.Errorf("新口令应能登录，状态码 = %d", code)
	}
	if code := login(t, testPassword); code != http.StatusUnauthorized {
		t.Errorf("旧口令应失效，状态码 = %d", code)
	}
}

// TestResetPasswordInvalidTokenRendersNotice 验证无效令牌渲染的是提示页而不是 404。
func TestResetPasswordInvalidTokenRendersNotice(t *testing.T) {
	stack, _ := newAccountStack(t)

	rec, _, _ := get(t, stack, PathResetPassword+"?token=nonsense", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("状态码 = %d，期望 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), noticeResetExpired) {
		t.Error("应提示链接无效或已过期")
	}
	if strings.Contains(rec.Body.String(), `name="newPassword"`) {
		t.Error("令牌无效时不该渲染出可提交的表单")
	}
}

// ---------- 账户页 ----------

// TestAccountPasswordChangeEndToEnd 验证改密码后所有会话失效并要求重新登录。
func TestAccountPasswordChangeEndToEnd(t *testing.T) {
	stack, _ := newAccountStack(t)
	ctx := context.Background()

	user, err := stack.Users.CreateUser(ctx, &auth.CreateUserParams{
		Username: "changer", Email: "changer@example.com",
		Password: testPassword, Roles: []string{perm.RoleMember}, EmailVerified: true,
	})
	if err != nil {
		t.Fatalf("创建用户失败: %v", err)
	}
	session, _ := stack.Session(t, "changer")

	// 原密码填错：原地重渲染并逐字段报错，不改任何东西。
	rec, jar, csrf := get(t, stack, PathAccount, session)
	if rec.Code != http.StatusOK {
		t.Fatalf("账户页状态码 = %d：%s", rec.Code, rec.Body.String())
	}
	rec = postForm(t, stack, PathAccountPassword, jar, url.Values{
		"oldPassword": {otherPassword}, "newPassword": {otherPassword},
		"newPasswordConfirm": {otherPassword}, FormCSRFField: {csrf},
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("原密码错误时状态码 = %d，期望 401", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "原密码不正确") {
		t.Error("应在原密码字段上提示")
	}

	// 填对：跳登录页，并且会话全部失效。
	_, jar, csrf = get(t, stack, PathAccount, session)
	rec = postForm(t, stack, PathAccountPassword, jar, url.Values{
		"oldPassword": {testPassword}, "newPassword": {otherPassword},
		"newPasswordConfirm": {otherPassword}, FormCSRFField: {csrf},
	})
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != PathLogin+"?changed=1" {
		t.Fatalf("改密码状态码 = %d，Location = %q：%s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	cleared := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == "lumo_session" && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Error("改密码后应清除会话 Cookie")
	}
	// 旧会话记录也必须没了——只清 Cookie 不删记录等于没踢下线。
	if _, err := stack.Users.FindUserByID(ctx, user.ID); err != nil {
		t.Fatalf("查询用户失败: %v", err)
	}
	rec, _, _ = get(t, stack, PathAccount, session)
	if rec.Code != http.StatusFound {
		t.Errorf("旧会话应已失效，账户页状态码 = %d", rec.Code)
	}
}

// TestLogoutEndToEnd 验证登出销毁会话并清 Cookie。
func TestLogoutEndToEnd(t *testing.T) {
	stack, _ := newAccountStack(t)

	if _, err := stack.Users.CreateUser(context.Background(), &auth.CreateUserParams{
		Username: "leaver", Email: "leaver@example.com",
		Password: testPassword, Roles: []string{perm.RoleMember}, EmailVerified: true,
	}); err != nil {
		t.Fatalf("创建用户失败: %v", err)
	}
	session, _ := stack.Session(t, "leaver")

	_, jar, csrf := get(t, stack, PathAccount, session)
	rec := postForm(t, stack, PathLogout, jar, url.Values{FormCSRFField: {csrf}})
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/" {
		t.Fatalf("登出状态码 = %d，Location = %q", rec.Code, rec.Header().Get("Location"))
	}
	rec, _, _ = get(t, stack, PathAccount, session)
	if rec.Code != http.StatusFound {
		t.Errorf("登出后账户页应跳登录，状态码 = %d", rec.Code)
	}
}

// TestFormCSRFIsRequired 验证缺少或错误的表单令牌一律被挡下。
func TestFormCSRFIsRequired(t *testing.T) {
	stack, _ := newAccountStack(t)
	enableRegistration(t, stack)

	_, cookies, csrf := get(t, stack, PathRegister, nil)
	form := url.Values{
		"username": {"csrf"}, "email": {"csrf@example.com"},
		"password": {testPassword}, "passwordConfirm": {testPassword},
		FormCSRFField: {csrf},
	}

	t.Run("缺少令牌", func(t *testing.T) {
		bad := url.Values{}
		for k, v := range form {
			if k != FormCSRFField {
				bad[k] = v
			}
		}
		rec := postForm(t, stack, PathRegister, cookies, bad)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("状态码 = %d，期望 400", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), noticeFormExpired) {
			t.Error("应提示表单已过期")
		}
	})

	t.Run("令牌不匹配", func(t *testing.T) {
		bad := url.Values{}
		for k, v := range form {
			bad[k] = v
		}
		bad.Set(FormCSRFField, "forged-token")
		rec := postForm(t, stack, PathRegister, cookies, bad)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("状态码 = %d，期望 400", rec.Code)
		}
	})

	t.Run("失败后仍下发新令牌以免用户陷死", func(t *testing.T) {
		rec := postForm(t, stack, PathRegister, nil, url.Values{
			"username": {"csrf2"}, "email": {"csrf2@example.com"},
			"password": {testPassword}, "passwordConfirm": {testPassword},
		})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("状态码 = %d", rec.Code)
		}
		if cookieValue(rec.Result().Cookies(), formCSRFCookieName) == "" {
			t.Error("CSRF 失败后必须下发一枚新令牌，否则用户陷在一个永远提交不了的页面里")
		}
	})
}
