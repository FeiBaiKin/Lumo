package media

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestValidateKeyRejectsTraversal(t *testing.T) {
	t.Parallel()

	bad := []string{
		"",
		"../etc/passwd",
		"2026/../../etc/passwd",
		"/absolute/path",
		"2026//09/a.png",
		"2026/09/",
		"2026\\09\\a.png",
		"2026/09/a b.png",
		"2026/./a.png",
		".hidden",
		strings.Repeat("a", maxKeyLength+1),
	}
	for _, key := range bad {
		if err := validateKey(key); err == nil {
			t.Errorf("%q 应被拒绝", key)
		} else if !errors.Is(err, ErrInvalidKey) {
			t.Errorf("%q 应返回 ErrInvalidKey，实际 %v", key, err)
		}
	}

	good := []string{"2026/09/abc.png", "a", "2026/09/ab-cd_ef.tar.gz"}
	for _, key := range good {
		if err := validateKey(key); err != nil {
			t.Errorf("%q 应通过校验: %v", key, err)
		}
	}
}

func TestNewObjectKeyIsRandomAndDated(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	key, name, err := newObjectKey(at, ".png")
	if err != nil {
		t.Fatalf("生成对象键失败: %v", err)
	}
	if !strings.HasPrefix(key, "2026/09/") {
		t.Errorf("键应按年月分目录，实际 %q", key)
	}
	if !strings.HasSuffix(name, ".png") || !strings.HasSuffix(key, name) {
		t.Errorf("键 %q 应以文件名 %q 结尾", key, name)
	}
	if len(name) != randomNameBytes*2+len(".png") {
		t.Errorf("文件名长度应为 %d，实际 %d", randomNameBytes*2+4, len(name))
	}

	other, _, err := newObjectKey(at, ".png")
	if err != nil {
		t.Fatalf("生成对象键失败: %v", err)
	}
	if other == key {
		t.Error("两次生成的对象键不应相同")
	}
}

func TestVariantKey(t *testing.T) {
	t.Parallel()

	cases := []struct{ key, variant, ext, want string }{
		{"2026/09/abc.png", "thumb", ".webp", "2026/09/abc-thumb.webp"},
		{"2026/09/abc.tar.gz", "medium", ".webp", "2026/09/abc.tar-medium.webp"},
		{"2026/09/noext", "large", ".webp", "2026/09/noext-large.webp"},
	}
	for _, c := range cases {
		if got := variantKey(c.key, c.variant, c.ext); got != c.want {
			t.Errorf("variantKey(%q) = %q，期望 %q", c.key, got, c.want)
		}
	}
}

func TestLocalStorageRoundTrip(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "uploads")
	store, err := NewLocalStorage(root, UploadsURLPrefix)
	if err != nil {
		t.Fatalf("构造本地存储失败: %v", err)
	}
	if store.Driver() != DriverLocal {
		t.Errorf("驱动名应为 %s，实际 %s", DriverLocal, store.Driver())
	}

	ctx := t.Context()
	const key = "2026/09/hello.txt"
	payload := []byte("你好，Lumo")
	if putErr := store.Put(ctx, key, bytes.NewReader(payload), int64(len(payload)), "text/plain"); putErr != nil {
		t.Fatalf("写入失败: %v", putErr)
	}

	got, err := os.ReadFile(filepath.Join(root, "2026", "09", "hello.txt"))
	if err != nil {
		t.Fatalf("读取写入的文件失败: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("内容不一致：%q", got)
	}
	if want := UploadsURLPrefix + "/" + key; store.URL(key) != want {
		t.Errorf("URL 应为 %q，实际 %q", want, store.URL(key))
	}

	// 写入时不应留下临时文件。
	entries, err := os.ReadDir(filepath.Join(root, "2026", "09"))
	if err != nil {
		t.Fatalf("列目录失败: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("目录下应只有一个文件，实际 %d 个", len(entries))
	}

	if err := store.Delete(ctx, key); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "2026", "09", "hello.txt")); !os.IsNotExist(err) {
		t.Error("文件应已删除")
	}
	// 删除不存在的对象是幂等的。
	if err := store.Delete(ctx, key); err != nil {
		t.Errorf("重复删除应当成功: %v", err)
	}
}

// TestLocalStorageRefusesEscape 验证越界的键在写与删两条路径上都被挡住。
func TestLocalStorageRefusesEscape(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "uploads")
	store, err := NewLocalStorage(root, UploadsURLPrefix)
	if err != nil {
		t.Fatalf("构造本地存储失败: %v", err)
	}

	ctx := context.Background()
	for _, key := range []string{"../escape.txt", "2026/../../escape.txt", "/etc/passwd"} {
		if err := store.Put(ctx, key, strings.NewReader("x"), 1, "text/plain"); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("写入 %q 应被拒绝，实际 %v", key, err)
		}
		if err := store.Delete(ctx, key); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("删除 %q 应被拒绝，实际 %v", key, err)
		}
	}
	// 目录之外不应出现任何文件。
	if _, err := os.Stat(filepath.Join(filepath.Dir(root), "escape.txt")); !os.IsNotExist(err) {
		t.Error("存储目录之外不应被写入")
	}
}

