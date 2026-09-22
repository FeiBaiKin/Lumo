// Package config 负责加载与校验运行配置。
//
// 优先级（后者覆盖前者）：内置默认值 → config.yaml → LUMO_* 环境变量。
// 数据库 DSN 只接受环境变量 LUMO_DATABASE_DSN，口令不得写入配置文件。
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// EnvPrefix 是所有环境变量覆盖项的统一前缀。
const EnvPrefix = "LUMO_"

// DefaultUpdateRepo 是在线升级的默认更新源，即本项目的官方仓库。
const DefaultUpdateRepo = "FeiBaiKin/Lumo"

// Config 是全部运行配置的根。
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	Log      LogConfig      `yaml:"log"`
	Update   UpdateConfig   `yaml:"update"`
	// DataDir 是运行时工作目录，内含 themes/uploads/cache/logs/backups。
	DataDir string `yaml:"dataDir"`
}

// ServerConfig 是 HTTP 服务配置。
type ServerConfig struct {
	Addr string `yaml:"addr"`
	// ExternalURL 是站点对外可访问地址，用于生成 canonical、sitemap 等绝对链接。
	ExternalURL       string        `yaml:"externalUrl"`
	ReadHeaderTimeout time.Duration `yaml:"readHeaderTimeout"`
	WriteTimeout      time.Duration `yaml:"writeTimeout"`
	IdleTimeout       time.Duration `yaml:"idleTimeout"`
	ShutdownTimeout   time.Duration `yaml:"shutdownTimeout"`
	// TrustedProxies 是可信反向代理的 CIDR 列表。
	//
	// 只有当直连对端属于这些网段时，X-Forwarded-For 才被采信。
	// 留空表示不信任任何代理，一律使用直连地址——这是最安全的默认值。
	TrustedProxies []string `yaml:"trustedProxies"`
	// SecureCookies 决定会话 Cookie 是否带 Secure 属性并启用 __Host- 前缀。
	// 生产环境（HTTPS）必须为 true；本地 HTTP 开发需为 false，否则浏览器不接受 Cookie。
	SecureCookies bool `yaml:"secureCookies"`
	// MaxBodySize 是普通请求体的字节上限。
	MaxBodySize int64 `yaml:"maxBodySize"`
	// MaxUploadSize 是 multipart 请求（附件上传）的字节上限。
	//
	// 与 MaxBodySize 分开是因为二者的取舍相反：JSON 接口越小越好，
	// 而附件动辄几十兆，用同一个值要么挡住正常上传，要么给所有接口放开口子。
	MaxUploadSize int64 `yaml:"maxUploadSize"`
}

// DatabaseConfig 是数据库连接配置。
//
// DSN 故意不带 yaml 标签：它只能来自环境变量，避免口令进入入库文件。
type DatabaseConfig struct {
	DSN             string        `yaml:"-"`
	MaxOpenConns    int           `yaml:"maxOpenConns"`
	MaxIdleConns    int           `yaml:"maxIdleConns"`
	ConnMaxLifetime time.Duration `yaml:"connMaxLifetime"`
	// MigrationLockTimeout 是等待迁移 advisory lock 的上限。
	//
	// 多个实例同时启动时，未拿到锁的实例必须等待而不是跳过迁移；但等待不能
	// 无限期（数据库黑洞、残留会话等都会让它永久挂起），超时即报错退出。
	MigrationLockTimeout time.Duration `yaml:"migrationLockTimeout"`
	// AutoMigrate 控制启动时是否自动执行迁移，可被 --no-migrate 关闭。
	AutoMigrate bool `yaml:"autoMigrate"`
}

// LogConfig 是日志配置。
type LogConfig struct {
	// Level 取值 debug / info / warn / error。
	Level string `yaml:"level"`
	// Format 取值 text / json。**只作用于控制台**：写进文件的那一路一律是 JSON，
	// 后台日志页要把每行解析成结构化字段。
	Format string `yaml:"format"`
	// File 为真时在控制台之外再写一份到 dataDir/logs，后台日志页读的就是它。
	// 关掉它日志页会是空的，但 docker logs 与 journalctl 不受影响。
	File bool `yaml:"file"`
	// RetainDays 是日志文件保留天数，超期的在跨天时删除。
	RetainDays int `yaml:"retainDays"`
	// MaxSizeMB 是单个日志文件的大小上限，超过则在同一天内滚到下一个序号。
	MaxSizeMB int `yaml:"maxSizeMB"`
}

