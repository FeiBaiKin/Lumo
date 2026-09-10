package settings

import (
	"encoding/json"
	"net/url"
	"time"

	"github.com/FeiBaiKin/lumo/internal/app"
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

// siteSchema 是 site 分组的表单 Schema（agent.md §5）。
const siteSchema = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "title": {"type": "string", "title": "站点标题", "minLength": 1, "maxLength": 128},
    "subtitle": {"type": "string", "title": "副标题", "maxLength": 256},
    "description": {"type": "string", "title": "站点描述", "maxLength": 1000, "x-widget": "textarea"},
    "url": {"type": "string", "title": "对外地址", "maxLength": 512,
            "description": "含协议的绝对地址，如 https://example.com；留空则使用服务器配置"},
    "language": {"type": "string", "title": "语言", "enum": ["zh-CN", "en-US"], "x-widget": "select"},
    "timezone": {"type": "string", "title": "时区", "minLength": 1, "maxLength": 64,
                 "description": "IANA 时区名，如 Asia/Shanghai"},
    "logoUrl": {"type": "string", "title": "Logo", "maxLength": 1024, "x-widget": "image"},
    "faviconUrl": {"type": "string", "title": "Favicon", "maxLength": 1024, "x-widget": "image"},
    "slugStrategy": {"type": "string", "title": "链接别名生成方式", "enum": ["unicode", "pinyin"], "x-widget": "select",
                     "description": "unicode 保留中文；pinyin 把汉字转为拼音"},
    "pageSize": {"type": "integer", "title": "前台每页条数", "minimum": 1, "maximum": 100}
  },
  "required": ["title", "language", "timezone", "slugStrategy", "pageSize"]
}`

// siteDefaults 是 site 分组的缺省值。
const siteDefaults = `{
  "title": "Lumo",
  "subtitle": "",
  "description": "",
  "url": "",
  "language": "zh-CN",
  "timezone": "Asia/Shanghai",
  "logoUrl": "",
  "faviconUrl": "",
  "slugStrategy": "unicode",
  "pageSize": 10
}`

// siteGroup 返回 site 分组的声明。
func siteGroup() app.SettingGroup {
	return app.SettingGroup{
		Name:        GroupSite,
		Label:       "站点",
		Description: "站点的基本信息与前台行为",
		Order:       0,
		Schema:      json.RawMessage(siteSchema),
		Defaults:    json.RawMessage(siteDefaults),
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
