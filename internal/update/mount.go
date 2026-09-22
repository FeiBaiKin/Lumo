package update

import (
	"os"
	"strconv"
	"strings"
)

// mountInfoPath 是内核给出的挂载表，只有 Linux 有。
const mountInfoPath = "/proc/self/mountinfo"

// 不算「持久」的文件系统：它们的内容随重启或容器生命周期消失，
// 哪怕挂载点看着是独立的。没人会把程序放这儿，但判断错的代价是
// 「升级成功，重启后版本没变」，写两行排除掉更省心。
var ephemeralFilesystems = map[string]bool{
	"tmpfs": true, "ramfs": true, "devtmpfs": true,
}

// mountPointOf 返回覆盖 dir 的最长挂载点，以及它的文件系统类型。
//
// 读不到挂载表（非 Linux、/proc 没挂）时 ok 为 false，调用方据此
// 放弃这条判断而不是猜一个答案。
func mountPointOf(dir string) (point, fstype string, ok bool) {
	data, err := os.ReadFile(mountInfoPath)
	if err != nil {
		return "", "", false
	}
	return parseMountInfo(string(data), dir)
}

// parseMountInfo 是上面那个函数的纯逻辑部分，与文件读取分开以便单独验证。
func parseMountInfo(data, dir string) (point, fstype string, ok bool) {
	for _, line := range strings.Split(data, "\n") {
		fields := strings.Fields(line)
		// 格式见 Documentation/filesystems/proc.rst：
		//   ID 父ID 主:次 源内路径 挂载点 挂载选项 [可选字段...] - 类型 源 超级块选项
		// 可选字段的个数不固定，故文件系统类型要从分隔符 "-" 往后数。
		if len(fields) < 5 {
			continue
		}
		candidate := unescapeMountPath(fields[4])
		if !underPath(dir, candidate) || len(candidate) <= len(point) {
			continue
		}
		point = candidate
		fstype = ""
		for i, f := range fields {
			if f == "-" && i+1 < len(fields) {
				fstype = fields[i+1]
				break
			}
		}
	}
	if point == "" {
		return "", "", false
	}
	return point, fstype, true
}

// persistsBeyondContainer 判断 dir 里的文件能不能活过一次容器重建。
//
// 判据是「它归哪个挂载点管」：落在容器自己的根文件系统上（挂载点就是 /，
// 通常是 overlay）的东西，随容器一起消失；落在宿主机挂进来的目录或卷上的，
// 容器换了它还在。
//
// 这正是「能不能就地升级」真正该问的问题。此前拿「在不在容器里」当它的替身，
// 在官方镜像上恰好同解，在「容器里跑挂载目录上的二进制」这种部署上就完全错了——
// 1Panel、宝塔的运行环境都是后者，而那是面板用户里最常见的一种装法。
func persistsBeyondContainer(dir string) (persists bool, point string, known bool) {
	point, fstype, ok := mountPointOf(dir)
	if !ok {
		return false, "", false
	}
	if point == "/" || ephemeralFilesystems[fstype] {
		return false, point, true
	}
	return true, point, true
}

// underPath 判断 path 是否在 base 之下（或正是 base）。
func underPath(path, base string) bool {
	if base == "/" {
		return true
	}
	return path == base || strings.HasPrefix(path, base+"/")
}

// unescapeMountPath 还原挂载表里的八进制转义。
//
// 内核把空格、制表符、换行与反斜杠写成 \040 这样的形式，
// 不还原的话「/mnt/my apps」这类路径永远匹配不上。
func unescapeMountPath(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] == '\\' && i+3 < len(s) {
			if n, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(n))
				i += 4
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}
