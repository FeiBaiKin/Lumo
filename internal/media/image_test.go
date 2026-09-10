package media

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"strings"
	"testing"

	"github.com/gen2brain/webp"
)

func TestFitWithin(t *testing.T) {
	t.Parallel()

	cases := []struct{ w, h, maxEdge, wantW, wantH int }{
		{1000, 500, 320, 320, 160},
		{500, 1000, 320, 160, 320},
		{300, 200, 320, 300, 200}, // 已经够小，原样返回
		{1000, 1, 320, 320, 1},    // 极端长宽比不得缩到 0
		{0, 0, 320, 0, 0},
	}
	for _, c := range cases {
		w, h := fitWithin(c.w, c.h, c.maxEdge)
		if w != c.wantW || h != c.wantH {
			t.Errorf("fitWithin(%d, %d, %d) = (%d, %d)，期望 (%d, %d)",
				c.w, c.h, c.maxEdge, w, h, c.wantW, c.wantH)
		}
	}
}

func TestProcessGeneratesWebPThumbnails(t *testing.T) {
	t.Parallel()

	processor := NewImageProcessor()
	info, variants, err := processor.Process(pngBytes(t, 1000, 500), DefaultThumbnails)
	if err != nil {
		t.Fatalf("处理图片失败: %v", err)
	}
	if info.Width != 1000 || info.Height != 500 {
		t.Errorf("原图尺寸应为 1000×500，实际 %d×%d", info.Width, info.Height)
	}
	// large 档为 1600，长边 1000 不足，应被跳过。
	if len(variants) != 2 {
		t.Fatalf("应生成 2 档缩略图（thumb、medium），实际 %d 档", len(variants))
	}

	want := map[string][2]int{"thumb": {320, 160}, "medium": {768, 384}}
	for _, v := range variants {
		size, ok := want[v.Name]
		if !ok {
			t.Errorf("不应出现档位 %q", v.Name)
			continue
		}
		if v.Width != size[0] || v.Height != size[1] {
			t.Errorf("%s 尺寸应为 %d×%d，实际 %d×%d", v.Name, size[0], size[1], v.Width, v.Height)
		}
		if v.Ext != ".webp" || v.MIME != "image/webp" {
			t.Errorf("%s 应编码为 WebP，实际 %s / %s", v.Name, v.Ext, v.MIME)
		}
		cfg, err := webp.DecodeConfig(bytes.NewReader(v.Data))
		if err != nil {
			t.Errorf("%s 不是合法的 WebP: %v", v.Name, err)
			continue
		}
		if cfg.Width != v.Width || cfg.Height != v.Height {
			t.Errorf("%s 实际编码尺寸为 %d×%d，与记录的 %d×%d 不符",
				v.Name, cfg.Width, cfg.Height, v.Width, v.Height)
		}
	}
}

// TestProcessNeverUpscales 验证小图不会被放大成更大的模糊文件。
func TestProcessNeverUpscales(t *testing.T) {
	t.Parallel()

	info, variants, err := NewImageProcessor().Process(pngBytes(t, 100, 80), DefaultThumbnails)
	if err != nil {
		t.Fatalf("处理图片失败: %v", err)
	}
	if info.Width != 100 || info.Height != 80 {
		t.Errorf("尺寸应为 100×80，实际 %d×%d", info.Width, info.Height)
	}
	if len(variants) != 0 {
		t.Errorf("小图不应生成任何缩略图，实际 %d 档", len(variants))
	}
}

// TestProcessRejectsHugeDeclaredSize 验证解压炸弹在解码前就被挡下。
func TestProcessRejectsHugeDeclaredSize(t *testing.T) {
	t.Parallel()

	// 构造一份**校验和正确**的 PNG，只把 IHDR 里的宽高改成极大值——
	// CRC 必须重算，否则解码器在尺寸检查之前就因校验和失败而报错，这条断言就白写了。
	//
	// PNG 布局：8 字节签名 + 4 字节长度 + 4 字节 "IHDR" + 13 字节数据 + 4 字节 CRC，
	// CRC 覆盖「类型 + 数据」。
	data := pngBytes(t, 4, 4)
	const (
		ihdrTypeOffset  = 8 + 4
		ihdrWidthOffset = ihdrTypeOffset + 4
		ihdrDataLength  = 13
		ihdrCRCOffset   = ihdrWidthOffset + ihdrDataLength
	)
	binary.BigEndian.PutUint32(data[ihdrWidthOffset:], 1<<16)   // 宽 65536
	binary.BigEndian.PutUint32(data[ihdrWidthOffset+4:], 1<<16) // 高 65536
	binary.BigEndian.PutUint32(data[ihdrCRCOffset:],
		crc32.ChecksumIEEE(data[ihdrTypeOffset:ihdrCRCOffset]))

	_, _, err := NewImageProcessor().Process(data, DefaultThumbnails)
	if err == nil {
		t.Fatal("超大尺寸的图片应被拒绝")
	}
	if !strings.Contains(err.Error(), "像素上限") {
		t.Fatalf("应因像素上限被拒，实际 %v", err)
	}
}

