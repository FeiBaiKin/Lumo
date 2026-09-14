// Package mail 提供 SMTP 发信能力（agent.md §8）。
//
// 对外只暴露 Sender 接口与一个后台队列：业务模块把邮件丢进队列就返回，
// 绝不在请求路径上等 SMTP——对方服务器慢一秒，用户的评论就要多等一秒。
package mail

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
)

// Message 是一封待发送的邮件。
type Message struct {
	// To 是收件人地址，至少一个。
	To []string
	// Subject 是主题。
	Subject string
	// Text 是纯文本正文，必填：不是所有客户端都渲染 HTML。
	Text string
	// HTML 是可选的 HTML 正文，作为替代部分随信发出。
	HTML string
}

// Validate 校验邮件的基本形态。
func (m *Message) Validate() error {
	if len(m.To) == 0 {
		return errors.New("邮件缺少收件人")
	}
	for _, addr := range m.To {
		if _, err := mail.ParseAddress(addr); err != nil {
			return fmt.Errorf("收件人地址不合法 %q: %w", addr, err)
		}
	}
	if strings.TrimSpace(m.Subject) == "" {
		return errors.New("邮件缺少主题")
	}
	if strings.TrimSpace(m.Text) == "" {
		return errors.New("邮件缺少正文")
	}
	return nil
}

// Sender 发送一封邮件并等待结果。
//
// 业务代码通常不直接用它，而是用 Service.Enqueue 走后台队列。
type Sender interface {
	Send(ctx context.Context, msg *Message) error
}

// ErrDisabled 表示未启用发信。
var ErrDisabled = errors.New("未启用邮件发送")

// ErrMissingPassword 表示配置了用户名却没有任何口令可用。
var ErrMissingPassword = fmt.Errorf(
	"已配置 SMTP 用户名但没有口令：请在「设置 → 邮件发送」里填写，或设置环境变量 %s", EnvSMTPPassword)
