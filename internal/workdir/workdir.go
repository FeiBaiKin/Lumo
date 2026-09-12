// Package workdir 负责运行时工作目录的初始化。
//
// 目录结构固定为 ./data/{themes,plugins,uploads,cache,logs,backups}（agent.md §9）。
package workdir

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// 工作目录下各子目录的名称。模块引用这些常量而不是自己写字面量，
// 避免「themes」这种名字散落在多处、改一处漏一处。
const (
	ThemesDirName  = "themes"
	PluginsDirName = "plugins"
	UploadsDirName = "uploads"
	CacheDirName   = "cache"
	LogsDirName    = "logs"
	BackupsDirName = "backups"
)

// Subdirs 是工作目录下必须存在的子目录。
var Subdirs = []string{ThemesDirName, PluginsDirName, UploadsDirName, CacheDirName, LogsDirName, BackupsDirName}

// dirPerm 是新建目录的权限。0o750 而非 0o777：
// 工作目录含上传文件与备份，不应对同机其他用户开放。
const dirPerm os.FileMode = 0o750

// Init 幂等地创建工作目录及其子目录，返回其绝对路径。
func Init(root string, logger *slog.Logger) (string, error) {
	if root == "" {
		return "", fmt.Errorf("workdir: 根目录不能为空")
	}

	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("解析工作目录路径 %s: %w", root, err)
	}

	// 若同名路径已存在但不是目录，必须显式报错，否则后续写入会以难懂的方式失败。
	if info, statErr := os.Stat(abs); statErr == nil && !info.IsDir() {
		return "", fmt.Errorf("工作目录路径 %s 已被一个非目录文件占用", abs)
	}

	created := 0
	for _, name := range append([]string{""}, Subdirs...) {
		path := abs
		if name != "" {
			path = filepath.Join(abs, name)
		}
		if _, statErr := os.Stat(path); statErr == nil {
			continue
		}
		if err := os.MkdirAll(path, dirPerm); err != nil {
			return "", fmt.Errorf("创建目录 %s: %w", path, err)
		}
		created++
	}

	if logger != nil {
		if created > 0 {
			logger.Info("工作目录已初始化",
				slog.String("path", abs),
				slog.Int("created", created))
		} else {
			logger.Debug("工作目录已就绪", slog.String("path", abs))
		}
	}
	return abs, nil
}
