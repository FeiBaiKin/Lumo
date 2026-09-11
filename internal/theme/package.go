package theme

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// 主题包的解压限额。主题包来自第三方上传，必须防 zip 炸弹。
const (
	// maxPackageFiles 是包内文件数上限。
	maxPackageFiles = 2000
	// maxFileSize 是单个文件解压后的字节上限。
	maxFileSize int64 = 8 << 20
	// maxTotalSize 是整包解压后的字节上限。
	maxTotalSize int64 = 64 << 20
	// maxPathDepth 是包内路径的层级上限。
	maxPathDepth = 8
)

// 解压时的文件与目录权限，与 workdir、media 保持一致。
const (
	themeDirPerm  os.FileMode = 0o750
	themeFilePerm os.FileMode = 0o640
)

// allowedExtensions 是主题包内允许出现的文件扩展名。
//
// 白名单而非黑名单：主题是静态资源加模板，没有理由包含可执行文件、
// 压缩包或系统库。放行 .php / .exe 一类只会给站点开后门。
var allowedExtensions = map[string]bool{
	".html": true, ".htm": true,
	".css": true, ".js": true, ".mjs": true, ".map": true,
	".json": true, ".yaml": true, ".yml": true, ".toml": true,
	".svg": true, ".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
	".webp": true, ".avif": true, ".ico": true, ".bmp": true,
	".woff": true, ".woff2": true, ".ttf": true, ".otf": true, ".eot": true,
	".txt": true, ".md": true, ".webmanifest": true,
}

// AllowedExtensions 返回允许的扩展名，供接口文档引用。
func AllowedExtensions() []string {
	out := make([]string, 0, len(allowedExtensions))
	for ext := range allowedExtensions {
		out = append(out, ext)
	}
	return out
}

// Install 把一个 zip 主题包解压安装到 root 下，返回其元信息。
//
// 安装是「先解到临时目录、校验通过再改名就位」的两段式：
// 半个主题留在 themes/ 下比装不上更糟——站点会在切换时渲染出残缺页面。
func Install(root string, r io.ReaderAt, size int64, overwrite bool) (*Manifest, error) {
	// 自己保证安装目录存在，不依赖调用方先跑过工作目录初始化：
	// 主题模块在测试与嵌入式场景下都可能被单独装配。
	if err := os.MkdirAll(root, themeDirPerm); err != nil {
		return nil, fmt.Errorf("创建主题目录 %s: %w", root, err)
	}

	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("%w：不是合法的 zip 包（%w）", ErrInvalidPackage, err)
	}

	staging, err := os.MkdirTemp(root, ".staging-*")
	if err != nil {
		return nil, fmt.Errorf("创建临时目录: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(staging)
		}
	}()

	prefix, err := detectPrefix(zr)
	if err != nil {
		return nil, err
	}
	if extractErr := extract(zr, staging, prefix); extractErr != nil {
		return nil, extractErr
	}

	manifest, err := validateDir(staging)
	if err != nil {
		return nil, err
	}

	target := filepath.Join(root, manifest.Name)
	if _, statErr := os.Stat(target); statErr == nil {
		if !overwrite {
			return nil, fmt.Errorf("%w：%s", ErrAlreadyExists, manifest.Name)
		}
		// 先挪开旧版本再就位，失败时还能挪回来。
		backup := target + ".old"
		_ = os.RemoveAll(backup)
		if err := os.Rename(target, backup); err != nil {
			return nil, fmt.Errorf("备份旧主题 %s: %w", manifest.Name, err)
		}
		if err := os.Rename(staging, target); err != nil {
			_ = os.Rename(backup, target)
			return nil, fmt.Errorf("安装主题 %s: %w", manifest.Name, err)
		}
		_ = os.RemoveAll(backup)
		committed = true
		return manifest, nil
	}

	if err := os.Rename(staging, target); err != nil {
		return nil, fmt.Errorf("安装主题 %s: %w", manifest.Name, err)
	}
	committed = true
	return manifest, nil
}

