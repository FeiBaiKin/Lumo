package media

import (
	"fmt"
	"mime"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
)

// sniffLen 是 http.DetectContentType 需要的最大前导字节数。
const sniffLen = 512

// rule 是一种允许上传的文件类型。
//
// 扩展名与内容必须彼此印证：扩展名决定落库的 MIME 与对外的 Content-Type，
// detected 列出 http.DetectContentType 对该类文件的**合法**返回值。
// 两者对不上就拒绝——把 .html 改名成 .png 上传，是同源站点上最典型的存储型 XSS 入口。
type rule struct {
	mime string
	kind Kind
	// image 为真时用图片处理器解码并生成缩略图。SVG 不在其列：它是可执行文档，只当普通文件存放。
	image bool
	// detected 是允许的嗅探结果（已去掉参数）。
	detected []string
}

// 通用嗅探结果：Go 的嗅探表认不出的二进制格式都会落到这里。
const (
	mimeOctetStream = "application/octet-stream"
	mimeZip         = "application/zip"
	mimeTextPlain   = "text/plain"
)

// 表内重复出现的媒体类型。
const (
	mimeJPEG = "image/jpeg"
	mimeWebP = "image/webp"
	mimeMP4  = "video/mp4"
)

// allowed 是扩展名白名单。名单之外的一律拒绝——白名单而非黑名单，
// 新格式需要显式加入，不会因为漏掉某个危险后缀而被绕过。
var allowed = map[string]rule{
	// ---- 图片 ----
	".jpg":  {mime: mimeJPEG, kind: KindImage, image: true, detected: []string{mimeJPEG}},
	".jpeg": {mime: mimeJPEG, kind: KindImage, image: true, detected: []string{mimeJPEG}},
	".png":  {mime: "image/png", kind: KindImage, image: true, detected: []string{"image/png"}},
	".gif":  {mime: "image/gif", kind: KindImage, image: true, detected: []string{"image/gif"}},
	".webp": {mime: mimeWebP, kind: KindImage, image: true, detected: []string{mimeWebP}},
	".bmp":  {mime: "image/bmp", kind: KindImage, image: false, detected: []string{"image/bmp"}},
	".ico":  {mime: "image/x-icon", kind: KindImage, image: false, detected: []string{"image/x-icon"}},
	// SVG 是 XML 文档，嗅探结果随首行是否为 XML 声明而变；一律不解码、不生成缩略图。
	".svg": {mime: "image/svg+xml", kind: KindImage, image: false,
		detected: []string{"image/svg+xml", "text/xml", mimeTextPlain}},

	// ---- 视频 ----
	".mp4":  {mime: mimeMP4, kind: KindVideo, detected: []string{mimeMP4, mimeOctetStream}},
	".webm": {mime: "video/webm", kind: KindVideo, detected: []string{"video/webm", mimeOctetStream}},
	".mov":  {mime: "video/quicktime", kind: KindVideo, detected: []string{"video/quicktime", mimeMP4, mimeOctetStream}},

	// ---- 音频 ----
	".mp3":  {mime: "audio/mpeg", kind: KindAudio, detected: []string{"audio/mpeg", mimeOctetStream}},
	".m4a":  {mime: "audio/mp4", kind: KindAudio, detected: []string{"audio/mp4", mimeMP4, mimeOctetStream}},
	".ogg":  {mime: "audio/ogg", kind: KindAudio, detected: []string{"audio/ogg", "application/ogg", mimeOctetStream}},
	".wav":  {mime: "audio/wav", kind: KindAudio, detected: []string{"audio/wave", "audio/wav", "audio/x-wav", mimeOctetStream}},
	".flac": {mime: "audio/flac", kind: KindAudio, detected: []string{"audio/flac", mimeOctetStream}},

	// ---- 文档 ----
	".pdf": {mime: "application/pdf", kind: KindDocument, detected: []string{"application/pdf"}},
	".txt": {mime: "text/plain", kind: KindDocument, detected: []string{mimeTextPlain, mimeOctetStream}},
	".md":  {mime: "text/markdown", kind: KindDocument, detected: []string{mimeTextPlain, mimeOctetStream}},
	".csv": {mime: "text/csv", kind: KindDocument, detected: []string{mimeTextPlain, mimeOctetStream}},
	".doc": {mime: "application/msword", kind: KindDocument, detected: []string{mimeOctetStream}},
	".xls": {mime: "application/vnd.ms-excel", kind: KindDocument, detected: []string{mimeOctetStream}},
	".ppt": {mime: "application/vnd.ms-powerpoint", kind: KindDocument, detected: []string{mimeOctetStream}},
	// OOXML 实为 zip 容器，Go 只能嗅到 zip。
	".docx": {mime: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		kind: KindDocument, detected: []string{mimeZip, mimeOctetStream}},
	".xlsx": {mime: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		kind: KindDocument, detected: []string{mimeZip, mimeOctetStream}},
	".pptx": {mime: "application/vnd.openxmlformats-officedocument.presentationml.presentation",
		kind: KindDocument, detected: []string{mimeZip, mimeOctetStream}},

	// ---- 归档 ----
	".zip": {mime: "application/zip", kind: KindOther, detected: []string{mimeZip, mimeOctetStream}},
}

