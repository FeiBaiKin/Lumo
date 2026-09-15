package theme

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"
)

// Loaded 是一个已加载并可用的主题。
type Loaded struct {
	// Manifest 是主题元信息。
	Manifest Manifest
	// Builtin 为真表示这是内置主题：不可删除，且始终是回退链的末端。
	// 它可能是从二进制里直接加载的，也可能是磁盘上那份解压出来的副本（见 Source）。
	Builtin bool
	// Source 说明内置主题从哪来：SourceEmbedded 或 SourceDisk；非内置主题为空串。
	Source string
	// Dir 是主题目录的绝对路径；来自二进制的内置主题为空串。
	Dir string
	// FS 是主题包的根文件系统。
	FS fs.FS
	// Static 是 static 子目录的文件系统；主题没有 static 时为 nil。
	Static fs.FS
	// AssetVersion 是静态资源的内容指纹，模板把它挂在资源 URL 的查询串上。
	// 主题文件一变它就变，浏览器的 1 小时缓存因此不会在改版后继续端出旧样式。
	AssetVersion string
	// Templates 是模板名列表（主题自己提供的）。
	Templates []string

	// engine 是已解析的模板引擎；开发模式下经 reloadable 换新。
	engine *reloadable
	// settings 是已编译的设置分组。
	settings *compiledSettings
}

// Engine 返回当前的模板引擎。
func (l *Loaded) Engine() *Engine { return l.engine.get() }

// SettingGroups 返回主题声明的设置分组。
func (l *Loaded) SettingGroups() []*compiledGroup { return l.settings.groups }

// Registry 持有全部已加载主题与当前启用项。
//
// 它是主题系统的唯一运行时状态：路由、Console 接口与热重载都经它取主题。
// 读远多于写（每个前台请求都要读一次启用主题），故用 RWMutex。
type Registry struct {
	root     string
	logger   *slog.Logger
	fallback *Loaded

	mu      sync.RWMutex
	themes  map[string]*Loaded
	active  string
	broken  map[string]string
	devMode bool
}

// RegistryOptions 是构造 Registry 的参数。
type RegistryOptions struct {
	// Root 是主题安装目录，通常是 data/themes。
	Root string
	// Builtin 是内置默认主题的文件系统（已剥到主题包根）。
	Builtin fs.FS
	// Logger 记录加载与重载事件。
	Logger *slog.Logger
	// DevMode 为真时每次渲染前检查模板改动并自动重解析。
	DevMode bool
}

// BuiltinName 是内置默认主题的标识。
const BuiltinName = "ink"

// NewRegistry 构造 Registry 并加载内置主题。
//
// 内置主题必须加载成功，否则整个主题系统没有回退可用——
// 那意味着任何第三方主题缺一个可选模板就会 500。故此处失败即启动失败。
// 这里加载的**始终是二进制里那一份**：它是回退链的末端。
// 磁盘上的副本由 LoadInstalled 顶上来（见 loadBuiltinFromDisk）。
func NewRegistry(opts *RegistryOptions) (*Registry, error) {
	r := &Registry{
		root:    opts.Root,
		logger:  opts.Logger,
		themes:  map[string]*Loaded{},
		broken:  map[string]string{},
		devMode: opts.DevMode,
	}

	builtin, err := loadFS(BuiltinName, opts.Builtin, "", true)
	if err != nil {
		return nil, fmt.Errorf("加载内置主题: %w", err)
	}
	builtin.Source = SourceEmbedded
	r.fallback = builtin
	r.themes[builtin.Manifest.Name] = builtin
	r.active = builtin.Manifest.Name
	return r, nil
}

