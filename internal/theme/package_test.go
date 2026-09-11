package theme

import (
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// zipFile 是构造测试用 zip 包的一个条目。
type zipFile struct {
	name string
	body string
}

// buildZip 打一个内存中的 zip 包，返回内容与其字节数。
func buildZip(t *testing.T, files []zipFile) (data []byte, size int64) {
	t.Helper()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range files {
		w, err := zw.Create(f.name)
		if err != nil {
			t.Fatalf("创建 zip 条目 %s 失败: %v", f.name, err)
		}
		if _, err := w.Write([]byte(f.body)); err != nil {
			t.Fatalf("写入 zip 条目 %s 失败: %v", f.name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("关闭 zip 失败: %v", err)
	}
	return buf.Bytes(), int64(buf.Len())
}

// validPackage 返回一个合法主题包的文件列表，prefix 非空时整体套一层目录。
func validPackage(prefix string) []zipFile {
	files := []zipFile{
		{"theme.yaml", "name: demo\nlabel: 演示\nversion: 1.0.0\n"},
		{"templates/index.html", `{{ template "layouts/base.html" . }}{{ define "main" }}I{{ end }}`},
		{"templates/post.html", `{{ template "layouts/base.html" . }}{{ define "main" }}P{{ end }}`},
		{"templates/page.html", `{{ template "layouts/base.html" . }}{{ define "main" }}G{{ end }}`},
		{"templates/404.html", `{{ template "layouts/base.html" . }}{{ define "main" }}N{{ end }}`},
		{"templates/layouts/base.html", `<html>{{ block "main" . }}{{ end }}</html>`},
		{"static/theme.css", "body{}"},
	}
	if prefix == "" {
		return files
	}
	out := make([]zipFile, 0, len(files))
	for _, f := range files {
		out = append(out, zipFile{prefix + f.name, f.body})
	}
	return out
}

// install 是 Install 的测试辅助。
func install(t *testing.T, root string, files []zipFile, overwrite bool) (*Manifest, error) {
	t.Helper()
	data, size := buildZip(t, files)
	return Install(root, bytes.NewReader(data), size, overwrite)
}

// TestInstallValidPackage 验证合法主题包能装上，且内容落到正确位置。
func TestInstallValidPackage(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	manifest, err := install(t, root, validPackage(""), false)
	if err != nil {
		t.Fatalf("安装失败: %v", err)
	}
	if manifest.Name != "demo" || manifest.Label != "演示" || manifest.Version != "1.0.0" {
		t.Errorf("元信息 = %+v", manifest)
	}
	for _, rel := range []string{"theme.yaml", "templates/index.html", "static/theme.css"} {
		if _, err := os.Stat(filepath.Join(root, "demo", filepath.FromSlash(rel))); err != nil {
			t.Errorf("缺少文件 %s: %v", rel, err)
		}
	}
	// 临时目录必须清理干净，不能留下 .staging-*。
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".staging-") {
			t.Errorf("残留临时目录 %s", e.Name())
		}
	}
}

// TestInstallStripsWrapperDir 验证从 GitHub 下载的「多套一层目录」的包也能装。
func TestInstallStripsWrapperDir(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if _, err := install(t, root, validPackage("lumo-theme-demo-main/"), false); err != nil {
		t.Fatalf("带包装目录的包安装失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "demo", "theme.yaml")); err != nil {
		t.Errorf("包装目录未被剥离: %v", err)
	}
}

// TestInstallRejectsPathTraversal 验证路径穿越被拒，且不在目标目录外留下文件。
//
// 这是主题上传最危险的一条：zip 里的 ../../ 能把文件写到工作目录之外。
func TestInstallRejectsPathTraversal(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outside := filepath.Join(root, "..", "pwned.txt")

	files := append(validPackage(""), zipFile{"../pwned.txt", "owned"})
	_, err := install(t, root, files, false)
	if !errors.Is(err, ErrInvalidPackage) {
		t.Fatalf("错误 = %v，期望 ErrInvalidPackage", err)
	}
	if _, statErr := os.Stat(outside); statErr == nil {
		t.Fatal("穿越路径的文件被写到了目标目录之外")
	}
	// 失败的安装不该留下半个主题。
	if _, statErr := os.Stat(filepath.Join(root, "demo")); statErr == nil {
		t.Error("安装失败后不该留下主题目录")
	}
}

// TestInstallRejectsAbsolutePath 验证绝对路径被拒。
func TestInstallRejectsAbsolutePath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	files := append(validPackage(""), zipFile{"/etc/evil.txt", "x"})
	if _, err := install(t, root, files, false); !errors.Is(err, ErrInvalidPackage) {
		t.Fatalf("错误 = %v，期望 ErrInvalidPackage", err)
	}
}

// TestInstallRejectsDisallowedExtension 验证扩展名白名单。
//
// 主题只该有模板与静态资源，没有理由包含可执行文件或脚本。
func TestInstallRejectsDisallowedExtension(t *testing.T) {
	t.Parallel()

	for _, bad := range []string{"shell.php", "run.exe", "lib.so", "script.sh"} {
		t.Run(bad, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			files := append(validPackage(""), zipFile{"static/" + bad, "x"})
			_, err := install(t, root, files, false)
			if !errors.Is(err, ErrInvalidPackage) {
				t.Fatalf("错误 = %v，期望 ErrInvalidPackage", err)
			}
			if !strings.Contains(err.Error(), filepath.Ext(bad)) {
				t.Errorf("错误应指出扩展名，实际 %v", err)
			}
		})
	}
}

