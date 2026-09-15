package plugin

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/FeiBaiKin/lumo/internal/pkgzip"
)

// 插件包的解压限额。插件包来自第三方上传，必须防 zip 炸弹。
//
// 与主题同一套数值：两者都是「静态资产 + 声明文件」，没有理由给出不同的宽严。
const (
	// maxPackageFiles 是包内文件数上限。
	maxPackageFiles = 2000
	// maxPackageEntries 是包内「文件 + 目录」的总条目数上限，理由同主题包：
	// 目录条目不占字节也不计入文件数，只有文件上限时挡不住空目录洪水。
	maxPackageEntries = 2500
	// maxFileSize 是单个文件解压后的字节上限。
	maxFileSize int64 = 8 << 20
	// maxTotalSize 是整包解压后的字节上限。
	maxTotalSize int64 = 64 << 20
	// maxPathDepth 是包内路径的层级上限。
	maxPathDepth = 8
)

// 解压时的文件与目录权限，与 workdir、theme 保持一致。
const (
	pluginDirPerm  os.FileMode = 0o750
	pluginFilePerm os.FileMode = 0o640
)

// allowedExtensions 是插件包内允许出现的文件扩展名。
//
// 白名单而非黑名单：v1 的插件是纯声明式的，包内只该有清单、设置与展示图。
// 到了能执行后端代码的那一期（期 4），.wasm 与插件的迁移 .sql
// 才会加进来——现在放行它们等于收下一堆永远不会被执行的文件，
// 而作者会以为自己写的逻辑生效了。
var allowedExtensions = map[string]bool{
	".yaml": true, ".yml": true, ".json": true,
	".html": true, ".css": true, ".js": true,
	".svg": true, ".png": true, ".jpg": true, ".jpeg": true,
	".gif": true, ".webp": true, ".ico": true,
	".woff": true, ".woff2": true, ".ttf": true, ".otf": true,
	".txt": true, ".md": true,
}

// AllowedExtensions 返回允许的扩展名，供接口文档引用。
func AllowedExtensions() []string { return pkgzip.AllowedExtensionList(allowedExtensions) }

// pkgOptions 组装插件包的安全解压参数。
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
		DirPerm:           pluginDirPerm,
		FilePerm:          pluginFilePerm,
	}
}

// Install 把一个 zip 插件包解压安装到 root 下，返回其元信息。
//
// 与主题同样的两段式：先解到临时目录、校验通过再改名就位。
// 半个插件留在 plugins/ 下比装不上更糟——它会被当成一个已安装的插件加载，
// 而缺少的清单或设置声明要到启用时才暴露。
func Install(root string, r io.ReaderAt, size int64, overwrite bool) (*Manifest, error) {
	if err := os.MkdirAll(root, pluginDirPerm); err != nil {
		return nil, fmt.Errorf("创建插件目录 %s: %w", root, err)
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

	// 用不带目录名校验的那一版：此刻目录还叫 .staging-xxx，
	// 而目录名一致性是「已安装目录」的属性（见 Validate），不属于包内容。
	manifest, err := validateContents(staging)
	if err != nil {
		return nil, err
	}

	target := filepath.Join(root, manifest.Metadata.Name)
	if _, statErr := os.Stat(target); statErr == nil {
		if !overwrite {
			return nil, fmt.Errorf("%w：%s", ErrAlreadyExists, manifest.Metadata.Name)
		}
		// 先挪开旧版本再就位，失败时还能挪回来。
		backup := target + ".old"
		_ = os.RemoveAll(backup)
		if err := os.Rename(target, backup); err != nil {
			return nil, fmt.Errorf("备份旧插件 %s: %w", manifest.Metadata.Name, err)
		}
		if err := os.Rename(staging, target); err != nil {
			_ = os.Rename(backup, target)
			return nil, fmt.Errorf("安装插件 %s: %w", manifest.Metadata.Name, err)
		}
		_ = os.RemoveAll(backup)
		committed = true
		return manifest, nil
	}

	if err := os.Rename(staging, target); err != nil {
		return nil, fmt.Errorf("安装插件 %s: %w", manifest.Metadata.Name, err)
	}
	committed = true
	return manifest, nil
}

// validateContents 校验一个已解压目录的内容，不检查它叫什么名字。
//
// 安装期用这一版：那时目录还在 .staging-xxx 名下，名字尚未确定。
func validateContents(dir string) (*Manifest, error) {
	fsys := os.DirFS(dir)
	manifest, err := readManifest(fsys, "")
	if err != nil {
		return nil, err
	}
	if _, err := loadSettings(fsys); err != nil {
		return nil, err
	}
	return manifest, nil
}

// Validate 校验一个**已安装**的插件目录，返回其元信息。
//
// 比 validateContents 多一条：目录名必须等于插件标识。
// 启停与卸载都按名字定位目录，两者不一致时那些操作会作用到别的东西上——
// 而这类错只有在这里能拦下，因为它是文件系统与清单之间的关系，不是包自身的性质。
func Validate(dir string) (*Manifest, error) {
	manifest, err := validateContents(dir)
	if err != nil {
		return nil, err
	}
	if base := filepath.Base(dir); manifest.Metadata.Name != base {
		return nil, fmt.Errorf("%w：metadata.name %q 与目录名 %q 不一致",
			ErrInvalidPackage, manifest.Metadata.Name, base)
	}
	return manifest, nil
}

// Remove 删除一个已安装的插件目录。
func Remove(root, name string) error {
	if !namePattern.MatchString(name) {
		return fmt.Errorf("%w：非法的插件名 %q", ErrInvalidPackage, name)
	}
	target := filepath.Join(root, name)
	// 即便名字合法，也要确认解析后的路径仍在 root 之下：
	// 符号链接或大小写不敏感的文件系统都可能让 Join 的结果跑到外面去。
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(absTarget, absRoot+string(os.PathSeparator)) {
		return fmt.Errorf("%w：插件目录越界", ErrInvalidPackage)
	}
	if err := os.RemoveAll(absTarget); err != nil {
		return fmt.Errorf("删除插件 %s: %w", name, err)
	}
	return nil
}

// ListInstalled 列出 root 下已安装的插件目录名。
//
// 目录不存在时返回空列表而不是报错：首次启动、或用不到插件的部署里，
// 这个目录压根不会被创建。
func ListInstalled(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取插件目录 %s: %w", root, err)
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		// 跳过安装中途留下的临时目录与备份。
		if !entry.IsDir() || strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".old") {
			continue
		}
		out = append(out, name)
	}
	return out, nil
}
