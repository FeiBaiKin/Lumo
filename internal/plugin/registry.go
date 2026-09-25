package plugin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/FeiBaiKin/lumo/internal/plugin/wasm"
)

// ErrConsentRequired 表示插件要用的能力还没征得站长同意。
var ErrConsentRequired = errors.New("这个插件要用的能力还没有经过确认")

// ErrBackend 表示插件的后端代码加载失败。
var ErrBackend = errors.New("插件后端加载失败")

// reasonNeedsConsent 是升级后多要了能力、被自动停用时记下的原因。
const reasonNeedsConsent = "新版本要用更多能力，确认之后才能重新启用"

// Loaded 是一个已安装的插件。
type Loaded struct {
	Manifest *Manifest
	// Dir 是插件目录的绝对路径。
	Dir string
	// FS 是插件包的根文件系统。
	FS fs.FS
	// Groups 是插件声明的设置分组。
	Groups []SettingsGroup
	// Enabled 是当前的启用状态。
	Enabled bool
	// Granted 是站长授予过的能力；nil 表示从没授予过。
	Granted *Capabilities
	// DisabledReason 是系统停用它的原因；站长手动停用时为空串。
	DisabledReason string

	// backend 是运行中的后端；未启用或纯声明式插件为 nil。只在持有注册表锁时读写。
	backend *wasm.Plugin
}

// ID 返回插件标识。
func (l *Loaded) ID() string { return l.Manifest.Metadata.Name }

// SettingGroup 按名字取设置分组。
func (l *Loaded) SettingGroup(name string) (*SettingsGroup, bool) {
	for i := range l.Groups {
		if l.Groups[i].Name == name {
			return &l.Groups[i], true
		}
	}
	return nil, false
}

// NeedsConsent 判断启用之前要不要先征得站长同意：声明了能力、而授予过的覆盖不了。
func (l *Loaded) NeedsConsent() bool {
	caps := &l.Manifest.Spec.Capabilities
	if caps.IsZero() {
		return false
	}
	return l.Granted == nil || !l.Granted.Covers(caps)
}

// Registry 持有全部已安装插件、它们的启用状态与运行中的后端。
//
// 它是插件系统的唯一运行时状态：设置分组、菜单项、列表接口与钩子分发都经它取数据。
// 读远多于写，故用 RWMutex。编译后端要一两秒，一律在锁外做。
type Registry struct {
	root   string
	store  *Store
	logger *slog.Logger
	engine *wasm.Engine

	mu      sync.RWMutex
	plugins map[string]*Loaded
	broken  map[string]string
}

// RegistryOptions 是构造 Registry 的参数。
type RegistryOptions struct {
	// Root 是插件安装目录，通常是 data/plugins。
	Root   string
	Store  *Store
	Logger *slog.Logger
}

// NewRegistry 构造 Registry。此时不读盘也不读库——那是 Load 的事。
func NewRegistry(opts *RegistryOptions) *Registry {
	return &Registry{
		root:    opts.Root,
		store:   opts.Store,
		logger:  opts.Logger,
		plugins: map[string]*Loaded{},
		broken:  map[string]string{},
	}
}

// Root 返回插件安装目录。
func (r *Registry) Root() string { return r.root }

// SetEngine 设置运行后端代码的引擎。没有引擎时，带后端的插件启用不了。
func (r *Registry) SetEngine(e *wasm.Engine) { r.engine = e }

// Load 扫描已安装的插件目录并与库里的状态对齐，再启动启用中插件的后端。
//
// 这是模块第一次被允许访问数据库的时机。
//
// 两边不一致时的处置：
//   - 目录在、库里没有 —— 手工拷进来的插件。照收，登记为「已安装但未启用」，
//     与主题的做法一致；不这样，用户把目录拷进去后会找不到它。
//   - 库里有、目录不在 —— 文件被手工删了。标记为损坏并在列表里标出来，
//     而不是静默丢掉那一行：用户需要知道发生了什么。
func (r *Registry) Load(ctx context.Context) error {
	installed, err := ListInstalled(r.root)
	if err != nil {
		return err
	}

	records := map[string]Record{}
	if r.store != nil {
		rows, listErr := r.store.List(ctx)
		if listErr != nil {
			return listErr
		}
		for i := range rows {
			records[rows[i].Name] = rows[i]
		}
	}

	r.mu.Lock()
	r.plugins = map[string]*Loaded{}
	r.broken = map[string]string{}
	var manual []*Loaded
	for _, name := range installed {
		loaded, loadErr := r.loadDir(name)
		if loadErr != nil {
			r.broken[name] = loadErr.Error()
			r.warn("插件加载失败", name, loadErr)
			continue
		}
		if record, ok := records[name]; ok {
			loaded.Enabled = record.Enabled
			loaded.Granted = record.Granted
			loaded.DisabledReason = record.DisabledReason
			// 库里的版本与目录里的不一致，说明文件被换过而没走升级流程。
			// 以目录为准（那才是实际会被执行的），但要说出来。
			if record.Version != loaded.Manifest.Spec.Version {
				r.warn("插件目录的版本与记录不一致，以目录为准", name, fmt.Errorf(
					"记录 %s，目录 %s", record.Version, loaded.Manifest.Spec.Version))
			}
		} else {
			manual = append(manual, loaded)
		}
		r.plugins[name] = loaded
	}
	// 库里有、目录不在的：文件被手工删了。
	for name := range records {
		if _, ok := r.plugins[name]; !ok {
			r.broken[name] = "插件目录不存在，文件可能被手工删除了"
		}
	}
	r.mu.Unlock()

	for _, loaded := range manual {
		if err := r.register(ctx, loaded); err != nil {
			r.warn("登记手工放入的插件失败", loaded.ID(), err)
		}
	}
	for _, loaded := range r.Enabled() {
		r.startOrSuspend(ctx, loaded)
	}
	return nil
}

