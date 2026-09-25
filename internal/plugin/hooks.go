package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/FeiBaiKin/lumo/internal/hooks"
	"github.com/FeiBaiKin/lumo/internal/plugin/wasm"
)

// Hooks 是插件订阅的钩子，写在 plugin.yaml 的 spec.hooks 里。
//
// 订阅要先声明：站长在启用前能看到插件会在什么时候被叫起来，
// 宿主也据此只把事情派给真正订阅了的插件，而不是挨个问一遍。
type Hooks struct {
	// Actions 是订阅的动作名，如 comment.created。
	Actions []string `yaml:"actions" json:"actions"`
	// Filters 是订阅的过滤器名，如 comment.judge。
	Filters []string `yaml:"filters" json:"filters"`
}

// 钩子的两个类别，也是发给插件的请求类别（wasm.Request.Type）。
const (
	kindAction    = "action"
	kindFilter    = "filter"
	kindCron      = "cron"
	kindRoute     = "route"
	kindSlot      = "slot"
	kindWidget    = "widget"
	kindShortcode = "shortcode"
)

// hookSpec 是一个钩子在插件一侧的约定：订阅它要什么能力、过滤器给多少时间。
type hookSpec struct {
	// needsRead 表示收到的数据属于站点内容（含评论者的邮箱与 IP），须被授予读内容。
	needsRead bool
	// needsFrontend 表示它改的是访客看到的东西，须被授予前台注入。
	needsFrontend bool
	// timeout 是过滤器的时间预算。
	timeout time.Duration
}

// actionHooks 是插件能订阅的动作。
var actionHooks = map[string]hookSpec{
	hooks.PostPublished:   {needsRead: true},
	hooks.PostUpdated:     {needsRead: true},
	hooks.PostTrashed:     {needsRead: true},
	hooks.PostDeleted:     {needsRead: true},
	hooks.CommentCreated:  {needsRead: true},
	hooks.CommentApproved: {needsRead: true},
	hooks.UserRegistered:  {needsRead: true},
}

// filterHooks 是插件能订阅的过滤器。
//
// 时限各不相同：正文渲染在每次打开文章时都要跑，只给 200 ms；评论判定发生在提交评论时，
// 反垃圾插件往往要调一次外部服务，给到 3 秒。
var filterHooks = map[string]hookSpec{
	hooks.CommentJudge:  {needsRead: true, timeout: 3 * time.Second},
	hooks.ContentRender: {needsRead: true, needsFrontend: true, timeout: 200 * time.Millisecond},
}

const (
	// actionTimeout 是处理一次动作的时限。
	actionTimeout = 5 * time.Second
	// actionQueueSize 是待派发动作的队列长度；满了就丢弃并记日志，不阻塞发出动作的请求。
	actionQueueSize = 1024
	// actionWorkers 是派发动作的并发数。
	actionWorkers = 4
)

// allowedBy 判断能力够不够订阅这个钩子。
func (s hookSpec) allowedBy(c *Capabilities) bool {
	return (!s.needsRead || c.Content.Read) && (!s.needsFrontend || c.Frontend)
}

// requirement 说明订阅它要声明什么，用在报错里。
func (s hookSpec) requirement() string {
	switch {
	case s.needsRead && s.needsFrontend:
		return "capabilities.content.read 与 capabilities.frontend"
	case s.needsFrontend:
		return "capabilities.frontend"
	default:
		return "capabilities.content.read"
	}
}

// normalize 校验订阅的钩子：名字要认得、能力要够、要有后端去处理，然后去重排序。
func (h *Hooks) normalize(caps *Capabilities, hasBackend bool) error {
	check := func(field string, names []string, catalog map[string]hookSpec) ([]string, error) {
		out := make([]string, 0, len(names))
		for _, name := range names {
			spec, ok := catalog[name]
			if !ok {
				return nil, fmt.Errorf("%w：spec.hooks.%s 里的 %q 不是宿主提供的钩子", ErrInvalidPackage, field, name)
			}
			if !spec.allowedBy(caps) {
				return nil, fmt.Errorf("%w：订阅 %s 需要声明 %s", ErrInvalidPackage, name, spec.requirement())
			}
			out = append(out, name)
		}
		slices.Sort(out)
		return slices.Compact(out), nil
	}
	var err error
	if h.Actions, err = check("actions", h.Actions, actionHooks); err != nil {
		return err
	}
	if h.Filters, err = check("filters", h.Filters, filterHooks); err != nil {
		return err
	}
	if !hasBackend && (len(h.Actions) > 0 || len(h.Filters) > 0) {
		return fmt.Errorf("%w：订阅钩子要由后端代码处理，请同时声明 spec.runtime: %s", ErrInvalidPackage, RuntimeWasm)
	}
	return nil
}

