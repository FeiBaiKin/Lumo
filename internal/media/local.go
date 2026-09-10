package media

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// DriverLocal 是本地文件系统驱动名。
const DriverLocal = "local"

// 本地文件与目录权限：目录 0750、文件 0640，与 workdir 的取值一致，
// 上传内容不对同机其他用户开放。
const (
	localDirPerm  os.FileMode = 0o750
	localFilePerm os.FileMode = 0o640
)

// LocalStorage 把对象写到工作目录下的 uploads 子目录，由核心路由以静态文件提供。
type LocalStorage struct {
	root      string
	urlPrefix string
}

// NewLocalStorage 构造本地驱动。root 为存放目录，urlPrefix 为对外访问前缀（如 /uploads）。
func NewLocalStorage(root, urlPrefix string) (*LocalStorage, error) {
	if root == "" {
		return nil, fmt.Errorf("media: 本地存储目录不能为空")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("media: 解析本地存储目录 %s: %w", root, err)
	}
	if err := os.MkdirAll(abs, localDirPerm); err != nil {
		return nil, fmt.Errorf("media: 创建本地存储目录 %s: %w", abs, err)
	}
	return &LocalStorage{root: abs, urlPrefix: strings.TrimSuffix(urlPrefix, "/")}, nil
}

// Driver 实现 Storage。
func (s *LocalStorage) Driver() string { return DriverLocal }

// Put 实现 Storage：原子写入，先写临时文件再改名，避免读到写了一半的文件。
func (s *LocalStorage) Put(_ context.Context, key string, r io.Reader, _ int64, _ string) error {
	path, err := s.resolve(key)
	if err != nil {
		return err
	}
	if mkErr := os.MkdirAll(filepath.Dir(path), localDirPerm); mkErr != nil {
		return fmt.Errorf("创建目录 %s: %w", filepath.Dir(path), mkErr)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".upload-*")
	if err != nil {
		return fmt.Errorf("创建临时文件: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		// 成功路径下临时文件已被改名，这里的删除只对失败路径生效。
		_ = os.Remove(tmpName)
	}()

	if _, err := io.Copy(tmp, r); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("写入文件 %s: %w", key, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("关闭文件 %s: %w", key, err)
	}
	if err := os.Chmod(tmpName, localFilePerm); err != nil {
		return fmt.Errorf("设置文件权限 %s: %w", key, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("提交文件 %s: %w", key, err)
	}
	return nil
}

// Delete 实现 Storage。
func (s *LocalStorage) Delete(_ context.Context, key string) error {
	path, err := s.resolve(key)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("删除文件 %s: %w", key, err)
	}
	return nil
}

// URL 实现 Storage：本地驱动给出站内相对地址，换域名或加 CDN 时无需改库。
func (s *LocalStorage) URL(key string) string {
	return s.urlPrefix + "/" + key
}

// resolve 把对象键解析为绝对路径，并确认结果确实落在 root 之内。
//
// validateKey 已挡掉 ..，这里再用 filepath.Rel 复核一次：符号链接、盘符前缀一类
// 平台相关的花样只有解析成绝对路径后才看得出来。
func (s *LocalStorage) resolve(key string) (string, error) {
	if err := validateKey(key); err != nil {
		return "", err
	}
	path := filepath.Join(s.root, filepath.FromSlash(key))
	rel, err := filepath.Rel(s.root, path)
	if err != nil {
		return "", fmt.Errorf("%w：无法定位 %q", ErrInvalidKey, key)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("%w：越出存储目录 %q", ErrInvalidKey, key)
	}
	return path, nil
}
