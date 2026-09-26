package plugin

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"

	"github.com/FeiBaiKin/lumo/internal/auth/perm"
	"github.com/FeiBaiKin/lumo/internal/extension"
	"github.com/FeiBaiKin/lumo/internal/form"
	"github.com/FeiBaiKin/lumo/internal/settings"
)

// ResourceDecl 是 plugin.yaml 里 spec.resources 的一项：插件自己的一类数据。
//
// 声明之后宿主替插件存数据（进 Extension 平面，按插件分组隔离），后台自动生成列表页与编辑页，
// 插件经宿主调用增删改查。字段用与设置同一套 Schema 描述，站长在后台看到的表单也是同一套控件。
type ResourceDecl struct {
	// Kind 是资源种类，单数 PascalCase，如 Visit。
	Kind string `yaml:"kind" json:"kind"`
	// Label 是显示名，如「访问记录」。
	Label       string `yaml:"label" json:"label"`
	Description string `yaml:"description" json:"description"`
	// Icon 是侧栏图标名（Console 的图标登记表），留空用拼图。
	Icon string `yaml:"icon" json:"icon"`
	// Schema 是字段声明，JSON Schema 子集，与设置分组相同。
	Schema map[string]any `yaml:"schema" json:"schema"`
	// Defaults 是新建记录时的缺省值。
	Defaults map[string]any `yaml:"defaults" json:"defaults"`
	// Columns 是列表页显示的字段，留空取前三个。
	Columns []string `yaml:"columns" json:"columns"`
	// Title 是用作记录标题的字段，留空取第一个文本字段。
	Title string `yaml:"title" json:"title"`
	// Editable 为 false 时后台只能查看，数据只由插件自己写。缺省为 true。
	Editable *bool `yaml:"editable" json:"editable"`
	// Menu 为 false 时不在侧栏出入口。缺省为 true。
	Menu *bool `yaml:"menu" json:"menu"`
	// Permission 是后台查看与编辑它要的权限串，缺省 plugins:manage。
	Permission string `yaml:"permission" json:"permission"`
}

// Resource 是编译好的资源声明。
type Resource struct {
	Kind string
	// Path 是 URL 里的复数段，由 Kind 推导，如 visits。
	Path        string
	Label       string
	Description string
	Icon        string
	Columns     []string
	Title       string
	Editable    bool
	Menu        bool
	Permission  perm.Permission
	Form        *form.Form
	validator   *settings.Validator
	// numeric 是数值型字段：按它们排序时要当数字比，而不是当文本。
	numeric map[string]bool
}

// Validator 返回字段校验器。
func (r *Resource) Validator() *settings.Validator { return r.validator }

// HasField 判断资源有没有某个顶层字段。
func (r *Resource) HasField(name string) bool {
	_, ok := r.Form.Field(name)
	return ok
}

// 资源声明的上限。
const (
	maxResources     = 20
	maxResourceField = 64
)

var resourceKindPattern = regexp.MustCompile(extension.KindPattern)

// ResourceGroup 返回插件资源在 Extension 平面上的分组：每个插件一组，插件之间看不见彼此的数据。
func ResourceGroup(plugin string) string { return extension.GroupLumo + ".plugin." + plugin }

// ResourceVersion 是插件资源统一使用的版本。
const ResourceVersion = "v1"

