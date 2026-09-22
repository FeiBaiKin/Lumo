package update

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/FeiBaiKin/lumo/internal/config"
	"github.com/FeiBaiKin/lumo/internal/version"
	"github.com/FeiBaiKin/lumo/internal/workdir"
)

// Phase 是升级任务所处的阶段。
type Phase string

// 升级任务的阶段取值。界面按它决定显示进度条、结果还是错误。
const (
	// PhaseIdle 表示没有正在进行的升级。
	PhaseIdle Phase = "idle"
	// PhaseDownloading 表示正在下载发布包，此时进度有意义。
	PhaseDownloading Phase = "downloading"
	// PhaseInstalling 表示正在校验与替换程序文件。
	PhaseInstalling Phase = "installing"
	// PhaseRestarting 表示新版本已就位，正在重启服务。
	PhaseRestarting Phase = "restarting"
	// PhaseReady 表示已装好但没能自动重启，需要人工重启。
	PhaseReady Phase = "ready"
	// PhaseFailed 表示这次升级失败，原因在 Error 里。
	PhaseFailed Phase = "failed"
)

// 任务与检查的时间参数。
const (
	// applyTimeout 是一次升级的总期限。几十 MB 的下载在慢网络上要几分钟，
	// 但拖到半小时还没完的多半是卡住了，该让它失败并把状态还回去。
	applyTimeout = 30 * time.Minute
	// initialCheckDelay 是启动后第一次自动检查的延迟。启动那几秒要留给迁移与
	// 播种，不跟它们抢；也避免反复重启的实例把更新源当成压测目标。
	initialCheckDelay = time.Minute
	// manualCheckThrottle 是两次检查之间的最小间隔。
	// 后台页面上「检查更新」是个按钮，按钮就会被连点。
	manualCheckThrottle = 10 * time.Second
	// updateCacheDir 是下载发布包的临时目录名，位于 data/cache 之下。
	updateCacheDir = "update"
)

// 升级流程的错误哨兵。
var (
	// ErrDisabled 表示在线升级功能被配置关闭。
	ErrDisabled = errors.New("在线升级已关闭")
	// ErrBusy 表示已有一个升级任务在进行。
	ErrBusy = errors.New("已有升级任务在进行")
	// ErrNotUpgradable 表示当前环境不支持就地升级。
	ErrNotUpgradable = errors.New("当前部署形态不支持就地升级")
	// ErrNoUpdate 表示没有可安装的新版本。
	ErrNoUpdate = errors.New("当前已是最新版本")
)

// UpdateProgress 是一次升级任务的实时状态。
type UpdateProgress struct {
	Phase Phase `json:"phase" doc:"idle / downloading / installing / restarting / ready / failed"`
	// Version 是这次要装的版本。
	Version string `json:"version,omitempty"`
	// Downloaded 与 Total 只在下载阶段有意义，Total 为 0 表示源没给出长度。
	Downloaded int64 `json:"downloaded" doc:"已下载字节数"`
	Total      int64 `json:"total" doc:"发布包总字节数，0 表示未知"`
	// Backup 是这次升级留下的备份文件名。
	Backup string `json:"backup,omitempty"`
	// Error 是失败原因，可直接显示。
	Error     string     `json:"error,omitempty"`
	StartedAt *time.Time `json:"startedAt,omitempty"`
	// UpdatedAt 是状态最后一次变化的时刻，界面用它判断任务是不是卡住了。
	UpdatedAt *time.Time `json:"updatedAt,omitempty"`
}

// BuildInfo 是当前运行的构建。
type BuildInfo struct {
	Version  string `json:"version"`
	Commit   string `json:"commit"`
	Date     string `json:"date"`
	Platform string `json:"platform"`
}

