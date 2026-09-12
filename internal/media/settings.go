package media

import (
	"net/url"
	"strings"

	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/form"
	"github.com/FeiBaiKin/lumo/internal/httpx"
	"github.com/FeiBaiKin/lumo/internal/settings"
)

// GroupStorage 是附件存储设置分组。
const GroupStorage = "storage"

// StorageSettings 是 storage 分组的有效值。
//
// 注意此处**没有**密钥字段：S3 访问密钥只从环境变量读取（见 EnvS3AccessKey）。
type StorageSettings struct {
	Driver      string `json:"driver"`
	S3Endpoint  string `json:"s3Endpoint"`
	S3Bucket    string `json:"s3Bucket"`
	S3Region    string `json:"s3Region"`
	S3PathStyle bool   `json:"s3PathStyle"`
	S3UseSSL    bool   `json:"s3UseSsl"`
	S3PublicURL string `json:"s3PublicUrl"`
}

// s3Only 表达「只在选了 S3 时才成立」。
//
// 抽成变量是为了让六个字段的声明读起来整齐；语义与逐个写 Eq 完全一样。
var s3Only = form.Eq("driver", DriverS3)

// storageForm 是 storage 分组的表单声明（agent.md §5）。
//
// 六个 s3 字段此前一律摊在页面上，选了「本地」也得看一遍。现在它们只在选 S3 时出现，
// 而「选了 S3 就必须填地址与桶名」这条规则也从 checkStorage 挪进了声明里——
// 界面与校验读同一句话，不会再出现「页面上没要求、保存时却说必填」。
var storageForm = form.New(
	form.NewSection("存储位置",
		form.Select("driver",
			form.Opt(DriverLocal, "本地"),
			form.Opt(DriverS3, "S3 兼容对象存储"),
		).Label("存储驱动").Default(DriverLocal).
			Help("local 存到 data/uploads 并由本站提供；s3 存到任意 S3 兼容对象存储"),
	).Describe("切换驱动不会搬动已有附件，旧地址仍然有效"),

	form.NewSection("S3 兼容对象存储",
		form.Text("s3Endpoint").Label("服务地址").Required().MaxLen(256).Default("").ShowIf(s3Only).
			Help("如 https://s3.example.com 或 minio.internal:9000；带协议时以协议为准"),
		form.Text("s3Bucket").Label("存储桶").Required().MaxLen(128).Default("").ShowIf(s3Only),
		form.Text("s3Region").Label("区域").MaxLen(64).Default("us-east-1").ShowIf(s3Only).
			Help("兼容实现大多不支持区域探测，建议显式填写"),
		form.Bool("s3PathStyle").Label("路径寻址").Default(false).ShowIf(s3Only).
			Help("自建 MinIO / Ceph 通常需要开启；AWS S3 保持关闭"),
		form.Bool("s3UseSsl").Label("使用 HTTPS").Default(true).ShowIf(s3Only).
			Help("服务地址已带协议时以协议为准"),
		form.Text("s3PublicUrl").Label("公开地址前缀").MaxLen(512).Default("").ShowIf(s3Only).
			Help("CDN 或自定义域名，如 https://cdn.example.com；留空则由服务地址与桶名推导"),
	),
).Named(GroupStorage)

// storageGroup 返回 storage 分组的声明。
func storageGroup() app.SettingGroup {
	return app.SettingGroup{
		Name:        GroupStorage,
		Label:       "附件存储",
		Description: "附件存放位置。S3 访问密钥只从环境变量 " + EnvS3AccessKey + " 与 " + EnvS3SecretKey + " 读取，不保存在此处。",
		Order:       20,
		Icon:        "hard-drive",
		Form:        storageForm,
		Check:       checkStorage,
	}
}

// checkStorage 做 Schema 表达不了的校验：公开地址须为绝对地址。
//
// 「选了 s3 时地址与桶名必填」不在这里——它已经是存储位置那两个字段的声明
// （见 storageForm 的 Required + ShowIf），界面与校验共用同一句话。
//
// 也不在此处校验密钥是否已配置：设置与环境变量的生命周期不同，
// 密钥缺失要在真正建连接时报错，而不是把设置卡住不让保存。
func checkStorage(values map[string]any) error {
	var details []httpx.ErrorDetail
	if raw, _ := values["s3PublicUrl"].(string); strings.TrimSpace(raw) != "" {
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != schemeHTTP && u.Scheme != schemeHTTPS) || u.Host == "" {
			details = append(details, httpx.ErrorDetail{
				Location: "body.s3PublicUrl", Message: "须为含 http 或 https 协议的绝对地址"})
		}
	}
	if len(details) > 0 {
		return &settings.ValidationError{Details: details}
	}
	return nil
}
