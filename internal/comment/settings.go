package comment

import (
	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/form"
)

// GroupComment 是评论设置分组。
const GroupComment = "comment"

// Settings 是 comment 分组的有效值。
type Settings struct {
	Enabled         bool   `json:"enabled"`
	AllowAnonymous  bool   `json:"allowAnonymous"`
	RequireEmail    bool   `json:"requireEmail"`
	RequireApproval bool   `json:"requireApproval"`
	MaxLength       int    `json:"maxLength"`
	IntervalSeconds int    `json:"intervalSeconds"`
	MaxLinks        int    `json:"maxLinks"`
	Blocklist       string `json:"blocklist"`
	NotifyNew       bool   `json:"notifyNew"`
	NotifyReply     bool   `json:"notifyReply"`
	NotifyTo        string `json:"notifyTo"`
}

// commentForm 是 comment 分组的表单声明（agent.md §5）。
//
// 三段按「开放范围 / 反垃圾 / 通知」切。评论关掉时后两段整段不显示——
// 一个不打算开放评论的站点，没有理由被要求决定同一 IP 隔几秒能发一次。
var commentForm = form.New(
	form.NewSection("开放范围",
		form.Bool("enabled").Label("开放评论").Default(true),
		form.Bool("allowAnonymous").Label("允许访客评论").Default(true).
			ShowIf(form.Eq("enabled", true)).
			Help("关闭后只有已登录用户能发表评论"),
		form.Bool("requireEmail").Label("访客必须填写邮箱").Default(true).
			ShowIf(form.Eq("enabled", true)).
			Help("邮箱不会公开，仅用于回复通知"),
		form.Bool("requireApproval").Label("评论需审核后显示").Default(true).
			ShowIf(form.Eq("enabled", true)),
	).Describe("评论是唯一由匿名访客写入的内容，信任模型与正文相反：一律先全文转义再做有限富化"),

	form.NewSection("反垃圾",
		form.Int("maxLength").Label("评论长度上限").Min(1).Max(10000).Default(2000).
			ShowIf(form.Eq("enabled", true)),
		form.Int("intervalSeconds").Label("同一 IP 的发表间隔").Min(0).Max(3600).Default(30).
			Unit("秒").ShowIf(form.Eq("enabled", true)).
			Help("0 表示不限制"),
		form.Int("maxLinks").Label("允许的链接数").Min(0).Max(50).Default(3).
			ShowIf(form.Eq("enabled", true)).
			Help("超过此数的评论直接判为垃圾"),
		form.Textarea("blocklist").Label("关键词黑名单").MaxLen(4000).Rows(6).Default("").
			ShowIf(form.Eq("enabled", true)).
			Help("每行一个关键词，命中即判为垃圾（不区分大小写）"),
	),

	form.NewSection("通知",
		form.Bool("notifyNew").Label("有新评论时邮件通知").Default(true).
			ShowIf(form.Eq("enabled", true)),
		form.Bool("notifyReply").Label("回复时邮件通知被回复者").Default(true).
			ShowIf(form.Eq("enabled", true)),
		form.Text("notifyTo").Label("通知收件地址").MaxLen(256).Default("").
			ShowIf(form.Eq("enabled", true)).
			Help("留空则发给内容作者的邮箱"),
	),
).Named(GroupComment)

// settingsGroup 返回 comment 分组的声明。
func settingsGroup() app.SettingGroup {
	return app.SettingGroup{
		Name:        GroupComment,
		Label:       "评论",
		Description: "评论的开放范围、审核策略与反垃圾规则",
		Order:       40,
		Icon:        "message-square",
		Toggle:      "enabled",
		Form:        commentForm,
		// 前台要据此决定是否渲染评论框、是否显示邮箱字段、以及本地先做一次长度校验。
		Public: []string{"enabled", "allowAnonymous", "requireEmail", "requireApproval", "maxLength"},
	}
}
