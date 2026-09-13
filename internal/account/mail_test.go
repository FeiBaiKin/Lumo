package account

import (
	"strings"
	"testing"

	"github.com/FeiBaiKin/lumo/internal/mail"
)

// TestVerificationMailIsSendable 验证验证邮件是一封**能发出去**的信。
//
// 这条不是形式主义：mail.Message.Validate 要求收件人、主题、正文三者齐全，
// 而 Enqueue 对不合法的邮件是「记一条 Warn 然后丢弃」——漏掉 To 的表现是
// 注册流程一路 302 成功、用户永远收不到信，日志里只有一行极易看漏的告警。
// 真实环境里这个问题是靠手工走查发现的，这条用例把它钉在了单元测试里。
func TestVerificationMailIsSendable(t *testing.T) {
	t.Parallel()

	const (
		to    = "reader@example.com"
		link  = "https://example.com/verify-email?token=abc"
		title = "示例站"
	)
	msg := verificationMail(to, title, link)

	if err := msg.Validate(); err != nil {
		t.Fatalf("邮件应能通过校验，实际 %v", err)
	}
	if len(msg.To) != 1 || msg.To[0] != to {
		t.Errorf("收件人 = %v，期望 [%s]", msg.To, to)
	}
	if !strings.Contains(msg.Subject, title) {
		t.Errorf("主题应带上站名，实际 %q", msg.Subject)
	}
	if !strings.Contains(msg.Text, link) {
		t.Error("正文必须含验证链接")
	}
	if !strings.Contains(msg.Text, "48") {
		t.Error("正文应说明链接的有效期")
	}
}

// TestResetMailIsSendable 验证重置邮件同样可发，且写明了一次性与有效期。
func TestResetMailIsSendable(t *testing.T) {
	t.Parallel()

	const (
		to    = "reader@example.com"
		link  = "https://example.com/reset-password?token=abc"
		title = "示例站"
	)
	msg := resetMail(to, title, link)

	if err := msg.Validate(); err != nil {
		t.Fatalf("邮件应能通过校验，实际 %v", err)
	}
	if len(msg.To) != 1 || msg.To[0] != to {
		t.Errorf("收件人 = %v，期望 [%s]", msg.To, to)
	}
	if !strings.Contains(msg.Text, link) {
		t.Error("正文必须含重置链接")
	}
	// 这两句是用户据以判断「要不要理它」的全部依据。
	if !strings.Contains(msg.Text, "只能用一次") {
		t.Error("正文应说明链接只能用一次")
	}
	if !strings.Contains(msg.Text, "密码不会发生任何改变") {
		t.Error("正文应说明非本人操作时密码不会改变")
	}
}

// TestMailBodyHasNoMarketing 验证两封信里既没有营销语也没有表情符号。
//
// agent.md §11.1 禁止界面用表情符号，邮件是界面的延伸；
// 而「快来写第一篇博客吧」这类话在一个只有三五个读者的自建站上格外滑稽。
func TestMailBodyHasNoMarketing(t *testing.T) {
	t.Parallel()

	for _, msg := range []*mail.Message{
		verificationMail("a@example.com", "示例站", "https://example.com/v"),
		resetMail("a@example.com", "示例站", "https://example.com/r"),
	} {
		for _, r := range msg.Text {
			// Emoji 与各类符号都在 U+1F300 之上；本项目的文案只用中日韩标点与拉丁字符。
			if r > 0x1F000 {
				t.Errorf("邮件正文不该含表情符号，发现 %q", r)
				break
			}
		}
		if strings.Contains(msg.Text, "欢迎加入") || strings.Contains(msg.Text, "快来") {
			t.Errorf("邮件不该带营销语：%q", msg.Subject)
		}
	}
}

// TestMailLinkRequiresSiteURL 验证站点没配对外地址时拼不出链接。
//
// 拼不出链接就不发信：一封链接是相对地址的信，用户点开只会得到一个打不开的页面，
// 而「收不到信」至少还让人知道去问站长。
func TestMailLinkRequiresSiteURL(t *testing.T) {
	t.Parallel()

	if got := mailLink("", "/verify-email", map[string]string{"token": "t"}); got != "" {
		t.Errorf("没有对外地址时不该拼出链接，实际 %q", got)
	}
	got := mailLink("https://example.com", "/verify-email", map[string]string{"token": "a b"})
	if got != "https://example.com/verify-email?token=a+b" {
		t.Errorf("链接 = %q，令牌应经过 URL 编码", got)
	}
}
