package wasm

import (
	"bytes"
	"context"
	"log/slog"
	"sync"
)

const (
	// maxLogLine 是一行输出的字节上限，超出部分截掉。
	maxLogLine = 2000
	// maxLogLines 是一个实例一生能写的行数上限：插件在循环里打印时不至于把日志刷满盘。
	maxLogLines = 1000
)

// logWriter 把插件的标准输出按行转成宿主日志。每个实例一个，不跨实例共享缓冲。
type logWriter struct {
	logger *slog.Logger
	plugin string
	level  slog.Level

	mu      sync.Mutex
	buf     []byte
	lines   int
	dropped bool
}

func newLogWriter(logger *slog.Logger, plugin string, level slog.Level) *logWriter {
	return &logWriter{logger: logger, plugin: plugin, level: level}
}

func (w *logWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		w.emit(w.buf[:i])
		w.buf = w.buf[i+1:]
	}
	// 一直不换行的输出也不能无限攒着
	if len(w.buf) > maxLogLine {
		w.emit(w.buf)
		w.buf = w.buf[:0]
	}
	return len(p), nil
}

func (w *logWriter) emit(line []byte) {
	line = bytes.TrimRight(line, "\r")
	if len(line) == 0 {
		return
	}
	if w.lines >= maxLogLines {
		if !w.dropped {
			w.dropped = true
			w.logger.Warn("插件输出过多，后面的不再记录", slog.String("plugin", w.plugin))
		}
		return
	}
	w.lines++
	if len(line) > maxLogLine {
		line = line[:maxLogLine]
	}
	w.logger.Log(context.Background(), w.level, string(line),
		slog.String("plugin", w.plugin), slog.String("source", "stdout"))
}