// UpdateConfig 是在线升级配置。
//
// 升级是运维动作而非站点设置：它取决于「这份二进制装在哪、谁能写它」，
// 换台机器部署结论就变了，故放在配置文件与环境变量里，而不是入库的设置表。
type UpdateConfig struct {
	// Enabled 为假时整个功能关闭：不做后台检查，接口一律 409。
	// 由发行方统一升级的部署（容器编排、包管理器）应当关掉它。
	Enabled bool `yaml:"enabled"`
	// AutoCheck 控制是否在后台定期检查新版本。关掉它仍可在后台手动检查。
	AutoCheck bool `yaml:"autoCheck"`
	// CheckInterval 是后台检查的间隔。
	//
	// 下限 1 小时：GitHub 对未认证请求按 IP 限 60 次/小时，
	// 同一出口下的多个站点把这个值调小会一起被限流。
	CheckInterval time.Duration `yaml:"checkInterval"`
	// Prerelease 为真时把预发布版本也算作可升级目标。
	Prerelease bool `yaml:"prerelease"`
	// Repo 是更新源仓库，形如 owner/name。
	//
	// 可配置是为了让 fork 与内网镜像能自管升级；默认指向官方仓库。
	// 改它等于改「这台机器信任谁发布的二进制」，故只接受配置文件与环境变量。
	Repo string `yaml:"repo"`
	// APIBase 是更新源的 API 根地址，留空即 GitHub 公有云。
	//
	// 只走环境变量 LUMO_UPDATE_API_BASE，与 Repo 是同一件事的两半：
	// 指向自建镜像或 GitHub Enterprise 的部署要改它。不写进配置文件是因为
	// 它决定「这台机器去哪里取二进制」，属于部署环境而非站点配置。
	APIBase string `yaml:"-"`
	// Token 是访问 GitHub API 的令牌，可留空。
	//
	// 只走环境变量 LUMO_UPDATE_TOKEN，理由同 DSN：凭据不写进入库文件。
	// 用途是抬高限流阈值或访问私有仓库，公开仓库不需要它。
	Token string `yaml:"-"`
	// KeepBackups 是升级成功后保留的旧版本备份份数，0 表示不自动清理。
	KeepBackups int `yaml:"keepBackups"`
}

// Default 返回内置默认配置。
func Default() Config {
	return Config{
		Server: ServerConfig{
			Addr:              ":8080",
			ReadHeaderTimeout: 10 * time.Second,
			WriteTimeout:      60 * time.Second,
			IdleTimeout:       60 * time.Second,
			ShutdownTimeout:   15 * time.Second,
			MaxBodySize:       10 << 20,
			MaxUploadSize:     64 << 20,
		},
		Database: DatabaseConfig{
			MaxOpenConns:         25,
			MaxIdleConns:         5,
			ConnMaxLifetime:      time.Hour,
			MigrationLockTimeout: 2 * time.Minute,
			AutoMigrate:          true,
		},
		Log: LogConfig{
			Level:      "info",
			Format:     "text",
			File:       true,
			RetainDays: 14,
			MaxSizeMB:  100,
		},
		Update: UpdateConfig{
			Enabled:       true,
			AutoCheck:     true,
			CheckInterval: 24 * time.Hour,
			Prerelease:    false,
			Repo:          DefaultUpdateRepo,
			KeepBackups:   3,
		},
		DataDir: "./data",
	}
}

