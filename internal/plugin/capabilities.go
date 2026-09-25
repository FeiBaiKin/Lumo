package plugin

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/FeiBaiKin/lumo/internal/auth/perm"
)

// Capabilities 是插件要用的宿主能力，写在 plugin.yaml 的 spec.capabilities 里。
//
// 日志、读自己的设置、存自己的数据是每个带后端的插件都有的，不在此列；这里只放
// 需要站长点头的那些。启用时逐项列给站长看，确认后记下「授予了什么」；
// 升级后声明多出来的部分没被授予过，插件就先停用、等再次确认。
type Capabilities struct {
	// Content 是对站点内容的读写。
	Content ContentAccess `yaml:"content" json:"content"`
	// HTTP 是允许访问的外部域名：精确域名，或 *.example.com 形式的一级通配。
	HTTP []string `yaml:"http" json:"http"`
	// Mail 表示要经站点的 SMTP 发邮件。
	Mail bool `yaml:"mail" json:"mail"`
	// Cron 表示要定时运行任务。
	Cron bool `yaml:"cron" json:"cron"`
	// Frontend 表示要往前台页面里放东西（插槽、小组件、短代码、静态脚本与样式）。
	Frontend bool `yaml:"frontend" json:"frontend"`
}

// ContentAccess 是对站点内容的读写权限。
type ContentAccess struct {
	// Read 表示读取文章、页面、分类、标签、评论与用户公开资料。
	Read bool `yaml:"read" json:"read"`
	// Write 是写操作要用的权限串，写入时按它判定；声明了写就隐含可读。
	Write []string `yaml:"write" json:"write"`
}

// writablePermissions 是插件能申请的写权限：只限内容类。
//
// 用户、角色、主题、设置、插件、站点这几类一律不给：插件拿到它们就能给自己提权，
// 或者把站点改得面目全非——那不是「一个功能」该有的权力。content:unsafe_html 同理，
// 它等于允许插件往正文里放任意脚本。
var writablePermissions = map[perm.Permission]bool{
	perm.PostsWrite:        true,
	perm.PostsWriteAny:     true,
	perm.PostsPublish:      true,
	perm.PostsDeleteAny:    true,
	perm.PagesWrite:        true,
	perm.PagesWriteAny:     true,
	perm.PagesPublish:      true,
	perm.PagesDeleteAny:    true,
	perm.TaxonomiesManage:  true,
	perm.CommentsManage:    true,
	perm.CommentsManageAny: true,
	perm.MediaWrite:        true,
	perm.MediaDeleteAny:    true,
}

// maxHTTPHosts 是一个插件能声明的外部域名数上限。
const maxHTTPHosts = 20

