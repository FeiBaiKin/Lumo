package app

import (
	"context"
	"html/template"
	"sync"

	"github.com/FeiBaiKin/lumo/internal/hooks"
)

// Frontend 是插件往前台页面里放东西的入口：插槽、侧栏小组件与短代码。
//
// 主题模块在渲染时调用它，插件模块是它的实现；返回的 HTML 已按插件的规则净化过，
// 主题原样放进页面。没接上实现时插槽为空、小组件不显示、短代码原样保留。
type Frontend interface {
	// Slot 返回插件放进某个插槽的内容。
	Slot(ctx context.Context, slot string, page *hooks.Page) template.HTML
	// Widget 渲染一个插件小组件，id 形如 <插件>/<小组件>；插件没启用或没产出内容时 ok 为假。
	Widget(ctx context.Context, id string, page *hooks.Page) (label string, html template.HTML, ok bool)
	// Shortcodes 展开正文里的短代码，返回新的正文；没有可展开的短代码时 changed 为假。
	Shortcodes(ctx context.Context, html string, page *hooks.Page) (out string, changed bool)
	// StripShortcodes 去掉纯文字里认得的短代码，摘要与页面描述里用。
	StripShortcodes(text string) string
}

// frontendHolder 让主题模块在装配期就拿住入口，插件模块晚些接上实现也无妨。
type frontendHolder struct {
	mu   sync.RWMutex
	impl Frontend
}

func (h *frontendHolder) get() Frontend {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.impl
}

func (h *frontendHolder) Slot(ctx context.Context, slot string, page *hooks.Page) template.HTML {
	if impl := h.get(); impl != nil {
		return impl.Slot(ctx, slot, page)
	}
	return ""
}

func (h *frontendHolder) Widget(ctx context.Context, id string, page *hooks.Page) (string, template.HTML, bool) {
	if impl := h.get(); impl != nil {
		return impl.Widget(ctx, id, page)
	}
	return "", "", false
}

func (h *frontendHolder) StripShortcodes(text string) string {
	if impl := h.get(); impl != nil {
		return impl.StripShortcodes(text)
	}
	return text
}

func (h *frontendHolder) Shortcodes(ctx context.Context, html string, page *hooks.Page) (string, bool) {
	if impl := h.get(); impl != nil {
		return impl.Shortcodes(ctx, html, page)
	}
	return html, false
}

// Frontend 返回前台扩展入口，总是非 nil。
func (a *App) Frontend() Frontend { return &a.frontend }

// SetFrontend 接上前台扩展的实现，由插件模块在装配时调用。
func (a *App) SetFrontend(f Frontend) {
	a.frontend.mu.Lock()
	a.frontend.impl = f
	a.frontend.mu.Unlock()
}
