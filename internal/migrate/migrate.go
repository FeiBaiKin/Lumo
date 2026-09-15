// Package migrate 封装 goose 迁移的执行与并发保护。
//
// 两条关键设计：
//
//  1. 启动自动迁移必须带锁。多实例同时启动时，无锁会让两个进程
//     并发执行同一批 DDL，轻则报错、重则留下半成品 schema。这里用 PostgreSQL 的
//     会话级 advisory lock 做互斥：拿不到锁的实例等待，而不是跳过迁移（跳过会让
//     实例在旧 schema 上运行，问题更隐蔽）。
//  2. 核心与每个模块是独立的迁移来源，各有自己的版本表。插件生态里不可能协调
//     全局唯一的迁移编号，模块只需在自己的序列内递增。
//
// 实现基于 goose 的 Provider（实例级、无包级全局状态），并发构造多个 Migrator 也安全。
package migrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/pressly/goose/v3"
)

// advisoryLockID 是迁移互斥锁的标识，取自 "lumo.migrate" 的任意稳定常量。
// 同一实例内所有 Lumo 进程共用它，故不可与其他项目冲突——取值刻意选得足够特异。
const advisoryLockID int64 = 0x4C554D4F4D4947 // "LUMOMIG"

// CoreName 是核心迁移来源的名称。
//
// 它的版本表沿用 goose 默认名 goose_db_version，以兼容阶段 1、2 已迁移过的库。
const CoreName = "core"

// DefaultLockTimeout 是未显式配置时等待迁移锁的上限。
//
// 等待是必要语义（多实例并发迁移必须串行），但必须有上限：数据库黑洞或
// 遗留会话会让 pg_advisory_lock 永久挂起，进而卡死整个启动流程。
const DefaultLockTimeout = 2 * time.Minute

const (
	coreTable         = "goose_db_version"
	moduleTablePrefix = "goose_db_version_"
)