// LoadInstalled 扫描并加载 root 下已安装的主题。
//
// 单个主题加载失败不影响其余：一个坏主题不该让整站起不来，
// 它会被记进 broken 并在后台列表里标出原因。
func (r *Registry) LoadInstalled() error {
	names, err := ListInstalled(r.root)
	if err != nil {
		return err
	}
	for _, name := range names {
		if name == BuiltinName {
			if err := r.loadBuiltinFromDisk(); err != nil {
				// 磁盘那份坏了不影响服务：二进制里那份还在 themes 里顶着，
				// 只是要在后台把原因说清楚，否则改坏模板的人会以为改动「没生效」。
				r.recordBroken(name, err.Error())
				if r.logger != nil {
					r.logger.Warn("内置主题的磁盘副本不可用，改用二进制里的原版",
						slog.Any("error", err))
				}
			}
			continue
		}
		if err := r.Load(name); err != nil {
			r.recordBroken(name, err.Error())
			if r.logger != nil {
				r.logger.Warn("主题加载失败", slog.String("theme", name), slog.Any("error", err))
			}
		}
	}
	return nil
}

// loadBuiltinFromDisk 用 data/themes/<内置主题名> 下的副本顶替注册表里的内置主题。
//
// 顶替而不是并列：内置主题只能有一份在生效，否则「当前启用的是哪个 ink」会变得说不清。
// 副本缺模板时仍回退到二进制那份（fallback 链不变），所以删掉一个文件不会让站点 500。
// 它依然是 Builtin（不可删），只是多了一个「可改」的属性。
func (r *Registry) loadBuiltinFromDisk() error {
	if err := r.Load(BuiltinName); err != nil {
		return err
	}
	if r.logger != nil {
		r.logger.Info("内置主题以磁盘副本为准",
			slog.String("dir", filepath.Join(r.root, BuiltinName)))
	}
	return nil
}

// Load 加载（或重新加载）一个已安装主题。
//
// 加载内置主题名（data/themes/<BuiltinName>）时，结果仍然是**内置**主题：
// 不可删除、排在列表最前、当别的主题的回退目标，只是来源标成磁盘。
// 这一点必须在这里保证：「重新载入模板」走的也是这个方法，
// 漏掉就会被悄悄降级成一个可以卸载的普通主题。
func (r *Registry) Load(name string) error {
	if !namePattern.MatchString(name) {
		return fmt.Errorf("%w：非法主题名 %q", ErrInvalidPackage, name)
	}
	dir := filepath.Join(r.root, name)
	if _, err := os.Stat(dir); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ErrNotFound
		}
		return fmt.Errorf("检查主题目录: %w", err)
	}

	isBuiltin := name == BuiltinName
	loaded, err := loadFS(name, os.DirFS(dir), dir, isBuiltin)
	if err != nil {
		return err
	}
	if loaded.Manifest.Name != name {
		return fmt.Errorf("%w：%s 声明的 name 为 %q，与目录名不一致",
			ErrInvalidPackage, name, loaded.Manifest.Name)
	}
	if isBuiltin {
		loaded.Source = SourceDisk
	}
	loaded.Engine().SetFallback(r.fallback.Engine())

	r.mu.Lock()
	r.themes[name] = loaded
	delete(r.broken, name)
	r.mu.Unlock()
	return nil
}

