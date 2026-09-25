package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/FeiBaiKin/lumo/internal/auth"
)

// rawResponse 是读完并关掉了的响应。
type rawResponse struct {
	StatusCode int
	Header     http.Header
}

// raw 发一个请求，返回状态码、响应头与响应体：要看响应头时用。
func (c *client) raw(method, path string, body []byte, headers map[string]string) (resp rawResponse, data []byte) {
	c.t.Helper()
	req, _ := http.NewRequestWithContext(context.Background(), method, c.base+path, bytes.NewReader(body))
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	res, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	data, _ = io.ReadAll(res.Body)
	return rawResponse{StatusCode: res.StatusCode, Header: res.Header}, data
}

// frontendManifest 在测试插件的清单上再加样式、脚本与后台页面。
var frontendManifest = strings.Replace(guestManifest, "  frontend:\n",
	"  frontend:\n    styles: [static/guest.css]\n    scripts: [static/guest.js]\n", 1) +
	"  pages:\n    - {path: stats, label: 统计, file: static/page.html}\n"

var frontendFiles = map[string][]byte{
	"static/guest.css": []byte(".guest{color:red}"),
	"static/guest.js":  []byte("console.log('guest')"),
	"static/page.html": []byte("<!doctype html><title>统计</title><p>页面</p>"),
	"static/evil.svg":  []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`),
}

// 插件的前台：样式脚本进页头页尾、插槽与短代码的输出被净化、侧栏小组件、静态文件的隔离头；停用后全部消失。
func TestPluginFrontend(t *testing.T) {
	s := newTestSite(t)
	s.createUser(t, "admin", "super-admin")
	admin := s.client(t)
	mustStatus(t, "管理员登录", admin.login("admin", "password-admin"), http.StatusOK, nil)
	installProbeFiles(t, admin, frontendManifest, frontendFiles)

	status, out := admin.do(http.MethodPost, "/api/v1/console/posts", map[string]any{
		"title": "前台探针", "slug": "frontend-probe", "rawType": "html",
		"raw": `<p>[hello name="小明"]</p><pre><code>[hello]</code></pre><p>[[hello]]</p><p>[unknown x=1]</p>`,
	})
	mustStatus(t, "发文", status, http.StatusCreated, out)
	postID, _ := out["id"].(float64)
	status, out = admin.do(http.MethodPost, "/api/v1/console/posts/"+jsonID(postID)+"/publish", map[string]any{})
	mustStatus(t, "发布", status, http.StatusOK, out)

	status, out = admin.do(http.MethodPut, "/api/v1/console/themes/ink/settings/sidebar", map[string]any{
		"showOnList": true, "showOnPost": true, "sticky": false,
		"widgets": []any{map[string]any{"type": "plugin", "scope": "all", "widget": "hook-probe/counter"}},
	})
	mustStatus(t, "侧栏放插件小组件", status, http.StatusOK, out)

	t.Run("页面里有插件的样式、脚本、插槽、小组件与短代码，夹带的脚本都被净化", func(t *testing.T) {
		status, html := s.client(t).page("/posts/frontend-probe")
		if status != http.StatusOK {
			t.Fatalf("文章页状态码 %d", status)
		}
		for _, want := range []string{
			`<link rel="stylesheet" href="/plugin-assets/hook-probe/guest.css?v=1.0.0" data-plugin="hook-probe">`,
			`<script defer src="/plugin-assets/hook-probe/guest.js?v=1.0.0" data-plugin="hook-probe" data-api="/api/v1/plugins/hook-probe" data-post="`,
			`<meta name="guest-head" content="1">`,
			`data-plugin="hook-probe" data-slot="content.after"><p class="guest-slot">插槽 post</p>`,
			`<button type="button">按钮</button>`,
			`<strong class="guest-sc">你好，小明</strong>`,
			`<span class="cl">[hello]</span>`,
			`<p>[hello]</p>`,
			`<p>[unknown x=1]</p>`,
			`<p class="guest-widget">访问 42</p>`,
			`访问计数`,
		} {
			if !strings.Contains(html, want) {
				t.Errorf("页面里缺少 %s", want)
			}
		}
		for _, bad := range []string{"alert(1)", "onclick", "onerror", "[hello name", "[hello name=&#34;"} {
			if strings.Contains(html, bad) {
				t.Errorf("插件输出里的 %s 没有被净化掉", bad)
			}
		}
	})

	t.Run("主题设置能列出插件小组件", func(t *testing.T) {
		status, out := admin.do(http.MethodGet, "/api/v1/console/plugin-widgets", nil)
		mustStatus(t, "插件小组件", status, http.StatusOK, out)
		items, _ := out["items"].([]any)
		if len(items) != 1 || items[0].(map[string]any)["id"] != "hook-probe/counter" {
			t.Fatalf("应列出 hook-probe/counter：%v", out)
		}
	})

	t.Run("静态文件：HTML 与 SVG 带隔离头，越界与未声明的读不到", func(t *testing.T) {
		anon := s.client(t)
		resp, _ := anon.raw(http.MethodGet, "/plugin-assets/hook-probe/guest.js", nil, nil)
		if resp.StatusCode != http.StatusOK || resp.Header.Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("脚本应可读且带 nosniff：%d %v", resp.StatusCode, resp.Header)
		}
		for _, file := range []string{"page.html", "evil.svg"} {
			resp, _ = anon.raw(http.MethodGet, "/plugin-assets/hook-probe/"+file, nil, nil)
			if resp.StatusCode != http.StatusOK || !strings.Contains(resp.Header.Get("Content-Security-Policy"), "sandbox") {
				t.Fatalf("%s 应带 CSP sandbox：%d %v", file, resp.StatusCode, resp.Header)
			}
		}
		for _, path := range []string{"/plugin-assets/hook-probe/../plugin.yaml", "/plugin-assets/hook-probe/%2e%2e/plugin.yaml",
			"/plugin-assets/hook-probe/nope.js", "/plugin-assets/nobody/guest.js"} {
			if resp, _ = anon.raw(http.MethodGet, path, nil, nil); resp.StatusCode != http.StatusNotFound {
				t.Errorf("%s 应为 404，得到 %d", path, resp.StatusCode)
			}
		}
	})

	t.Run("后台页面按权限给出隔离的地址，侧栏有入口", func(t *testing.T) {
		status, out := admin.do(http.MethodGet, "/api/v1/console/plugins/hook-probe/pages/stats", nil)
		mustStatus(t, "后台页面", status, http.StatusOK, out)
		if out["src"] != "/plugin-assets/hook-probe/page.html?v=1.0.0" || out["api"] != "/api/v1/plugins/hook-probe" {
			t.Fatalf("页面信息不对：%v", out)
		}
		s.createUser(t, "editor", "editor")
		editor := s.client(t)
		mustStatus(t, "编辑登录", editor.login("editor", "password-editor"), http.StatusOK, nil)
		status, out = editor.do(http.MethodGet, "/api/v1/console/plugins/hook-probe/pages/stats", nil)
		mustStatus(t, "编辑看后台页面", status, http.StatusForbidden, out)
		status, out = admin.do(http.MethodGet, "/api/v1/console/navigation", nil)
		mustStatus(t, "取侧栏", status, http.StatusOK, out)
		if raw, _ := json.Marshal(out); !strings.Contains(string(raw), "/plugins/hook-probe/p/stats") {
			t.Fatalf("侧栏里没有后台页面的入口：%s", raw)
		}
	})

	t.Run("停用后前台的东西全部消失，短代码原样保留", func(t *testing.T) {
		status, out := admin.do(http.MethodPut, "/api/v1/console/plugins/hook-probe/enabled", map[string]any{"enabled": false})
		mustStatus(t, "停用", status, http.StatusOK, out)
		_, html := s.client(t).page("/posts/frontend-probe")
		if strings.Contains(html, "hook-probe") || strings.Contains(html, "访问 42") {
			t.Fatal("停用后页面里不该还有插件的东西")
		}
		if !strings.Contains(html, `[hello name="小明"]`) && !strings.Contains(html, `[hello name=&#34;小明&#34;]`) {
			t.Fatal("停用后短代码应原样保留")
		}
		if resp, _ := s.client(t).raw(http.MethodGet, "/plugin-assets/hook-probe/guest.js", nil, nil); resp.StatusCode != http.StatusNotFound {
			t.Fatalf("停用后静态文件应读不到，得到 %d", resp.StatusCode)
		}
	})
}

