package theme

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
)

// 内置主题的两种来源。
const (
	// SourceEmbedded 表示这份内置主题直接来自二进制（磁盘上没有副本，或那份副本坏了）。
	SourceEmbedded = "embedded"
	// SourceDisk 表示它是 data/themes/<内置主题名> 下那份副本：站长可以改，改了就生效。
	SourceDisk = "disk"
)

// factoryManifestName 是出厂清单的文件名：解压时记下每个文件的 SHA-256。
//
// 有了它，启动时才分得清「这份副本没人动过、只是版本旧了」与「站长改过模板」——
// 前者可以放心换成新版本，后者一个字节都不能碰。以点开头，免得站长把它当成主题的一部分。
const factoryManifestName = ".factory.json"

// BuiltinSync 是启动时核对内置主题磁盘副本的结果。
type BuiltinSync int

const (
	// BuiltinUnchanged 副本与二进制里的版本一致（或改过但二进制没出新版本），不需要动。
	BuiltinUnchanged BuiltinSync = iota
	// BuiltinCreated 磁盘上还没有副本，刚解压了一份。
	BuiltinCreated
	// BuiltinUpdated 副本没被改动过，已换成二进制里的新版本。
	BuiltinUpdated
	// BuiltinModified 二进制里有新版本，但副本被改动过，原样保留。
	BuiltinModified
	// BuiltinUnknown 副本是 0.1.5 及更早的版本解压的，没有出厂清单，
	// 也对不上任何一版出厂文件，判断不了改没改过，原样保留。
	BuiltinUnknown
)

// legacyFactoryDigests 是 0.1.0–0.1.5 出厂「墨」整套文件的摘要（算法见 manifestDigest），
// 由各版本标签下的仓库文件算出。这些版本解压时不写出厂清单；磁盘副本与其中某一版
// 逐字节一致，就说明没人动过，可以直接换成新版本，老站点升级时不必手动恢复出厂。
// 此后的版本都会写清单，这张表不再增长。
var legacyFactoryDigests = map[string]string{
	"c07adc401846ff45e06206411ce3db418bc8f7b50276259bccc5f539b52b8315": "0.1.0",
	"f478979f2b0ff3ec512a7d3c0bc27d69304c6420eb4b00a3fbe39295afcd02b3": "0.1.1–0.1.2",
	"5aa64e68e769c0b19d38186f775ba082a8ee1e1fe8e526c2ededeba83e518318": "0.1.3–0.1.4",
	"953e2e896f7a31a706dfdd46d16876a9a193e9f62aff2698f7985facd1d7d4b4": "0.1.5",
}

// MaterializeBuiltin 让 root/<BuiltinName> 下有一份内置主题，并在程序升级后把没改过的副本换成新版本。
//
// 为什么要落到磁盘（2026-09-15 站长定）：内置主题原先只活在二进制里，
// 于是 data/themes 空空如也 —— 站长既看不到默认主题长什么样，也改不动它，
// 而默认主题本该是一份普通主题：看得到、改得动。
//
// 落盘之后的规矩：**以磁盘为准**（缺的模板回退到二进制里的原版），后台不可删除，
// 改坏了走「恢复出厂」。
//
// 为什么升级时要换（2026-09-23）：原先「已存在则不动」，副本就永远停在第一次启动的版本，
// 升级程序换不来主题的任何修复，0.1.5 的落地页模块在升上来的站点上根本不出现。
// 换不换只看一件事：磁盘上的文件是否与出厂清单逐字节一致，一致才换，多一个少一个都不换。
func MaterializeBuiltin(root string, builtinFS fs.FS) (BuiltinSync, error) {
	dir := filepath.Join(root, BuiltinName)
	if _, statErr := os.Stat(dir); errors.Is(statErr, fs.ErrNotExist) {
		if err := writeFactory(dir, builtinFS); err != nil {
			return BuiltinUnchanged, err
		}
		return BuiltinCreated, nil
	} else if statErr != nil {
		return BuiltinUnchanged, fmt.Errorf("检查内置主题目录: %w", statErr)
	}

	recorded, err := readManifest(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return syncLegacy(root, dir, builtinFS)
	}
	if err != nil {
		return BuiltinUnchanged, err
	}
	shipped, err := manifestOf(builtinFS)
	if err != nil {
		return BuiltinUnchanged, err
	}
	if maps.Equal(recorded, shipped) {
		return BuiltinUnchanged, nil
	}
	onDisk, err := manifestOf(os.DirFS(dir))
	if err != nil {
		return BuiltinUnchanged, err
	}
	if !maps.Equal(onDisk, recorded) {
		return BuiltinModified, nil
	}
	if err := RestoreBuiltin(root, builtinFS); err != nil {
		return BuiltinUnchanged, err
	}
	return BuiltinUpdated, nil
}