// UpdateStatus 是关于页要展示的全部信息，一个端点一次给全。
//
// 类型名带 Update 前缀而不是简单的 Status：OpenAPI 的组件名是全局命名空间，
// 一个叫 Status 的模式会先到先得地占住那个名字，让后来者被迫改名或加后缀。
//
// 合成一个响应而不是拆成「版本/进度/备份」三个端点：这一页会被轮询
// （升级过程中每秒一次），三个端点就是三倍的请求与三份可能互相矛盾的快照。
type UpdateStatus struct {
	Enabled bool      `json:"enabled" doc:"功能是否启用，关闭时其余字段仅供展示"`
	Current BuildInfo `json:"current"`
	// Latest 是查到的最新发布，尚未检查或检查失败时为 null。
	Latest *Release `json:"latest,omitempty"`
	// HasUpdate 为真表示 Latest 比当前版本新。
	HasUpdate bool `json:"hasUpdate"`
	// VersionComparable 为假表示当前版本号解析不出来（源码直接构建的开发版），
	// 此时不做「有没有新版本」的判断，只把查到的最新版本摆出来。
	VersionComparable bool       `json:"versionComparable"`
	CheckedAt         *time.Time `json:"checkedAt,omitempty"`
	CheckError        string     `json:"checkError,omitempty"`
	// CanUpdate 为假表示这台机器上不能就地升级，原因在 Reason 里。
	CanUpdate bool   `json:"canUpdate"`
	Reason    string `json:"reason,omitempty" doc:"不能就地升级的原因"`
	Container bool   `json:"container" doc:"是否运行在容器中"`
	// Image 是容器部署该换成的镜像标签，仅在容器里且查到新版本时有值。
	Image string `json:"image,omitempty" doc:"容器部署要换成的镜像引用"`
	// Rollback 非空表示当前跑的版本比在线升级装过的那个旧——多半是容器被重建，
	// 回到了镜像里的版本。
	Rollback *Rollback `json:"rollback,omitempty"`
	// Executable 是当前程序的路径，排障时要看的第一样东西。
	Executable    string         `json:"executable,omitempty"`
	Repo          string         `json:"repo" doc:"更新源仓库"`
	AutoCheck     bool           `json:"autoCheck"`
	CheckInterval string         `json:"checkInterval" doc:"自动检查间隔"`
	Prerelease    bool           `json:"prerelease" doc:"是否把预发布版本算作升级目标"`
	Progress      UpdateProgress `json:"progress"`
	Backups       []Backup       `json:"backups"`
	BackupDir     string         `json:"backupDir"`
	// KeepBackups 是升级后自动保留的备份份数，0 表示不自动清理。
	KeepBackups int `json:"keepBackups"`
}

// Service 是在线升级的全部业务逻辑。
type Service struct {
	cfg        config.UpdateConfig
	env        Environment
	current    version.Info
	source     *Source
	downloader *Downloader
	installer  *Installer
	backupDir  string
	cacheDir   string
	dataDir    string
	logger     *slog.Logger

	// requestShutdown 由 cmd/lumo 注入，用来在装好之后触发一次优雅停机。
	// 没有注入时（openapi 这类不起服务的命令）升级会停在 ready 阶段。
	requestShutdown func()

	mu        sync.Mutex
	latest    *Release
	checkedAt time.Time
	checkErr  string
	progress  UpdateProgress
	running   bool
	// relaunch 为真表示停机之后要用新二进制把自己拉起来。
	relaunch bool
	// rollback 在启动时算一次：版本不会在进程运行期间变。
	rollback *Rollback
}

// NewService 构造服务。
func NewService(cfg config.Config, current version.Info, logger *slog.Logger) *Service {
	env := DetectEnvironment()
	userAgent := fmt.Sprintf("lumo/%s (+https://github.com/%s)", current.Version, cfg.Update.Repo)
	backupDir := filepath.Join(cfg.DataDir, workdir.BackupsDirName)

	return &Service{
		cfg:        cfg.Update,
		env:        env,
		current:    current,
		source:     NewSource(cfg.Update.Repo, cfg.Update.Token, userAgent, cfg.Update.APIBase),
		downloader: NewDownloader(cfg.Update.Token, userAgent),
		installer:  NewInstaller(env, backupDir),
		backupDir:  backupDir,
		cacheDir:   filepath.Join(cfg.DataDir, workdir.CacheDirName, updateCacheDir),
		dataDir:    cfg.DataDir,
		logger:     logger,
		progress:   UpdateProgress{Phase: PhaseIdle},
	}
}

