package theme

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/FeiBaiKin/lumo/internal/app"
)

// 主题包内的固定文件名（agent.md §4.4）。
const (
	// FileManifest 是主题元信息。
	FileManifest = "theme.yaml"
	// FileSettings 是主题设置项声明，可缺省。
	FileSettings = "settings.yaml"
	// FileScreenshot 是后台展示用截图，可缺省。
	FileScreenshot = "screenshot.png"
	// DirTemplates 是模板目录。
	DirTemplates = "templates"
	// DirStatic 是静态资源目录，可缺省。
	DirStatic = "static"
)

// namePattern 限定主题标识形态（DNS-1123），它同时是目录名与 URL 片段。
var namePattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// versionPattern 限定版本号形态：宽松的 semver，允许省略修订号与带预发布标记。
var versionPattern = regexp.MustCompile(`^\d+(\.\d+){0,2}(-[0-9A-Za-z.-]+)?$`)

// 主题元信息各字段的长度上限。主题包来自第三方，任何字段都不能无限长。
const (
	maxNameLength        = 64
	maxLabelLength       = 64
	maxDescriptionLength = 500
	maxAuthorLength      = 64
	maxURLLength         = 512
	maxLicenseLength     = 64
	maxVersionLength     = 32
)

// Manifest 是 theme.yaml 的内容。
//
// 只认 YAML：主题作者写 settings.yaml 已经要用 YAML，没必要再引入第二种格式。
type Manifest struct {
	// Name 是主题标识（DNS-1123），同时是安装目录名与 URL 片段，必须与目录名一致。
	Name string `yaml:"name" json:"name"`
	// Label 是显示名。
	Label string `yaml:"label" json:"label"`
	// Version 是主题版本。
	Version string `yaml:"version" json:"version"`
	// Description 是一句话说明。
	Description string `yaml:"description" json:"description"`
	// Author 是作者名。
	Author string `yaml:"author" json:"author"`
	// Homepage 是主题主页。
	Homepage string `yaml:"homepage" json:"homepage"`
	// Repo 是源码仓库地址。
	Repo string `yaml:"repo" json:"repo"`
	// License 是许可证标识。
	License string `yaml:"license" json:"license"`
	// RequireLumo 声明所需的最低 Lumo 版本，形如 0.1.0；留空表示不限制。
	RequireLumo string `yaml:"requireLumo" json:"requireLumo"`
}

// parseManifest 解析并校验 theme.yaml。
//
// 未知字段一律报错：主题作者把 lable 拼成 label 的反面时，静默忽略会让他对着
// 一个没有显示名的主题找半天。
func parseManifest(data []byte) (*Manifest, error) {
	var m Manifest
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true)
	if err := decoder.Decode(&m); err != nil {
		return nil, fmt.Errorf("%w：解析 %s: %w", ErrInvalidPackage, FileManifest, err)
	}

	m.Name = strings.TrimSpace(m.Name)
	m.Label = strings.TrimSpace(m.Label)
	m.Version = strings.TrimSpace(m.Version)

	if !namePattern.MatchString(m.Name) || len(m.Name) > maxNameLength {
		return nil, fmt.Errorf("%w：name %q 须为小写字母、数字与连字符组成且不以连字符开头或结尾", ErrInvalidPackage, m.Name)
	}
	if m.Label == "" {
		m.Label = m.Name
	}
	if !versionPattern.MatchString(m.Version) || len(m.Version) > maxVersionLength {
		return nil, fmt.Errorf("%w：version %q 须形如 1.0.0", ErrInvalidPackage, m.Version)
	}
	if m.RequireLumo != "" && !versionPattern.MatchString(m.RequireLumo) {
		return nil, fmt.Errorf("%w：requireLumo %q 须形如 1.0.0", ErrInvalidPackage, m.RequireLumo)
	}

	limits := []struct {
		field string
		value string
		limit int
	}{
		{"label", m.Label, maxLabelLength},
		{"description", m.Description, maxDescriptionLength},
		{"author", m.Author, maxAuthorLength},
		{"homepage", m.Homepage, maxURLLength},
		{"repo", m.Repo, maxURLLength},
		{"license", m.License, maxLicenseLength},
	}
	for _, l := range limits {
		if len([]rune(l.value)) > l.limit {
			return nil, fmt.Errorf("%w：%s 超过 %d 个字符", ErrInvalidPackage, l.field, l.limit)
		}
	}
	return &m, nil
}

