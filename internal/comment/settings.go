package comment

import (
	"encoding/json"

	"github.com/FeiBaiKin/lumo/internal/app"
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

// commentSchema 是 comment 分组的表单 Schema（agent.md §5）。
const commentSchema = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "enabled": {"type": "boolean", "title": "开放评论"},
    "allowAnonymous": {"type": "boolean", "title": "允许访客评论",
                       "description": "关闭后只有已登录用户能发表评论"},
    "requireEmail": {"type": "boolean", "title": "访客必须填写邮箱",
                     "description": "邮箱不会公开，仅用于回复通知"},
    "requireApproval": {"type": "boolean", "title": "评论需审核后显示"},
    "maxLength": {"type": "integer", "title": "评论长度上限", "minimum": 1, "maximum": 10000},
    "intervalSeconds": {"type": "integer", "title": "同一 IP 的发表间隔（秒）", "minimum": 0, "maximum": 3600,
                        "description": "0 表示不限制"},
    "maxLinks": {"type": "integer", "title": "允许的链接数", "minimum": 0, "maximum": 50,
                 "description": "超过此数的评论直接判为垃圾"},
    "blocklist": {"type": "string", "title": "关键词黑名单", "maxLength": 4000, "x-widget": "textarea",
                  "description": "每行一个关键词，命中即判为垃圾（不区分大小写）"},
    "notifyNew": {"type": "boolean", "title": "有新评论时邮件通知"},
    "notifyReply": {"type": "boolean", "title": "回复时邮件通知被回复者"},
    "notifyTo": {"type": "string", "title": "通知收件地址", "maxLength": 256,
                 "description": "留空则发给内容作者的邮箱"}
  },
  "required": ["enabled", "maxLength"]
}`

// commentDefaults 是 comment 分组的缺省值。
//
// 默认开启审核：一个刚上线、还没配反垃圾的站点，宁可让管理员多点几下，
// 也不该让垃圾评论直接出现在前台。
const commentDefaults = `{
  "enabled": true,
  "allowAnonymous": true,
  "requireEmail": true,
  "requireApproval": true,
  "maxLength": 2000,
  "intervalSeconds": 30,
  "maxLinks": 3,
  "blocklist": "",
  "notifyNew": true,
  "notifyReply": true,
  "notifyTo": ""
}`

// settingsGroup 返回 comment 分组的声明。
func settingsGroup() app.SettingGroup {
	return app.SettingGroup{
		Name:        GroupComment,
		Label:       "评论",
		Description: "评论的开放范围、审核策略与反垃圾规则",
		Order:       40,
		Schema:      json.RawMessage(commentSchema),
		Defaults:    json.RawMessage(commentDefaults),
		// 前台要据此决定是否渲染评论框、是否显示邮箱字段、以及本地先做一次长度校验。
		Public: []string{"enabled", "allowAnonymous", "requireEmail", "requireApproval", "maxLength"},
	}
}
