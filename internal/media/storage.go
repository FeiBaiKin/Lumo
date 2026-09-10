package media

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
)

// Storage 是附件的对象存储抽象。
//
// 实现必须把 key 当作**不可信输入**再校验一次：调用方虽然只传自己生成的键，
// 但删除走的是库里的历史记录，多一道校验才能保证路径穿越在任何路径上都不成立。
type Storage interface {
	// Driver 返回驱动名，随附件记录入库。
	Driver() string
	// Put 写入对象；size 为负表示长度未知。同名对象存在时覆盖。
	Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
	// Delete 删除对象；对象不存在不算错误。
	Delete(ctx context.Context, key string) error
	// URL 返回对象的公开访问地址。
	URL(key string) string
}

// ErrInvalidKey 表示对象键不合法。
var ErrInvalidKey = errors.New("对象键不合法")

// maxKeyLength 是对象键的长度上限，与库中的 CHECK 约束一致。
const maxKeyLength = 255

// keyPattern 限定对象键的形态：只允许字母、数字与 . _ - /，且必须以字母或数字开头。
var keyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)

// validateKey 校验对象键。
//
// 拒绝 ..、前后斜杠、连续斜杠与反斜杠：本地驱动会把键拼进文件路径，
// 任何一条漏网都可能写到 data/uploads 之外去。
func validateKey(key string) error {
	switch {
	case key == "":
		return fmt.Errorf("%w：不能为空", ErrInvalidKey)
	case len(key) > maxKeyLength:
		return fmt.Errorf("%w：超过 %d 字节", ErrInvalidKey, maxKeyLength)
	case !keyPattern.MatchString(key):
		return fmt.Errorf("%w：含非法字符 %q", ErrInvalidKey, key)
	case strings.Contains(key, ".."):
		return fmt.Errorf("%w：不允许出现上级目录记号", ErrInvalidKey)
	case strings.HasSuffix(key, "/"), strings.Contains(key, "//"):
		return fmt.Errorf("%w：路径片段不能为空", ErrInvalidKey)
	}
	for _, segment := range strings.Split(key, "/") {
		if segment == "." {
			return fmt.Errorf("%w：路径片段不能是单个点", ErrInvalidKey)
		}
	}
	return nil
}

// randomNameBytes 是随机文件名的熵，16 字节即 32 位十六进制。
const randomNameBytes = 16

// newObjectName 生成随机文件名。
//
// 不沿用上传时的原始名：原始名可能含中文、空格、路径分隔符甚至同名覆盖既有文件，
// 而随机名让对象键与用户输入完全解耦（原始名另存一列，仅作展示）。
func newObjectName(ext string) (string, error) {
	buf := make([]byte, randomNameBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成随机文件名: %w", err)
	}
	return hex.EncodeToString(buf) + ext, nil
}

// newObjectKey 生成对象键：YYYY/MM/<随机名>。
//
// 按年月分目录是为了本地驱动：单目录堆几十万文件后，很多文件系统的目录遍历会明显变慢。
func newObjectKey(now time.Time, ext string) (key, name string, err error) {
	name, err = newObjectName(ext)
	if err != nil {
		return "", "", err
	}
	key = fmt.Sprintf("%04d/%02d/%s", now.Year(), int(now.Month()), name)
	if err := validateKey(key); err != nil {
		return "", "", err
	}
	return key, name, nil
}

// variantKey 由原图键派生缩略图键，如 2026/09/ab..cd.jpg → 2026/09/ab..cd-thumb.webp。
func variantKey(originalKey, variant, ext string) string {
	base := originalKey
	if idx := strings.LastIndex(base, "."); idx > strings.LastIndex(base, "/") {
		base = base[:idx]
	}
	return base + "-" + variant + ext
}