func TestParseEndpoint(t *testing.T) {
	t.Parallel()

	cases := []struct {
		endpoint   string
		useSSL     bool
		wantHost   string
		wantSecure bool
		wantErr    bool
	}{
		{endpoint: "minio.internal:9000", useSSL: false, wantHost: "minio.internal:9000"},
		{endpoint: "minio.internal:9000", useSSL: true, wantHost: "minio.internal:9000", wantSecure: true},
		{endpoint: "https://s3.example.com", useSSL: false, wantHost: "s3.example.com", wantSecure: true},
		{endpoint: "http://s3.example.com/", useSSL: true, wantHost: "s3.example.com"},
		{endpoint: "", wantErr: true},
		{endpoint: "ftp://s3.example.com", wantErr: true},
	}
	for _, c := range cases {
		host, secure, err := parseEndpoint(c.endpoint, c.useSSL)
		if c.wantErr {
			if err == nil {
				t.Errorf("%q 应报错", c.endpoint)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q 不应报错: %v", c.endpoint, err)
			continue
		}
		if host != c.wantHost || secure != c.wantSecure {
			t.Errorf("parseEndpoint(%q, %v) = (%q, %v)，期望 (%q, %v)",
				c.endpoint, c.useSSL, host, secure, c.wantHost, c.wantSecure)
		}
	}
}

// TestS3Credentials 验证密钥的来源顺序与「缺一个就报错」。
func TestS3Credentials(t *testing.T) {
	t.Setenv(EnvS3AccessKey, "")
	t.Setenv(EnvS3SecretKey, "")

	if _, _, err := S3Credentials(); !errors.Is(err, ErrMissingS3Credentials) {
		t.Fatalf("缺少环境变量应返回 ErrMissingS3Credentials，实际 %v", err)
	}

	t.Setenv(EnvS3AccessKey, "AKIA_TEST")
	t.Setenv(EnvS3SecretKey, "secret_test")
	access, secret, err := S3Credentials()
	if err != nil {
		t.Fatalf("配置齐全时不应报错: %v", err)
	}
	if access != "AKIA_TEST" || secret != "secret_test" {
		t.Errorf("读到的密钥不正确：%q / %q", access, secret)
	}

	// 后台填了就以后台为准，环境变量只是兜底。
	stored := StorageSettings{S3AccessKey: "AKIA_STORED", S3SecretKey: "secret_stored"}
	if access, secret, err := stored.Credentials(); err != nil || access != "AKIA_STORED" || secret != "secret_stored" {
		t.Errorf("后台填的密钥应优先：%q / %q, err %v", access, secret, err)
	}

	// 只填一边是个配置错误。若这时去环境变量里凑另一边，拼出来的是一个
	// 谁也没配过的密钥对，报错还会指向「密钥不对」——真正的问题是少填了一格。
	half := StorageSettings{S3AccessKey: "AKIA_STORED"}
	if _, _, err := half.Credentials(); !errors.Is(err, ErrMissingS3Credentials) {
		t.Errorf("只填一个密钥时应报错，实际 %v", err)
	}
}

func TestS3StorageURL(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		cfg  S3Config
		want string
	}{
		{
			name: "路径寻址",
			cfg:  S3Config{Endpoint: "https://s3.example.com", Bucket: "assets", PathStyle: true},
			want: "https://s3.example.com/assets/2026/09/a.png",
		},
		{
			name: "虚拟主机寻址",
			cfg:  S3Config{Endpoint: "https://s3.example.com", Bucket: "assets"},
			want: "https://assets.s3.example.com/2026/09/a.png",
		},
		{
			name: "自定义公开前缀",
			cfg:  S3Config{Endpoint: "minio:9000", Bucket: "assets", PublicURL: "https://cdn.example.com/"},
			want: "https://cdn.example.com/2026/09/a.png",
		},
	}
	for _, c := range cases {
		store, err := NewS3Storage(c.cfg, "ak", "sk")
		if err != nil {
			t.Fatalf("%s：构造失败 %v", c.name, err)
		}
		if got := store.URL("2026/09/a.png"); got != c.want {
			t.Errorf("%s：URL = %q，期望 %q", c.name, got, c.want)
		}
	}
}

func TestSanitizeDisplayName(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		`C:\Users\kong\照片.png`:   "照片.png",
		"../../etc/passwd":       "passwd",
		"  报告.pdf  ":             "报告.pdf",
		"带\x00控制\x07符.txt":       "带控制符.txt",
		"":                       "未命名",
		strings.Repeat("字", 300): strings.Repeat("字", maxDisplayNameLength),
	}
	for input, want := range cases {
		if got := sanitizeDisplayName(input); got != want {
			t.Errorf("sanitizeDisplayName(%q) = %q，期望 %q", input, got, want)
		}
	}
}
