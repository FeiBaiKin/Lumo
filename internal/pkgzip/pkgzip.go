// Package pkgzip 安全地把 zip 包解压到磁盘，供主题与插件共用一个实现。
//
// 为什么不各写一份：这里的每一条约束都对应一种真实的攻击或事故——
// 路径穿越、zip 炸弹、符号链接指向 /etc/passwd、把 .wasm 塞进主题包。
// 两份实现意味着每次补一个洞要补两遍，而漏掉的那一份不会有任何症状，
// 直到有人拿它做事。
//
// 本包只负责「安全解压」这一件事。包长什么样、清单怎么解析、
// 什么时候提交到最终目录，都由调用方决定——那些是策略，不同包类型本就不同。
//
// 本包**不定义错误哨兵**：错误的措辞随包类型而变（「主题包不合法」与「插件包不合法」
// 该说清楚是哪一个），由调用方用它自己的哨兵包装这里返回的错误。
package pkgzip

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Limits 是解压限额。包来自第三方上传，必须防 zip 炸弹。
type Limits struct {
	// MaxFiles 是包内文件数上限。
	MaxFiles int
	// MaxEntries 是「文件 + 落盘目录」的总条目数上限。
	//
	// 单看文件数不够：目录条目不占字节、也不计入文件数，一个只含几万个空目录的
	// 几十 KB 压缩包能绕过 MaxFiles/MaxTotalSize，耗尽 inode 与磁盘元数据。
	// 非正数时退回 MaxFiles，保持旧调用方的行为不变。
	MaxEntries int
	// MaxFileSize 是单个文件解压后的字节上限。
	MaxFileSize int64
	// MaxTotalSize 是整包解压后的字节上限。
	MaxTotalSize int64
	// MaxPathDepth 是包内路径的层级上限。
	MaxPathDepth int
}

// Options 是一次解压的参数。
type Options struct {
	Limits Limits
	// AllowedExtensions 是允许出现的文件扩展名（小写，含点）。
	//
	// 白名单而非黑名单：两类包都只该包含声明式资产，没有理由出现可执行文件。
	// 放行一个不该放行的后缀，等于给站点留下一个可上传的后门。
	AllowedExtensions map[string]bool
	// DirPerm 与 FilePerm 是新建目录与文件的权限。
	DirPerm  os.FileMode
	FilePerm os.FileMode
}

// AllowedExtensionList 把扩展名白名单摊成有序列表，供接口文档展示。
func AllowedExtensionList(allowed map[string]bool) []string {
	out := make([]string, 0, len(allowed))
	for ext := range allowed {
		out = append(out, ext)
	}
	return out
}

// DetectPrefix 判断包内是否有统一的顶层目录，返回需要剥掉的前缀。
//
// 从 GitHub 下载的 zip 会多套一层 `<repo>-<branch>/`，而手工打的包通常没有。
// 两种都得支持，否则一半作者会卡在「为什么提示缺少清单文件」上。
func DetectPrefix(zr *zip.Reader, manifestName string) (string, error) {
	// 包根直接有清单文件就不需要剥层。
	for _, f := range zr.File {
		if path.Clean(f.Name) == manifestName {
			return "", nil
		}
	}
	for _, f := range zr.File {
		clean := path.Clean(f.Name)
		if path.Base(clean) != manifestName {
			continue
		}
		dir := path.Dir(clean)
		if dir == "." || strings.Contains(dir, "/") {
			// 只接受恰好一层的包装目录，再深就说明包结构不对。
			continue
		}
		return dir + "/", nil
	}
	return "", fmt.Errorf("包内找不到 %s", manifestName)
}

// Extract 把 zip 内容安全地解压到 dest，逐项施加限额与白名单。
func Extract(zr *zip.Reader, dest, prefix string, opts Options) error {
	b := newBudget(opts.Limits)
	var totalSize int64

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

		rel, err := safeRelPath(name, opts.Limits.MaxPathDepth)
		if err != nil {
			return err
		}
		if rel == "" {
			continue
		}
		rel = filepath.ToSlash(rel)

		info := f.FileInfo()
		if info.IsDir() {
			// 目录同样要计入条目预算：它不占字节，但会占 inode 与文件系统元数据。
			if budgetErr := b.addDir(rel); budgetErr != nil {
				return budgetErr
			}
			if mkErr := os.MkdirAll(filepath.Join(dest, rel), opts.DirPerm); mkErr != nil {
				return fmt.Errorf("创建目录 %s: %w", rel, mkErr)
			}
			continue
		}
		// 符号链接可以指到 /etc/passwd，解压时一律拒绝。
		if !info.Mode().IsRegular() {
			return fmt.Errorf("包内 %s 不是普通文件", rel)
		}
		if ext := strings.ToLower(path.Ext(rel)); !opts.AllowedExtensions[ext] {
			return fmt.Errorf("不允许的文件类型 %s（%s）", ext, rel)
		}

		// 落盘前先入账：文件本身与它隐含创建的父目录都要占额度。
		if budgetErr := b.addDir(path.Dir(rel)); budgetErr != nil {
			return budgetErr
		}
		if budgetErr := b.addFile(); budgetErr != nil {
			return budgetErr
		}
		written, err := extractFile(f, filepath.Join(dest, filepath.FromSlash(rel)), rel, opts)
		if err != nil {
			return err
		}
		totalSize += written
		if totalSize > opts.Limits.MaxTotalSize {
			return fmt.Errorf("解压后总大小超过 %d 字节", opts.Limits.MaxTotalSize)
		}
	}
	if b.files == 0 {
		return errors.New("包内没有任何文件")
	}
	return nil
}

