package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/FeiBaiKin/lumo/internal/settings"
)

// hostOp 是一项宿主能力。
type hostOp struct {
	// allowed 判断插件被授予的能力够不够用这一项；nil 表示每个带后端的插件都有。
	allowed func(granted *Capabilities) bool
	call    func(ctx context.Context, m *Module, loaded *Loaded, args json.RawMessage) (any, error)
	// denied 是没被授予时给插件的说明，告诉作者该在清单里声明什么。
	denied string
}

// hostOps 是宿主开放给插件的全部能力，键是插件 SDK 里用的调用名。
var hostOps = map[string]hostOp{
	"log":          {call: hostLog},
	"settings.get": {call: hostSettings},
	"plugin.info":  {call: hostInfo},
}

// host 处理插件发起的一次宿主调用：认调用名、核能力、再交给对应的处理函数。
//
// 能力按**站长授予过的**判定，不按清单里声明的：两者通常相同，但升级后多声明的那部分
// 在站长点头之前不能生效（那种情况下插件本来就会先被停用，这里是第二道闸）。
func (m *Module) host(ctx context.Context, plugin, op string, args json.RawMessage) (any, error) {
	loaded, ok := m.registry.Get(plugin)
	if !ok || !loaded.Enabled {
		return nil, fmt.Errorf("插件 %s 未启用", plugin)
	}
	entry, ok := hostOps[op]
	if !ok {
		return nil, fmt.Errorf("宿主没有「%s」这项能力", op)
	}
	if entry.allowed != nil {
		granted := loaded.Granted
		if granted == nil || !entry.allowed(granted) {
			return nil, fmt.Errorf("没有「%s」的权限：%s", op, entry.denied)
		}
	}
	return entry.call(ctx, m, loaded, args)
}

// decodeArgs 解析宿主调用的参数。
func decodeArgs(op string, args json.RawMessage, v any) error {
	if len(args) == 0 {
		return nil
	}
	if err := json.Unmarshal(args, v); err != nil {
		return fmt.Errorf("「%s」的参数不对：%w", op, err)
	}
	return nil
}

// 插件日志的上限：一条消息与属性的数量、长度。
const (
	maxLogMessage   = 2000
	maxLogAttrs     = 20
	maxLogAttrValue = 500
)

func hostLog(_ context.Context, m *Module, loaded *Loaded, args json.RawMessage) (any, error) {
	var in struct {
		Level   string         `json:"level"`
		Message string         `json:"message"`
		Attrs   map[string]any `json:"attrs"`
	}
	if err := decodeArgs("log", args, &in); err != nil {
		return nil, err
	}
	level := slog.LevelInfo
	switch in.Level {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	attrs := make([]any, 0, 2+min(len(in.Attrs), maxLogAttrs))
	attrs = append(attrs, slog.String("plugin", loaded.ID()))
	for key, value := range in.Attrs {
		if len(attrs) > maxLogAttrs {
			break
		}
		if key == "plugin" || key == "module" {
			// 不许插件冒充别的来源
			key = "attr." + key
		}
		attrs = append(attrs, slog.String(key, truncate(fmt.Sprint(value), maxLogAttrValue)))
	}
	m.logger.Log(context.Background(), level, truncate(in.Message, maxLogMessage), attrs...)
	return nil, nil
}

func hostSettings(ctx context.Context, m *Module, loaded *Loaded, args json.RawMessage) (any, error) {
	var in struct {
		Group string `json:"group"`
	}
	if err := decodeArgs("settings.get", args, &in); err != nil {
		return nil, err
	}
	group, ok := loaded.SettingGroup(in.Group)
	if !ok {
		return nil, fmt.Errorf("没有「%s」这个设置分组", in.Group)
	}
	stored, err := m.store.LoadSettings(ctx, loaded.ID())
	if err != nil {
		return nil, err
	}
	return settings.Merge(group.validator.DefaultValues(), stored[in.Group]), nil
}

func hostInfo(ctx context.Context, m *Module, loaded *Loaded, _ json.RawMessage) (any, error) {
	out := struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		Site    string `json:"site"`
	}{Name: loaded.ID(), Version: loaded.Manifest.Spec.Version}
	if m.settings != nil {
		var site settings.Site
		if err := m.settings.Get(ctx, "site", &site); err == nil {
			out.Site = strings.TrimRight(site.URL, "/")
		}
	}
	return out, nil
}

// truncate 按字节上限截断，不切坏多字节字符。
func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	s = s[:limit]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s + "…"
}
