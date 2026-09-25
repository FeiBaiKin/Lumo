package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/microcosm-cc/bluemonday"

	"github.com/FeiBaiKin/lumo/internal/content"
	"github.com/FeiBaiKin/lumo/internal/hooks"
	"github.com/FeiBaiKin/lumo/internal/plugin/wasm"
)

// StaticDir 是插件包里放前台与后台静态文件的目录。
const StaticDir = "static"

// AssetsPath 是插件静态文件的访问前缀：/plugin-assets/<插件>/<static 之下的路径>。
const AssetsPath = "/plugin-assets"

// FrontendDecl 是 plugin.yaml 里的 spec.frontend：插件往前台页面里放的东西。都要声明 capabilities.frontend。
type FrontendDecl struct {
	// Styles 是放进页头的样式表，包里 static/ 下的 .css。
	Styles []string `yaml:"styles" json:"styles"`
	// Scripts 是放在页尾的脚本，包里 static/ 下的 .js，defer 加载。
	Scripts []string `yaml:"scripts" json:"scripts"`
	// Slots 是由后端代码渲染内容的插槽，取值见 hooks.Slots。
	Slots []string `yaml:"slots" json:"slots"`
	// Widgets 是插件提供的侧栏小组件，站长在主题的侧边栏设置里选用。
	Widgets []WidgetDecl `yaml:"widgets" json:"widgets"`
	// Shortcodes 是插件提供的短代码：作者在正文里写 [名字 参数="值"]，前台展开成插件渲染的内容。
	Shortcodes []ShortcodeDecl `yaml:"shortcodes" json:"shortcodes"`
}

// WidgetDecl 是一个侧栏小组件。
type WidgetDecl struct {
	Name        string `yaml:"name" json:"name"`
	Label       string `yaml:"label" json:"label"`
	Description string `yaml:"description" json:"description"`
}

// ShortcodeDecl 是一个短代码。
type ShortcodeDecl struct {
	Name        string `yaml:"name" json:"name"`
	Description string `yaml:"description" json:"description"`
}

const (
	maxFrontendAssets = 10
	maxWidgets        = 10
	maxShortcodes     = 20
	// fragmentTimeout 是插槽、小组件与短代码各自的时限：它们都在访客打开页面时同步执行。
	fragmentTimeout = 200 * time.Millisecond
	// maxFragment 是一段插件片段的字节上限。
	maxFragment = 64 << 10
	// maxShortcodesPerPage 是一页里最多展开的短代码数，多出来的原样保留。
	maxShortcodesPerPage = 20
)

var shortcodeNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

// IsZero 判断插件有没有往前台放任何东西。
func (f *FrontendDecl) IsZero() bool {
	return len(f.Styles) == 0 && len(f.Scripts) == 0 && len(f.Slots) == 0 && len(f.Widgets) == 0 && len(f.Shortcodes) == 0
}

// normalize 校验 spec.frontend：要声明前台能力；插槽、小组件与短代码还要有后端去渲染。
func (f *FrontendDecl) normalize(caps *Capabilities, hasBackend bool) error {
	if f.IsZero() {
		return nil
	}
	if !caps.Frontend {
		return fmt.Errorf("%w：往前台放东西（spec.frontend）要声明 capabilities.frontend: true", ErrInvalidPackage)
	}
	for label, list := range map[string][]string{"styles": f.Styles, "scripts": f.Scripts} {
		if len(list) > maxFrontendAssets {
			return fmt.Errorf("%w：spec.frontend.%s 最多 %d 个", ErrInvalidPackage, label, maxFrontendAssets)
		}
		ext := map[string]string{"styles": ".css", "scripts": ".js"}[label]
		for _, file := range list {
			if err := checkStaticPath(file, ext); err != nil {
				return fmt.Errorf("%w：spec.frontend.%s 里的 %q %w", ErrInvalidPackage, label, file, err)
			}
		}
	}
	if !hasBackend && (len(f.Slots) > 0 || len(f.Widgets) > 0 || len(f.Shortcodes) > 0) {
		return fmt.Errorf("%w：插槽、小组件与短代码要由后端代码渲染，请同时声明 spec.runtime: %s", ErrInvalidPackage, RuntimeWasm)
	}
	for _, slot := range f.Slots {
		if !slices.Contains(hooks.Slots, slot) {
			return fmt.Errorf("%w：spec.frontend.slots 里的 %q 不是宿主提供的插槽（%s）", ErrInvalidPackage, slot, strings.Join(hooks.Slots, "、"))
		}
	}
	slices.Sort(f.Slots)
	f.Slots = slices.Compact(f.Slots)
	if len(f.Widgets) > maxWidgets {
		return fmt.Errorf("%w：小组件最多 %d 个", ErrInvalidPackage, maxWidgets)
	}
	seen := map[string]bool{}
	for i := range f.Widgets {
		w := &f.Widgets[i]
		if !cronNamePattern.MatchString(w.Name) || len(w.Name) > 64 || seen[w.Name] {
			return fmt.Errorf("%w：小组件名 %q 只能用小写字母、数字与连字符，且不能重复", ErrInvalidPackage, w.Name)
		}
		seen[w.Name] = true
		if w.Label == "" {
			w.Label = w.Name
		}
	}
	if len(f.Shortcodes) > maxShortcodes {
		return fmt.Errorf("%w：短代码最多 %d 个", ErrInvalidPackage, maxShortcodes)
	}
	seen = map[string]bool{}
	for _, sc := range f.Shortcodes {
		if !shortcodeNamePattern.MatchString(sc.Name) || seen[sc.Name] {
			return fmt.Errorf("%w：短代码名 %q 须以小写字母开头、只含小写字母数字与连字符，且不能重复", ErrInvalidPackage, sc.Name)
		}
		seen[sc.Name] = true
	}
	return nil
}

