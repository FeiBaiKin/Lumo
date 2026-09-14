package account

import (
	"context"
	"log/slog"

	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/form"
)

// GroupAccount 是前台账户设置分组。
const GroupAccount = "account"

// 设置项键名。
const (
	keyAllowRegistration = "allowRegistration"
	keyNotice            = "registrationNotice"
)

// Settings 是 account 分组的有效值。
type Settings struct {
	// AllowRegistration 为真才开放自助注册。
	AllowRegistration bool `json:"allowRegistration"`
	// RegistrationNotice 是显示在注册表单上方的纯文本，不解析 HTML。
	RegistrationNotice string `json:"registrationNotice"`
}

// accountForm 是 account 分组的表单声明（agent.md §5）。
//
// 只有两项，是刻意的：
//   - 不做 defaultRole 下拉——注册用户固定 member。一个能让访客自助拿到 author
//     的开关是自找麻烦，而它带来的灵活性没有任何一个站长真的会用到。
//   - 不做令牌有效期设置——48 小时与 2 小时见 module.go 的常量与理由。
//     设置项不是越多越好：每一项都要在 UI 上解释、在文档里重复一遍。
var accountForm = form.New(
	form.NewSection("注册",
		form.Bool(keyAllowRegistration).Label("开放注册").Default(false).
			Help("关闭后 /register 返回 404。开启前请先在邮件设置里配好 SMTP："+
				"注册需要发送验证邮件，站点对外地址（站点设置里的 url）也要先填好——"+
				"验证链接必须是一个能点开的绝对地址"),
		form.Textarea(keyNotice).Label("注册页说明").MaxLen(500).Default("").
			Help("显示在注册表单上方的纯文本，可写站规或审核说明；不解析 HTML"),
	),
).Named(GroupAccount)

// group 返回 account 分组的声明。
func group() app.SettingGroup {
	return app.SettingGroup{
		Name:        GroupAccount,
		Label:       "前台账户",
		Description: "访客自助注册与账户页",
		// 排在「邮件发送」（30）之后：开放注册的前提是先有能发信的 SMTP，
		// 设置页的顺序也该照这个因果排。
		Order: 35,
		Icon:  "users",
		Form:  accountForm,
		// 白名单只放这两项：主题据此决定页眉要不要显示「注册」入口。
		// 其余字段（将来若有）不公开——Public 平面是匿名可读的。
		Public: []string{keyAllowRegistration, keyNotice},
		// 不进侧边栏：这两项说的是「谁能成为用户」，与「用户」是同一件事，
		// 分成两个入口只会让站长在两者之间找。表单改由用户页承载，地址不变。
		Hidden: true,
	}
}

// settingsOf 读取 account 分组的有效值；设置服务不可用时退回缺省值。
//
// 不返回错误：注册开关读不到就按「关闭」处理（fail-closed），
// 而不是让整个注册页 500——一次设置读取失败不该表现为站点故障。
func (m *Module) settingsOf(ctx context.Context) Settings {
	var cfg Settings
	if m.settings == nil {
		return cfg
	}
	if err := m.settings.Get(ctx, GroupAccount, &cfg); err != nil {
		m.logger.Warn("读取前台账户设置失败，按未开放注册处理", slog.Any("error", err))
		return Settings{}
	}
	return cfg
}
