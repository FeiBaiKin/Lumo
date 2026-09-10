package media

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

// pngBytes 生成一张纯色 PNG，供类型判定与图片处理测试使用。
func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 0x40, A: 0xff})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("生成测试 PNG 失败: %v", err)
	}
	return buf.Bytes()
}

func TestDetectTypeAcceptsMatchingContent(t *testing.T) {
	t.Parallel()

	data := pngBytes(t, 4, 4)
	ft, err := detectType("照片.PNG", data)
	if err != nil {
		t.Fatalf("应通过校验: %v", err)
	}
	if ft.Ext != ".png" {
		t.Errorf("扩展名应归一为小写 .png，实际 %q", ft.Ext)
	}
	if ft.MIME != "image/png" {
		t.Errorf("MIME 应为 image/png，实际 %q", ft.MIME)
	}
	if ft.Kind != KindImage || !ft.Image {
		t.Errorf("PNG 应判为可处理的图片，实际 kind=%s image=%v", ft.Kind, ft.Image)
	}
}

// TestDetectTypeRejectsUnknownExtension 验证白名单之外的扩展名一律拒绝。
func TestDetectTypeRejectsUnknownExtension(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"payload.exe", "shell.php", "page.html", "无扩展名", "archive.tar.xz"} {
		if _, err := detectType(name, []byte("whatever")); err == nil {
			t.Errorf("%q 应被拒绝", name)
		} else {
			var unsupported *ErrUnsupportedType
			if !errors.As(err, &unsupported) {
				t.Errorf("%q 应返回 ErrUnsupportedType，实际 %v", name, err)
			}
		}
	}
}

// TestDetectTypeRejectsDisguisedHTML 是本模块最关键的一条安全断言：
// 把 HTML 改名成图片上传，会在同源上得到一个可执行文档。
func TestDetectTypeRejectsDisguisedHTML(t *testing.T) {
	t.Parallel()

	html := []byte("<!DOCTYPE HTML><html><script>alert(1)</script></html>")
	for _, name := range []string{"evil.png", "evil.jpg", "evil.svg", "evil.txt", "evil.pdf"} {
		_, err := detectType(name, html)
		if err == nil {
			t.Errorf("%q 内容为 HTML，应被拒绝", name)
			continue
		}
		var mismatch *ErrContentMismatch
		if !errors.As(err, &mismatch) {
			t.Errorf("%q 应返回 ErrContentMismatch，实际 %v", name, err)
		}
	}
}

// TestDetectTypeRejectsWrongImageFormat 验证扩展名与真实图片格式不符时被拒。
func TestDetectTypeRejectsWrongImageFormat(t *testing.T) {
	t.Parallel()

	if _, err := detectType("其实是png.jpg", pngBytes(t, 4, 4)); err == nil {
		t.Fatal("PNG 内容配 .jpg 扩展名应被拒绝")
	}
}

// TestDetectTypeAllowsSVGAsPlainFile 验证 SVG 通过校验但不参与图片处理。
func TestDetectTypeAllowsSVGAsPlainFile(t *testing.T) {
	t.Parallel()

	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"></svg>`)
	ft, err := detectType("icon.svg", svg)
	if err != nil {
		t.Fatalf("SVG 应通过校验: %v", err)
	}
	if ft.Image {
		t.Error("SVG 不应进入图片处理流程")
	}
	if ft.Kind != KindImage {
		t.Errorf("SVG 分类应为 image，实际 %s", ft.Kind)
	}
}

// TestDetectTypeAllowsGenericBinary 验证 Go 嗅探不出的格式按 octet-stream 放行。
func TestDetectTypeAllowsGenericBinary(t *testing.T) {
	t.Parallel()

	// 一段没有已知魔数的二进制。
	blob := []byte{0x01, 0x02, 0x03, 0x04, 0xfe, 0xff, 0x00, 0x10}
	ft, err := detectType("report.docx", blob)
	if err != nil {
		t.Fatalf("docx 应通过校验: %v", err)
	}
	if ft.Kind != KindDocument {
		t.Errorf("docx 分类应为 document，实际 %s", ft.Kind)
	}
}

func TestAllowedExtensionsSorted(t *testing.T) {
	t.Parallel()

	exts := AllowedExtensions()
	if len(exts) != len(allowed) {
		t.Fatalf("应返回全部 %d 个扩展名，实际 %d", len(allowed), len(exts))
	}
	for i := 1; i < len(exts); i++ {
		if exts[i-1] >= exts[i] {
			t.Fatalf("应按字典序排列：%q 在 %q 之前", exts[i-1], exts[i])
		}
	}
	if !strings.HasPrefix(exts[0], ".") {
		t.Errorf("扩展名应带点，实际 %q", exts[0])
	}
}

func TestNormalizeMIMEStripsParameters(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"text/plain; charset=utf-8": "text/plain",
		"IMAGE/PNG":                 "image/png",
		"application/pdf":           "application/pdf",
		"":                          "",
	}
	for input, want := range cases {
		if got := normalizeMIME(input); got != want {
			t.Errorf("normalizeMIME(%q) = %q，期望 %q", input, got, want)
		}
	}
}
