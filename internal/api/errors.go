package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/danielgtaylor/huma/v2"

	"github.com/FeiBaiKin/lumo/internal/httpx"
)

// errorLogger 记录 5xx 的内部错误。huma.NewError 本身是包级变量，这里同样只能是进程级。
var errorLogger atomic.Pointer[slog.Logger]

// SetErrorLogger 设置记录 5xx 内部错误的日志器。
func SetErrorLogger(logger *slog.Logger) {
	errorLogger.Store(logger)
}

// 把 huma 的错误构造函数替换为 httpx.Problem：全站只有一种错误模型。
// huma 会用 NewError 的返回类型反射出 OpenAPI 里的错误响应 schema。
func init() {
	huma.NewError = newProblem
	huma.NewErrorWithContext = newProblemWithContext
}

// newProblem 由状态码、说明与明细构造 Problem。
//
// 请求体位置的错误不回显原始值：huma 会把出错位置的值原样放进 value，
// 对象级错误（如未知字段）时那就是整个请求体，登录接口会因此把密码回显给客户端。
// 路径、查询串与请求头的值本就出现在 URL 或头里，保留以便排查。
func newProblem(status int, detail string, errs ...error) huma.StatusError {
	problem := &httpx.Problem{
		Type:   "about:blank",
		Title:  http.StatusText(status),
		Status: status,
		Detail: detail,
	}
	for _, err := range errs {
		if err == nil {
			continue
		}
		var detailer huma.ErrorDetailer
		if errors.As(err, &detailer) {
			d := detailer.ErrorDetail()
			item := httpx.ErrorDetail{Message: d.Message, Location: d.Location, Value: d.Value}
			if strings.HasPrefix(d.Location, "body") {
				item.Value = nil
			}
			problem.Errors = append(problem.Errors, item)
			continue
		}
		problem.Errors = append(problem.Errors, httpx.ErrorDetail{Message: err.Error()})
	}
	return problem
}

// newProblemWithContext 是 huma 在请求处理链内构造错误的入口。
//
// 5xx 一律不回传内部细节：错误只记入日志，响应体仅含状态与标题。
// 处理器返回的普通 error 会走到这里并成为 500，因此模块不必担心 DB 错误文本外泄。
func newProblemWithContext(ctx huma.Context, status int, detail string, errs ...error) huma.StatusError {
	if status < http.StatusInternalServerError {
		return newProblem(status, detail, errs...)
	}
	if logger := errorLogger.Load(); logger != nil {
		attrs := []any{
			slog.Int("status", status),
			slog.String("method", ctx.Method()),
			slog.String("path", ctx.URL().Path),
		}
		if op := ctx.Operation(); op != nil {
			attrs = append(attrs, slog.String("operation", op.OperationID))
		}
		attrs = append(attrs, slog.Any("error", errors.Join(errs...)))
		logger.Error("请求处理失败", attrs...)
	}
	return newProblem(status, "")
}