// detectPrefix 判断包内是否有统一的顶层目录。
//
// 从 GitHub 下载的 zip 会多套一层 `<repo>-<branch>/`，而手工打的包通常没有。
// 两种都得支持，否则一半用户会在「为什么提示缺少 theme.yaml」上卡住。
func detectPrefix(zr *zip.Reader) (string, error) {
	// 包根直接有 theme.yaml 就不需要剥层。
	for _, f := range zr.File {
		if path.Clean(f.Name) == FileManifest {
			return "", nil
		}
	}
	for _, f := range zr.File {
		clean := path.Clean(f.Name)
		if base := path.Base(clean); base != FileManifest {
			continue
		}
		dir := path.Dir(clean)
		if dir == "." || strings.Contains(dir, "/") {
			// 只接受恰好一层的包装目录，再深就说明包结构不对。
			continue
		}
		return dir + "/", nil
	}
	return "", fmt.Errorf("%w：包内找不到 %s", ErrInvalidPackage, FileManifest)
}

// extract 把 zip 内容解压到 dest，逐项施加安全限额。
func extract(zr *zip.Reader, dest, prefix string) error {
	var (
		fileCount int
		totalSize int64
	)
	for _, f := range zr.File {
		name := f.Name
		if prefix != "" {
			if !strings.HasPrefix(name, prefix) {
				continue
			}
			name = strings.TrimPrefix(name, prefix)
		}
		if name == "" {
			continue
		}

		rel, err := safeRelPath(name)
		if err != nil {
			return err
		}
		if rel == "" {
			continue
		}

		info := f.FileInfo()
		if info.IsDir() {
			if mkErr := os.MkdirAll(filepath.Join(dest, rel), themeDirPerm); mkErr != nil {
				return fmt.Errorf("创建目录 %s: %w", rel, mkErr)
			}
			continue
		}
		// 符号链接可以指到 /etc/passwd，解压时一律拒绝。
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%w：包内 %s 不是普通文件", ErrInvalidPackage, rel)
		}
		if ext := strings.ToLower(path.Ext(rel)); !allowedExtensions[ext] {
			return fmt.Errorf("%w：不允许的文件类型 %s（%s）", ErrInvalidPackage, ext, rel)
		}

		fileCount++
		if fileCount > maxPackageFiles {
			return fmt.Errorf("%w：文件数超过 %d 个", ErrInvalidPackage, maxPackageFiles)
		}
		written, err := extractFile(f, filepath.Join(dest, rel), rel)
		if err != nil {
			return err
		}
		totalSize += written
		if totalSize > maxTotalSize {
			return fmt.Errorf("%w：解压后总大小超过 %d 字节", ErrInvalidPackage, maxTotalSize)
		}
	}
	if fileCount == 0 {
		return fmt.Errorf("%w：包内没有任何文件", ErrInvalidPackage)
	}
	return nil
}

// extractFile 解压单个文件，返回写入字节数。
func extractFile(f *zip.File, target, rel string) (int64, error) {
	if err := os.MkdirAll(filepath.Dir(target), themeDirPerm); err != nil {
		return 0, fmt.Errorf("创建目录 %s: %w", filepath.Dir(rel), err)
	}
	src, err := f.Open()
	if err != nil {
		return 0, fmt.Errorf("读取包内文件 %s: %w", rel, err)
	}
	defer func() { _ = src.Close() }()

	dst, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, themeFilePerm)
	if err != nil {
		return 0, fmt.Errorf("写入文件 %s: %w", rel, err)
	}
	defer func() { _ = dst.Close() }()

	// 限流复制：不信任 zip 头里声明的 UncompressedSize64，
	// 高压缩比的炸弹正是靠谎报这个字段绕过预检的。
	written, err := io.Copy(dst, io.LimitReader(src, maxFileSize+1))
	if err != nil {
		return 0, fmt.Errorf("解压文件 %s: %w", rel, err)
	}
	if written > maxFileSize {
		return 0, fmt.Errorf("%w：%s 解压后超过 %d 字节", ErrInvalidPackage, rel, maxFileSize)
	}
	return written, nil
}

