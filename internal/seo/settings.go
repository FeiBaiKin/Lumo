package seo

import (
	"net/url"
	"strings"

	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/form"
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

// seoForm 是 seo 分组的表单声明（agent.md §5）。
//
// 订阅源的两段只在开启订阅源时出现；此前 feedSize 与 feedFullText 一直摆在那里，
// 即使订阅源是关的。
var seoForm = form.New(
	form.NewSection("元信息",
		form.Text("titleSuffix").Label("标题后缀").MaxLen(64).Default("").
			Help("追加在内容标题之后，如「 — 我的站点」"),
		form.Textarea("defaultDescription").Label("默认描述").MaxLen(500).Default("").
			Help("内容未写摘要时用于 meta description 与 OpenGraph"),
		form.Image("defaultImage").Label("默认分享图").MaxLen(1024).Default("").
			Help("内容未设置封面时用于分享卡片；建议 1200×630"),
		form.Text("twitterSite").Label("Twitter 账号").MaxLen(64).Default("").
			Help("含 @ 的站点账号，用于 Twitter Card"),
	),

	form.NewSection("站点地图",
		form.Bool("sitemapEnabled").Label("输出 sitemap.xml").Default(true),
		form.Bool("includePages").Label("收录独立页面").Default(false).
			ShowIf(form.Eq("sitemapEnabled", true)).
			Help("默认只收录文章；独立页面通常是「关于」「联系」这类，是否收录取决于站点形态"),
	).Describe("站点地图与订阅源必须位于站点根路径，爬虫只认根路径。未配置对外地址时它们会明确报错，不产出相对地址"),

	form.NewSection("订阅源",
		form.Bool("feedEnabled").Label("输出订阅源").Default(true),
		form.Int("feedSize").Label("订阅源条数").Min(1).Max(100).Default(20).
			ShowIf(form.Eq("feedEnabled", true)),
		form.Bool("feedFullText").Label("订阅源输出全文").Default(false).
			ShowIf(form.Eq("feedEnabled", true)).
			Help("关闭时只输出摘要"),
	),

	form.NewSection("robots.txt",
		form.Textarea("robotsExtra").Label("附加内容").MaxLen(2000).Rows(6).Default("").
			Help("追加在自动生成的规则之后，可写 Disallow 与 Sitemap 等"),
	),
).Named(GroupSEO)

// settingsGroup 返回 seo 分组的声明。
func settingsGroup() app.SettingGroup {
	return app.SettingGroup{
		Name:        GroupSEO,
		Label:       "SEO",
		Description: "标题后缀、默认分享信息，以及 sitemap 与订阅源的开关",
		Order:       50,
		Form:        seoForm,
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