// loadFS 从一个主题文件系统构造 Loaded。
//
// 不接收 devMode：模板是否随请求重解析由 Registry 调用 reload 闭包决定，
// 这个函数只负责把 build 闭包备好。参数留在这里只会成为一个没人读的开关。
func loadFS(name string, fsys fs.FS, dir string, builtin bool) (*Loaded, error) {
	manifest, err := readManifestFS(fsys)
	if err != nil {
		return nil, err
	}
	templatesFS, err := fs.Sub(fsys, DirTemplates)
	if err != nil {
		return nil, fmt.Errorf("%w：缺少 %s 目录", ErrInvalidPackage, DirTemplates)
	}
	names, err := collectTemplateNames(templatesFS)
	if err != nil {
		return nil, err
	}
	if tmplErr := validateTemplates(names); tmplErr != nil {
		return nil, tmplErr
	}

	build := func() (*Engine, error) {
		// 每次重建都重新遍历并读取目录：作者可能新增了 partial，
		// 而内存文件系统与磁盘文件系统都能这样重读，无需分支。
		return parseEngine(name, templatesFS, baseFuncs())
	}
	engine, err := build()
	if err != nil {
		return nil, err
	}

	decl, err := readSettingsFS(fsys)
	if err != nil {
		return nil, err
	}
	compiled, err := compileSettings(manifest.Name, decl)
	if err != nil {
		return nil, err
	}

	loaded := &Loaded{
		Manifest:  *manifest,
		Builtin:   builtin,
		Dir:       dir,
		FS:        fsys,
		Templates: names,
		engine:    &reloadable{current: engine, build: build},
		settings:  compiled,
	}
	if staticFS, err := fs.Sub(fsys, DirStatic); err == nil {
		if _, statErr := fs.Stat(staticFS, "."); statErr == nil {
			loaded.Static = staticFS
			loaded.AssetVersion = assetVersion(staticFS)
		}
	}
	return loaded, nil
}

// assetVersion 给静态资源算一个随文件变化的版本号。
//
// 主题的静态资源带 1 小时的缓存头。没有版本号时，主题升级或「恢复出厂」之后
// 浏览器会在最长一小时内继续用旧的 CSS —— HTML 已经是新版、样式还是上一版，
// 看上去就像主题坏了。把版本号挂在 URL 上，文件一变 URL 就变，缓存自然失效。
//
// 用「路径 + 大小 + 修改时间」而不是文件内容：静态目录里有 3.8 MB 的字体切片，
// 为了算个版本号把它们全读一遍，启动会明显变慢，而这三样足以捕捉任何一次改动。
func assetVersion(staticFS fs.FS) string {
	hash := fnv.New32a()
	// 遍历失败就地返回、由调用方丢弃：版本号只用于缓存失效，
	// 拿不到完整清单时最坏的结果是浏览器多缓存一会儿 —— 与没有这个字段时一样。
	_ = fs.WalkDir(staticFS, ".", func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, statErr := entry.Info()
		if statErr != nil {
			return statErr
		}
		fmt.Fprintf(hash, "%s|%d|%d\n", p, info.Size(), info.ModTime().UnixNano())
		return nil
	})
	return strconv.FormatUint(uint64(hash.Sum32()), 36)
}

// Active 返回当前启用的主题。
//
// 启用项不可用时回退到内置主题：站长删掉正在用的主题目录后，
// 站点应该退回默认样式继续服务，而不是整站 500。
func (r *Registry) Active() *Loaded {
	r.mu.RLock()
	loaded, ok := r.themes[r.active]
	r.mu.RUnlock()
	if !ok {
		return r.fallback
	}
	return loaded
}

// ActiveName 返回当前启用的主题名。
func (r *Registry) ActiveName() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.active
}

// Fallback 返回内置默认主题。
func (r *Registry) Fallback() *Loaded { return r.fallback }

// Get 按名称取已加载主题。
func (r *Registry) Get(name string) (*Loaded, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	loaded, ok := r.themes[name]
	return loaded, ok
}

// Activate 切换启用主题。调用方负责持久化。
func (r *Registry) Activate(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.themes[name]; !ok {
		return ErrNotFound
	}
	r.active = name
	return nil
}

// Unload 从注册表移除一个主题（不删磁盘文件）。
func (r *Registry) Unload(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if name == BuiltinName {
		return
	}
	delete(r.themes, name)
	delete(r.broken, name)
	if r.active == name {
		r.active = BuiltinName
	}
}

// List 返回全部已加载主题，内置主题排在最前，其余按名称排序。
func (r *Registry) List() []*Loaded {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Loaded, 0, len(r.themes))
	for _, loaded := range r.themes {
		out = append(out, loaded)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Builtin != out[j].Builtin {
			return out[i].Builtin
		}
		return out[i].Manifest.Name < out[j].Manifest.Name
	})
	return out
}

