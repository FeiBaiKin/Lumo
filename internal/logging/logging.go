// Package logging 统一构造 slog 日志器。
package logging

import (
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
	handlerOpts := &slog.HandlerOptions{Level: ParseLevel(opts.Level)}

	var handler slog.Handler
	if strings.EqualFold(opts.Format, "json") {
		handler = slog.NewJSONHandler(w, handlerOpts)
	} else {
		handler = slog.NewTextHandler(w, handlerOpts)
	}
	return slog.New(handler)
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
