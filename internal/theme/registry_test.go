package theme

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeThemeDir 在 root 下写一个最小主题，返回其目录。
func writeThemeDir(t *testing.T, root, name, marker string) string {
	t.Helper()

	dir := filepath.Join(root, name)
	files := map[string]string{
		FileManifest: "name: " + name + "\nlabel: " + name + "\nversion: 1.0.0\n",
		"templates/index.html": `{{ template "layouts/base.html" . }}{{ define "main" }}` +
			marker + `{{ end }}`,
		"templates/post.html":          `{{ template "layouts/base.html" . }}{{ define "main" }}P{{ end }}`,
		"templates/page.html":          `{{ template "layouts/base.html" . }}{{ define "main" }}G{{ end }}`,
		"templates/404.html":           `{{ template "layouts/base.html" . }}{{ define "main" }}N{{ end }}`,
		"templates/layouts/base.html":  `<html>{{ block "main" . }}{{ end }}</html>`,
		"templates/partials/head.html": `<meta charset="utf-8">`,
		"static/theme.css":             "body{}",
	}
	for rel, body := range files {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatalf("创建目录失败: %v", err)
		}
		if err := os.WriteFile(path, []byte(body), 0o640); err != nil {
			t.Fatalf("写入 %s 失败: %v", rel, err)
		}
	}
	return dir
}

// newTestRegistry 构造一个以 tempdir 为安装目录、内置主题为回退的注册表，
// 返回注册表与该安装目录。
func newTestRegistry(t *testing.T, devMode bool) (registry *Registry, root string) {
	t.Helper()

	root = t.TempDir()
	var err error
	registry, err = NewRegistry(&RegistryOptions{
		Root:    root,
		Builtin: builtinThemeFS(t),
		DevMode: devMode,
	})
	if err != nil {
		t.Fatalf("构造注册表失败: %v", err)
	}
	return registry, root
}

// renderIndex 渲染首页模板并返回输出。
func renderIndex(t *testing.T, registry *Registry) string {
	t.Helper()

	var sb strings.Builder
	if err := registry.Active().Engine().Render(&sb, "index.html", &Context{}); err != nil {
		t.Fatalf("渲染首页失败: %v", err)
	}
	return sb.String()
}

// TestRegistryLoadsInstalledThemes 验证启动时扫描并加载已安装主题。
func TestRegistryLoadsInstalledThemes(t *testing.T) {
	t.Parallel()

	registry, root := newTestRegistry(t, false)
	writeThemeDir(t, root, "demo", "DEMO")
	writeThemeDir(t, root, "other", "OTHER")

	if err := registry.LoadInstalled(); err != nil {
		t.Fatalf("加载已安装主题失败: %v", err)
	}
	if len(registry.List()) != 3 { // 内置 + 两个
		t.Errorf("主题数 = %d，期望 3", len(registry.List()))
	}
	// 内置主题始终排在最前，便于后台展示。
	if !registry.List()[0].Builtin {
		t.Error("内置主题应排在最前")
	}
}

// TestRegistrySkipsBrokenTheme 验证单个坏主题不影响其余主题加载。
//
// 一个坏主题不该让整站起不来——那会把「上传了一个有问题的主题包」
// 升级成「站点无法访问」。
func TestRegistrySkipsBrokenTheme(t *testing.T) {
	t.Parallel()

	registry, root := newTestRegistry(t, false)
	writeThemeDir(t, root, "good", "GOOD")

	// 写一个缺少必需模板的坏主题。
	bad := filepath.Join(root, "bad")
	if err := os.MkdirAll(filepath.Join(bad, "templates"), 0o750); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bad, FileManifest),
		[]byte("name: bad\nversion: 1.0.0\n"), 0o640); err != nil {
		t.Fatalf("写入元信息失败: %v", err)
	}

	if err := registry.LoadInstalled(); err != nil {
		t.Fatalf("整体加载不应失败: %v", err)
	}
	if _, ok := registry.Get("good"); !ok {
		t.Error("正常主题应加载成功")
	}
	if _, ok := registry.Get("bad"); ok {
		t.Error("坏主题不该出现在可用列表里")
	}
	broken := registry.Broken()
	if _, ok := broken["bad"]; !ok {
		t.Errorf("坏主题应被记录，实际 %v", broken)
	}
}