// ErrUnsupportedType 表示扩展名不在白名单内。
type ErrUnsupportedType struct{ Ext string }

// Error 实现 error。
func (e *ErrUnsupportedType) Error() string {
	if e.Ext == "" {
		return "文件缺少扩展名，无法确定类型"
	}
	return "不支持的文件类型：" + e.Ext
}

// ErrContentMismatch 表示文件内容与扩展名声明的类型不符。
type ErrContentMismatch struct {
	Ext      string
	Declared string
	Detected string
}

// Error 实现 error。
func (e *ErrContentMismatch) Error() string {
	return fmt.Sprintf("文件内容与扩展名不符：%s 应为 %s，实际嗅探为 %s", e.Ext, e.Declared, e.Detected)
}

// fileType 是通过校验的文件类型判定结果。
type fileType struct {
	// Ext 是规范化后的扩展名（小写，含点）。
	Ext string
	// MIME 是落库并对外声明的类型，由扩展名决定而非由客户端声明。
	MIME string
	Kind Kind
	// Image 为真时应生成缩略图。
	Image bool
}

// detectType 由原始文件名与文件前导字节判定类型。
//
// 客户端声明的 Content-Type 一概不参考：它完全由上传方控制，没有任何证明力。
func detectType(originalName string, head []byte) (fileType, error) {
	ext := strings.ToLower(filepath.Ext(originalName))
	r, ok := allowed[ext]
	if !ok {
		return fileType{}, &ErrUnsupportedType{Ext: ext}
	}

	detected := normalizeMIME(http.DetectContentType(head))
	for _, candidate := range r.detected {
		if detected == candidate {
			return fileType{Ext: ext, MIME: r.mime, Kind: r.kind, Image: r.image}, nil
		}
	}
	return fileType{}, &ErrContentMismatch{Ext: ext, Declared: r.mime, Detected: detected}
}

// normalizeMIME 去掉媒体类型的参数并转小写，如 "text/plain; charset=utf-8" → "text/plain"。
func normalizeMIME(value string) string {
	parsed, _, err := mime.ParseMediaType(value)
	if err != nil {
		return strings.ToLower(strings.TrimSpace(value))
	}
	return parsed
}

// AllowedExtensions 返回全部允许的扩展名，按字典序，供接口文档与前端提示使用。
func AllowedExtensions() []string {
	exts := make([]string, 0, len(allowed))
	for ext := range allowed {
		exts = append(exts, ext)
	}
	sort.Strings(exts)
	return exts
}
