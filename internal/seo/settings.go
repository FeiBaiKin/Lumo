package seo

import (
	"encoding/json"
	"net/url"
	"strings"

	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/httpx"
	"github.com/FeiBaiKin/lumo/internal/settings"
)

// GroupSEO 是 SEO 设置分组。
const GroupSEO = "seo"

// Settings 是 seo 分组的有效值。
type Settings struct {
	TitleSuffix    string `json:"titleSuffix"`
	DefaultDesc    string `json:"defaultDescription"`
	DefaultImage   string `json:"defaultImage"`
	TwitterSite    string `json:"twitterSite"`
	RobotsExtra    string `json:"robotsExtra"`
	SitemapEnabled bool   `json:"sitemapEnabled"`
	FeedEnabled    bool   `json:"feedEnabled"`
	FeedSize       int    `json:"feedSize"`
	FeedFullText   bool   `json:"feedFullText"`
	IncludePages   bool   `json:"includePages"`
}

// seoSchema 是 seo 分组的表单 Schema（agent.md §5）。
const seoSchema = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "titleSuffix": {"type": "string", "title": "标题后缀", "maxLength": 64,
                    "description": "追加在内容标题之后，如「 — 我的站点」"},
    "defaultDescription": {"type": "string", "title": "默认描述", "maxLength": 500, "x-widget": "textarea",
                           "description": "内容未写摘要时用于 meta description 与 OpenGraph"},
    "defaultImage": {"type": "string", "title": "默认分享图", "maxLength": 1024, "x-widget": "image"},
    "twitterSite": {"type": "string", "title": "Twitter 账号", "maxLength": 64,
                    "description": "含 @ 的站点账号，用于 Twitter Card"},
    "robotsExtra": {"type": "string", "title": "robots.txt 附加内容", "maxLength": 2000, "x-widget": "textarea",
                    "description": "追加在自动生成的规则之后，可写 Disallow 与 Sitemap 等"},
    "sitemapEnabled": {"type": "boolean", "title": "输出 sitemap.xml"},
    "feedEnabled": {"type": "boolean", "title": "输出订阅源"},
    "feedSize": {"type": "integer", "title": "订阅源条数", "minimum": 1, "maximum": 100},
    "feedFullText": {"type": "boolean", "title": "订阅源输出全文",
                     "description": "关闭时只输出摘要"},
    "includePages": {"type": "boolean", "title": "订阅源包含独立页面"}
  },
  "required": ["sitemapEnabled", "feedEnabled", "feedSize"]
}`

// seoDefaults 是 seo 分组的缺省值。
const seoDefaults = `{
  "titleSuffix": "",
  "defaultDescription": "",
  "defaultImage": "",
  "twitterSite": "",
  "robotsExtra": "",
  "sitemapEnabled": true,
  "feedEnabled": true,
  "feedSize": 20,
  "feedFullText": false,
  "includePages": false
}`

// settingsGroup 返回 seo 分组的声明。
func settingsGroup() app.SettingGroup {
	return app.SettingGroup{
		Name:        GroupSEO,
		Label:       "SEO",
		Description: "标题后缀、默认分享信息，以及 sitemap 与订阅源的开关",
		Order:       50,
		Schema:      json.RawMessage(seoSchema),
		Defaults:    json.RawMessage(seoDefaults),
		Public:      []string{"titleSuffix", "defaultDescription", "defaultImage", "twitterSite"},
		Check:       check,
	}
}

// check 做 Schema 表达不了的校验：分享图与 Twitter 账号须是能用的形态。
func check(values map[string]any) error {
	var details []httpx.ErrorDetail
	if raw, _ := values["defaultImage"].(string); strings.TrimSpace(raw) != "" {
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			details = append(details, httpx.ErrorDetail{
				Location: "body.defaultImage", Message: "须为含 http 或 https 协议的绝对地址"})
		}
	}
	if handle, _ := values["twitterSite"].(string); handle != "" && !strings.HasPrefix(handle, "@") {
		details = append(details, httpx.ErrorDetail{
			Location: "body.twitterSite", Message: "须以 @ 开头，如 @example"})
	}
	if len(details) > 0 {
		return &settings.ValidationError{Details: details}
	}
	return nil
}
