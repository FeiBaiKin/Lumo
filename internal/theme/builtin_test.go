package theme

import (
	"context"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/FeiBaiKin/lumo/internal/auth"
	"github.com/FeiBaiKin/lumo/internal/content"
)

// builtinThemeFS 返回内置主题的文件系统。
func builtinThemeFS(t *testing.T) fs.FS {
	t.Helper()
	sub, err := fs.Sub(builtinFS, "builtin/"+BuiltinName)
	if err != nil {
		t.Fatalf("内置主题目录缺失: %v", err)
	}
	return sub
}

// TestBuiltinThemeLoads 验证内置主题可加载。
//
// 内置主题是所有主题的回退：它加载不了，整个主题系统就没有兜底，
// 第三方主题缺一个可选模板就会 500。故这条断言是硬底线。
func TestBuiltinThemeLoads(t *testing.T) {
	t.Parallel()

	loaded, err := loadFS(BuiltinName, builtinThemeFS(t), "", true)
	if err != nil {
		t.Fatalf("内置主题加载失败: %v", err)
	}
	if loaded.Manifest.Name != BuiltinName {
		t.Errorf("name = %q，期望 %q", loaded.Manifest.Name, BuiltinName)
	}
	if loaded.Static == nil {
		t.Error("内置主题应带 static 目录")
	}
}

// TestBuiltinThemeProvidesEveryTemplate 验证内置主题提供全部模板（必需 4 + 可选 11）。
//
// 可选模板对第三方主题是可选的，对内置主题不是——它是回退目标，
// 缺哪个，装了不提供该模板的第三方主题时那个页面就没得渲染。
func TestBuiltinThemeProvidesEveryTemplate(t *testing.T) {
	t.Parallel()

	loaded, err := loadFS(BuiltinName, builtinThemeFS(t), "", true)
	if err != nil {
		t.Fatalf("内置主题加载失败: %v", err)
	}
	provided := make(map[string]bool, len(loaded.Templates))
	for _, n := range loaded.Templates {
		provided[n] = true
	}
	for _, n := range append(RequiredTemplates(), OptionalTemplates()...) {
		if !provided[n] {
			t.Errorf("内置主题缺少模板 %s", n)
		}
	}
}

// TestBuiltinThemeRendersEveryPage 验证全部页面在最小上下文下都能渲染出内容。
//
// 用最小上下文（大量零值）而非精心构造的数据：模板最常见的崩法是
// 对 nil 指针取字段，而真实站点上「没有封面」「没有作者」「没有分类」都是常态。
func TestBuiltinThemeRendersEveryPage(t *testing.T) {
	t.Parallel()

	loaded, err := loadFS(BuiltinName, builtinThemeFS(t), "", true)
	if err != nil {
		t.Fatalf("内置主题加载失败: %v", err)
	}
	engine := loaded.Engine()
	settings := loaded.settings.effectiveSettings(nil)

	base := func(kind string) *Context {
		return &Context{
			Kind: kind,
			Site: SiteContext{
				Title: "测试站点", Language: "zh-CN", Now: time.Now(), Generator: "Lumo test",
			},
			Theme: ThemeContext{
				Name: BuiltinName, Label: "墨", Version: "1.0.0",
				AssetsBase: "/theme-assets/ink", Settings: settings,
			},
			Find:       newFinder(t.Context(), nil, ""),
			Posts:      []PostView{},
			Pagination: newPagination(1, 10, 0, "/"),
			Params:     map[string]string{},
		}
	}

	post := &PostView{
		ID: 1, Type: "post", Title: "测试文章", Slug: "test",
		URL: "/posts/test", Content: "<p>正文</p>", Excerpt: "摘要",
		PublishedAt: time.Now(), UpdatedAt: time.Now(),
		ReadingTime: 1, WordCount: 2,
	}

	cases := []struct {
		template string
		build    func() *Context
	}{
		{"index.html", func() *Context { return base(KindIndex) }},
		{"post.html", func() *Context {
			c := base(KindPost)
			c.Post = post
			return c
		}},
		{"page.html", func() *Context {
			c := base(KindPage)
			c.Post = post
			return c
		}},
		{"404.html", func() *Context { return base(KindNotFound) }},
		{"category.html", func() *Context {
			c := base(KindCategory)
			c.Category = &testCategory
			return c
		}},
		{"tag.html", func() *Context {
			c := base(KindTag)
			c.Tag = &testTag
			return c
		}},
		{"archive.html", func() *Context {
			c := base(KindArchive)
			c.Archive = &ArchiveContext{Year: 2026, Month: 9, Label: "2026 年 9 月"}
			return c
		}},
		{"search.html", func() *Context {
			c := base(KindSearch)
			c.Query = "关键词"
			return c
		}},
		{"author.html", func() *Context {
			c := base(KindAuthor)
			c.Author = &AuthorView{ID: 1, Username: "u", DisplayName: "作者", URL: "/authors/u"}
			return c
		}},
		// 账户五个页面。它们带 Form 与 CurrentUser 才有内容，但即便这两者为零值
		// 也必须能渲染出来——主题不该因为账户模块没装配就整页报错。
		{"login.html", func() *Context { c := base(KindLogin); c.Form = NewFormState(); return c }},
		{"register.html", func() *Context { c := base(KindRegister); c.Form = NewFormState(); return c }},
		{"forgot-password.html", func() *Context {
			c := base(KindForgotPassword)
			c.Form = NewFormState()
			return c
		}},
		{"reset-password.html", func() *Context {
			c := base(KindResetPassword)
			c.Form = NewFormState()
			c.Form.Token = "token"
			return c
		}},
		{"account.html", func() *Context {
			c := base(KindAccount)
			c.Form = NewFormState()
			c.CurrentUser = &CurrentUserView{ID: 1, Username: "u", DisplayName: "会员"}
			return c
		}},
		// 收藏页。这里刻意不注入收藏实现：没装配收藏模块时它是一页空列表，
		// 而「空列表也要渲染得出来」正是本用例要盯的。
		{"favorites.html", func() *Context {
			c := base(KindFavorites)
			c.CurrentUser = &CurrentUserView{ID: 1, Username: "u", DisplayName: "会员"}
			return c
		}},
	}

	for _, tc := range cases {
		t.Run(tc.template, func(t *testing.T) {
			t.Parallel()
			var sb strings.Builder
			if err := engine.Render(&sb, tc.template, tc.build()); err != nil {
				t.Fatalf("渲染 %s 失败: %v", tc.template, err)
			}
			out := sb.String()
			if !strings.Contains(out, "<!doctype html>") {
				t.Errorf("%s 未渲染出完整文档: %.200s", tc.template, out)
			}
			if !strings.Contains(out, "测试站点") {
				t.Errorf("%s 未渲染出站点标题", tc.template)
			}
			// 模板里任何未定义的动作都会留下空白，但真正的失败是渲染出 Go 的错误占位符。
			if strings.Contains(out, "<no value>") {
				t.Errorf("%s 输出含 <no value>，说明有字段取不到: %.300s", tc.template, out)
			}
		})
	}
}

