// Package install 实现首次安装向导：连接测试、安装执行与状态查询。
//
// 站点还没有数据库连接信息时 serve 进入安装模式，只挂本包的路由与 Console 静态资源，
// 其余一律不装配。安装成功后会把连接串写进工作目录的 install.json（见 config 包），
// 服务随即重新初始化一次，这一次带着数据库，就是正常的站点。
//
// 本包不认识模块清单，也不认识设置模块：迁移、初始管理员与站点设置由调用方经
// Provision 注入（见 cmd/lumo/install.go），安装包只负责编排顺序与守住安装的前置条件。
package install

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/FeiBaiKin/lumo/internal/config"
	"github.com/FeiBaiKin/lumo/internal/console"
	"github.com/FeiBaiKin/lumo/internal/database"
)

// 连接串的默认取值，与 config.example.yaml 的示例保持一致。
const (
	defaultPort    = 5432
	defaultSSLMode = "disable"
	probeTimeout   = 20 * time.Second
	stepLabelDB    = "连接数据库"
)

// ErrAlreadyInstalled 表示站点已经装好了，安装入口一律拒绝。
var ErrAlreadyInstalled = errors.New("站点已完成安装")

// BadRequestError 标记「按提示改输入就能解决」的错误。
//
// 处理器靠它把这些消息原样回给向导（400），而把其余错误当内部故障处理（500，
// 细节只进日志）——数据库口令写错和磁盘写不进去，对站长是两件完全不同的事。
type BadRequestError struct{ err error }

func (e *BadRequestError) Error() string { return e.err.Error() }
func (e *BadRequestError) Unwrap() error { return e.err }

// badRequest 构造一个可展示给站长的错误。
func badRequest(format string, args ...any) error {
	return &BadRequestError{err: fmt.Errorf(format, args...)}
}

// WrapBadRequest 把调用方（注入的 Provision）返回的错误标记成站长可自行修正的，
// 处理器据此原样回显而不是当成内部故障。
//
// 安装阶段的失败几乎都属于这一类：数据库权限不足、参数写错、口令不合规。
func WrapBadRequest(err error) error {
	if err == nil {
		return nil
	}
	return &BadRequestError{err: err}
}

// DatabaseInput 是数据库连接信息。填了 DSN 就忽略逐项字段。
type DatabaseInput struct {
	DSN      string `json:"dsn,omitempty" doc:"完整连接串；填了就忽略下面各项"`
	Host     string `json:"host,omitempty" doc:"主机地址，例如 127.0.0.1"`
	Port     int    `json:"port,omitempty" doc:"端口，默认 5432" minimum:"1" maximum:"65535"`
	User     string `json:"user,omitempty" doc:"用户名"`
	Password string `json:"password,omitempty" doc:"口令"`
	Name     string `json:"name,omitempty" doc:"数据库名"`
	SSLMode  string `json:"sslMode,omitempty" doc:"TLS 模式" enum:"disable,prefer,require,verify-ca,verify-full"`
}

// SiteInput 是站点信息。
type SiteInput struct {
	Title string `json:"title,omitempty" maxLength:"100" doc:"站点名称"`
	URL   string `json:"url,omitempty" maxLength:"255" doc:"对外访问地址，例如 https://example.com"`
}

// AdminInput 是初始管理员账号。
//
// 用户名与口令的规则照抄数据库层的约束（migrations/00002_auth.sql 的 CHECK 与
// auth/password 的长度限制）：向导早一步拦住，比让站长在最后一步收到一条
// 「违反约束 users_username_format」要好得多。
type AdminInput struct {
	Username string `json:"username" minLength:"2" maxLength:"64" pattern:"^[a-z0-9]([-a-z0-9]*[a-z0-9])?$" doc:"小写字母、数字与连字符"`
	Email    string `json:"email" format:"email" maxLength:"254"`
	Password string `json:"password" minLength:"8" maxLength:"128"`
}

// Params 是交给调用方执行安装所需的全部输入。
type Params struct {
	DSN   string
	Site  SiteInput
	Admin AdminInput
}

// ProvisionResult 是安装执行的结果。
type ProvisionResult struct {
	// AdminCreated 为假表示目标库里已有用户，故未创建新的管理员。
	AdminCreated bool
}

