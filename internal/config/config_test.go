package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultIsValid(t *testing.T) {
	t.Parallel()

	cfg := Default()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("默认配置应当合法，实际错误: %v", err)
	}
}

func TestApplyEnv(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		"LUMO_ADDR":                       ":9000",
		"LUMO_LOG_LEVEL":                  "debug",
		"LUMO_LOG_FORMAT":                 "json",
		"LUMO_DATA_DIR":                   "/srv/lumo",
		"LUMO_DATABASE_DSN":               "postgres://u:p@h:5432/db",
		"LUMO_DATABASE_MAX_OPEN_CONNS":    "50",
		"LUMO_DATABASE_CONN_MAX_LIFETIME": "30m",
		"LUMO_DATABASE_AUTO_MIGRATE":      "false",
		"LUMO_MAX_BODY_SIZE":              "2097152",
		"LUMO_MAX_UPLOAD_SIZE":            "134217728",
	}

	cfg := Default()
	if err := applyEnv(&cfg, func(k string) string { return env[k] }); err != nil {
		t.Fatalf("applyEnv 返回错误: %v", err)
	}

	if cfg.Server.Addr != ":9000" {
		t.Errorf("Addr = %q，期望 :9000", cfg.Server.Addr)
	}
	if cfg.Log.Level != "debug" || cfg.Log.Format != "json" {
		t.Errorf("日志配置未生效: %+v", cfg.Log)
	}
	if cfg.DataDir != "/srv/lumo" {
		t.Errorf("DataDir = %q", cfg.DataDir)
	}
	if cfg.Database.MaxOpenConns != 50 {
		t.Errorf("MaxOpenConns = %d，期望 50", cfg.Database.MaxOpenConns)
	}
	if cfg.Database.ConnMaxLifetime != 30*time.Minute {
		t.Errorf("ConnMaxLifetime = %v，期望 30m", cfg.Database.ConnMaxLifetime)
	}
	if cfg.Database.AutoMigrate {
		t.Error("AutoMigrate 应被覆盖为 false")
	}
	if cfg.Server.MaxBodySize != 2<<20 {
		t.Errorf("MaxBodySize = %d，期望 %d", cfg.Server.MaxBodySize, 2<<20)
	}
	if cfg.Server.MaxUploadSize != 128<<20 {
		t.Errorf("MaxUploadSize = %d，期望 %d", cfg.Server.MaxUploadSize, 128<<20)
	}
}

func TestApplyEnvRejectsBadValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		env  map[string]string
	}{
		{"整数非法", map[string]string{"LUMO_DATABASE_MAX_OPEN_CONNS": "abc"}},
		{"时长非法", map[string]string{"LUMO_DATABASE_CONN_MAX_LIFETIME": "5 weeks"}},
		{"布尔非法", map[string]string{"LUMO_DATABASE_AUTO_MIGRATE": "maybe"}},
		{"请求体上限非法", map[string]string{"LUMO_MAX_BODY_SIZE": "10MB"}},
		{"上传上限非法", map[string]string{"LUMO_MAX_UPLOAD_SIZE": "64MiB"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := Default()
			if err := applyEnv(&cfg, func(k string) string { return tt.env[k] }); err == nil {
				t.Fatal("期望返回错误，实际为 nil")
			}
		})
	}
}

func TestValidateCatchesBadConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*Config)
		want   string
	}{
		{"空监听地址", func(c *Config) { c.Server.Addr = "" }, "server.addr"},
		{"非法日志级别", func(c *Config) { c.Log.Level = "trace" }, "log.level"},
		{"非法日志格式", func(c *Config) { c.Log.Format = "xml" }, "log.format"},
		{"空工作目录", func(c *Config) { c.DataDir = "" }, "dataDir"},
		{"连接数非正", func(c *Config) { c.Database.MaxOpenConns = 0 }, "maxOpenConns"},
		{"空闲连接超过上限", func(c *Config) {
			c.Database.MaxOpenConns = 5
			c.Database.MaxIdleConns = 10
		}, "maxIdleConns"},
		{"非法外部 URL", func(c *Config) { c.Server.ExternalURL = "not-a-url" }, "externalUrl"},
		{"请求体上限非正", func(c *Config) { c.Server.MaxBodySize = 0 }, "maxBodySize"},
		{"上传上限非正", func(c *Config) { c.Server.MaxUploadSize = -1 }, "maxUploadSize"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := Default()
			tt.mutate(&cfg)
			err := cfg.Validate()
			if err == nil {
				t.Fatal("期望校验失败，实际通过")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("错误信息 %q 未包含 %q", err.Error(), tt.want)
			}
		})
	}
}