// 插件的接口：公开接口谁都能调，后台接口要登录与权限；登录用户的写请求要过 CSRF；
// 凭据类请求头不转给插件，插件写不了 Cookie；超限、方法不对、出错各有状态码。
func TestPluginRoutes(t *testing.T) {
	s := newTestSite(t)
	s.createUser(t, "admin", "super-admin")
	s.createUser(t, "editor", "editor")
	admin := s.client(t)
	mustStatus(t, "管理员登录", admin.login("admin", "password-admin"), http.StatusOK, nil)
	installProbe(t, admin)
	const base = "/api/v1/plugins/hook-probe"
	jsonHeader := map[string]string{"Content-Type": "application/json"}

	t.Run("公开接口：路径参数、查询与请求体都到了，响应头只留允许的", func(t *testing.T) {
		resp, data := s.client(t).raw(http.MethodPost, base+"/echo/7?q=x", []byte(`{"a":1}`), jsonHeader)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("状态码 %d：%s", resp.StatusCode, data)
		}
		var got map[string]any
		_ = json.Unmarshal(data, &got)
		if got["id"] != "7" || got["q"] != "x" || got["body"].(map[string]any)["a"] != 1.0 || got["user"] != "" {
			t.Fatalf("插件收到的请求不对：%s", data)
		}
		if resp.Header.Get("Set-Cookie") != "" || resp.Header.Get("X-Guest") != "yes" {
			t.Fatalf("Set-Cookie 应被拦下、X- 头应放行：%v", resp.Header)
		}
		if !strings.Contains(resp.Header.Get("Content-Security-Policy"), "sandbox") {
			t.Fatal("插件接口的响应应带 CSP sandbox")
		}
	})

	t.Run("登录用户调写接口：带令牌才认身份，Cookie 不转给插件", func(t *testing.T) {
		resp, data := admin.raw(http.MethodPost, base+"/echo/1", []byte(`{}`), jsonHeader)
		var anon map[string]any
		_ = json.Unmarshal(data, &anon)
		if resp.StatusCode != http.StatusOK || anon["user"] != "" {
			t.Fatalf("公开接口不带令牌应按匿名处理：%d %s", resp.StatusCode, data)
		}
		if resp, _ = admin.raw(http.MethodPost, base+"/whoami", nil, nil); resp.StatusCode != http.StatusForbidden {
			t.Fatalf("后台写接口不带令牌应为 403，得到 %d", resp.StatusCode)
		}
		resp, data = admin.raw(http.MethodPost, base+"/echo/1", []byte(`{}`),
			map[string]string{"Content-Type": "application/json", auth.CSRFHeaderName: admin.csrf})
		var got map[string]any
		_ = json.Unmarshal(data, &got)
		if resp.StatusCode != http.StatusOK || got["user"] != "admin" || got["cookie"] != "" {
			t.Fatalf("带令牌应认出是谁、且不把 Cookie 给插件：%d %s", resp.StatusCode, data)
		}
	})

	t.Run("后台接口要登录与权限", func(t *testing.T) {
		if resp, _ := s.client(t).raw(http.MethodGet, base+"/whoami", nil, nil); resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("匿名应为 401，得到 %d", resp.StatusCode)
		}
		editor := s.client(t)
		mustStatus(t, "编辑登录", editor.login("editor", "password-editor"), http.StatusOK, nil)
		if resp, _ := editor.raw(http.MethodGet, base+"/whoami", nil, nil); resp.StatusCode != http.StatusForbidden {
			t.Fatalf("没有 plugins:manage 应为 403，得到 %d", resp.StatusCode)
		}
		if resp, data := admin.raw(http.MethodGet, base+"/whoami", nil, nil); resp.StatusCode != http.StatusOK || string(data) != "admin" {
			t.Fatalf("管理员应拿到自己的用户名：%d %s", resp.StatusCode, data)
		}
	})

	t.Run("方法不对、没有这个接口、插件出错、请求体超限", func(t *testing.T) {
		anon := s.client(t)
		resp, _ := anon.raw(http.MethodGet, base+"/echo/1", nil, nil)
		if resp.StatusCode != http.StatusMethodNotAllowed || resp.Header.Get("Allow") != http.MethodPost {
			t.Fatalf("方法不对应为 405 并给出 Allow：%d %v", resp.StatusCode, resp.Header)
		}
		if resp, _ = anon.raw(http.MethodGet, base+"/nope", nil, nil); resp.StatusCode != http.StatusNotFound {
			t.Fatalf("没有的接口应为 404，得到 %d", resp.StatusCode)
		}
		resp, data := anon.raw(http.MethodGet, base+"/broken", nil, nil)
		if resp.StatusCode != http.StatusInternalServerError || strings.Contains(string(data), "故意出错") {
			t.Fatalf("插件出错应为 500 且不把细节回给调用方：%d %s", resp.StatusCode, data)
		}
		big := bytes.Repeat([]byte("x"), 70<<10)
		if resp, _ = anon.raw(http.MethodPost, base+"/echo/1", big, nil); resp.StatusCode != http.StatusRequestEntityTooLarge {
			t.Fatalf("公开接口的请求体超过 64 KiB 应为 413，得到 %d", resp.StatusCode)
		}
		if resp, _ = anon.raw(http.MethodGet, "/api/v1/plugins/nobody/x", nil, nil); resp.StatusCode != http.StatusNotFound {
			t.Fatalf("没有的插件应为 404，得到 %d", resp.StatusCode)
		}
	})
}