// ProvisionFunc 在目标库上完成安装的实质工作：迁移、内置角色、初始管理员、站点设置。
//
// 由 cmd/lumo 注入：它拿得到模块清单与设置模块，而本包刻意不认识它们。
type ProvisionFunc func(ctx context.Context, db *database.DB, cfg config.Config, in Params) (ProvisionResult, error)

// Options 是构造 Service 的依赖。
type Options struct {
	// Config 是本次启动的配置（此时必然不含可用 DSN）。
	Config config.Config
	// DataDir 是已初始化的工作目录绝对路径。
	DataDir string
	Logger  *slog.Logger
	Version string
	// Provision 由调用方注入。
	Provision ProvisionFunc
}

// Service 是安装向导的服务端逻辑。
type Service struct {
	cfg       config.Config
	dataDir   string
	logger    *slog.Logger
	version   string
	provision ProvisionFunc

	mu        sync.Mutex
	installed bool
	done      chan struct{}
	once      sync.Once
}

// New 构造 Service。
func New(opts Options) *Service {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		cfg:       opts.Config,
		dataDir:   opts.DataDir,
		logger:    logger,
		version:   opts.Version,
		provision: opts.Provision,
		done:      make(chan struct{}),
	}
}

// Done 在安装成功时关闭，供 serve 据此重启一轮。
func (s *Service) Done() <-chan struct{} { return s.done }

// Installed 报告本次进程内安装是否已经成功。
func (s *Service) Installed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.installed
}

// Check 是一项安装前置检查的结果。
type Check struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

// StatusView 是安装状态。
type StatusView struct {
	// Installed 为真时向导应当自行退出：要么跳后台，要么按提示手动配置。
	Installed bool    `json:"installed"`
	Version   string  `json:"version"`
	DataDir   string  `json:"dataDir"`
	Checks    []Check `json:"checks"`
}

// Status 汇总当前是否可安装与前置检查结果。
func (s *Service) Status() StatusView {
	return StatusView{
		Installed: s.unavailable(),
		Version:   s.version,
		DataDir:   s.dataDir,
		Checks:    s.checks(),
	}
}

// DatabaseInfo 是测试连接的结果。
type DatabaseInfo struct {
	ServerVersion string `json:"serverVersion"`
	Database      string `json:"database"`
	User          string `json:"user"`
	Encoding      string `json:"encoding"`
	LatencyMS     int64  `json:"latencyMs"`
	// ExistingUsers 是目标库里已有的用户数；-1 表示该库还没有 Lumo 的数据表。
	// 大于 0 时向导要提醒站长：继续下去是接管一个已有站点，不是新建。
	ExistingUsers int64 `json:"existingUsers"`
}

// TestDatabase 试连目标库并回报它的身份。
func (s *Service) TestDatabase(ctx context.Context, in DatabaseInput) (*DatabaseInfo, error) {
	if err := s.ensureAvailable(); err != nil {
		return nil, err
	}
	dsn, err := buildDSN(in)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	started := time.Now()
	db, err := openDB(ctx, s.cfg.Database, dsn)
	if err != nil {
		// 连接失败的原因（口令错、库不存在、白名单没放行）站长自己能修，故可展示。
		return nil, badRequest("%s", err)
	}
	defer func() { _ = db.Close() }()

	info, err := probeInfo(ctx, db)
	if err != nil {
		return nil, fmt.Errorf("读取数据库信息: %w", err)
	}
	info.LatencyMS = time.Since(started).Milliseconds()
	return info, nil
}

// Step 是安装过程中的一个阶段及其耗时。
type Step struct {
	Key        string `json:"key"`
	Label      string `json:"label"`
	DurationMS int64  `json:"durationMs"`
	Detail     string `json:"detail,omitempty"`
}

// ApplyResult 是安装执行的结果。
type ApplyResult struct {
	Steps []Step `json:"steps"`
	// ExternalURL 是站长填的对外地址，可能为空。
	ExternalURL string `json:"externalUrl,omitempty"`
	// AdminCreated 为假表示库里已有用户，未创建新的管理员。
	AdminCreated bool `json:"adminCreated"`
}

// ApplyInput 是执行安装所需的全部输入。
type ApplyInput struct {
	Database DatabaseInput `json:"database"`
	Site     SiteInput     `json:"site"`
	Admin    AdminInput    `json:"admin"`
}

