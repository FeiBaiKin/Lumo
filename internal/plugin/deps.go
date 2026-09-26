package plugin

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Dependency 是 plugin.yaml 里 spec.dependencies 的一项：本插件要用到的另一个插件。
//
// 与 spec.requires 分工不同：requires 说的是「要哪个版本的 Lumo」，这里说的是「要哪个插件」。
type Dependency struct {
	// Name 是被依赖插件的标识。
	Name string `yaml:"name" json:"name"`
	// Version 是版本范围，如 >=1.0.0、^1.2、~1.2.3、1.2.*；留空表示不限。写法见 version.go。
	Version string `yaml:"version" json:"version"`

	// need 是解析后的范围。
	need versionRange
}

// maxDependencies 是一个插件能声明的依赖数。
const maxDependencies = 20

// ErrMissingDeps 表示插件依赖的别的插件没就绪，因而不能启用。
var ErrMissingDeps = errors.New("依赖的插件没有就绪")

// 连带停用时记下的原因前缀与拼法。
//
// 依赖的插件重新启用后，要把这些原因改写成「可以再次启用」，所以拼法得是能认回来的固定形态。
const dependencyReasonPrefix = "依赖的插件 "

func dependencyStoppedReason(name, verb string) string {
	return dependencyReasonPrefix + name + " 已" + verb
}

// normalizeDependencies 校验 spec.dependencies。
//
// 只查形态：被依赖的插件在不在、版本够不够，那要等启用时才知道（清单校验发生在安装那一刻，
// 而依赖可以在它之后安装）。
func normalizeDependencies(deps []Dependency, self string) error {
	if len(deps) == 0 {
		return nil
	}
	if len(deps) > maxDependencies {
		return fmt.Errorf("%w：依赖最多 %d 个", ErrInvalidPackage, maxDependencies)
	}
	seen := map[string]bool{}
	for i := range deps {
		d := &deps[i]
		name := strings.TrimSpace(d.Name)
		switch {
		case name == "":
			return fmt.Errorf("%w：依赖里有一项没写 name", ErrInvalidPackage)
		case len(name) > maxNameLength:
			return fmt.Errorf("%w：依赖名 %q 超过 %d 个字符", ErrInvalidPackage, name, maxNameLength)
		case !namePattern.MatchString(name):
			return fmt.Errorf("%w：依赖名 %q 须为 DNS-1123（小写字母、数字与连字符）", ErrInvalidPackage, name)
		case name == self:
			// 自依赖会让启用判据永远不成立，插件再也启用不了。
			return fmt.Errorf("%w：插件不能依赖自己", ErrInvalidPackage)
		case seen[name]:
			return fmt.Errorf("%w：依赖 %q 重复", ErrInvalidPackage, name)
		}
		seen[name] = true
		d.Name = name
		d.Version = strings.TrimSpace(d.Version)
		need, err := parseRange(d.Version)
		if err != nil {
			return fmt.Errorf("%w：依赖 %s 的 version %q 不是有效的版本范围：%w", ErrInvalidPackage, name, d.Version, err)
		}
		d.need = need
	}
	return nil
}

// Dependencies 返回插件声明的依赖。
func (l *Loaded) Dependencies() []Dependency { return l.Manifest.Spec.Dependencies }

// installedVersion 取插件当前版本的解析结果；清单校验保证它可解析，解析不了按出错处理。
func (l *Loaded) installedVersion() (semver, error) {
	return parseVersion(l.Manifest.Spec.Version)
}

// DepStatus 是一条依赖的现状。
type DepStatus struct {
	Dep Dependency
	// Installed 为真表示被依赖的插件已安装。
	Installed bool
	// InstalledVersion 是已安装那个插件的版本；没装时为空串。
	InstalledVersion string
	// Enabled 为真表示已安装且已启用。
	Enabled bool
	// Satisfied 为真表示这条依赖已就绪：装了、启用了、版本也够。
	Satisfied bool
}

// DepStatuses 返回各条依赖的现状，按声明顺序。
func (r *Registry) DepStatuses(deps []Dependency) []DepStatus {
	if len(deps) == 0 {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]DepStatus, 0, len(deps))
	for _, d := range deps {
		st := DepStatus{Dep: d}
		if dep, ok := r.plugins[d.Name]; ok {
			st.Installed = true
			st.InstalledVersion = dep.Manifest.Spec.Version
			st.Enabled = dep.Enabled
			if have, err := dep.installedVersion(); err == nil {
				st.Satisfied = dep.Enabled && d.need.matches(have)
			}
		}
		out = append(out, st)
	}
	return out
}