// TestBuiltinThemeAccountPagesTolerateEmptyContext 验证账户页在 Form 与 CurrentUser 都是 nil 时也能渲染。
//
// 真实站点上到不了这里——account 一定会填这两个字段。但模板对 nil 的容忍度决定了
// 第三方主题的单独预览、以及任何「只给一个 Context 就渲染」的场景会不会整页 500，
// 而 nil 指针取字段在 html/template 里是**执行期**错误，编译期看不出来。
func TestBuiltinThemeAccountPagesTolerateEmptyContext(t *testing.T) {
	t.Parallel()

	loaded, err := loadFS(BuiltinName, builtinThemeFS(t), "", true)
	if err != nil {
		t.Fatalf("内置主题加载失败: %v", err)
	}
	eng := engine(loaded)

	for _, name := range []string{
		"login.html", "register.html", "forgot-password.html", "reset-password.html", "account.html",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := &Context{
				Kind:        KindLogin,
				Site:        SiteContext{Title: "站点", Language: "zh-CN", Now: time.Now()},
				Theme:       ThemeContext{Name: BuiltinName, AssetsBase: "/theme-assets/ink"},
				Find:        newFinder(t.Context(), nil, ""),
				Params:      map[string]string{},
				CurrentUser: nil,
				Form:        nil,
			}
			var sb strings.Builder
			if err := eng.Render(&sb, name, ctx); err != nil {
				t.Fatalf("渲染 %s 失败: %v", name, err)
			}
			if strings.Contains(sb.String(), "<no value>") {
				t.Errorf("%s 输出含 <no value>: %.200s", name, sb.String())
			}
		})
	}
}

// TestBuiltinThemeDialogStaysHidden 盯住一条踩过的坑：弹窗的 display 只能写在 [open] 上。
//
// `dialog:not([open]) { display: none }` 来自 UA 样式表，而作者样式无条件赢过它。
// 在 .auth-dialog 上直接写一个 display，那条隐藏规则就整个失效——表现是**未登录的访客
// 每一页都顶着一个空弹窗**，点它还没有反应（没经过 showModal，也就没有模态层）。
//
// 读 CSS 而不是跑浏览器：这条规则的错法只有一种，而它在源码里是看得见的。
func TestBuiltinThemeDialogStaysHidden(t *testing.T) {
	t.Parallel()

	css, err := fs.ReadFile(builtinThemeFS(t), "static/theme.css")
	if err != nil {
		t.Fatalf("读取主题样式失败: %v", err)
	}

	// 逐条取出以 .auth-dialog 开头的规则块，检查裸选择器里有没有 display
	for block := range strings.SplitSeq(string(css), "}") {
		i := strings.LastIndex(block, ".auth-dialog")
		if i < 0 {
			continue
		}
		head, body, ok := strings.Cut(block[i:], "{")
		if !ok {
			continue
		}
		selector := strings.TrimSpace(head)
		// 只看 .auth-dialog 这一个选择器本身；带 [open]、::backdrop
		// 或后代选择器的都不在这条规则的约束范围内
		if selector != ".auth-dialog" {
			continue
		}
		if strings.Contains(body, "display:") || strings.Contains(body, "display :") {
			t.Error(".auth-dialog 的裸选择器上写了 display，" +
				"它会盖掉 UA 样式里的 dialog:not([open]){display:none}，" +
				"未登录的访客每一页都会看到一个空弹窗。请写到 .auth-dialog[open] 上")
		}
	}
}