// Load 按「默认值 → 配置文件 → 环境变量 → 安装向导产物」的顺序装配配置并校验。
//
// path 为空时按约定依次尝试 ./config.yaml、./config.yml；文件不存在不算错误，
// 因为「仅靠环境变量运行」是容器部署的常见形态。
func Load(path string) (Config, error) {
	cfg := Default()

	file, err := resolveConfigFile(path)
	if err != nil {
		return Config{}, err
	}
	if file != "" {
		if err := applyFile(&cfg, file); err != nil {
			return Config{}, err
		}
	}

	if err := applyEnv(&cfg, os.Getenv); err != nil {
		return Config{}, err
	}
	// 安装产物最后读：要先知道 LUMO_DATA_DIR 才找得到那个文件，
	// 而它只补环境变量没给的值（见 applyInstallState）。
	if err := applyInstallState(&cfg); err != nil {
		return Config{}, err
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// resolveConfigFile 决定实际使用的配置文件路径。
// 显式指定但不存在时报错；未指定时静默回退到「无配置文件」。
func resolveConfigFile(path string) (string, error) {
	if path != "" {
		if _, err := os.Stat(path); err != nil {
			return "", fmt.Errorf("读取配置文件 %s: %w", path, err)
		}
		return path, nil
	}
	for _, candidate := range []string{"config.yaml", "config.yml"} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", nil
}

// applyFile 把 YAML 文件内容合并进 cfg。
func applyFile(cfg *Config, path string) error {
	data, err := os.ReadFile(path) //nolint:gosec // 路径由部署者显式提供
	if err != nil {
		return fmt.Errorf("读取配置文件 %s: %w", path, err)
	}
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true) // 拼错的键要报错，而不是被静默忽略
	if err := decoder.Decode(cfg); err != nil {
		return fmt.Errorf("解析配置文件 %s: %w", path, err)
	}
	return nil
}

// getenv 抽象环境变量读取，便于测试注入。
type getenv func(string) string

// applyEnv 用 LUMO_* 环境变量覆盖配置。
func applyEnv(cfg *Config, env getenv) error {
	strs := map[string]*string{
		"ADDR":            &cfg.Server.Addr,
		"EXTERNAL_URL":    &cfg.Server.ExternalURL,
		"DATABASE_DSN":    &cfg.Database.DSN,
		"LOG_LEVEL":       &cfg.Log.Level,
		"LOG_FORMAT":      &cfg.Log.Format,
		"DATA_DIR":        &cfg.DataDir,
		"UPDATE_REPO":     &cfg.Update.Repo,
		"UPDATE_TOKEN":    &cfg.Update.Token,
		"UPDATE_API_BASE": &cfg.Update.APIBase,
	}
	for key, target := range strs {
		if v := env(EnvPrefix + key); v != "" {
			*target = v
		}
	}

	ints := map[string]*int{
		"DATABASE_MAX_OPEN_CONNS": &cfg.Database.MaxOpenConns,
		"DATABASE_MAX_IDLE_CONNS": &cfg.Database.MaxIdleConns,
		"LOG_RETAIN_DAYS":         &cfg.Log.RetainDays,
		"LOG_MAX_SIZE_MB":         &cfg.Log.MaxSizeMB,
		"UPDATE_KEEP_BACKUPS":     &cfg.Update.KeepBackups,
	}
	for key, target := range ints {
		v := env(EnvPrefix + key)
		if v == "" {
			continue
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("环境变量 %s%s 不是整数: %q", EnvPrefix, key, v)
		}
		*target = n
	}

	int64s := map[string]*int64{
		"MAX_BODY_SIZE":   &cfg.Server.MaxBodySize,
		"MAX_UPLOAD_SIZE": &cfg.Server.MaxUploadSize,
	}
	for key, target := range int64s {
		v := env(EnvPrefix + key)
		if v == "" {
			continue
		}
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return fmt.Errorf("环境变量 %s%s 不是整数: %q", EnvPrefix, key, v)
		}
		*target = n
	}

	durations := map[string]*time.Duration{
		"DATABASE_CONN_MAX_LIFETIME":      &cfg.Database.ConnMaxLifetime,
		"DATABASE_MIGRATION_LOCK_TIMEOUT": &cfg.Database.MigrationLockTimeout,
		"SHUTDOWN_TIMEOUT":                &cfg.Server.ShutdownTimeout,
		"UPDATE_CHECK_INTERVAL":           &cfg.Update.CheckInterval,
	}
	for key, target := range durations {
		v := env(EnvPrefix + key)
		if v == "" {
			continue
		}
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("环境变量 %s%s 不是合法时长: %q", EnvPrefix, key, v)
		}
		*target = d
	}

	bools := map[string]*bool{
		"DATABASE_AUTO_MIGRATE": &cfg.Database.AutoMigrate,
		"SECURE_COOKIES":        &cfg.Server.SecureCookies,
		"LOG_FILE":              &cfg.Log.File,
		"UPDATE_ENABLED":        &cfg.Update.Enabled,
		"UPDATE_AUTO_CHECK":     &cfg.Update.AutoCheck,
		"UPDATE_PRERELEASE":     &cfg.Update.Prerelease,
	}
	for key, target := range bools {
		v := env(EnvPrefix + key)
		if v == "" {
			continue
		}
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("环境变量 %s%s 不是布尔值: %q", EnvPrefix, key, v)
		}
		*target = b
	}

	// 逗号分隔的 CIDR 列表。
	if v := env(EnvPrefix + "TRUSTED_PROXIES"); v != "" {
		parts := strings.Split(v, ",")
		proxies := make([]string, 0, len(parts))
		for _, part := range parts {
			if trimmed := strings.TrimSpace(part); trimmed != "" {
				proxies = append(proxies, trimmed)
			}
		}
		cfg.Server.TrustedProxies = proxies
	}
	return nil
}