// loadDir 加载并校验一个插件目录。
func (r *Registry) loadDir(name string) (*Loaded, error) {
	dir := filepath.Join(r.root, name)
	manifest, err := Validate(dir)
	if err != nil {
		return nil, err
	}
	fsys := os.DirFS(dir)
	groups, err := loadSettings(fsys)
	if err != nil {
		return nil, err
	}
	return &Loaded{Manifest: manifest, Dir: dir, FS: fsys, Groups: groups}, nil
}

// register 把插件登记进库里。
func (r *Registry) register(ctx context.Context, loaded *Loaded) error {
	if r.store == nil {
		return nil
	}
	raw, err := os.ReadFile(filepath.Join(loaded.Dir, FileManifest))
	if err != nil {
		return fmt.Errorf("读取清单: %w", err)
	}
	return r.store.Put(ctx, loaded.Manifest, raw)
}

// Get 按名字取插件。
func (r *Registry) Get(name string) (*Loaded, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	loaded, ok := r.plugins[name]
	return loaded, ok
}

// List 返回全部已安装插件，按标识排序。
func (r *Registry) List() []*Loaded {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Loaded, 0, len(r.plugins))
	for _, loaded := range r.plugins {
		out = append(out, loaded)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID() < out[j].ID() })
	return out
}

// Enabled 返回全部已启用的插件，按标识排序。
//
// 每次调用都现算：设置分组与菜单项都靠它，而启用状态随时会变。
func (r *Registry) Enabled() []*Loaded {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Loaded, 0, len(r.plugins))
	for _, loaded := range r.plugins {
		if loaded.Enabled {
			out = append(out, loaded)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID() < out[j].ID() })
	return out
}

// Backend 返回运行中插件的后端。
func (r *Registry) Backend(name string) (*wasm.Plugin, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	loaded, ok := r.plugins[name]
	if !ok || loaded.backend == nil {
		return nil, false
	}
	return loaded.backend, true
}

// Broken 返回加载失败或被手工删除的插件及原因。
func (r *Registry) Broken() map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]string, len(r.broken))
	for name, reason := range r.broken {
		out[name] = reason
	}
	return out
}

// Install 从 zip 包安装或升级一个插件，新装的登记为未启用。
//
// 升级**保持原来的启用状态**——升级一个正在用的插件不该把它停掉；
// 除非新版本多要了能力，那就先停用、等站长确认。
func (r *Registry) Install(ctx context.Context, reader io.ReaderAt, size int64) (*Manifest, error) {
	manifest, err := Install(r.root, reader, size, true)
	if err != nil {
		return nil, err
	}
	name := manifest.Metadata.Name
	loaded, loadErr := r.loadDir(name)
	if loadErr != nil {
		// 装完才发现加载不了：把目录清掉，不留一个半成品在那里。
		_ = Remove(r.root, name)
		return nil, loadErr
	}

	r.mu.Lock()
	var old *wasm.Plugin
	if previous, ok := r.plugins[name]; ok {
		loaded.Enabled = previous.Enabled
		loaded.Granted = previous.Granted
		loaded.DisabledReason = previous.DisabledReason
		old = previous.backend
	}
	r.plugins[name] = loaded
	delete(r.broken, name)
	r.mu.Unlock()
	closeBackend(old)

	if err := r.register(ctx, loaded); err != nil {
		return nil, err
	}
	if loaded.Enabled {
		r.startOrSuspend(ctx, loaded)
	}
	return manifest, nil
}

