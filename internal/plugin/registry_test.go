package plugin

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/FeiBaiKin/lumo/internal/plugin/wasm"
	"github.com/FeiBaiKin/lumo/internal/plugin/wasm/wasmtest"
)

func zipOf(t *testing.T, files map[string][]byte) *bytes.Reader {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(buf.Bytes())
}

// guestSpec 是测试插件要的声明：它登记的钩子、定时任务、接口与前台片段。
const guestSpec = "  runtime: wasm\n  hooks:\n    actions: [comment.created, post.updated]\n" +
	"    filters: [comment.judge, content.render]\n  cron:\n    - {name: tick, every: 1h}\n" +
	"  routes:\n    - {name: echo, method: POST, path: \"/echo/{id}\", public: true}\n" +
	"    - {name: whoami, path: /whoami}\n    - {name: whoami-write, method: POST, path: /whoami}\n" +
	"    - {name: broken, path: /broken, public: true}\n" +
	"  frontend:\n    slots: [content.after, head]\n    widgets:\n      - {name: counter}\n" +
	"    shortcodes:\n      - {name: hello}\n"

func manifest(extra string) []byte {
	return []byte("apiVersion: plugin.lumo.run/v1alpha1\nkind: Plugin\nmetadata:\n  name: demo\nspec:\n  version: 1.0.0\n" + extra)
}

// testModule 装好一个不连库的插件模块：注册表 + 运行时。
func testModule(t *testing.T) *Module {
	t.Helper()
	m := &Module{logger: slog.New(slog.DiscardHandler), crashes: map[string]int{}}
	m.registry = NewRegistry(&RegistryOptions{Root: t.TempDir(), Logger: m.logger})
	engine, err := wasm.NewEngine(context.Background(), wasm.Options{Host: m.host, Logger: m.logger})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		m.registry.Close()
		_ = engine.Close(context.Background())
	})
	m.registry.SetEngine(engine)
	return m
}

func install(t *testing.T, r *Registry, files map[string][]byte) error {
	t.Helper()
	pkg := zipOf(t, files)
	_, err := r.Install(context.Background(), pkg, pkg.Size())
	return err
}

// 声明了能力的插件，站长没确认之前启用不了；确认之后记下授予了什么。
func TestEnableRequiresConsent(t *testing.T) {
	m := testModule(t)
	ctx := context.Background()
	if err := install(t, m.registry, map[string][]byte{FileManifest: manifest("  capabilities:\n    frontend: true\n")}); err != nil {
		t.Fatal(err)
	}
	if err := m.registry.SetEnabled(ctx, "demo", true, false); !errors.Is(err, ErrConsentRequired) {
		t.Fatalf("未确认能力就启用应返回 ErrConsentRequired，得到 %v", err)
	}
	if err := m.registry.SetEnabled(ctx, "demo", true, true); err != nil {
		t.Fatal(err)
	}
	loaded, _ := m.registry.Get("demo")
	if !loaded.Enabled || loaded.Granted == nil || !loaded.Granted.Frontend || loaded.NeedsConsent() {
		t.Fatalf("确认后应启用并记下授予的能力，得到 %+v", loaded)
	}
}

// 升级：能力没变照常运行（后端换成新版本）；多要了能力就先停用、等站长确认。
func TestUpgradeWithMoreCapabilitiesSuspends(t *testing.T) {
	m := testModule(t)
	ctx := context.Background()
	binary := wasmtest.Guest(t)
	caps := "  capabilities:\n    content: {read: true}\n    frontend: true\n    cron: true\n    mail: true\n"
	v1 := map[string][]byte{FileManifest: manifest(guestSpec + caps), FileWasm: binary}
	if err := install(t, m.registry, v1); err != nil {
		t.Fatal(err)
	}
	if err := m.registry.SetEnabled(ctx, "demo", true, true); err != nil {
		t.Fatalf("启用带后端的插件：%v", err)
	}
	if _, ok := m.registry.Backend("demo"); !ok {
		t.Fatal("启用后后端应在运行")
	}

	if err := install(t, m.registry, v1); err != nil {
		t.Fatal(err)
	}
	if loaded, _ := m.registry.Get("demo"); !loaded.Enabled {
		t.Fatalf("能力没变的升级不该停用插件：%q", loaded.DisabledReason)
	}
	if _, ok := m.registry.Backend("demo"); !ok {
		t.Fatal("升级后后端应换成新版本继续运行")
	}

	v2 := map[string][]byte{FileManifest: manifest(guestSpec + caps + "    http: [api.example.com]\n"), FileWasm: binary}
	if err := install(t, m.registry, v2); err != nil {
		t.Fatal(err)
	}
	loaded, _ := m.registry.Get("demo")
	if loaded.Enabled || loaded.DisabledReason != reasonNeedsConsent || !loaded.NeedsConsent() {
		t.Fatalf("多要了能力应先停用并说明原因，得到 enabled=%v reason=%q", loaded.Enabled, loaded.DisabledReason)
	}
	if _, ok := m.registry.Backend("demo"); ok {
		t.Fatal("停用后后端不该还在运行")
	}
}

