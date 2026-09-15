package theme

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/FeiBaiKin/lumo/internal/auth"
	"github.com/FeiBaiKin/lumo/internal/content"
)

// urlQueryEscape 是 url.QueryEscape 的本地别名，供拼接分页地址使用。
func urlQueryEscape(s string) string { return url.QueryEscape(s) }

// Frontend 是访客前台的路由处理器。
//
// 前台由 Go 服务端模板渲染，不经三平面：它产出的是 HTML 页面
// 而非 JSON，鉴权模型也不同（匿名可读，登录用户额外看得到自己的私密内容）。
type Frontend struct {
	renderer *Renderer
	store    *Store
}

// NewFrontend 构造前台处理器。
func NewFrontend(renderer *Renderer, store *Store) *Frontend {
	return &Frontend{renderer: renderer, store: store}
}

// Mount 把前台路由挂到根路由上。
//
// 路由顺序要紧：/posts/{slug} 这类带固定前缀的必须先注册，
// 最后才是兜底的 /{slug}（独立页面），否则页面路由会吞掉一切。
func (f *Frontend) Mount(r chi.Router) {
	r.Get("/", f.index)
	r.Get(PathPosts+"{slug}", f.post)
	r.Get(PathCategories+"{slug}", f.category)
	r.Get(PathTags+"{slug}", f.tag)
	r.Get(PathArchives+"{year}", f.archive)
	r.Get(PathArchives+"{year}/{month}", f.archive)
	r.Get(PathAuthors+"{username}", f.author)
	r.Get(PathSearch, f.search)
	r.Get(PathFavorites, f.favorites)
	// 兜底：根路径下的单段路径视为独立页面。
	r.Get("/{slug}", f.page)
}

// viewerID 返回当前登录用户的 ID；匿名为 0。
//
// 用于放行作者本人的私密内容：作者点自己文章的链接不该看到 404。
// pathParam 取路径参数并做一次百分号解码。
//
// html/template 在 href 上下文里会把中文 slug 规范化成**小写**的百分号编码
// （/posts/%e7%a7%8b…），而 Go 的 url.Parse 只在编码形式与规范形式（大写）不同时才保留
// RawPath；chi 一旦看到 RawPath 就按它路由，参数值因此是**未解码**的编码串，
// 拿去查库自然找不到。站点默认的 slug 策略是保留中文（settings.site.slugStrategy = unicode），
// 不解码就等于所有中文标题的文章前台都打不开。解码失败时按原值处理。
func pathParam(r *http.Request, name string) string {
	raw := chi.URLParam(r, name)
	if decoded, err := url.PathUnescape(raw); err == nil {
		return decoded
	}
	return raw
}

func viewerID(r *http.Request) int64 { return viewerIDFrom(r.Context()) }

// viewerIDFrom 从上下文取当前登录用户的 ID；匿名为 0。
//
// 单独一个取 context 的版本是给 Finder 用的：模板里的取数入口只拿得到 context，
// 而「当前是谁」这件事只有它能回答（见 FavoritesFinder.Has）。
func viewerIDFrom(ctx context.Context) int64 {
	principal, ok := auth.FromContext(ctx)
	if !ok || principal == nil {
		return 0
	}
	return principal.UserID()
}

// pageParam 读取 ?page= 参数，非法值一律按第 1 页。
func pageParam(r *http.Request) int {
	raw := r.URL.Query().Get("page")
	if raw == "" {
		return 1
	}
	page, err := strconv.Atoi(raw)
	if err != nil || page < 1 {
		return 1
	}
	// 上限防止 ?page=99999999 触发一次巨大的 OFFSET 扫描。
	const maxPage = 10000
	return min(page, maxPage)
}

// index 渲染首页：已发布文章的分页列表。
func (f *Frontend) index(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	pageCtx, err := f.renderer.NewContext(ctx, r, KindIndex)
	if err != nil {
		f.renderer.renderError(w, r, err, "index.html")
		return
	}

	page := pageParam(r)
	size := f.renderer.PageSize(ctx)
	posts, total, err := f.store.Posts(ctx, page, size)
	if err != nil {
		f.renderer.renderError(w, r, err, "index.html")
		return
	}

	pageCtx.Title = pageCtx.Site.Title
	pageCtx.Description = pageCtx.Site.Description
	pageCtx.Posts = posts
	pageCtx.Pagination = newPagination(page, size, total, "/")
	f.renderer.Render(w, r, http.StatusOK, "index.html", pageCtx)
}

// post 渲染文章详情页。
func (f *Frontend) post(w http.ResponseWriter, r *http.Request) {
	f.renderContent(w, r, string(content.TypePost), pathParam(r, "slug"), KindPost, "post.html")
}

