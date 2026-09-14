package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/settings"
)

// TestNavigationContract 校验各模块声明的 Console 菜单落在前端的渲染能力范围内。
//
// 与 TestSettingsSchemaContract 同源：菜单由 Go 侧声明、TS 侧渲染，中间没有编译期约束。
// 最容易出问题的是图标名——后端给出的是一个字符串，前端认不出时只会画一个默认图标，
// 界面上不会有任何提示，而「这一项怎么长得跟别的不一样」几乎没人会去追。
//
// 因此图标清单**从 console/src/lib/icons.ts 里读**，不在 Go 侧再抄一份。
func TestNavigationContract(t *testing.T) {
	icons := tsIcons(t)

	application := app.New(&app.Options{
		Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
	})
	if err := application.Register(modules()...); err != nil {
		t.Fatalf("装配模块失败: %v", err)
	}
	// 设置分组的登记在 Start 里（那时全部模块才注册完毕），而设置页的菜单入口
	// 正是从那份分组列表推导出来的。serve 会走这一步，测试也必须走，
	// 否则「评论分组有页面没入口」这类问题在这条测试里永远是绿的。
	if svc := settings.From(application); svc != nil {
		if err := svc.RegisterGroups(application.Settings()); err != nil {
			t.Fatalf("登记设置分组失败: %v", err)
		}
	}

	nav := application.Navigation()
	if len(nav.Groups) == 0 {
		t.Fatal("没有任何菜单分组")
	}
	if len(nav.Items) == 0 {
		t.Fatal("没有任何菜单项")
	}

	groups := make(map[string]bool, len(nav.Groups))
	for _, group := range nav.Groups {
		if group.Label == "" {
			t.Errorf("分组 %q 缺少显示名", group.Name)
		}
		groups[group.Name] = true
	}

	paths := make(map[string]string, len(nav.Items))
	for _, item := range nav.Items {
		label := fmt.Sprintf("菜单项 [%s]", item.Key)
		if !strings.HasPrefix(item.Path, "/") {
			t.Errorf("%s 的路径 %q 不是绝对路径；相对路径会被解析到当前页之下，表现为「点了没反应」", label, item.Path)
		}
		// 路径冲突的结果是两项互相遮蔽，而界面上看起来只是「少了一个入口」。
		if other, dup := paths[item.Path]; dup {
			t.Errorf("%s 与 [%s] 指向同一个路径 %q", label, other, item.Path)
		}
		paths[item.Path] = item.Key

		if !groups[item.Group] {
			t.Errorf("%s 属于未声明的分组 %q，它不会出现在侧边栏里", label, item.Group)
		}
		if !icons[item.Icon] {
			t.Errorf("%s 的图标 %q 不在 console/src/lib/icons.ts 的登记表里，"+
				"前端会画一个默认图标且不做任何提示", label, item.Icon)
		}
		if item.Label == "" {
			t.Errorf("%s 缺少显示名", label)
		}
	}

	// 权限串拼错的表现是「这一项永远不显示」，而菜单是显示条件、不是安全边界，
	// 服务端不会因此报错，故只能在这里核对。
	known := map[string]bool{}
	for _, permission := range append(app.CorePermissions(), application.Permissions()...) {
		known[permission.Key] = true
	}
	for _, item := range nav.Items {
		if item.Permission != "" && !known[item.Permission] {
			t.Errorf("菜单项 [%s] 要求的权限 %q 不在权限清单里，它永远不会显示",
				item.Key, item.Permission)
		}
	}

	// 设置分组不各占一条侧边栏菜单：它们全都铺在 /settings 这一页上。
	//
	// 分组仍各下发一条 Hidden 的菜单项（命令面板与标题映射要用）。漏写 Hidden
	// 不会有任何报错，只会让侧边栏重新长出一串与页内区块一一重复的入口——
	// 也就是「同一个动作画两遍」，正是这次合并要去掉的东西。
	// 故按声明本身核对，而不是把分组名硬编码进来。
	for _, group := range application.Settings() {
		want := "/settings/" + group.Name
		for _, item := range nav.Items {
			if item.Path == want && !item.Hidden {
				t.Errorf("设置分组 %q 在侧边栏里仍有独立入口 [%s]（%s）；"+
					"它应当只作为 /settings 页里的一个区块，菜单项须标为 Hidden",
					group.Name, item.Key, want)
			}
		}
	}

	// 收成一个入口之后，那一个入口就必须在。它要是没了，设置页从侧边栏再也进不去。
	var settingsEntry bool
	for _, item := range nav.Items {
		if item.Path == "/settings" && !item.Hidden {
			settingsEntry = true
		}
	}
	if !settingsEntry {
		t.Error("侧边栏里没有指向 /settings 的可见入口，设置页无从进入")
	}

	// 菜单清单变了却没人检查时这条测试会静默空转。
	if len(nav.Items) < 15 {
		t.Errorf("只检查到 %d 个菜单项，期望至少 15 个——模块是不是漏声明了？", len(nav.Items))
	}
	t.Logf("已校验 %d 个分组、%d 个菜单项，前端登记了 %d 个图标",
		len(nav.Groups), len(nav.Items), len(icons))
}

// tsIcons 从 Console 的图标登记表里读出全部图标名。
func tsIcons(t *testing.T) map[string]bool {
	t.Helper()

	path := filepath.Join("..", "..", "console", "src", "lib", "icons.ts")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取图标登记表失败: %v", err)
	}

	block := regexp.MustCompile(`(?s)export const ICONS: Record<string, LucideIcon> = \{(.*?)\n\};`).
		FindSubmatch(data)
	if block == nil {
		t.Fatalf("%s 里找不到 ICONS 登记表——它的形态改了，这条契约测试要跟着改", path)
	}

	out := map[string]bool{}
	for _, match := range regexp.MustCompile(`(?m)^\s*"?([a-z0-9-]+)"?:\s`).FindAllSubmatch(block[1], -1) {
		out[string(match[1])] = true
	}
	if len(out) == 0 {
		t.Fatalf("%s 里的 ICONS 是空的，解析大概失效了", path)
	}
	return out
}