// 连续崩溃到上限就自动停用，原因写明；处理函数自己返回的错误不累计。
func TestConsecutiveCrashesSuspendPlugin(t *testing.T) {
	m := testModule(t)
	ctx := context.Background()
	caps := "  capabilities:\n    content: {read: true}\n    frontend: true\n    cron: true\n"
	files := map[string][]byte{FileManifest: manifest(guestSpec + caps), FileWasm: wasmtest.Guest(t)}
	if err := install(t, m.registry, files); err != nil {
		t.Fatal(err)
	}
	if err := m.registry.SetEnabled(ctx, "demo", true, true); err != nil {
		t.Fatal(err)
	}
	for range maxConsecutiveCrashes * 2 {
		_, _ = m.Invoke(ctx, "demo", wasm.Request{Type: "action", Name: "post.updated", Payload: map[string]string{"mode": "fail"}}, time.Second)
	}
	if loaded, _ := m.registry.Get("demo"); !loaded.Enabled {
		t.Fatal("处理函数返回的错误不该让插件被停用")
	}
	for range maxConsecutiveCrashes {
		_, _ = m.Invoke(ctx, "demo", wasm.Request{Type: "action", Name: "post.updated", Payload: map[string]string{"mode": "panic"}}, time.Second)
	}
	loaded, _ := m.registry.Get("demo")
	if loaded.Enabled || !strings.Contains(loaded.DisabledReason, "连续") {
		t.Fatalf("连续崩溃应自动停用并说明原因，得到 enabled=%v reason=%q", loaded.Enabled, loaded.DisabledReason)
	}
}

// 后端代码与清单要对得上：声明了 wasm 就得带 plugin.wasm，别处的 .wasm 不收。
func TestPackageBackendMustMatchManifest(t *testing.T) {
	m := testModule(t)
	cases := map[string]map[string][]byte{
		"声明了却没带":  {FileManifest: manifest("  runtime: wasm\n")},
		"带了却没声明":  {FileManifest: manifest(""), FileWasm: []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}},
		"不是 wasm": {FileManifest: manifest("  runtime: wasm\n"), FileWasm: []byte("MZ this is not wasm")},
		"放错位置":    {FileManifest: manifest(""), "lib/extra.wasm": []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}},
	}
	for name, files := range cases {
		if err := install(t, m.registry, files); !errors.Is(err, ErrInvalidPackage) {
			t.Errorf("%s：应被拒绝为非法插件包，得到 %v", name, err)
		}
	}
}

// 重复启用一个已经启用、后端也在跑的插件是个空操作。
//
// 之前这里会再走一遍启用：新后端在旧后端还占着实例名的时候启动，wazero 直接报
// 「已实例化」，接口回 422。界面上的开关不会这么点，但接口可以被重复调用（重试、
// 手写脚本、并发两次点击），所以这道理要挡住。
func TestEnableAlreadyEnabledIsNoop(t *testing.T) {
	m := testModule(t)
	ctx := context.Background()
	caps := "  capabilities:\n    content: {read: true}\n    cron: true\n    frontend: true\n"
	files := map[string][]byte{FileManifest: manifest(guestSpec + caps), FileWasm: wasmtest.Guest(t)}
	if err := install(t, m.registry, files); err != nil {
		t.Fatal(err)
	}
	if err := m.registry.SetEnabled(ctx, "demo", true, true); err != nil {
		t.Fatalf("首次启用：%v", err)
	}
	before, ok := m.registry.Backend("demo")
	if !ok {
		t.Fatal("启用后后端应在运行")
	}
	for i := 0; i < 3; i++ {
		if err := m.registry.SetEnabled(ctx, "demo", true, true); err != nil {
			t.Fatalf("第 %d 次重复启用该是无操作，却报错：%v", i+2, err)
		}
	}
	after, ok := m.registry.Backend("demo")
	if !ok {
		t.Fatal("重复启用之后后端仍应在运行")
	}
	if before != after {
		t.Fatal("重复启用不该换成另一个后端：那说明又起了一个")
	}
	if loaded, _ := m.registry.Get("demo"); !loaded.Enabled {
		t.Fatal("重复启用之后该仍是启用状态")
	}
}
