package theme

import (
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"
)

// 错误哨兵。
var (
	// ErrInvalidPackage 表示主题包不符合规范。
	ErrInvalidPackage = errors.New("主题包不合法")
	// ErrTemplateNotFound 表示模板在主题与回退主题中都不存在。
	ErrTemplateNotFound = errors.New("模板不存在")
	// ErrNotFound 表示主题未安装。
	ErrNotFound = errors.New("主题不存在")
	// ErrAlreadyExists 表示同名主题已安装。
	ErrAlreadyExists = errors.New("同名主题已存在")
	// ErrBuiltin 表示该操作不允许作用于内置主题。
	ErrBuiltin = errors.New("内置主题不可删除")
)

// 必需模板（agent.md §4.4）。缺任何一个，主题都装不上。
var requiredTemplates = []string{"index.html", "post.html", "page.html", "404.html"}

// 可选模板：缺省时回退到内置默认主题的同名模板。
//
// 顺序按访客实际会走的流程排（登录 → 注册 → 找回 → 重置 → 账户），
// 后台展示模板提供情况时也就按这个顺序列出来。
var optionalTemplates = []string{
	"category.html", "tag.html", "archive.html", "search.html", "author.html",
	"login.html", "register.html", "forgot-password.html", "reset-password.html", "account.html",
}

// RequiredTemplates 返回必需模板名，供接口与文档引用。
func RequiredTemplates() []string { return append([]string(nil), requiredTemplates...) }

// OptionalTemplates 返回可选模板名。
func OptionalTemplates() []string { return append([]string(nil), optionalTemplates...) }

// 模板目录内的约定子目录（Hugo 风格，agent.md §4.1）。
const (
	// dirLayouts 存放外层骨架，页面模板用 {{ template "layouts/base.html" . }} 之类引用。
	dirLayouts = "layouts"
	// dirPartials 存放可复用片段。
	dirPartials = "partials"
)

// Engine 是一套已解析的主题模板。
//
// 用 html/template 而非 pongo2 / jet：主题是第三方代码，XSS 面就在主题里，
// 只有 html/template 做上下文感知转义（agent.md §4.1）。代价是模板作者体验一般，
// 用 Hugo 风格的 layout / partial 约定与函数库补齐。
//
// 并发安全：解析在构造期一次完成，Render 只读。
type Engine struct {
	// name 是主题标识，用于错误信息。
	name string
	// pages 是每个页面模板各自独立的模板集合，键为页面模板名。
	//
	// 为什么一个页面一套而不是全主题共用一套：Go 的模板没有继承，
	// layout 约定只能靠「页面定义 main 块、骨架回调它」实现，而同一套集合里
	// 模板名全局唯一——index.html 与 post.html 都定义 "main" 时后者会覆盖前者，
	// 表现为「所有页面都渲染成最后解析的那个」。按页面分集合是 Hugo 的做法，
	// 代价是共享模板被解析多次（只在启动或重载时发生，不在请求路径上）。
	pages map[string]*template.Template
	// fallback 是缺失模板时的回退引擎，通常是内置默认主题；可为 nil。
	fallback *Engine
}

// 共享模板的目录前缀：它们不作为页面入口，而是被每个页面集合各解析一份。
var sharedPrefixes = []string{dirLayouts + "/", dirPartials + "/"}

// isShared 报告模板是否属于共享目录。
func isShared(name string) bool {
	for _, prefix := range sharedPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// parseEngine 从模板目录解析出一套引擎。
//
// 解析范围：templates/ 下的全部 .html 文件，按相对路径命名（如 post.html、
// layouts/base.html、partials/header.html）。这样模板之间用路径互相引用，
// 与 Hugo 的心智模型一致。
func parseEngine(name string, templatesFS fs.FS, funcs template.FuncMap) (*Engine, error) {
	names, err := collectTemplateNames(templatesFS)
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("%w：%s 下没有任何 .html 模板", ErrInvalidPackage, DirTemplates)
	}

	sources := make(map[string]string, len(names))
	var shared, pages []string
	for _, n := range names {
		data, readErr := fs.ReadFile(templatesFS, n)
		if readErr != nil {
			return nil, fmt.Errorf("读取模板 %s: %w", n, readErr)
		}
		sources[n] = string(data)
		if isShared(n) {
			shared = append(shared, n)
		} else {
			pages = append(pages, n)
		}
	}
	if len(pages) == 0 {
		return nil, fmt.Errorf("%w：%s 下没有页面模板（layouts 与 partials 之外的 .html）",
			ErrInvalidPackage, DirTemplates)
	}

	engine := &Engine{name: name, pages: make(map[string]*template.Template, len(pages))}
	for _, page := range pages {
		// 根模板名取一个不会与任何主题模板重名的哨兵值。
		// html/template 不允许「先以某名建根模板、再以同名 New 并 Parse」——
		// 那会让该名下已完成转义分析的模板被替换，执行时报 "is an incomplete template"。
		set := template.New(rootTemplateName).Funcs(funcs)
		// 先解析共享模板，再解析页面本身：页面里的 define 得以覆盖同名共享块。
		for _, s := range append(append([]string{}, shared...), page) {
			if _, parseErr := set.New(s).Parse(sources[s]); parseErr != nil {
				// 原样带上 html/template 的报错：它已经指明了行号与出错的动作。
				return nil, fmt.Errorf("%w：解析模板 %s: %w", ErrInvalidPackage, s, parseErr)
			}
		}
		engine.pages[page] = set
	}
	return engine, nil
}

