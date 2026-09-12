package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/FeiBaiKin/lumo/internal/form"
	"github.com/FeiBaiKin/lumo/internal/settings"
)

// SettingsGroup 是一个插件的设置分组。
//
// 形态与 app.SettingGroup 同构，但**不注册进全局设置服务**：
// 插件的设置随插件走，停用时要能连同它一起收起，而全局设置分组的生命周期
// 钉在进程启动那一刻。主题设置也是同样的处置（见 internal/theme/settings.go）。
type SettingsGroup struct {
	Name        string
	Label       string
	Description string
	Order       int
	Icon        string
	Form        *form.Form

	// validator 是编译好的校验器，随分组一起构造。
	//
	// 不每次请求现编译：JSON Schema 的编译不便宜，而分组一旦加载就不再变化。
	// 主题设置那边是同一个处置（见 internal/theme 的 compiledGroup）。
	validator *settings.Validator
}

// Validator 返回分组的校验器。
func (g *SettingsGroup) Validator() *settings.Validator { return g.validator }

// loadSettings 读取插件的 settings.yaml。
//
// 文件缺失时返回空列表而不是报错：绝大多数插件没有设置项，
// 强制它们放一个空文件只会制造噪音。
//
// 解析走 form.Parse，与主题设置、站点设置共用同一套条件依赖语义（agent.md §5）。
// 各处的「设置」如果长着不一样的脸，站长就得学两遍。
func loadSettings(fsys fs.FS) ([]SettingsGroup, error) {
	data, err := fs.ReadFile(fsys, FileSettings)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("%w：读取 %s: %w", ErrInvalidPackage, FileSettings, err)
	}

	var decl settingsFile
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true)
	if err := decoder.Decode(&decl); err != nil {
		return nil, fmt.Errorf("%w：解析 %s: %w", ErrInvalidPackage, FileSettings, err)
	}
	if len(decl.Groups) > maxSettingsGroups {
		return nil, fmt.Errorf("%w：设置分组不得超过 %d 个", ErrInvalidPackage, maxSettingsGroups)
	}

	out := make([]SettingsGroup, 0, len(decl.Groups))
	seen := make(map[string]bool, len(decl.Groups))
	for i := range decl.Groups {
		group := &decl.Groups[i]
		name := strings.TrimSpace(group.Name)
		if !namePattern.MatchString(name) {
			return nil, fmt.Errorf("%w：设置分组名 %q 非法", ErrInvalidPackage, name)
		}
		if seen[name] {
			return nil, fmt.Errorf("%w：设置分组 %q 重复", ErrInvalidPackage, name)
		}
		seen[name] = true
		if group.Label == "" {
			group.Label = name
		}
		if group.Schema == nil {
			return nil, fmt.Errorf("%w：设置分组 %q 缺少 schema", ErrInvalidPackage, name)
		}
		if group.Defaults == nil {
			group.Defaults = map[string]any{}
		}

		schema, err := json.Marshal(group.Schema)
		if err != nil {
			return nil, fmt.Errorf("%w：设置分组 %q 的 schema 无法编码: %w", ErrInvalidPackage, name, err)
		}
		defaults, err := json.Marshal(group.Defaults)
		if err != nil {
			return nil, fmt.Errorf("%w：设置分组 %q 的 defaults 无法编码: %w", ErrInvalidPackage, name, err)
		}
		parsed, err := form.Parse(name, schema, defaults)
		if err != nil {
			return nil, fmt.Errorf("%w：设置分组 %q：%w", ErrInvalidPackage, name, err)
		}

		validator, err := settings.NewValidator(name, parsed)
		if err != nil {
			return nil, fmt.Errorf("%w：设置分组 %q：%w", ErrInvalidPackage, name, err)
		}
		// 缺省值必须自洽：作者把 defaults 写得不符合自己的 schema 时，
		// 站长打开设置页会看到一堆无法保存的初始值。
		if err := validator.Validate(validator.DefaultValues()); err != nil {
			return nil, fmt.Errorf("%w：设置分组 %q 的 defaults 未通过自身 schema：%w",
				ErrInvalidPackage, name, err)
		}

		out = append(out, SettingsGroup{
			Name:        name,
			Label:       group.Label,
			Description: group.Description,
			Order:       group.Order,
			Icon:        group.Icon,
			Form:        parsed,
			validator:   validator,
		})
	}
	return out, nil
}
