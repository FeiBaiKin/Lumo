package theme

import (
	"strings"
	"testing"
	"testing/fstest"
)

// newTestFS 构造一个最小可用的模板目录。
func newTestFS(files map[string]string) fstest.MapFS {
	out := fstest.MapFS{}
	for name, body := range files {
		out[name] = &fstest.MapFile{Data: []byte(body)}
	}
	return out
}

// minimalTemplates 是满足必需模板要求的最小集合。
func minimalTemplates() map[string]string {
	return map[string]string{
		"index.html":        `{{ template "layouts/base.html" . }}{{ define "main" }}INDEX{{ end }}`,
		"post.html":         `{{ template "layouts/base.html" . }}{{ define "main" }}POST:{{ .Title }}{{ end }}`,
		"page.html":         `{{ template "layouts/base.html" . }}{{ define "main" }}PAGE{{ end }}`,
		"404.html":          `{{ template "layouts/base.html" . }}{{ define "main" }}NOTFOUND{{ end }}`,
		"layouts/base.html": `<html><body>[H]{{ block "main" . }}{{ end }}[F]</body></html>`,
	}
}

// TestPerPageTemplateSets 验证每个页面各有一套模板集合。
//
// 这是本包最容易回归的一处：若所有页面共用一个 template.Template，
// 同名的 main 块会互相覆盖，表现为「所有页面都渲染成最后解析的那个」。
func TestPerPageTemplateSets(t *testing.T) {
	t.Parallel()

	engine, err := parseEngine("test", newTestFS(minimalTemplates()), baseFuncs())
	if err != nil {
		t.Fatalf("解析模板失败: %v", err)
	}

	tests := []struct {
		template string
		want     string
	}{
		{"index.html", "[H]INDEX[F]"},
		{"post.html", "[H]POST:标题[F]"},
		{"page.html", "[H]PAGE[F]"},
		{"404.html", "[H]NOTFOUND[F]"},
	}
	for _, tt := range tests {
		t.Run(tt.template, func(t *testing.T) {
			t.Parallel()
			var sb strings.Builder
			if err := engine.Render(&sb, tt.template, &Context{Title: "标题"}); err != nil {
				t.Fatalf("渲染失败: %v", err)
			}
			if !strings.Contains(sb.String(), tt.want) {
				t.Errorf("输出 = %q，期望含 %q", sb.String(), tt.want)
			}
		})
	}
}

// TestFallbackRendersMissingTemplate 验证缺失模板回退到回退主题（agent.md §4.4）。
func TestFallbackRendersMissingTemplate(t *testing.T) {
	t.Parallel()

	// 回退主题提供全部九个模板。
	fallbackFiles := minimalTemplates()
	fallbackFiles["category.html"] = `{{ template "layouts/base.html" . }}{{ define "main" }}FALLBACK-CATEGORY{{ end }}`
	fallback, err := parseEngine("builtin", newTestFS(fallbackFiles), baseFuncs())
	if err != nil {
		t.Fatalf("解析回退主题失败: %v", err)
	}

	// 子主题只有四个必需模板，没有 category.html。
	child, err := parseEngine("child", newTestFS(minimalTemplates()), baseFuncs())
	if err != nil {
		t.Fatalf("解析子主题失败: %v", err)
	}
	child.SetFallback(fallback)

	if child.Has("category.html") {
		t.Fatal("子主题不该自带 category.html")
	}
	var sb strings.Builder
	if err := child.Render(&sb, "category.html", &Context{}); err != nil {
		t.Fatalf("回退渲染失败: %v", err)
	}
	if !strings.Contains(sb.String(), "FALLBACK-CATEGORY") {
		t.Errorf("输出 = %q，期望回退到默认主题的 category.html", sb.String())
	}

	// 自己有的模板不该被回退覆盖。
	sb.Reset()
	if err := child.Render(&sb, "index.html", &Context{}); err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	if !strings.Contains(sb.String(), "INDEX") {
		t.Errorf("输出 = %q，期望用子主题自己的 index.html", sb.String())
	}
}

// TestRenderMissingWithoutFallback 验证无回退时缺失模板返回明确错误。
func TestRenderMissingWithoutFallback(t *testing.T) {
	t.Parallel()

	engine, err := parseEngine("test", newTestFS(minimalTemplates()), baseFuncs())
	if err != nil {
		t.Fatalf("解析模板失败: %v", err)
	}
	var sb strings.Builder
	err = engine.Render(&sb, "search.html", &Context{})
	if !ErrTemplateMissing(err) {
		t.Fatalf("错误 = %v，期望 ErrTemplateNotFound", err)
	}
}

