package pkgzip

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"hash/crc32"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var testOptions = Options{
	Limits: Limits{
		MaxFiles:     10,
		MaxFileSize:  1 << 10,
		MaxTotalSize: 4 << 10,
		MaxPathDepth: 8,
	},
	AllowedExtensions: map[string]bool{".css": true, ".html": true, ".yaml": true},
	DirPerm:           0o755,
	FilePerm:          0o644,
}

type entry struct {
	name string
	body string
	mode os.FileMode
}

func buildZip(t *testing.T, entries ...entry) *zip.Reader {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		hdr := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		if e.mode != 0 {
			hdr.SetMode(e.mode)
		}
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(e.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	return zr
}

// extractInto 解压到临时目录下的 pkg 子目录，返回 pkg 的上一级，便于检查有没有写到外面去。
func extractInto(t *testing.T, zr *zip.Reader) (root string, err error) {
	t.Helper()
	root = t.TempDir()
	return root, Extract(zr, filepath.Join(root, "pkg"), "", testOptions)
}

func TestExtractValidPackage(t *testing.T) {
	root, err := extractInto(t, buildZip(t,
		entry{name: "theme.yaml", body: "name: x"},
		entry{name: "templates/index.html", body: "<p>hi</p>"},
	))
	if err != nil {
		t.Fatalf("合法的包被拒绝：%v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "pkg", "templates", "index.html"))
	if err != nil || string(data) != "<p>hi</p>" {
		t.Fatalf("解压结果不对：%q, %v", data, err)
	}
}

func TestExtractRejectsTraversal(t *testing.T) {
	for _, name := range []string{
		"../evil.css",
		"templates/../../evil.css",
		"/abs/evil.css",
		`templates\..\..\evil.css`,
		"C:/evil.css",
	} {
		t.Run(name, func(t *testing.T) {
			root, err := extractInto(t, buildZip(t, entry{name: name, body: "x"}))
			if err == nil {
				t.Fatalf("路径 %q 没有被拒绝", name)
			}
			if _, statErr := os.Stat(filepath.Join(root, "evil.css")); statErr == nil {
				t.Fatalf("路径 %q 写到了目标目录之外", name)
			}
		})
	}
}

func TestExtractRejectsSymlink(t *testing.T) {
	_, err := extractInto(t, buildZip(t, entry{name: "link.css", body: "/etc/passwd", mode: os.ModeSymlink | 0o777}))
	if err == nil || !strings.Contains(err.Error(), "不是普通文件") {
		t.Fatalf("符号链接应被拒绝，得到 %v", err)
	}
}

func TestExtractRejectsExtension(t *testing.T) {
	for _, name := range []string{"payload.exe", "plugin.wasm", "shell.sh", "noext"} {
		if _, err := extractInto(t, buildZip(t, entry{name: name, body: "x"})); err == nil {
			t.Errorf("%s 不在白名单里，应被拒绝", name)
		}
	}
}

func TestExtractRejectsTooManyFiles(t *testing.T) {
	var entries []entry
	for i := range testOptions.Limits.MaxFiles + 1 {
		entries = append(entries, entry{name: "f" + string(rune('a'+i)) + ".css", body: "x"})
	}
	if _, err := extractInto(t, buildZip(t, entries...)); err == nil {
		t.Fatal("文件数超限应被拒绝")
	}
}

// 高压缩比炸弹靠谎报 UncompressedSize64 绕过预检：限额必须按实际解出的字节数算。
func TestExtractIgnoresLyingSizeHeader(t *testing.T) {
	payload := bytes.Repeat([]byte{'a'}, 64<<10)
	var compressed bytes.Buffer
	fw, err := flate.NewWriter(&compressed, flate.BestCompression)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fw.Write(payload)
	_ = fw.Close()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.CreateRaw(&zip.FileHeader{
		Name:               "bomb.css",
		Method:             zip.Deflate,
		CRC32:              crc32.ChecksumIEEE(payload),
		CompressedSize64:   uint64(compressed.Len()),
		UncompressedSize64: 10, // 谎报：声称只有 10 字节
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write(compressed.Bytes())
	_ = zw.Close()
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}

	root, err := extractInto(t, zr)
	if err == nil {
		t.Fatal("实际解压超限的文件应被拒绝")
	}
	if info, statErr := os.Stat(filepath.Join(root, "pkg", "bomb.css")); statErr == nil && info.Size() > testOptions.Limits.MaxFileSize {
		t.Fatalf("写出了 %d 字节，超过单文件上限", info.Size())
	}
}