// OnShutdownRequest 注入「请求停机」的回调。
func (s *Service) OnShutdownRequest(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requestShutdown = fn
}

// Environment 返回探测到的运行环境。
func (s *Service) Environment() Environment { return s.env }

// ---------- 检查 ----------

// Check 查询更新源并缓存结果。
func (s *Service) Check(ctx context.Context) (*Release, error) {
	if !s.cfg.Enabled {
		return nil, ErrDisabled
	}

	s.mu.Lock()
	// 节流窗口内复用上次的成功结果：页面上「检查更新」是个按钮，按钮会被连点，
	// 而每次点击都是一次对 GitHub 的请求——未认证配额只有 60 次/小时。
	if s.latest != nil && s.checkErr == "" && time.Since(s.checkedAt) < manualCheckThrottle {
		cached := s.latest
		s.mu.Unlock()
		return cached, nil
	}
	s.mu.Unlock()

	rel, err := s.source.Latest(ctx, s.cfg.Prerelease)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.checkedAt = time.Now()
	// 没有当前平台的资产时 rel 仍然是有效的版本信息，一并留下：
	// 「有 1.2.0 了，但没打 windows/arm64 的包」比「检查失败」有用。
	if rel != nil {
		s.latest = rel
	}
	if err != nil {
		s.checkErr = err.Error()
		return rel, err
	}
	s.checkErr = ""
	return rel, nil
}

// StartAutoCheck 起一个后台循环定期检查新版本。ctx 结束即退出。
func (s *Service) StartAutoCheck(ctx context.Context) {
	if !s.cfg.Enabled || !s.cfg.AutoCheck {
		return
	}
	go func() {
		timer := time.NewTimer(initialCheckDelay)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
			}

			rel, err := s.Check(ctx)
			switch {
			case err != nil:
				// 检查更新失败不是故障：断网、限流、仓库还没发版都会走到这里。
				// 记 warn 让它在日志里留痕，但不做任何重试——下一轮自然会再问一次。
				s.logger.Warn("检查更新失败", slog.Any("error", err))
			case s.hasUpdate(rel):
				s.logger.Info("发现新版本",
					slog.String("current", s.current.Version),
					slog.String("latest", rel.Tag))
			}
			timer.Reset(s.cfg.CheckInterval)
		}
	}()
}

// hasUpdate 判断一个发布是否比当前版本新。
func (s *Service) hasUpdate(rel *Release) bool {
	if rel == nil || rel.Version.Raw == "" {
		return false
	}
	currentVersion, ok := ParseVersion(s.current.Version)
	if !ok {
		return false
	}
	return CompareVersions(rel.Version, currentVersion) > 0
}

// ---------- 状态 ----------

// Status 汇总当前状态。
func (s *Service) Status() UpdateStatus {
	s.mu.Lock()
	latest := s.latest
	checkedAt := s.checkedAt
	checkErr := s.checkErr
	progress := s.progress
	s.mu.Unlock()

	canUpdate, reason := s.env.CanUpdate(s.cfg.AllowInContainer)
	if !s.cfg.Enabled {
		canUpdate, reason = false, "在线升级已在配置中关闭（update.enabled）"
	}
	_, parsable := ParseVersion(s.current.Version)

	status := UpdateStatus{
		Enabled: s.cfg.Enabled,
		Current: BuildInfo{
			Version:  s.current.Version,
			Commit:   s.current.Commit,
			Date:     s.current.Date,
			Platform: s.current.Platform,
		},
		Latest:            latest,
		HasUpdate:         s.hasUpdate(latest),
		VersionComparable: parsable,
		CheckError:        checkErr,
		CanUpdate:         canUpdate,
		Reason:            reason,
		Container:         s.env.Container,
		Executable:        s.env.Executable,
		Repo:              s.cfg.Repo,
		AutoCheck:         s.cfg.AutoCheck,
		CheckInterval:     s.cfg.CheckInterval.String(),
		Prerelease:        s.cfg.Prerelease,
		Progress:          progress,
		BackupDir:         s.backupDir,
		KeepBackups:       s.cfg.KeepBackups,
	}
	if !checkedAt.IsZero() {
		at := checkedAt
		status.CheckedAt = &at
	}
	// 容器里给出该换成哪个标签：说「请改用新的镜像标签」而不说是哪个，
	// 等于把站长支去翻文档。
	if s.env.Container && latest != nil && status.HasUpdate && s.cfg.Image != "" {
		status.Image = s.cfg.Image + ":" + latest.Version.Raw
	}
	status.Rollback = s.rollback

	backups, err := ListBackups(s.backupDir)
	if err != nil {
		s.logger.Warn("读取备份目录失败", slog.Any("error", err))
	}
	if backups == nil {
		backups = []Backup{}
	}
	status.Backups = backups
	return status
}