// SetEnabled 启用或停用一个插件。
//
// 启用一个声明了能力的插件时，accept 表示站长已经看过并同意了这些能力；
// 没同意而又需要同意时返回 ErrConsentRequired。带后端的插件在启用时编译加载，
// 加载失败则保持停用并返回 ErrBackend。
func (r *Registry) SetEnabled(ctx context.Context, name string, enabled, accept bool) error {
	loaded, ok := r.Get(name)
	if !ok {
		return fmt.Errorf("%w：%s", ErrNotFound, name)
	}
	if !enabled {
		if err := r.persist(ctx, name, State{}); err != nil {
			return err
		}
		r.mu.Lock()
		loaded.Enabled, loaded.DisabledReason = false, ""
		old := loaded.backend
		loaded.backend = nil
		r.mu.Unlock()
		closeBackend(old)
		return nil
	}

	granted := loaded.Granted
	var grant *Capabilities
	if loaded.NeedsConsent() {
		if !accept {
			return ErrConsentRequired
		}
		caps := loaded.Manifest.Spec.Capabilities
		granted, grant = &caps, &caps
	}
	var backend *wasm.Plugin
	if loaded.Manifest.HasBackend() {
		started, err := r.startBackend(ctx, loaded)
		if err != nil {
			return fmt.Errorf("%w：%w", ErrBackend, err)
		}
		backend = started
	}
	if err := r.persist(ctx, name, State{Enabled: true, Granted: grant}); err != nil {
		closeBackend(backend)
		return err
	}
	r.mu.Lock()
	loaded.Enabled, loaded.Granted, loaded.DisabledReason = true, granted, ""
	old := loaded.backend
	loaded.backend = backend
	r.mu.Unlock()
	closeBackend(old)
	return nil
}

// Suspend 由系统停用一个插件并记下原因，如连续崩溃。原因会显示在后台。
func (r *Registry) Suspend(ctx context.Context, name, reason string) {
	r.mu.Lock()
	loaded, ok := r.plugins[name]
	if !ok || !loaded.Enabled {
		r.mu.Unlock()
		return
	}
	loaded.Enabled, loaded.DisabledReason = false, reason
	old := loaded.backend
	loaded.backend = nil
	r.mu.Unlock()
	closeBackend(old)

	if err := r.persist(ctx, name, State{Reason: reason}); err != nil {
		r.warn("记录插件停用状态失败", name, err)
	}
	if r.logger != nil {
		r.logger.Warn("插件已被自动停用", slog.String("plugin", name), slog.String("reason", reason))
	}
}

// startOrSuspend 启动一个启用中插件的后端；需要重新确认或启动失败的，停用并记下原因。
func (r *Registry) startOrSuspend(ctx context.Context, loaded *Loaded) {
	if loaded.NeedsConsent() {
		r.Suspend(ctx, loaded.ID(), reasonNeedsConsent)
		return
	}
	if !loaded.Manifest.HasBackend() {
		return
	}
	backend, err := r.startBackend(ctx, loaded)
	if err != nil {
		r.Suspend(ctx, loaded.ID(), "后端加载失败："+err.Error())
		return
	}
	r.mu.Lock()
	old := loaded.backend
	loaded.backend = backend
	r.mu.Unlock()
	closeBackend(old)
}

// startBackend 编译并加载插件的 plugin.wasm。
func (r *Registry) startBackend(ctx context.Context, loaded *Loaded) (*wasm.Plugin, error) {
	if r.engine == nil {
		return nil, errors.New("插件运行时没有就绪")
	}
	binary, err := os.ReadFile(filepath.Join(loaded.Dir, FileWasm))
	if err != nil {
		return nil, fmt.Errorf("读取 %s: %w", FileWasm, err)
	}
	backend, err := r.engine.Load(ctx, loaded.ID(), binary)
	if err != nil {
		return nil, err
	}
	if err := checkHandlers(loaded.Manifest, backend.Description()); err != nil {
		closeBackend(backend)
		return nil, err
	}
	return backend, nil
}

// persist 写入启用状态；没有库时什么也不做。
func (r *Registry) persist(ctx context.Context, name string, st State) error {
	if r.store == nil {
		return nil
	}
	return r.store.SetState(ctx, name, st)
}

// Uninstall 卸载一个插件：停后端、删状态、级联删设置值、删目录。
//
// 顺序是刻意的：**先删库再删文件**。反过来的话，删文件成功而删库失败时，
// 库里会留下一条指向不存在目录的记录，而列表里会一直显示一个打不开的插件。
// 先删库则最坏情况只是留下一个未登记目录，重启时会被当成手工放入的插件重新登记。
func (r *Registry) Uninstall(ctx context.Context, name string) error {
	if _, ok := r.Get(name); !ok {
		return fmt.Errorf("%w：%s", ErrNotFound, name)
	}
	if r.store != nil {
		if err := r.store.Delete(ctx, name); err != nil {
			return err
		}
	}
	r.mu.Lock()
	var old *wasm.Plugin
	if loaded, ok := r.plugins[name]; ok {
		old = loaded.backend
	}
	delete(r.plugins, name)
	delete(r.broken, name)
	r.mu.Unlock()
	closeBackend(old)

	return Remove(r.root, name)
}

// Close 停掉全部运行中的后端。
func (r *Registry) Close() {
	r.mu.Lock()
	var backends []*wasm.Plugin
	for _, loaded := range r.plugins {
		if loaded.backend != nil {
			backends = append(backends, loaded.backend)
			loaded.backend = nil
		}
	}
	r.mu.Unlock()
	for _, backend := range backends {
		closeBackend(backend)
	}
}

func closeBackend(p *wasm.Plugin) {
	if p != nil {
		_ = p.Close(context.Background())
	}
}

// warn 记录一条带插件名的告警。
func (r *Registry) warn(message, name string, err error) {
	if r.logger == nil {
		return
	}
	r.logger.Warn(message, slog.String("plugin", name), slog.Any("error", err))
}