// SettingsDecl 是 settings.yaml 的内容：主题自己的设置项声明。
//
// 复用站点设置的统一表单 Schema（agent.md §5）：Console 用同一个表单引擎渲染，
// 主题作者只需学一次。与站点设置的唯一差别是它不走 app.SettingGroup 的启动期登记——
// 主题可以随时换，分组不能钉死在进程启动那一刻。
type SettingsDecl struct {
	// Groups 是设置分组列表。
	Groups []SettingsGroupDecl `yaml:"groups" json:"groups"`
}

// SettingsGroupDecl 是主题设置的一个分组。
type SettingsGroupDecl struct {
	Name        string `yaml:"name" json:"name"`
	Label       string `yaml:"label" json:"label"`
	Description string `yaml:"description" json:"description"`
	Order       int    `yaml:"order" json:"order"`
	// Schema 是 JSON Schema 2020-12 子集 + x-widget，以 YAML 书写。
	Schema map[string]any `yaml:"schema" json:"schema"`
	// Defaults 是缺省值对象，须能通过 Schema 校验。
	Defaults map[string]any `yaml:"defaults" json:"defaults"`
}

// maxSettingsGroups 是主题设置分组数上限。
//
// 主题设置是给站长在一个页面里调的，不是配置管理系统；几十个分组说明主题设计有问题。
const maxSettingsGroups = 20

// parseSettings 解析并校验 settings.yaml。文件缺失时调用方应跳过本函数。
func parseSettings(data []byte) (*SettingsDecl, error) {
	var decl SettingsDecl
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true)
	if err := decoder.Decode(&decl); err != nil {
		return nil, fmt.Errorf("%w：解析 %s: %w", ErrInvalidPackage, FileSettings, err)
	}
	if len(decl.Groups) > maxSettingsGroups {
		return nil, fmt.Errorf("%w：设置分组不得超过 %d 个", ErrInvalidPackage, maxSettingsGroups)
	}

	seen := make(map[string]bool, len(decl.Groups))
	for i := range decl.Groups {
		g := &decl.Groups[i]
		g.Name = strings.TrimSpace(g.Name)
		if !namePattern.MatchString(g.Name) {
			return nil, fmt.Errorf("%w：设置分组名 %q 非法", ErrInvalidPackage, g.Name)
		}
		if seen[g.Name] {
			return nil, fmt.Errorf("%w：设置分组 %q 重复", ErrInvalidPackage, g.Name)
		}
		seen[g.Name] = true
		if g.Label == "" {
			g.Label = g.Name
		}
		if g.Schema == nil {
			return nil, fmt.Errorf("%w：设置分组 %q 缺少 schema", ErrInvalidPackage, g.Name)
		}
		if g.Defaults == nil {
			g.Defaults = map[string]any{}
		}
	}
	return &decl, nil
}

// SettingGroups 把主题声明转成与站点设置同构的分组，供表单引擎与校验器使用。
//
// 返回 app.SettingGroup 而非自定义类型：Console 的通用表单引擎只认这一种形态，
// 主题设置与站点设置在前端走完全相同的渲染路径。
func (d *SettingsDecl) SettingGroups() ([]app.SettingGroup, error) {
	out := make([]app.SettingGroup, 0, len(d.Groups))
	for i := range d.Groups {
		g := &d.Groups[i]
		schema, err := json.Marshal(g.Schema)
		if err != nil {
			return nil, fmt.Errorf("%w：设置分组 %q 的 schema 无法编码: %w", ErrInvalidPackage, g.Name, err)
		}
		defaults, err := json.Marshal(g.Defaults)
		if err != nil {
			return nil, fmt.Errorf("%w：设置分组 %q 的 defaults 无法编码: %w", ErrInvalidPackage, g.Name, err)
		}
		out = append(out, app.SettingGroup{
			Name:        g.Name,
			Label:       g.Label,
			Description: g.Description,
			Order:       g.Order,
			Schema:      schema,
			Defaults:    defaults,
		})
	}
	return out, nil
}

// readManifestFS 从主题文件系统读取并解析 theme.yaml。
func readManifestFS(fsys fs.FS) (*Manifest, error) {
	data, err := fs.ReadFile(fsys, FileManifest)
	if err != nil {
		return nil, fmt.Errorf("%w：缺少 %s", ErrInvalidPackage, FileManifest)
	}
	return parseManifest(data)
}

// readSettingsFS 从主题文件系统读取并解析 settings.yaml；文件不存在时返回空声明。
func readSettingsFS(fsys fs.FS) (*SettingsDecl, error) {
	data, err := fs.ReadFile(fsys, FileSettings)
	if err != nil {
		return &SettingsDecl{}, nil //nolint:nilerr // 设置声明是可选的
	}
	return parseSettings(data)
}