// Progress 返回当前任务进度。
func (s *Service) Progress() UpdateProgress {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.progress
}

// ---------- 升级 ----------

// Apply 启动一次升级。接口立即返回，进度经 Status 查询。
//
// ctx 只用于启动前的校验：任务本身跑在应用级的 context 上，
// 不能让一次浏览器刷新把正在替换文件的过程掐断。
func (s *Service) Apply(base context.Context) (UpdateProgress, error) {
	if !s.cfg.Enabled {
		return UpdateProgress{}, ErrDisabled
	}
	if ok, reason := s.env.CanUpdate(s.cfg.AllowInContainer); !ok {
		return UpdateProgress{}, fmt.Errorf("%w：%s", ErrNotUpgradable, reason)
	}

	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return UpdateProgress{}, ErrBusy
	}
	target := s.latest
	s.mu.Unlock()

	if target == nil {
		// 还没查过就直接点了升级：先查一次，省掉「请先检查更新」这种把状态机
		// 甩给用户的提示。
		rel, err := s.Check(base)
		if err != nil {
			return UpdateProgress{}, err
		}
		target = rel
	}
	if !s.hasUpdate(target) {
		return UpdateProgress{}, ErrNoUpdate
	}
	if target.Asset.URL == "" {
		return UpdateProgress{}, ErrNoAsset
	}

	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return UpdateProgress{}, ErrBusy
	}
	s.running = true
	now := time.Now()
	s.progress = UpdateProgress{
		Phase:     PhaseDownloading,
		Version:   target.Version.Raw,
		Total:     target.Asset.Size,
		StartedAt: &now,
		UpdatedAt: &now,
	}
	progress := s.progress
	s.mu.Unlock()

	go s.run(target)
	return progress, nil
}

// run 执行一次完整升级：下载 → 校验 → 安装 → 重启。
func (s *Service) run(target *Release) {
	// 任务跑在自己的 context 上：它与发起升级的那次 HTTP 请求无关，
	// 但也不能永远跑下去。
	ctx, cancel := context.WithTimeout(context.Background(), applyTimeout)
	defer cancel()
	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()

	s.logger.Info("开始升级",
		slog.String("from", s.current.Version),
		slog.String("to", target.Tag),
		slog.String("asset", target.Asset.Name))

	archive, err := s.download(ctx, target)
	if err != nil {
		s.fail(err)
		return
	}
	// 发布包留着没有意义：装完即弃，装不上也该弃——几十 MB 躺在 cache 里，
	// 下次升级还会重新下。
	defer func() { _ = os.RemoveAll(s.cacheDir) }()

	s.setPhase(PhaseInstalling)
	backup, err := s.installer.Install(ctx, archive, target.Version, s.current.Version)
	if err != nil {
		s.fail(err)
		return
	}
	if removed := PruneBackups(s.backupDir, s.cfg.KeepBackups); removed > 0 {
		s.logger.Info("已清理旧备份", slog.Int("removed", removed))
	}
	// 记下「这台机器由在线升级装到了这个版本」。容器被重建回旧镜像时，
	// 下次启动靠它把回退说出来（见 state.go）。
	if err := writeState(s.dataDir, target.Version.Raw); err != nil {
		s.logger.Warn("记录升级状态失败，版本回退将无法被检测到", slog.Any("error", err))
	}

	s.mu.Lock()
	s.progress.Backup = backup
	s.progress.Phase = PhaseRestarting
	s.touchLocked()
	shutdown := s.requestShutdown
	s.relaunch = shutdown != nil
	s.mu.Unlock()

	s.logger.Info("新版本已就位",
		slog.String("version", target.Version.Raw),
		slog.String("backup", backup))

	if shutdown == nil {
		// 没有停机回调意味着当前进程不是在提供服务（例如被当成工具调用），
		// 这时不该自作主张地 exec，交给人去重启。
		s.mu.Lock()
		s.progress.Phase = PhaseReady
		s.touchLocked()
		s.mu.Unlock()
		return
	}
	shutdown()
}

