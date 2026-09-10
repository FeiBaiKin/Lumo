// Package version 承载构建期注入的版本信息。
package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// 以下变量由链接器在构建时注入，见 Taskfile.yml 与 .goreleaser.yaml。
// 默认值对应「从源码直接 go build / go run」的场景。
var (
	// Version 语义化版本号，发布构建由 goreleaser 注入 tag。
	Version = "0.0.0-dev"
	// Commit git 提交短哈希。
	Commit = "unknown"
	// Date 构建时间，RFC 3339 格式。
	Date = "unknown"
)

// Info 汇总一次构建的完整版本信息。
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	Date      string `json:"date"`
	GoVersion string `json:"goVersion"`
	Platform  string `json:"platform"`
}

// Get 返回当前构建的版本信息。
// 未经链接器注入时，尝试从 VCS 构建信息中回填提交哈希，方便本地调试。
func Get() Info {
	info := Info{
		Version:   Version,
		Commit:    Commit,
		Date:      Date,
		GoVersion: runtime.Version(),
		Platform:  runtime.GOOS + "/" + runtime.GOARCH,
	}

	if info.Commit != "unknown" {
		return info
	}

	build, ok := debug.ReadBuildInfo()
	if !ok {
		return info
	}
	for _, setting := range build.Settings {
		switch setting.Key {
		case "vcs.revision":
			if len(setting.Value) >= 7 {
				info.Commit = setting.Value[:7]
			} else if setting.Value != "" {
				info.Commit = setting.Value
			}
		case "vcs.time":
			if setting.Value != "" {
				info.Date = setting.Value
			}
		}
	}
	return info
}

// String 返回单行可读版本描述。
func (i Info) String() string {
	return fmt.Sprintf("lumo %s (commit %s, built %s, %s, %s)",
		i.Version, i.Commit, i.Date, i.GoVersion, i.Platform)
}