// Broken 返回加载失败的主题及其原因。
func (r *Registry) Broken() map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]string, len(r.broken))
	for k, v := range r.broken {
		out[k] = v
	}
	return out
}

// recordBroken 记录一个加载失败的主题。
func (r *Registry) recordBroken(name, reason string) {
	r.mu.Lock()
	r.broken[name] = reason
	r.mu.Unlock()
}

// DevMode 报告是否处于开发模式。
func (r *Registry) DevMode() bool { return r.devMode }

// Reload 重新解析指定主题的模板，供开发模式与后台手动触发使用。
func (r *Registry) Reload(name string) error {
	loaded, ok := r.Get(name)
	if !ok {
		return ErrNotFound
	}
	// 只有落在磁盘上的主题能重载：二进制里那份重新解析一遍没有任何意义。
	// 内置主题的磁盘副本走这条路（站长改了模板要能立刻看到）。
	if loaded.Dir == "" {
		return nil
	}
	if err := loaded.engine.reload(); err != nil {
		return err
	}
	loaded.Engine().SetFallback(r.fallback.Engine())
	if r.logger != nil {
		r.logger.Debug("主题模板已重载", slog.String("theme", name))
	}
	return nil
}

// ---------- 开发模式热重载 ----------

// watchInterval 是开发模式下检查模板改动的间隔。
//
// 用轮询而非 fsnotify：为一个开发期便利引入跨平台文件监听依赖不划算，
// 而且 Windows 上的目录监听在网络盘与部分编辑器的「写临时文件再改名」下并不可靠。
const watchInterval = time.Second

// Watch 在开发模式下轮询主题目录，检测到改动即重解析模板。
//
// 只监控当前启用的非内置主题：其余主题改了也不影响任何人看到的页面。
func (r *Registry) Watch(ctx context.Context) {
	if !r.devMode {
		return
	}
	if r.logger != nil {
		r.logger.Info("主题开发模式已启用，模板改动将自动重载",
			slog.Duration("interval", watchInterval))
	}

	ticker := time.NewTicker(watchInterval)
	defer ticker.Stop()
	fingerprints := map[string]string{}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			loaded := r.Active()
			if loaded.Dir == "" {
				continue
			}
			current, err := fingerprint(filepath.Join(loaded.Dir, DirTemplates))
			if err != nil {
				continue
			}

			name := loaded.Manifest.Name
			previous, seen := fingerprints[name]
			fingerprints[name] = current
			switch {
			case !seen:
				// 首轮只记录基线：启动时刚解析过，没必要立刻再解析一遍。
				continue
			case previous == current:
				continue
			}

			if err := r.Reload(name); err != nil {
				if r.logger != nil {
					r.logger.Warn("主题模板重载失败，仍使用上一版",
						slog.String("theme", name), slog.Any("error", err))
				}
				continue
			}
			if r.logger != nil {
				r.logger.Info("主题模板已重载", slog.String("theme", name))
			}
		}
	}
}

// fingerprint 计算目录下全部模板的「路径+大小+修改时间」指纹。
//
// 不读文件内容：开发时每秒一次全量哈希在大主题上会有明显的磁盘开销，
// 而大小与 mtime 的组合足以捕捉编辑器的保存动作。
func fingerprint(dir string) (string, error) {
	var sb []byte
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, statErr := d.Info()
		if statErr != nil {
			return statErr
		}
		sb = append(sb, p...)
		sb = append(sb, byte('|'))
		sb = append(sb, []byte(info.ModTime().UTC().Format(time.RFC3339Nano))...)
		sb = append(sb, byte('|'))
		sb = append(sb, []byte(itoa(int(info.Size())))...)
		sb = append(sb, byte('\n'))
		return nil
	})
	if err != nil {
		return "", err
	}
	return string(sb), nil
}