// TestBuiltinThemeAuthPanelFeedsDialog 验证账户弹窗要从面板里取的三样东西。
//
// 弹窗自己不写表单，它取的是这几页里的那块 `.auth-panel`（见 static/auth.js）。
// 标题、支线出口、提交按钮的忙碌文案因此都得**长在面板之内**——
// 挪到面板外面，页面看起来分毫不差，弹窗里却会少掉对应的那一块，
// 而且不会有任何报错：标题顶着上一次的值，出口整个消失，按钮按下去没有动静。
func TestBuiltinThemeAuthPanelFeedsDialog(t *testing.T) {
	t.Parallel()

	loaded, err := loadFS(BuiltinName, builtinThemeFS(t), "", true)
	if err != nil {
		t.Fatalf("内置主题加载失败: %v", err)
	}
	eng := engine(loaded)

	cases := []struct {
		template string
		kind     string
		title    string
		// 面板底部该有的支线出口。登录那页两条，其余各一条。
		exits []string
		busy  string
	}{
		{"login.html", KindLogin, "登录", []string{`href="/forgot-password"`, `href="/register"`}, "正在登录"},
		{"register.html", KindRegister, "创建账户", []string{`href="/login"`}, "正在创建"},
		{"forgot-password.html", KindForgotPassword, "重置密码", []string{`href="/login"`}, "正在发送"},
	}
	for _, tc := range cases {
		t.Run(tc.template, func(t *testing.T) {
			t.Parallel()
			ctx := &Context{
				Kind:   tc.kind,
				Site:   SiteContext{Title: "站点", Language: "zh-CN", Now: time.Now()},
				Theme:  ThemeContext{Name: BuiltinName, AssetsBase: "/theme-assets/ink"},
				Find:   newFinder(t.Context(), nil, ""),
				Form:   NewFormState(),
				Public: registerable(),
				Params: map[string]string{"registrationOpen": "1"},
			}
			var sb strings.Builder
			if err := eng.Render(&sb, tc.template, ctx); err != nil {
				t.Fatalf("渲染失败: %v", err)
			}
			out := sb.String()

			panel := panelOf(t, out)
			if want := `data-auth-title="` + tc.title + `"`; !strings.Contains(panel, want) {
				t.Errorf("面板缺少 %s，弹窗会顶着上一次的标题", want)
			}
			for _, exit := range tc.exits {
				if !strings.Contains(panel, exit) {
					t.Errorf("面板里没有支线出口 %s", exit)
				}
			}
			if want := `data-busy="` + tc.busy + `"`; !strings.Contains(panel, want) {
				t.Errorf("提交按钮缺少 %s，弹窗里按下去不会有任何反馈", want)
			}
		})
	}
}

// panelOf 截出 data-auth-panel 那一块，断言因此只看弹窗真正会取走的部分。
func panelOf(t *testing.T, html string) string {
	t.Helper()
	start := strings.Index(html, "data-auth-panel")
	if start < 0 {
		t.Fatal("页面里没有 data-auth-panel，弹窗取不到任何东西")
	}
	// 面板一直延伸到 </section>：这几页的面板是 section 里的最后一块
	end := strings.Index(html[start:], "</section>")
	if end < 0 {
		t.Fatal("找不到面板的结尾")
	}
	return html[start : start+end]
}

