package update

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Environment 是「这份二进制能不能自己换掉自己」的判断依据。
//
// 三个否定条件各自对应一种真实部署：容器里换了也白换（重启回到镜像里的旧版本）、
// 目录只读的话根本写不进去、拿不到自己的路径就无从替换。
// 判断结果要原样告诉站长 —— 「更新按钮为什么是灰的」必须当场能答。
type Environment struct {
	// Executable 是当前二进制的绝对路径，已解开符号链接。
	//
	// 解符号链接是必须的：不少部署把 /usr/local/bin/lumo 指到带版本号的实体文件，
	// 替换链接本身会把那套版本管理弄乱，而替换实体才是站长想要的。
	Executable string
	// Dir 是二进制所在目录，新版本先落到这里再原地改名。
	Dir string
	// Container 为真表示跑在容器里。
	Container bool
	// Writable 为真表示二进制所在目录可写。
	Writable bool
	// OS 与 Arch 是当前平台，用来挑发布资产。
	OS   string
	Arch string
	// Err 是探测过程中的错误描述，空串表示探测正常。
	Err string
}

// DetectEnvironment 探测当前运行环境。
func DetectEnvironment() Environment {
	env := Environment{
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		Container: inContainer(),
	}

	exe, err := os.Executable()
	if err != nil {
		env.Err = "无法定位当前程序的路径：" + err.Error()
		return env
	}
	if resolved, linkErr := filepath.EvalSymlinks(exe); linkErr == nil {
		exe = resolved
	}
	if abs, absErr := filepath.Abs(exe); absErr == nil {
		exe = abs
	}
	env.Executable = exe
	env.Dir = filepath.Dir(exe)
	env.Writable = dirWritable(env.Dir)
	return env
}

// CanUpdate 报告能否就地升级，不能时一并给出原因。
//
// allowInContainer 来自配置：容器里默认不升级，但站长可以明确选择承担那个代价
// （见 config.UpdateConfig.AllowInContainer 与 state.go 的回退检测）。
func (e Environment) CanUpdate(allowInContainer bool) (ok bool, reason string) {
	switch {
	case e.Err != "":
		return false, e.Err
	case e.Container && !allowInContainer:
		return false, "运行在容器中：换掉的文件活在容器的可写层里，重建容器就会回到镜像里的版本。" +
			"请改用新的镜像标签升级；确实要在容器里就地升级，开 update.allowInContainer"
	case !e.Writable:
		return false, "程序所在目录不可写（" + e.Dir + "），请改由部署脚本或包管理器升级"
	default:
		return true, ""
	}
}

// containerMarkers 是容器运行时留下的标记文件。
var containerMarkers = []string{"/.dockerenv", "/run/.containerenv"}

// containerCgroupHints 是 /proc/1/cgroup 中指示**应用容器**的片段。
//
// 刻意不含 lxc：LXC/OpenVZ 多数时候是「系统容器」，也就是一台按 VPS 卖的机器——
// 上面跑的是直接部署的二进制，文件系统持久，就地升级完全有效。把它算作容器
// 等于对一大批最普通的部署关掉这个功能，还附赠一句让人摸不着头脑的
// 「运行在容器中」。应用容器有更明确的标记（下面那两个文件与这里的片段）。
var containerCgroupHints = []string{"docker", "containerd", "kubepods", "/podman"}

// inContainer 判断当前进程是否运行在**应用容器**里。
//
// 判断不可能百分之百准确。宁可漏判也不误判：漏判的代价是站长在容器里升了一次
// 级，而那件事现在有回退检测兜着；误判的代价是功能对一台好端端的 VPS 直接关闭，
// 且没有任何办法说服它。
func inContainer() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	if os.Getenv("KUBERNETES_SERVICE_HOST") != "" {
		return true
	}
	for _, marker := range containerMarkers {
		if _, err := os.Stat(marker); err == nil {
			return true
		}
	}
	data, err := os.ReadFile("/proc/1/cgroup")
	if err != nil {
		return false
	}
	text := string(data)
	for _, hint := range containerCgroupHints {
		if strings.Contains(text, hint) {
			return true
		}
	}
	return false
}

// dirWritable 用「真写一个文件」判断目录是否可写。
//
// 不看权限位：容器的只读挂载、SELinux、Windows 的 ACL 都能让权限位看着没问题
// 而写入照样失败。唯一可靠的检查就是写一次。
func dirWritable(dir string) bool {
	f, err := os.CreateTemp(dir, ".lumo-writable-*")
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return true
}
