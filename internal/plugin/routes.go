package plugin

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"github.com/FeiBaiKin/lumo/internal/auth"
	"github.com/FeiBaiKin/lumo/internal/auth/perm"
	"github.com/FeiBaiKin/lumo/internal/httpx"
	"github.com/FeiBaiKin/lumo/internal/plugin/wasm"
	"github.com/FeiBaiKin/lumo/internal/ratelimit"
)

// RoutesPrefix 是插件接口的前缀：/api/v1/plugins/<插件>/<路径>。
//
// 公开接口与后台接口共用这一个前缀，由每条接口自己声明谁能调：后台的 /api/v1/console/plugins/<插件>/
// 已经是插件管理接口（启停、设置、资源），插件的路径放进去迟早撞名。
const RoutesPrefix = "/api/v1/plugins"

// Route 是 plugin.yaml 里 spec.routes 的一项：插件自己的一个 HTTP 接口。
type Route struct {
	// Name 是处理函数名，对应 SDK 里 OnRoute 登记的名字。
	Name string `yaml:"name" json:"name"`
	// Method 是请求方法，缺省 GET。
	Method string `yaml:"method" json:"method"`
	// Path 是插件前缀之后的路径，如 /hit、/items/{id}。
	Path string `yaml:"path" json:"path"`
	// Public 为真时谁都能调；否则要登录并持有 Permission。
	Public bool `yaml:"public" json:"public"`
	// Permission 是非公开接口要的权限串，缺省 plugins:manage。
	Permission  string `yaml:"permission" json:"permission"`
	Description string `yaml:"description" json:"description"`

	segments   []string
	permission perm.Permission
}

const (
	maxRoutes       = 50
	maxRouteDepth   = 8
	routeTimeout    = 10 * time.Second
	maxPublicBody   = 64 << 10
	maxConsoleBody  = 1 << 20
	maxRouteHeaders = 30
	// publicPerMinute 是每个访客 IP 每分钟能调一个插件公开接口的次数。
	publicPerMinute = 120
	// consolePerMinute 是每个登录用户每分钟能调一个插件后台接口的次数。
	consolePerMinute = 600
)

var (
	routeSegment = regexp.MustCompile(`^[a-z0-9][-a-z0-9_.]*$`)
	routeParam   = regexp.MustCompile(`^\{[a-z][a-zA-Z0-9]*\}$`)
	routeMethods = map[string]bool{
		http.MethodGet: true, http.MethodPost: true, http.MethodPut: true,
		http.MethodPatch: true, http.MethodDelete: true,
	}
)

// splitPath 把路径拆成段；根路径是零段。
func splitPath(p string) []string {
	p = strings.Trim(p, "/")
	if p == "" {
		return nil
	}
	return strings.Split(p, "/")
}

