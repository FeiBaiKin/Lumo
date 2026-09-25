package plugin

import (
	"errors"
	"strings"
	"testing"
)

func TestMatchRoute(t *testing.T) {
	routes := []Route{
		{Name: "item", Method: "GET", Path: "/items/{id}"},
		{Name: "latest", Method: "GET", Path: "/items/latest"},
		{Name: "create", Method: "POST", Path: "/items"},
		{Name: "root", Method: "GET", Path: "/"},
	}
	if err := normalizeRoutes(routes, true); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		method, path, want, param string
		allow                     int
	}{
		{"GET", "/items/7", "item", "7", 0},
		{"GET", "/items/latest", "latest", "", 0},
		{"GET", "/", "root", "", 0},
		{"GET", "", "root", "", 0},
		{"POST", "/items", "create", "", 0},
		{"GET", "/items", "", "", 1},
		{"GET", "/items/7/x", "", "", 0},
	}
	for _, tc := range cases {
		route, params, allow := matchRoute(routes, tc.method, tc.path)
		got := ""
		if route != nil {
			got = route.Name
		}
		if got != tc.want || params["id"] != tc.param || len(allow) != tc.allow {
			t.Errorf("%s %s 匹配到 %q（参数 %v，允许 %v），应为 %q", tc.method, tc.path, got, params, allow, tc.want)
		}
	}
}

// 前台、接口与后台页面的声明各有规矩：能力、后端、路径、文件位置，写错了装不上。
func TestFrontendDeclarationRules(t *testing.T) {
	cases := map[string]string{
		"没声明前台能力就放样式":   "  frontend:\n    styles: [static/a.css]\n",
		"样式不在 static 下": "  capabilities: {frontend: true}\n  frontend:\n    styles: [a.css]\n",
		"样式路径越界":        "  capabilities: {frontend: true}\n  frontend:\n    styles: [static/../plugin.yaml]\n",
		"脚本扩展名不对":       "  capabilities: {frontend: true}\n  frontend:\n    scripts: [static/a.css]\n",
		"没有后端却要插槽":      "  capabilities: {frontend: true}\n  frontend:\n    slots: [footer]\n",
		"插槽名写错":         "  runtime: wasm\n  capabilities: {frontend: true}\n  frontend:\n    slots: [sidebar]\n",
		"短代码名不合规":       "  runtime: wasm\n  capabilities: {frontend: true}\n  frontend:\n    shortcodes: [{name: Views}]\n",
		"没有后端却有接口":      "  routes:\n    - {name: a, path: /a}\n",
		"接口路径不以斜杠开头":    "  runtime: wasm\n  routes:\n    - {name: a, path: a}\n",
		"同方法同形态的路径":     "  runtime: wasm\n  routes:\n    - {name: a, path: \"/x/{id}\"}\n    - {name: b, path: \"/x/{key}\"}\n",
		"公开接口还写权限":      "  runtime: wasm\n  routes:\n    - {name: a, path: /a, public: true, permission: posts:write}\n",
		"接口方法不支持":       "  runtime: wasm\n  routes:\n    - {name: a, path: /a, method: TRACE}\n",
		"后台页面不是 html":   "  pages:\n    - {path: stats, file: static/a.js}\n",
	}
	for name, spec := range cases {
		if _, err := parseManifest(manifest(spec), ""); !errors.Is(err, ErrInvalidPackage) {
			t.Errorf("%s：应被拒绝，得到 %v", name, err)
		}
	}
	ok := "  capabilities: {frontend: true}\n  frontend:\n    styles: [static/a.css]\n    scripts: [static/js/a.js]\n" +
		"  pages:\n    - {path: stats, file: static/page.html}\n"
	if _, err := parseManifest(manifest(ok), ""); err != nil {
		t.Fatalf("纯声明式的前台插件应能装上：%v", err)
	}
}

// 包里得真有清单引用的静态文件。
func TestStaticFilesMustExist(t *testing.T) {
	m := testModule(t)
	spec := "  capabilities: {frontend: true}\n  frontend:\n    styles: [static/a.css]\n"
	if err := install(t, m.registry, map[string][]byte{FileManifest: manifest(spec)}); !errors.Is(err, ErrInvalidPackage) {
		t.Fatalf("缺了引用的样式表应装不上，得到 %v", err)
	}
	if err := install(t, m.registry, map[string][]byte{FileManifest: manifest(spec), "static/a.css": []byte("a{}")}); err != nil {
		t.Fatal(err)
	}
}

func TestExpandShortcodes(t *testing.T) {
	render := func(name string, attrs map[string]string) (string, bool) {
		if name != "hi" {
			return "", false
		}
		return "<b>" + attrs["to"] + "|" + attrs["n"] + "|" + attrs["flag"] + "</b>", true
	}
	cases := map[string]string{
		`<p>[hi to="小明" n='2' flag]</p>`:          `<p><b>小明|2|</b></p>`,
		`<p>前 [hi to=x /] 后</p>`:                  `<p>前 <b>x||</b> 后</p>`,
		`<p>[[hi]] 与 [other]</p>`:                 `<p>[hi] 与 [other]</p>`,
		`<pre><code>[hi]</code></pre>`:            `<pre><code>[hi]</code></pre>`,
		`<a href="/x?[hi]" title="[hi]">[hi]</a>`: `<a href="/x?[hi]" title="[hi]"><b>||</b></a>`,
		`<p>a &lt; b [hi] &amp; c</p>`:            `<p>a &lt; b <b>||</b> &amp; c</p>`,
	}
	for in, want := range cases {
		got, _ := expandShortcodes(in, render)
		if got != want {
			t.Errorf("展开 %s\n得到 %s\n应为 %s", in, got, want)
		}
	}
	if out, changed := expandShortcodes(`<p>[other]</p>`, render); changed || out != `<p>[other]</p>` {
		t.Fatalf("没有认得的短代码时应原样返回：%q %v", out, changed)
	}
}

// 插件片段里的脚本、事件属性与 head 里的样式表都进不来。
func TestFragmentPolicies(t *testing.T) {
	body := fragmentPolicy().Sanitize(`<form action="/x"><input name="q" onfocus="x()"><button>搜</button></form><script>1</script>`)
	if strings.Contains(body, "script") || strings.Contains(body, "onfocus") || !strings.Contains(body, `<input name="q">`) {
		t.Fatalf("片段净化不对：%s", body)
	}
	head := headPolicy().Sanitize(`<meta name="a" content="b"><link rel="stylesheet" href="/x.css"><style>*{}</style><script>1</script>`)
	if head != `<meta name="a" content="b"><link href="/x.css">` {
		t.Fatalf("head 净化不对：%s", head)
	}
}
