package media

import "encoding/binary"

// 方向标记（EXIF tag 0x0112）。1 为正常，2–8 表示镜像与旋转的组合。
const (
	orientationNormal = 1
	orientationMax    = 8
)

// JPEG 标记。
const (
	markerPrefix = 0xFF
	markerSOI    = 0xD8
	markerAPP1   = 0xE1
	markerSOS    = 0xDA
	markerEOI    = 0xD9
)

// tagOrientation 是 TIFF/EXIF 的方向标记号。
const tagOrientation = 0x0112

// jpegOrientation 读取 JPEG 的 EXIF 方向标记；没有或读不出时返回 1（正常）。
//
// 只解析这一个标记，不引入完整 EXIF 库：手机拍摄的竖图几乎都靠它标注方向，
// 而缩略图是按像素直接缩放的，不纠正就会横躺。
func jpegOrientation(data []byte) int {
	if len(data) < 4 || data[0] != markerPrefix || data[1] != markerSOI {
		return orientationNormal
	}

	for offset := 2; offset+4 <= len(data); {
		if data[offset] != markerPrefix {
			return orientationNormal
		}
		marker := data[offset+1]
		// 填充字节：连续的 0xFF 允许出现在标记之前。
		if marker == markerPrefix {
			offset++
			continue
		}
		if marker == markerSOS || marker == markerEOI {
			return orientationNormal
		}
		length := int(binary.BigEndian.Uint16(data[offset+2 : offset+4]))
		if length < 2 || offset+2+length > len(data) {
			return orientationNormal
		}
		if marker == markerAPP1 {
			segment := data[offset+4 : offset+2+length]
			if o, ok := exifOrientation(segment); ok {
				return o
			}
		}
		offset += 2 + length
	}
	return orientationNormal
}

// exifOrientation 从 APP1 段体中读取方向标记。
func exifOrientation(segment []byte) (int, bool) {
	const header = "Exif\x00\x00"
	if len(segment) < len(header)+8 || string(segment[:len(header)]) != header {
		return 0, false
	}
	tiff := segment[len(header):]

	var order binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 0, false
	}
	if order.Uint16(tiff[2:4]) != 42 {
		return 0, false
	}

	ifdOffset := int(order.Uint32(tiff[4:8]))
	if ifdOffset < 8 || ifdOffset+2 > len(tiff) {
		return 0, false
	}
	count := int(order.Uint16(tiff[ifdOffset : ifdOffset+2]))
	// 每个目录项固定 12 字节。
	const entrySize = 12
	if ifdOffset+2+count*entrySize > len(tiff) {
		return 0, false
	}

	for i := range count {
		entry := tiff[ifdOffset+2+i*entrySize:]
		if order.Uint16(entry[:2]) != tagOrientation {
			continue
		}
		// 类型须为 SHORT(3) 且只有一个值，值直接内联在偏移字段的前两字节。
		if order.Uint16(entry[2:4]) != 3 || order.Uint32(entry[4:8]) != 1 {
			return 0, false
		}
		value := int(order.Uint16(entry[8:10]))
		if value < orientationNormal || value > orientationMax {
			return 0, false
		}
		return value, true
	}
	return 0, false
}
