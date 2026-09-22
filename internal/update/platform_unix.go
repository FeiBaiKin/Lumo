//go:build !windows

package update

import (
	"fmt"
	"os"
	"syscall"
)

// replaceExecutable 用新文件覆盖正在运行的主程序。
//
// Unix 上 rename 覆盖一个正在执行的文件是安全的：内核认的是 inode，
// 当前进程继续跑在旧 inode 上，新请求走新文件。所以这里一步到位，没有中间态。
func replaceExecutable(staged, target string) error {
	if err := os.Rename(staged, target); err != nil {
		return fmt.Errorf("替换程序文件: %w", err)
	}
	return nil
}

// cleanupStale 清理上一次升级留下的旧文件。Unix 上不会产生，留空实现对齐两边。
func cleanupStale(string) {}

// execSelf 用新版本替换当前进程镜像。
//
// 用 exec 而不是「起一个新进程再退出」：进程号不变，systemd 的 MainPID
// 仍然对得上，supervisor 也不会把这次升级看成一次崩溃重启。
// 监听套接字带着 close-on-exec 标志，exec 的那一刻自动释放，端口不会撞上。
func execSelf(exe string, args []string) error {
	return syscall.Exec(exe, args, os.Environ())
}
