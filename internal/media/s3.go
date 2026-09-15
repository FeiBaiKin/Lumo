package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// DriverS3 是 S3 兼容对象存储驱动名。
const DriverS3 = "s3"

// 公开地址与服务地址允许的协议。
const (
	schemeHTTP  = "http"
	schemeHTTPS = "https"
)

// S3 凭据的兜底环境变量。
//
// 密钥的正规去处是后台「附件存储」里的两个字段（加密入库、接口不回传），
// 环境变量留给不便改后台的部署。两处都有时以后台为准。
const (
	EnvS3AccessKey = "LUMO_S3_ACCESS_KEY"
	EnvS3SecretKey = "LUMO_S3_SECRET_KEY"
)

// ErrMissingS3Credentials 表示没有可用的 S3 凭据。
var ErrMissingS3Credentials = fmt.Errorf(
	"没有对象存储访问密钥：请在「设置 → 附件存储」里填写，或设置环境变量 %s 与 %s",
	EnvS3AccessKey, EnvS3SecretKey)

// S3Config 是 S3 兼容存储的连接参数，来自 storage 设置分组（密钥除外）。
type S3Config struct {
	// Endpoint 是服务地址，可带协议（https://minio.example.com）也可只写 host:port。
	Endpoint string
	Bucket   string
	Region   string
	// PathStyle 为真时用 endpoint/bucket/key 形式寻址。自建 MinIO、Ceph 一般需要它；
	// 各家兼容程度不一，故不做自动探测，交由部署者显式指定。
	PathStyle bool
	UseSSL    bool
	// PublicURL 是对象的公开访问前缀（如 CDN 域名）；留空则由 endpoint 与 bucket 推导。
	PublicURL string
}

// S3Storage 是基于 minio-go 的 S3 兼容驱动。
type S3Storage struct {
	client *minio.Client
	cfg    S3Config
	// urlPrefix 是预先算好的公开地址前缀，不含末尾斜杠。
	urlPrefix string
}

// S3Credentials 从环境变量读取访问密钥，作为后台未配置时的兜底。
func S3Credentials() (accessKey, secretKey string, err error) {
	accessKey = strings.TrimSpace(os.Getenv(EnvS3AccessKey))
	secretKey = strings.TrimSpace(os.Getenv(EnvS3SecretKey))
	if accessKey == "" || secretKey == "" {
		return "", "", ErrMissingS3Credentials
	}
	return accessKey, secretKey, nil
}

// NewS3Storage 构造 S3 驱动。
func NewS3Storage(cfg S3Config, accessKey, secretKey string) (*S3Storage, error) {
	if strings.TrimSpace(cfg.Bucket) == "" {
		return nil, errors.New("media: S3 存储桶名不能为空")
	}
	host, secure, err := parseEndpoint(cfg.Endpoint, cfg.UseSSL)
	if err != nil {
		return nil, err
	}

	lookup := minio.BucketLookupAuto
	if cfg.PathStyle {
		lookup = minio.BucketLookupPath
	}
	client, err := minio.New(host, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: secure,
		// 显式给出 Region：兼容实现大多不支持 GetBucketLocation，
		// 留空会让每次请求先做一次多余且可能失败的探测。
		Region:       cfg.Region,
		BucketLookup: lookup,
	})
	if err != nil {
		return nil, fmt.Errorf("media: 构造 S3 客户端: %w", err)
	}

	s := &S3Storage{client: client, cfg: cfg}
	s.urlPrefix = s.publicPrefix(host, secure)
	return s, nil
}

// Driver 实现 Storage。
func (s *S3Storage) Driver() string { return DriverS3 }

// Put 实现 Storage。
func (s *S3Storage) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	if err := validateKey(key); err != nil {
		return err
	}
	if _, err := s.client.PutObject(ctx, s.cfg.Bucket, key, r, size, minio.PutObjectOptions{
		ContentType: contentType,
	}); err != nil {
		return fmt.Errorf("上传对象 %s: %w", key, err)
	}
	return nil
}

// Delete 实现 Storage。S3 的删除本就幂等，对象不存在也返回成功。
func (s *S3Storage) Delete(ctx context.Context, key string) error {
	if err := validateKey(key); err != nil {
		return err
	}
	if err := s.client.RemoveObject(ctx, s.cfg.Bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("删除对象 %s: %w", key, err)
	}
	return nil
}

// URL 实现 Storage。
func (s *S3Storage) URL(key string) string {
	return s.urlPrefix + "/" + key
}

// publicPrefix 计算对象的公开地址前缀。
func (s *S3Storage) publicPrefix(host string, secure bool) string {
	if custom := strings.TrimSuffix(strings.TrimSpace(s.cfg.PublicURL), "/"); custom != "" {
		return custom
	}
	scheme := schemeHTTP
	if secure {
		scheme = schemeHTTPS
	}
	if s.cfg.PathStyle {
		return scheme + "://" + host + "/" + s.cfg.Bucket
	}
	return scheme + "://" + s.cfg.Bucket + "." + host
}

// parseEndpoint 把配置里的地址拆成 minio-go 需要的 host[:port] 与是否启用 TLS。
//
// 地址带协议时以协议为准（这是部署者更直观的表达），否则用 useSSL。
func parseEndpoint(endpoint string, useSSL bool) (host string, secure bool, err error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return "", false, errors.New("media: S3 服务地址不能为空")
	}
	if !strings.Contains(endpoint, "://") {
		return strings.TrimSuffix(endpoint, "/"), useSSL, nil
	}
	u, parseErr := url.Parse(endpoint)
	if parseErr != nil || u.Host == "" {
		return "", false, fmt.Errorf("media: S3 服务地址不合法: %q", endpoint)
	}
	switch u.Scheme {
	case schemeHTTPS:
		return u.Host, true, nil
	case schemeHTTP:
		return u.Host, false, nil
	default:
		return "", false, fmt.Errorf("media: S3 服务地址协议须为 http 或 https: %q", endpoint)
	}
}
