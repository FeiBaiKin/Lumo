package theme_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FeiBaiKin/lumo/internal/testsupport"
	"github.com/FeiBaiKin/lumo/internal/theme"
)

// demoThemeZip 打一个可安装的第三方主题包，首页输出 marker。
func demoThemeZip(t *testing.T, name, marker string) []byte {
	t.Helper()

	files := map[string]string{
		"theme.yaml": "name: " + name + "\nlabel: 演示主题\nversion: 2.1.0\n" +
			"description: 测试用主题\nauthor: 测试\nlicense: MIT\n",
		// 设置声明：走与站点设置同一套表单 Schema。
		"settings.yaml": `
groups:
  - name: look
    label: 外观
    schema:
      type: object
      additionalProperties: false
      properties:
        mark:
          type: string
          title: 标记
    defaults:
      mark: "默认标记"
`,
		"templates/layouts/base.html":  `<html><body>{{ block "main" . }}{{ end }}</body></html>`,
		"templates/partials/head.html": `<meta charset="utf-8">`,
		"templates/index.html": `{{ template "layouts/base.html" . }}{{ define "main" }}` +
			marker + `|{{ .Theme.Settings.look.mark }}{{ end }}`,
		"templates/post.html": `{{ template "layouts/base.html" . }}{{ define "main" }}` +
			`DEMO-POST:{{ .Post.Title }}{{ end }}`,
		"templates/page.html": `{{ template "layouts/base.html" . }}{{ define "main" }}DEMO-PAGE{{ end }}`,
		"templates/404.html":  `{{ template "layouts/base.html" . }}{{ define "main" }}DEMO-404{{ end }}`,
		// 只提供四个必需模板，故意不给 category.html —— 用来验证回退。
		"static/demo.css": "body{color:red}",
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for rel, body := range files {
		w, err := zw.Create(rel)
		if err != nil {
			t.Fatalf("创建 zip 条目失败: %v", err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatalf("写入 zip 条目失败: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("关闭 zip 失败: %v", err)
	}
	return buf.Bytes()
}

// uploadTheme 通过 Console 接口上传主题包。
func uploadTheme(t *testing.T, s *testsupport.Stack, auth string, data []byte, overwrite bool) *http.Response {
	t.Helper()

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("file", "theme.zip")
	if err != nil {
		t.Fatalf("构造 multipart 失败: %v", err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatalf("写入 multipart 失败: %v", err)
	}
	if overwrite {
		if err := mw.WriteField("overwrite", "true"); err != nil {
			t.Fatalf("写入 overwrite 字段失败: %v", err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("关闭 multipart 失败: %v", err)
	}

	rec := s.Do(t, &testsupport.Request{
		Method: http.MethodPost, Path: "/api/v1/console/themes",
		Body: body.String(), ContentType: mw.FormDataContentType(), Auth: auth,
	})
	return rec.Result()
}

// TestThemeLifecycleInstallActivateDelete 覆盖主题的完整生命周期。
//
// 上传 → 列表可见 → 启用 → 前台换成新主题 → 设置生效 → 卸载 → 回退内置。
// 这是阶段 4「主题包加载：zip 解析、校验、启用、切换」的端到端验收。
func TestThemeLifecycleInstallActivateDelete(t *testing.T) {
	stack, mod := newThemeStack(t)
	admin := stack.Bearer(t, "admin-lifecycle", "admin")
	_ = seedPost(t, stack, admin, "生命周期文章", "lifecycle-post", "正文")

	// ---- 初始状态：内置主题 ----
	if got := mod.Registry().ActiveName(); got != theme.BuiltinName {
		t.Fatalf("初始启用主题 = %q", got)
	}
	_, before := get(t, stack, "/")
	if strings.Contains(before, "DEMO-HOME") {
		t.Fatal("尚未安装主题，不该出现演示主题的输出")
	}

	// ---- 上传 ----
	resp := uploadTheme(t, stack, admin, demoThemeZip(t, "demo", "DEMO-HOME"), false)
	if resp.StatusCode != http.StatusCreated {
		body, _ := readAll(resp)
		t.Fatalf("上传状态码 = %d，期望 201：%s", resp.StatusCode, body)
	}
	_ = resp.Body.Close()

	// 主题目录应落到工作目录的 themes 下。
	themeDir := filepath.Join(dataDirOf(t, stack), "themes", "demo")
	if _, err := os.Stat(filepath.Join(themeDir, "theme.yaml")); err != nil {
		t.Fatalf("主题文件未落盘: %v", err)
	}

	// ---- 列表可见 ----
	names := listThemeNames(t, stack, admin)
	if !contains(names, "demo") || !contains(names, theme.BuiltinName) {
		t.Fatalf("主题列表 = %v，应同时含 demo 与内置主题", names)
	}

	// ---- 启用 ----
	rec := stack.Do(t, &testsupport.Request{
		Method: http.MethodPost, Path: "/api/v1/console/themes/demo/activate",
		Body: `{}`, Auth: admin,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("启用失败 %d: %s", rec.Code, rec.Body.String())
	}
	if got := mod.Registry().ActiveName(); got != "demo" {
		t.Fatalf("启用后主题 = %q，期望 demo", got)
	}

	// ---- 前台换新 ----
	code, after := get(t, stack, "/")
	if code != http.StatusOK {
		t.Fatalf("首页状态码 = %d", code)
	}
	if !strings.Contains(after, "DEMO-HOME") {
		t.Errorf("首页应使用新主题渲染，实际 %.300s", after)
	}
	if !strings.Contains(after, "默认标记") {
		t.Error("主题设置缺省值应注入上下文")
	}
	// 标记：新主题只有四个必需模板。
	if !strings.Contains(after, "<html><body>") {
		t.Error("新主题的骨架未生效")
	}

	// ---- 文章页也是新主题 ----
	_, postPage := get(t, stack, "/posts/lifecycle-post")
	if !strings.Contains(postPage, "DEMO-POST:生命周期文章") {
		t.Errorf("文章页应使用新主题，实际 %.300s", postPage)
	}

	// ---- 可选模板回退到内置主题 ----
	// demo 没有 category.html，应整页回退而不是报错。
	adminRec := stack.Do(t, &testsupport.Request{
		Method: http.MethodPost, Path: "/api/v1/console/categories",
		Body: `{"name":"回退分类","slug":"fallback-cat"}`, Auth: admin,
	})
	if adminRec.Code != http.StatusCreated {
		t.Fatalf("创建分类失败 %d: %s", adminRec.Code, adminRec.Body.String())
	}
	code, catPage := get(t, stack, "/categories/fallback-cat")
	if code != http.StatusOK {
		t.Fatalf("分类页状态码 = %d，期望 200（应回退到内置主题）", code)
	}
	// 回退后是内置主题「墨」渲染的页面：它带完整文档骨架。
	if !strings.Contains(catPage, "<!doctype html>") {
		t.Errorf("分类页应回退到内置主题渲染，实际 %.300s", catPage)
	}

	// ---- 主题设置读写 ----
	rec = stack.Do(t, &testsupport.Request{
		Method: http.MethodPut, Path: "/api/v1/console/themes/demo/settings/look",
		Body: `{"mark":"自定义标记"}`, Auth: admin,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("更新主题设置失败 %d: %s", rec.Code, rec.Body.String())
	}
	_, withSetting := get(t, stack, "/")
	if !strings.Contains(withSetting, "自定义标记") {
		t.Error("更新后的主题设置应反映到页面")
	}

	// ---- 主题静态资源 ----
	asset := stack.Do(t, &testsupport.Request{
		Method: http.MethodGet, Path: "/theme-assets/demo/demo.css",
	})
	if asset.Code != http.StatusOK || !strings.Contains(asset.Body.String(), "color:red") {
		t.Errorf("主题静态资源状态码 = %d", asset.Code)
	}

	// ---- 当前启用的主题不可删除 ----
	rec = stack.Do(t, &testsupport.Request{
		Method: http.MethodDelete, Path: "/api/v1/console/themes/demo", Auth: admin,
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("删除启用中的主题状态码 = %d，期望 409", rec.Code)
	}

	// ---- 切回内置后可删除 ----
	rec = stack.Do(t, &testsupport.Request{
		Method: http.MethodPost, Path: "/api/v1/console/themes/" + theme.BuiltinName + "/activate",
		Body: `{}`, Auth: admin,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("切回内置失败 %d: %s", rec.Code, rec.Body.String())
	}
	_, back := get(t, stack, "/")
	if strings.Contains(back, "DEMO-HOME") {
		t.Error("切回内置主题后不该再出现演示主题的输出")
	}

	rec = stack.Do(t, &testsupport.Request{
		Method: http.MethodDelete, Path: "/api/v1/console/themes/demo", Auth: admin,
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("删除状态码 = %d，期望 204：%s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(themeDir); !os.IsNotExist(err) {
		t.Error("主题目录应被删除")
	}
	if names := listThemeNames(t, stack, admin); contains(names, "demo") {
		t.Errorf("主题列表仍含 demo：%v", names)
	}
}

// TestThemeInstallRejectsBadPackage 验证坏主题包被拒且不留残留。
func TestThemeInstallRejectsBadPackage(t *testing.T) {
	stack, mod := newThemeStack(t)
	admin := stack.Bearer(t, "admin-badpkg", "admin")

	tests := []struct {
		name       string
		data       []byte
		wantStatus int
	}{
		{"非 zip", []byte("这不是 zip"), http.StatusUnprocessableEntity},
		{"空 zip", emptyZip(t), http.StatusUnprocessableEntity},
		{"缺少必需模板", zipWithout(t, "templates/404.html"), http.StatusUnprocessableEntity},
		{"模板语法错误", zipWithBrokenTemplate(t), http.StatusUnprocessableEntity},
		{"设置 schema 非法", zipWithBadSettings(t), http.StatusUnprocessableEntity},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := uploadTheme(t, stack, admin, tt.data, false)
			if resp.StatusCode != tt.wantStatus {
				body, _ := readAll(resp)
				t.Fatalf("状态码 = %d，期望 %d：%s", resp.StatusCode, tt.wantStatus, body)
			}
			_ = resp.Body.Close()
		})
	}

	// 全部失败之后，启用主题应不受影响，且磁盘上没有半个主题。
	if got := mod.Registry().ActiveName(); got != theme.BuiltinName {
		t.Errorf("启用主题 = %q，应保持内置", got)
	}
	if code, _ := get(t, stack, "/"); code != http.StatusOK {
		t.Errorf("站点在多次失败上传后仍应正常服务，状态码 = %d", code)
	}
	themeRoot := filepath.Join(dataDirOf(t, stack), "themes")
	entries, err := os.ReadDir(themeRoot)
	if err == nil {
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".staging-") {
				t.Errorf("残留临时目录 %s", e.Name())
			}
		}
	}
}

// TestThemeInstallOverwrite 验证覆盖安装同名主题。
func TestThemeInstallOverwrite(t *testing.T) {
	stack, _ := newThemeStack(t)
	admin := stack.Bearer(t, "admin-overwrite", "admin")

	resp := uploadTheme(t, stack, admin, demoThemeZip(t, "demo2", "V1"), false)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("首次上传状态码 = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// 不传 overwrite 时应拒绝。
	resp = uploadTheme(t, stack, admin, demoThemeZip(t, "demo2", "V2"), false)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("重复上传状态码 = %d，期望 409", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// 传 overwrite=true 时应替换。
	resp = uploadTheme(t, stack, admin, demoThemeZip(t, "demo2", "V2"), true)
	if resp.StatusCode != http.StatusCreated {
		body, _ := readAll(resp)
		t.Fatalf("覆盖上传状态码 = %d，期望 201：%s", resp.StatusCode, body)
	}
	_ = resp.Body.Close()

	rec := stack.Do(t, &testsupport.Request{
		Method: http.MethodPost, Path: "/api/v1/console/themes/demo2/activate",
		Body: `{}`, Auth: admin,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("启用失败 %d", rec.Code)
	}
	_, body := get(t, stack, "/")
	if !strings.Contains(body, "V2") {
		t.Error("覆盖后应使用新版本模板")
	}
}

// TestThemeSettingsRejectUnknownGroup 验证主题设置分组的边界。
func TestThemeSettingsRejectUnknownGroup(t *testing.T) {
	stack, _ := newThemeStack(t)
	admin := stack.Bearer(t, "admin-tset", "admin")

	resp := uploadTheme(t, stack, admin, demoThemeZip(t, "demo3", "X"), false)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("上传失败 %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	base := "/api/v1/console/themes/demo3/settings"
	rec := stack.Do(t, &testsupport.Request{Method: http.MethodGet, Path: base, Auth: admin})
	if rec.Code != http.StatusOK {
		t.Fatalf("列出设置失败 %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Items []struct {
			Name   string         `json:"name"`
			Values map[string]any `json:"values"`
		} `json:"items"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if len(body.Items) != 1 || body.Items[0].Name != "look" {
		t.Fatalf("分组 = %+v", body.Items)
	}
	if body.Items[0].Values["mark"] != "默认标记" {
		t.Errorf("缺省值未生效: %v", body.Items[0].Values)
	}

	// 未知类型应 422 且逐条定位。
	rec = stack.Do(t, &testsupport.Request{
		Method: http.MethodPut, Path: base + "/look", Body: `{"mark":123}`, Auth: admin,
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("状态码 = %d，期望 422：%s", rec.Code, rec.Body.String())
	}

	// 主题不存在时应 404。
	rec = stack.Do(t, &testsupport.Request{
		Method: http.MethodGet, Path: "/api/v1/console/themes/nope/settings", Auth: admin,
	})
	if rec.Code != http.StatusNotFound {
		t.Errorf("状态码 = %d，期望 404", rec.Code)
	}
}

// ---------- 辅助 ----------

// readAll 读取并关闭响应体。
func readAll(resp *http.Response) (string, error) {
	defer func() { _ = resp.Body.Close() }()
	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// dataDirOf 取回测试栈使用的数据目录。
func dataDirOf(t *testing.T, s *testsupport.Stack) string {
	t.Helper()
	return s.App.Config().DataDir
}

// listThemeNames 调接口取回主题名列表。
func listThemeNames(t *testing.T, s *testsupport.Stack, auth string) []string {
	t.Helper()

	rec := s.Do(t, &testsupport.Request{
		Method: http.MethodGet, Path: "/api/v1/console/themes", Auth: auth,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("列出主题失败 %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Items []struct {
			Name string `json:"name"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析主题列表失败: %v", err)
	}
	names := make([]string, 0, len(body.Items))
	for _, item := range body.Items {
		names = append(names, item.Name)
	}
	return names
}

// contains 报告切片是否含某值。
func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// buildZipFrom 由文件表打包。
func buildZipFrom(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for rel, body := range files {
		w, err := zw.Create(rel)
		if err != nil {
			t.Fatalf("创建 zip 条目失败: %v", err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatalf("写入 zip 条目失败: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("关闭 zip 失败: %v", err)
	}
	return buf.Bytes()
}

// demoFiles 返回一个合法主题包的文件表。
func demoFiles() map[string]string {
	return map[string]string{
		"theme.yaml":                  "name: edge\nlabel: 边界\nversion: 1.0.0\n",
		"templates/layouts/base.html": `<html>{{ block "main" . }}{{ end }}</html>`,
		"templates/index.html":        `{{ template "layouts/base.html" . }}{{ define "main" }}I{{ end }}`,
		"templates/post.html":         `{{ template "layouts/base.html" . }}{{ define "main" }}P{{ end }}`,
		"templates/page.html":         `{{ template "layouts/base.html" . }}{{ define "main" }}G{{ end }}`,
		"templates/404.html":          `{{ template "layouts/base.html" . }}{{ define "main" }}N{{ end }}`,
	}
}

// emptyZip 返回一个合法的空 zip。
func emptyZip(t *testing.T) []byte { return buildZipFrom(t, map[string]string{}) }

// zipWithout 返回缺少指定文件的主题包。
func zipWithout(t *testing.T, drop string) []byte {
	t.Helper()
	files := demoFiles()
	delete(files, drop)
	return buildZipFrom(t, files)
}

// zipWithBrokenTemplate 返回模板语法错误的主题包。
func zipWithBrokenTemplate(t *testing.T) []byte {
	t.Helper()
	files := demoFiles()
	files["templates/index.html"] = `{{ if .X }}没有 end`
	return buildZipFrom(t, files)
}

// zipWithBadSettings 返回设置声明无法编译的主题包。
func zipWithBadSettings(t *testing.T) []byte {
	t.Helper()
	files := demoFiles()
	// 缺省值不符合自身 schema：size 声明最小 10 却给了 1。
	files["settings.yaml"] = `
groups:
  - name: look
    schema:
      type: object
      properties:
        size:
          type: integer
          minimum: 10
    defaults:
      size: 1
`
	return buildZipFrom(t, files)
}