// TestRegistryRejectsNameMismatch 验证目录名与 theme.yaml 的 name 必须一致。
//
// 不一致会让「启用主题」与「磁盘上的目录」对不上号，排查起来很绕。
func TestRegistryRejectsNameMismatch(t *testing.T) {
	t.Parallel()

	registry, root := newTestRegistry(t, false)
	dir := writeThemeDir(t, root, "dirname", "X")
	// 把声明里的 name 改成另一个值。
	manifest := "name: othername\nversion: 1.0.0\n"
	if err := os.WriteFile(filepath.Join(dir, FileManifest), []byte(manifest), 0o640); err != nil {
		t.Fatalf("改写元信息失败: %v", err)
	}

	if err := registry.LoadInstalled(); err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if _, ok := registry.Get("dirname"); ok {
		t.Error("声明名与目录名不一致时不该加载")
	}
	if reason := registry.Broken()["dirname"]; !strings.Contains(reason, "不一致") {
		t.Errorf("失败原因应说明不一致，实际 %q", reason)
	}
}

// TestRegistryIgnoresBuiltinNameCollision 验证同名目录不会遮蔽内置主题。
//
// 内置主题是所有主题的回退，被遮蔽就等于回退链断了。
func TestRegistryIgnoresBuiltinNameCollision(t *testing.T) {
	t.Parallel()

	registry, root := newTestRegistry(t, false)
	writeThemeDir(t, root, BuiltinName, "SHADOW")

	if err := registry.LoadInstalled(); err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	loaded, ok := registry.Get(BuiltinName)
	if !ok {
		t.Fatal("内置主题应始终可用")
	}
	if !loaded.Builtin {
		t.Error("同名目录不该顶掉内置主题")
	}
}

// TestReloadPicksUpTemplateChanges 验证重载会重新读取磁盘上的模板。
//
// 这是开发模式的核心行为：作者改了模板不必重启进程。
func TestReloadPicksUpTemplateChanges(t *testing.T) {
	t.Parallel()

	registry, root := newTestRegistry(t, true)
	writeThemeDir(t, root, "demo", "BEFORE")
	if err := registry.LoadInstalled(); err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if err := registry.Activate("demo"); err != nil {
		t.Fatalf("启用失败: %v", err)
	}
	if got := renderIndex(t, registry); !strings.Contains(got, "BEFORE") {
		t.Fatalf("初始输出 = %q", got)
	}

	// 改写模板后重载。
	indexPath := filepath.Join(root, "demo", "templates", "index.html")
	updated := `{{ template "layouts/base.html" . }}{{ define "main" }}AFTER{{ end }}`
	if err := os.WriteFile(indexPath, []byte(updated), 0o640); err != nil {
		t.Fatalf("改写模板失败: %v", err)
	}
	if err := registry.Reload("demo"); err != nil {
		t.Fatalf("重载失败: %v", err)
	}
	if got := renderIndex(t, registry); !strings.Contains(got, "AFTER") {
		t.Errorf("重载后输出 = %q，期望含 AFTER", got)
	}
}

// TestReloadKeepsOldEngineOnFailure 验证重载失败时保留旧模板。
//
// 作者把模板改坏的那一刻，站点应该继续用上一版服务，
// 而不是当场白屏——那正是他最需要看文档的时候。
func TestReloadKeepsOldEngineOnFailure(t *testing.T) {
	t.Parallel()

	registry, root := newTestRegistry(t, true)
	writeThemeDir(t, root, "demo", "GOOD")
	if err := registry.LoadInstalled(); err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if err := registry.Activate("demo"); err != nil {
		t.Fatalf("启用失败: %v", err)
	}

	indexPath := filepath.Join(root, "demo", "templates", "index.html")
	if err := os.WriteFile(indexPath, []byte(`{{ if .X }}没有 end`), 0o640); err != nil {
		t.Fatalf("改写模板失败: %v", err)
	}
	if err := registry.Reload("demo"); err == nil {
		t.Fatal("语法错误的模板应导致重载失败")
	}
	if got := renderIndex(t, registry); !strings.Contains(got, "GOOD") {
		t.Errorf("重载失败后应保留旧模板，实际 %q", got)
	}
}

// TestReloadBuiltinIsNoop 验证重载内置主题不报错也不做事。
//
// 内置主题的模板编译在二进制里，重载没有意义，但也不该是个错误。
func TestReloadBuiltinIsNoop(t *testing.T) {
	t.Parallel()

	registry, _ := newTestRegistry(t, false)
	if err := registry.Reload(BuiltinName); err != nil {
		t.Errorf("重载内置主题不应报错: %v", err)
	}
	if err := registry.Reload("nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("重载不存在的主题 = %v，期望 ErrNotFound", err)
	}
}

