package update

import (
	"strconv"
	"strings"
)

// Version 是一个解析后的语义化版本。
//
// 自己解析而不是引依赖：需要的只是「比大小」这一件事，而发布用的 tag 与
// 本地 go build 注入的 `git describe` 输出（如 v0.3.0-4-gab12cd-dirty）
// 都不是严格的 semver —— 严格库会直接拒收后者，于是开发态下整页报错，
// 而那恰恰是最常看到这个页面的场景。
type Version struct {
	Major int
	Minor int
	Patch int
	// Prerelease 是 `-` 之后、`+` 之前的部分，空串表示正式版。
	Prerelease string
	// Raw 是去掉 v 前缀后的原文，回显给界面用。
	Raw string
}

// IsPrerelease 报告是否为预发布版本。
func (v Version) IsPrerelease() bool { return v.Prerelease != "" }

// ParseVersion 解析版本串，宽容地接受 `v` 前缀与构建元数据。
//
// 解析不出主版本号时返回 false：调用方据此把「比不出大小」与「比出来更旧」
// 区分开 —— 前者要在界面上说明原因，后者才是提示升级。
func ParseVersion(s string) (Version, bool) {
	raw := strings.TrimSpace(s)
	raw = strings.TrimPrefix(raw, "v")
	raw = strings.TrimPrefix(raw, "V")
	if raw == "" {
		return Version{}, false
	}

	core := raw
	// 构建元数据不参与比较（semver 规范如此），先摘掉。
	if i := strings.IndexByte(core, '+'); i >= 0 {
		core = core[:i]
	}
	var pre string
	if i := strings.IndexByte(core, '-'); i >= 0 {
		pre = core[i+1:]
		core = core[:i]
	}

	parts := strings.Split(core, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return Version{}, false
	}
	nums := make([]int, 3)
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return Version{}, false
		}
		nums[i] = n
	}
	return Version{Major: nums[0], Minor: nums[1], Patch: nums[2], Prerelease: pre, Raw: raw}, true
}

// CompareVersions 比较两个版本，返回 -1 / 0 / 1。
func CompareVersions(a, b Version) int {
	if c := compareInt(a.Major, b.Major); c != 0 {
		return c
	}
	if c := compareInt(a.Minor, b.Minor); c != 0 {
		return c
	}
	if c := compareInt(a.Patch, b.Patch); c != 0 {
		return c
	}
	return comparePrerelease(a.Prerelease, b.Prerelease)
}

func compareInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// comparePrerelease 按 semver 第 11 条比较预发布标识。
//
// 要点有三：有预发布的小于没有的（1.0.0-rc1 < 1.0.0）；
// 标识逐段比，纯数字按数值、其余按字典序，且数字段小于字母段；
// 前面都相等时段数多的更大。
func comparePrerelease(a, b string) int {
	if a == b {
		return 0
	}
	if a == "" {
		return 1
	}
	if b == "" {
		return -1
	}

	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		if c := compareIdentifier(as[i], bs[i]); c != 0 {
			return c
		}
	}
	return compareInt(len(as), len(bs))
}

func compareIdentifier(a, b string) int {
	an, aErr := strconv.Atoi(a)
	bn, bErr := strconv.Atoi(b)
	switch {
	case aErr == nil && bErr == nil:
		return compareInt(an, bn)
	case aErr == nil:
		// 数字段总是小于字母段。
		return -1
	case bErr == nil:
		return 1
	default:
		return strings.Compare(a, b)
	}
}
