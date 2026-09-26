package plugin

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRunningVersion(t *testing.T) {
	cases := []struct {
		raw  string
		want string
		ok   bool
	}{
		{"0.0.0-dev", "", false},
		{"abc1234", "", false},
		{"", "", false},
		{"0.2.0", "0.2.0", true},
		{"v0.2.0", "0.2.0", true},
		{"v0.2.0-3-gab12cd-dirty", "0.2.0", true},
		{"0.3.0-rc.1", "0.3.0", true},
		{"0.2.1+build.5", "0.2.1", true},
	}
	for _, c := range cases {
		got, ok := runningVersion(c.raw)
		if ok != c.ok || (ok && got.String() != c.want) {
			t.Errorf("runningVersion(%q) = %v, %v；想要 %q, %v", c.raw, got, ok, c.want, c.ok)
		}
	}
}

// Lumo 版本不满足 requires 时装不上；升级被拒时旧版本原样留着。
func TestInstallRejectsIncompatibleLumo(t *testing.T) {
	m := testModule(t)
	m.registry.lumoVersion = "0.2.0"

	err := install(t, m.registry, map[string][]byte{
		FileManifest: manifestNamed("demo", "1.0.0", "  requires: \">=0.3.0\"\n"),
	})
	if !errors.Is(err, ErrIncompatible) || !strings.Contains(err.Error(), ">=0.3.0") || !strings.Contains(err.Error(), "0.2.0") {
		t.Fatalf("该以 ErrIncompatible 拒装并说清要什么、有什么，得到 %v", err)
	}
	if _, ok := m.registry.Get("demo"); ok {
		t.Fatal("拒装的插件不该出现在列表里")
	}

	err = install(t, m.registry, map[string][]byte{
		FileManifest: manifestNamed("demo", "1.0.0", "  requires: \">=0.2.0\"\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	err = install(t, m.registry, map[string][]byte{
		FileManifest: manifestNamed("demo", "1.1.0", "  requires: \"^0.3\"\n"),
	})
	if !errors.Is(err, ErrIncompatible) {
		t.Fatalf("升级到不兼容的版本该被拒，得到 %v", err)
	}
	loaded, _ := m.registry.Get("demo")
	if loaded.Manifest.Spec.Version != "1.0.0" {
		t.Fatalf("升级被拒后该还是 1.0.0，得到 %s", loaded.Manifest.Spec.Version)
	}
	if _, err := Validate(loaded.Dir); err != nil {
		t.Fatalf("旧版本的文件该原样留着：%v", err)
	}
}

// 站点回滚到旧版之后，要新版 Lumo 的插件在启动时停用，也启用不了。
func TestRollbackSuspendsIncompatible(t *testing.T) {
	m := testModule(t)
	ctx := context.Background()
	m.registry.lumoVersion = "0.3.0"
	installEnabled(t, m, manifestNamed("demo", "1.0.0", "  requires: \">=0.3.0\"\n"))

	m.registry.lumoVersion = "0.2.0"
	loaded, _ := m.registry.Get("demo")
	m.registry.startOrSuspend(ctx, loaded)
	if loaded, _ = m.registry.Get("demo"); loaded.Enabled || !strings.Contains(loaded.DisabledReason, ">=0.3.0") {
		t.Fatalf("版本不够时该停用并说要哪一版，得到 enabled=%v %q", loaded.Enabled, loaded.DisabledReason)
	}
	if err := m.registry.SetEnabled(ctx, "demo", true, false); !errors.Is(err, ErrIncompatible) {
		t.Fatalf("版本不够时启用该返回 ErrIncompatible，得到 %v", err)
	}
}

// 源码构建（0.0.0-dev）不比版本：本地开发时声明了 requires 的插件照常能用。
func TestDevBuildSkipsRequires(t *testing.T) {
	m := testModule(t)
	m.registry.lumoVersion = "0.0.0-dev"
	installEnabled(t, m, manifestNamed("demo", "1.0.0", "  requires: \">=9.0.0\"\n"))
	if loaded, _ := m.registry.Get("demo"); !loaded.Enabled {
		t.Fatal("源码构建时不该拦 requires")
	}
}

func TestRequiresSyntaxValidated(t *testing.T) {
	_, err := parseManifest(manifestNamed("demo", "1.0.0", "  requires: \"1.*.3\"\n"), "demo")
	if !errors.Is(err, ErrInvalidPackage) || !strings.Contains(err.Error(), "spec.requires") {
		t.Fatalf("写错的 requires 该在清单校验时挡下，得到 %v", err)
	}
}