// page 渲染独立页面。
//
// 页面可选主题提供的 page-*.html 模板（WordPress 模式）；
// 模板不存在时回退到 page.html，而不是报错——主题换了之后旧页面还得能打开。
func (f *Frontend) page(w http.ResponseWriter, r *http.Request) {
	slug := pathParam(r, "slug")
	ctx := r.Context()

	view, err := f.store.GetContent(ctx, string(content.TypePage), slug, viewerID(r))
	if err != nil {
		f.notFound(w, r)
		return
	}

	name := "page.html"
	if view.Template != "" {
		candidate := view.Template + ".html"
		if _, ok := f.renderer.registry.Active().Engine().Lookup(candidate); ok {
			name = candidate
		}
	}
	f.renderSingle(w, r, view, KindPage, name)
}

// renderContent 渲染一条内容的详情页。
func (f *Frontend) renderContent(w http.ResponseWriter, r *http.Request, kind, slug, pageKind, tmpl string) {
	view, err := f.store.GetContent(r.Context(), kind, slug, viewerID(r))
	if err != nil {
		f.notFound(w, r)
		return
	}
	f.renderSingle(w, r, view, pageKind, tmpl)
}

// renderSingle 组装并渲染单条内容的上下文。
func (f *Frontend) renderSingle(w http.ResponseWriter, r *http.Request, view *PostView, kind, tmpl string) {
	ctx := r.Context()
	pageCtx, err := f.renderer.NewContext(ctx, r, kind)
	if err != nil {
		f.renderer.renderError(w, r, err, tmpl)
		return
	}

	pageCtx.Post = view
	pageCtx.Title = view.Title
	pageCtx.Description = view.Excerpt
	pageCtx.Canonical = absoluteURL(pageCtx.Site.URL, view.URL)
	f.renderer.Render(w, r, http.StatusOK, tmpl, pageCtx)
}

// category 渲染分类归档页。
func (f *Frontend) category(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	slug := pathParam(r, "slug")

	category, err := f.store.GetCategory(ctx, slug)
	if err != nil {
		f.notFound(w, r)
		return
	}
	pageCtx, err := f.renderer.NewContext(ctx, r, KindCategory)
	if err != nil {
		f.renderer.renderError(w, r, err, "category.html")
		return
	}

	page := pageParam(r)
	size := f.renderer.PageSize(ctx)
	posts, total, err := f.store.PostsByTerm(ctx, termCategory, slug, page, size)
	if err != nil {
		f.renderer.renderError(w, r, err, "category.html")
		return
	}

	pageCtx.Category = category
	pageCtx.Title = category.Name
	pageCtx.Description = category.Description
	pageCtx.Posts = posts
	pageCtx.Pagination = newPagination(page, size, total, PathCategories+slug)
	f.renderer.Render(w, r, http.StatusOK, "category.html", pageCtx)
}

// tag 渲染标签归档页。
func (f *Frontend) tag(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	slug := pathParam(r, "slug")

	tag, err := f.store.GetTag(ctx, slug)
	if err != nil {
		f.notFound(w, r)
		return
	}
	pageCtx, err := f.renderer.NewContext(ctx, r, KindTag)
	if err != nil {
		f.renderer.renderError(w, r, err, "tag.html")
		return
	}

	page := pageParam(r)
	size := f.renderer.PageSize(ctx)
	posts, total, err := f.store.PostsByTerm(ctx, termTag, slug, page, size)
	if err != nil {
		f.renderer.renderError(w, r, err, "tag.html")
		return
	}

	pageCtx.Tag = tag
	pageCtx.Title = tag.Name
	pageCtx.Description = tag.Description
	pageCtx.Posts = posts
	pageCtx.Pagination = newPagination(page, size, total, PathTags+slug)
	f.renderer.Render(w, r, http.StatusOK, "tag.html", pageCtx)
}

// archive 渲染时间归档页，支持按年与按月。
func (f *Frontend) archive(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	year, err := strconv.Atoi(chi.URLParam(r, "year"))
	// 年份范围卡在合理区间：既挡住 /archives/abc，也挡住 /archives/999999999。
	if err != nil || year < 1970 || year > 9999 {
		f.notFound(w, r)
		return
	}
	month := 0
	if raw := chi.URLParam(r, "month"); raw != "" {
		month, err = strconv.Atoi(raw)
		if err != nil || month < 1 || month > 12 {
			f.notFound(w, r)
			return
		}
	}

	pageCtx, err := f.renderer.NewContext(ctx, r, KindArchive)
	if err != nil {
		f.renderer.renderError(w, r, err, "archive.html")
		return
	}

	page := pageParam(r)
	size := f.renderer.PageSize(ctx)
	posts, total, err := f.store.PostsByArchive(ctx, year, month, page, size)
	if err != nil {
		f.renderer.renderError(w, r, err, "archive.html")
		return
	}

	archive := &ArchiveContext{Year: year, Month: month, Label: strconv.Itoa(year) + " 年"}
	base := PathArchives + strconv.Itoa(year)
	if month > 0 {
		archive.Label += " " + strconv.Itoa(month) + " 月"
		base += "/" + twoDigits(month)
	}

	pageCtx.Archive = archive
	pageCtx.Title = archive.Label
	pageCtx.Posts = posts
	pageCtx.Pagination = newPagination(page, size, total, base)
	f.renderer.Render(w, r, http.StatusOK, "archive.html", pageCtx)
}

