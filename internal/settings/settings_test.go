package settings

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/FeiBaiKin/lumo/internal/app"
)

// newService 登记 site 分组，不带存储：有效值就是缺省值。
func newService(t *testing.T) *Service {
	t.Helper()
	s := NewService(nil)
	if err := s.RegisterGroups([]app.SettingGroup{siteGroup()}); err != nil {
		t.Fatalf("登记 site 分组失败: %v", err)
	}
	return s
}

func TestSiteDefaultsPassSchema(t *testing.T) {
	t.Parallel()

	s := newService(t)
	var site Site
	if err := s.Get(context.Background(), GroupSite, &site); err != nil {
		t.Fatalf("读取缺省值失败: %v", err)
	}
	if site.Title != "Lumo" || site.Language != "zh-CN" || site.Timezone != "Asia/Shanghai" ||
		site.SlugStrategy != SlugUnicode || site.PageSize != 10 {
		t.Errorf("缺省值 = %+v", site)
	}
}

func TestValidationDetails(t *testing.T) {
	t.Parallel()

	g, ok := newService(t).Group(GroupSite)
	if !ok {
		t.Fatal("site 分组未登记")
	}

	tests := []struct {
		name     string
		values   map[string]any
		location string
	}{
		{"标题超长", map[string]any{"title": strings.Repeat("长", 129)}, "body.title"},
		{"未知字段", map[string]any{"bogus": 1}, "body"},
		{"枚举之外", map[string]any{"slugStrategy": "uuid"}, "body.slugStrategy"},
		{"整数越界", map[string]any{"pageSize": 1000}, "body.pageSize"},
		{"类型错误", map[string]any{"pageSize": "十"}, "body.pageSize"},
		{"时区不存在", map[string]any{"timezone": "Mars/Olympus"}, "body.timezone"},
		{"地址缺协议", map[string]any{"url": "example.com"}, "body.url"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := g.validate(merge(g.defaults, tt.values))
			var verr *ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("应返回 ValidationError，实际 %v", err)
			}
			found := false
			for _, d := range verr.Details {
				if d.Location == tt.location {
					found = true
				}
			}
			if !found {
				t.Errorf("明细未定位到 %s：%+v", tt.location, verr.Details)
			}
		})
	}

	valid := map[string]any{"title": "我的站点", "url": "https://example.com", "pageSize": 20, "slugStrategy": "pinyin"}
	if err := g.validate(merge(g.defaults, valid)); err != nil {
		t.Errorf("合法值不应报错: %v", err)
	}
}

func TestRegisterGroupsRejectsBadDeclarations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		group app.SettingGroup
	}{
		{"非法分组名", app.SettingGroup{Name: "Bad Name", Schema: json.RawMessage(`{"type":"object"}`)}},
		{"缺少 Schema", app.SettingGroup{Name: "x"}},
		{"Schema 非 object", app.SettingGroup{Name: "x", Schema: json.RawMessage(`{"type":"string"}`)}},
		{"Schema 不是 JSON", app.SettingGroup{Name: "x", Schema: json.RawMessage(`{`)}},
		{"Defaults 未通过 Schema", app.SettingGroup{
			Name:     "x",
			Schema:   json.RawMessage(`{"type":"object","properties":{"n":{"type":"integer"}},"required":["n"]}`),
			Defaults: json.RawMessage(`{"n":"one"}`),
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := NewService(nil).RegisterGroups([]app.SettingGroup{tt.group}); err == nil {
				t.Fatal("应拒绝非法声明")
			}
		})
	}

	s := NewService(nil)
	err := s.RegisterGroups([]app.SettingGroup{
		{Name: "b", Order: 1, Schema: json.RawMessage(`{"type":"object"}`)},
		{Name: "a", Order: 1, Schema: json.RawMessage(`{"type":"object"}`)},
		{Name: "z", Order: 0, Schema: json.RawMessage(`{"type":"object"}`)},
	})
	if err != nil {
		t.Fatalf("合法声明不应报错: %v", err)
	}
	groups := s.Groups()
	if len(groups) != 3 || groups[0].Name != "z" || groups[1].Name != "a" || groups[2].Name != "b" {
		t.Errorf("分组顺序应按 Order 再按名称：%v", names(groups))
	}
	if err := s.RegisterGroups([]app.SettingGroup{{Name: "a", Schema: json.RawMessage(`{"type":"object"}`)}}); err == nil {
		t.Error("重复分组名应报错")
	}
}

func names(groups []*Group) []string {
	out := make([]string, 0, len(groups))
	for _, g := range groups {
		out = append(out, g.Name)
	}
	return out
}

func TestSlugFallsBackWithoutStore(t *testing.T) {
	t.Parallel()

	s := newService(t)
	if got := s.Slug(context.Background(), "你好 World"); got != "你好-world" {
		t.Errorf("缺省策略应保留中文，得到 %q", got)
	}
	if got := NewService(nil).Slug(context.Background(), "你好"); got != "你好" {
		t.Errorf("分组未登记时应退回 unicode 策略，得到 %q", got)
	}
}