// budget 统计落盘条目：文件与目录（显式的目录条目、以及文件路径隐含创建的父目录）。
//
// 目录去重后计数：同一个父目录被多个文件隐含创建时只算一次，
// 否则一个正常的深层包会被重复计账而误判。
type budget struct {
	files   int
	dirs    map[string]bool
	maxFile int
	maxAll  int
}

func newBudget(limits Limits) *budget {
	maxAll := limits.MaxEntries
	if maxAll <= 0 {
		// 旧调用方没给总条目额度时退回文件数上限，行为与过去一致。
		maxAll = limits.MaxFiles
	}
	return &budget{dirs: map[string]bool{}, maxFile: limits.MaxFiles, maxAll: maxAll}
}

// addFile 记一个文件。
func (b *budget) addFile() error {
	b.files++
	if b.files > b.maxFile {
		return fmt.Errorf("文件数超过 %d 个", b.maxFile)
	}
	return b.check()
}

// addDir 记一个目录及其全部上级目录（rel 为空或 "." 时忽略）。
func (b *budget) addDir(rel string) error {
	rel = strings.Trim(path.Clean(rel), "/")
	for rel != "" && rel != "." {
		if !b.dirs[rel] {
			b.dirs[rel] = true
			if err := b.check(); err != nil {
				return err
			}
		}
		parent := path.Dir(rel)
		if parent == rel {
			break
		}
		rel = parent
	}
	return nil
}

// check 校验总条目额度。
func (b *budget) check() error {
	if b.files+len(b.dirs) > b.maxAll {
		return fmt.Errorf("包内条目数超过 %d 个", b.maxAll)
	}
	return nil
}

// extractFile 解压单个文件，返回写入字节数。
func extractFile(f *zip.File, target, rel string, opts Options) (int64, error) {
	if err := os.MkdirAll(filepath.Dir(target), opts.DirPerm); err != nil {
		return 0, fmt.Errorf("创建目录 %s: %w", filepath.Dir(rel), err)
	}
	src, err := f.Open()
	if err != nil {
		return 0, fmt.Errorf("读取包内文件 %s: %w", rel, err)
	}
	defer func() { _ = src.Close() }()

	dst, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, opts.FilePerm)
	if err != nil {
		return 0, fmt.Errorf("写入文件 %s: %w", rel, err)
	}
	defer func() { _ = dst.Close() }()

	// 限流复制：不信任 zip 头里声明的 UncompressedSize64，
	// 高压缩比的炸弹正是靠谎报这个字段绕过预检的。
	written, err := io.Copy(dst, io.LimitReader(src, opts.Limits.MaxFileSize+1))
	if err != nil {
		return 0, fmt.Errorf("解压文件 %s: %w", rel, err)
	}
	if written > opts.Limits.MaxFileSize {
		return 0, fmt.Errorf("%s 解压后超过 %d 字节", rel, opts.Limits.MaxFileSize)
	}
	return written, nil
}

// safeRelPath 校验并规范化包内路径，拒绝穿越与绝对路径。
func safeRelPath(name string, maxDepth int) (string, error) {
	// zip 规范里分隔符就是 /，但实际存在用 \ 打的包。
	normalized := strings.ReplaceAll(name, `\`, "/")
	if strings.HasPrefix(normalized, "/") {
		return "", fmt.Errorf("包内路径不得为绝对路径（%s）", name)
	}
	if strings.Contains(normalized, "\x00") {
		return "", errors.New("包内路径含非法字符")
	}
	// 冒号一律拒绝：Windows 上 C:/x 是盘符，x.exe:y.css 会写成 x.exe 的备用数据流——
	// 扩展名检查看到的是 .css，落到盘上的却是一个 x.exe。主题与插件的文件名没有理由带冒号。
	if strings.Contains(normalized, ":") {
		return "", fmt.Errorf("包内路径不得含冒号（%s）", name)
	}

	clean := path.Clean(normalized)
	if clean == "." {
		return "", nil
	}
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("包内路径越界（%s）", name)
	}
	// 忽略打包工具留下的元数据目录。
	for _, junk := range []string{"__MACOSX/", ".git/", "node_modules/"} {
		if strings.HasPrefix(clean, junk) {
			return "", nil
		}
	}
	if strings.Count(clean, "/") >= maxDepth {
		return "", fmt.Errorf("包内路径层级过深（%s）", name)
	}
	return filepath.FromSlash(clean), nil
}
