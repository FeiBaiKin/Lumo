package plugin

import (
	"fmt"
	"strconv"
	"strings"
)

// 版本与范围比较，供插件依赖（spec.dependencies）使用。
//
// 只做清单需要的那一小块 semver：数字段一到三段加可选的预发布标记。范围写法：
// `*` 不限、`1.2.3` 精确、`= > >= < <=` 比较、`^` 与 `~` 简写、数字段位置的 `x` / `*` 通配。
// 比较符之间是「与」（空格或逗号），`||` 是「或」。写法与 node-semver 一致，包括那两条
// 容易记错的：光写 `1.2` 等同于 `1.2.x`（不是「正好 1.2.0」），而 `>=1.2` 等同于 `>=1.2.0`。
//
// 预发布版本按普通大小比较（1.0.0-alpha < 1.0.0），不实现 npm 那条「预发布只在与它
// 同主次修订的比较符下才算命中」的细则：依赖里写预发布范围本来就少见，而按普通
// 比较更严——`^1.0.0` 不会把 1.0.0-beta 当成满足。

// semver 是一个解析后的版本。缺的段按 0 补。
type semver struct {
	major, minor, patch int
	// pre 是预发布标记，如 1.2.3-beta.1 里的 [beta 1]；没有时为空。
	pre []string
}

// parseNumeric 解析一个纯数字段。前导零不算合法数字段。
func parseNumeric(s string) (int, bool) {
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 || s != strconv.Itoa(n) {
		return 0, false
	}
	return n, true
}

func isWildcard(s string) bool { return s == "*" || strings.EqualFold(s, "x") }

// parseVersion 解析一个版本号。接受一段到三段数字，可带预发布标记。
func parseVersion(s string) (semver, error) {
	var v semver
	s = strings.TrimSpace(s)
	if s == "" {
		return v, fmt.Errorf("版本号为空")
	}
	core, pre, hasPre := strings.Cut(s, "-")
	if hasPre {
		if pre == "" {
			return v, fmt.Errorf("版本号 %q 的预发布标记为空", s)
		}
		v.pre = strings.Split(pre, ".")
	}
	parts := strings.Split(core, ".")
	if len(parts) > 3 {
		return v, fmt.Errorf("版本号 %q 最多三段数字", s)
	}
	for i, part := range parts {
		n, ok := parseNumeric(part)
		if !ok {
			return semver{}, fmt.Errorf("版本号 %q 的第 %d 段不是数字", s, i+1)
		}
		switch i {
		case 0:
			v.major = n
		case 1:
			v.minor = n
		case 2:
			v.patch = n
		}
	}
	return v, nil
}

// compare 返回 -1、0 或 1。预发布版本小于同号的正规版本。
func (v semver) compare(o semver) int {
	for _, pair := range [][2]int{{v.major, o.major}, {v.minor, o.minor}, {v.patch, o.patch}} {
		if pair[0] != pair[1] {
			return sign(pair[0] - pair[1])
		}
	}
	switch {
	case len(v.pre) == 0 && len(o.pre) == 0:
		return 0
	case len(v.pre) == 0:
		return 1
	case len(o.pre) == 0:
		return -1
	}
	for i := 0; i < len(v.pre) && i < len(o.pre); i++ {
		a, b := v.pre[i], o.pre[i]
		an, aok := parseNumeric(a)
		bn, bok := parseNumeric(b)
		switch {
		case aok && bok:
			if an != bn {
				return sign(an - bn)
			}
		case aok != bok:
			// 数字标识符比字母标识符小（semver 第 11 条）。
			if aok {
				return -1
			}
			return 1
		case a != b:
			if a < b {
				return -1
			}
			return 1
		}
	}
	return sign(len(v.pre) - len(o.pre))
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
}

// String 还原成 1.2.3 形态；缺省的段补 0。
func (v semver) String() string {
	out := fmt.Sprintf("%d.%d.%d", v.major, v.minor, v.patch)
	if len(v.pre) > 0 {
		out += "-" + strings.Join(v.pre, ".")
	}
	return out
}

// comparator 是比较符加一个版本，如 >=1.2.0。
type comparator struct {
	op string
	v  semver
}

// holds 判断版本是否满足这个比较符。
func (c comparator) holds(v semver) bool {
	switch c.op {
	case ">":
		return v.compare(c.v) > 0
	case ">=":
		return v.compare(c.v) >= 0
	case "<":
		return v.compare(c.v) < 0
	case "<=":
		return v.compare(c.v) <= 0
	default: // = 与精确写法
		return v.compare(c.v) == 0
	}
}

// versionRange 是一组范围：外层是「或」，内层是「与」。空表示不限版本。
type versionRange struct {
	groups [][]comparator
	// raw 是原始写法，用于回显与报错。
	raw string
}

// any 判断范围是不是不限版本。
func (r versionRange) any() bool { return len(r.groups) == 0 }

// String 返回原始写法，不限时返回 *。
func (r versionRange) String() string {
	if r.raw == "" {
		return "*"
	}
	return r.raw
}

