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

// installEnabled 装上一组插件并依次启用。
func installEnabled(t *testing.T, m *Module, manifests ...[]byte) {
	t.Helper()
	for _, raw := range manifests {
		if err := install(t, m.registry, map[string][]byte{FileManifest: raw}); err != nil {
			t.Fatal(err)
		}
		parsed, err := parseManifest(raw, "")
		if err != nil {
			t.Fatal(err)
		}
		if err := m.registry.SetEnabled(context.Background(), parsed.Metadata.Name, true, false); err != nil {
			t.Fatalf("启用 %s：%v", parsed.Metadata.Name, err)
		}
	}
}

// 被依赖的插件升到范围之外，依赖方跟着停用；再升回范围之内，原因改写成可以重新启用。
func TestUpgradeOutOfRangeSuspendsDependents(t *testing.T) {
	m := testModule(t)
	installEnabled(t, m,
		manifestNamed("base", "1.0.0", ""),
		manifestNamed("app", "1.0.0", "  dependencies:\n"+dep("base", "^1.0")))

	if err := install(t, m.registry, map[string][]byte{FileManifest: manifestNamed("base", "2.0.0", "")}); err != nil {
		t.Fatal(err)
	}
	app, _ := m.registry.Get("app")
	if app.Enabled {
		t.Fatal("base 升到 2.0.0 后，要 ^1.0 的 app 该跟着停用")
	}
	for _, want := range []string{"base", "2.0.0"} {
		if !strings.Contains(app.DisabledReason, want) {
			t.Errorf("停用原因里该有 %q，得到 %q", want, app.DisabledReason)
		}
	}
	if base, _ := m.registry.Get("base"); !base.Enabled {
		t.Fatal("升级本身不该停掉 base")
	}

	if err := install(t, m.registry, map[string][]byte{FileManifest: manifestNamed("base", "1.5.0", "")}); err != nil {
		t.Fatal(err)
	}
	app, _ = m.registry.Get("app")
	if app.Enabled || app.DisabledReason != reasonDepsReady {
		t.Fatalf("base 回到范围内后，app 该停着并提示可以重新启用，得到 enabled=%v %q", app.Enabled, app.DisabledReason)
	}
}

// 升级后新声明的依赖没就绪，插件自己停用，不带着缺口继续跑。
func TestUpgradeAddingUnmetDependencySuspends(t *testing.T) {
	m := testModule(t)
	installEnabled(t, m, manifestNamed("app", "1.0.0", ""))
	err := install(t, m.registry, map[string][]byte{
		FileManifest: manifestNamed("app", "1.1.0", "  dependencies:\n"+dep("base", "")),
	})
	if err != nil {
		t.Fatal(err)
	}
	app, _ := m.registry.Get("app")
	if app.Enabled || !strings.Contains(app.DisabledReason, "缺少插件 base") {
		t.Fatalf("新依赖没装时该停用并说缺什么，得到 enabled=%v %q", app.Enabled, app.DisabledReason)
	}
}

// 系统自动停用（崩溃、要重新确认）同样连带依赖方，一路传下去。
func TestSuspendCascadesToDependents(t *testing.T) {
	m := testModule(t)
	ctx := context.Background()
	installEnabled(t, m,
		manifestNamed("base", "1.0.0", ""),
		manifestNamed("middle", "1.0.0", "  dependencies:\n"+dep("base", "")),
		manifestNamed("top", "1.0.0", "  dependencies:\n"+dep("middle", "")))

	m.registry.Suspend(ctx, "base", "连续 5 次运行出错，已自动停用")
	for _, name := range []string{"middle", "top"} {
		loaded, _ := m.registry.Get(name)
		if loaded.Enabled {
			t.Fatalf("%s 该被连带停用", name)
		}
		if !strings.HasPrefix(loaded.DisabledReason, dependencyReasonMark) {
			t.Fatalf("%s 的停用原因该说是依赖带的，得到 %q", name, loaded.DisabledReason)
		}
	}

	if err := m.registry.SetEnabled(ctx, "base", true, false); err != nil {
		t.Fatal(err)
	}
	middle, _ := m.registry.Get("middle")
	if middle.DisabledReason != reasonDepsReady {
		t.Fatalf("base 回来后 middle 该提示可以重新启用，得到 %q", middle.DisabledReason)
	}
	top, _ := m.registry.Get("top")
	if !strings.Contains(top.DisabledReason, "middle") {
		t.Fatalf("middle 还停着，top 的原因该继续指着它，得到 %q", top.DisabledReason)
	}
}

// 启动时依赖已经不在（库里记着启用、目录被删了之类），依赖方停用而不是照常起来。
func TestStartSuspendsWhenDependencyGone(t *testing.T) {
	m := testModule(t)
	ctx := context.Background()
	installEnabled(t, m,
		manifestNamed("base", "1.0.0", ""),
		manifestNamed("app", "1.0.0", "  dependencies:\n"+dep("base", "")))

	// 模拟重启时读到的状态：app 记着启用，base 却不在了
	m.registry.mu.Lock()
	delete(m.registry.plugins, "base")
	m.registry.mu.Unlock()

	app, _ := m.registry.Get("app")
	m.registry.startOrSuspend(ctx, app)
	if app, _ = m.registry.Get("app"); app.Enabled || !strings.Contains(app.DisabledReason, "缺少插件 base") {
		t.Fatalf("依赖不在时该停用并说缺什么，得到 enabled=%v %q", app.Enabled, app.DisabledReason)
	}
}