// compileResources 校验并编译 spec.resources。
func compileResources(decls []ResourceDecl) ([]Resource, error) {
	if len(decls) > maxResources {
		return nil, fmt.Errorf("%w：spec.resources 最多声明 %d 种", ErrInvalidPackage, maxResources)
	}
	out := make([]Resource, 0, len(decls))
	seen := map[string]bool{}
	for i := range decls {
		d := &decls[i]
		if !resourceKindPattern.MatchString(d.Kind) || len(d.Kind) > extension.MaxKindLen {
			return nil, fmt.Errorf("%w：资源种类 %q 须为单数 PascalCase，如 Visit", ErrInvalidPackage, d.Kind)
		}
		path := extension.Resource(d.Kind)
		if seen[path] {
			return nil, fmt.Errorf("%w：资源种类 %q 与另一种重名（地址都是 %s）", ErrInvalidPackage, d.Kind, path)
		}
		seen[path] = true
		if d.Schema == nil {
			return nil, fmt.Errorf("%w：资源 %s 缺少 schema", ErrInvalidPackage, d.Kind)
		}
		if d.Defaults == nil {
			d.Defaults = map[string]any{}
		}
		schema, err := json.Marshal(d.Schema)
		if err != nil {
			return nil, fmt.Errorf("%w：资源 %s 的 schema 无法编码: %w", ErrInvalidPackage, d.Kind, err)
		}
		defaults, err := json.Marshal(d.Defaults)
		if err != nil {
			return nil, fmt.Errorf("%w：资源 %s 的 defaults 无法编码: %w", ErrInvalidPackage, d.Kind, err)
		}
		parsed, err := form.Parse(path, schema, defaults)
		if err != nil {
			return nil, fmt.Errorf("%w：资源 %s：%w", ErrInvalidPackage, d.Kind, err)
		}
		if secretErr := settings.RejectSecrets("插件资源 "+d.Kind, parsed); secretErr != nil {
			return nil, fmt.Errorf("%w：%w", ErrInvalidPackage, secretErr)
		}
		validator, err := settings.NewValidator(path, parsed)
		if err != nil {
			return nil, fmt.Errorf("%w：资源 %s：%w", ErrInvalidPackage, d.Kind, err)
		}
		top := parsed.Fields()
		if len(top) > maxResourceField {
			return nil, fmt.Errorf("%w：资源 %s 的字段不得超过 %d 个", ErrInvalidPackage, d.Kind, maxResourceField)
		}
		fields := make([]string, 0, len(top))
		types := make(map[string]form.Type, len(top))
		for _, f := range top {
			fields = append(fields, f.Key())
			types[f.Key()] = f.Type()
		}
		res := Resource{
			Kind: d.Kind, Path: path, Label: d.Label, Description: d.Description, Icon: d.Icon,
			Columns: d.Columns, Title: d.Title, Editable: d.Editable == nil || *d.Editable,
			Menu: d.Menu == nil || *d.Menu, Permission: perm.PluginsManage,
			Form: parsed, validator: validator, numeric: map[string]bool{},
		}
		if res.Label == "" {
			res.Label = d.Kind
		}
		if d.Permission != "" {
			p, err := perm.Parse(d.Permission)
			if err != nil {
				return nil, fmt.Errorf("%w：资源 %s 的 permission %q 不是有效的权限串", ErrInvalidPackage, d.Kind, d.Permission)
			}
			res.Permission = p
		}
		for name, typ := range types {
			if typ == form.TypeInteger || typ == form.TypeNumber {
				res.numeric[name] = true
			}
		}
		if len(res.Columns) == 0 {
			res.Columns = fields[:min(3, len(fields))]
		}
		for _, col := range res.Columns {
			if !slices.Contains(fields, col) {
				return nil, fmt.Errorf("%w：资源 %s 的 columns 里的 %q 不是它的字段", ErrInvalidPackage, d.Kind, col)
			}
		}
		if res.Title == "" {
			for _, name := range fields {
				if types[name] == form.TypeString {
					res.Title = name
					break
				}
			}
		} else if !slices.Contains(fields, res.Title) {
			return nil, fmt.Errorf("%w：资源 %s 的 title %q 不是它的字段", ErrInvalidPackage, d.Kind, res.Title)
		}
		out = append(out, res)
	}
	return out, nil
}

// ResourceByPath 按 URL 复数段取插件的资源声明。
func (l *Loaded) ResourceByPath(path string) (*Resource, bool) {
	for i := range l.Resources {
		if l.Resources[i].Path == path {
			return &l.Resources[i], true
		}
	}
	return nil, false
}

// ResourceByKind 按种类取插件的资源声明。
func (l *Loaded) ResourceByKind(kind string) (*Resource, bool) {
	for i := range l.Resources {
		if l.Resources[i].Kind == kind {
			return &l.Resources[i], true
		}
	}
	return nil, false
}

// Normalize 用缺省值补齐一条记录并按字段声明校验，返回要存下来的完整数据。
//
// 插件经宿主写入与站长在后台编辑走同一道校验：两边写进去的东西长得一样，列表页才画得出来。
func (r *Resource) Normalize(data map[string]any) (map[string]any, error) {
	merged := settings.Merge(r.validator.DefaultValues(), data)
	if err := r.validator.Validate(merged); err != nil {
		return nil, err
	}
	return merged, nil
}