// TestBuiltinThemeNavIsProgressive 验证二级菜单的结构契约。
//
// 三条：一级项有子项时是**按钮**而不是链接（用户要求点击展开、不跳转）；
// 按钮带 aria-expanded 与 aria-controls（键盘与读屏软件靠它们）；
// 子菜单首项重复父链接（否则父页面就成了死入口）。
func TestBuiltinThemeNavIsProgressive(t *testing.T) {
	t.Parallel()

	loaded, err := loadFS(BuiltinName, builtinThemeFS(t), "", true)
	if err != nil {
		t.Fatalf("内置主题加载失败: %v", err)
	}

	// 菜单由 Finder 从库里取，这里绕过查询直接预置一次结果：
	// 本用例要验的是**模板怎么渲染一棵树**，不是菜单怎么从库里读出来
	// （后者由 theme_integration_test.go 覆盖）。
	items := []MenuItemView{
		{Label: "关于", URL: "/about", Children: []MenuItemView{
			{Label: "关于", URL: "/about"},
			{Label: "联系", URL: "/contact"},
		}},
		{Label: "归档", URL: "/archives"},
	}

	settings := loaded.settings.effectiveSettings(nil)
	ctx := &Context{
		Kind:  KindIndex,
		Site:  SiteContext{Title: "站点", Language: "zh-CN", Now: time.Now()},
		Theme: ThemeContext{Name: BuiltinName, AssetsBase: "/theme-assets/ink", Settings: settings},
		Find: &Finder{
			ctx:   t.Context(),
			store: &Store{},
			cache: map[string]any{"menus:primary": items},
		},
		Posts:  []PostView{},
		Params: map[string]string{},
	}

	var sb strings.Builder
	if err := engine(loaded).Render(&sb, "index.html", ctx); err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	out := sb.String()

	for _, want := range []string{
		`class="site-nav-toggle"`,
		`aria-expanded="false"`,
		`aria-controls="site-submenu-0"`,
		`id="site-submenu-0"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("页眉缺少 %s", want)
		}
	}

	navStart := strings.Index(out, `<nav class="site-nav"`)
	subStart := strings.Index(out, `id="site-submenu-0"`)
	if navStart < 0 || subStart < 0 {
		t.Fatalf("页眉里没有导航或子菜单: %.600s", out)
	}

	// 有子项的一级项**不再**渲染成链接：点击只展开，不跳转。
	if strings.Contains(out[navStart:subStart], `<a href="/about"`) {
		t.Error("有子项的一级项不该再渲染成链接——用户要求点击即展开、不跳转")
	}

	// 子菜单首项必须指向父项的地址，否则父页面就成了死入口。
	block := out[subStart:]
	if end := strings.Index(block, "</ul>"); end >= 0 {
		block = block[:end]
	}
	anchor := strings.Index(block, "<a ")
	if anchor < 0 || !strings.HasPrefix(block[anchor:], `<a href="/about"`) {
		t.Errorf("子菜单首项不是父项链接: %.400s", block)
	}
}

// registerable 返回一份「注册三条判据都成立」的公开设置。
func registerable() map[string]map[string]any {
	return map[string]map[string]any{
		"account": {"allowRegistration": true},
		"mail":    {"enabled": true},
		"site":    {"url": "https://example.com"},
	}
}

// withPublic 复制一份公开设置并改掉其中一项。
//
// 复制而不是就地改：用例是并行跑的，共用一张 map 会互相改到对方的输入，
// 那种失败只在特定调度顺序下出现，最难查。
func withPublic(base map[string]map[string]any, group, key string, value any) map[string]map[string]any {
	out := make(map[string]map[string]any, len(base))
	for name, fields := range base {
		copied := make(map[string]any, len(fields))
		for k, v := range fields {
			copied[k] = v
		}
		out[name] = copied
	}
	out[group][key] = value
	return out
}

// TestBuiltinThemeAccountEntry 验证页眉账户入口的三种形态。
//
// 这三条互为边界，一起改才不容易漏：匿名要能看到「登录」并打开弹窗、
// 已登录要看到头像而不是别人也能看到的按钮、账户页自己不能再出一个入口。
// 最后一条不是洁癖——弹窗取的就是那些页面里的表单，那份表单带着一组固定的
// input id，页眉若在这些页面上再插一份进弹窗，同一个 id 在文档里就有两个，
// label for 会指错，整张表的标签全部失准。
func TestBuiltinThemeAccountEntry(t *testing.T) {
	t.Parallel()

	loaded, err := loadFS(BuiltinName, builtinThemeFS(t), "", true)
	if err != nil {
		t.Fatalf("内置主题加载失败: %v", err)
	}

	settings := loaded.settings.effectiveSettings(nil)
	render := func(mutate func(*Context)) string {
		t.Helper()
		ctx := &Context{
			Kind:  KindIndex,
			Site:  SiteContext{Title: "站点", Language: "zh-CN", Now: time.Now()},
			Theme: ThemeContext{Name: BuiltinName, AssetsBase: "/theme-assets/ink", Settings: settings},
			Find: &Finder{
				ctx:   t.Context(),
				store: &Store{},
				cache: map[string]any{"menus:primary": []MenuItemView{}},
			},
			Public: registerable(),
			Posts:  []PostView{},
			Params: map[string]string{},
		}
		mutate(ctx)
		var sb strings.Builder
		if err := engine(loaded).Render(&sb, "index.html", ctx); err != nil {
			t.Fatalf("渲染失败: %v", err)
		}
		return sb.String()
	}

	t.Run("匿名显示登录注册并带弹窗", func(t *testing.T) {
		t.Parallel()
		out := render(func(*Context) {})
		for _, want := range []string{
			`data-auth-tab="login"`,
			`data-auth-tab="register"`,
			`<dialog class="auth-dialog" id="site-auth"`,
			// 标题与关闭键是弹窗自己的两件家具，脚本按这两个标记找它们；
			// 少了标题它会一直顶着初值，少了关闭键就只剩 Esc 这一条出路。
			`data-auth-title`,
			`data-auth-close`,
			`data-auth-body`,
		} {
			if !strings.Contains(out, want) {
				t.Errorf("匿名页眉缺少 %s", want)
			}
		}
		// 弹窗靠 aria-labelledby 指向自己的标题；对不上就是一个无名的对话框
		if !strings.Contains(out, `aria-labelledby="site-auth-title"`) ||
			!strings.Contains(out, `id="site-auth-title"`) {
			t.Error("弹窗的 aria-labelledby 与标题 id 没有对上")
		}
	})

	// 「注册」入口的三条判据在模板里重写了一遍（主题只能读公开设置），
	// 与账户模块的 registrationOpen 是同一条规则。这里按行为逐条核对，
	// 因为两处分叉的表现恰好是最难看的一种：入口看得见，点进去是一张「暂未开放」。
	t.Run("注册入口要三条都成立", func(t *testing.T) {
		t.Parallel()
		cases := []struct {
			name   string
			public map[string]map[string]any
			want   bool
		}{
			{"三条都成立", registerable(), true},
			{
				"开关没开",
				withPublic(registerable(), "account", "allowRegistration", false),
				false,
			},
			{
				"发不出信",
				withPublic(registerable(), "mail", "enabled", false),
				false,
			},
			{
				"站点没配对外地址",
				withPublic(registerable(), "site", "url", ""),
				false,
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				out := render(func(c *Context) { c.Public = tc.public })
				got := strings.Contains(out, `data-auth-tab="register"`)
				if got != tc.want {
					t.Errorf("有注册入口 = %v，期望 %v", got, tc.want)
				}
				// 登录入口与这三条都无关，任何组合下都该在
				if !strings.Contains(out, `data-auth-tab="login"`) {
					t.Error("登录入口与注册开关无关，应当一直在")
				}
			})
		}
	})

	t.Run("已登录显示首字头像且没有弹窗", func(t *testing.T) {
		t.Parallel()
		out := render(func(c *Context) {
			c.CurrentUser = &CurrentUserView{ID: 1, Username: "u", DisplayName: "张三"}
		})
		if !strings.Contains(out, `class="site-avatar"`) {
			t.Error("已登录应显示头像")
		}
		if !strings.Contains(out, `<span class="avatar-fallback" aria-hidden="true">张</span>`) {
			t.Errorf("没有头像图时应落显示名的第一个字: %.400s", out)
		}
		if strings.Contains(out, `id="site-auth"`) {
			t.Error("已登录没有打开弹窗的入口，DOM 里不该留着它")
		}
		if strings.Contains(out, `data-auth-tab="login"`) {
			t.Error("已登录不该再有「登录」入口")
		}
	})

	t.Run("填了头像地址就用图", func(t *testing.T) {
		t.Parallel()
		out := render(func(c *Context) {
			c.CurrentUser = &CurrentUserView{
				ID: 1, Username: "u", DisplayName: "张三", AvatarURL: "/uploads/a.png",
			}
		})
		// 只看头像那个 <a> 里面：整页别处（空状态、404 一类）本来就盖着印，
		// 在整段 HTML 上找首字标记是在找页面里的别的东西，不是这一枚。
		_, avatar, ok := strings.Cut(out, `class="site-avatar"`)
		if !ok {
			t.Fatalf("页眉里没有头像: %.400s", out)
		}
		avatar, _, _ = strings.Cut(avatar, "</a>")
		if !strings.Contains(avatar, `src="/uploads/a.png"`) {
			t.Errorf("用户上传了头像就该用图，而不是落首字: %.300s", avatar)
		}
		if strings.Contains(avatar, "avatar-fallback") {
			t.Errorf("用了图就不该再渲染首字: %.300s", avatar)
		}
	})

	t.Run("账户页自己不再出账户入口", func(t *testing.T) {
		t.Parallel()
		out := render(func(c *Context) { c.Kind = KindLogin })
		if strings.Contains(out, `class="site-account"`) || strings.Contains(out, `id="site-auth"`) {
			t.Error("站在登录页上，页眉再放一个入口只是噪音，且会与页面里的表单撞 id")
		}
	})
}

// fakeFavorites 是收藏能力的假实现，供模板用例注入。
//
// 用假实现而不是连库：本组用例要验的是**模板按状态渲染成什么样**，
// 而收藏是怎么存的由 internal/favorite 的集成测试覆盖。
type fakeFavorites struct {
	has   bool
	count int
}

func (f fakeFavorites) FavoritePostIDs(context.Context, int64, int, int) (
	ids []int64, total int, err error,
) {
	return nil, 0, nil
}

func (f fakeFavorites) HasFavorite(context.Context, int64, int64) (bool, error) {
	return f.has, nil
}

func (f fakeFavorites) CountFavorites(context.Context, int64) (int, error) {
	return f.count, nil
}

// viewerContext 返回一个带登录态的 context，供 FavoritesFinder.Has 取「当前是谁」。
func viewerContext(t *testing.T, userID int64) context.Context {
	t.Helper()
	return auth.WithPrincipal(t.Context(), &auth.Principal{User: &auth.User{ID: userID}})
}

// TestBuiltinThemeAccountMenu 验证页眉账户菜单的四条分支各自按条件出现。
//
// 四条都是「看得见就必须点得到」的判断，错一条的表现都不是报错而是一个骗人的入口：
// 没权限的人看到「管理后台」（点进去一屏 403）、没装配收藏的站点看到「我的收藏」
// （点进去永远空着）、拿不到令牌却渲染了退出登录（点了必被 CSRF 挡下）。
func TestBuiltinThemeAccountMenu(t *testing.T) {
	t.Parallel()

	loaded, err := loadFS(BuiltinName, builtinThemeFS(t), "", true)
	if err != nil {
		t.Fatalf("内置主题加载失败: %v", err)
	}
	settings := loaded.settings.effectiveSettings(nil)

	render := func(mutate func(*Context)) string {
		t.Helper()
		ctx := &Context{
			Kind:  KindIndex,
			Site:  SiteContext{Title: "站点", Language: "zh-CN", Now: time.Now()},
			Theme: ThemeContext{Name: BuiltinName, AssetsBase: "/theme-assets/ink", Settings: settings},
			Find: &Finder{
				ctx:   viewerContext(t, 1),
				store: &Store{},
				cache: map[string]any{"menus:primary": []MenuItemView{}},
			},
			Public:      registerable(),
			Posts:       []PostView{},
			Params:      map[string]string{},
			CurrentUser: &CurrentUserView{ID: 1, Username: "zhangsan", DisplayName: "张三"},
		}
		mutate(ctx)
		var sb strings.Builder
		if err := engine(loaded).Render(&sb, "index.html", ctx); err != nil {
			t.Fatalf("渲染失败: %v", err)
		}
		return sb.String()
	}

	t.Run("菜单初始收起且触发器是普通链接", func(t *testing.T) {
		t.Parallel()
		out := render(func(*Context) {})
		if !strings.Contains(out, `<div class="account-menu" id="site-account-menu" hidden>`) {
			t.Errorf("菜单应当整块渲染好并带 hidden: %.500s", out)
		}
		// 没有 JS 时头像必须还是那个能点去账户页的链接，菜单里的每一项在那一页上都有。
		if !strings.Contains(out, `<a class="site-avatar" href="/account" data-account-trigger`) {
			t.Errorf("触发器应当是指向 /account 的链接: %.500s", out)
		}
		if !strings.Contains(out, "@zhangsan") {
			t.Error("菜单顶部应当显示用户名")
		}
	})

	t.Run("有权限才显示管理后台且新窗口打开", func(t *testing.T) {
		t.Parallel()
		out := render(func(c *Context) { c.CurrentUser.ConsoleAccess = true })
		if !strings.Contains(out, `<a href="/console/" target="_blank" rel="noopener noreferrer">`) {
			t.Errorf("有权限时应显示管理后台，且新窗口打开并带 noopener: %.600s", out)
		}

		out = render(func(*Context) {})
		if strings.Contains(out, `href="/console/"`) {
			t.Error("零权限账号不该看到一个点进去就是 403 的后台入口")
		}
	})

	t.Run("拿不到表单令牌就不渲染退出登录", func(t *testing.T) {
		t.Parallel()
		out := render(func(c *Context) { c.CSRFToken = "tok" })
		if !strings.Contains(out, `<form class="account-menu-logout" method="post" action="/logout">`) {
			t.Errorf("有令牌时应渲染退出登录表单: %.600s", out)
		}
		if !strings.Contains(out, `name="_csrf" value="tok"`) {
			t.Error("退出登录表单应带上令牌")
		}

		out = render(func(*Context) {})
		if strings.Contains(out, `action="/logout"`) {
			t.Error("没有令牌时那张表单必被 CSRF 挡下，不该渲染出来")
		}
	})

	t.Run("装配了收藏才显示我的收藏", func(t *testing.T) {
		t.Parallel()
		out := render(func(c *Context) {
			c.Find.store = &Store{favorites: fakeFavorites{}}
		})
		if !strings.Contains(out, `href="/account/favorites"`) {
			t.Errorf("装配收藏后菜单里应有我的收藏: %.600s", out)
		}

		out = render(func(*Context) {})
		if strings.Contains(out, `href="/account/favorites"`) {
			t.Error("没装配收藏模块时不该给一个永远空着的入口")
		}
	})
}

// TestBuiltinThemeAccountMenuStaysHidden 盯住与 auth-dialog 同一类的坑：
// 给 .account-menu 写了 display: flex，就必须再写一条 [hidden] 的 display: none。
//
// 作者样式无条件赢过 UA 那条 [hidden] { display: none }——少了它，
// 每一个已登录访客的每一页都会在页眉底下挂着一块摊开的菜单，点头像也收不起来。
// 读 CSS 而不是跑浏览器：这条规则的错法只有一种，而它在源码里看得见。
func TestBuiltinThemeAccountMenuStaysHidden(t *testing.T) {
	t.Parallel()

	css := readThemeFile(t, "static/theme.css")
	open := strings.Index(css, ".account-menu {")
	hidden := strings.Index(css, ".account-menu[hidden]")
	if open < 0 {
		t.Fatal("theme.css 里没有 .account-menu 规则")
	}
	if hidden < 0 {
		t.Fatal("theme.css 缺少 .account-menu[hidden] 的 display: none")
	}
	if hidden < open {
		t.Error(".account-menu[hidden] 必须写在 display 那条之后，否则同特异性下后者获胜")
	}
	block := css[hidden:]
	if end := strings.Index(block, "}"); end >= 0 {
		block = block[:end]
	}
	if !strings.Contains(block, "display: none") {
		t.Errorf(".account-menu[hidden] 里应当是 display: none: %q", block)
	}
}

// TestBuiltinThemeFavoriteButton 验证文末收藏按钮的几种形态。
//
// 它们各自对应一种真实状态，错一种的表现都是「点了没反应」：
// 未装配收藏模块时不该有按钮、匿名访客点了要去登录、已登录才是那个能按的开关。
func TestBuiltinThemeFavoriteButton(t *testing.T) {
	t.Parallel()

	loaded, err := loadFS(BuiltinName, builtinThemeFS(t), "", true)
	if err != nil {
		t.Fatalf("内置主题加载失败: %v", err)
	}

	render := func(store *Store, mutate func(*Context)) string {
		t.Helper()
		// 相关文章与评论都要连库，而本用例给的是一个没有 db 的 Store（收藏用假实现注入）。
		// 从设置里关掉这两块，比给 Finder 逐个预置缓存键清楚——
		// 它们由站长的开关控制本来就是模板的既有行为。
		settings := loaded.settings.effectiveSettings(nil)
		settings["content"]["showRelated"] = false
		settings["content"]["showComments"] = false
		ctx := &Context{
			Kind:  KindPost,
			Site:  SiteContext{Title: "站点", Language: "zh-CN", Now: time.Now()},
			Theme: ThemeContext{Name: BuiltinName, AssetsBase: "/theme-assets/ink", Settings: settings},
			// 菜单查询要连库，这里预置一份空菜单绕开它：本用例验的是文末那个按钮。
			Find: &Finder{
				ctx:   viewerContext(t, 1),
				store: store,
				cache: map[string]any{"menus:primary": []MenuItemView{}},
			},
			Path: "/posts/test",
			Post: &PostView{
				ID: 7, Type: "post", Title: "测试文章", Slug: "test",
				URL: "/posts/test", Content: "<p>正文</p>", PublishedAt: time.Now(),
			},
			Posts:  []PostView{},
			Params: map[string]string{},
		}
		mutate(ctx)
		var sb strings.Builder
		if err := engine(loaded).Render(&sb, "post.html", ctx); err != nil {
			t.Fatalf("渲染失败: %v", err)
		}
		return sb.String()
	}

	t.Run("没装配收藏就整块不渲染", func(t *testing.T) {
		t.Parallel()
		out := render(&Store{}, func(*Context) {})
		if strings.Contains(out, "article-actions") || strings.Contains(out, "favorite.js") {
			t.Error("没装配收藏模块时，文末不该有一个点了必然报错的按钮")
		}
	})

	t.Run("匿名给一个去登录的链接", func(t *testing.T) {
		t.Parallel()
		out := render(&Store{favorites: fakeFavorites{count: 3}}, func(*Context) {})
		// 回跳地址由 html/template 在查询串上下文里百分号编码（/ → %2f），
		// account 的 safeNext 读的是解码后的值，两边对得上。
		if !strings.Contains(out, `href="/login?next=%2fposts%2ftest" data-auth-tab="login"`) {
			t.Errorf("匿名访客点收藏应当去登录并带回跳地址: %.800s", out)
		}
		if strings.Contains(out, "data-favorite ") {
			t.Error("匿名访客不该拿到那个切换按钮")
		}
		// 收藏数对所有人可见。
		if !strings.Contains(out, "3 人收藏") {
			t.Error("收藏数应当渲染在按钮旁边")
		}
	})

	t.Run("已登录是带状态的开关按钮", func(t *testing.T) {
		t.Parallel()
		store := &Store{favorites: fakeFavorites{has: true, count: 1}}
		out := render(store, func(c *Context) {
			c.CurrentUser = &CurrentUserView{ID: 1, Username: "u", DisplayName: "张三"}
		})
		// hidden 是渐进增强的关键：切换要走接口，没有 JS 时不给按钮。
		if !strings.Contains(out, `data-favorite data-post-id="7"`) {
			t.Errorf("已登录应当渲染切换按钮: %.800s", out)
		}
		if !strings.Contains(out, `favorite-toggle" type="button" hidden`) {
			t.Errorf("切换按钮必须带 hidden，由 favorite.js 放出来: %.800s", out)
		}
		if !strings.Contains(out, `aria-pressed="true"`) || !strings.Contains(out, "已收藏") {
			t.Errorf("已收藏的初态应当渲染在服务端，而不是等脚本再问一次: %.800s", out)
		}
	})

	t.Run("未收藏时收藏数为零不占位", func(t *testing.T) {
		t.Parallel()
		store := &Store{favorites: fakeFavorites{has: false, count: 0}}
		out := render(store, func(c *Context) {
			c.CurrentUser = &CurrentUserView{ID: 1, Username: "u", DisplayName: "张三"}
		})
		if !strings.Contains(out, `aria-pressed="false"`) {
			t.Error("未收藏时 aria-pressed 应为 false")
		}
		if !strings.Contains(out, `data-favorite-count role="status" hidden`) {
			t.Errorf("收藏数为零时不该占一行: %.800s", out)
		}
	})
}