// sourceNamePattern 限定来源名形态：它会拼进版本表名，必须是安全的标识符。
var sourceNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,40}$`)

// Source 是一个迁移来源：核心或某个模块。
//
// FS 的根目录直接包含 goose SQL 文件（goose 不递归子目录，需要时用 fs.Sub 定位）。
type Source struct {
	Name string
	FS   fs.FS
}

// Status 是一条迁移的应用状态。
type Status struct {
	Source    string
	Version   int64
	Path      string
	Applied   bool
	AppliedAt time.Time
}

// Migrator 按来源顺序执行迁移。
type Migrator struct {
	db          *sql.DB
	sources     []Source
	logger      *slog.Logger
	lockTimeout time.Duration
	// lockDB 是仅用于持有 advisory lock 的独立连接池，可为 nil（表示与迁移共用主池）。
	lockDB *sql.DB
}

// Option 是 Migrator 的可选配置。
type Option func(*Migrator)

// WithLockTimeout 覆盖等待迁移锁的上限；d <= 0 时保持默认值。
func WithLockTimeout(d time.Duration) Option {
	return func(m *Migrator) {
		if d > 0 {
			m.lockTimeout = d
		}
	}
}

// WithLockDB 指定一个仅用于持有迁移 advisory lock 的独立连接池（容量 1 即可）。
//
// goose 的 Provider 只接受 *sql.DB，无法让锁与迁移共用同一条会话；锁与迁移
// 若从同一池取连接，池容量为 1 时会互相等待。独立池把两者彻底隔开，
// 是单连接池部署的首选解法。
func WithLockDB(db *sql.DB) Option {
	return func(m *Migrator) { m.lockDB = db }
}

// New 构造 Migrator。sources 按给定顺序执行，核心应放在最前；logger 可为 nil。
func New(db *sql.DB, sources []Source, logger *slog.Logger, opts ...Option) (*Migrator, error) {
	if db == nil {
		return nil, errors.New("migrate: db 不能为 nil")
	}
	if len(sources) == 0 {
		return nil, errors.New("migrate: 至少需要一个迁移来源")
	}
	seen := make(map[string]bool, len(sources))
	for _, src := range sources {
		if !sourceNamePattern.MatchString(src.Name) {
			return nil, fmt.Errorf("migrate: 非法的迁移来源名 %q", src.Name)
		}
		if src.FS == nil {
			return nil, fmt.Errorf("migrate: 来源 %q 的文件系统为 nil", src.Name)
		}
		if seen[src.Name] {
			return nil, fmt.Errorf("migrate: 迁移来源 %q 重复", src.Name)
		}
		seen[src.Name] = true
	}
	m := &Migrator{db: db, sources: sources, logger: logger, lockTimeout: DefaultLockTimeout}
	for _, opt := range opts {
		if opt != nil {
			opt(m)
		}
	}
	return m, nil
}

// TableName 返回来源对应的版本表名。
func TableName(source string) string {
	if source == CoreName {
		return coreTable
	}
	return moduleTablePrefix + strings.ReplaceAll(source, "-", "_")
}

// Sources 返回来源名列表，顺序即执行顺序。
func (m *Migrator) Sources() []string {
	names := make([]string, 0, len(m.sources))
	for _, src := range m.sources {
		names = append(names, src.Name)
	}
	return names
}

// Up 按来源顺序应用所有待执行的迁移，全程持有互斥锁。
func (m *Migrator) Up(ctx context.Context) error {
	return m.withLock(ctx, func() error {
		for _, src := range m.sources {
			provider, err := m.provider(src)
			if err != nil {
				return err
			}
			results, err := provider.Up(ctx)
			if err != nil {
				return fmt.Errorf("执行来源 %q 的迁移: %w", src.Name, err)
			}
			version, err := provider.GetDBVersion(ctx)
			if err != nil {
				return fmt.Errorf("读取来源 %q 的版本: %w", src.Name, err)
			}
			if m.logger == nil {
				continue
			}
			if len(results) == 0 {
				m.logger.Info("数据库 schema 已是最新",
					slog.String("source", src.Name),
					slog.Int64("version", version))
				continue
			}
			m.logger.Info("迁移完成",
				slog.String("source", src.Name),
				slog.Int64("from", version-int64(len(results))),
				slog.Int64("to", version),
				slog.Int("applied", len(results)))
		}
		return nil
	})
}

// Down 回滚指定来源的最后一个迁移；source 为空时取核心。仅供开发排错，生产不应使用。
func (m *Migrator) Down(ctx context.Context, source string) error {
	src, err := m.find(source)
	if err != nil {
		return err
	}
	return m.withLock(ctx, func() error {
		provider, err := m.provider(src)
		if err != nil {
			return err
		}
		current, err := provider.GetDBVersion(ctx)
		if err != nil {
			return fmt.Errorf("读取来源 %q 的版本: %w", src.Name, err)
		}
		if current == 0 {
			if m.logger != nil {
				m.logger.Info("没有可回滚的迁移", slog.String("source", src.Name))
			}
			return nil
		}
		result, err := provider.Down(ctx)
		if err != nil {
			if errors.Is(err, goose.ErrNoNextVersion) {
				return nil
			}
			return fmt.Errorf("回滚来源 %q 的迁移: %w", src.Name, err)
		}
		if m.logger != nil && result != nil && result.Source != nil {
			m.logger.Info("已回滚迁移",
				slog.String("source", src.Name),
				slog.Int64("version", result.Source.Version),
				slog.String("file", result.Source.Path))
		}
		return nil
	})
}

// Status 返回全部来源中每个迁移的应用状态，按来源顺序、版本升序排列。
func (m *Migrator) Status(ctx context.Context) ([]Status, error) {
	var out []Status
	for _, src := range m.sources {
		provider, err := m.provider(src)
		if err != nil {
			return nil, err
		}
		statuses, err := provider.Status(ctx)
		if err != nil {
			return nil, fmt.Errorf("查询来源 %q 的迁移状态: %w", src.Name, err)
		}
		for _, st := range statuses {
			if st == nil || st.Source == nil {
				continue
			}
			out = append(out, Status{
				Source:    src.Name,
				Version:   st.Source.Version,
				Path:      st.Source.Path,
				Applied:   st.State == goose.StateApplied,
				AppliedAt: st.AppliedAt,
			})
		}
	}
	return out, nil
}

// Version 返回指定来源当前已应用的最新迁移版本号；source 为空时取核心。
// 版本表尚不存在时返回 0。
func (m *Migrator) Version(ctx context.Context, source string) (int64, error) {
	src, err := m.find(source)
	if err != nil {
		return 0, err
	}
	provider, err := m.provider(src)
	if err != nil {
		return 0, err
	}
	version, err := provider.GetDBVersion(ctx)
	if err != nil {
		return 0, fmt.Errorf("读取来源 %q 的版本: %w", src.Name, err)
	}
	return version, nil
}

// provider 为来源构造 goose Provider。Provider 不持有连接，无需关闭；
// 切勿调用其 Close——那会关掉共享的连接池。
func (m *Migrator) provider(src Source) (*goose.Provider, error) {
	provider, err := goose.NewProvider(goose.DialectPostgres, m.db, src.FS,
		goose.WithTableName(TableName(src.Name)))
	if err != nil {
		if errors.Is(err, goose.ErrNoMigrations) {
			return nil, fmt.Errorf("迁移来源 %q 没有任何迁移文件", src.Name)
		}
		return nil, fmt.Errorf("构造迁移来源 %q: %w", src.Name, err)
	}
	return provider, nil
}

// find 按名称查找来源，空名取核心。
func (m *Migrator) find(name string) (Source, error) {
	if name == "" {
		name = CoreName
	}
	for _, src := range m.sources {
		if src.Name == name {
			return src, nil
		}
	}
	return Source{}, fmt.Errorf("migrate: 未知的迁移来源 %q（可用：%s）", name, strings.Join(m.Sources(), ", "))
}

// withLock 在持有 advisory lock 的独占连接上执行 fn。
//
// 用 pg_advisory_lock 而非 pg_try_advisory_lock：拿不到锁时应当等待其他实例
// 完成迁移，而不是放弃后在旧 schema 上启动。等待上限由 lockTimeout 控制：
// 超时返回明确错误，绝不无限挂起。
func (m *Migrator) withLock(ctx context.Context, fn func() error) error {
	// 锁等待单独计时：迁移本身仍用原始 ctx，不受该期限限制。
	lockCtx, cancel := context.WithTimeout(ctx, m.lockTimeout)
	defer cancel()

	conn, release, err := m.lockConn(lockCtx)
	if err != nil {
		return err
	}
	defer release()

	if m.logger != nil {
		m.logger.Debug("正在获取迁移锁", slog.Int64("lockId", advisoryLockID))
	}
	start := time.Now()
	if _, err := conn.ExecContext(lockCtx, "SELECT pg_advisory_lock($1)", advisoryLockID); err != nil {
		// 父 ctx 自身取消/到期不算锁超时，原样报出，避免错误归因。
		if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
			return fmt.Errorf(
				"等待迁移锁超时（%s）：可能有其他实例正在迁移；如确认没有，请检查数据库连接或调大 LUMO_DATABASE_MIGRATION_LOCK_TIMEOUT",
				m.lockTimeout)
		}
		return fmt.Errorf("获取迁移锁: %w", err)
	}
	if waited := time.Since(start); waited > time.Second && m.logger != nil {
		m.logger.Info("等待其他实例完成迁移", slog.Duration("waited", waited))
	}

	defer func() {
		// 解锁用独立的 context：即使 ctx 已取消，锁也必须释放，
		// 否则该锁会一直挂到连接断开，阻塞后续所有实例启动。
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := conn.ExecContext(unlockCtx, "SELECT pg_advisory_unlock($1)", advisoryLockID); err != nil {
			if m.logger != nil {
				m.logger.Error("释放迁移锁失败", slog.Any("error", err))
			}
		}
	}()

	return fn()
}

// lockConn 取一条用于持有 advisory lock 的连接，并返回释放函数。
//
// 配置了独立锁池时直接用锁池。否则退回主池，并在主池容量 < 2 时临时把上限
// 提到 2：advisory lock 会一直占住连接直到解锁，容量为 1 时 goose 再也取不到
// 连接，迁移会永久等待。临时提高只影响本次迁移，结束后恢复原值。
func (m *Migrator) lockConn(ctx context.Context) (*sql.Conn, func(), error) {
	if m.lockDB != nil {
		conn, err := m.lockDB.Conn(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("获取迁移锁专用连接: %w", err)
		}
		return conn, func() { _ = conn.Close() }, nil
	}

	maxOpen := m.db.Stats().MaxOpenConnections
	raised := maxOpen == 1
	if raised {
		m.db.SetMaxOpenConns(2)
	}
	conn, err := m.db.Conn(ctx)
	if err != nil {
		if raised {
			m.db.SetMaxOpenConns(maxOpen)
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, nil, fmt.Errorf("等待可用数据库连接超时（%s）：连接池已被占满", m.lockTimeout)
		}
		return nil, nil, fmt.Errorf("获取迁移专用连接: %w", err)
	}
	release := func() {
		_ = conn.Close()
		if raised {
			m.db.SetMaxOpenConns(maxOpen)
		}
	}
	return conn, release, nil
}
