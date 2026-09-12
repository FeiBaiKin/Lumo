package plugin

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

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

// Registry 持有全部已安装插件与它们的启用状态。
//
// 它是插件系统的唯一运行时状态：设置分组、菜单项与列表接口都经它取数据。
// 读远多于写，故用 RWMutex。
type Registry struct {
	root   string
	store  *Store
	logger *slog.Logger

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

// Load 扫描已安装的插件目录并与库里的状态对齐。
//
// 这是模块第一次被允许访问数据库的时机（agent.md §3.2）。
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
	// 记下启动时库里有哪些：下面判断「这个目录是手工拷进来的吗」要用到它。
	known := make(map[string]bool, len(records))
	for name := range records {
		known[name] = true
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.plugins = map[string]*Loaded{}
	r.broken = map[string]string{}

	for _, name := range installed {
		loaded, loadErr := r.loadDir(name)
		if loadErr != nil {
			r.broken[name] = loadErr.Error()
			r.warn("插件加载失败", name, loadErr)
			continue
		}
		if record, ok := records[name]; ok {
			loaded.Enabled = record.Enabled
			// 库里的版本与目录里的不一致，说明文件被换过而没走升级流程。
			// 以目录为准（那才是实际会被执行的），但要说出来。
			if record.Version != loaded.Manifest.Spec.Version {
				r.warn("插件目录的版本与记录不一致，以目录为准", name, fmt.Errorf(
					"记录 %s，目录 %s", record.Version, loaded.Manifest.Spec.Version))
			}
		}
		r.plugins[name] = loaded
	}

	// 库里有、目录不在的：文件被手工删了。标为损坏而不是静默丢掉那一行，
	// 用户需要知道发生了什么。
	for name := range records {
		if _, ok := r.plugins[name]; ok {
			continue
		}
		r.broken[name] = "插件目录不存在，文件可能被手工删除了"
	}

	// 目录里有、库里没有的：手工拷进来的。照收，登记为「已安装但未启用」，
	// 与主题的做法一致；不这样，用户把目录拷进去后会找不到它。
	if r.store != nil {
		for name, loaded := range r.plugins {
			if known[name] {
				continue
			}
			if err := r.register(ctx, loaded); err != nil {
				r.warn("登记手工放入的插件失败", name, err)
			}
		}
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

// Install 从 zip 包安装或升级一个插件，登记为未启用。
//
// 已存在的插件走覆盖安装（升级）：升级后仍然是「已安装」状态，
// 但**保持原来的启用状态**——升级一个正在用的插件不该把它停掉。
func (r *Registry) Install(ctx context.Context, reader io.ReaderAt, size int64) (*Manifest, error) {
	manifest, err := Install(r.root, reader, size, true)
	if err != nil {
		return nil, err
	}
	name := manifest.Metadata.Name

	r.mu.Lock()
	loaded, loadErr := r.loadDir(name)
	if loadErr != nil {
		r.mu.Unlock()
		// 装完才发现加载不了：把目录清掉，不留一个半成品在那里。
		_ = Remove(r.root, name)
		return nil, loadErr
	}
	// 升级时保留原来的启用状态。
	if previous, ok := r.plugins[name]; ok {
		loaded.Enabled = previous.Enabled
	}
	r.plugins[name] = loaded
	delete(r.broken, name)
	r.mu.Unlock()

	if err := r.register(ctx, loaded); err != nil {
		return nil, err
	}
	if loaded.Enabled && r.store != nil {
		if err := r.store.SetEnabled(ctx, name, true); err != nil {
			return nil, err
		}
	}
	return manifest, nil
}

// SetEnabled 启用或停用一个插件。
func (r *Registry) SetEnabled(ctx context.Context, name string, enabled bool) error {
	r.mu.Lock()
	loaded, ok := r.plugins[name]
	if !ok {
		r.mu.Unlock()
		return fmt.Errorf("%w：%s", ErrNotFound, name)
	}
	previous := loaded.Enabled
	loaded.Enabled = enabled
	r.mu.Unlock()

	if r.store != nil {
		if err := r.store.SetEnabled(ctx, name, enabled); err != nil {
			// 落库失败就回滚内存状态，否则重启后两边对不上。
			r.mu.Lock()
			loaded.Enabled = previous
			r.mu.Unlock()
			return err
		}
	}
	return nil
}

// Uninstall 卸载一个插件：删目录、删状态、级联删设置值。
//
// 顺序是刻意的：**先删库再删文件**。反过来的话，删文件成功而删库失败时，
// 库里会留下一条指向不存在目录的记录，而列表里会一直显示一个打不开的插件。
// 先删库则最坏情况只是留下一个未登记目录，重启时会被当成手工放入的插件重新登记。
func (r *Registry) Uninstall(ctx context.Context, name string) error {
	r.mu.RLock()
	_, ok := r.plugins[name]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("%w：%s", ErrNotFound, name)
	}

	if r.store != nil {
		if err := r.store.Delete(ctx, name); err != nil {
			return err
		}
	}
	r.mu.Lock()
	delete(r.plugins, name)
	delete(r.broken, name)
	r.mu.Unlock()

	return Remove(r.root, name)
}

// warn 记录一条带插件名的告警。
func (r *Registry) warn(message, name string, err error) {
	if r.logger == nil {
		return
	}
	r.logger.Warn(message, slog.String("plugin", name), slog.Any("error", err))
}