// normalizeRoutes 校验 spec.routes：名字、方法、路径形态，同一方法下不能有两条形态相同的路径。
func normalizeRoutes(routes []Route, hasBackend bool) error {
	if len(routes) == 0 {
		return nil
	}
	if !hasBackend {
		return fmt.Errorf("%w：接口要由后端代码处理，请同时声明 spec.runtime: %s", ErrInvalidPackage, RuntimeWasm)
	}
	if len(routes) > maxRoutes {
		return fmt.Errorf("%w：接口最多 %d 个", ErrInvalidPackage, maxRoutes)
	}
	names := map[string]bool{}
	shapes := map[string]bool{}
	for i := range routes {
		rt := &routes[i]
		if !cronNamePattern.MatchString(rt.Name) || len(rt.Name) > 64 {
			return fmt.Errorf("%w：接口名 %q 只能用小写字母、数字与连字符", ErrInvalidPackage, rt.Name)
		}
		if names[rt.Name] {
			return fmt.Errorf("%w：接口 %q 重复", ErrInvalidPackage, rt.Name)
		}
		names[rt.Name] = true
		rt.Method = strings.ToUpper(strings.TrimSpace(rt.Method))
		if rt.Method == "" {
			rt.Method = http.MethodGet
		}
		if !routeMethods[rt.Method] {
			return fmt.Errorf("%w：接口 %s 的 method 只能是 GET、POST、PUT、PATCH 或 DELETE", ErrInvalidPackage, rt.Name)
		}
		if !strings.HasPrefix(rt.Path, "/") {
			return fmt.Errorf("%w：接口 %s 的 path 须以 / 开头", ErrInvalidPackage, rt.Name)
		}
		rt.segments = splitPath(rt.Path)
		if len(rt.segments) > maxRouteDepth {
			return fmt.Errorf("%w：接口 %s 的路径最多 %d 段", ErrInvalidPackage, rt.Name, maxRouteDepth)
		}
		shape := make([]string, len(rt.segments))
		params := map[string]bool{}
		for j, seg := range rt.segments {
			switch {
			case routeParam.MatchString(seg):
				if params[seg] {
					return fmt.Errorf("%w：接口 %s 的路径参数 %s 重复", ErrInvalidPackage, rt.Name, seg)
				}
				params[seg] = true
				shape[j] = "{}"
			case routeSegment.MatchString(seg):
				shape[j] = seg
			default:
				return fmt.Errorf("%w：接口 %s 的路径 %q 只能用小写字母、数字、- _ . 与 {参数}", ErrInvalidPackage, rt.Name, rt.Path)
			}
		}
		key := rt.Method + " /" + strings.Join(shape, "/")
		if shapes[key] {
			return fmt.Errorf("%w：接口 %s 与另一条接口的方法和路径相同", ErrInvalidPackage, rt.Name)
		}
		shapes[key] = true
		rt.permission = perm.PluginsManage
		if rt.Public {
			if rt.Permission != "" {
				return fmt.Errorf("%w：接口 %s 是公开的，不能再写 permission", ErrInvalidPackage, rt.Name)
			}
		} else if rt.Permission != "" {
			p, err := perm.Parse(rt.Permission)
			if err != nil {
				return fmt.Errorf("%w：接口 %s 的 permission %q 不是有效的权限串", ErrInvalidPackage, rt.Name, rt.Permission)
			}
			rt.permission = p
		}
	}
	return nil
}

// match 判断一条接口是否匹配请求路径，返回路径参数与静态段数（越多越具体）。
func (rt *Route) match(segments []string) (params map[string]string, static int, ok bool) {
	if len(segments) != len(rt.segments) {
		return nil, 0, false
	}
	params = map[string]string{}
	for i, seg := range rt.segments {
		if strings.HasPrefix(seg, "{") {
			if segments[i] == "" {
				return nil, 0, false
			}
			params[seg[1:len(seg)-1]] = segments[i]
			continue
		}
		if seg != segments[i] {
			return nil, 0, false
		}
		static++
	}
	return params, static, true
}

// matchRoute 找出匹配的接口：路径相同时静态段多的优先；路径对得上但方法不对时返回允许的方法。
func matchRoute(routes []Route, method, path string) (route *Route, params map[string]string, allow []string) {
	segments := splitPath(path)
	best := -1
	for i := range routes {
		rt := &routes[i]
		got, static, ok := rt.match(segments)
		if !ok {
			continue
		}
		if rt.Method != method {
			allow = append(allow, rt.Method)
			continue
		}
		if static > best {
			route, params, best = rt, got, static
		}
	}
	return route, params, allow
}

// routeRequest 是发给插件的一次接口请求。
type routeRequest struct {
	Method string              `json:"method"`
	Path   string              `json:"path"`
	Params map[string]string   `json:"params"`
	Query  map[string][]string `json:"query"`
	// Headers 是请求头，名字小写；Cookie 与凭据类请求头不转给插件。
	Headers map[string]string `json:"headers"`
	// Body 是 UTF-8 文本请求体；不是合法 UTF-8 时放在 BodyBase64 里。
	Body       string `json:"body,omitempty"`
	BodyBase64 string `json:"bodyBase64,omitempty"`
	IP         string `json:"ip"`
	// User 是已登录的调用者；匿名为 nil。
	User *routeUser `json:"user,omitempty"`
}

type routeUser struct {
	ID          int64    `json:"id"`
	Username    string   `json:"username"`
	DisplayName string   `json:"displayName"`
	Permissions []string `json:"permissions"`
}

