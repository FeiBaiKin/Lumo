package plugin

import (
	"fmt"

	"github.com/FeiBaiKin/lumo/internal/auth/perm"
)

// PageDecl 是 plugin.yaml 里 spec.pages 的一项：后台的一张自定义页面。
//
// 页面是插件包里的一个 HTML 文件，放在隔离的 iframe 里：它跑在一个不透明的源上，
// 拿不到管理员的会话，只能经后台转发调用本插件自己的接口（见 docs/plugin-development.md 的「后台页面」）。
type PageDecl struct {
	// Path 是地址里的一段，后台地址为 /plugins/<插件>/p/<path>。
	Path        string `yaml:"path" json:"path"`
	Label       string `yaml:"label" json:"label"`
	Description string `yaml:"description" json:"description"`
	// Icon 是侧栏图标名，留空用拼图。
	Icon string `yaml:"icon" json:"icon"`
	// File 是包里 static/ 下的 .html 文件。
	File string `yaml:"file" json:"file"`
	// Menu 为 false 时不在侧栏出入口。缺省为 true。
	Menu *bool `yaml:"menu" json:"menu"`
	// Permission 是打开它要的权限串，缺省 plugins:manage。
	Permission string `yaml:"permission" json:"permission"`

	permission perm.Permission
}

// maxPages 是一个插件能声明的后台页面数。
const maxPages = 10

// InMenu 判断页面要不要在侧栏出入口。
func (p *PageDecl) InMenu() bool { return p.Menu == nil || *p.Menu }

// RequiredPermission 返回打开页面要的权限。
func (p *PageDecl) RequiredPermission() perm.Permission { return p.permission }

// normalizePages 校验 spec.pages。
func normalizePages(pages []PageDecl) error {
	if len(pages) > maxPages {
		return fmt.Errorf("%w：后台页面最多 %d 个", ErrInvalidPackage, maxPages)
	}
	seen := map[string]bool{}
	for i := range pages {
		p := &pages[i]
		if !cronNamePattern.MatchString(p.Path) || len(p.Path) > 64 || seen[p.Path] {
			return fmt.Errorf("%w：后台页面的 path %q 只能用小写字母、数字与连字符，且不能重复", ErrInvalidPackage, p.Path)
		}
		seen[p.Path] = true
		if err := checkStaticPath(p.File, ".html"); err != nil {
			return fmt.Errorf("%w：后台页面 %s 的 file %q %w", ErrInvalidPackage, p.Path, p.File, err)
		}
		if p.Label == "" {
			p.Label = p.Path
		}
		p.permission = perm.PluginsManage
		if p.Permission != "" {
			parsed, err := perm.Parse(p.Permission)
			if err != nil {
				return fmt.Errorf("%w：后台页面 %s 的 permission %q 不是有效的权限串", ErrInvalidPackage, p.Path, p.Permission)
			}
			p.permission = parsed
		}
	}
	return nil
}

// PageByPath 按 path 取后台页面。
func (l *Loaded) PageByPath(path string) (*PageDecl, bool) {
	for i := range l.Manifest.Spec.Pages {
		if l.Manifest.Spec.Pages[i].Path == path {
			return &l.Manifest.Spec.Pages[i], true
		}
	}
	return nil, false
}
