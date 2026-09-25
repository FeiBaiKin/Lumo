package theme

import (
	"context"
	"html/template"
	"time"

	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/hooks"

	"github.com/FeiBaiKin/lumo/internal/content"
	"github.com/FeiBaiKin/lumo/internal/taxonomy"
)

// 路由种类。每种路由注入固定的、有文档的上下文，
// 保证主题作者「照着写就能跑」。
const (
	KindIndex    = "index"
	KindPost     = "post"
	KindPage     = "page"
	KindCategory = "category"
	KindTag      = "tag"
	KindArchive  = "archive"
	KindSearch   = "search"
	KindAuthor   = "author"
	KindNotFound = "404"

	// KindPosts 是全部文章的分页列表（/posts）。首页第一页可能摆的是模块，
	// 这里才是一篇不落的完整列表。
	KindPosts = "posts"

	// KindFavorites 是「我的收藏」页。它由本模块渲染而不是 account——
	// 那一页上是一列文章，而按可见性取文章、补作者分类标签、算分页
	// 全都在本包里（account 不为内容提供任何接口）。
	KindFavorites = "favorites"

	// 以下五种由 account 模块注入。
	KindLogin          = "login"
	KindRegister       = "register"
	KindForgotPassword = "forgot-password"
	KindResetPassword  = "reset-password"
	KindAccount        = "account"
)

// Context 是注入模板的根对象，模板里以 `.` 访问。
//
// 所有路由共用一个结构体而非每种路由一个类型：模板作者只需记住一套字段名，
// 且 partial 可以无差别地接收任何页面的上下文（页头页脚要用 Site 与 Menus，
// 而它们在每种页面上都得在）。与具体路由相关的字段按需填充，其余为零值。
type Context struct {
	// Kind 是当前路由种类，取值见 Kind* 常量。模板可据此在 partial 里分支。
	Kind string
	// Site 是站点级信息，每个页面都有。
	Site SiteContext
	// Theme 是当前主题的信息与设置值。
	Theme ThemeContext
	// Title 是本页标题（不含站点标题后缀），主题自行拼接。
	Title string
	// Description 是本页描述，供 meta 使用。
	Description string
	// Canonical 是本页的绝对地址；站点未配置对外地址时为相对路径。
	Canonical string
	// SEO 是本页的 SEO 派生信息，由 Renderer 按站点设置与本页内容算好。
	SEO SEOContext
	// Path 是当前请求路径。
	Path string

	// Post 在文章页与独立页面上为当前内容，其余为 nil。
	Post *PostView
	// Posts 是列表类页面（首页、分类、标签、归档、搜索、作者）的内容列表。
	Posts []PostView
	// Pagination 是列表类页面的翻页信息。
	Pagination Pagination

	// Category 在分类页上为当前分类。
	Category *taxonomy.Category
	// Tag 在标签页上为当前标签。
	Tag *taxonomy.Tag
	// Author 在作者页上为当前作者。
	Author *AuthorView
	// Archive 在归档页上为当前归档区间。
	Archive *ArchiveContext
	// Query 在搜索页上为搜索词。
	Query string

	// Find 是只读数据访问面，供侧栏页脚一类小部件取数。
	//
	// 为什么挂在上下文上而不是做成 posts.recent 这样的顶层模板函数：
	// 模板函数在解析期就固定了，而 Finder 必须绑定当次请求的 context
	// （超时、取消、请求内缓存都依赖它）。要让顶层函数拿到请求 context，
	// 只能每个请求 template.Clone 一份——而 html/template 的转义分析结果
	// 缓存在模板实例上，克隆意味着每个请求都重跑一遍全量转义分析，
	// 这是前台每次访问都要付的代价。故改为 {{ .Find.Posts.Recent 5 }}。
	Find *Finder

	// Params 是路由未消费的额外参数，供主题自行取用。
	Params map[string]string

	// Public 是各设置分组中标为公开的字段值，按分组名索引，
	// 如 account.allowRegistration。模板里用 .Setting 取，不要直接索引。
	//
	// 为什么主题需要它：页眉要显示「注册」入口，就得知道注册是不是开着，
	// 而页眉出现在**每一个**页面上——由账户模块渲染时逐页塞进 Params 是不行的。
	// 取值走 settings 分组的 Public 白名单（与 Public 平面接口同一份声明），
	// 主题拿不到未公开的字段。
	Public map[string]map[string]any

	// CurrentUser 是当前登录用户，匿名时为 nil。
	//
	// 只放前台该知道的字段：邮箱与验证时间不在其中，页眉不该把用户的注册邮箱渲染出来。
	// 由 Renderer.NewContext 从 auth 的 principal 填充——根路由上没有鉴权中间件，
	// 没有 auth.Authenticator.Optional 挂在前台，这个字段就恒为 nil。
	CurrentUser *CurrentUserView

	// CSRFToken 是本页可用的表单令牌，供**页面自带**的表单使用——
	// 目前只有页眉账户菜单里的退出登录那一张。
	//
	// 只对已登录访客签发（匿名页因此仍然可以被共享缓存），由 Renderer.Render
	// 在渲染前填入；没有接上签发钩子时为空串，模板据此不渲染那张表单。
	// 表单页（登录、注册、账户等）请用 .Form.CSRFToken，那是同一枚令牌。
	CSRFToken string

	// plugins 与 reqCtx 供 Slot 与 Widget 调插件：插件渲染要绑定当次请求的超时与取消。
	plugins app.Frontend
	reqCtx  context.Context

	// Form 是表单页的状态：回填值、逐字段错误、提示与令牌。
	//
	// 做成显式结构而不是塞进 Params：值与错误混在一个 map 里，
	// 主题作者要靠键名前缀区分，那是约定而不是契约。
	Form *FormState
}

