// Package logging 统一构造 slog 日志器。
package logging

import (
	"context"
	"io"
	"log/slog"
	"strings"
)

// Options 是日志器构造参数。
type Options struct {
	// Level 取值 debug / info / warn / error，大小写不敏感。
	Level string
	// Format 取值 text / json，大小写不敏感。
	Format string
}

// New 按 opts 构造日志器，写入 w。
// 无法识别的取值回退到 info / text，而不是报错——日志本身不该成为启动失败的原因。
func New(w io.Writer, opts Options) *slog.Logger {
	return slog.New(newHandler(w, opts.Format, ParseLevel(opts.Level)))
}

// NewTee 构造同时写控制台与文件的日志器。
//
// 两路各自成句：控制台跟随 opts.Format（站长习惯的 text 或 json），
// 文件一律 JSON —— 后台日志页要把每行解析成结构化字段，text 格式做不到这件事。
//
// 为什么保留控制台那一路：Docker 部署靠 docker logs、systemd 靠 journald，
// 两者都读 stdout。只写文件会让这两种部署方式的用户失去他们习惯的排障入口。
func NewTee(console, file io.Writer, opts Options) *slog.Logger {
	level := ParseLevel(opts.Level)
	if file == nil {
		return slog.New(newHandler(console, opts.Format, level))
	}
	return slog.New(&teeHandler{
		handlers: []slog.Handler{
			newHandler(console, opts.Format, level),
			newHandler(file, "json", level),
		},
	})
}

// newHandler 按格式名构造 handler。
func newHandler(w io.Writer, format string, level slog.Level) slog.Handler {
	handlerOpts := &slog.HandlerOptions{Level: level}
	if strings.EqualFold(format, "json") {
		return slog.NewJSONHandler(w, handlerOpts)
	}
	return slog.NewTextHandler(w, handlerOpts)
}

// teeHandler 把每条记录分发给多个 handler。
//
// 不用「一个 handler 写 io.MultiWriter」：那样两路只能是同一种格式，
// 而控制台要给人读、文件要给程序解析，需求本来就不同。
type teeHandler struct {
	handlers []slog.Handler
}

// Enabled 实现 slog.Handler：任一路接受即处理。
func (t *teeHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range t.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

// Handle 实现 slog.Handler。
//
// 一路失败不影响另一路：文件写满磁盘时，控制台那一路仍要出日志。
func (t *teeHandler) Handle(ctx context.Context, r slog.Record) error {
	var firstErr error
	for _, h := range t.handlers {
		if !h.Enabled(ctx, r.Level) {
			continue
		}
		// 每路各拿一份副本：Record 的属性是可共享的，但 Handle 允许持有它。
		if err := h.Handle(ctx, r.Clone()); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// WithAttrs 实现 slog.Handler。
func (t *teeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := make([]slog.Handler, len(t.handlers))
	for i, h := range t.handlers {
		out[i] = h.WithAttrs(attrs)
	}
	return &teeHandler{handlers: out}
}

// WithGroup 实现 slog.Handler。
func (t *teeHandler) WithGroup(name string) slog.Handler {
	out := make([]slog.Handler, len(t.handlers))
	for i, h := range t.handlers {
		out[i] = h.WithGroup(name)
	}
	return &teeHandler{handlers: out}
}

// ParseLevel 把级别字符串转成 slog.Level，无法识别时回退 info。
func ParseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