// checkStaticPath 校验一个指向包里 static/ 的路径：不越界、扩展名对得上。
func checkStaticPath(file, ext string) error {
	clean := path.Clean(file)
	switch {
	case clean != file || !strings.HasPrefix(clean, StaticDir+"/"):
		return fmt.Errorf("须是 %s/ 下的相对路径", StaticDir)
	case ext != "" && !strings.EqualFold(path.Ext(clean), ext):
		return fmt.Errorf("须是 %s 文件", ext)
	}
	return nil
}

// validateStaticFiles 核对清单引用的静态文件都在包里。
func validateStaticFiles(fsys fs.FS, m *Manifest) error {
	files := slices.Concat(m.Spec.Frontend.Styles, m.Spec.Frontend.Scripts)
	for i := range m.Spec.Pages {
		files = append(files, m.Spec.Pages[i].File)
	}
	for _, file := range files {
		info, err := fs.Stat(fsys, file)
		if err != nil || info.IsDir() {
			return fmt.Errorf("%w：清单引用的 %s 不在包里", ErrInvalidPackage, file)
		}
	}
	return nil
}

// frontendPlugins 返回被授予了前台能力、且往前台放了东西的运行中插件，按标识排序。
func (m *Module) frontendPlugins() []*Loaded {
	var out []*Loaded
	for _, loaded := range m.registry.Enabled() {
		if loaded.Granted == nil || !loaded.Granted.Frontend || loaded.Manifest.Spec.Frontend.IsZero() {
			continue
		}
		out = append(out, loaded)
	}
	return out
}

// assetURL 返回插件静态文件的访问地址，带版本号，升级后浏览器不会用旧缓存。
func assetURL(loaded *Loaded, file string) string {
	return AssetsPath + "/" + loaded.ID() + "/" + strings.TrimPrefix(file, StaticDir+"/") + "?v=" + loaded.Manifest.Spec.Version
}

// Slot 实现 app.Frontend。
//
// 页头放各插件的样式表，页尾放各插件的脚本（脚本只能是插件包里的文件，不收内联脚本）；
// 登记了这个插槽的插件再各渲染一段，净化后包在带插件名的容器里，主题与插件脚本都能按它定位。
func (m *Module) Slot(ctx context.Context, slot string, page *hooks.Page) template.HTML {
	var b strings.Builder
	for _, loaded := range m.frontendPlugins() {
		fe := &loaded.Manifest.Spec.Frontend
		if slot == hooks.SlotHead {
			for _, file := range fe.Styles {
				fmt.Fprintf(&b, `<link rel="stylesheet" href="%s" data-plugin="%s">`, html.EscapeString(assetURL(loaded, file)), loaded.ID())
			}
		}
		if slices.Contains(fe.Slots, slot) {
			out := m.renderFragment(ctx, loaded, kindSlot, slot, fragmentPayload{Slot: slot, Page: page})
			if slot == hooks.SlotHead {
				b.WriteString(headPolicy().Sanitize(out))
			} else if out = fragmentPolicy().Sanitize(out); strings.TrimSpace(out) != "" {
				fmt.Fprintf(&b, `<div class="plugin-slot" data-plugin="%s" data-slot="%s">%s</div>`, loaded.ID(), slot, out)
			}
		}
		if slot == hooks.SlotFooter {
			for _, file := range fe.Scripts {
				post := ""
				if page != nil && page.Post != nil {
					post = fmt.Sprintf(` data-post="%d"`, page.Post.ID)
				}
				fmt.Fprintf(&b, `<script defer src="%s" data-plugin="%s" data-api="%s/%s"%s></script>`,
					html.EscapeString(assetURL(loaded, file)), loaded.ID(), RoutesPrefix, loaded.ID(), post)
			}
		}
	}
	return template.HTML(b.String()) //nolint:gosec // 片段已按插件规则净化，其余是宿主拼的标签
}

