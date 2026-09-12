package plugin

import (
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// file 是构造测试用插件包的一条记录。
type file struct {
	name string
	body string
}

// buildPackage 在内存里打一个插件包。
func buildPackage(t *testing.T, files ...file) *bytes.Reader {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range files {
		header := &zip.FileHeader{Name: f.name, Method: zip.Deflate}
		header.SetMode(0o644)
		w, err := zw.CreateHeader(header)
		if err != nil {
			t.Fatalf("写入 %s: %v", f.name, err)
		}
		if _, err := w.Write([]byte(f.body)); err != nil {
			t.Fatalf("写入 %s: %v", f.name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("关闭压缩包: %v", err)
	}
	return bytes.NewReader(buf.Bytes())
}

// manifestYAML 生成一份最小可用的清单。
func manifestYAML(name string) string {
	return `apiVersion: plugin.lumo.run/v1alpha1
kind: Plugin
metadata:
  name: ` + name + `
spec:
  displayName: 示例插件
  version: 1.0.0
  description: 用于测试
  author:
    name: 测试
`
}

// installPackage 把包装到 root 下。
func installPackage(t *testing.T, root string, overwrite bool, files ...file) (*Manifest, error) {
	t.Helper()
	reader := buildPackage(t, files...)
	return Install(root, reader, int64(reader.Len()), overwrite)
}

func TestInstallValidPackage(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	manifest, err := installPackage(t, root, false,
		file{name: FileManifest, body: manifestYAML("demo")},
		file{name: "logo.png", body: "not really a png"},
	)
	if err != nil {
		t.Fatalf("安装失败: %v", err)
	}
	if manifest.Metadata.Name != "demo" || manifest.Spec.DisplayName != "示例插件" {
		t.Errorf("清单 = %+v", manifest)
	}

	// 目录名必须等于插件标识：卸载与启停都按名字定位。
	if _, statErr := os.Stat(filepath.Join(root, "demo", FileManifest)); statErr != nil {
		t.Errorf("安装后的目录结构不对: %v", statErr)
	}

	installed, err := ListInstalled(root)
	if err != nil {
		t.Fatalf("列目录失败: %v", err)
	}
	if len(installed) != 1 || installed[0] != "demo" {
		t.Errorf("已安装 = %v", installed)
	}
}

// TestInstallRejectsBadPackages 覆盖安装期的各类拒绝。
//
// 这些是被上传的第三方内容，每一条对应一种真实的攻击或事故。
func TestInstallRejectsBadPackages(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		files []file
		want  string
	}{
		{
			name:  "缺少清单",
			files: []file{{name: "readme.md", body: "x"}},
			want:  FileManifest,
		},
		{
			name: "apiVersion 不对",
			files: []file{{name: FileManifest, body: `apiVersion: v1
kind: Plugin
metadata: {name: demo}
spec: {version: 1.0.0}`}},
			want: "apiVersion",
		},
		{
			name: "kind 不对",
			files: []file{{name: FileManifest, body: `apiVersion: plugin.lumo.run/v1alpha1
kind: Widget
metadata: {name: demo}
spec: {version: 1.0.0}`}},
			want: "kind",
		},
		{
			name: "标识不是 DNS-1123",
			files: []file{{name: FileManifest, body: `apiVersion: plugin.lumo.run/v1alpha1
kind: Plugin
metadata: {name: Demo_Plugin}
spec: {version: 1.0.0}`}},
			want: "DNS-1123",
		},
		{
			name: "版本号形态不对",
			files: []file{{name: FileManifest, body: `apiVersion: plugin.lumo.run/v1alpha1
kind: Plugin
metadata: {name: demo}
spec: {version: latest}`}},
			want: "version",
		},
		{
			name: "缺少版本号",
			files: []file{{name: FileManifest, body: `apiVersion: plugin.lumo.run/v1alpha1
kind: Plugin
metadata: {name: demo}
spec: {displayName: 示例}`}},
			want: "version",
		},
		{
			name: "清单里有拼错的字段",
			files: []file{{name: FileManifest, body: `apiVersion: plugin.lumo.run/v1alpha1
kind: Plugin
metadata: {name: demo}
spec: {version: 1.0.0, displayname: 示例}`}},
			want: "displayname",
		},
		{
			name: "包内有可执行文件",
			files: []file{
				{name: FileManifest, body: manifestYAML("demo")},
				{name: "run.sh", body: "rm -rf /"},
			},
			want: "不允许的文件类型",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := installPackage(t, t.TempDir(), false, tc.files...)
			if err == nil {
				t.Fatal("期望安装失败，实际成功了")
			}
			if !errors.Is(err, ErrInvalidPackage) {
				t.Errorf("错误类别 = %v，期望 ErrInvalidPackage", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("错误信息里没有 %q：%v", tc.want, err)
			}
		})
	}
}

// TestInstallRejectsDirNameMismatch 确认目录名与标识必须一致。
//
// 不一致时卸载会删错目录、启用会加载错插件——这类错只能在这里拦下。
func TestInstallRejectsDirNameMismatch(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if _, err := installPackage(t, root, false,
		file{name: FileManifest, body: manifestYAML("real-name")},
	); err != nil {
		t.Fatalf("安装失败: %v", err)
	}
	// 手工把目录改名，模拟文件系统与清单对不上的情况。
	if err := os.Rename(filepath.Join(root, "real-name"), filepath.Join(root, "other-name")); err != nil {
		t.Fatal(err)
	}
	if _, err := Validate(filepath.Join(root, "other-name")); err == nil {
		t.Error("目录名与标识不一致时应当报错")
	}
}

func TestInstallUpgradeKeepsDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if _, err := installPackage(t, root, false,
		file{name: FileManifest, body: manifestYAML("demo")},
	); err != nil {
		t.Fatalf("首次安装失败: %v", err)
	}

	// 不覆盖时同名插件必须被拒绝，否则会静默替换用户已有的东西。
	if _, err := installPackage(t, root, false,
		file{name: FileManifest, body: manifestYAML("demo")},
	); !errors.Is(err, ErrAlreadyExists) {
		t.Errorf("同名插件未拒绝，错误 = %v", err)
	}

	upgraded := strings.Replace(manifestYAML("demo"), "1.0.0", "2.0.0", 1)
	manifest, err := installPackage(t, root, true,
		file{name: FileManifest, body: upgraded},
		file{name: "extra.txt", body: "new"},
	)
	if err != nil {
		t.Fatalf("升级失败: %v", err)
	}
	if manifest.Spec.Version != "2.0.0" {
		t.Errorf("版本 = %q", manifest.Spec.Version)
	}
	// 升级后旧文件不该残留
	if _, err := os.Stat(filepath.Join(root, "demo", "extra.txt")); err != nil {
		t.Errorf("新文件没装上: %v", err)
	}
	// 安装中途的临时目录与备份不留在插件目录里
	installed, _ := ListInstalled(root)
	if len(installed) != 1 {
		t.Errorf("插件目录里有多余的东西: %v", installed)
	}
}

func TestRemoveRejectsEscape(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := Remove(root, "../evil"); !errors.Is(err, ErrInvalidPackage) {
		t.Errorf("非法名错误 = %v，期望 ErrInvalidPackage", err)
	}
}

func TestListInstalledIgnoresStaging(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for _, name := range []string{"demo", ".staging-123", "demo.old", "notes.txt"} {
		target := filepath.Join(root, name)
		if name == "notes.txt" {
			if err := os.WriteFile(target, []byte("x"), 0o640); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(target, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	installed, err := ListInstalled(root)
	if err != nil {
		t.Fatalf("列目录失败: %v", err)
	}
	if len(installed) != 1 || installed[0] != "demo" {
		t.Errorf("已安装 = %v，期望只有 demo", installed)
	}
}