// 合法的日志级别与格式。
var (
	validLevels  = map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	validFormats = map[string]bool{"text": true, "json": true}
)

// Validate 校验配置的自洽性。
func (c *Config) Validate() error {
	var errs []error

	if c.Server.Addr == "" {
		errs = append(errs, errors.New("server.addr 不能为空"))
	}
	if c.Server.ExternalURL != "" {
		u, err := url.Parse(c.Server.ExternalURL)
		if err != nil || u.Scheme == "" || u.Host == "" {
			errs = append(errs, fmt.Errorf("server.externalUrl 不是合法的绝对 URL: %q", c.Server.ExternalURL))
		}
	}
	if c.Log.File {
		if c.Log.RetainDays < 1 {
			errs = append(errs, fmt.Errorf("log.retainDays 至少为 1，实际 %d", c.Log.RetainDays))
		}
		if c.Log.MaxSizeMB < 1 {
			errs = append(errs, fmt.Errorf("log.maxSizeMB 至少为 1，实际 %d", c.Log.MaxSizeMB))
		}
	}
	if !validLevels[strings.ToLower(c.Log.Level)] {
		errs = append(errs, fmt.Errorf("log.level 必须是 debug/info/warn/error，实际 %q", c.Log.Level))
	}
	if !validFormats[strings.ToLower(c.Log.Format)] {
		errs = append(errs, fmt.Errorf("log.format 必须是 text/json，实际 %q", c.Log.Format))
	}
	if c.DataDir == "" {
		errs = append(errs, errors.New("dataDir 不能为空"))
	}
	if c.Server.MaxBodySize <= 0 {
		errs = append(errs, fmt.Errorf("server.maxBodySize 必须为正数，实际 %d", c.Server.MaxBodySize))
	}
	if c.Server.MaxUploadSize <= 0 {
		errs = append(errs, fmt.Errorf("server.maxUploadSize 必须为正数，实际 %d", c.Server.MaxUploadSize))
	}
	if c.Database.MaxOpenConns <= 0 {
		errs = append(errs, fmt.Errorf("database.maxOpenConns 必须为正数，实际 %d", c.Database.MaxOpenConns))
	}
	if c.Database.MaxIdleConns < 0 {
		errs = append(errs, fmt.Errorf("database.maxIdleConns 不能为负数，实际 %d", c.Database.MaxIdleConns))
	}
	if c.Database.MaxIdleConns > c.Database.MaxOpenConns {
		errs = append(errs, fmt.Errorf("database.maxIdleConns(%d) 不能大于 maxOpenConns(%d)",
			c.Database.MaxIdleConns, c.Database.MaxOpenConns))
	}
	if c.Update.Enabled {
		errs = append(errs, c.validateUpdate()...)
	}
	if c.Database.MigrationLockTimeout <= 0 {
		errs = append(errs, fmt.Errorf("database.migrationLockTimeout 必须为正数，实际 %v",
			c.Database.MigrationLockTimeout))
	}

	return errors.Join(errs...)
}