// TestSetFallbackIgnoresSelf 验证回退指向自身时被忽略，避免 Lookup 无限递归。
func TestSetFallbackIgnoresSelf(t *testing.T) {
	t.Parallel()

	engine, err := parseEngine("test", newTestFS(minimalTemplates()), baseFuncs())
	if err != nil {
		t.Fatalf("解析模板失败: %v", err)
	}
	engine.SetFallback(engine)
	if _, ok := engine.Lookup("search.html"); ok {
		t.Error("自指回退不应让缺失模板变成可用")
	}
}

// TestValidateTemplatesRequiresFour 验证四个必需模板缺一不可（agent.md §4.4）。
func TestValidateTemplatesRequiresFour(t *testing.T) {
	t.Parallel()

	if err := validateTemplates(requiredTemplates); err != nil {
		t.Fatalf("齐备时不应报错: %v", err)
	}
	for _, missing := range requiredTemplates {
		t.Run("缺少"+missing, func(t *testing.T) {
			t.Parallel()
			var names []string
			for _, n := range requiredTemplates {
				if n != missing {
					names = append(names, n)
				}
			}
			err := validateTemplates(names)
			if err == nil {
				t.Fatal("缺少必需模板时应报错")
			}
			if !strings.Contains(err.Error(), missing) {
				t.Errorf("错误信息应指出缺少 %s，实际 %v", missing, err)
			}
		})
	}
}

// TestParseEngineRejectsBadSyntax 验证语法错误在解析期被拒。
//
// 安装时就暴露，而不是等切换主题后整站白屏。
func TestParseEngineRejectsBadSyntax(t *testing.T) {
	t.Parallel()

	files := minimalTemplates()
	files["index.html"] = `{{ if .Title }}没有 end`
	_, err := parseEngine("bad", newTestFS(files), baseFuncs())
	if err == nil {
		t.Fatal("语法错误应导致解析失败")
	}
	if !strings.Contains(err.Error(), "index.html") {
		t.Errorf("错误应指出出错的模板名，实际 %v", err)
	}
}

// TestParseEngineRejectsEmptyDir 验证没有页面模板时报错。
func TestParseEngineRejectsEmptyDir(t *testing.T) {
	t.Parallel()

	t.Run("完全为空", func(t *testing.T) {
		t.Parallel()
		if _, err := parseEngine("empty", newTestFS(map[string]string{}), baseFuncs()); err == nil {
			t.Fatal("空目录应报错")
		}
	})
	t.Run("只有共享模板", func(t *testing.T) {
		t.Parallel()
		files := map[string]string{
			"layouts/base.html":    `<html>{{ block "main" . }}{{ end }}</html>`,
			"partials/header.html": `<header></header>`,
		}
		if _, err := parseEngine("shared-only", newTestFS(files), baseFuncs()); err == nil {
			t.Fatal("只有 layouts 与 partials 时应报错")
		}
	})
}

// TestPageTemplates 验证只识别根目录下的 page-*.html。
func TestPageTemplates(t *testing.T) {
	t.Parallel()

	names := []string{
		"index.html", "post.html", "page.html", "404.html",
		"page-about.html", "page-wide.html",
		// 以下都不该被识别：子目录里的、名字不以 page- 开头的。
		"partials/page-foo.html", "layouts/page-bar.html", "pages.html",
	}
	got := PageTemplates(names)
	want := []string{"page-about", "page-wide"}
	if len(got) != len(want) {
		t.Fatalf("结果 = %v，期望 %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 项 = %q，期望 %q", i, got[i], want[i])
		}
	}
}

// TestTemplateStatuses 验证模板提供情况的汇总。
func TestTemplateStatuses(t *testing.T) {
	t.Parallel()

	statuses := TemplateStatuses([]string{"index.html", "post.html", "page.html", "404.html", "tag.html"})
	byName := make(map[string]TemplateStatus, len(statuses))
	for _, s := range statuses {
		byName[s.Name] = s
	}

	if got := byName["index.html"]; !got.Required || !got.Provided {
		t.Errorf("index.html = %+v，期望必需且已提供", got)
	}
	if got := byName["tag.html"]; got.Required || !got.Provided {
		t.Errorf("tag.html = %+v，期望可选且已提供", got)
	}
	if got := byName["search.html"]; got.Required || got.Provided {
		t.Errorf("search.html = %+v，期望可选且未提供", got)
	}
	if len(statuses) != len(requiredTemplates)+len(optionalTemplates) {
		t.Errorf("条目数 = %d，期望 %d", len(statuses), len(requiredTemplates)+len(optionalTemplates))
	}
}

// TestRenderLimitsOutput 验证渲染输出超限时中止而不是吃光内存。
func TestRenderLimitsOutput(t *testing.T) {
	t.Parallel()

	w := &limitedWriter{w: &strings.Builder{}, remaining: 10}
	if _, err := w.Write([]byte("12345")); err != nil {
		t.Fatalf("未超限的写入不应报错: %v", err)
	}
	if _, err := w.Write([]byte("123456")); err == nil {
		t.Fatal("超限的写入应报错")
	}
}
