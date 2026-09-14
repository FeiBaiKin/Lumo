package settings

import (
	"net/url"
	"time"

	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/form"
	"github.com/FeiBaiKin/lumo/internal/httpx"
)

// GroupSite 是站点基本信息分组。
const GroupSite = "site"

// slug 生成策略。
const (
	SlugUnicode = "unicode"
	SlugPinyin  = "pinyin"
)

// Site 是 site 分组的有效值。
type Site struct {
	Title        string `json:"title"`
	Subtitle     string `json:"subtitle"`
	Description  string `json:"description"`
	URL          string `json:"url"`
	Language     string `json:"language"`
	Timezone     string `json:"timezone"`
	LogoURL      string `json:"logoUrl"`
	FaviconURL   string `json:"faviconUrl"`
	SlugStrategy string `json:"slugStrategy"`
	PageSize     int    `json:"pageSize"`
}

// siteForm 是 site 分组的表单声明（agent.md §5）。
//
// 声明式而非手写 JSON：字段名在 Site 结构体、这份表单与 Public 白名单里各出现一次，
// 但前两处的对应关系现在由编译器看着——写错一个键是编译错误，不是「打开那一页才发现」。
//
// 分段按「站长会一起改的东西」切：站名与副标题一起改，标志与图标一起换，
// 地址、语言、时区都属于「这个站住在哪」，最后一段是内容呈现的偏好。
var siteForm = form.New(
	form.NewSection("基本信息",
		form.Text("title").Label("站点标题").Required().MinLen(1).MaxLen(128).Default("Lumo"),
		form.Text("subtitle").Label("副标题").MaxLen(256).Default(""),
		form.Textarea("description").Label("站点描述").MaxLen(1000).Default("").
			Help("用于首页的 meta description 与分享卡片；留空则退回 SEO 设置里的默认描述"),
	).Describe("站点的名字与自我介绍"),

	form.NewSection("标识",
		form.Image("logoUrl").Label("站点标志").MaxLen(1024).Default("").
			Help("显示在后台侧栏与主题页头；建议方形、背景透明"),
		form.Image("faviconUrl").Label("站点图标").MaxLen(1024).Default("").
			Help("浏览器标签页上的小图标；建议 32×32 的 PNG 或 ICO"),
	),

	form.NewSection("地址与语言",
		form.Text("url").Label("对外地址").MaxLen(512).Default("").
			Help("含协议的绝对地址，如 https://example.com；留空则使用服务器配置。"+
				"sitemap 与订阅源需要绝对地址，未配置时会明确报错而不是产出相对地址"),
		form.Select("language",
			form.Opt("zh-CN", "简体中文"),
			form.Opt("en-US", "English"),
		).Label("语言").Default("zh-CN"),
		form.Text("timezone").Label("时区").Required().MinLen(1).MaxLen(64).Default("Asia/Shanghai").
			Help("IANA 时区名，如 Asia/Shanghai。定时发布的判断以此为准"),
	),

	form.NewSection("内容与链接",
		form.Select("slugStrategy",
			form.Opt("unicode", "保留中文"),
			form.Opt("pinyin", "转为拼音"),
		).Label("链接别名生成方式").Default("unicode").
			Help("由标题自动生成链接时使用；已有内容的链接不受影响"),
		form.Slider("pageSize").Label("前台每页条数").Min(1).Max(100).Default(10),
	),
).Named(GroupSite)

// siteGroup 返回 site 分组的声明。
func siteGroup() app.SettingGroup {
	return app.SettingGroup{
		Name: GroupSite,
		// 「站点信息」而不是「站点」：设置页把六个分组并排铺开，
		// 一行「站点」夹在「附件存储」「邮件发送」中间读不出是什么，
		// 而这一组装的正是站名、地址、语言这些站点自身的信息。
		Label:       "站点信息",
		Description: "站点的基本信息与前台行为",
		Order:       0,
		Icon:        Icon,
		Form:        siteForm,
		Public:      []string{"title", "subtitle", "description", "url", "language", "logoUrl", "faviconUrl"},
		Check:       checkSite,
	}
}

// checkSite 做 Schema 表达不了的校验：时区名须真实存在，对外地址须为 http(s) 绝对地址。
func checkSite(values map[string]any) error {
	var details []httpx.ErrorDetail
	if tz, _ := values["timezone"].(string); tz != "" {
		if _, err := time.LoadLocation(tz); err != nil {
			details = append(details, httpx.ErrorDetail{Location: "body.timezone", Message: "不是有效的 IANA 时区名"})
		}
	}
	if raw, _ := values["url"].(string); raw != "" {
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			details = append(details, httpx.ErrorDetail{Location: "body.url", Message: "须为含 http 或 https 协议的绝对地址"})
		}
	}
	if len(details) > 0 {
		return &ValidationError{Details: details}
	}
	return nil
}