// TestInstallIgnoresJunkDirs 验证打包工具留下的元数据目录被忽略而非报错。
func TestInstallIgnoresJunkDirs(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	files := append(validPackage(""),
		zipFile{"__MACOSX/._theme.yaml", "junk"},
		zipFile{".git/config", "junk"},
	)
	if _, err := install(t, root, files, false); err != nil {
		t.Fatalf("元数据目录不应导致失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "demo", "__MACOSX")); err == nil {
		t.Error("__MACOSX 不该被解压")
	}
}

// TestInstallRequiresManifest 验证缺少 theme.yaml 时报错。
func TestInstallRequiresManifest(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	files := validPackage("")[1:] // 去掉 theme.yaml
	_, err := install(t, root, files, false)
	if !errors.Is(err, ErrInvalidPackage) {
		t.Fatalf("错误 = %v，期望 ErrInvalidPackage", err)
	}
	if !strings.Contains(err.Error(), FileManifest) {
		t.Errorf("错误应指出缺少 %s，实际 %v", FileManifest, err)
	}
}

// TestInstallRequiresFourTemplates 验证缺少必需模板时报错。
func TestInstallRequiresFourTemplates(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	var files []zipFile
	for _, f := range validPackage("") {
		if f.name == "templates/404.html" {
			continue
		}
		files = append(files, f)
	}
	_, err := install(t, root, files, false)
	if !errors.Is(err, ErrInvalidPackage) {
		t.Fatalf("错误 = %v，期望 ErrInvalidPackage", err)
	}
	if !strings.Contains(err.Error(), "404.html") {
		t.Errorf("错误应指出缺少 404.html，实际 %v", err)
	}
}

// TestInstallRejectsBrokenTemplate 验证模板语法错误在安装时就被拒。
func TestInstallRejectsBrokenTemplate(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	var files []zipFile
	for _, f := range validPackage("") {
		if f.name == "templates/index.html" {
			f.body = `{{ if .X }}没有 end`
		}
		files = append(files, f)
	}
	if _, err := install(t, root, files, false); !errors.Is(err, ErrInvalidPackage) {
		t.Fatalf("错误 = %v，期望 ErrInvalidPackage", err)
	}
	if _, err := os.Stat(filepath.Join(root, "demo")); err == nil {
		t.Error("模板有错时不该留下主题目录")
	}
}

// TestInstallOverwrite 验证同名主题的覆盖策略。
func TestInstallOverwrite(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if _, err := install(t, root, validPackage(""), false); err != nil {
		t.Fatalf("首次安装失败: %v", err)
	}

	// 默认不覆盖。
	if _, err := install(t, root, validPackage(""), false); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("错误 = %v，期望 ErrAlreadyExists", err)
	}

	// 显式覆盖时用新版本替换。
	var v2 []zipFile
	for _, f := range validPackage("") {
		if f.name == "theme.yaml" {
			f.body = "name: demo\nlabel: 演示\nversion: 2.0.0\n"
		}
		v2 = append(v2, f)
	}
	manifest, err := install(t, root, v2, true)
	if err != nil {
		t.Fatalf("覆盖安装失败: %v", err)
	}
	if manifest.Version != "2.0.0" {
		t.Errorf("版本 = %q，期望 2.0.0", manifest.Version)
	}
	// 备份目录必须清理。
	if _, err := os.Stat(filepath.Join(root, "demo.old")); err == nil {
		t.Error("覆盖后不该留下 demo.old")
	}
}

// TestInstallRejectsNonZip 验证非 zip 数据被拒。
func TestInstallRejectsNonZip(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	data := []byte("这不是一个 zip 包")
	_, err := Install(root, bytes.NewReader(data), int64(len(data)), false)
	if !errors.Is(err, ErrInvalidPackage) {
		t.Fatalf("错误 = %v，期望 ErrInvalidPackage", err)
	}
}

// TestRemoveAndList 验证卸载与列举。
func TestRemoveAndList(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if _, err := install(t, root, validPackage(""), false); err != nil {
		t.Fatalf("安装失败: %v", err)
	}

	names, err := ListInstalled(root)
	if err != nil {
		t.Fatalf("列举失败: %v", err)
	}
	if len(names) != 1 || names[0] != "demo" {
		t.Fatalf("列表 = %v，期望 [demo]", names)
	}

	if err := Remove(root, "demo"); err != nil {
		t.Fatalf("卸载失败: %v", err)
	}
	if err := Remove(root, "demo"); !errors.Is(err, ErrNotFound) {
		t.Errorf("重复卸载错误 = %v，期望 ErrNotFound", err)
	}
	// 非法主题名不得触碰文件系统。
	if err := Remove(root, "../evil"); !errors.Is(err, ErrInvalidPackage) {
		t.Errorf("非法名错误 = %v，期望 ErrInvalidPackage", err)
	}
}

// TestSafeRelPath 逐条验证路径规范化。
func TestSafeRelPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in      string
		wantErr bool
		skip    bool
	}{
		{in: "templates/index.html"},
		{in: "a/b/c.css"},
		{in: "../evil", wantErr: true},
		{in: "a/../../evil", wantErr: true},
		{in: "/abs/path", wantErr: true},
		{in: `..\evil`, wantErr: true},
		{in: "__MACOSX/x", skip: true},
		{in: "node_modules/x", skip: true},
		{in: "a/b/c/d/e/f/g/h/i.css", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			got, err := safeRelPath(tt.in)
			switch {
			case tt.wantErr && err == nil:
				t.Errorf("期望报错，实际得到 %q", got)
			case !tt.wantErr && err != nil:
				t.Errorf("不应报错，实际 %v", err)
			case tt.skip && got != "":
				t.Errorf("期望被忽略，实际 %q", got)
			}
		})
	}
}
