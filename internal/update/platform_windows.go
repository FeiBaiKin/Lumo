//go:build windows

package update

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

// staleSuffix 是被挪开的旧程序的后缀。
const staleSuffix = ".old"

// replaceExecutable 用新文件换掉正在运行的主程序。
//
// Windows 不允许覆盖正在运行的可执行文件，但允许**重命名**它。
// 于是先把旧的挪到 lumo.exe.old，再把新的改名就位；失败则把旧的挪回来。
// 那个 .old 要等进程退出后才删得掉，故留到下次启动时清理。
func replaceExecutable(staged, target string) error {
	stale := target + staleSuffix
	// 上一次升级留下的那份此刻已经没有进程占用，先清掉，否则改名会失败。
	_ = os.Remove(stale)
	if err := os.Rename(target, stale); err != nil {
		return fmt.Errorf("挪开当前程序: %w", err)
	}
	if err := os.Rename(staged, target); err != nil {
		_ = os.Rename(stale, target)
		return fmt.Errorf("替换程序文件: %w", err)
	}
	return nil
}

// cleanupStale 删除上一次升级挪开的旧程序。
func cleanupStale(target string) {
	if target == "" {
		return
	}
	_ = os.Remove(target + staleSuffix)
}

// execSelf 拉起新版本进程。
//
// Windows 没有 exec 语义，只能新起一个进程、让当前进程随后退出。
// 句柄直接继承，控制台输出不断线；调用方必须**先完成停机**再调它，
// 否则新进程会撞上还没释放的监听端口。
func execSelf(exe string, args []string) error {
	// 用 context.Background()：新进程要活过当前进程，不能挂在任何会被取消的
	// context 上——那等于刚拉起来就被杀掉。
	cmd := exec.CommandContext(context.Background(), exe, args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if wd, err := os.Getwd(); err == nil {
		cmd.Dir = wd
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("拉起新版本进程: %w", err)
	}
	return nil
}