// safeRelPath 校验并规范化包内路径，拒绝穿越与绝对路径。
func safeRelPath(name string) (string, error) {
	// zip 规范里分隔符就是 /，但实际存在用 \ 打的包。
	normalized := strings.ReplaceAll(name, `\`, "/")
	if strings.HasPrefix(normalized, "/") {
		return "", fmt.Errorf("%w：包内路径不得为绝对路径（%s）", ErrInvalidPackage, name)
	}
	if strings.Contains(normalized, "\x00") {
		return "", fmt.Errorf("%w：包内路径含非法字符", ErrInvalidPackage)
	}

	clean := path.Clean(normalized)
	if clean == "." {
		return "", nil
	}
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("%w：包内路径越界（%s）", ErrInvalidPackage, name)
	}
	// 忽略打包工具留下的元数据目录。
	for _, junk := range []string{"__MACOSX/", ".git/", "node_modules/"} {
		if strings.HasPrefix(clean, junk) {
			return "", nil
		}
	}
	if strings.Count(clean, "/") >= maxPathDepth {
		return "", fmt.Errorf("%w：包内路径层级过深（%s）", ErrInvalidPackage, name)
	}
	return filepath.FromSlash(clean), nil
}

// validateDir 校验一个已解压的主题目录，返回其元信息。
func validateDir(dir string) (*Manifest, error) {
	fsys := os.DirFS(dir)
	manifest, err := readManifestFS(fsys)
	if err != nil {
		return nil, err
	}

	templatesFS, err := fs.Sub(fsys, DirTemplates)
	if err != nil {
		return nil, fmt.Errorf("%w：缺少 %s 目录", ErrInvalidPackage, DirTemplates)
	}
	names, err := collectTemplateNames(templatesFS)
	if err != nil {
		return nil, fmt.Errorf("%w：读取 %s 目录失败（%w）", ErrInvalidPackage, DirTemplates, err)
	}
	if tmplErr := validateTemplates(names); tmplErr != nil {
		return nil, tmplErr
	}

	// 模板必须能解析：语法错误在安装时暴露，比切换主题后整站白屏好得多。
	if _, parseErr := parseEngine(manifest.Name, templatesFS, baseFuncs()); parseErr != nil {
		return nil, parseErr
	}

	// 设置声明必须能编译：Schema 写错在安装时报错，而不是等站长打开设置页。
	decl, err := readSettingsFS(fsys)
	if err != nil {
		return nil, err
	}
	if _, err := compileSettings(manifest.Name, decl); err != nil {
		return nil, err
	}
	return manifest, nil
}

// Validate 校验一个已安装的主题目录，供启动自检与后台重新校验使用。
func Validate(dir string) (*Manifest, error) { return validateDir(dir) }

// Remove 删除一个已安装的主题目录。
func Remove(root, name string) error {
	if !namePattern.MatchString(name) {
		return fmt.Errorf("%w：非法主题名 %q", ErrInvalidPackage, name)
	}
	target := filepath.Join(root, name)
	if _, err := os.Stat(target); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ErrNotFound
		}
		return fmt.Errorf("检查主题目录: %w", err)
	}
	if err := os.RemoveAll(target); err != nil {
		return fmt.Errorf("删除主题 %s: %w", name, err)
	}
	return nil
}

// ListInstalled 扫描 root 下已安装的主题目录名。
//
// 只列目录名不做校验：校验要解析全部模板，而列表接口可能被频繁调用。
// 具体主题是否可用由 Registry 在加载时判定并记录。
func ListInstalled(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取主题目录: %w", err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() || !namePattern.MatchString(e.Name()) {
			continue
		}
		names = append(names, e.Name())
	}
	return names, nil
}