// hostLabel 是域名里的一段（LDH 规则）。
var hostLabel = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`)

// IsZero 判断插件是否没有申请任何需要确认的能力。
func (c *Capabilities) IsZero() bool {
	return !c.Content.Read && len(c.Content.Write) == 0 && len(c.HTTP) == 0 &&
		!c.Mail && !c.Cron && !c.Frontend
}

// NeedsBackend 判断声明的能力里有没有只能由后端代码使用的。
func (c *Capabilities) NeedsBackend() bool {
	return c.Content.Read || len(c.Content.Write) > 0 || len(c.HTTP) > 0 || c.Mail || c.Cron
}

// normalize 校验并规范化：去重、排序、小写，写权限隐含读。
func (c *Capabilities) normalize() error {
	writes := make([]string, 0, len(c.Content.Write))
	for _, raw := range c.Content.Write {
		p, err := perm.Parse(strings.TrimSpace(raw))
		if err != nil {
			return fmt.Errorf("%w：spec.capabilities.content.write 里的 %q 不是有效的权限串", ErrInvalidPackage, raw)
		}
		if !writablePermissions[p] {
			return fmt.Errorf("%w：插件不能申请 %s 权限，只能申请文章、页面、分类、评论与附件的写权限", ErrInvalidPackage, p)
		}
		writes = append(writes, p.String())
	}
	slices.Sort(writes)
	c.Content.Write = slices.Compact(writes)
	if len(c.Content.Write) > 0 {
		c.Content.Read = true
	}

	if len(c.HTTP) > maxHTTPHosts {
		return fmt.Errorf("%w：spec.capabilities.http 最多声明 %d 个域名", ErrInvalidPackage, maxHTTPHosts)
	}
	hosts := make([]string, 0, len(c.HTTP))
	for _, raw := range c.HTTP {
		host := strings.ToLower(strings.TrimSpace(raw))
		if err := validateHost(host); err != nil {
			return fmt.Errorf("%w：spec.capabilities.http 里的 %q %w", ErrInvalidPackage, raw, err)
		}
		hosts = append(hosts, host)
	}
	slices.Sort(hosts)
	c.HTTP = slices.Compact(hosts)
	return nil
}

// validateHost 校验一个外部域名声明：必须是公网域名形态，不收 IP、端口、路径与 localhost。
func validateHost(host string) error {
	name := strings.TrimPrefix(host, "*.")
	labels := strings.Split(name, ".")
	switch {
	case host == "":
		return fmt.Errorf("不能为空")
	case len(name) > 253:
		return fmt.Errorf("过长")
	case strings.ContainsAny(host, ":/@[]"):
		return fmt.Errorf("只写域名，不带协议、端口或路径")
	case len(labels) < 2:
		return fmt.Errorf("须是完整域名，如 api.example.com")
	case strings.Count(host, "*") > 1 || (strings.Contains(host, "*") && !strings.HasPrefix(host, "*.")):
		return fmt.Errorf("通配只能写在最前面，如 *.example.com")
	}
	allDigits := true
	for _, label := range labels {
		if !hostLabel.MatchString(label) {
			return fmt.Errorf("不是合法的域名")
		}
		if strings.Trim(label, "0123456789") != "" {
			allDigits = false
		}
	}
	if allDigits {
		return fmt.Errorf("不能写 IP 地址")
	}
	if name == "localhost" || strings.HasSuffix(name, ".localhost") || strings.HasSuffix(name, ".local") ||
		strings.HasSuffix(name, ".internal") {
		return fmt.Errorf("不能访问本机或内网域名")
	}
	return nil
}

// Covers 判断已授予的能力是否覆盖 want：升级后的插件是否不需要再征得同意。
func (c *Capabilities) Covers(want *Capabilities) bool {
	if want.Content.Read && !c.Content.Read {
		return false
	}
	for _, p := range want.Content.Write {
		if !slices.Contains(c.Content.Write, p) {
			return false
		}
	}
	for _, host := range want.HTTP {
		if !slices.Contains(c.HTTP, host) {
			return false
		}
	}
	return (!want.Mail || c.Mail) && (!want.Cron || c.Cron) && (!want.Frontend || c.Frontend)
}

// AllowsHost 判断插件能否访问某个主机名。
func (c *Capabilities) AllowsHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, allowed := range c.HTTP {
		if suffix, ok := strings.CutPrefix(allowed, "*."); ok {
			if strings.HasSuffix(host, "."+suffix) {
				return true
			}
		} else if host == allowed {
			return true
		}
	}
	return false
}

// AllowsWrite 判断插件被授予了某个写权限。
func (c *Capabilities) AllowsWrite(p perm.Permission) bool {
	return slices.Contains(c.Content.Write, p.String())
}

// capabilityLine 是给站长看的一条能力说明。
type capabilityLine struct {
	// Key 是能力类别：content.read、content.write、http、mail、cron、frontend。
	Key string `json:"key"`
	// Title 是一句话说明这项能力是什么。
	Title string `json:"title"`
	// Detail 是具体范围：哪些权限、哪些域名。
	Detail string `json:"detail"`
}

// describeCapabilities 把能力声明写成给站长看的清单，启用前的确认框与插件详情都用它。
func (m *Module) describeCapabilities(c *Capabilities) []capabilityLine {
	out := []capabilityLine{}
	if c.Content.Read {
		out = append(out, capabilityLine{Key: "content.read", Title: "读取站点内容", Detail: "文章、页面、分类、标签、评论与用户的公开资料"})
	}
	if len(c.Content.Write) > 0 {
		labels := map[string]string{}
		if m.app != nil {
			for _, p := range m.app.Permissions() {
				labels[p.Key] = p.Label
			}
		}
		names := make([]string, 0, len(c.Content.Write))
		for _, key := range c.Content.Write {
			if label := labels[key]; label != "" {
				names = append(names, label)
			} else {
				names = append(names, key)
			}
		}
		out = append(out, capabilityLine{Key: "content.write", Title: "修改站点内容", Detail: strings.Join(names, "、")})
	}
	if len(c.HTTP) > 0 {
		out = append(out, capabilityLine{Key: "http", Title: "访问外部网络", Detail: "只能访问 " + strings.Join(c.HTTP, "、")})
	}
	if c.Mail {
		out = append(out, capabilityLine{Key: "mail", Title: "发送邮件", Detail: "经站点配置的 SMTP 发出，有频率限制"})
	}
	if c.Cron {
		out = append(out, capabilityLine{Key: "cron", Title: "定时运行任务", Detail: "按插件设定的间隔在后台执行"})
	}
	if c.Frontend {
		out = append(out, capabilityLine{Key: "frontend", Title: "在前台页面里放东西", Detail: "页头、页脚、正文前后、评论区下方、侧边栏小组件与文章里的短代码"})
	}
	return out
}
