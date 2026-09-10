package media

import (
	"encoding/json"
	"net/url"
	"strings"

	"github.com/FeiBaiKin/lumo/internal/app"
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

// storageSchema 是 storage 分组的表单 Schema（agent.md §5）。
const storageSchema = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "driver": {"type": "string", "title": "存储驱动", "enum": ["local", "s3"], "x-widget": "select",
               "description": "local 存到 data/uploads 并由本站提供；s3 存到任意 S3 兼容对象存储"},
    "s3Endpoint": {"type": "string", "title": "S3 服务地址", "maxLength": 256,
                   "description": "如 https://s3.example.com 或 minio.internal:9000；带协议时以协议为准"},
    "s3Bucket": {"type": "string", "title": "存储桶", "maxLength": 128},
    "s3Region": {"type": "string", "title": "区域", "maxLength": 64,
                 "description": "兼容实现大多不支持区域探测，建议显式填写，如 us-east-1"},
    "s3PathStyle": {"type": "boolean", "title": "路径寻址",
                    "description": "自建 MinIO / Ceph 通常需要开启；AWS S3 保持关闭"},
    "s3UseSsl": {"type": "boolean", "title": "使用 HTTPS", "description": "服务地址已带协议时以协议为准"},
    "s3PublicUrl": {"type": "string", "title": "公开地址前缀", "maxLength": 512,
                    "description": "CDN 或自定义域名，如 https://cdn.example.com；留空则由服务地址与桶名推导"}
  },
  "required": ["driver"]
}`

// storageDefaults 是 storage 分组的缺省值。
const storageDefaults = `{
  "driver": "local",
  "s3Endpoint": "",
  "s3Bucket": "",
  "s3Region": "",
  "s3PathStyle": false,
  "s3UseSsl": true,
  "s3PublicUrl": ""
}`

// storageGroup 返回 storage 分组的声明。
func storageGroup() app.SettingGroup {
	return app.SettingGroup{
		Name:        GroupStorage,
		Label:       "附件存储",
		Description: "附件存放位置。S3 访问密钥只从环境变量 " + EnvS3AccessKey + " 与 " + EnvS3SecretKey + " 读取，不保存在此处。",
		Order:       20,
		Schema:      json.RawMessage(storageSchema),
		Defaults:    json.RawMessage(storageDefaults),
		Check:       checkStorage,
	}
}

// checkStorage 做 Schema 表达不了的校验：选 s3 时地址与桶名必填，公开地址须为绝对地址。
//
// 不在此处校验密钥是否已配置：设置与环境变量的生命周期不同，
// 密钥缺失要在真正建连接时报错，而不是把设置卡住不让保存。
func checkStorage(values map[string]any) error {
	var details []httpx.ErrorDetail
	driver, _ := values["driver"].(string)
	if driver == DriverS3 {
		if endpoint, _ := values["s3Endpoint"].(string); strings.TrimSpace(endpoint) == "" {
			details = append(details, httpx.ErrorDetail{Location: "body.s3Endpoint", Message: "选择 S3 存储时必填"})
		}
		if bucket, _ := values["s3Bucket"].(string); strings.TrimSpace(bucket) == "" {
			details = append(details, httpx.ErrorDetail{Location: "body.s3Bucket", Message: "选择 S3 存储时必填"})
		}
	}
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
