package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// InstallFileName 是安装向导的产物文件名，位于工作目录下。
//
// 不写进 config.yaml 是因为 DatabaseConfig.DSN 刻意不带 yaml 标签：配置文件一旦
// 接受口令，它就会跟着版本库与配置备份到处走。这个文件权限 0600，且与 data/ 同生共死。
const InstallFileName = "install.json"

// InstallState 是安装向导写下的产物。
type InstallState struct {
	// Installed 为真表示向导已经跑完。它是「向导已关闭」的唯一凭据。
	Installed   bool      `json:"installed"`
	InstalledAt time.Time `json:"installedAt"`
	// Version 是执行安装时的程序版本，便于日后排查。
	Version string `json:"version"`
	// Database 是向导填写的数据库连接信息。
	Database InstallDatabase `json:"database"`
	// Site 是向导里填的站点信息，安装时已写入 settings，这里留档备查。
	Site InstallSite `json:"site"`
}

// InstallDatabase 是数据库连接信息。
type InstallDatabase struct {
	// DSN 含口令，故整个文件按 0600 保存，日志里一律经 RedactedDSN 输出。
	DSN string `json:"dsn"`
}

// InstallSite 是安装时填写的站点信息。
type InstallSite struct {
	Title string `json:"title,omitempty"`
	URL   string `json:"url,omitempty"`
}

// InstallStatePath 返回安装产物文件的路径。dataDir 为空时用内置默认值。
func InstallStatePath(dataDir string) string {
	if dataDir == "" {
		dataDir = Default().DataDir
	}
	return filepath.Join(dataDir, InstallFileName)
}

// LoadInstallState 读取安装产物。
//
// 文件不存在返回 (nil, nil)：没装过是正常状态而不是错误，调用方不必先 Stat。
func LoadInstallState(dataDir string) (*InstallState, error) {
	path := InstallStatePath(dataDir)
	data, err := os.ReadFile(path) //nolint:gosec // 路径由部署者经 dataDir 指定
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取安装文件 %s: %w", path, err)
	}

	var state InstallState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("解析安装文件 %s: %w", path, err)
	}
	return &state, nil
}

// SaveInstallState 写入安装产物。
//
// 先写同目录的临时文件再改名：进程若在写一半时被杀，下次启动读到的要么是旧内容、
// 要么是新内容，不会是截断的半截 JSON —— 那会让站点既起不来也重装不了。
func SaveInstallState(dataDir string, state *InstallState) error {
	path := InstallStatePath(dataDir)
	dir := filepath.Dir(path)
	// 目录通常已由 workdir.Init 建好；这里兜底，权限同样不含 other。
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("创建目录 %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化安装文件: %w", err)
	}
	data = append(data, '\n')

	// CreateTemp 建出的文件权限就是 0600，不必再 Chmod。
	tmp, err := os.CreateTemp(dir, InstallFileName+".tmp*")
	if err != nil {
		return fmt.Errorf("创建临时文件: %w", err)
	}
	tmpName := tmp.Name()
	// 任一步失败都要把临时文件收走，否则 dataDir 会慢慢堆满半截文件。
	defer func() {
		if _, statErr := os.Stat(tmpName); statErr == nil {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("写入临时文件: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("刷盘临时文件: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("关闭临时文件: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("替换安装文件 %s: %w", path, err)
	}
	return nil
}

// applyInstallState 用安装产物补齐配置。
//
// 只取数据库连接串，且只在环境变量没给的情况下取：运维用 LUMO_DATABASE_DSN 显式指定的
// 连接串永远优先于向导写下的那份，安装产物是兜底而不是权威。
func applyInstallState(cfg *Config) error {
	if cfg.Database.DSN != "" {
		return nil
	}
	state, err := LoadInstallState(cfg.DataDir)
	if err != nil {
		return err
	}
	if state == nil || !state.Installed || state.Database.DSN == "" {
		return nil
	}
	cfg.Database.DSN = state.Database.DSN
	return nil
}
