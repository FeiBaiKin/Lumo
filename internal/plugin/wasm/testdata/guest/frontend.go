package main

import (
	"errors"
	"fmt"
	"html"

	lumo "github.com/FeiBaiKin/lumo/sdk/go"
)

// registerFrontend 登记接口、插槽、小组件与短代码；输出里故意夹带脚本与事件属性，测宿主的净化。
func registerFrontend() {
	lumo.OnRoute("echo", func(_ *lumo.Context, req *lumo.Request) (*lumo.Response, error) {
		var body map[string]any
		if len(req.Body) > 0 {
			if err := req.Decode(&body); err != nil {
				return lumo.Problem(400, "请求体不是 JSON"), nil
			}
		}
		user := ""
		if req.User != nil {
			user = req.User.Username
		}
		resp := lumo.JSON(200, map[string]any{
			"id": req.Params["id"], "q": req.QueryValue("q"), "body": body, "user": user,
			"cookie": req.Header("Cookie"), "ip": req.IP,
		})
		resp.Headers["Set-Cookie"] = "stolen=1"
		resp.Headers["X-Guest"] = "yes"
		return resp, nil
	})
	whoami := func(_ *lumo.Context, req *lumo.Request) (*lumo.Response, error) {
		return lumo.Text(200, req.User.Username), nil
	}
	lumo.OnRoute("whoami", whoami)
	lumo.OnRoute("whoami-write", whoami)
	lumo.OnRoute("broken", func(*lumo.Context, *lumo.Request) (*lumo.Response, error) {
		return nil, errors.New("故意出错")
	})
	lumo.OnSlot(lumo.SlotContentAfter, func(_ *lumo.Context, page *lumo.PageInfo) (string, error) {
		return fmt.Sprintf(`<p class="guest-slot">插槽 %s</p><script>alert(1)</script><button type="button" onclick="x()">按钮</button>`,
			html.EscapeString(page.Kind)), nil
	})
	lumo.OnSlot(lumo.SlotHead, func(*lumo.Context, *lumo.PageInfo) (string, error) {
		return `<meta name="guest-head" content="1"><script>alert(1)</script>`, nil
	})
	lumo.OnWidget("counter", func(*lumo.Context, *lumo.PageInfo) (string, error) {
		return `<p class="guest-widget">访问 42</p>`, nil
	})
	lumo.OnShortcode("hello", func(_ *lumo.Context, sc *lumo.Shortcode) (string, error) {
		return "<strong class=\"guest-sc\">你好，" + html.EscapeString(sc.Attr("name", "访客")) + "</strong><img src=x onerror=alert(1)>", nil
	})
}
