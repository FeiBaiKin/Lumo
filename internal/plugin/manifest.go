package plugin

import (
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// 插件包内的固定文件名。
const (
	// FileManifest 是插件元信息，必需。
	FileManifest = "plugin.yaml"
	// FileSettings 是插件设置项声明，可缺省；格式与主题设置完全一致。
	FileSettings = "settings.yaml"
	// FileLogo 是后台展示用的图标，可缺省。
	FileLogo = "logo.png"
	// FileWasm 是插件的后端代码，spec.runtime 为 wasm 时必需。
	FileWasm = "plugin.wasm"
)

// RuntimeWasm 表示插件带 WebAssembly 后端（包根目录的 plugin.wasm）。
const RuntimeWasm = "wasm"

// APIVersion 与 Kind 是 plugin.yaml 里的固定写法。
//
// 与 Extension 平面同一套形态：插件清单本身就是一份 GVK 资源，
// 将来要把它放进 extensions 表或经 API 暴露，都不必改格式。
const (
	APIVersion = "plugin.lumo.run/v1alpha1"
	Kind       = "Plugin"
)

// ErrInvalidPackage 表示插件包不合法。
var ErrInvalidPackage = errors.New("插件包不合法")

// ErrNotFound 表示插件不存在。
var ErrNotFound = errors.New("插件不存在")

// ErrAlreadyExists 表示同名插件已安装。
var ErrAlreadyExists = errors.New("插件已安装")

// namePattern 限定插件标识形态（DNS-1123），它同时是目录名。
var namePattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// versionPattern 限定版本号形态：宽松的 semver，允许省略修订号与带预发布标记。
var versionPattern = regexp.MustCompile(`^\d+(\.\d+){0,2}(-[0-9A-Za-z.-]+)?$`)

// 清单各字段的长度上限。插件包来自第三方，任何字段都不能无限长。
const (
	maxNameLength        = 64
	maxDisplayNameLength = 64
	maxDescriptionLength = 500
	maxAuthorLength      = 64
	maxURLLength         = 512
	maxLicenseLength     = 64
	maxVersionLength     = 32
	maxRequiresLength    = 64
)

// Manifest 是 plugin.yaml 的内容。
type Manifest struct {
	APIVersion string `yaml:"apiVersion" json:"apiVersion"`
	Kind       string `yaml:"kind" json:"kind"`
	Metadata   Meta   `yaml:"metadata" json:"metadata"`
	Spec       Spec   `yaml:"spec" json:"spec"`
}

// Meta 是清单的元信息。
type Meta struct {
	// Name 是插件标识（DNS-1123），同时是安装目录名，必须与目录名一致。
	Name string `yaml:"name" json:"name"`
}

// Spec 是清单的规格。
type Spec struct {
	// DisplayName 是显示名。
	DisplayName string `yaml:"displayName" json:"displayName"`
	// Version 是插件版本。
	Version string `yaml:"version" json:"version"`
	// Description 是一句话说明。
	Description string `yaml:"description" json:"description"`
	// Author 是作者信息。
	Author Author `yaml:"author" json:"author"`
	// Homepage 是插件主页。
	Homepage string `yaml:"homepage" json:"homepage"`
	// Repo 是源码仓库地址。
	Repo string `yaml:"repo" json:"repo"`
	// License 是许可证标识。
	License string `yaml:"license" json:"license"`
	// Requires 声明所需的 Lumo 版本范围；留空表示不限制。
	//
	// v1 只做语法校验，不做范围求解与拒绝安装——「不满足就装不上」需要一套
	// 完整的版本比较，而那要等插件间依赖一起做（§14.2 期 3）。届时这里会被真正用上。
	Requires string `yaml:"requires" json:"requires"`
	// Runtime 是后端的运行方式：留空表示纯声明式插件，wasm 表示包里带 plugin.wasm。
	Runtime string `yaml:"runtime" json:"runtime"`
	// Capabilities 是插件要用的宿主能力，启用时由站长逐项确认。
	Capabilities Capabilities `yaml:"capabilities" json:"capabilities"`
	// Hooks 是插件订阅的动作与过滤器。
	Hooks Hooks `yaml:"hooks" json:"hooks"`
}

// Author 是作者信息。
type Author struct {
	Name    string `yaml:"name" json:"name"`
	Website string `yaml:"website" json:"website"`
}

// parseManifest 解析并校验 plugin.yaml。
//
// 未知字段一律报错：作者把 displayName 拼成 displayname 时，静默忽略会让他对着
// 一个没有显示名的插件找半天。
func parseManifest(data []byte, dirName string) (*Manifest, error) {
	var m Manifest
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true)
	if err := decoder.Decode(&m); err != nil {
		return nil, fmt.Errorf("%w：解析 %s: %w", ErrInvalidPackage, FileManifest, err)
	}
	if err := m.validate(dirName); err != nil {
		return nil, err
	}
	return &m, nil
}

