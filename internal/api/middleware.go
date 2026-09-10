package api

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
)

// HTTPMiddleware 把标准 net/http 中间件适配为 huma 中间件。
//
// 阶段 2 已测试过的鉴权、CSRF 等中间件因此可以原样复用。中间件内部写出的响应
// （如 401）直接送达客户端，huma 链随之终止；中间件放行时，它写入 context 的值
// （如 Principal）随请求带回 huma 上下文，处理器经 ctx.Context() 可见。
//
// 限制：中间件不得替换 ResponseWriter（如包一层记录字节数的封装），
// 因为后续写响应仍走 huma 持有的原始 writer。
func HTTPMiddleware(mw Middleware) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		r, w := humachi.Unwrap(ctx)
		mw(http.HandlerFunc(func(_ http.ResponseWriter, inner *http.Request) {
			next(huma.WithContext(ctx, inner.Context()))
		})).ServeHTTP(w, r)
	}
}
