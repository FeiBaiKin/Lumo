package lumo

import (
	"encoding/base64"
	"encoding/json"
	"slices"
	"strings"
	"unicode/utf8"
)

// ---------- 接口（spec.routes） ----------

// Request 是插件接口收到的一次请求，地址是 /api/v1/plugins/<插件>/<路径>。
type Request struct {
	Method string
	// Path 是插件前缀之后的路径，如 /hit。
	Path string
	// Params 是路径参数：声明为 /items/{id} 时，Params["id"] 是那一段。
	Params map[string]string
	Query  map[string][]string
	// Headers 是请求头，名字小写；Cookie 与凭据类请求头宿主不转给插件。
	Headers map[string]string
	Body    []byte
	// IP 是访客的地址（按站点的可信代理设置解析过）。
	IP string
	// User 是已登录的调用者；匿名为 nil。公开接口也可能有，后台接口一定有。
	User *Caller
}

// Caller 是调用接口的登录用户。
type Caller struct {
	ID          int64    `json:"id"`
	Username    string   `json:"username"`
	DisplayName string   `json:"displayName"`
	Permissions []string `json:"permissions"`
}

// Can 判断调用者有没有某个权限串，如 posts:write。
func (c *Caller) Can(permission string) bool {
	return c != nil && slices.Contains(c.Permissions, permission)
}

// Header 取一个请求头，名字不分大小写。
func (r *Request) Header(name string) string { return r.Headers[strings.ToLower(name)] }

// QueryValue 取一个查询参数的第一个值。
func (r *Request) QueryValue(name string) string {
	if values := r.Query[name]; len(values) > 0 {
		return values[0]
	}
	return ""
}

// Decode 把 JSON 请求体解到 v 里。
func (r *Request) Decode(v any) error { return json.Unmarshal(r.Body, v) }

// Response 是插件接口的答复。
//
// 能写的响应头只有 Content-Type、Cache-Control、ETag、Last-Modified、Expires、Content-Disposition、
// Content-Language、Vary 与 X- 开头的自定义头；Set-Cookie、CORS 与跳转一律不收。
type Response struct {
	// Status 缺省 200；3xx 不收（插件接口不能做跳转）。
	Status  int
	Headers map[string]string
	Body    []byte
}

// JSON 返回一个 JSON 答复。
func JSON(status int, v any) *Response {
	body, err := json.Marshal(v)
	if err != nil {
		return Problem(500, "结果无法编码成 JSON")
	}
	return &Response{Status: status, Headers: map[string]string{"Content-Type": "application/json"}, Body: body}
}

// Text 返回一个纯文本答复。
func Text(status int, s string) *Response {
	return &Response{Status: status, Headers: map[string]string{"Content-Type": "text/plain; charset=utf-8"}, Body: []byte(s)}
}

// NoContent 返回 204。
func NoContent() *Response { return &Response{Status: 204} }

// Problem 返回一个 RFC 9457 错误答复，与站点自己的接口同一种格式，后台页面能直接显示 detail。
func Problem(status int, detail string) *Response {
	body, _ := json.Marshal(map[string]any{"status": status, "title": statusText[status], "detail": detail})
	return &Response{Status: status, Headers: map[string]string{"Content-Type": "application/problem+json"}, Body: body}
}

// statusText 是常用状态码的标准说明。不引 net/http：它会把 TLS 等一整套带进插件，wasm 平白大好几兆。
var statusText = map[int]string{
	400: "Bad Request", 401: "Unauthorized", 403: "Forbidden", 404: "Not Found", 405: "Method Not Allowed",
	409: "Conflict", 410: "Gone", 413: "Request Entity Too Large", 422: "Unprocessable Entity",
	429: "Too Many Requests", 500: "Internal Server Error", 501: "Not Implemented", 502: "Bad Gateway",
	503: "Service Unavailable", 504: "Gateway Timeout",
}

type routeWire struct {
	Method     string              `json:"method"`
	Path       string              `json:"path"`
	Params     map[string]string   `json:"params"`
	Query      map[string][]string `json:"query"`
	Headers    map[string]string   `json:"headers"`
	Body       string              `json:"body"`
	BodyBase64 string              `json:"bodyBase64"`
	IP         string              `json:"ip"`
	User       *Caller             `json:"user"`
}

type responseWire struct {
	Status     int               `json:"status"`
	Headers    map[string]string `json:"headers,omitempty"`
	Body       string            `json:"body,omitempty"`
	BodyBase64 string            `json:"bodyBase64,omitempty"`
}

// OnRoute 登记一个接口，名字对应 plugin.yaml 里 spec.routes 的 name，方法、路径与谁能调也写在那里。
//
// 处理函数返回 error 时调用方收到 500（细节只进日志）；要告诉调用方哪里不对，返回 Problem(400, "…")。
// 已登录访客的写请求（POST、PUT、PATCH、DELETE）要带 X-CSRF-Token 请求头（值取 Cookie lumo_csrf，
// HTTPS 下是 __Host-lumo_csrf）：后台接口不带就是 403；公开接口不带也照常处理，只是当作匿名，User 为 nil。
// 单次时限 10 秒；公开接口每个访客每分钟 120 次，请求体最大 64 KiB。
func OnRoute(name string, fn func(ctx *Context, req *Request) (*Response, error)) {
	register("route", name, func(ctx *Context, payload json.RawMessage) (any, error) {
		var in routeWire
		if err := json.Unmarshal(payload, &in); err != nil {
			return nil, err
		}
		req := &Request{
			Method: in.Method, Path: in.Path, Params: in.Params, Query: in.Query,
			Headers: in.Headers, Body: []byte(in.Body), IP: in.IP, User: in.User,
		}
		if in.BodyBase64 != "" {
			body, err := base64.StdEncoding.DecodeString(in.BodyBase64)
			if err != nil {
				return nil, err
			}
			req.Body = body
		}
		resp, err := fn(ctx, req)
		if err != nil {
			return nil, err
		}
		if resp == nil {
			resp = NoContent()
		}
		out := responseWire{Status: resp.Status, Headers: resp.Headers}
		if utf8.Valid(resp.Body) {
			out.Body = string(resp.Body)
		} else {
			out.BodyBase64 = base64.StdEncoding.EncodeToString(resp.Body)
		}
		return out, nil
	})
}