// routeResponse 是插件的答复。
type routeResponse struct {
	Status     int               `json:"status"`
	Headers    map[string]string `json:"headers"`
	Body       string            `json:"body"`
	BodyBase64 string            `json:"bodyBase64"`
}

// hiddenRequestHeaders 是不转给插件的请求头：拿到它们就能冒充当前用户。
var hiddenRequestHeaders = map[string]bool{
	"cookie": true, "authorization": true, "proxy-authorization": true, "x-csrf-token": true,
}

// allowedResponseHeaders 是插件能写的响应头。Set-Cookie、CORS 与安全策略一类的一律不给：
// 插件接口与站点同源，写得了 Cookie 就能动会话，放得开 CORS 就能让别的站读用户的数据。
var allowedResponseHeaders = map[string]bool{
	"content-type": true, "cache-control": true, "etag": true, "last-modified": true,
	"expires": true, "content-disposition": true, "content-language": true, "vary": true,
}

// serveRoute 处理 /api/v1/plugins/<插件>/<路径> 上的请求。
func (m *Module) serveRoute(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "plugin")
	loaded, ok := m.registry.Get(name)
	if !ok || !loaded.Enabled {
		httpx.Error(w, r, http.StatusNotFound, "插件不存在或没有启用")
		return
	}
	path := "/" + chi.URLParam(r, "*")
	route, params, allow := matchRoute(loaded.Manifest.Spec.Routes, r.Method, path)
	if route == nil {
		if len(allow) > 0 {
			w.Header().Set("Allow", strings.Join(allow, ", "))
			httpx.Error(w, r, http.StatusMethodNotAllowed, "这个接口不支持 "+r.Method)
			return
		}
		httpx.Error(w, r, http.StatusNotFound, "插件没有这个接口")
		return
	}

	principal, _ := auth.FromContext(r.Context())
	if principal != nil && !csrfPassed(r, principal) {
		if !route.Public {
			httpx.Error(w, r, http.StatusForbidden, "缺少或不匹配的 "+auth.CSRFHeaderName+" 请求头")
			return
		}
		// 公开接口不拒绝：没带令牌就当匿名访客，插件看不到是谁。
		// 访问统计一类的脚本因此不必管令牌；要认人的写接口，前台脚本带上令牌即可
		principal = nil
	}
	ip := httpx.ClientIPFrom(r)
	limitKey, perMinute, maxBody := "plugin.route:"+name+":"+ip, publicPerMinute, int64(maxPublicBody)
	if !route.Public {
		if principal == nil || principal.User == nil {
			httpx.Unauthorized(w, r, "请先登录")
			return
		}
		if !principal.Has(route.permission) {
			httpx.Error(w, r, http.StatusForbidden, "没有权限调用这个接口，需要 "+route.permission.String())
			return
		}
		limitKey = "plugin.route:" + name + ":u" + strconv.FormatInt(principal.UserID(), 10)
		perMinute, maxBody = consolePerMinute, maxConsoleBody
	}
	if m.db != nil {
		count, err := ratelimit.New(m.db).Hit(r.Context(), limitKey, time.Minute)
		if err != nil {
			m.logger.Warn("插件接口限流计数失败", slog.Any("error", err))
		} else if count > perMinute {
			w.Header().Set("Retry-After", "60")
			httpx.Error(w, r, http.StatusTooManyRequests, "请求太频繁，请稍后再试")
			return
		}
	}

	req, err := buildRouteRequest(r, path, params, ip, maxBody)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			httpx.Error(w, r, http.StatusRequestEntityTooLarge, fmt.Sprintf("请求体不能超过 %d KiB", maxBody>>10))
			return
		}
		httpx.BadRequest(w, r, "读取请求体失败")
		return
	}
	if principal != nil && principal.User != nil {
		user := principal.User
		req.User = &routeUser{ID: user.ID, Username: user.Username, DisplayName: user.DisplayName, Permissions: []string{}}
		for _, p := range principal.Permissions().List() {
			req.User.Permissions = append(req.User.Permissions, p.String())
		}
	}

	out, err := m.Invoke(r.Context(), name, wasm.Request{Type: kindRoute, Name: route.Name, Payload: req}, routeTimeout)
	if err != nil {
		m.writeRouteError(w, r, name, route.Name, err)
		return
	}
	var resp routeResponse
	if len(out) > 0 && string(out) != "null" {
		if err := json.Unmarshal(out, &resp); err != nil {
			m.writeRouteError(w, r, name, route.Name, fmt.Errorf("答复不是合法的接口响应: %w", err))
			return
		}
	}
	writeRouteResponse(w, r, &resp)
}