// Slot 输出插件放进某个插槽的内容：head、footer、content.before、content.after、comments.after。
//
// 主题在对应位置各调一次，如 {{ .Slot "head" }} 放在 </head> 之前、{{ .Slot "footer" }} 放在 </body> 之前。
// 插件的样式表与脚本也是经 head 与 footer 两个插槽放进来的；主题不调它们，插件就进不了前台。
// 内容已由插件模块净化，原样输出即可。
func (c *Context) Slot(name string) template.HTML {
	if c == nil || c.plugins == nil {
		return ""
	}
	return c.plugins.Slot(c.requestContext(), name, c.pluginPage())
}

// PluginWidget 是一个插件小组件渲染出的内容。
type PluginWidget struct {
	// Label 是插件给小组件起的名字，站长没填标题时可用作标题。
	Label string
	HTML  template.HTML
}

// Widget 渲染插件提供的侧栏小组件，id 形如 <插件>/<小组件>；插件没启用或没有内容时为 nil。
func (c *Context) Widget(id string) *PluginWidget {
	if c == nil || c.plugins == nil || id == "" {
		return nil
	}
	label, html, ok := c.plugins.Widget(c.requestContext(), id, c.pluginPage())
	if !ok {
		return nil
	}
	return &PluginWidget{Label: label, HTML: html}
}

func (c *Context) requestContext() context.Context {
	if c.reqCtx != nil {
		return c.reqCtx
	}
	return context.Background()
}

// pluginPage 是交给插件的当前页面。
func (c *Context) pluginPage() *hooks.Page {
	page := &hooks.Page{Kind: c.Kind, Path: c.Path, Title: c.Title}
	if c.Post != nil {
		page.Post = &hooks.PostRef{ID: c.Post.ID, Type: c.Post.Type, Title: c.Post.Title, Path: c.Post.URL}
	}
	return page
}

// Setting 读取某个设置分组的公开字段值；分组未注册或字段未公开时返回 nil。
//
// 做成方法而不是让模板写 {{ index (index .Public "account") "allowRegistration" }}：
// 嵌套索引在模板里读不出意图，而且少写一层就会撞上「nil map 没有这个键」的渲染错误。
// 缺键返回 nil 而不是报错是有意的——第三方主题引用一个本项目不存在的分组时，
// 该少显示一个链接，而不是让整站 500。
func (c *Context) Setting(group, key string) any {
	if c == nil || c.Public == nil {
		return nil
	}
	return c.Public[group][key]
}

// CurrentUserView 是注入模板的当前登录用户视图。
type CurrentUserView struct {
	ID          int64
	Username    string
	DisplayName string
	AvatarURL   string
	// ConsoleAccess 为真表示该账号至少有一条权限，页眉可以显示「进入后台」。
	//
	// 判据是「有没有权限」而不是「角色名是不是管理员」：自定义角色同样可能持有权限，
	// 而 member 这类零权限账号点进后台只会得到一屏 403。
	ConsoleAccess bool
}

