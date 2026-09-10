package logging

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestParseLevel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"info", slog.LevelInfo},
		// 两侧空白应被容忍：配置文件里手写的值常带空格。
		{" info ", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"error", slog.LevelError},
		{"", slog.LevelInfo},
		{"nonsense", slog.LevelInfo},
	}

	for _, tt := range tests {
		if got := ParseLevel(tt.input); got != tt.want {
			t.Errorf("ParseLevel(%q) = %v，期望 %v", tt.input, got, tt.want)
		}
	}
}

func TestNewJSONFormat(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := New(&buf, Options{Level: "info", Format: "json"})
	logger.Info("测试消息")

	out := buf.String()
	if !strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Errorf("json 格式应输出 JSON 对象，实际 %q", out)
	}
	if !strings.Contains(out, "测试消息") {
		t.Errorf("消息未输出: %q", out)
	}
}

func TestNewTextFormatIsDefault(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := New(&buf, Options{Level: "info", Format: "unknown"})
	logger.Info("hello")

	if strings.HasPrefix(strings.TrimSpace(buf.String()), "{") {
		t.Error("未识别的格式应回退到 text")
	}
}

// TestLevelFiltering 验证级别过滤生效：debug 消息在 info 级别下不应输出。
func TestLevelFiltering(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := New(&buf, Options{Level: "warn", Format: "text"})
	logger.Info("这条不该出现")
	logger.Warn("这条应该出现")

	out := buf.String()
	if strings.Contains(out, "这条不该出现") {
		t.Error("低于配置级别的日志不应输出")
	}
	if !strings.Contains(out, "这条应该出现") {
		t.Error("达到配置级别的日志应输出")
	}
}
