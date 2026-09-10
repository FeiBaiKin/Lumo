package media

import "image"

// ThumbnailSpec 是一档缩略图的规格：按最长边等比缩放。
type ThumbnailSpec struct {
	// Name 是档位名，写入缩略图记录并出现在文件名里。
	Name string
	// MaxEdge 是缩放后最长边的像素上限。
	MaxEdge int
}

// DefaultThumbnails 是默认的缩略图档位。
//
// 三档覆盖列表缩略图、正文内联图与全宽大图；原图长边不足某档时跳过该档，
// 绝不放大——放大只会得到更大的模糊文件。
var DefaultThumbnails = []ThumbnailSpec{
	{Name: "thumb", MaxEdge: 320},
	{Name: "medium", MaxEdge: 768},
	{Name: "large", MaxEdge: 1600},
}

// ImageInfo 是原图的基本信息。
type ImageInfo struct {
	Width  int
	Height int
}

// Variant 是一档已编码的缩略图。
type Variant struct {
	Name   string
	Width  int
	Height int
	// Ext 是编码格式对应的扩展名，如 .webp。
	Ext string
	// MIME 是编码格式的媒体类型。
	MIME string
	Data []byte
}

// ImageProcessor 从原始图片字节生成缩略图。
//
// 抽成接口是为了给 libvips 留位置：默认实现是纯 Go 的（CGO_ENABLED=0 可编译，
// 单二进制不依赖系统库），大站可以换成 vips 实现以换取速度与更好的编码质量。
// 接入方式：新增 image_vips.go 并加 //go:build vips，同时给 image_purego.go 加 //go:build !vips。
type ImageProcessor interface {
	// Name 返回实现名，写入启动日志便于排查。
	Name() string
	// Process 解码图片（含 EXIF 方向纠正）并按 specs 生成缩略图。
	//
	// 返回的尺寸是**纠正方向之后**的，与缩略图和前台布局一致。
	Process(data []byte, specs []ThumbnailSpec) (ImageInfo, []Variant, error)
}

// fitWithin 返回把 w×h 等比缩放到最长边不超过 maxEdge 后的尺寸；已经足够小时原样返回。
func fitWithin(w, h, maxEdge int) (width, height int) {
	longest := max(w, h)
	if longest <= maxEdge || longest == 0 {
		return w, h
	}
	scale := float64(maxEdge) / float64(longest)
	nw := int(float64(w)*scale + 0.5)
	nh := int(float64(h)*scale + 0.5)
	return max(nw, 1), max(nh, 1)
}

// bounds 返回图片的宽高。
func bounds(img image.Image) (width, height int) {
	b := img.Bounds()
	return b.Dx(), b.Dy()
}