// FormState 是表单页的渲染状态。
//
// 密码字段**永远不进 Values**：回填密码意味着它会被写进 HTML，
// 而 HTML 会进浏览器缓存、会被「查看源代码」看到、会被截图带出去。
type FormState struct {
	// Values 是回填值，绝不含任何密码字段。
	Values map[string]string
	// Errors 的键是字段名，值是给用户看的中文原因。
	Errors map[string]string
	// Notice 是页面级提示（成功或失败），空串表示不显示。
	Notice string
	// NoticeKind 取值 "ok" 或 "error"，模板据此选样式。
	NoticeKind string
	// CSRFToken 要渲染进表单的 hidden 字段。
	CSRFToken string
	// Token 是重置密码页要把邮件里的令牌带回 POST 的隐藏字段。
	Token string
}

// NewFormState 构造一个空的表单状态，映射已初始化。
//
// 让 account 模块不必每处都手写 make：漏掉一个，模板里的 .Form.Values.xxx 就会
// 报「nil map」而不是渲染成空值。
func NewFormState() *FormState {
	return &FormState{Values: map[string]string{}, Errors: map[string]string{}}
}

// SiteContext 是站点级信息，取自 site 设置分组。
type SiteContext struct {
	Title       string
	Subtitle    string
	Description string
	// URL 是站点对外地址（不含尾斜杠）；未配置时为空串。
	URL        string
	Language   string
	LogoURL    string
	FaviconURL string
	// Now 是渲染时刻，已按站点时区换算，供页脚年份一类用途。
	Now time.Time
	// Generator 是生成器标识，形如 Lumo 1.0.0。
	Generator string
}

// ThemeContext 是当前主题的信息与设置值。
type ThemeContext struct {
	Name    string
	Label   string
	Version string
	// Settings 是主题设置的有效值，按分组名索引，如 .Theme.Settings.appearance.accentColor。
	Settings map[string]map[string]any
	// AssetsBase 是本主题静态资源的访问前缀，如 /themes/ink/static。
	AssetsBase string
	// AssetsVersion 是静态资源指纹，模板挂在 URL 上做缓存失效（见 Loaded.AssetVersion）。
	AssetsVersion string
}

// SEOContext 是 SEO 设置与结构化数据在模板里的出口。
//
// 主题不必自己去 .Public.seo 里挖那几项再做回落：标题后缀、默认描述、
// 默认分享图的优先级规则是站点行为，不该由每个主题各写一遍。
type SEOContext struct {
	// TitleSuffix 是 SEO 设置里的标题后缀，主题拼在 <title> 之后。
	TitleSuffix string
	// Image 是本页分享图的绝对地址；内容没有封面时回落到默认分享图。
	Image string
	// TwitterSite 是站点的 Twitter 账号（含 @），为空则不渲染 twitter:site。
	TwitterSite string
	// JSONLD 是本页的 schema.org 结构化数据，已序列化；没有数据时为空。
	//
	// 类型是 template.JS，模板里直接输出进 <script type="application/ld+json">。
	// 内容经 encoding/json 序列化，其中的尖括号与 & 已转成 Unicode 转义形式。
	JSONLD template.JS
}

// PostView 是注入模板的内容视图。
//
// 不直接用 content.Post：那是数据库实体，带着 Raw 原稿与 Meta 这些主题不该碰的字段。
// 主题只消费渲染后的 Content，编辑器换了主题也不受影响。
type PostView struct {
	ID    int64
	Type  string
	Title string
	Slug  string
	// URL 是本内容的站内路径，如 /posts/hello。
	URL string
	// Content 是渲染后的 HTML。模板里须用 safeHTML 输出。
	Content string
	// source 是高亮之前的正文，content.render 过滤器处理的是它（高亮的结构经不起再净化一遍）。
	source  string
	Excerpt string
	// CoverURL 是封面图地址，可能为空。
	CoverURL string
	Pinned   bool
	// Template 是页面选择的主题模板名（仅独立页面），如 page-about。
	Template   string
	Author     *AuthorView
	Categories []taxonomy.Category
	// Tags 用 TermView 而不是 taxonomy.Tag：它要带一个派生出来的色相（Hue），
	// 而色相是「怎么画」的事，不该塞进核心的领域模型里。
	Tags        []TermView
	PublishedAt time.Time
	UpdatedAt   time.Time
	// ReadingTime 是估算阅读分钟数。
	ReadingTime int
	// WordCount 是正文字数（CJK 按字、拉丁按词）。
	WordCount int
}

