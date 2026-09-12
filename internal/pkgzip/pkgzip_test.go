package pkgzip

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testOptions 是一份宽松的参数，让各用例只调它关心的那一项。
func testOptions() Options {
	return Options{
		Limits: Limits{
			MaxFiles:     100,
			MaxFileSize:  1 << 20,
			MaxTotalSize: 4 << 20,
			MaxPathDepth: 8,
		},
		AllowedExtensions: map[string]bool{
			".html": true, ".css": true, ".yaml": true, ".png": true,
		},
		DirPerm:  0o750,
		FilePerm: 0o640,
	}
}

// entry 是构造测试用压缩包的一条记录。
type entry struct {
	name string
	body string
	// symlink 为真时把这条记录做成符号链接，target 是它指向的位置。
	symlink string
}

// buildZip 在内存里打一个包。
func buildZip(t *testing.T, entries ...entry) *zip.Reader {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		header := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		if e.symlink != "" {
			header.SetMode(os.ModeSymlink | 0o777)
			w, err := zw.CreateHeader(header)
			if err != nil {
				t.Fatalf("写入 %s: %v", e.name, err)
			}
			if _, err := w.Write([]byte(e.symlink)); err != nil {
				t.Fatalf("写入 %s: %v", e.name, err)
			}
			continue
		}
		header.SetMode(0o644)
		w, err := zw.CreateHeader(header)
		if err != nil {
			t.Fatalf("写入 %s: %v", e.name, err)
		}
		if _, err := w.Write([]byte(e.body)); err != nil {
			t.Fatalf("写入 %s: %v", e.name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("关闭压缩包: %v", err)
	}
	reader, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("读取压缩包: %v", err)
	}
	return reader
}

func TestSafeRelPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in      string
		wantErr bool
		// skip 为真表示这条路径应当被静默忽略（返回空串），不是报错。
		skip bool
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
		// 反斜杠打的包得当成路径分隔符处理，否则 ..\evil 会被当成普通文件名写出目录外
		{in: `a\b\c.css`},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			got, err := safeRelPath(tt.in, 8)
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

// TestExtractRejectsEscape 覆盖解压时的越界与非法内容。
//
// 这些用例是本包存在的全部理由：包来自第三方上传，
// 每一条对应一种真实的攻击或事故。
func TestExtractRejectsEscape(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		entries []entry
		want    string
	}{
		{
			name:    "路径穿越",
			entries: []entry{{name: "../evil.html", body: "x"}},
			want:    "越界",
		},
		{
			name:    "绝对路径",
			entries: []entry{{name: "/etc/passwd", body: "x"}},
			want:    "绝对路径",
		},
		{
			name:    "符号链接",
			entries: []entry{{name: "link.html", symlink: "/etc/passwd"}},
			want:    "不是普通文件",
		},
		{
			name:    "不允许的文件类型",
			entries: []entry{{name: "run.sh", body: "rm -rf /"}},
			want:    "不允许的文件类型",
		},
		{
			name:    "空包",
			entries: nil,
			want:    "没有任何文件",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dest := t.TempDir()
			err := Extract(buildZip(t, tc.entries...), dest, "", testOptions())
			if err == nil {
				t.Fatal("期望报错，实际解压成功")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("错误信息里没有 %q：%v", tc.want, err)
			}
		})
	}
}

// TestExtractEnforcesLimits 确认限额真的生效。
//
// 单文件超限那条尤其要紧：高压缩比的 zip 炸弹正是靠谎报 zip 头里的
// UncompressedSize64 绕过预检的，故实现里用的是限流复制而不是信任声明值。
func TestExtractEnforcesLimits(t *testing.T) {
	t.Parallel()

	t.Run("单个文件超过上限", func(t *testing.T) {
		t.Parallel()
		opts := testOptions()
		opts.Limits.MaxFileSize = 16
		big := strings.Repeat("a", 1024)
		err := Extract(buildZip(t, entry{name: "a.html", body: big}), t.TempDir(), "", opts)
		if err == nil || !strings.Contains(err.Error(), "超过") {
			t.Errorf("期望因单文件超限报错，实际 %v", err)
		}
	})

	t.Run("文件数超过上限", func(t *testing.T) {
		t.Parallel()
		opts := testOptions()
		opts.Limits.MaxFiles = 2
		err := Extract(buildZip(t,
			entry{name: "a.html", body: "x"},
			entry{name: "b.html", body: "x"},
			entry{name: "c.html", body: "x"},
		), t.TempDir(), "", opts)
		if err == nil || !strings.Contains(err.Error(), "文件数超过") {
			t.Errorf("期望因文件数超限报错，实际 %v", err)
		}
	})

	t.Run("总大小超过上限", func(t *testing.T) {
		t.Parallel()
		opts := testOptions()
		opts.Limits.MaxTotalSize = 4
		err := Extract(buildZip(t,
			entry{name: "a.html", body: "aaaa"},
			entry{name: "b.html", body: "bbbb"},
		), t.TempDir(), "", opts)
		if err == nil || !strings.Contains(err.Error(), "总大小超过") {
			t.Errorf("期望因总大小超限报错，实际 %v", err)
		}
	})

	t.Run("层级过深", func(t *testing.T) {
		t.Parallel()
		opts := testOptions()
		opts.Limits.MaxPathDepth = 2
		err := Extract(buildZip(t, entry{name: "a/b/c/d.html", body: "x"}), t.TempDir(), "", opts)
		if err == nil || !strings.Contains(err.Error(), "层级过深") {
			t.Errorf("期望因层级过深报错，实际 %v", err)
		}
	})
}