// updateRepoPattern 是 owner/name 的形状，与 GitHub 的命名规则同宽。
var updateRepoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// minUpdateCheckInterval 是后台检查间隔的下限，理由见 UpdateConfig.CheckInterval。
const minUpdateCheckInterval = time.Hour

// validateUpdate 校验在线升级配置。功能关闭时不校验：
// 关着的功能不该因为一个没人用的字段写错而挡住整个进程启动。
func (c *Config) validateUpdate() []error {
	var errs []error
	if !updateRepoPattern.MatchString(c.Update.Repo) {
		errs = append(errs, fmt.Errorf("update.repo 必须形如 owner/name，实际 %q", c.Update.Repo))
	}
	if c.Update.AutoCheck && c.Update.CheckInterval < minUpdateCheckInterval {
		errs = append(errs, fmt.Errorf("update.checkInterval 至少为 %v，实际 %v",
			minUpdateCheckInterval, c.Update.CheckInterval))
	}
	if c.Update.APIBase != "" {
		u, err := url.Parse(c.Update.APIBase)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			errs = append(errs, fmt.Errorf("%sUPDATE_API_BASE 不是合法的绝对 URL: %q",
				EnvPrefix, c.Update.APIBase))
		}
	}
	if c.Update.KeepBackups < 0 {
		errs = append(errs, fmt.Errorf("update.keepBackups 不能为负数，实际 %d", c.Update.KeepBackups))
	}
	return errs
}

// RequireDSN 在需要数据库的场景校验 DSN 已配置且形态正确。
//
// 单独成方法而非并入 Validate：serve 在未配置数据库时也应能启动到给出明确指引，
// 而 migrate 这类命令必须强制要求 DSN。
func (c *Config) RequireDSN() error {
	if strings.TrimSpace(c.Database.DSN) == "" {
		return fmt.Errorf("未配置数据库连接，请设置环境变量 %sDATABASE_DSN", EnvPrefix)
	}
	u, err := url.Parse(c.Database.DSN)
	if err != nil {
		return fmt.Errorf("%sDATABASE_DSN 不是合法的连接串: %w", EnvPrefix, err)
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return fmt.Errorf("%sDATABASE_DSN 的 scheme 必须是 postgres:// 或 postgresql://，实际 %q",
			EnvPrefix, u.Scheme)
	}
	if strings.TrimPrefix(u.Path, "/") == "" {
		return fmt.Errorf("%sDATABASE_DSN 缺少数据库名", EnvPrefix)
	}
	return nil
}

// DataSubdir 返回工作目录下某个子目录的路径。
func (c *Config) DataSubdir(name string) string {
	return filepath.Join(c.DataDir, name)
}

// RedactedDSN 返回去掉口令的 DSN，供日志输出使用。
// 无法解析时返回固定占位串，绝不回落到原始 DSN，以免口令泄漏进日志。
func (c *Config) RedactedDSN() string {
	return RedactDSN(c.Database.DSN)
}

// RedactDSN 脱敏任意连接串。
//
// 与 Config.RedactedDSN 同源：安装向导手里只有一条待验证的候选连接串，
// 还没有落成配置，但同样不能把口令写进日志与接口响应。
func RedactDSN(dsn string) string {
	if dsn == "" {
		return ""
	}
	u, err := url.Parse(dsn)
	if err != nil {
		return "(无法解析的 DSN)"
	}
	if u.User != nil {
		if _, hasPassword := u.User.Password(); hasPassword {
			u.User = url.UserPassword(u.User.Username(), "xxxxx")
		}
	}
	return u.Redacted()
}