// validate 校验清单的形态。
func (m *Manifest) validate(dirName string) error {
	if m.APIVersion != APIVersion {
		return fmt.Errorf("%w：apiVersion 须为 %s，实际 %q", ErrInvalidPackage, APIVersion, m.APIVersion)
	}
	if m.Kind != Kind {
		return fmt.Errorf("%w：kind 须为 %s，实际 %q", ErrInvalidPackage, Kind, m.Kind)
	}

	name := strings.TrimSpace(m.Metadata.Name)
	switch {
	case name == "":
		return fmt.Errorf("%w：metadata.name 不能为空", ErrInvalidPackage)
	case len(name) > maxNameLength:
		return fmt.Errorf("%w：metadata.name 超过 %d 个字符", ErrInvalidPackage, maxNameLength)
	case !namePattern.MatchString(name):
		return fmt.Errorf("%w：metadata.name %q 须为 DNS-1123（小写字母、数字与连字符）", ErrInvalidPackage, name)
	case dirName != "" && name != dirName:
		// 目录名与标识不一致时，卸载会删错目录、启用会加载错插件。
		return fmt.Errorf("%w：metadata.name %q 与目录名 %q 不一致", ErrInvalidPackage, name, dirName)
	}
	m.Metadata.Name = name

	if m.Spec.DisplayName == "" {
		// 显示名缺省时退回标识：后台列表里总得有个能认的东西。
		m.Spec.DisplayName = name
	}
	if len(m.Spec.DisplayName) > maxDisplayNameLength {
		return fmt.Errorf("%w：spec.displayName 超过 %d 个字符", ErrInvalidPackage, maxDisplayNameLength)
	}
	if m.Spec.Version == "" {
		return fmt.Errorf("%w：spec.version 不能为空", ErrInvalidPackage)
	}
	if len(m.Spec.Version) > maxVersionLength {
		return fmt.Errorf("%w：spec.version 超过 %d 个字符", ErrInvalidPackage, maxVersionLength)
	}
	if !versionPattern.MatchString(m.Spec.Version) {
		return fmt.Errorf("%w：spec.version %q 须形如 1.0.0", ErrInvalidPackage, m.Spec.Version)
	}
	if len(m.Spec.Description) > maxDescriptionLength {
		return fmt.Errorf("%w：spec.description 超过 %d 个字符", ErrInvalidPackage, maxDescriptionLength)
	}
	if len([]rune(m.Spec.Author.Name)) > maxAuthorLength {
		return fmt.Errorf("%w：spec.author.name 超过 %d 个字符", ErrInvalidPackage, maxAuthorLength)
	}
	if len(m.Spec.Requires) > maxRequiresLength {
		return fmt.Errorf("%w：spec.requires 超过 %d 个字符", ErrInvalidPackage, maxRequiresLength)
	}
	for label, value := range map[string]string{
		"spec.homepage":       m.Spec.Homepage,
		"spec.repo":           m.Spec.Repo,
		"spec.author.website": m.Spec.Author.Website,
	} {
		if len(value) > maxURLLength {
			return fmt.Errorf("%w：%s 超过 %d 个字符", ErrInvalidPackage, label, maxURLLength)
		}
	}
	if len(m.Spec.License) > maxLicenseLength {
		return fmt.Errorf("%w：spec.license 超过 %d 个字符", ErrInvalidPackage, maxLicenseLength)
	}
	switch m.Spec.Runtime {
	case "", RuntimeWasm:
	default:
		return fmt.Errorf("%w：spec.runtime 只能留空或写 %s，实际 %q", ErrInvalidPackage, RuntimeWasm, m.Spec.Runtime)
	}
	if err := m.Spec.Capabilities.normalize(); err != nil {
		return err
	}
	if err := m.Spec.Hooks.normalize(&m.Spec.Capabilities, m.Spec.Runtime == RuntimeWasm); err != nil {
		return err
	}
	if m.Spec.Runtime == "" && m.Spec.Capabilities.NeedsBackend() {
		// 声明了却用不上：纯声明式插件没有代码去读内容、发请求，站长却要为这些权限点头。
		return fmt.Errorf("%w：读写内容、访问网络、发邮件与定时任务都要由后端代码使用，请同时声明 spec.runtime: %s",
			ErrInvalidPackage, RuntimeWasm)
	}
	return nil
}

// HasBackend 判断插件带不带后端代码。
func (m *Manifest) HasBackend() bool { return m.Spec.Runtime == RuntimeWasm }

// readManifest 从插件文件系统读取并解析清单。
func readManifest(fsys fs.FS, dirName string) (*Manifest, error) {
	data, err := fs.ReadFile(fsys, FileManifest)
	if err != nil {
		return nil, fmt.Errorf("%w：缺少 %s", ErrInvalidPackage, FileManifest)
	}
	return parseManifest(data, dirName)
}

// SettingsDecl 是 settings.yaml 的内容。
//
// 与主题的设置声明同构，故直接复用主题那边的解析结果形态：
// 一个分组列表，每组一份 JSON Schema 子集与缺省值（§5）。
type settingsFile struct {
	Groups []struct {
		Name        string         `yaml:"name"`
		Label       string         `yaml:"label"`
		Description string         `yaml:"description"`
		Order       int            `yaml:"order"`
		Icon        string         `yaml:"icon"`
		Schema      map[string]any `yaml:"schema"`
		Defaults    map[string]any `yaml:"defaults"`
	} `yaml:"groups"`
}

// maxSettingsGroups 是插件设置分组数上限。
//
// 插件设置是给站长在一个页面里调的，不是配置管理系统；几十个分组说明插件设计有问题。
const maxSettingsGroups = 20
