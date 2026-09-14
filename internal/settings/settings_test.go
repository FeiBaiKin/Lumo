package settings

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/form"
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

// tinyForm 构造一个最小的合法表单，供只关心装配行为的用例使用。
//
// 必须至少有一个字段：空表单是声明错误（见 internal/form 的 compileDSL），
// 而那正好也是本文件要覆盖的一类。
func tinyForm(key string) *form.Form {
	return form.New(form.NewSection("s",
		form.Text(key).Label("字段").Default(""),
	))
}

func TestRegisterGroupsRejectsBadDeclarations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		group app.SettingGroup
	}{
		{"非法分组名", app.SettingGroup{Name: "Bad Name", Form: tinyForm("a")}},
		{"缺少表单声明", app.SettingGroup{Name: "x"}},
		{
			// 声明错误由 internal/form 在构建时报出，这里确认它确实传得上来：
			// 模块作者写错一个字段，应当在启动那一刻就失败，而不是等有人点开那一页。
			"表单声明有误",
			app.SettingGroup{Name: "x", Form: form.New(form.NewSection("s", form.Text("a")))},
		},
		{
			// 缺省值必须能通过自己的 Schema，否则站长打开设置页会看到一堆存不进去的初始值。
			"缺省值未通过自身 Schema",
			app.SettingGroup{Name: "x", Form: form.New(form.NewSection("s",
				form.Int("n").Label("数量").Default("one"),
			))},
		},
		{
			// 主开关写错名字的表现是「设置页那一块的标题栏上少个开关」——
			// 界面上没有任何提示，故必须在注册时失败。
			"主开关不在表单里",
			app.SettingGroup{Name: "x", Toggle: "enabled", Form: tinyForm("a")},
		},
		{
			// 非布尔字段当不了开关：界面要么画不出来，要么画出一个存不回去的开关。
			"主开关不是布尔字段",
			app.SettingGroup{Name: "x", Toggle: "a", Form: tinyForm("a")},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := NewService(nil).RegisterGroups([]app.SettingGroup{tt.group}); err == nil {
				t.Fatal("应拒绝非法声明")
			}
		})
	}
}

func TestToggleDeclarationAccepted(t *testing.T) {
	t.Parallel()

	g := app.SettingGroup{Name: "x", Toggle: "on", Form: form.New(form.NewSection("s",
		form.Bool("on").Label("开").Default(false),
	))}
	s := NewService(nil)
	if err := s.RegisterGroups([]app.SettingGroup{g}); err != nil {
		t.Fatalf("布尔主开关应被接受: %v", err)
	}
	got, ok := s.Group("x")
	if !ok {
		t.Fatal("分组未登记")
	}
	// 接口要把它带给前端，否则声明了也没人用得上。
	if got.Toggle != "on" {
		t.Errorf("Toggle = %q，应为 on", got.Toggle)
	}
}

func TestRegisterGroupsSortsByOrderThenName(t *testing.T) {
	t.Parallel()

	s := NewService(nil)
	err := s.RegisterGroups([]app.SettingGroup{
		{Name: "b", Order: 1, Form: tinyForm("a")},
		{Name: "a", Order: 1, Form: tinyForm("a")},
		{Name: "z", Order: 0, Form: tinyForm("a")},
	})
	if err != nil {
		t.Fatalf("合法声明不应报错: %v", err)
	}
	groups := s.Groups()
	if len(groups) != 3 || groups[0].Name != "z" || groups[1].Name != "a" || groups[2].Name != "b" {
		t.Errorf("分组顺序应按 Order 再按名称：%v", names(groups))
	}
	if err := s.RegisterGroups([]app.SettingGroup{{Name: "a", Form: tinyForm("a")}}); err == nil {
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
