package plugin

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// manifestNamed 造一份指定标识与版本的清单，extra 接在 spec 下面。
func manifestNamed(name, version, extra string) []byte {
	return []byte("apiVersion: plugin.lumo.run/v1alpha1\nkind: Plugin\nmetadata:\n  name: " + name +
		"\nspec:\n  version: " + version + "\n" + extra)
}

// dep 拼一条依赖声明。
func dep(name, rang string) string {
	return "    - {name: " + name + ", version: \"" + rang + "\"}\n"
}

// 依赖没就绪时启用不了；依赖就绪后能启用；被依赖者停用会连带停用依赖方，
// 重新启用后依赖方的停用原因改写成「可以再次启用」，但仍要站长点一下才回来。
func TestDependencyGatesEnableAndCascades(t *testing.T) {
	m := testModule(t)
	ctx := context.Background()

	if err := install(t, m.registry, map[string][]byte{
		FileManifest: manifestNamed("base", "1.2.0", ""),
	}); err != nil {
		t.Fatal(err)
	}
	if err := install(t, m.registry, map[string][]byte{
		FileManifest: manifestNamed("app", "1.0.0", "  dependencies:\n"+dep("base", ">=1.0.0")),
	}); err != nil {
		t.Fatal(err)
	}

	// 装得上不等于启用得了：base 还没启用
	err := m.registry.SetEnabled(ctx, "app", true, false)
	if !errors.Is(err, ErrMissingDeps) {
		t.Fatalf("依赖没启用时该返回 ErrMissingDeps，得到 %v", err)
	}
	if !strings.Contains(err.Error(), "base") {
		t.Fatalf("报错该点名是哪个插件，得到 %v", err)
	}
	if loaded, _ := m.registry.Get("app"); loaded.Enabled {
		t.Fatal("启用失败后不该是启用状态")
	}

	if err := m.registry.SetEnabled(ctx, "base", true, false); err != nil {
		t.Fatal(err)
	}
	if err := m.registry.SetEnabled(ctx, "app", true, false); err != nil {
		t.Fatalf("依赖就绪后该能启用：%v", err)
	}

	// 后台要在确认框里列出会被连带停用的插件
	if got := m.registry.EnabledDependents("base"); len(got) != 1 || got[0] != "app" {
		t.Fatalf("base 的启用中依赖方该是 [app]，得到 %v", got)
	}

	// 停用 base 连带停用 app，并记下原因
	if err := m.registry.SetEnabled(ctx, "base", false, false); err != nil {
		t.Fatal(err)
	}
	app, _ := m.registry.Get("app")
	if app.Enabled {
		t.Fatal("被依赖者停用后，依赖方该跟着停用")
	}
	if !strings.Contains(app.DisabledReason, "base") || !strings.Contains(app.DisabledReason, "停用") {
		t.Fatalf("停用原因该说清是被谁带的，得到 %q", app.DisabledReason)
	}

	// 依赖回来了：原因改写，但插件仍停着，等站长点
	if err := m.registry.SetEnabled(ctx, "base", true, false); err != nil {
		t.Fatal(err)
	}
	if app, _ = m.registry.Get("app"); app.Enabled {
		t.Fatal("依赖恢复不该悄悄把插件启用回来")
	}
	if !strings.Contains(app.DisabledReason, "重新启用") {
		t.Fatalf("依赖恢复后原因该改写，得到 %q", app.DisabledReason)
	}
	if err := m.registry.SetEnabled(ctx, "app", true, false); err != nil {
		t.Fatalf("站长点一下该能启用：%v", err)
	}
	if app, _ = m.registry.Get("app"); app.DisabledReason != "" {
		t.Fatalf("启用后不该还留着停用原因，得到 %q", app.DisabledReason)
	}
}