// TestBuiltinThemeEscapesContent 验证正文之外的字段都经过转义。
//
// 只有 safeHTML 标注的正文与评论 HTML 才允许原样输出，其余一律转义——
// 主题是 XSS 面的所在，这条一旦破了，每个装这个主题的站点都中招。
func TestBuiltinThemeEscapesContent(t *testing.T) {
	t.Parallel()

	loaded, err := loadFS(BuiltinName, builtinThemeFS(t), "", true)
	if err != nil {
		t.Fatalf("内置主题加载失败: %v", err)
	}

	const payload = `<script>alert(1)</script>`
	ctx := &Context{
		Kind: KindPost,
		Site: SiteContext{Title: payload, Description: payload, Now: time.Now()},
		Theme: ThemeContext{
			Name: BuiltinName, AssetsBase: "/theme-assets/ink",
			Settings: loaded.settings.effectiveSettings(nil),
		},
		Find: newFinder(t.Context(), nil, ""),
		Post: &PostView{
			ID: 1, Type: "post", Title: payload, Slug: "x", URL: "/posts/x",
			Excerpt: payload, Content: "<p>正文</p>",
		},
		Params: map[string]string{},
	}

	var sb strings.Builder
	if err := engine(loaded).Render(&sb, "post.html", ctx); err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	out := sb.String()
	if strings.Contains(out, "<script>alert(1)</script>") {
		t.Fatalf("标题或摘要未被转义，输出含可执行脚本")
	}
	if !strings.Contains(out, "&lt;script&gt;") {
		t.Errorf("未找到转义后的内容，输出可能不含这些字段: %.300s", out)
	}
	// 正文经 safeHTML 原样输出，这是唯一的例外。
	if !strings.Contains(out, "<p>正文</p>") {
		t.Error("正文应经 safeHTML 原样输出")
	}
}