// Apply 执行安装：连库、迁移、建管理员、写站点设置，最后落盘安装产物。
//
// 顺序是有意的：安装产物写在最后一步。中途任何一步失败都不会留下这个文件，
// 于是下次启动仍然进入安装模式，站长可以改完参数重来。
func (s *Service) Apply(ctx context.Context, in ApplyInput) (*ApplyResult, error) {
	if err := s.ensureAvailable(); err != nil {
		return nil, err
	}
	dsn, err := buildDSN(in.Database)
	if err != nil {
		return nil, err
	}

	// 期限必须短于 server.WriteTimeout（默认 60s）：超时再长也留不住连接，
	// 站长看到的会变成「网络错误」而不是安装失败的原因。
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	steps := make([]Step, 0, 2)
	result := &ApplyResult{ExternalURL: strings.TrimSpace(in.Site.URL)}

	// 阶段一：连接数据库。这一步同时也是对连接串的最后一次校验。
	started := time.Now()
	db, err := openDB(ctx, s.cfg.Database, dsn)
	if err != nil {
		return nil, badRequest("%s", err)
	}
	defer func() { _ = db.Close() }()
	steps = append(steps, Step{
		Key:        "connect",
		Label:      stepLabelDB,
		DurationMS: time.Since(started).Milliseconds(),
		Detail:     config.RedactDSN(dsn),
	})

	// 阶段二：迁移、播种角色、建管理员、写站点设置。实现见 cmd/lumo/install.go。
	//
	// 传给它的配置必须带上刚连通的这条连接串：迁移会另开一个连接池去持有 advisory lock，
	// 而那个池是从配置里取 DSN 的 —— 漏这一步，它会去连本机默认库，报一条与真实原因
	// 毫无关系的「用户 xxx 认证失败」。
	provisionCfg := s.cfg
	provisionCfg.Database.DSN = dsn

	started = time.Now()
	provisioned, err := s.provision(ctx, db, provisionCfg, Params{
		DSN:   dsn,
		Site:  in.Site,
		Admin: in.Admin,
	})
	if err != nil {
		s.logger.Error("安装执行失败", slog.Any("error", err))
		return nil, err
	}
	result.AdminCreated = provisioned.AdminCreated
	steps = append(steps, Step{
		Key:        "provision",
		Label:      "初始化数据库与管理员",
		DurationMS: time.Since(started).Milliseconds(),
	})

	// 阶段三：落盘安装产物，此后向导关闭。
	started = time.Now()
	state := &config.InstallState{
		Installed:   true,
		InstalledAt: time.Now().UTC(),
		Version:     s.version,
		Database:    config.InstallDatabase{DSN: dsn},
		Site:        config.InstallSite{Title: in.Site.Title, URL: in.Site.URL},
	}
	if err := config.SaveInstallState(s.dataDir, state); err != nil {
		return nil, fmt.Errorf("保存安装信息: %w", err)
	}
	steps = append(steps, Step{
		Key:        "save",
		Label:      "保存安装信息",
		DurationMS: time.Since(started).Milliseconds(),
		Detail:     filepath.Join(s.dataDir, config.InstallFileName),
	})

	result.Steps = steps
	s.markInstalled()
	s.logger.Info("安装完成",
		slog.String("dsn", config.RedactDSN(dsn)),
		slog.Bool("adminCreated", result.AdminCreated))
	return result, nil
}

// ensureAvailable 拦住已经装好的情形。
//
// 两条判据都要看：配置里有连接串说明这个站点已被运维配置（可能是环境变量，
// 也可能是上一轮安装的产物），而 installed 只覆盖本次进程内刚装完的窗口。
func (s *Service) ensureAvailable() error {
	if s.cfg.Database.DSN != "" {
		return ErrAlreadyInstalled
	}
	if s.Installed() {
		return ErrAlreadyInstalled
	}
	return nil
}

// unavailable 报告向导是否应当关闭。
func (s *Service) unavailable() bool {
	return s.cfg.Database.DSN != "" || s.Installed()
}

// markInstalled 置位并唤醒等待重启的 serve。
func (s *Service) markInstalled() {
	s.mu.Lock()
	s.installed = true
	s.mu.Unlock()
	s.once.Do(func() { close(s.done) })
}