// 版本不满足与缺插件都要说清是哪种。
func TestDependencyVersionAndMissing(t *testing.T) {
	m := testModule(t)
	ctx := context.Background()
	if err := install(t, m.registry, map[string][]byte{
		FileManifest: manifestNamed("base", "1.2.0", ""),
	}); err != nil {
		t.Fatal(err)
	}
	if err := m.registry.SetEnabled(ctx, "base", true, false); err != nil {
		t.Fatal(err)
	}
	if err := install(t, m.registry, map[string][]byte{
		FileManifest: manifestNamed("app", "1.0.0", "  dependencies:\n"+dep("base", ">=2.0.0")),
	}); err != nil {
		t.Fatal(err)
	}

	err := m.registry.SetEnabled(ctx, "app", true, false)
	if !errors.Is(err, ErrMissingDeps) {
		t.Fatalf("版本不够该返回 ErrMissingDeps，得到 %v", err)
	}
	for _, want := range []string{"base", "1.2.0", ">=2.0.0"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("报错里该有 %q，得到 %v", want, err)
		}
	}

	// 接口视图要让后台看出是哪一条不满足
	loaded, _ := m.registry.Get("app")
	statuses := m.registry.DepStatuses(loaded.Manifest.Spec.Dependencies)
	if len(statuses) != 1 || !statuses[0].Installed || !statuses[0].Enabled || statuses[0].Satisfied {
		t.Fatalf("依赖状态该是「装了、启用了、版本不够」，得到 %+v", statuses)
	}

	// 压根没装的依赖
	err = install(t, m.registry, map[string][]byte{
		FileManifest: manifestNamed("other", "1.0.0", "  dependencies:\n"+dep("nowhere", "")),
	})
	if err != nil {
		t.Fatal(err)
	}
	err = m.registry.SetEnabled(ctx, "other", true, false)
	if !errors.Is(err, ErrMissingDeps) || !strings.Contains(err.Error(), "缺少插件 nowhere") {
		t.Fatalf("缺插件该明说，得到 %v", err)
	}
}

// 卸载被依赖的插件同样连带停用依赖方。
func TestUninstallCascadesToDependents(t *testing.T) {
	m := testModule(t)
	ctx := context.Background()
	for _, files := range []map[string][]byte{
		{FileManifest: manifestNamed("base", "1.0.0", "")},
		{FileManifest: manifestNamed("app", "1.0.0", "  dependencies:\n"+dep("base", ""))},
	} {
		if err := install(t, m.registry, files); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.registry.SetEnabled(ctx, "base", true, false); err != nil {
		t.Fatal(err)
	}
	if err := m.registry.SetEnabled(ctx, "app", true, false); err != nil {
		t.Fatal(err)
	}
	if err := m.registry.Uninstall(ctx, "base", false); err != nil {
		t.Fatal(err)
	}
	app, _ := m.registry.Get("app")
	if app.Enabled {
		t.Fatal("被依赖者卸载后，依赖方该跟着停用")
	}
	if !strings.Contains(app.DisabledReason, "卸载") {
		t.Fatalf("原因该说清是被卸载带的，得到 %q", app.DisabledReason)
	}
	// 依赖方还在，只是停着
	if _, ok := m.registry.Get("app"); !ok {
		t.Fatal("连带停用不该把依赖方也卸了")
	}
}

// 清单校验：自依赖、重名、坏范围都要在安装时就被挡下。
func TestDependencyManifestValidation(t *testing.T) {
	cases := []struct {
		name  string
		spec  string
		wants string
	}{
		{"自依赖", "  dependencies:\n" + dep("demo", ""), "不能依赖自己"},
		{"重复", "  dependencies:\n" + dep("base", "") + dep("base", ">=1.0.0"), "重复"},
		{"坏范围", "  dependencies:\n" + dep("base", "1.*.3"), "版本范围"},
		{"空名字", "  dependencies:\n    - {version: \">=1.0.0\"}\n", "没写 name"},
		{"名字大写", "  dependencies:\n" + dep("Base", ""), "DNS-1123"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := parseManifest(manifestNamed("demo", "1.0.0", c.spec), "demo")
			if !errors.Is(err, ErrInvalidPackage) {
				t.Fatalf("本该被清单校验挡下，得到 %v", err)
			}
			if !strings.Contains(err.Error(), c.wants) {
				t.Fatalf("报错里该有 %q，得到 %v", c.wants, err)
			}
		})
	}
}

// 启动顺序：依赖先起，用它的后起。
func TestStartOrderPutsDependenciesFirst(t *testing.T) {
	m := testModule(t)
	ctx := context.Background()
	for _, files := range []map[string][]byte{
		{FileManifest: manifestNamed("bottom", "1.0.0", "")},
		{FileManifest: manifestNamed("middle", "1.0.0", "  dependencies:\n"+dep("bottom", ""))},
		{FileManifest: manifestNamed("top", "1.0.0", "  dependencies:\n"+dep("middle", ""))},
	} {
		if err := install(t, m.registry, files); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"bottom", "middle", "top"} {
		if err := m.registry.SetEnabled(ctx, name, true, false); err != nil {
			t.Fatalf("启用 %s：%v", name, err)
		}
	}
	var order []string
	for _, loaded := range m.registry.startOrder() {
		order = append(order, loaded.ID())
	}
	want := []string{"bottom", "middle", "top"}
	if len(order) != len(want) {
		t.Fatalf("启动顺序该有 %d 个，得到 %v", len(want), order)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("启动顺序该是 %v，得到 %v", want, order)
		}
	}
}