// matches 判断版本是否落在范围内。
func (r versionRange) matches(v semver) bool {
	if r.any() {
		return true
	}
	for _, group := range r.groups {
		ok := true
		for _, c := range group {
			if !c.holds(v) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// parseRange 解析一个版本范围写法。
func parseRange(raw string) (versionRange, error) {
	r := versionRange{raw: strings.TrimSpace(raw)}
	if r.raw == "" || r.raw == "*" {
		return versionRange{}, nil
	}
	for _, alt := range strings.Split(r.raw, "||") {
		group, err := parseRangeGroup(alt)
		if err != nil {
			return versionRange{}, err
		}
		if len(group) > 0 {
			r.groups = append(r.groups, group)
		}
	}
	if len(r.groups) == 0 {
		return versionRange{}, nil
	}
	return r, nil
}

// parseRangeGroup 解析一组「与」关系的要求：`>=1.0.0 <2.0.0`、`^1.2`、`1.2.*`。
func parseRangeGroup(s string) ([]comparator, error) {
	fields := strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == ',' || r == '\t' })
	var out []comparator
	for _, field := range fields {
		op := "="
		rest := field
		for _, prefix := range []string{">=", "<=", "^", "~", ">", "<", "="} {
			if strings.HasPrefix(field, prefix) {
				op, rest = prefix, strings.TrimPrefix(field, prefix)
				break
			}
		}
		lo, hi, segments, wild, err := parseSegment(rest)
		if err != nil {
			return nil, err
		}
		switch {
		case op == "=" && wild && segments == 0:
			// 光写 * 的那一段不约束任何东西（`*`、`>=1.0.0 *` 里的后者）。
			// ^* 与 ~* 不在此列：那是写错了，下面照旧报错。
			continue
		case op == "^" || op == "~":
			if wild {
				return nil, fmt.Errorf("%s 不能与通配符一起写", op)
			}
			upper := hi
			if upper == nil {
				upper = shorthandUpper(op, lo, segments)
			}
			out = append(out, comparator{">=", lo}, comparator{"<", *upper})
		case op == "=" && (wild || segments < 3):
			// 光写 1.2 或 1.2.* 都是「1.2 那一档」，不是「正好 1.2.0」
			out = append(out, comparator{">=", lo})
			if upper := hi; upper != nil {
				out = append(out, comparator{"<", *upper})
			} else {
				next := hiNext(lo, segments)
				out = append(out, comparator{"<", next})
			}
		default:
			out = append(out, comparator{op, lo})
		}
	}
	return out, nil
}

// shorthandUpper 由 ^ 与 ~ 的下界推出上界。
//
// ^1.2.3 → <2.0.0；^0.2.3 → <0.3.0；^0.0.3 → <0.0.4；~1.2.3 → <1.3.0；~1 → <2.0.0。
func shorthandUpper(op string, lo semver, segments int) *semver {
	if op == "~" {
		if segments == 1 {
			return &semver{major: lo.major + 1}
		}
		return &semver{major: lo.major, minor: lo.minor + 1}
	}
	switch {
	case lo.major > 0:
		return &semver{major: lo.major + 1}
	case lo.minor > 0:
		return &semver{minor: lo.minor + 1}
	case segments >= 3:
		return &semver{patch: lo.patch + 1}
	case segments == 2:
		return &semver{minor: 1}
	default:
		return &semver{major: 1}
	}
}

// parseSegment 解析一段版本写法，返回下界与（可能为 nil 的）上界。
//
// segments 是写了几段（通配符算一段，但不计入下界），wild 表示写了通配符。
// 通配符只能在最后一段：`1.2.*` 可以，`1.*.3` 不是个范围。
func parseSegment(s string) (lo semver, hi *semver, segments int, wild bool, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return lo, nil, 0, false, fmt.Errorf("版本范围里有一个空的版本")
	}
	if s == "*" {
		return semver{}, nil, 0, true, nil
	}
	core, pre, hasPre := strings.Cut(s, "-")
	if hasPre && pre == "" {
		return lo, nil, 0, false, fmt.Errorf("版本 %q 的预发布标记为空", s)
	}
	parts := strings.Split(core, ".")
	if len(parts) > 3 {
		return lo, nil, 0, false, fmt.Errorf("版本 %q 最多三段数字", s)
	}
	segments = len(parts)
	numbers := make([]int, 0, 3)
	for i, part := range parts {
		if isWildcard(part) {
			if hasPre {
				return lo, nil, 0, false, fmt.Errorf("版本 %q 的通配符段不能带预发布标记", s)
			}
			// 通配符只能在最后一段
			for _, rest := range parts[i+1:] {
				if !isWildcard(rest) {
					return lo, nil, 0, false, fmt.Errorf("版本 %q 的通配符后面还有数字段", s)
				}
			}
			lo = fill(numbers)
			next := hiNext(lo, len(numbers))
			return lo, &next, segments, true, nil
		}
		n, ok := parseNumeric(part)
		if !ok {
			return lo, nil, 0, false, fmt.Errorf("版本 %q 的第 %d 段不是数字或通配符", s, i+1)
		}
		numbers = append(numbers, n)
	}
	lo = fill(numbers)
	if hasPre {
		lo.pre = strings.Split(pre, ".")
	}
	return lo, nil, segments, false, nil
}

// fill 把写出来的数字段补成三段。
func fill(numbers []int) semver {
	var v semver
	if len(numbers) > 0 {
		v.major = numbers[0]
	}
	if len(numbers) > 1 {
		v.minor = numbers[1]
	}
	if len(numbers) > 2 {
		v.patch = numbers[2]
	}
	return v
}

// hiNext 由写了的前几段推出上界：1 → 2.0.0，1.2 → 1.3.0，1.2.3 → 1.2.4。
func hiNext(lo semver, segments int) semver {
	switch segments {
	case 0:
		return semver{}
	case 1:
		return semver{major: lo.major + 1}
	case 2:
		return semver{major: lo.major, minor: lo.minor + 1}
	default:
		return semver{major: lo.major, minor: lo.minor, patch: lo.patch + 1}
	}
}
