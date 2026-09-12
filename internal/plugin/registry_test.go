package plugin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeInstalled 在 root 下落一个可用的插件目录。
func writeInstalled(t *testing.T, root, name, settings string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, FileManifest), []byte(manifestYAML(name)), 0o640); err != nil {
		t.Fatal(err)
	}
	if settings != "" {
		if err := os.WriteFile(filepath.Join(dir, FileSettings), []byte(settings), 0o640); err != nil {
			t.Fatal(err)
		}
	}
}

// newTestRegistry 构造一个不带数据库的注册表。
//
// store 为 nil 时 Load 只走文件系统那一半，正好覆盖「目录里有什么」
// 这一类判断；与库对齐的那一半由集成测试与实机走查覆盖。
func newTestRegistry(t *testing.T) (registry *Registry, root string) {
	t.Helper()
	root = t.TempDir()
	return NewRegistry(&RegistryOptions{Root: root}), root
}

func TestRegistryLoadsInstalledPlugins(t *testing.T) {
	t.Parallel()

	r, root := newTestRegistry(t)
	writeInstalled(t, root, "alpha", "")
	writeInstalled(t, root, "beta", settingsYAML)

	if err := r.Load(context.Background()); err != nil {
		t.Fatalf("加载失败: %v", err)
	}

	list := r.List()
	if len(list) != 2 {
		t.Fatalf("已安装 = %d 个，期望 2", len(list))
	}
	// 排序稳定：列表顺序每次刷新都一样，否则用户会以为数据在变
	if list[0].ID() != "alpha" || list[1].ID() != "beta" {
		t.Errorf("顺序 = %s, %s", list[0].ID(), list[1].ID())
	}
	// 新装入的一律是停用状态：插件能改后台的行为，一装就生效来不及看清
	if len(r.Enabled()) != 0 {
		t.Errorf("刚加载时不该有启用的插件，实际 %d 个", len(r.Enabled()))
	}
	loaded, ok := r.Get("beta")
	if !ok {
		t.Fatal("beta 应当已加载")
	}
	if len(loaded.Groups) != 1 || loaded.Groups[0].Name != "sync" {
		t.Errorf("beta 的设置分组 = %+v", loaded.Groups)
	}
}

// TestRegistryRecordsBroken 确认坏掉的插件被记下来而不是让整个加载失败。
//
// 一个插件装坏了不该让整站起不来——这与主题那边的取舍是同一条
// （见 internal/theme 的 Registry.LoadInstalled）。
func TestRegistryRecordsBroken(t *testing.T) {
	t.Parallel()

	r, root := newTestRegistry(t)
	writeInstalled(t, root, "good", "")
	// 清单缺字段
	bad := filepath.Join(root, "bad")
	if err := os.MkdirAll(bad, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad, FileManifest), []byte("apiVersion: nope\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	// 目录名与清单里的标识对不上
	writeInstalled(t, root, "actual-name", "")
	if err := os.Rename(filepath.Join(root, "actual-name"), filepath.Join(root, "other-name")); err != nil {
		t.Fatal(err)
	}

	if err := r.Load(context.Background()); err != nil {
		t.Fatalf("加载失败: %v", err)
	}

	if len(r.List()) != 1 {
		t.Errorf("可用的插件 = %d 个，期望只有 good", len(r.List()))
	}
	broken := r.Broken()
	if len(broken) != 2 {
		t.Fatalf("损坏的插件 = %v，期望 2 个", broken)
	}
	if !strings.Contains(broken["other-name"], "不一致") {
		t.Errorf("other-name 的原因 = %q", broken["other-name"])
	}
	if _, ok := broken["bad"]; !ok {
		t.Errorf("bad 应当被记为损坏：%v", broken)
	}
}

func TestRegistrySetEnabledAndUninstall(t *testing.T) {
	t.Parallel()

	r, root := newTestRegistry(t)
	writeInstalled(t, root, "alpha", "")
	if err := r.Load(context.Background()); err != nil {
		t.Fatalf("加载失败: %v", err)
	}

	if err := r.SetEnabled(context.Background(), "alpha", true); err != nil {
		t.Fatalf("启用失败: %v", err)
	}
	if len(r.Enabled()) != 1 {
		t.Error("启用后应当出现在 Enabled 里")
	}

	if err := r.SetEnabled(context.Background(), "nope", true); err == nil {
		t.Error("对不存在的插件启用应当报错")
	}

	if err := r.Uninstall(context.Background(), "alpha"); err != nil {
		t.Fatalf("卸载失败: %v", err)
	}
	if _, ok := r.Get("alpha"); ok {
		t.Error("卸载后不该还在注册表里")
	}
	if _, err := os.Stat(filepath.Join(root, "alpha")); !os.IsNotExist(err) {
		t.Error("卸载后目录应当被删掉")
	}
	if err := r.Uninstall(context.Background(), "alpha"); err == nil {
		t.Error("重复卸载应当报错")
	}
}

// TestRegistryReloadDropsStale 确认重新加载会把上一次的状态清干净。
//
// 不清的话，卸载后再重载会让已经不存在的插件继续出现在设置与菜单里。
func TestRegistryReloadDropsStale(t *testing.T) {
	t.Parallel()

	r, root := newTestRegistry(t)
	writeInstalled(t, root, "alpha", "")
	if err := r.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := r.SetEnabled(context.Background(), "alpha", true); err != nil {
		t.Fatal(err)
	}

	if err := os.RemoveAll(filepath.Join(root, "alpha")); err != nil {
		t.Fatal(err)
	}
	if err := r.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(r.List()) != 0 || len(r.Enabled()) != 0 {
		t.Errorf("重载后仍有残留：%d 个已安装，%d 个启用", len(r.List()), len(r.Enabled()))
	}
}
