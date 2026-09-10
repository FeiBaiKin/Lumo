package auth

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"

	"github.com/FeiBaiKin/lumo/internal/auth/perm"
	"github.com/FeiBaiKin/lumo/internal/httpx"
)

// RequirePermission 是 huma 操作级中间件：要求调用者直接持有指定权限之一。
//
// 用法：
//
//	huma.Register(api, huma.Operation{
//		Middlewares: huma.Middlewares{auth.RequirePermission(perm.UsersManage)},
//		...
//	}, handler)
//
// 适用于与所有权无关的权限（如 users:manage）。涉及所有权的检查必须在处理器内
// 用 Principal.Allows 完成，失败时返回 ForbiddenProblem——中间件此时还不知道目标对象归属谁。
func RequirePermission(permissions ...perm.Permission) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		principal, ok := FromContext(ctx.Context())
		if !ok {
			writeProblem(ctx, unauthorizedProblem("需要登录"))
			return
		}
		if !principal.Permissions().HasAny(permissions...) {
			writeProblem(ctx, ForbiddenProblem(permissions...))
			return
		}
		next(ctx)
	}
}

// ForbiddenProblem 构造 403 错误，扩展成员 requiredPermissions 列出所需权限，便于前端提示与排错。
//
// 处理器内的所有权判定失败时直接返回它：*httpx.Problem 实现了 huma.StatusError。
func ForbiddenProblem(required ...perm.Permission) *httpx.Problem {
	names := make([]string, 0, len(required))
	for _, p := range required {
		names = append(names, p.String())
	}
	return &httpx.Problem{
		Type:       "about:blank",
		Status:     http.StatusForbidden,
		Title:      "Forbidden",
		Detail:     "权限不足",
		Extensions: map[string]any{"requiredPermissions": names},
	}
}

// unauthorizedProblem 构造 401 错误。
func unauthorizedProblem(detail string) *httpx.Problem {
	return &httpx.Problem{
		Type:   "about:blank",
		Status: http.StatusUnauthorized,
		Title:  "Unauthorized",
		Detail: detail,
	}
}

// writeProblem 在 huma 中间件里直接写出错误响应并终止链。
func writeProblem(ctx huma.Context, problem *httpx.Problem) {
	r, w := humachi.Unwrap(ctx)
	httpx.WriteProblem(w, r, problem, nil)
}