// unmetDeps 返回一组依赖里没满足的，每项是一句给站长看的话。
func (r *Registry) unmetDeps(deps []Dependency) []string {
	var out []string
	for _, st := range r.DepStatuses(deps) {
		switch {
		case !st.Installed:
			out = append(out, fmt.Sprintf("缺少插件 %s", st.Dep.Name))
		case !st.Enabled:
			out = append(out, fmt.Sprintf("插件 %s 没有启用", st.Dep.Name))
		case !st.Satisfied:
			out = append(out, fmt.Sprintf("插件 %s 的版本 %s 不满足 %s", st.Dep.Name, st.InstalledVersion, st.Dep.need))
		}
	}
	return out
}

// dependentsOf 返回声明依赖 name 的已安装插件，按标识排序；enabledOnly 为真时只取启用中的。
//
// 「启用中」在锁内判断：调用方（连带停用、后台确认框）拿到的必须是一份一致的名单，
// 不能是过滤完再被人改过的旧快照。
func (r *Registry) dependentsOf(name string, enabledOnly bool) []*Loaded {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*Loaded
	for _, loaded := range r.plugins {
		if enabledOnly && !loaded.Enabled {
			continue
		}
		for _, d := range loaded.Manifest.Spec.Dependencies {
			if d.Name == name {
				out = append(out, loaded)
				break
			}
		}
	}
	sortLoaded(out)
	return out
}

// sortLoaded 按标识排序。
func sortLoaded(list []*Loaded) {
	sort.Slice(list, func(i, j int) bool { return list[i].ID() < list[j].ID() })
}

// EnabledDependents 返回依赖 name 且当前启用中的插件标识，按标识排序。
//
// 停用或卸载 name 时，这些正是会被连带停用的插件；后台拿它在确认框里列出后果。
// 没有时返回空切片而不是 nil：接口那边 JSON 给 [] 比 null 好用。
func (r *Registry) EnabledDependents(name string) []string {
	out := []string{}
	for _, dep := range r.dependentsOf(name, true) {
		out = append(out, dep.ID())
	}
	return out
}

// disableDependents 连带停用依赖 name 的插件，并记下原因。
//
// 站长在确认框里已经看过依赖它的插件清单；停用照常进行，依赖方跟着停用而不是留在
// 半坏的状态里——与「连续崩溃自动停用」同一套处置，列表里能直接看到原因。
func (r *Registry) disableDependents(ctx context.Context, name, verb string) {
	for _, dep := range r.dependentsOf(name, true) {
		r.Suspend(ctx, dep.ID(), dependencyStoppedReason(name, verb))
	}
}

// restoreDependentReasons 把「因为 name 停用而跟着停用」的插件的原因改写成可再次启用。
//
// 不改启用状态：依赖回来了不等于站长还想让它跑，那一步仍由站长点。
func (r *Registry) restoreDependentReasons(ctx context.Context, name string) {
	prefix := dependencyReasonPrefix + name + " 已"
	for _, dep := range r.dependentsOf(name, false) {
		// 只在它还停着、且原因是「被这次停用带的」时改写；已启用的原因本来就是空的。
		if dep.Enabled || !strings.HasPrefix(dep.DisabledReason, prefix) {
			continue
		}
		reason := fmt.Sprintf("%s%s 已重新启用，可以再次启用本插件", dependencyReasonPrefix, name)
		if err := r.persist(ctx, dep.ID(), State{Reason: reason}); err != nil {
			r.warn("记录插件停用状态失败", dep.ID(), err)
			continue
		}
		r.mu.Lock()
		if current, ok := r.plugins[dep.ID()]; ok {
			current.DisabledReason = reason
		}
		r.mu.Unlock()
	}
}

// startOrder 返回启用中插件的启动顺序：依赖先起，用它的后起；同层按标识。
//
// 插件之间不能互相调用，故这个顺序只影响日志与启动耗时的可读性；真出现环时
// 两个插件谁也启用不了（各自的依赖都没启用），轮不到这里处理。
func (r *Registry) startOrder() []*Loaded {
	enabled := r.Enabled()
	index := make(map[string]*Loaded, len(enabled))
	for _, loaded := range enabled {
		index[loaded.ID()] = loaded
	}
	const (
		visiting = 1
		done     = 2
	)
	state := make(map[string]int, len(enabled))
	out := make([]*Loaded, 0, len(enabled))
	var visit func(*Loaded)
	visit = func(loaded *Loaded) {
		if state[loaded.ID()] != 0 {
			return // 已排好，或是正访问中的环
		}
		state[loaded.ID()] = visiting
		for _, d := range loaded.Manifest.Spec.Dependencies {
			if dep, ok := index[d.Name]; ok {
				visit(dep)
			}
		}
		state[loaded.ID()] = done
		out = append(out, loaded)
	}
	for _, loaded := range enabled {
		visit(loaded)
	}
	return out
}