// download 下载发布包到缓存目录。
func (s *Service) download(ctx context.Context, target *Release) (string, error) {
	// 每次都从空目录开始：上次失败留下的半截文件会让校验和对不上，
	// 而那个错误看起来像是发布方的问题。
	_ = os.RemoveAll(s.cacheDir)
	if err := os.MkdirAll(s.cacheDir, 0o750); err != nil {
		return "", fmt.Errorf("创建下载目录: %w", err)
	}
	return s.downloader.Fetch(ctx, target, s.cacheDir, func(done, total int64) {
		s.mu.Lock()
		s.progress.Downloaded = done
		if total > 0 {
			s.progress.Total = total
		}
		s.touchLocked()
		s.mu.Unlock()
	})
}

// setPhase 切换阶段。
func (s *Service) setPhase(phase Phase) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.progress.Phase = phase
	s.touchLocked()
}

// fail 把任务标记为失败，错误原文留给界面显示。
func (s *Service) fail(err error) {
	s.logger.Error("升级失败", slog.Any("error", err))
	s.mu.Lock()
	defer s.mu.Unlock()
	s.progress.Phase = PhaseFailed
	s.progress.Error = err.Error()
	s.touchLocked()
}

// touchLocked 更新状态时间戳，调用方必须已持有锁。
func (s *Service) touchLocked() {
	now := time.Now()
	s.progress.UpdatedAt = &now
}

// ---------- 重启 ----------

// RelaunchPending 报告停机之后是否要拉起新版本。
func (s *Service) RelaunchPending() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.relaunch
}

// Relaunch 用替换后的二进制重新拉起服务。
//
// 必须在 HTTP 已经停机、模块与数据库都关好之后调用：Unix 上它不会返回
// （当前进程镜像就地被换掉），Windows 上它会起一个新进程，而那个新进程
// 立刻就要去绑同一个端口。
func (s *Service) Relaunch() error {
	exe := s.env.Executable
	if exe == "" {
		return errors.New("找不到当前程序的路径，无法自动重启")
	}
	args := append([]string{exe}, os.Args[1:]...)
	s.logger.Info("正在以新版本重启", slog.String("executable", exe))
	return execSelf(exe, args)
}

// ---------- 备份 ----------

// Backups 列出备份。
func (s *Service) Backups() ([]Backup, error) { return ListBackups(s.backupDir) }

// DeleteBackup 删除一份备份。
func (s *Service) DeleteBackup(name string) error {
	if err := DeleteBackup(s.backupDir, name); err != nil {
		return err
	}
	s.logger.Info("已删除升级备份", slog.String("name", name))
	return nil
}

// CleanupStale 清理上一次升级遗留的旧程序文件，启动时调用一次。
func (s *Service) CleanupStale() { cleanupStale(s.env.Executable) }

// DetectRollback 在启动时检测版本回退，结果留给 Status 展示。
//
// 最典型的一幕：容器里开着 allowInContainer 升到了新版本，几个月后站长在面板上
// 改了个端口，容器被重建，站点悄悄回到镜像里的旧版本——而数据库已经迁到新版本的
// schema 了。这条 warn 是把它说出来的唯一机会。
func (s *Service) DetectRollback() {
	s.rollback = detectRollback(s.dataDir, s.current.Version)
	if s.rollback == nil {
		return
	}
	s.logger.Warn("检测到版本回退",
		slog.String("installed", s.rollback.From),
		slog.String("running", s.rollback.To),
		slog.Time("installedAt", s.rollback.At),
		slog.Bool("container", s.env.Container))
}
