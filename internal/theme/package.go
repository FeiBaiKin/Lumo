package theme

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/FeiBaiKin/lumo/internal/pkgzip"
)

// 主题包的解压限额。主题包来自第三方上传，必须防 zip 炸弹。
const (
	// maxPackageFiles 是包内文件数上限。
	maxPackageFiles = 2000
	// maxPackageEntries 是包内「文件 + 目录」的总条目数上限。
	//
	// 比文件数上限宽裕一些（正常的主题包目录不多），但它必须存在：
	// 目录条目不占字节也不计入文件数，只有文件上限时，一个几十 KB 的
	// 「几万个空目录」压缩包就能耗尽 inode。
	maxPackageEntries = 2500
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

// pkgOptions 组装本包类型的安全解压参数。
//
// 安全解压的实现共用 internal/pkgzip：路径穿越、zip 炸弹、符号链接这几条约束
// 对主题与插件是同一回事，两份实现意味着每次补洞要补两遍。
// 白名单与限额是策略，各包类型不同，故由各自给出。
func pkgOptions() pkgzip.Options {
	return pkgzip.Options{
		Limits: pkgzip.Limits{
			MaxFiles:     maxPackageFiles,
			MaxEntries:   maxPackageEntries,
			MaxFileSize:  maxFileSize,
			MaxTotalSize: maxTotalSize,
			MaxPathDepth: maxPathDepth,
		},
		AllowedExtensions: allowedExtensions,
		DirPerm:           themeDirPerm,
		FilePerm:          themeFilePerm,
	}
}

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

	prefix, err := pkgzip.DetectPrefix(zr, FileManifest)
	if err != nil {
		return nil, fmt.Errorf("%w：%w", ErrInvalidPackage, err)
	}
	if extractErr := pkgzip.Extract(zr, staging, prefix, pkgOptions()); extractErr != nil {
		return nil, fmt.Errorf("%w：%w", ErrInvalidPackage, extractErr)
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
