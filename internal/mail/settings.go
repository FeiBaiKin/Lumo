package mail

import (
	"net/mail"
	"os"
	"strings"

	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/form"
	"github.com/FeiBaiKin/lumo/internal/httpx"
	"github.com/FeiBaiKin/lumo/internal/settings"
)

// GroupMail 是发信设置分组。
const GroupMail = "mail"

// EnvSMTPPassword 是 SMTP 口令的环境变量名（agent.md §9）。
//
// 与数据库 DSN、S3 密钥同一处置：设置会随备份、日志与接口响应流出，口令不进库。
const EnvSMTPPassword = "LUMO_SMTP_PASSWORD"

// 传输加密方式。
const (
	// EncryptionNone 明文连接，仅适合同机或内网的中继。
	EncryptionNone = "none"
	// EncryptionStartTLS 先明文连接再升级为 TLS，对应 587 端口。
	EncryptionStartTLS = "starttls"
	// EncryptionTLS 直接建立 TLS 连接，对应 465 端口。
	EncryptionTLS = "tls"
)

// Settings 是 mail 分组的有效值。
//
// 注意此处**没有**口令字段：口令只从环境变量读取。
type Settings struct {
	Enabled     bool   `json:"enabled"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Username    string `json:"username"`
	Encryption  string `json:"encryption"`
	FromAddress string `json:"fromAddress"`
	FromName    string `json:"fromName"`
}

// Password 从环境变量读取 SMTP 口令。
func Password() string {
	return os.Getenv(EnvSMTPPassword)
}

// mailForm 是 mail 分组的表单声明（agent.md §5）。
//
// 未启用发信时，SMTP 服务器与发件人两段整段不出现——此前它们一律摊在页面上，
// 一个只想关掉通知的站长仍要面对七个字段。
var mailForm = form.New(
	form.NewSection("发信",
		form.Bool("enabled").Label("启用邮件发送").Default(false).
			Help("关闭时评论通知等邮件不会发出，也不会重试"),
	).Describe("口令只从环境变量 "+EnvSMTPPassword+" 读取，不保存在这里"),

	form.NewSection("SMTP 服务器",
		form.Text("host").Label("SMTP 服务器").Required().MaxLen(256).Default("").
			ShowIf(form.Eq("enabled", true)),
		form.Int("port").Label("端口").Min(1).Max(65535).Default(587).
			ShowIf(form.Eq("enabled", true)),
		form.Text("username").Label("用户名").MaxLen(256).Default("").
			ShowIf(form.Eq("enabled", true)).
			Help("留空表示服务器不需要认证；口令请设环境变量 "+EnvSMTPPassword),
		form.Select("encryption",
			form.Opt(EncryptionNone, "不加密"),
			form.Opt(EncryptionStartTLS, "STARTTLS"),
			form.Opt(EncryptionTLS, "TLS"),
		).Label("加密方式").Default(EncryptionStartTLS).
			ShowIf(form.Eq("enabled", true)).
			Help("STARTTLS 对应 587 端口，TLS 对应 465。"+
				"STARTTLS 是强制升级而非机会性加密——服务器不支持时直接报错，不会悄悄退回明文"),
	),

	form.NewSection("发件人",
		form.Text("fromAddress").Label("发件地址").Required().MaxLen(256).Default("").
			ShowIf(form.Eq("enabled", true)),
		form.Text("fromName").Label("发件人名称").MaxLen(128).Default("Lumo").
			ShowIf(form.Eq("enabled", true)),
	),
).Named(GroupMail)

// group 返回 mail 分组的声明。
func group() app.SettingGroup {
	return app.SettingGroup{
		Name:        GroupMail,
		Label:       "邮件发送",
		Description: "SMTP 发信配置。口令只从环境变量 " + EnvSMTPPassword + " 读取，不保存在此处。",
		Order:       30,
		Icon:        "mail",
		Form:        mailForm,
		// 只公开「开着没有」，不公开主机、端口与账号：主题据此决定要不要给
		// 「注册」入口，而注册必须先能发验证邮件（见 internal/account 的 registrationOpen）。
		// 让入口跟着这条走，才不会出现「入口看得见、点进去是一张暂未开放的页」。
		Public: []string{"enabled"},
		Check:  check,
	}
}

// check 做 Schema 表达不了的校验：发件地址须是合法的邮件地址。
//
// 「启用时服务器与发件地址必填」不在这里——它是那两个字段的 Required + ShowIf，
// 界面与校验读的是同一句话。
func check(values map[string]any) error {
	var details []httpx.ErrorDetail

	from, _ := values["fromAddress"].(string)
	from = strings.TrimSpace(from)
	if from != "" {
		if _, err := mail.ParseAddress(from); err != nil {
			details = append(details, httpx.ErrorDetail{Location: "body.fromAddress", Message: "不是合法的邮件地址"})
		}
	}
	if len(details) > 0 {
		return &settings.ValidationError{Details: details}
	}
	return nil
}
