package theme

import (
	"time"

	"github.com/FeiBaiKin/lumo/internal/content"
	"github.com/FeiBaiKin/lumo/internal/taxonomy"
)

// 路由种类。每种路由注入固定的、有文档的上下文（agent.md §4.2），
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

	// Find 是只读数据访问面，供侧栏页脚一类小部件取数（agent.md §4.2）。
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
}

// PostView 是注入模板的内容视图。
//
// 不直接用 content.Post：那是数据库实体，带着 Raw 原稿与 Meta 这些主题不该碰的字段。
// 主题只消费渲染后的 Content（agent.md §3.3），编辑器换了主题也不受影响。
type PostView struct {
	ID    int64
	Type  string
	Title string
	Slug  string
	// URL 是本内容的站内路径，如 /posts/hello。
	URL string
	// Content 是渲染后的 HTML。模板里须用 safeHTML 输出。
	Content string
	Excerpt string
	// CoverURL 是封面图地址，可能为空。
	CoverURL string
	Pinned   bool
	// Template 是页面选择的主题模板名（仅独立页面），如 page-about。
	Template    string
	Author      *AuthorView
	Categories  []taxonomy.Category
	Tags        []taxonomy.Tag
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
func ContentPath(kind, slug string) string {
	if kind == string(content.TypePage) {
		return "/" + slug
	}
	return PathPosts + slug
}

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
