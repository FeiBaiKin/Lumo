package update

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// ErrBadBackupName 表示备份名不合法。
var ErrBadBackupName = errors.New("备份名不合法")

// backupTimeLayout 是备份名里的时间格式。
const backupTimeLayout = "20060102-150405"

// backupPattern 限定备份文件名的形状：lumo-<版本>-<时间戳>[.exe]。
//
// 删除接口拿名字定位文件，这条正则就是那唯一的守卫——它同时挡住了路径穿越
// 与「把备份目录里别的东西删掉」。宁可严一点：手工改过名的文件在界面上不出现，
// 也删不掉，但它本来也不是这个功能管的。
var backupPattern = regexp.MustCompile(`^lumo-([A-Za-z0-9._+-]+)-(\d{8}-\d{6})(\.exe)?$`)

// versionSanitizer 去掉版本串里不适合进文件名的字符。
var versionSanitizer = regexp.MustCompile(`[^A-Za-z0-9._+-]+`)

// Backup 是一份旧版本备份。
type Backup struct {
	Name string `json:"name" doc:"备份文件名，删除时用它定位"`
	// Version 是这份备份对应的版本，从文件名解析。
	Version string `json:"version"`
	Size    int64  `json:"size" doc:"字节数"`
	// CreatedAt 取自文件名里的时间戳，不是文件的修改时间——
	// 拷贝、迁移都会改后者，而备份是哪天的这件事不该跟着变。
	CreatedAt time.Time `json:"createdAt"`
}

// backupName 拼出一份备份的文件名。
func backupName(version string, at time.Time) string {
	clean := versionSanitizer.ReplaceAllString(strings.TrimSpace(version), "-")
	if clean == "" {
		clean = "unknown"
	}
	return fmt.Sprintf("lumo-%s-%s%s", clean, at.Format(backupTimeLayout), exeExt())
}

// ListBackups 列出备份目录里的旧版本，最新的在前。
//
// 目录不存在时返回空列表而不是报错：从没升级过的站点就没有这个目录。
func ListBackups(dir string) ([]Backup, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取备份目录: %w", err)
	}

	out := make([]Backup, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		match := backupPattern.FindStringSubmatch(entry.Name())
		if match == nil {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			continue
		}
		created, _ := time.ParseInLocation(backupTimeLayout, match[2], time.Local)
		out = append(out, Backup{
			Name:      entry.Name(),
			Version:   match[1],
			Size:      info.Size(),
			CreatedAt: created,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

// DeleteBackup 删除一份备份。
func DeleteBackup(dir, name string) error {
	if !backupPattern.MatchString(name) {
		return ErrBadBackupName
	}
	if err := os.Remove(filepath.Join(dir, name)); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return fmt.Errorf("删除备份 %s: %w", name, err)
	}
	return nil
}

// PruneBackups 只保留最新的 keep 份备份，返回删掉的份数。
// keep 小于 1 时不清理——站长把它设成 0 就是「都留着，我自己管」。
func PruneBackups(dir string, keep int) int {
	if keep < 1 {
		return 0
	}
	list, err := ListBackups(dir)
	if err != nil || len(list) <= keep {
		return 0
	}
	removed := 0
	for _, backup := range list[keep:] {
		if err := DeleteBackup(dir, backup.Name); err == nil {
			removed++
		}
	}
	return removed
}