// checkHandlers 核对清单与后端：声明了的钩子必须有处理函数，登记了的处理函数必须声明过。
//
// 两个方向都要查：声明了却没登记，动作派过去只会报错；登记了却没声明，站长确认时
// 看不到它，而那段代码在作者看来「写了却不生效」。
func checkHandlers(m *Manifest, desc wasm.Description) error {
	var cron, routes, widgets, shortcodes []string
	for i := range m.Spec.Cron {
		cron = append(cron, m.Spec.Cron[i].Name)
	}
	for i := range m.Spec.Routes {
		routes = append(routes, m.Spec.Routes[i].Name)
	}
	fe := &m.Spec.Frontend
	for i := range fe.Widgets {
		widgets = append(widgets, fe.Widgets[i].Name)
	}
	for i := range fe.Shortcodes {
		shortcodes = append(shortcodes, fe.Shortcodes[i].Name)
	}
	for kind, declared := range map[string][]string{
		kindAction:    m.Spec.Hooks.Actions,
		kindFilter:    m.Spec.Hooks.Filters,
		kindCron:      cron,
		kindRoute:     routes,
		kindSlot:      fe.Slots,
		kindWidget:    widgets,
		kindShortcode: shortcodes,
	} {
		registered := desc.Handlers[kind]
		for _, name := range declared {
			if !slices.Contains(registered, name) {
				return fmt.Errorf("清单声明了 %s %s，但插件没有登记它的处理函数", kind, name)
			}
		}
		for _, name := range registered {
			if !slices.Contains(declared, name) {
				return fmt.Errorf("插件登记了 %s %s 的处理函数，但清单没有声明它"+
					"（钩子在 spec.hooks，定时任务在 spec.cron，接口在 spec.routes，插槽、小组件与短代码在 spec.frontend）", kind, name)
			}
		}
	}
	return nil
}

// actionJob 是一次待派发的动作。
type actionJob struct {
	plugin  string
	action  string
	payload json.RawMessage
}

// subscribers 返回订阅了某个钩子、且被授予了所需能力的运行中插件，按标识排序。
func (m *Module) subscribers(kind, hook string) []string {
	catalog, list := actionHooks, func(h *Hooks) []string { return h.Actions }
	if kind == kindFilter {
		catalog, list = filterHooks, func(h *Hooks) []string { return h.Filters }
	}
	spec := catalog[hook]
	var out []string
	for _, loaded := range m.registry.Enabled() {
		if !slices.Contains(list(&loaded.Manifest.Spec.Hooks), hook) {
			continue
		}
		// 按授予过的能力判定：清单多声明的部分在站长点头之前不生效
		if loaded.Granted == nil || !spec.allowedBy(loaded.Granted) {
			continue
		}
		out = append(out, loaded.ID())
	}
	return out
}

// Emit 实现 app.Events：把动作放进队列，由后台协程派给订阅的插件。
func (m *Module) Emit(_ context.Context, action string, data any) {
	subs := m.subscribers(kindAction, action)
	if len(subs) == 0 {
		return
	}
	payload, err := json.Marshal(data)
	if err != nil {
		m.logger.Warn("动作数据无法编码", slog.String("action", action), slog.Any("error", err))
		return
	}
	for _, name := range subs {
		select {
		case m.jobs <- actionJob{plugin: name, action: action, payload: payload}:
		default:
			m.logger.Warn("插件动作队列已满，这次通知丢弃了", slog.String("plugin", name), slog.String("action", action))
		}
	}
}

// runActions 从队列里取动作派给插件，直到 ctx 取消。
func (m *Module) runActions(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-m.jobs:
			req := wasm.Request{Type: kindAction, Name: job.action, Payload: job.payload}
			if _, err := m.Invoke(ctx, job.plugin, req, actionTimeout); err != nil && !wasm.IsCrash(err) {
				m.logger.Info("插件处理动作没有成功",
					slog.String("plugin", job.plugin), slog.String("action", job.action), slog.Any("error", err))
			}
		}
	}
}

// Subscribed 实现 app.Events。
func (m *Module) Subscribed(filter string) bool {
	return len(m.subscribers(kindFilter, filter)) > 0
}

// Filter 实现 app.Events：把值依次交给订阅的插件，每一环失败都跳过、沿用上一环的值。
func (m *Module) Filter(ctx context.Context, filter string, value json.RawMessage) json.RawMessage {
	spec := filterHooks[filter]
	for _, name := range m.subscribers(kindFilter, filter) {
		req := wasm.Request{Type: kindFilter, Name: filter, Payload: value}
		out, err := m.Invoke(ctx, name, req, spec.timeout)
		if err != nil {
			if !wasm.IsCrash(err) {
				m.logger.Info("插件处理过滤器没有成功，沿用原值",
					slog.String("plugin", name), slog.String("filter", filter), slog.Any("error", err))
			}
			continue
		}
		if len(out) == 0 || string(out) == "null" {
			continue
		}
		value = out
	}
	return value
}