// rootTemplateName 是每个页面集合的根模板名。
//
// 带前后下划线是为了不可能与 collectTemplateNames 产出的 .html 路径相撞。
const rootTemplateName = "__lumo_theme__"

// collectTemplateNames 列出模板目录下的全部 .html 文件，按相对路径排序。
//
// 排序是为了让解析顺序稳定：模板重定义时后者覆盖前者，顺序不定会导致
// 同一个主题包在不同机器上渲染出不同结果。
func collectTemplateNames(fsys fs.FS) ([]string, error) {
	var names []string
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".html") {
			return nil
		}
		names = append(names, p)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("遍历模板目录: %w", err)
	}
	sort.Strings(names)
	return names, nil
}

// SetFallback 设置回退引擎：本主题缺失的模板转由它渲染（agent.md §4.4）。
func (e *Engine) SetFallback(fallback *Engine) {
	if fallback != e {
		e.fallback = fallback
	}
}

// Has 报告本主题自身是否提供该页面模板（不含回退）。
func (e *Engine) Has(name string) bool {
	_, ok := e.pages[name]
	return ok
}

// Lookup 返回渲染 name 时实际生效的引擎：本主题有就是自己，否则找回退。
func (e *Engine) Lookup(name string) (*Engine, bool) {
	if e.Has(name) {
		return e, true
	}
	if e.fallback != nil {
		return e.fallback.Lookup(name)
	}
	return nil, false
}

// Render 渲染指定模板。
//
// 模板缺失时按 agent.md §4.4 回退到内置默认主题的同名模板，而不是报错或渲染空白——
// 分类、标签、归档、搜索、作者这五个模板对主题作者是可选的。
//
// 注意回退是**整页**回退而非按块回退：缺 category.html 时整页都由默认主题渲染，
// 页头页脚也跟着变。按块混合会得到一个两种设计语言拼起来的页面，比整页回退更糟。
func (e *Engine) Render(w io.Writer, name string, data any) error {
	target, ok := e.Lookup(name)
	if !ok {
		return fmt.Errorf("%w：%s（主题 %s）", ErrTemplateNotFound, name, e.name)
	}
	if err := target.pages[name].ExecuteTemplate(w, name, data); err != nil {
		return fmt.Errorf("渲染模板 %s（主题 %s）: %w", name, target.name, err)
	}
	return nil
}

// Names 返回本主题自身提供的页面模板名，已排序。
func (e *Engine) Names() []string {
	out := make([]string, 0, len(e.pages))
	for n := range e.pages {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// validateTemplates 校验模板集合是否满足主题规范。
func validateTemplates(names []string) error {
	present := make(map[string]bool, len(names))
	for _, n := range names {
		present[n] = true
	}
	var missing []string
	for _, n := range requiredTemplates {
		if !present[n] {
			missing = append(missing, n)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("%w：缺少必需模板 %s", ErrInvalidPackage, strings.Join(missing, "、"))
	}
	return nil
}

// TemplateStatus 描述一个模板在主题中的可用情况，供后台展示。
type TemplateStatus struct {
	Name string `json:"name"`
	// Required 为真表示这是四个必需模板之一。
	Required bool `json:"required"`
	// Provided 为真表示主题自己提供了它；否则渲染时回退到默认主题。
	Provided bool `json:"provided"`
}

// TemplateStatuses 汇总必需与可选模板的提供情况。
func TemplateStatuses(names []string) []TemplateStatus {
	present := make(map[string]bool, len(names))
	for _, n := range names {
		present[n] = true
	}
	out := make([]TemplateStatus, 0, len(requiredTemplates)+len(optionalTemplates))
	for _, n := range requiredTemplates {
		out = append(out, TemplateStatus{Name: n, Required: true, Provided: present[n]})
	}
	for _, n := range optionalTemplates {
		out = append(out, TemplateStatus{Name: n, Provided: present[n]})
	}
	return out
}

// PageTemplates 返回主题提供的 page-*.html 模板名（WordPress 模式，agent.md §4.2）。
//
// 独立页面可以在后台选择其中之一；列表由主题决定，Console 据此渲染下拉框。
func PageTemplates(names []string) []string {
	out := []string{}
	for _, n := range names {
		base := path.Base(n)
		if base != n {
			// 只认模板根目录下的 page-*.html，不扫 layouts / partials。
			continue
		}
		if strings.HasPrefix(n, "page-") && strings.HasSuffix(n, ".html") {
			out = append(out, strings.TrimSuffix(n, ".html"))
		}
	}
	sort.Strings(out)
	return out
}

// reloadable 把「重新解析一次模板」的能力包起来，供开发模式热重载使用。
//
// 单独一个结构体而不是给 Engine 加锁：生产模式下 Engine 完全只读，
// 不该为了开发期的便利给每次渲染都加一次锁竞争。
type reloadable struct {
	mu      sync.RWMutex
	current *Engine
	build   func() (*Engine, error)
}

// get 返回当前引擎。
func (r *reloadable) get() *Engine {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.current
}

// reload 重新构建引擎；失败时保留旧引擎，让站点在改坏模板时仍能提供服务。
func (r *reloadable) reload() error {
	next, err := r.build()
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.current = next
	r.mu.Unlock()
	return nil
}
