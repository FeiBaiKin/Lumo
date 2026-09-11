package extension

import (
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/FeiBaiKin/lumo/internal/api"
)

// Extension 是 Extension 平面上的一条记录。
//
// 表在核心迁移里就建好了（migrations/00001_init.sql），本模块只补一列 resource
// 与按它寻址的唯一索引，见 migrations/00001_extension.sql。
type Extension struct {
	bun.BaseModel `bun:"table:extensions,alias:ext"`

	ID       int64  `bun:"id,pk,autoincrement" json:"id"`
	APIGroup string `bun:"api_group,notnull"   json:"apiGroup" doc:"反向域名形式的 API 分组"`
	Version  string `bun:"version,notnull"     json:"version"  doc:"API 版本，如 v1alpha1"`
	Kind     string `bun:"kind,notnull"        json:"kind"     doc:"单数 PascalCase 的资源种类"`
	Resource string `bun:"resource,notnull"    json:"resource" doc:"URL 里的复数段，由 kind 推导"`
	Name     string `bun:"name,notnull"        json:"name"     doc:"DNS-1123 名称，在同一资源下唯一"`
	// Spec 是自定义字段，核心不解释其内容，只整体存取。
	Spec      map[string]any `bun:"spec,type:jsonb"     json:"spec" doc:"自定义字段，服务端不解释其内容"`
	CreatedAt time.Time      `bun:"created_at,nullzero" json:"createdAt"`
	UpdatedAt time.Time      `bun:"updated_at,nullzero" json:"updatedAt"`

	// SelfLink 是本记录的访问地址，不是数据库列。
	//
	// 由服务端给出而不是让客户端自己拼：kind 到复数段的映射规则在服务端，
	// 通用客户端拿到一条记录就该知道它住在哪儿。
	SelfLink string `bun:"-" json:"selfLink" doc:"本记录的 Extension 平面地址"`
}

// APIVersion 返回 Kubernetes 风格的 group/version 串，供客户端识别记录来源。
func (e *Extension) APIVersion() string { return e.APIGroup + "/" + e.Version }

// fill 补全派生字段：resource 缺省时由 kind 推导，selfLink 由四元组拼成。
//
// 从库里读出来的行已带 resource，此时不重算：存下来的值才是这条记录的实际地址。
func (e *Extension) fill() {
	if e.Resource == "" {
		e.Resource = Resource(e.Kind)
	}
	e.SelfLink = strings.Join(
		[]string{api.PrefixExtension, e.APIGroup, e.Version, e.Resource, e.Name}, "/")
	if e.Spec == nil {
		e.Spec = map[string]any{}
	}
}