// AuthorView 是作者的公开信息。
//
// 邮箱不在其中：作者页是公开页面，把注册邮箱渲染上去等于给爬虫送礼。
type AuthorView struct {
	ID          int64
	Username    string
	DisplayName string
	AvatarURL   string
	Bio         string
	// URL 是作者归档页地址。
	URL string
}

// ArchiveContext 描述归档页的时间区间。
type ArchiveContext struct {
	Year int
	// Month 为 0 表示按年归档。
	Month int
	// Label 是可读标题，如「2026 年 9 月」。
	Label string
}

// Pagination 是列表页的翻页信息。
//
// 字段全部预先算好而不是让模板自己算：分页边界是最容易写错的地方，
// 每个主题各算一遍必然各错一遍。
type Pagination struct {
	// Page 是当前页码，从 1 起。
	Page int
	// Size 是每页条数。
	Size int
	// Total 是总条数。
	Total int
	// TotalPages 是总页数，至少为 1。
	TotalPages int
	// HasPrev / HasNext 表示是否存在上一页 / 下一页。
	HasPrev bool
	HasNext bool
	// PrevURL / NextURL 是上一页与下一页的地址，不存在时为空串。
	PrevURL string
	NextURL string
	// BaseURL 是本列表的基地址，主题拼页码时用，如 /categories/go。
	BaseURL string
}

// newPagination 计算翻页信息。
func newPagination(page, size, total int, baseURL string) Pagination {
	if size <= 0 {
		size = 10
	}
	if page <= 0 {
		page = 1
	}
	totalPages := max(1, ceilDiv(total, size))
	p := Pagination{
		Page:       page,
		Size:       size,
		Total:      total,
		TotalPages: totalPages,
		HasPrev:    page > 1,
		HasNext:    page < totalPages,
		BaseURL:    baseURL,
	}
	if p.HasPrev {
		p.PrevURL = pageURL(baseURL, page-1)
	}
	if p.HasNext {
		p.NextURL = pageURL(baseURL, page+1)
	}
	return p
}

// pageURL 拼出某一页的地址。
//
// 第一页不带查询参数：/posts 与 /posts?page=1 是同一个页面，
// 让它们有两个地址会稀释搜索引擎的权重。
func pageURL(base string, page int) string {
	if page <= 1 {
		return base
	}
	sep := "?"
	if containsByte(base, '?') {
		sep = "&"
	}
	return base + sep + "page=" + itoa(page)
}

// PageURL 返回列表某一页的地址，供模板渲染页码条。
func (p *Pagination) PageURL(page int) string { return pageURL(p.BaseURL, page) }

// Pages 返回页码条上应显示的页码，超长时以 0 表示省略号。
//
// 预先算好而不是丢给模板：省略号的边界条件（首尾保留、当前页居中、
// 总页数少于窗口时不省略）是典型的「每个人都会写错一次」的逻辑。
func (p *Pagination) Pages() []int {
	const window = 2 // 当前页左右各显示几页
	if p.TotalPages <= 7 {
		return seq(1, p.TotalPages)
	}

	out := []int{1}
	from := max(2, p.Page-window)
	to := min(p.TotalPages-1, p.Page+window)
	if from > 2 {
		out = append(out, 0)
	}
	out = append(out, seq(from, to)...)
	if to < p.TotalPages-1 {
		out = append(out, 0)
	}
	return append(out, p.TotalPages)
}

// ContentPath 返回内容的前台路径。
//
// 与 internal/seo 和 internal/menu 的约定保持一致：文章在 /posts/<slug>，
// 独立页面直接挂在根路径 /<slug>。
func ContentPath(kind, slug string) string { return content.Path(content.Type(kind), slug) }

// containsByte 报告字符串是否包含某个字节。
func containsByte(s string, b byte) bool {
	for i := range len(s) {
		if s[i] == b {
			return true
		}
	}
	return false
}

// itoa 是 strconv.Itoa 的本地别名，避免在 URL 拼接处引入额外导入。
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	negative := i < 0
	if negative {
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if negative {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
