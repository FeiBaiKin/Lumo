package extension

import (
	"fmt"
	"regexp"
	"strings"
)

// 核心自用的分组与版本（agent.md §6.1）。插件另起分组，版本沿用同一套形态。
const (
	// GroupLumo 是核心与默认主题使用的 API 分组。
	GroupLumo = "io.github.feibaiikin.lumo"
	// VersionAlpha 是 v1 统一使用的版本；语义稳定后再升 v1beta1 / v1。
	VersionAlpha = "v1alpha1"
)

// 命名形态（agent.md §6.1）。这些模式同时写进 OpenAPI 的参数声明，
// 形态不合法的地址在进入处理器之前就被 huma 拦下。
const (
	// GroupPattern 是反向域名：至少两段，段间以点分隔。
	GroupPattern = `^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)+$`
	// VersionPattern 覆盖 v1alpha1 / v1beta2 / v1 三种形态。
	VersionPattern = `^v[1-9]\d*((alpha|beta)[1-9]\d*)?$`
	// ResourcePattern 是 URL 里的复数段：由 kind 小写后加复数后缀而来，故不含连字符。
	ResourcePattern = `^[a-z][a-z0-9]*$`
	// KindPattern 是单数 PascalCase。
	KindPattern = `^[A-Z][A-Za-z0-9]*$`
	// NamePattern 是 DNS-1123，与核心迁移里的 extensions_name_dns1123 约束一致。
	NamePattern = `^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
)

// 长度上限。group 与 name 取 DNS 名上限，kind 取一个标识符够用的长度。
const (
	MaxGroupLen    = 253
	MaxVersionLen  = 32
	MaxKindLen     = 63
	MaxResourceLen = 64
	MaxNameLen     = 253
)

var (
	groupRe    = regexp.MustCompile(GroupPattern)
	versionRe  = regexp.MustCompile(VersionPattern)
	resourceRe = regexp.MustCompile(ResourcePattern)
	kindRe     = regexp.MustCompile(KindPattern)
	nameRe     = regexp.MustCompile(NamePattern)
)

// Resource 把 kind 映射成 URL 里的复数段。
//
// 规则是「小写后加复数后缀」，与英语语法只是近似：URL 段是寻址用的机器标识，
// 可预测比读起来地道更重要：规则只看后缀，不认英语里的不规则复数。
//
// 映射**不可逆**——Movie 与 Movy 都得到 movies。因此反向寻址一律靠数据库里存下的
// resource 列，而不是在 Go 侧从复数段猜单数形；kind 也因此必须由创建方显式给出。
func Resource(kind string) string {
	s := strings.ToLower(kind)
	switch {
	case hasAnySuffix(s, "s", "x", "z", "ch", "sh"):
		return s + "es"
	case len(s) >= 2 && strings.HasSuffix(s, "y") && !isVowel(s[len(s)-2]):
		return s[:len(s)-1] + "ies"
	default:
		return s + "s"
	}
}

// ValidateGroup 校验 API 分组形态。
func ValidateGroup(group string) error {
	switch {
	case group == "":
		return fmt.Errorf("%w：apiGroup 不能为空", ErrInvalid)
	case len(group) > MaxGroupLen:
		return fmt.Errorf("%w：apiGroup 长度超过 %d", ErrInvalid, MaxGroupLen)
	case !groupRe.MatchString(group):
		return fmt.Errorf("%w：apiGroup %q 须为反向域名，如 %s", ErrInvalid, group, GroupLumo)
	}
	return nil
}

// ValidateVersion 校验 API 版本形态。
func ValidateVersion(version string) error {
	switch {
	case version == "":
		return fmt.Errorf("%w：version 不能为空", ErrInvalid)
	case len(version) > MaxVersionLen:
		return fmt.Errorf("%w：version 长度超过 %d", ErrInvalid, MaxVersionLen)
	case !versionRe.MatchString(version):
		return fmt.Errorf("%w：version %q 须形如 %s", ErrInvalid, version, VersionAlpha)
	}
	return nil
}

// ValidateResource 校验 URL 里的复数段形态。
func ValidateResource(resource string) error {
	switch {
	case resource == "":
		return fmt.Errorf("%w：资源段不能为空", ErrInvalid)
	case len(resource) > MaxResourceLen:
		return fmt.Errorf("%w：资源段长度超过 %d", ErrInvalid, MaxResourceLen)
	case !resourceRe.MatchString(resource):
		return fmt.Errorf("%w：资源段 %q 须为小写字母与数字", ErrInvalid, resource)
	}
	return nil
}

// ValidateKind 校验 kind 形态。
func ValidateKind(kind string) error {
	switch {
	case kind == "":
		return fmt.Errorf("%w：kind 不能为空", ErrInvalid)
	case len(kind) > MaxKindLen:
		return fmt.Errorf("%w：kind 长度超过 %d", ErrInvalid, MaxKindLen)
	case !kindRe.MatchString(kind):
		return fmt.Errorf("%w：kind %q 须为单数 PascalCase，如 Post", ErrInvalid, kind)
	}
	return nil
}

// ValidateName 校验记录名形态。
func ValidateName(name string) error {
	switch {
	case name == "":
		return fmt.Errorf("%w：name 不能为空", ErrInvalid)
	case len(name) > MaxNameLen:
		return fmt.Errorf("%w：name 长度超过 %d", ErrInvalid, MaxNameLen)
	case !nameRe.MatchString(name):
		return fmt.Errorf("%w：name %q 须为 DNS-1123（小写字母、数字与连字符，不以连字符起止）", ErrInvalid, name)
	}
	return nil
}

// hasAnySuffix 报告 s 是否以任一后缀结尾。
func hasAnySuffix(s string, suffixes ...string) bool {
	for _, suffix := range suffixes {
		if strings.HasSuffix(s, suffix) {
			return true
		}
	}
	return false
}

// isVowel 判断一个 ASCII 字母是否元音，供复数规则区分 category 与 key。
func isVowel(c byte) bool {
	return strings.IndexByte("aeiou", c) >= 0
}