// checks 跑一遍安装前置检查。
func (s *Service) checks() []Check {
	checks := make([]Check, 0, 2)

	// 工作目录必须真的能写：安装的最后一步要往里写 install.json，
	// 而「目录存在」与「能写」在以 root 启动、容器只读挂载等场景下是两回事。
	probe := filepath.Join(s.dataDir, ".install-write-test")
	err := os.WriteFile(probe, []byte("ok"), 0o600) //nolint:gosec // 探针文件，随即删除
	if err != nil {
		checks = append(checks, Check{
			Key: "datadir", Label: "工作目录可写", OK: false,
			Detail: fmt.Sprintf("%s：%v", s.dataDir, err),
		})
	} else {
		_ = os.Remove(probe)
		checks = append(checks, Check{
			Key: "datadir", Label: "工作目录可写", OK: true, Detail: s.dataDir,
		})
	}

	built := console.Built()
	detail := "前端产物已嵌入"
	if !built {
		detail = "前端未构建，请用 task console:build 重新编译后再安装"
	}
	checks = append(checks, Check{Key: "console", Label: "管理后台可用", OK: built, Detail: detail})

	return checks
}

// buildDSN 把表单拼成连接串。
//
// 走 url.URL 而不是字符串拼接：口令里出现 @ : / ? # 是常事，手工拼接会拼出
// 一条主机名解析错误的 DSN，而报错信息通常指向别处，极难排查。
func buildDSN(in DatabaseInput) (string, error) {
	if dsn := strings.TrimSpace(in.DSN); dsn != "" {
		if err := validateDSN(dsn); err != nil {
			return "", err
		}
		return dsn, nil
	}

	host := strings.TrimSpace(in.Host)
	if host == "" {
		return "", badRequest("请填写数据库主机地址")
	}
	user := strings.TrimSpace(in.User)
	if user == "" {
		return "", badRequest("请填写数据库用户名")
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return "", badRequest("请填写数据库名")
	}
	port := in.Port
	if port == 0 {
		port = defaultPort
	}
	mode := strings.TrimSpace(in.SSLMode)
	if mode == "" {
		mode = defaultSSLMode
	}

	u := &url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(user, in.Password),
		Host:     net.JoinHostPort(host, strconv.Itoa(port)),
		Path:     "/" + name,
		RawQuery: url.Values{"sslmode": {mode}}.Encode(),
	}
	dsn := u.String()
	if err := validateDSN(dsn); err != nil {
		return "", err
	}
	return dsn, nil
}

// validateDSN 复用配置包对连接串的校验（scheme、库名、可解析）。
func validateDSN(dsn string) error {
	probe := config.Default()
	probe.Database.DSN = dsn
	if err := probe.RequireDSN(); err != nil {
		return badRequest("%s", err)
	}
	return nil
}

// openDB 在给定 DSN 上开一条连接并确认可用。
//
// 向导期间只借用一条连接：这里不存在并发，建池只会让被拒的连接多留几份。
func openDB(ctx context.Context, cfg config.DatabaseConfig, dsn string) (*database.DB, error) {
	probe := cfg
	probe.DSN = dsn
	probe.MaxOpenConns = 1
	probe.MaxIdleConns = 1
	return database.Open(ctx, probe, false)
}

// probeInfo 读取目标库的身份与已有数据情况。
func probeInfo(ctx context.Context, db *database.DB) (*DatabaseInfo, error) {
	var info DatabaseInfo
	row := db.SQLDB().QueryRowContext(ctx,
		`select version(), current_database(), current_user, current_setting('server_encoding')`)
	if err := row.Scan(&info.ServerVersion, &info.Database, &info.User, &info.Encoding); err != nil {
		return nil, err
	}

	// 已有 users 表说明这个库不是空的：迁移可以重复跑，但初始管理员不会再建，
	// 站长需要知道自己正在接管一个已有站点。没有该表则记 -1。
	var exists bool
	if err := db.SQLDB().QueryRowContext(ctx,
		`select to_regclass('public.users') is not null`).Scan(&exists); err != nil {
		return nil, err
	}
	info.ExistingUsers = -1
	if exists {
		if err := db.SQLDB().QueryRowContext(ctx, `select count(*) from users`).Scan(&info.ExistingUsers); err != nil {
			return nil, err
		}
	}
	return &info, nil
}