func TestProcessRejectsNonImage(t *testing.T) {
	t.Parallel()

	if _, _, err := NewImageProcessor().Process([]byte("这不是图片"), DefaultThumbnails); err == nil {
		t.Fatal("非图片内容应报错")
	}
}

// TestProcessAppliesOrientation 验证带方向标记的 JPEG 会被摆正：
// 一张横向的原图标记为「需顺时针旋转 90 度」后，处理结果应变成竖向。
func TestProcessAppliesOrientation(t *testing.T) {
	t.Parallel()

	data := jpegWithOrientation(t, 400, 200, 6)
	info, _, err := NewImageProcessor().Process(data, DefaultThumbnails)
	if err != nil {
		t.Fatalf("处理图片失败: %v", err)
	}
	if info.Width != 200 || info.Height != 400 {
		t.Errorf("纠正方向后应为 200×400，实际 %d×%d", info.Width, info.Height)
	}
}

func TestJPEGOrientation(t *testing.T) {
	t.Parallel()

	for _, want := range []int{1, 3, 6, 8} {
		if got := jpegOrientation(jpegWithOrientation(t, 8, 8, want)); got != want {
			t.Errorf("方向标记应为 %d，实际 %d", want, got)
		}
	}

	// 没有 EXIF 段、不是 JPEG、被截断的数据都应安全地退回 1。
	plain := jpegBytes(t, 8, 8)
	if got := jpegOrientation(plain); got != orientationNormal {
		t.Errorf("无 EXIF 时应返回 1，实际 %d", got)
	}
	if got := jpegOrientation(pngBytes(t, 8, 8)); got != orientationNormal {
		t.Errorf("非 JPEG 应返回 1，实际 %d", got)
	}
	truncated := jpegWithOrientation(t, 8, 8, 6)[:12]
	if got := jpegOrientation(truncated); got != orientationNormal {
		t.Errorf("截断数据应返回 1，实际 %d", got)
	}
	// 越界的方向值不予采信。
	if got := jpegOrientation(jpegWithOrientation(t, 8, 8, 99)); got != orientationNormal {
		t.Errorf("非法方向值应返回 1，实际 %d", got)
	}
}

// jpegBytes 生成一张纯色 JPEG。
func jpegBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: 0x80, G: 0x40, B: 0x20, A: 0xff})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("生成测试 JPEG 失败: %v", err)
	}
	return buf.Bytes()
}

// jpegWithOrientation 生成带 EXIF 方向标记的 JPEG：在 SOI 之后插入一个 APP1 段。
func jpegWithOrientation(t *testing.T, w, h, orientation int) []byte {
	t.Helper()
	base := jpegBytes(t, w, h)

	// TIFF 头（大端）+ 一个目录项：方向标记，SHORT 类型，值内联。
	tiff := []byte{
		'M', 'M', 0x00, 0x2a, // 字节序 + 魔数 42
		0x00, 0x00, 0x00, 0x08, // IFD0 偏移
		0x00, 0x01, // 目录项数量
		0x01, 0x12, // 标记号 0x0112
		0x00, 0x03, // 类型 SHORT
		0x00, 0x00, 0x00, 0x01, // 数量 1
		byte(orientation >> 8), byte(orientation), 0x00, 0x00, // 值（内联，左对齐）
		0x00, 0x00, 0x00, 0x00, // 下一个 IFD 偏移
	}
	payload := append([]byte("Exif\x00\x00"), tiff...)
	length := len(payload) + 2

	segment := []byte{markerPrefix, markerAPP1, byte(length >> 8), byte(length)}
	segment = append(segment, payload...)

	out := make([]byte, 0, len(base)+len(segment))
	out = append(out, base[:2]...) // SOI
	out = append(out, segment...)
	out = append(out, base[2:]...)
	return out
}