func TestExtractWritesAllowedFiles(t *testing.T) {
	t.Parallel()

	dest := t.TempDir()
	zr := buildZip(t,
		entry{name: "theme.yaml", body: "name: demo"},
		entry{name: "templates/index.html", body: "<h1>hi</h1>"},
		entry{name: "__MACOSX/x.html", body: "junk"},
	)
	opts := testOptions()
	opts.AllowedExtensions[".yaml"] = true
	if err := Extract(zr, dest, "", opts); err != nil {
		t.Fatalf("解压失败: %v", err)
	}

	body, err := os.ReadFile(filepath.Join(dest, "templates", "index.html"))
	if err != nil {
		t.Fatalf("读回文件失败: %v", err)
	}
	if string(body) != "<h1>hi</h1>" {
		t.Errorf("内容 = %q", body)
	}
	// 打包工具的元数据目录应当被静默忽略，而不是解出一堆垃圾
	if _, err := os.Stat(filepath.Join(dest, "__MACOSX")); !os.IsNotExist(err) {
		t.Error("__MACOSX 目录不该被解出来")
	}
}

// TestExtractStripsPrefix 覆盖从 GitHub 下载的包多套一层目录的情况。
func TestExtractStripsPrefix(t *testing.T) {
	t.Parallel()

	// 清单文件固定在包根，故用 plugin.yaml 探测——它同时也是解压后要校验的文件。
	zr := buildZip(t,
		entry{name: "demo-main/plugin.yaml", body: "name: demo"},
		entry{name: "demo-main/templates/index.html", body: "x"},
	)
	prefix, err := DetectPrefix(zr, "plugin.yaml")
	if err != nil {
		t.Fatalf("探测前缀失败: %v", err)
	}
	if prefix != "demo-main/" {
		t.Fatalf("前缀 = %q，期望 demo-main/", prefix)
	}

	dest := t.TempDir()
	if err := Extract(zr, dest, prefix, testOptions()); err != nil {
		t.Fatalf("解压失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "plugin.yaml")); err != nil {
		t.Errorf("前缀没有被剥掉: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "templates", "index.html")); err != nil {
		t.Errorf("前缀没有被剥掉: %v", err)
	}
}

func TestDetectPrefix(t *testing.T) {
	t.Parallel()

	t.Run("包根直接有清单", func(t *testing.T) {
		t.Parallel()
		prefix, err := DetectPrefix(buildZip(t,
			entry{name: "plugin.yaml", body: "x"},
			entry{name: "logo.png", body: "x"},
		), "plugin.yaml")
		if err != nil || prefix != "" {
			t.Errorf("prefix = %q, err = %v", prefix, err)
		}
	})

	t.Run("找不到清单", func(t *testing.T) {
		t.Parallel()
		if _, err := DetectPrefix(buildZip(t, entry{name: "a.html", body: "x"}), "plugin.yaml"); err == nil {
			t.Error("期望报错")
		}
	})

	t.Run("包装层超过一层时不认", func(t *testing.T) {
		t.Parallel()
		// 两层包装说明包结构不对，宁可报「找不到清单」也不要猜
		if _, err := DetectPrefix(buildZip(t,
			entry{name: "a/b/plugin.yaml", body: "x"},
		), "plugin.yaml"); err == nil {
			t.Error("期望报错")
		}
	})
}