// Widget 实现 app.Frontend。
func (m *Module) Widget(ctx context.Context, id string, page *hooks.Page) (string, template.HTML, bool) {
	plugin, name, ok := strings.Cut(id, "/")
	if !ok {
		return "", "", false
	}
	for _, loaded := range m.frontendPlugins() {
		if loaded.ID() != plugin {
			continue
		}
		for _, w := range loaded.Manifest.Spec.Frontend.Widgets {
			if w.Name != name {
				continue
			}
			out := fragmentPolicy().Sanitize(m.renderFragment(ctx, loaded, kindWidget, name, fragmentPayload{Widget: name, Page: page}))
			if strings.TrimSpace(out) == "" {
				return "", "", false
			}
			return w.Label, template.HTML(out), true //nolint:gosec // 已净化
		}
	}
	return "", "", false
}

// renderFragment 叫插件渲染一段前台内容；失败、超时或超长都当作没有内容。
func (m *Module) renderFragment(ctx context.Context, loaded *Loaded, kind, name string, payload fragmentPayload) string {
	out, err := m.Invoke(ctx, loaded.ID(), wasm.Request{Type: kind, Name: name, Payload: payload}, fragmentTimeout)
	if err != nil {
		if !wasm.IsCrash(err) {
			m.logger.Info("插件没能渲染前台内容", slog.String("plugin", loaded.ID()),
				slog.String("kind", kind), slog.String("name", name), slog.Any("error", err))
		}
		return ""
	}
	var htmlOut string
	if len(out) == 0 || json.Unmarshal(out, &htmlOut) != nil || len(htmlOut) > maxFragment {
		return ""
	}
	return htmlOut
}

// fragmentPayload 是插槽、小组件与短代码收到的数据。
type fragmentPayload struct {
	Slot   string            `json:"slot,omitempty"`
	Widget string            `json:"widget,omitempty"`
	Name   string            `json:"name,omitempty"`
	Attrs  map[string]string `json:"attrs,omitempty"`
	Page   *hooks.Page       `json:"page"`
}

// fragmentPolicy 是插件片段的净化规则：正文的那一套，再加上表单控件。
//
// 插件要做订阅框、投票一类的小表单；事件属性与内联脚本照旧不给——插件要交互，
// 就在 spec.frontend.scripts 里带一个脚本文件，按 data-plugin 找到自己的元素。
var fragmentPolicy = sync.OnceValue(func() *bluemonday.Policy {
	p := content.NewPolicy()
	p.AllowElements("form", "input", "button", "select", "option", "optgroup", "textarea", "label", "fieldset", "legend",
		"output", "progress", "meter")
	p.AllowAttrs("action", "method").OnElements("form")
	p.AllowAttrs("type", "name", "value", "placeholder", "required", "disabled", "checked", "min", "max", "step",
		"minlength", "maxlength", "pattern", "autocomplete", "readonly", "multiple", "size", "rows", "cols", "for",
		"selected", "label", "form").Globally()
	p.AllowAttrs("aria-label", "aria-hidden", "aria-live", "aria-describedby", "role", "title", "hidden").Globally()
	return p
})

// headPolicy 是 head 插槽的净化规则：只收 meta 与少数几种 link，样式表走 spec.frontend.styles。
var headPolicy = sync.OnceValue(func() *bluemonday.Policy {
	p := bluemonday.NewPolicy()
	p.AllowElements("meta", "link")
	p.AllowAttrs("name", "property", "content").OnElements("meta")
	p.AllowAttrs("rel").Matching(regexp.MustCompile(`^(alternate|preconnect|dns-prefetch|me|author|license)$`)).OnElements("link")
	p.AllowAttrs("href", "type", "title", "hreflang").OnElements("link")
	p.AllowURLSchemes("http", "https")
	p.AllowRelativeURLs(true)
	return p
})

