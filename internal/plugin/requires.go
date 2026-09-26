package plugin

import (
	"errors"
	"fmt"
	"strings"
)

// ErrIncompatible 表示插件要的 Lumo 版本（spec.requires）与本站运行的对不上。
var ErrIncompatible = errors.New("插件不支持本站的 Lumo 版本")

// runningVersion 把本站 Lumo 的版本号解析成可比对的形态；比不了时 ok 为假，调用方就不比。
//
// 源码直接构建的 0.0.0-dev 不比，否则本地开发时一切声明了 requires 的插件都启用不了。
// 发布版的 tag 可能带 v 前缀；task build 注入的是 git describe（v0.2.0-3-gab12cd-dirty），
// 它其实比 0.2.0 新，按预发布去比反而会判成更旧——所以一律只取主次修订号。
func runningVersion(raw string) (semver, bool) {
	raw = strings.TrimPrefix(strings.TrimSpace(raw), "v")
	raw, _, _ = strings.Cut(raw, "+")
	core, _, _ := strings.Cut(raw, "-")
	v, err := parseVersion(core)
	if err != nil || (v.major == 0 && v.minor == 0 && v.patch == 0) {
		return semver{}, false
	}
	return v, true
}

// checkRequires 核对插件要的 Lumo 版本；本站版本比不了（源码构建）时放行。
func (r *Registry) checkRequires(m *Manifest) error {
	have, ok := runningVersion(r.lumoVersion)
	if !ok {
		return nil
	}
	need, err := parseRange(m.Spec.Requires)
	if err != nil {
		return fmt.Errorf("%w：spec.requires %q 不是有效的版本范围：%w", ErrInvalidPackage, m.Spec.Requires, err)
	}
	if need.matches(have) {
		return nil
	}
	return fmt.Errorf("%w：它要 Lumo %s，本站是 %s", ErrIncompatible, need, have)
}