func TestRequireDSN(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		dsn     string
		wantErr bool
	}{
		{"合法 postgres", "postgres://u:p@127.0.0.1:5432/gocms_dev?sslmode=disable", false},
		{"合法 postgresql", "postgresql://u:p@127.0.0.1:5432/gocms_dev", false},
		{"空值", "", true},
		{"仅空白", "   ", true},
		{"错误 scheme", "mysql://u:p@127.0.0.1:3306/db", true},
		{"缺库名", "postgres://u:p@127.0.0.1:5432/", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := Default()
			cfg.Database.DSN = tt.dsn
			err := cfg.RequireDSN()
			if tt.wantErr && err == nil {
				t.Fatal("期望错误，实际为 nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("期望通过，实际错误: %v", err)
			}
		})
	}
}

// TestRedactedDSN 是安全相关测试：日志绝不能出现明文口令。
func TestRedactedDSN(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		dsn        string
		mustNotHas string
	}{
		{"带口令", "postgres://postgres:s3cr3t@127.0.0.1:5432/gocms_dev", "s3cr3t"},
		{"无口令", "postgres://postgres@127.0.0.1:5432/gocms_dev", ""},
		{"无法解析", "://bad url", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := Default()
			cfg.Database.DSN = tt.dsn
			got := cfg.RedactedDSN()
			if tt.mustNotHas != "" && strings.Contains(got, tt.mustNotHas) {
				t.Fatalf("脱敏后仍含口令: %q", got)
			}
		})
	}

	cfg := Default()
	if cfg.RedactedDSN() != "" {
		t.Error("空 DSN 应返回空串")
	}
}

func TestLoadFromFileAndEnvPrecedence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `server:
  addr: ":7000"
log:
  level: warn
  format: json
dataDir: ./custom-data
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("写入测试配置失败: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load 返回错误: %v", err)
	}
	if cfg.Server.Addr != ":7000" {
		t.Errorf("配置文件未生效，Addr = %q", cfg.Server.Addr)
	}
	if cfg.Log.Level != "warn" {
		t.Errorf("配置文件未生效，Level = %q", cfg.Log.Level)
	}

	// 环境变量必须覆盖配置文件。
	t.Setenv("LUMO_ADDR", ":7777")
	cfg, err = Load(path)
	if err != nil {
		t.Fatalf("Load 返回错误: %v", err)
	}
	if cfg.Server.Addr != ":7777" {
		t.Errorf("环境变量未覆盖配置文件，Addr = %q", cfg.Server.Addr)
	}
}

// TestLoadRejectsUnknownFields 保证配置项拼错时报错而不是被静默忽略。
func TestLoadRejectsUnknownFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("serverr:\n  addr: \":1\"\n"), 0o600); err != nil {
		t.Fatalf("写入测试配置失败: %v", err)
	}

	if _, err := Load(path); err == nil {
		t.Fatal("拼错的配置键应导致加载失败")
	}
}

func TestLoadMissingExplicitFileFails(t *testing.T) {
	t.Parallel()

	if _, err := Load(filepath.Join(t.TempDir(), "nope.yaml")); err == nil {
		t.Fatal("显式指定的配置文件不存在时应报错")
	}
}

func TestDataSubdir(t *testing.T) {
	t.Parallel()

	cfg := Default()
	cfg.DataDir = filepath.Join("var", "lumo")
	got := cfg.DataSubdir("uploads")
	want := filepath.Join("var", "lumo", "uploads")
	if got != want {
		t.Errorf("DataSubdir = %q，期望 %q", got, want)
	}
}
