package account

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/FeiBaiKin/lumo/internal/mail"
	"github.com/FeiBaiKin/lumo/internal/settings"
)

// 站点信息读不到时的兜底站名。
const fallbackSiteTitle = "站点"

// siteInfo 读站点名称与对外地址。
//
// 对外地址未配置时返回空串，调用方据此拒绝发信（见 mailLink）而不是退而求其次
// 发一封相对地址的信——用户点开只会得到一个打不开的页面，比收不到信更难排查。
func (m *Module) siteInfo(ctx context.Context) (title, baseURL string) {
	title = fallbackSiteTitle
	if m.settings == nil {
		return title, ""
	}
	var site settings.Site
	if err := m.settings.Get(ctx, settings.GroupSite, &site); err != nil {
		m.logger.Warn("读取站点设置失败，验证邮件将无法发出", slog.Any("error", err))
		return title, ""
	}
	if name := strings.TrimSpace(site.Title); name != "" {
		title = name
	}
	return title, strings.TrimSuffix(strings.TrimSpace(site.URL), "/")
}

// mailLink 拼出邮件里的绝对链接；站点未配置对外地址时返回空串。
func mailLink(baseURL, path string, params map[string]string) string {
	if baseURL == "" {
		return ""
	}
	query := url.Values{}
	for key, value := range params {
		query.Set(key, value)
	}
	return baseURL + path + "?" + query.Encode()
}

// verificationMail 组装邮箱验证邮件。
//
// 只发纯文本、不带 HTML 替代部分：mail.Message 的 HTML 是可选的，
// 而两份正文一旦并存就会分叉——改了纯文本忘了改 HTML，收到的是两个版本的说法。
// 这封邮件里没有任何需要排版的内容，纯文本在哪个客户端上都是完整的。
//
// **收件人必须在这里写上**：mail.Message.Validate 要求至少一个 To，
// 而 Enqueue 对不合法的邮件是「记一条 Warn 然后丢弃」——漏了这个字段的表现是
// 注册流程一路 302 成功、日志里只有一行容易看漏的告警，用户永远收不到信。
func verificationMail(to, siteTitle, link string) *mail.Message {
	return &mail.Message{
		To:      []string{to},
		Subject: fmt.Sprintf("验证你在 %s 的邮箱", siteTitle),
		Text: fmt.Sprintf(`你好，

有人用这个邮箱在 %s 注册了账号。点下面的链接完成验证：

%s

链接 48 小时内有效。验证之后才能登录。

如果这不是你做的，忽略这封信即可——没有点开链接，账号就一直是不能登录的状态，
不会对你造成任何影响。

—— %s
`, siteTitle, link, siteTitle),
	}
}

// resetMail 组装密码重置邮件。
//
// 主题是「重置密码」而不是「找回密码」：后者听起来像站点能告诉你原密码，
// 而实际上站点从来不知道原密码，只会让你设一个新的。
func resetMail(to, siteTitle, link string) *mail.Message {
	return &mail.Message{
		To:      []string{to},
		Subject: fmt.Sprintf("重置你在 %s 的密码", siteTitle),
		Text: fmt.Sprintf(`你好，

有人请求重置你在 %s 的账号密码。点下面的链接设置一个新密码：

%s

链接 2 小时内有效，且只能用一次。

如果这不是你做的，忽略这封信即可——你的密码不会发生任何改变。

—— %s
`, siteTitle, link, siteTitle),
	}
}

// enqueue 把邮件放进后台队列。
//
// 走 mail.Service.Enqueue 而不是 Send：发信是在请求路径之外做的，
// 对方 SMTP 慢一秒，用户的这次提交就要多等一秒。
func (m *Module) enqueue(ctx context.Context, msg *mail.Message) {
	if m.mail == nil {
		return
	}
	m.mail.Enqueue(ctx, msg)
}

// mailAvailable 报告此刻能不能真的把信发出去。
//
// 注册与找回密码都要求这个条件：不能发信的注册流程会让用户卡在一个
// 永远收不到验证邮件的账号上，而那样的账号连登录都不行。
func (m *Module) mailAvailable(ctx context.Context) bool {
	return m.mail != nil && m.mail.Enabled(ctx)
}
