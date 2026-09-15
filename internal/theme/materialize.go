package theme

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// 内置主题的两种来源。
const (
	// SourceEmbedded 表示这份内置主题直接来自二进制（磁盘上没有副本，或那份副本坏了）。
	SourceEmbedded = "embedded"
	// SourceDisk 表示它是 data/themes/<内置主题名> 下那份副本：站长可以改，改了就生效。
	SourceDisk = "disk"
)

// MaterializeBuiltin 把内置主题解压到 root/<BuiltinName>，已存在则不动。
//
// 为什么要落到磁盘（2026-09-15 站长定）：内置主题原先只活在二进制里，
// 于是 data/themes 空空如也 —— 站长既看不到默认主题长什么样，也改不动它，
// 而「默认主题也是一份普通主题」是 WordPress 那一类系统的通行做法。
//
// 落盘之后的规矩：**以磁盘为准**（缺的模板回退到二进制里的原版），后台不可删除，
// 改坏了走「恢复出厂」。返回 true 表示这次真的写了文件（首次启动或刚恢复出厂）。
func MaterializeBuiltin(root string, builtinFS fs.FS) (bool, error) {
	dir := filepath.Join(root, BuiltinName)
	if _, err := os.Stat(dir); err == nil {
		return false, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return false, fmt.Errorf("检查内置主题目录: %w", err)
	}
	if err := writeTree(dir, builtinFS); err != nil {
		return false, err
	}
	return true, nil
}

// RestoreBuiltin 把内置主题恢复成出厂状态：删掉磁盘副本再重新解压一遍。
//
// 站长的改动会全部丢失，所以调用方必须先确认。这条路径同时是「改坏了怎么回去」的
// 唯一答案 —— 没有它，站长改崩一个模板就只能去翻发行包重新解压。
func RestoreBuiltin(root string, builtinFS fs.FS) error {
	dir := filepath.Join(root, BuiltinName)
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("清除内置主题目录: %w", err)
	}
	return writeTree(dir, builtinFS)
}

// writeTree 把 fsys 里的全部文件原样写到 dir 下。
func writeTree(dir string, fsys fs.FS) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("创建主题目录: %w", err)
	}
	return fs.WalkDir(fsys, ".", func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(dir, filepath.FromSlash(p))
		if entry.IsDir() {
			if mkErr := os.MkdirAll(target, 0o755); mkErr != nil {
				return fmt.Errorf("创建目录 %s: %w", p, mkErr)
			}
			return nil
		}
		data, readErr := fs.ReadFile(fsys, p)
		if readErr != nil {
			return fmt.Errorf("读取 %s: %w", p, readErr)
		}
		// 一律 0644，不保留可执行位：embed 里的文件本就没有有意义的权限位，
		// 而把可执行位带给主题自带的脚本，会让「装一个主题」凭空多出一层风险。
		if writeErr := os.WriteFile(target, data, 0o644); writeErr != nil {
			return fmt.Errorf("写入 %s: %w", p, writeErr)
		}
		return nil
	})
}