// author 渲染作者归档页。
func (f *Frontend) author(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	username := pathParam(r, "username")

	author, err := f.store.GetAuthor(ctx, username)
	if err != nil {
		f.notFound(w, r)
		return
	}
	pageCtx, err := f.renderer.NewContext(ctx, r, KindAuthor)
	if err != nil {
		f.renderer.renderError(w, r, err, "author.html")
		return
	}

	page := pageParam(r)
	size := f.renderer.PageSize(ctx)
	posts, total, err := f.store.PostsByAuthor(ctx, author.ID, page, size)
	if err != nil {
		f.renderer.renderError(w, r, err, "author.html")
		return
	}

	pageCtx.Author = author
	pageCtx.Title = author.DisplayName
	pageCtx.Description = author.Bio
	pageCtx.Posts = posts
	pageCtx.Pagination = newPagination(page, size, total, PathAuthors+username)
	f.renderer.Render(w, r, http.StatusOK, "author.html", pageCtx)
}

// maxQueryLength 是搜索词的长度上限。
const maxQueryLength = 100

// search 渲染搜索结果页。
func (f *Frontend) search(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if len([]rune(query)) > maxQueryLength {
		query = string([]rune(query)[:maxQueryLength])
	}

	pageCtx, err := f.renderer.NewContext(ctx, r, KindSearch)
	if err != nil {
		f.renderer.renderError(w, r, err, "search.html")
		return
	}

	pageCtx.Query = query
	pageCtx.Title = "搜索"
	pageCtx.Posts = []PostView{}
	page := pageParam(r)
	size := f.renderer.PageSize(ctx)

	if query != "" {
		posts, total, searchErr := f.store.SearchPosts(ctx, query, page, size)
		if searchErr != nil {
			f.renderer.renderError(w, r, searchErr, "search.html")
			return
		}
		pageCtx.Title = "搜索：" + query
		pageCtx.Posts = posts
		pageCtx.Pagination = newPagination(page, size, total, PathSearch+"?q="+urlQueryEscape(query))
	}
	f.renderer.Render(w, r, http.StatusOK, "search.html", pageCtx)
}

// pathLogin 是账户模块的登录页地址。
//
// 这里写死而不是引 account 的常量：account 依赖本包（它用本包的 Renderer 渲染页面），
// 反过来引就是一个导入环。两处同步靠 TestFavoritesRedirectsAnonymous 盯着。
const pathLogin = "/login"

// favorites 渲染「我的收藏」页。
//
// 这一页只有本人看得到，故未登录时跳登录页并带上回跳地址——而不是 404。
// 与账户页（account 模块的 getAccount）同一种处置：一个需要登录才有内容的页面，
// 对匿名访客的正确回答是「先登录」，不是「没有这个页面」。
func (f *Frontend) favorites(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := viewerID(r)
	if userID <= 0 {
		http.Redirect(w, r, pathLogin+"?next="+urlQueryEscape(PathFavorites), http.StatusFound)
		return
	}

	pageCtx, err := f.renderer.NewContext(ctx, r, KindFavorites)
	if err != nil {
		f.renderer.renderError(w, r, err, "favorites.html")
		return
	}

	page := pageParam(r)
	size := f.renderer.PageSize(ctx)
	posts, total, err := f.store.FavoritePosts(ctx, userID, page, size)
	if err != nil {
		f.renderer.renderError(w, r, err, "favorites.html")
		return
	}

	pageCtx.Title = "我的收藏"
	pageCtx.Posts = posts
	pageCtx.Pagination = newPagination(page, size, total, PathFavorites)
	f.renderer.Render(w, r, http.StatusOK, "favorites.html", pageCtx)
}

// notFound 渲染 404 页面。
func (f *Frontend) notFound(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	pageCtx, err := f.renderer.NewContext(ctx, r, KindNotFound)
	if err != nil {
		f.renderer.renderError(w, r, err, "404.html")
		return
	}
	pageCtx.Title = "页面不存在"
	f.renderer.Render(w, r, http.StatusNotFound, "404.html", pageCtx)
}

// NotFoundHandler 返回主题渲染的 404 处理器，供根路由在无匹配时使用。
func (f *Frontend) NotFoundHandler() http.HandlerFunc { return f.notFound }

// IsNotFound 报告错误是否表示对象不存在。
func IsNotFound(err error) bool { return errors.Is(err, ErrNotFound) }
