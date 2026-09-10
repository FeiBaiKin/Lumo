package mail

import (
	"context"
	"fmt"
	"strings"

	gomail "github.com/wneessen/go-mail"
)

// smtpSender 是基于 go-mail 的发信实现。
type smtpSender struct {
	cfg      Settings
	password string
}

// NewSMTPSender 按配置构造发信器。
//
// 每次发信重新拨号：CMS 的发信频率很低，长连接带来的复杂度（超时、断线重连、
// 服务器主动断开）远大于收益。
func NewSMTPSender(cfg *Settings, password string) (Sender, error) {
	if !cfg.Enabled {
		return nil, ErrDisabled
	}
	if strings.TrimSpace(cfg.Host) == "" {
		return nil, fmt.Errorf("mail: 未配置 SMTP 服务器")
	}
	if strings.TrimSpace(cfg.FromAddress) == "" {
		return nil, fmt.Errorf("mail: 未配置发件地址")
	}
	if strings.TrimSpace(cfg.Username) != "" && password == "" {
		return nil, ErrMissingPassword
	}
	return &smtpSender{cfg: *cfg, password: password}, nil
}

// Send 实现 Sender。
func (s *smtpSender) Send(ctx context.Context, msg *Message) error {
	if err := msg.Validate(); err != nil {
		return err
	}

	m := gomail.NewMsg()
	if err := m.FromFormat(s.cfg.FromName, s.cfg.FromAddress); err != nil {
		return fmt.Errorf("设置发件人: %w", err)
	}
	if err := m.To(msg.To...); err != nil {
		return fmt.Errorf("设置收件人: %w", err)
	}
	m.Subject(msg.Subject)
	m.SetBodyString(gomail.TypeTextPlain, msg.Text)
	if strings.TrimSpace(msg.HTML) != "" {
		m.AddAlternativeString(gomail.TypeTextHTML, msg.HTML)
	}

	client, err := s.client()
	if err != nil {
		return err
	}
	if err := client.DialAndSendWithContext(ctx, m); err != nil {
		return fmt.Errorf("发送邮件: %w", err)
	}
	return nil
}

// client 按加密方式与认证配置构造 SMTP 客户端。
func (s *smtpSender) client() (*gomail.Client, error) {
	opts := []gomail.Option{gomail.WithPort(s.port())}

	switch s.cfg.Encryption {
	case EncryptionTLS:
		opts = append(opts, gomail.WithSSL())
	case EncryptionNone:
		// 明文：显式声明而非依赖默认值，免得改了库的默认策略就悄悄降级或升级。
		opts = append(opts, gomail.WithTLSPolicy(gomail.NoTLS))
	default:
		// STARTTLS 且**强制**：机会性加密会在服务器不支持时静默退回明文，
		// 口令就这么裸奔出去了。
		opts = append(opts, gomail.WithTLSPolicy(gomail.TLSMandatory))
	}

	if strings.TrimSpace(s.cfg.Username) != "" {
		opts = append(opts,
			gomail.WithSMTPAuth(gomail.SMTPAuthAutoDiscover),
			gomail.WithUsername(s.cfg.Username),
			gomail.WithPassword(s.password),
		)
	}

	client, err := gomail.NewClient(s.cfg.Host, opts...)
	if err != nil {
		return nil, fmt.Errorf("构造 SMTP 客户端: %w", err)
	}
	return client, nil
}

// port 返回实际使用的端口，未配置时按加密方式取约定端口。
func (s *smtpSender) port() int {
	if s.cfg.Port > 0 {
		return s.cfg.Port
	}
	switch s.cfg.Encryption {
	case EncryptionTLS:
		return 465
	case EncryptionNone:
		return 25
	default:
		return 587
	}
}