// TestWatchReloadsOnTemplateChange 验证开发模式的轮询监听确实会触发重载。
func TestWatchReloadsOnTemplateChange(t *testing.T) {
	t.Parallel()

	registry, root := newTestRegistry(t, true)
	writeThemeDir(t, root, "demo", "WATCH-BEFORE")
	if err := registry.LoadInstalled(); err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if err := registry.Activate("demo"); err != nil {
		t.Fatalf("启用失败: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go registry.Watch(ctx)

	// 等首轮基线建立，避免把「首轮只记指纹」误当成重载。
	time.Sleep(watchInterval + 200*time.Millisecond)

	indexPath := filepath.Join(root, "demo", "templates", "index.html")
	updated := `{{ template "layouts/base.html" . }}{{ define "main" }}WATCH-AFTER{{ end }}`
	if err := os.WriteFile(indexPath, []byte(updated), 0o640); err != nil {
		t.Fatalf("改写模板失败: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(renderIndex(t, registry), "WATCH-AFTER") {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Errorf("超时未重载，当前输出 = %q", renderIndex(t, registry))
}

// TestWatchDisabledOutsideDevMode 验证非开发模式下不启动监听。
func TestWatchDisabledOutsideDevMode(t *testing.T) {
	t.Parallel()

	registry, _ := newTestRegistry(t, false)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	done := make(chan struct{})
	go func() {
		registry.Watch(ctx)
		close(done)
	}()

	select {
	case <-done:
		// 非开发模式下应立即返回。
	case <-time.After(time.Second):
		t.Error("非开发模式下 Watch 应立即返回")
	}
}

// TestActivateUnknownTheme 验证启用不存在的主题被拒。
func TestActivateUnknownTheme(t *testing.T) {
	t.Parallel()

	registry, _ := newTestRegistry(t, false)
	if err := registry.Activate("nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("错误 = %v，期望 ErrNotFound", err)
	}
	// 失败后仍应停留在内置主题。
	if registry.ActiveName() != BuiltinName {
		t.Errorf("启用主题 = %q，期望保持内置", registry.ActiveName())
	}
}

// TestUnloadFallsBackToBuiltin 验证卸载当前启用的主题后回退到内置主题。
func TestUnloadFallsBackToBuiltin(t *testing.T) {
	t.Parallel()

	registry, root := newTestRegistry(t, false)
	writeThemeDir(t, root, "demo", "DEMO")
	if err := registry.LoadInstalled(); err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if err := registry.Activate("demo"); err != nil {
		t.Fatalf("启用失败: %v", err)
	}

	registry.Unload("demo")
	if registry.ActiveName() != BuiltinName {
		t.Errorf("卸载后启用主题 = %q，期望回退到内置", registry.ActiveName())
	}
	// 内置主题不可被卸载。
	registry.Unload(BuiltinName)
	if _, ok := registry.Get(BuiltinName); !ok {
		t.Error("内置主题不该被卸载")
	}
}

// TestActiveFallsBackWhenMissing 验证启用项不可用时回退到内置主题。
func TestActiveFallsBackWhenMissing(t *testing.T) {
	t.Parallel()

	registry, _ := newTestRegistry(t, false)
	// 直接篡改内部状态，模拟「启用项被删但状态没更新」。
	registry.mu.Lock()
	registry.active = "已经不存在了"
	registry.mu.Unlock()

	if got := registry.Active(); got == nil || !got.Builtin {
		t.Errorf("启用项缺失时应回退到内置主题，实际 %v", got)
	}
}

// TestTemplateStatusesForInstalledTheme 验证主题列表里的模板提供情况。
func TestTemplateStatusesForInstalledTheme(t *testing.T) {
	t.Parallel()

	registry, root := newTestRegistry(t, false)
	writeThemeDir(t, root, "demo", "DEMO")
	if err := registry.LoadInstalled(); err != nil {
		t.Fatalf("加载失败: %v", err)
	}

	loaded, ok := registry.Get("demo")
	if !ok {
		t.Fatal("主题未加载")
	}
	statuses := TemplateStatuses(loaded.Templates)
	byName := make(map[string]TemplateStatus, len(statuses))
	for _, s := range statuses {
		byName[s.Name] = s
	}
	if !byName["index.html"].Provided {
		t.Error("index.html 应标记为已提供")
	}
	// 这个最小主题没有 category.html，应标记为回退。
	if byName["category.html"].Provided {
		t.Error("category.html 应标记为未提供（走回退）")
	}
}
