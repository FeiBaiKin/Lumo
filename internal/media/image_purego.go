package media

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"  // 注册 GIF 解码器（动图只取第一帧）
	_ "image/jpeg" // 注册 JPEG 解码器
	_ "image/png"  // 注册 PNG 解码器

	"github.com/gen2brain/webp"
	xdraw "golang.org/x/image/draw"
)

// webpQuality 是缩略图的有损 WebP 质量。
//
// 80 是画质与体积的常用折衷：肉眼几乎看不出损失，体积约为同画质 JPEG 的七成。
const webpQuality = 80

// webpMethod 是编码器的速度/压缩权衡（0 快 6 慢）。4 是库的默认值。
const webpMethod = 4

// maxPixels 是允许解码的像素数上限。
//
// 图片头部声称的尺寸决定解码时分配的内存，一张 32000×32000 的 PNG 头只有几十字节，
// 解码却要吃掉数 GB——这就是解压炸弹。上限约合 8000×8000。
const maxPixels = 64 << 20

// pureGoProcessor 是纯 Go 的图片处理器：x/image/draw 缩放 + libwebp（转译为 Go）编码。
type pureGoProcessor struct{}

// NewImageProcessor 返回默认的图片处理器。
func NewImageProcessor() ImageProcessor { return pureGoProcessor{} }

// Name 实现 ImageProcessor。
func (pureGoProcessor) Name() string { return "purego" }

// Process 实现 ImageProcessor。
func (p pureGoProcessor) Process(data []byte, specs []ThumbnailSpec) (ImageInfo, []Variant, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return ImageInfo{}, nil, fmt.Errorf("读取图片信息: %w", err)
	}
	if int64(cfg.Width)*int64(cfg.Height) > maxPixels {
		return ImageInfo{}, nil, fmt.Errorf("图片尺寸过大：%d×%d 超过 %d 像素上限", cfg.Width, cfg.Height, maxPixels)
	}

	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return ImageInfo{}, nil, fmt.Errorf("解码图片: %w", err)
	}
	img = applyOrientation(img, jpegOrientation(data))

	width, height := bounds(img)
	info := ImageInfo{Width: width, Height: height}

	variants := make([]Variant, 0, len(specs))
	for _, spec := range specs {
		w, h := fitWithin(width, height, spec.MaxEdge)
		if w == width && h == height && max(width, height) <= spec.MaxEdge {
			// 原图本就不超过该档：不生成，避免堆出一份只是换了容器的等大文件。
			continue
		}
		encoded, err := encodeWebP(scale(img, w, h))
		if err != nil {
			return info, nil, fmt.Errorf("生成 %s 缩略图: %w", spec.Name, err)
		}
		variants = append(variants, Variant{
			Name: spec.Name, Width: w, Height: h,
			Ext: ".webp", MIME: mimeWebP, Data: encoded,
		})
	}
	return info, variants, nil
}

// scale 把图片等比缩放到 w×h。
//
// CatmullRom 比双线性慢一些但锐度明显更好；缩略图只生成一次，这点开销值得。
func scale(src image.Image, w, h int) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Src, nil)
	return dst
}

// encodeWebP 把图片编码为有损 WebP。
func encodeWebP(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := webp.Encode(&buf, img, webp.Options{Quality: webpQuality, Method: webpMethod}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// applyOrientation 按 EXIF 方向标记把图片摆正。
//
// 取值含义见 EXIF 规范：偶数值含镜像，5–8 还带 90 度旋转。
func applyOrientation(img image.Image, orientation int) image.Image {
	switch orientation {
	case 2:
		return flipHorizontal(img)
	case 3:
		return rotate180(img)
	case 4:
		return flipVertical(img)
	case 5:
		return rotate90(flipHorizontal(img))
	case 6:
		return rotate90(img)
	case 7:
		return rotate270(flipHorizontal(img))
	case 8:
		return rotate270(img)
	default:
		return img
	}
}

// flipHorizontal 左右镜像。
func flipHorizontal(src image.Image) image.Image {
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := range b.Dy() {
		for x := range b.Dx() {
			dst.Set(b.Dx()-1-x, y, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

// flipVertical 上下镜像。
func flipVertical(src image.Image) image.Image {
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := range b.Dy() {
		for x := range b.Dx() {
			dst.Set(x, b.Dy()-1-y, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

// rotate90 顺时针旋转 90 度。
func rotate90(src image.Image) image.Image {
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dy(), b.Dx()))
	for y := range b.Dy() {
		for x := range b.Dx() {
			dst.Set(b.Dy()-1-y, x, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

// rotate180 旋转 180 度。
func rotate180(src image.Image) image.Image {
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := range b.Dy() {
		for x := range b.Dx() {
			dst.Set(b.Dx()-1-x, b.Dy()-1-y, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

// rotate270 顺时针旋转 270 度。
func rotate270(src image.Image) image.Image {
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dy(), b.Dx()))
	for y := range b.Dy() {
		for x := range b.Dx() {
			dst.Set(y, b.Dx()-1-x, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}
