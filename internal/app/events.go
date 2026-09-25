package app

import (
	"context"
	"encoding/json"
	"sync"
)

// Events 是内核向插件派发动作与过滤器的总线。
//
// 内核模块不认识插件：它们只在事情发生之后 Emit，在要把数据交给别人改一遍时 Filter。
// 插件模块装配时经 SetEvents 接上实现；没接的时候总线是空的——Emit 什么也不做，
// Filter 原样返回，内核照常工作。
type Events interface {
	// Emit 通知一个动作。实现须异步执行，不拖慢调用方，也不向调用方报错。
	Emit(ctx context.Context, action string, data any)
	// Subscribed 判断有没有插件订阅了某个过滤器；没有时调用方可以连编码都省掉。
	Subscribed(filter string) bool
	// Filter 把值依次交给订阅了过滤器的插件，返回最终的值；任何一环失败都跳过那一环。
	Filter(ctx context.Context, filter string, value json.RawMessage) json.RawMessage
}

// eventBus 是 App 持有的总线：先于实现存在，实现接上之前的调用一律落空。
type eventBus struct {
	mu   sync.RWMutex
	impl Events
}

func (b *eventBus) get() Events {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.impl
}

func (b *eventBus) Emit(ctx context.Context, action string, data any) {
	if impl := b.get(); impl != nil {
		impl.Emit(ctx, action, data)
	}
}

func (b *eventBus) Subscribed(filter string) bool {
	impl := b.get()
	return impl != nil && impl.Subscribed(filter)
}

func (b *eventBus) Filter(ctx context.Context, filter string, value json.RawMessage) json.RawMessage {
	if impl := b.get(); impl != nil {
		return impl.Filter(ctx, filter, value)
	}
	return value
}

// Events 返回事件总线，总是非 nil；模块可以在装配期就拿住它，实现晚些接上也无妨。
func (a *App) Events() Events { return &a.events }

// SetEvents 接上事件总线的实现，由插件模块在装配时调用。
func (a *App) SetEvents(e Events) {
	a.events.mu.Lock()
	a.events.impl = e
	a.events.mu.Unlock()
}

// ApplyFilter 是 Filter 的类型化包装：没人订阅时原样返回；插件的结果解不回 T 时也原样返回。
func ApplyFilter[T any](ctx context.Context, e Events, filter string, value T) T {
	if e == nil || !e.Subscribed(filter) {
		return value
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var out T
	if err := json.Unmarshal(e.Filter(ctx, filter, raw), &out); err != nil {
		return value
	}
	return out
}
