package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// binaryStem 是发布产物里主程序的名字，与 .goreleaser.yaml 的 binary 一致。
const binaryStem = "lumo"

// binaryPerm 是主程序文件的权限。0o755 而非 0o750：
// 部署里「装的人」与「跑的人」常常不是同一个账号（root 装、lumo 跑）。
const binaryPerm os.FileMode = 0o755

// backupPerm 是备份文件的权限。备份躺在工作目录里，只该由跑服务的账号读写。
const backupPerm os.FileMode = 0o700

// selfCheckTimeout 是新版本自检的期限。只是跑一次 version 子命令，
// 拖到 20 秒还没结果说明这个二进制根本不能用。
const selfCheckTimeout = 20 * time.Second

// exeExt 返回当前平台可执行文件的扩展名。
func exeExt() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// binaryName 返回当前平台主程序的文件名。
func binaryName() string { return binaryStem + exeExt() }

// Installer 把下载好的发布包换成正在运行的这份二进制。
//
// 顺序是「解包 → 自检 → 备份 → 替换」，每一步失败都不留下半成品：
// 一次装坏的升级意味着站点起不来，而那时站长手上只剩 SSH。
type Installer struct {
	env       Environment
	backupDir string
}

// NewInstaller 构造安装器。
func NewInstaller(env Environment, backupDir string) *Installer {
	return &Installer{env: env, backupDir: backupDir}
}

// Install 安装一个已经过校验和核对的发布包，返回备份文件名。
//
// expect 是期望装上的版本，用于自检时核对——发布包名字对不代表里面的东西对。
func (i *Installer) Install(ctx context.Context, archivePath string, expect Version, currentVersion string) (string, error) {
	staged, err := i.stage(archivePath)
	if err != nil {
		return "", err
	}
	// 从这里到替换成功之间的任何失败都必须把暂存文件清掉，
	// 否则程序目录里会攒下一堆没人认得的 .lumo-new-*。
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(staged)
		}
	}()

	if checkErr := selfCheck(ctx, staged, expect); checkErr != nil {
		return "", checkErr
	}

	backup, err := i.backup(currentVersion)
	if err != nil {
		return "", err
	}

	if replaceErr := replaceExecutable(staged, i.env.Executable); replaceErr != nil {
		// 替换失败时把备份也删掉：它对应的还是正在跑的这个版本，留着只会让
		// 备份列表里出现一份与当前完全相同的文件，误导下一次排障。
		_ = os.Remove(filepath.Join(i.backupDir, backup))
		return "", replaceErr
	}
	committed = true
	return backup, nil
}

// stage 把发布包里的主程序解到目标目录下的临时文件。
//
// 必须解到**主程序所在目录**：替换靠 rename，而 rename 跨不了文件系统，
// 解到 data/cache 再搬过去会在「程序在 /usr/local/bin、数据在 /var/lib」这类
// 再普通不过的布局上失败。
func (i *Installer) stage(archivePath string) (string, error) {
	f, err := os.CreateTemp(i.env.Dir, ".lumo-new-*"+exeExt())
	if err != nil {
		return "", fmt.Errorf("在程序目录创建临时文件: %w", err)
	}
	staged := f.Name()
	_ = f.Close()

	if err := extractBinary(archivePath, staged); err != nil {
		_ = os.Remove(staged)
		return "", err
	}
	return staged, nil
}

// backup 把当前二进制复制一份到备份目录，返回备份文件名。
//
// 复制而不是移动：移动之后若替换失败，机器上就一份可执行文件也不剩了——
// 当前进程还活着，但它的文件已经不在原位，重启即失败。多花几十 MB 磁盘换这个。
func (i *Installer) backup(currentVersion string) (string, error) {
	if err := os.MkdirAll(i.backupDir, 0o750); err != nil {
		return "", fmt.Errorf("创建备份目录: %w", err)
	}

	name := backupName(currentVersion, time.Now())
	dst := filepath.Join(i.backupDir, name)

	src, err := os.Open(i.env.Executable)
	if err != nil {
		return "", fmt.Errorf("读取当前程序: %w", err)
	}
	defer func() { _ = src.Close() }()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, backupPerm)
	if err != nil {
		return "", fmt.Errorf("创建备份文件: %w", err)
	}
	if _, err := io.Copy(out, src); err != nil {
		_ = out.Close()
		_ = os.Remove(dst)
		return "", fmt.Errorf("写入备份文件: %w", err)
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		_ = os.Remove(dst)
		return "", fmt.Errorf("写入备份文件: %w", err)
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(dst)
		return "", fmt.Errorf("写入备份文件: %w", err)
	}
	return name, nil
}

// selfCheck 运行新二进制的 version 子命令，确认它能跑、且确实是要装的那个版本。
//
// 这一步是整条链路上最值钱的检查：校验和只能证明「下到的东西没被改过」，
// 证明不了它能在这台机器上运行（架构拿错、glibc 版本不合、文件被杀软掏空）。
// 不做这步的代价是重启之后站点直接起不来，而那时已经没有界面可以操作了。
func selfCheck(ctx context.Context, path string, expect Version) error {
	ctx, cancel := context.WithTimeout(ctx, selfCheckTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, path, "version", "-json").Output()
	if err != nil {
		return fmt.Errorf("新版本自检失败，它在本机无法运行：%w", err)
	}

	var info struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(out, &info); err != nil {
		return fmt.Errorf("新版本自检失败，输出不是预期的版本信息：%w", err)
	}
	got, ok := ParseVersion(info.Version)
	if !ok {
		return fmt.Errorf("新版本自检失败，版本号无法解析：%q", info.Version)
	}
	if expect.Raw != "" && CompareVersions(got, expect) != 0 {
		return fmt.Errorf("新版本自检失败，包里装的是 %s 而不是 %s", got.Raw, expect.Raw)
	}
	return nil
}