// engine 是取引擎的测试辅助，避免在断言里反复写 .Engine()。
func engine(l *Loaded) *Engine { return l.Engine() }

// TestBuiltinThemeRendersHighlightedCode 验证代码块的高亮结构真的落到了页面上。
//
// 这条守的是「服务端产出的结构」与「主题的 CSS/JS」之间的那道缝：
// 两边各自都对，中间对不上（类名改了、外壳没了、脚本没引）时页面看不出报错，
// 只是代码块退化成一段没有行号的灰底文本。
func TestBuiltinThemeRendersHighlightedCode(t *testing.T) {
	t.Parallel()

	loaded, err := loadFS(BuiltinName, builtinThemeFS(t), "", true)
	if err != nil {
		t.Fatalf("内置主题加载失败: %v", err)
	}

	// 走与前台一致的顺序：先渲染原稿，再过高亮。
	rendered, err := content.Render(content.RawMarkdown,
		"```go\nfunc main() {\n\tfmt.Println(\"hi\")\n}\n```\n")
	if err != nil {
		t.Fatalf("渲染 Markdown 失败: %v", err)
	}
	highlighted := content.Highlight(rendered)

	ctx := &Context{
		Kind: KindPost,
		Site: SiteContext{Title: "站点", Language: "zh-CN", Now: time.Now()},
		Theme: ThemeContext{
			Name: BuiltinName, AssetsBase: "/theme-assets/ink",
			Settings: loaded.settings.effectiveSettings(nil),
		},
		Find: newFinder(t.Context(), nil, ""),
		Post: &PostView{
			ID: 1, Type: "post", Title: "文章", Slug: "x", URL: "/posts/x",
			Content: highlighted,
		},
		Params: map[string]string{},
	}

	var sb strings.Builder
	if err := engine(loaded).Render(&sb, "post.html", ctx); err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	out := sb.String()

	for _, want := range []string{
		`class="code-block"`,        // 高亮外壳
		`class="code-lines"`,        // 行号容器
		`<span class="line">`,       // 逐行结构（CSS 计数器挂在这里）
		`/theme-assets/ink/code.js`, // 三个按钮的脚本
	} {
		if !strings.Contains(out, want) {
			t.Errorf("页面缺少 %s", want)
		}
	}
	// 行号是 CSS 生成的，DOM 里不能出现 chroma 的行号元素——
	// 否则读者复制代码会把行号一起带走。
	if strings.Contains(out, `class="ln"`) {
		t.Error("行号不该进 DOM")
	}
}