// csrfPassed 判断一次请求能不能按调用者的身份处理：安全方法与令牌调用不需要 CSRF 令牌，
// 会话调用的写请求要带上与会话绑定的那一枚（同各平面的双提交校验）。
func csrfPassed(r *http.Request, principal *auth.Principal) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	if principal.Method != auth.MethodSession {
		return true
	}
	provided := r.Header.Get(auth.CSRFHeaderName)
	return principal.Session != nil && provided != "" && auth.ConstantTimeEqual(provided, principal.Session.CSRFToken)
}

// buildRouteRequest 把 HTTP 请求整理成发给插件的数据。
func buildRouteRequest(r *http.Request, path string, params map[string]string, ip string, maxBody int64) (*routeRequest, error) {
	req := &routeRequest{
		Method: r.Method, Path: path, Params: params, Query: r.URL.Query(),
		Headers: map[string]string{}, IP: ip,
	}
	for key := range r.Header {
		lower := strings.ToLower(key)
		if hiddenRequestHeaders[lower] || len(req.Headers) >= maxRouteHeaders {
			continue
		}
		req.Headers[lower] = r.Header.Get(key)
	}
	if r.Body == nil {
		return req, nil
	}
	body, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, maxBody))
	if err != nil {
		return nil, err
	}
	if utf8.Valid(body) {
		req.Body = string(body)
	} else {
		req.BodyBase64 = base64.StdEncoding.EncodeToString(body)
	}
	return req, nil
}

// writeRouteError 把调用失败翻成状态码；细节只进日志，不回给调用方。
func (m *Module) writeRouteError(w http.ResponseWriter, r *http.Request, plugin, route string, err error) {
	status, detail := http.StatusInternalServerError, "插件处理请求时出错"
	switch {
	case errors.Is(err, wasm.ErrClosed):
		status, detail = http.StatusNotFound, "插件不存在或没有启用"
	case errors.Is(err, wasm.ErrBusy):
		status, detail = http.StatusServiceUnavailable, "插件正忙，请稍后再试"
	case errors.Is(err, wasm.ErrTimeout):
		status, detail = http.StatusGatewayTimeout, "插件处理请求超时"
	}
	if !wasm.IsCrash(err) {
		m.logger.Info("插件接口没有成功", slog.String("plugin", plugin), slog.String("route", route), slog.Any("error", err))
	}
	httpx.Error(w, r, status, detail)
}

// writeRouteResponse 写出插件的答复，并加上同源内容必须有的安全头。
func writeRouteResponse(w http.ResponseWriter, r *http.Request, resp *routeResponse) {
	body := []byte(resp.Body)
	if resp.BodyBase64 != "" {
		decoded, err := base64.StdEncoding.DecodeString(resp.BodyBase64)
		if err != nil {
			httpx.Error(w, r, http.StatusInternalServerError, "插件处理请求时出错")
			return
		}
		body = decoded
	}
	header := w.Header()
	for key, value := range resp.Headers {
		lower := strings.ToLower(key)
		if allowedResponseHeaders[lower] || (strings.HasPrefix(lower, "x-") && !strings.HasPrefix(lower, "x-frame") &&
			lower != "x-content-type-options") {
			header.Set(key, value)
		}
	}
	if header.Get("Content-Type") == "" {
		header.Set("Content-Type", http.DetectContentType(body))
	}
	if header.Get("Cache-Control") == "" {
		header.Set("Cache-Control", "no-store")
	}
	// 响应与站点同源：哪怕插件回了一段 HTML，浏览器直接打开时也跑不了脚本、拿不到 Cookie
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	status := resp.Status
	if status == 0 {
		status = http.StatusOK
	}
	if status < 200 || status > 599 || (status >= 300 && status < 400) {
		status = http.StatusInternalServerError
	}
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}