// syncLegacy 处理没有出厂清单的旧副本：与某一版出厂文件完全一致才替换。
func syncLegacy(root, dir string, builtinFS fs.FS) (BuiltinSync, error) {
	onDisk, err := manifestOf(os.DirFS(dir))
	if err != nil {
		return BuiltinUnchanged, err
	}
	if _, ok := legacyFactoryDigests[manifestDigest(onDisk)]; !ok {
		return BuiltinUnknown, nil
	}
	if err := RestoreBuiltin(root, builtinFS); err != nil {
		return BuiltinUnchanged, err
	}
	return BuiltinUpdated, nil
}

// RestoreBuiltin 把内置主题恢复成出厂状态：删掉磁盘副本再重新解压一遍。
//
// 站长的改动会全部丢失，所以调用方必须先确认。这条路径同时是「改坏了怎么回去」的
// 唯一答案 —— 没有它，站长改崩一个模板就只能去翻发行包重新解压。
// 旧版本解压的副本没有出厂清单，恢复一次之后就有了，此后升级会自动更新。
func RestoreBuiltin(root string, builtinFS fs.FS) error {
	dir := filepath.Join(root, BuiltinName)
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("清除内置主题目录: %w", err)
	}
	return writeFactory(dir, builtinFS)
}

// writeFactory 解压内置主题并写下出厂清单。清单最后写：解压中途失败时没有清单，
// 下次启动按「判断不了」处理，而不是把一份残缺的副本当成完好的出厂版本。
func writeFactory(dir string, builtinFS fs.FS) error {
	if err := writeTree(dir, builtinFS); err != nil {
		return err
	}
	manifest, err := manifestOf(builtinFS)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("编码出厂清单: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, factoryManifestName), data, 0o644); err != nil {
		return fmt.Errorf("写入出厂清单: %w", err)
	}
	return nil
}

// readManifest 读出厂清单；文件不存在时原样返回 fs.ErrNotExist。
func readManifest(dir string) (map[string]string, error) {
	data, err := os.ReadFile(filepath.Join(dir, factoryManifestName))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		return nil, fmt.Errorf("读取出厂清单: %w", err)
	}
	var manifest map[string]string
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("解析出厂清单: %w", err)
	}
	return manifest, nil
}

// manifestDigest 把一份清单压成一个摘要：按路径排序，逐行「路径 NUL 摘要 换行」再取 SHA-256。
func manifestDigest(manifest map[string]string) string {
	h := sha256.New()
	for _, p := range slices.Sorted(maps.Keys(manifest)) {
		fmt.Fprintf(h, "%s\x00%s\n", p, manifest[p])
	}
	return hex.EncodeToString(h.Sum(nil))
}

// manifestOf 计算 fsys 里每个文件的 SHA-256（键为斜杠分隔的相对路径），跳过出厂清单本身。
func manifestOf(fsys fs.FS) (map[string]string, error) {
	manifest := map[string]string{}
	err := fs.WalkDir(fsys, ".", func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || p == factoryManifestName {
			return nil
		}
		data, readErr := fs.ReadFile(fsys, p)
		if readErr != nil {
			return fmt.Errorf("读取 %s: %w", p, readErr)
		}
		sum := sha256.Sum256(data)
		manifest[p] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("计算主题文件摘要: %w", err)
	}
	return manifest, nil
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