// TestBuiltinThemeCodeScriptNotGatedByMotion 验证复制按钮不受「页面动效」开关管辖。
//
// 复制是功能不是装饰：站长关掉动效后按钮还得在。
func TestBuiltinThemeCodeScriptNotGatedByMotion(t *testing.T) {
	t.Parallel()

	loaded, err := loadFS(BuiltinName, builtinThemeFS(t), "", true)
	if err != nil {
		t.Fatalf("内置主题加载失败: %v", err)
	}

	settings := loaded.settings.effectiveSettings(map[string]map[string]any{
		"appearance": {"motion": false, "webFont": false},
	})
	ctx := &Context{
		Kind:  KindPost,
		Site:  SiteContext{Title: "站点", Language: "zh-CN", Now: time.Now()},
		Theme: ThemeContext{Name: BuiltinName, AssetsBase: "/theme-assets/ink", Settings: settings},
		Find:  newFinder(t.Context(), nil, ""),
		Post: &PostView{
			ID: 1, Type: "post", Title: "文章", Slug: "x", URL: "/posts/x",
			Content: `<p>x</p>`,
		},
		Params: map[string]string{},
	}

	var sb strings.Builder
	if err := engine(loaded).Render(&sb, "post.html", ctx); err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	out := sb.String()

	if !strings.Contains(out, "/theme-assets/ink/code.js") {
		t.Error("关掉动效后 code.js 仍应加载")
	}
	if strings.Contains(out, "gsap.min.js") {
		t.Error("关掉动效后不该再加载 GSAP")
	}
}

