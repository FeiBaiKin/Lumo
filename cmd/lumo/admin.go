package main

import (
	"errors"
	"flag"
	"fmt"
)

// runAdmin 是管理命令。用户体系与 argon2id 密码哈希在阶段 2 落地，
// 此处保留命令契约，避免将来变更 CLI 形态。
func runAdmin(args []string) error {
	fs := flag.NewFlagSet("admin", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return errors.New("用法：lumo admin reset-password")
	}
	switch fs.Arg(0) {
	case "reset-password":
		return errors.New("admin reset-password 尚未实现（阶段 2 · 认证与权限）")
	default:
		return fmt.Errorf("未知子命令 %q，可用：reset-password", fs.Arg(0))
	}
}
