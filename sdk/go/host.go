package lumo

// 基础宿主能力：任何插件都有，不需要在清单里声明。

// logArgs 是 log 调用的参数。
type logArgs struct {
	Level   string         `json:"level"`
	Message string         `json:"message"`
	Attrs   map[string]any `json:"attrs,omitempty"`
}

// logAt 写一条日志。attrs 按「键, 值, 键, 值……」成对给出，落单的最后一个键被丢掉。
func logAt(level, msg string, attrs []any) {
	var fields map[string]any
	if len(attrs) >= 2 {
		fields = make(map[string]any, len(attrs)/2)
		for i := 0; i+1 < len(attrs); i += 2 {
			if key, ok := attrs[i].(string); ok {
				fields[key] = attrs[i+1]
			}
		}
	}
	// 日志写不出去也不能让处理函数失败
	_ = call("log", logArgs{Level: level, Message: msg, Attrs: fields}, nil)
}

// Debug 写一条调试日志。
func Debug(msg string, attrs ...any) { logAt("debug", msg, attrs) }

// Info 写一条日志。日志出现在后台「日志」页，带插件名。
func Info(msg string, attrs ...any) { logAt("info", msg, attrs) }

// Warn 写一条告警。
func Warn(msg string, attrs ...any) { logAt("warn", msg, attrs) }

// Error 写一条错误日志。
func Error(msg string, attrs ...any) { logAt("error", msg, attrs) }

// Settings 读本插件某个设置分组的当前值（已保存的值覆盖 settings.yaml 里的缺省值）。
func Settings(group string) (map[string]any, error) {
	var out map[string]any
	err := SettingsInto(group, &out)
	return out, err
}

// SettingsInto 把某个设置分组的当前值解到 v 里，v 通常是一个带 json 标签的结构体指针。
func SettingsInto(group string, v any) error {
	return call("settings.get", map[string]string{"group": group}, v)
}

// PluginInfo 是本插件的身份信息。
type PluginInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	// Site 是站点对外地址，没配置时为空串。
	Site string `json:"site"`
}

// Self 返回本插件的身份信息。
func Self() (PluginInfo, error) {
	var out PluginInfo
	err := call("plugin.info", nil, &out)
	return out, err
}
