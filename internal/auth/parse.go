package auth

import (
	"fmt"
	"strings"

	"github.com/FeiBaiKin/lumo/internal/auth/perm"
)

// parseScopes 把字符串列表校验为权限串。
//
// 空列表返回 nil。它只负责「串是否合法」，不解释空值的语义——
// 空值该怎么处理由 resolveScopes 决定。
func parseScopes(raw []string) ([]perm.Permission, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	scopes := make([]perm.Permission, 0, len(raw))
	for _, s := range raw {
		if strings.TrimSpace(s) == "" {
			continue
		}
		p, err := perm.Parse(s)
		if err != nil {
			return nil, err
		}
		scopes = append(scopes, p)
	}
	return scopes, nil
}

// resolveScopes 把请求中的 scope 列表解析为**落库**的权限集合。
//
// granted 是调用者本次调用的有效权限。两条规则：
//
//  1. 省略 scope（nil 或空）表示「继承账号当前的全部权限」，但必须在此**显式展开**
//     成权限清单再落库。空 scopes 在鉴权时不再有特殊含义（见 NewTokenPrincipal），
//     否则一行被改空的 scopes 就等于账号无限权限。
//  2. 显式给出的 scope 必须落在 granted 之内，超出即报错而不是静默忽略——
//     静默忽略会让人以为令牌拿到了它其实没有的权限。
func resolveScopes(raw []string, granted perm.Set) ([]perm.Permission, error) {
	scopes, err := parseScopes(raw)
	if err != nil {
		return nil, err
	}
	if len(scopes) == 0 {
		return granted.List(), nil
	}

	out := make([]perm.Permission, 0, len(scopes))
	seen := make(perm.Set, len(scopes))
	for _, scope := range scopes {
		if !granted.Has(scope) {
			return nil, fmt.Errorf("权限串 %q 超出你当前的权限范围", scope)
		}
		if seen.Has(scope) {
			continue
		}
		seen.Add(scope)
		out = append(out, scope)
	}
	return out, nil
}
