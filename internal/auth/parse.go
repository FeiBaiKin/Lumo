package auth

import (
	"strconv"
	"strings"

	"github.com/FeiBaiKin/lumo/internal/auth/perm"
)

// parseScopes 把字符串列表校验为权限串。
//
// 空列表返回 nil，语义为「继承用户全部权限」。
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

// parseInt64 解析路径参数中的整数 ID。
func parseInt64(raw string) (int64, error) {
	return strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
}