// Shortcodes 实现 app.Frontend：把正文里认得的短代码交给提供它的插件渲染。
//
// 同名短代码由标识排在前面的插件提供。一页最多展开 maxShortcodesPerPage 个，其余原样保留。
func (m *Module) Shortcodes(ctx context.Context, src string, page *hooks.Page) (string, bool) {
	if !strings.Contains(src, "[") {
		return src, false
	}
	owners := map[string]*Loaded{}
	for _, loaded := range m.frontendPlugins() {
		for _, sc := range loaded.Manifest.Spec.Frontend.Shortcodes {
			if _, taken := owners[sc.Name]; !taken {
				owners[sc.Name] = loaded
			}
		}
	}
	if len(owners) == 0 {
		return src, false
	}
	budget := maxShortcodesPerPage
	return expandShortcodes(src, func(name string, attrs map[string]string) (string, bool) {
		loaded := owners[name]
		if loaded == nil || budget == 0 {
			return "", false
		}
		budget--
		out := m.renderFragment(ctx, loaded, kindShortcode, name, fragmentPayload{Name: name, Attrs: attrs, Page: page})
		return fragmentPolicy().Sanitize(out), true
	})
}

// StripShortcodes 实现 app.Frontend：摘要是从正文取的纯文字，里面的短代码只会是一串方括号，
// 去掉认得的那些，[[名字]] 还原成 [名字]；不认得的原样保留。
func (m *Module) StripShortcodes(text string) string {
	if !strings.Contains(text, "[") {
		return text
	}
	known := map[string]bool{}
	for _, loaded := range m.frontendPlugins() {
		for _, sc := range loaded.Manifest.Spec.Frontend.Shortcodes {
			known[sc.Name] = true
		}
	}
	if len(known) == 0 {
		return text
	}
	return strings.TrimSpace(shortcodePattern.ReplaceAllStringFunc(text, func(match string) string {
		sub := shortcodePattern.FindStringSubmatch(match)
		open, closing := sub[1] != "", sub[4] != ""
		switch {
		case !known[sub[2]] || open != closing:
			return match
		case open:
			return match[1 : len(match)-1]
		default:
			return ""
		}
	}))
}

// widgetInfo 是给主题设置里「插件小组件」下拉框用的一项。
type widgetInfo struct {
	// ID 形如 <插件>/<小组件>，存进主题设置。
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Plugin      string `json:"plugin"`
	PluginLabel string `json:"pluginLabel"`
}

// widgets 列出运行中插件提供的小组件。
func (m *Module) widgets() []widgetInfo {
	out := []widgetInfo{}
	for _, loaded := range m.frontendPlugins() {
		for _, w := range loaded.Manifest.Spec.Frontend.Widgets {
			out = append(out, widgetInfo{
				ID: loaded.ID() + "/" + w.Name, Label: w.Label, Description: w.Description,
				Plugin: loaded.ID(), PluginLabel: loaded.Manifest.Spec.DisplayName,
			})
		}
	}
	return out
}

// AssetsHandler 提供启用中插件的 static 目录，挂在 AssetsPath 下。
//
// 插件的文件与站点同源：HTML 与 SVG 带上 CSP sandbox，直接打开时落在一个不透明的源里，
// 碰不到站点的 Cookie 与后台接口；后台的自定义页也靠这一条与管理员会话隔开。
func (m *Module) AssetsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet && req.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		rest := strings.TrimPrefix(req.URL.Path, AssetsPath+"/")
		name, file, ok := strings.Cut(rest, "/")
		loaded, exists := m.registry.Get(name)
		if !ok || !exists || !loaded.Enabled || loaded.FS == nil {
			http.NotFound(w, req)
			return
		}
		clean := path.Clean("/" + file)[1:]
		if clean == "" || clean == "." || strings.HasPrefix(clean, "../") {
			http.NotFound(w, req)
			return
		}
		static, err := fs.Sub(loaded.FS, StaticDir)
		if err != nil {
			http.NotFound(w, req)
			return
		}
		f, err := static.Open(clean)
		if err != nil {
			http.NotFound(w, req)
			return
		}
		defer func() { _ = f.Close() }()
		info, err := f.Stat()
		seeker, seekable := f.(io.ReadSeeker)
		if err != nil || info.IsDir() || !seekable {
			http.NotFound(w, req)
			return
		}
		header := w.Header()
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("Cache-Control", "public, max-age=3600")
		switch strings.ToLower(path.Ext(clean)) {
		case ".html":
			header.Set("Content-Security-Policy", "sandbox allow-scripts allow-forms allow-popups allow-modals allow-downloads")
			header.Set("Cache-Control", "no-cache")
		case ".svg":
			header.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
		}
		// 不用 FileServer：它会把 .../index.html 跳转到目录上
		http.ServeContent(w, req, info.Name(), info.ModTime(), seeker)
	})
}