// TestBuiltinThemeSettingsCompile 验证内置主题的设置声明可编译且缺省值自洽。
func TestBuiltinThemeSettingsCompile(t *testing.T) {
	t.Parallel()

	decl, err := readSettingsFS(builtinThemeFS(t))
	if err != nil {
		t.Fatalf("读取设置声明失败: %v", err)
	}
	compiled, err := compileSettings(BuiltinName, decl)
	if err != nil {
		t.Fatalf("编译设置失败: %v", err)
	}
	if len(compiled.groups) == 0 {
		t.Fatal("内置主题应声明设置分组")
	}
	for _, g := range compiled.groups {
		if err := g.validator.Validate(g.defaults); err != nil {
			t.Errorf("分组 %q 的缺省值未通过自身 schema: %v", g.Name, err)
		}
	}
	// 三个分组是本主题的设计约定（外观 / 版式 / 内容）。
	for _, name := range []string{"appearance", "layout", "content"} {
		if _, ok := compiled.group(name); !ok {
			t.Errorf("缺少设置分组 %q", name)
		}
	}
}

// TestValidateBuiltinAsPackage 验证内置主题同样能通过第三方主题的校验规则。
//
// 内置主题若通不过自己的校验器，说明规则与实现脱节了。
func TestValidateBuiltinAsPackage(t *testing.T) {
	t.Parallel()

	fsys := builtinThemeFS(t)
	manifest, err := readManifestFS(fsys)
	if err != nil {
		t.Fatalf("读取元信息失败: %v", err)
	}
	templatesFS, err := fs.Sub(fsys, DirTemplates)
	if err != nil {
		t.Fatalf("缺少模板目录: %v", err)
	}
	names, err := collectTemplateNames(templatesFS)
	if err != nil {
		t.Fatalf("遍历模板失败: %v", err)
	}
	if err := validateTemplates(names); err != nil {
		t.Fatalf("必需模板校验失败: %v", err)
	}
	if _, err := parseEngine(manifest.Name, templatesFS, baseFuncs()); err != nil {
		t.Fatalf("模板解析失败: %v", err)
	}
}
