package content

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/FeiBaiKin/lumo/internal/api"
	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/auth"
	"github.com/FeiBaiKin/lumo/internal/auth/perm"
	"github.com/FeiBaiKin/lumo/internal/hooks"
	"github.com/FeiBaiKin/lumo/internal/slug"
	"github.com/FeiBaiKin/lumo/internal/taxonomy"
)

// maxSlugRetries 是自动生成的 slug 冲突时追加序号重试的上限。
const maxSlugRetries = 20

// kind 描述一种内容类型在接口与权限上的差异。
type kind struct {
	typ       Type
	path      string
	singular  string
	tags      []string
	write     perm.Permission
	writeAny  perm.Permission
	publish   perm.Permission
	deleteAny perm.Permission
	// hasTerms 为真时参与分类与标签。
	hasTerms bool
	// hasTemplate 为真时可选择主题模板。
	hasTemplate bool
}

var kinds = []*kind{
	{
		typ: TypePost, path: "/posts", singular: "post", tags: []string{"posts"},
		write: perm.PostsWrite, writeAny: perm.PostsWriteAny, publish: perm.PostsPublish, deleteAny: perm.PostsDeleteAny,
		hasTerms: true,
	},
	{
		typ: TypePage, path: "/pages", singular: "page", tags: []string{"pages"},
		write: perm.PagesWrite, writeAny: perm.PagesWriteAny, publish: perm.PagesPublish, deleteAny: perm.PagesDeleteAny,
		hasTemplate: true,
	},
}

// canWrite 报告调用者能否修改该内容：自己的内容需 write，他人的需 write_any。
func (k *kind) canWrite(p *auth.Principal, post *Post) bool {
	return p.Allows(k.write, post.AuthorID)
}

// canPublish 报告调用者能否发布或撤回该内容。
func (k *kind) canPublish(p *auth.Principal, post *Post) bool {
	return p.Has(k.publish) && k.canWrite(p, post)
}

// canDelete 报告调用者能否删除该内容：自己的内容需 write，他人的需 delete_any。
func (k *kind) canDelete(p *auth.Principal, post *Post) bool {
	if post.AuthorID == p.UserID() && p.Has(k.write) {
		return true
	}
	return p.Has(k.deleteAny)
}

// Slugger 由文本生成 slug；站点设置决定保留中文还是转拼音。
type Slugger func(ctx context.Context, text string) string

// Handler 提供内容的 Console 与 Public 接口。
type Handler struct {
	store   *Store
	slugify Slugger
	events  app.Events
}

// NewHandler 构造 Handler；slugify 为 nil 时使用保留中文的缺省策略，events 为 nil 时不派发动作。
func NewHandler(store *Store, slugify Slugger, events app.Events) *Handler {
	if slugify == nil {
		slugify = func(_ context.Context, text string) string { return slug.Make(text) }
	}
	return &Handler{store: store, slugify: slugify, events: events}
}

// emit 在内容变动之后通知插件。
func (h *Handler) emit(ctx context.Context, action string, post *Post) {
	if h.events != nil {
		h.events.Emit(ctx, action, HookPost(post))
	}
}

// Register 为文章与页面各挂一套接口。
func (h *Handler) Register(console, public huma.API) {
	for _, k := range kinds {
		h.registerKind(console, public, k)
	}
}

func (h *Handler) registerKind(console, public huma.API, k *kind) {
	id := func(action string) string { return "content-" + action + "-" + k.singular }
	byID := k.path + "/{id}"
	writer := huma.Middlewares{auth.RequirePermission(k.write, k.writeAny)}

	// ---- Console ----
	huma.Register(console, huma.Operation{
		OperationID: id("page"), Method: http.MethodGet, Path: k.path, Tags: k.tags,
		Summary:     "分页列出" + k.label(),
		Description: "按更新时间倒序。没有 " + k.writeAny.String() + " 权限的调用者只能看到自己的内容。",
		Middlewares: writer,
	}, h.list(k))
	huma.Register(console, huma.Operation{
		OperationID: id("get"), Method: http.MethodGet, Path: byID, Tags: k.tags,
		Summary: "获取" + k.label(), Middlewares: writer,
		Errors: []int{http.StatusForbidden, http.StatusNotFound},
	}, h.get(k))
	huma.Register(console, huma.Operation{
		OperationID: id("create"), Method: http.MethodPost, Path: k.path, Tags: k.tags,
		Summary:       "创建" + k.label(),
		Description:   "新建内容始终为草稿；发布走单独的接口。slug 留空则由标题生成，冲突时追加序号。",
		DefaultStatus: http.StatusCreated, Middlewares: writer,
		Errors: []int{http.StatusBadRequest, http.StatusForbidden, http.StatusConflict},
	}, h.create(k))
	huma.Register(console, huma.Operation{
		OperationID: id("update"), Method: http.MethodPut, Path: byID, Tags: k.tags,
		Summary:     "更新" + k.label(),
		Description: "整体替换可编辑字段，不改变发布状态。标题或正文变化时记录一次修订。",
		Middlewares: writer,
		Errors:      []int{http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound, http.StatusConflict},
	}, h.update(k))
	huma.Register(console, huma.Operation{
		OperationID: id("publish"), Method: http.MethodPost, Path: byID + "/publish", Tags: k.tags,
		Summary:     "发布" + k.label(),
		Description: "publishAt 为未来时间则定时发布，到点由后台任务推进；留空或为过去时间则立即发布。需要 " + k.publish.String() + "。",
		Middlewares: writer,
		Errors:      []int{http.StatusForbidden, http.StatusNotFound, http.StatusConflict},
	}, h.publish(k))
	huma.Register(console, huma.Operation{
		OperationID: id("unpublish"), Method: http.MethodPost, Path: byID + "/unpublish", Tags: k.tags,
		Summary: "撤回" + k.label() + "为草稿", Middlewares: writer,
		Errors: []int{http.StatusForbidden, http.StatusNotFound},
	}, h.unpublish(k))
	huma.Register(console, huma.Operation{
		OperationID: id("trash"), Method: http.MethodDelete, Path: byID, Tags: k.tags,
		Summary:       "移入回收站",
		Description:   "自己的内容需 " + k.write.String() + "，他人的需 " + k.deleteAny.String() + "。彻底删除请用 /permanent。",
		DefaultStatus: http.StatusNoContent, Middlewares: writer,
		Errors: []int{http.StatusForbidden, http.StatusNotFound},
	}, h.trash(k))
	huma.Register(console, huma.Operation{
		OperationID: id("restore"), Method: http.MethodPost, Path: byID + "/restore", Tags: k.tags,
		Summary: "从回收站恢复为草稿", Middlewares: writer,
		Errors: []int{http.StatusForbidden, http.StatusNotFound, http.StatusConflict},
	}, h.restore(k))
	huma.Register(console, huma.Operation{
		OperationID: id("delete"), Method: http.MethodDelete, Path: byID + "/permanent", Tags: k.tags,
		Summary:       "彻底删除",
		Description:   "只能删除回收站中的内容；修订与关联随之消失，不可恢复。",
		DefaultStatus: http.StatusNoContent, Middlewares: writer,
		Errors: []int{http.StatusForbidden, http.StatusNotFound, http.StatusConflict},
	}, h.deletePermanently(k))
	huma.Register(console, huma.Operation{
		OperationID: id("revisions"), Method: http.MethodGet, Path: byID + "/revisions", Tags: k.tags,
		Summary: "修订列表", Middlewares: writer,
		Errors: []int{http.StatusForbidden, http.StatusNotFound},
	}, h.revisions(k))
	huma.Register(console, huma.Operation{
		OperationID: id("revision"), Method: http.MethodGet, Path: byID + "/revisions/{revisionId}", Tags: k.tags,
		Summary: "获取修订", Middlewares: writer,
		Errors: []int{http.StatusForbidden, http.StatusNotFound},
	}, h.revision(k))
	huma.Register(console, huma.Operation{
		OperationID: id("restore-revision"), Method: http.MethodPost, Path: byID + "/revisions/{revisionId}/restore", Tags: k.tags,
		Summary:     "恢复到指定修订",
		Description: "把标题与正文恢复为该修订的快照，并记录一次新修订；不改变发布状态。",
		Middlewares: writer,
		Errors:      []int{http.StatusForbidden, http.StatusNotFound},
	}, h.restoreRevision(k))

	// ---- Public ----
	huma.Register(public, huma.Operation{
		OperationID: id("public-page"), Method: http.MethodGet, Path: k.path, Tags: k.tags,
		Summary:     "已发布的" + k.label() + "列表",
		Description: "置顶优先，再按发布时间倒序。匿名只见公开内容；已登录用户还能看到自己的私密内容。",
	}, h.publicList(k))
	huma.Register(public, huma.Operation{
		OperationID: id("public-get"), Method: http.MethodGet, Path: k.path + "/{slug}", Tags: k.tags,
		Summary: "按 slug 获取已发布的" + k.label(),
		Errors:  []int{http.StatusNotFound},
	}, h.publicGet(k))
}

// label 返回类型的中文名。
func (k *kind) label() string {
	if k.typ == TypePage {
		return "页面"
	}
	return "文章"
}

// ---------- 输入输出 ----------

type idInput struct {
	ID int64 `path:"id" minimum:"1"`
}

type slugInput struct {
	Slug string `path:"slug" minLength:"1" maxLength:"128"`
}

// body 是创建与更新共用的请求体。
type body struct {
	Title       string         `json:"title" minLength:"1" maxLength:"256"`
	Slug        string         `json:"slug,omitempty" maxLength:"128" doc:"URL 片段；留空则由标题生成"`
	RawType     RawType        `json:"rawType,omitempty" enum:"html,markdown" doc:"原稿格式，缺省 html"`
	Raw         string         `json:"raw,omitempty" doc:"原稿：Markdown 源码或规范 HTML"`
	Excerpt     string         `json:"excerpt,omitempty" maxLength:"1000" doc:"摘要；留空则由正文自动生成"`
	CoverURL    string         `json:"coverUrl,omitempty" maxLength:"1024"`
	Pinned      bool           `json:"pinned,omitempty"`
	Visibility  Visibility     `json:"visibility,omitempty" enum:"public,private" doc:"缺省 public"`
	Template    string         `json:"template,omitempty" maxLength:"64" pattern:"^[a-z0-9-]*$" doc:"页面模板名，如 page-about（仅页面）"`
	CategoryIDs []int64        `json:"categoryIds,omitempty" doc:"分类 ID 列表（仅文章）"`
	TagIDs      []int64        `json:"tagIds,omitempty" doc:"标签 ID 列表（仅文章）"`
	Meta        map[string]any `json:"meta,omitempty" doc:"扩展元数据"`
}

type createInput struct {
	Body body
}

type updateInput struct {
	ID   int64 `path:"id" minimum:"1"`
	Body body
}

type listInput struct {
	api.PageParams
	Status   string `query:"status" enum:"draft,published,scheduled,trashed" doc:"按状态筛选；留空为回收站之外的全部"`
	Category string `query:"category" maxLength:"128" doc:"分类 slug（仅文章）"`
	Tag      string `query:"tag" maxLength:"128" doc:"标签 slug（仅文章）"`
	Author   string `query:"author" maxLength:"64" doc:"作者用户名"`
	Q        string `query:"q" maxLength:"128" doc:"标题模糊匹配"`
}

type publicListInput struct {
	api.PageParams
	Category string `query:"category" maxLength:"128" doc:"分类 slug（仅文章）"`
	Tag      string `query:"tag" maxLength:"128" doc:"标签 slug（仅文章）"`
	Author   string `query:"author" maxLength:"64" doc:"作者用户名"`
}

type publishInput struct {
	ID   int64 `path:"id" minimum:"1"`
	Body *struct {
		PublishAt *time.Time `json:"publishAt,omitempty" doc:"计划发布时间；未来时间为定时发布，留空立即发布"`
	}
}

type revisionInput struct {
	ID         int64 `path:"id" minimum:"1"`
	RevisionID int64 `path:"revisionId" minimum:"1"`
}

type postOutput struct {
	Body Post
}

type postPageOutput struct {
	Body api.Page[Post]
}

type revisionList struct {
	Items []RevisionSummary `json:"items" doc:"新的在前"`
}

type revisionListOutput struct {
	Body revisionList
}

type revisionOutput struct {
	Body Revision
}

// ---------- Console 处理器 ----------

func (h *Handler) list(k *kind) func(context.Context, *listInput) (*postPageOutput, error) {
	return func(ctx context.Context, in *listInput) (*postPageOutput, error) {
		principal := auth.MustFromContext(ctx)
		f := &Filter{Type: k.typ, Status: Status(in.Status), Query: in.Q}
		if k.hasTerms {
			f.CategorySlug, f.TagSlug = in.Category, in.Tag
		}
		if !principal.Has(k.writeAny) {
			// 没有不限所有权的写权限，就只能看自己的。
			f.AuthorID = principal.UserID()
		} else if in.Author != "" {
			authorID, err := h.authorID(ctx, in.Author)
			if err != nil {
				return nil, err
			}
			f.AuthorID = authorID
		}
		items, total, err := h.store.Page(ctx, f, in.PageParams)
		if err != nil {
			return nil, err
		}
		return &postPageOutput{Body: api.NewPage(items, in.PageParams, total)}, nil
	}
}

func (h *Handler) get(k *kind) func(context.Context, *idInput) (*postOutput, error) {
	return func(ctx context.Context, in *idInput) (*postOutput, error) {
		post, err := h.load(ctx, k, in.ID, k.canWrite)
		if err != nil {
			return nil, err
		}
		return &postOutput{Body: *post}, nil
	}
}

func (h *Handler) create(k *kind) func(context.Context, *createInput) (*postOutput, error) {
	return func(ctx context.Context, in *createInput) (*postOutput, error) {
		principal := auth.MustFromContext(ctx)
		post := &Post{Type: k.typ, Status: StatusDraft, AuthorID: principal.UserID()}
		if err := applyBody(post, &in.Body, k, canWriteUnsafeHTML(principal)); err != nil {
			return nil, err
		}
		s, err := h.resolveSlug(ctx, in.Body.Slug, post.Title, "")
		if err != nil {
			return nil, err
		}
		err = withSlugRetry(isGenerated(in.Body.Slug), s, func(candidate string) error {
			post.Slug = candidate
			return h.store.Create(ctx, post, in.Body.CategoryIDs, in.Body.TagIDs)
		})
		if err != nil {
			return nil, mapError(err)
		}
		h.emit(ctx, hooks.PostUpdated, post)
		return &postOutput{Body: *post}, nil
	}
}

func (h *Handler) update(k *kind) func(context.Context, *updateInput) (*postOutput, error) {
	return func(ctx context.Context, in *updateInput) (*postOutput, error) {
		principal := auth.MustFromContext(ctx)
		post, err := h.load(ctx, k, in.ID, k.canWrite)
		if err != nil {
			return nil, err
		}
		before := *post
		if applyErr := applyBody(post, &in.Body, k, canWriteUnsafeHTML(principal)); applyErr != nil {
			return nil, applyErr
		}
		s, err := h.resolveSlug(ctx, in.Body.Slug, post.Title, before.Slug)
		if err != nil {
			return nil, err
		}
		post.Slug = s

		snapshot := before.Title != post.Title || before.Raw != post.Raw || before.RawType != post.RawType
		if err := h.store.Update(ctx, post, in.Body.CategoryIDs, in.Body.TagIDs, snapshot, principal.UserID()); err != nil {
			return nil, mapError(err)
		}
		h.emit(ctx, hooks.PostUpdated, post)
		return &postOutput{Body: *post}, nil
	}
}

func (h *Handler) publish(k *kind) func(context.Context, *publishInput) (*postOutput, error) {
	return func(ctx context.Context, in *publishInput) (*postOutput, error) {
		post, err := h.load(ctx, k, in.ID, k.canPublish)
		if err != nil {
			return nil, err
		}
		if post.Status == StatusTrashed {
			return nil, huma.Error409Conflict("回收站中的内容不能发布，请先恢复")
		}

		now := time.Now()
		at := now
		if in.Body != nil && in.Body.PublishAt != nil {
			at = *in.Body.PublishAt
		}
		post.PublishedAt = &at
		if at.After(now) {
			post.Status = StatusScheduled
		} else {
			post.Status = StatusPublished
		}
		if err := h.store.UpdateStatus(ctx, post); err != nil {
			return nil, mapError(err)
		}
		if post.Status == StatusPublished {
			h.emit(ctx, hooks.PostPublished, post)
		} else {
			// 定时发布：到点时由 PublishDue 发 post.published
			h.emit(ctx, hooks.PostUpdated, post)
		}
		return &postOutput{Body: *post}, nil
	}
}

func (h *Handler) unpublish(k *kind) func(context.Context, *idInput) (*postOutput, error) {
	return func(ctx context.Context, in *idInput) (*postOutput, error) {
		post, err := h.load(ctx, k, in.ID, k.canPublish)
		if err != nil {
			return nil, err
		}
		post.Status = StatusDraft
		if err := h.store.UpdateStatus(ctx, post); err != nil {
			return nil, mapError(err)
		}
		h.emit(ctx, hooks.PostUpdated, post)
		return &postOutput{Body: *post}, nil
	}
}

func (h *Handler) trash(k *kind) func(context.Context, *idInput) (*struct{}, error) {
	return func(ctx context.Context, in *idInput) (*struct{}, error) {
		post, err := h.load(ctx, k, in.ID, k.canDelete)
		if err != nil {
			return nil, err
		}
		now := time.Now()
		post.Status = StatusTrashed
		post.TrashedAt = &now
		if err := h.store.UpdateStatus(ctx, post); err != nil {
			return nil, mapError(err)
		}
		h.emit(ctx, hooks.PostTrashed, post)
		return nil, nil //nolint:nilnil // 无响应体，huma 按 DefaultStatus 返回 204
	}
}

func (h *Handler) restore(k *kind) func(context.Context, *idInput) (*postOutput, error) {
	return func(ctx context.Context, in *idInput) (*postOutput, error) {
		post, err := h.load(ctx, k, in.ID, k.canDelete)
		if err != nil {
			return nil, err
		}
		if post.Status != StatusTrashed {
			return nil, huma.Error409Conflict("内容不在回收站")
		}
		post.Status = StatusDraft
		post.TrashedAt = nil
		if err := h.store.UpdateStatus(ctx, post); err != nil {
			return nil, mapError(err)
		}
		h.emit(ctx, hooks.PostUpdated, post)
		return &postOutput{Body: *post}, nil
	}
}

func (h *Handler) deletePermanently(k *kind) func(context.Context, *idInput) (*struct{}, error) {
	return func(ctx context.Context, in *idInput) (*struct{}, error) {
		post, err := h.load(ctx, k, in.ID, k.canDelete)
		if err != nil {
			return nil, err
		}
		if post.Status != StatusTrashed {
			return nil, huma.Error409Conflict("只能彻底删除回收站中的内容")
		}
		// 真正的守卫在存储层的删除条件里；这里的预检查只是为了给出更友好的提示。
		err = h.store.DeletePermanently(ctx, k.typ, post.ID)
		switch {
		case err == nil:
			h.emit(ctx, hooks.PostDeleted, post)
		case errors.Is(err, ErrNotTrashed):
			return nil, huma.Error409Conflict("内容已不在回收站（可能刚被恢复），请刷新后重试")
		default:
			return nil, mapError(err)
		}
		return nil, nil //nolint:nilnil // 无响应体，huma 按 DefaultStatus 返回 204
	}
}

func (h *Handler) revisions(k *kind) func(context.Context, *idInput) (*revisionListOutput, error) {
	return func(ctx context.Context, in *idInput) (*revisionListOutput, error) {
		post, err := h.load(ctx, k, in.ID, k.canWrite)
		if err != nil {
			return nil, err
		}
		items, err := h.store.Revisions(ctx, post.ID)
		if err != nil {
			return nil, err
		}
		return &revisionListOutput{Body: revisionList{Items: items}}, nil
	}
}

func (h *Handler) revision(k *kind) func(context.Context, *revisionInput) (*revisionOutput, error) {
	return func(ctx context.Context, in *revisionInput) (*revisionOutput, error) {
		post, err := h.load(ctx, k, in.ID, k.canWrite)
		if err != nil {
			return nil, err
		}
		rev, err := h.store.Revision(ctx, post.ID, in.RevisionID)
		if err != nil {
			return nil, mapError(err)
		}
		return &revisionOutput{Body: *rev}, nil
	}
}

func (h *Handler) restoreRevision(k *kind) func(context.Context, *revisionInput) (*postOutput, error) {
	return func(ctx context.Context, in *revisionInput) (*postOutput, error) {
		principal := auth.MustFromContext(ctx)
		post, err := h.load(ctx, k, in.ID, k.canWrite)
		if err != nil {
			return nil, err
		}
		rev, err := h.store.Revision(ctx, post.ID, in.RevisionID)
		if err != nil {
			return nil, mapError(err)
		}
		post.Title, post.RawType, post.Raw, post.Content, post.Excerpt = rev.Title, rev.RawType, rev.Raw, rev.Content, rev.Excerpt
		// 恢复的修订可能由更高权限的账号保存过（含未净化正文），
		// 所以这里按**当前调用者**的权限再过一次。
		post.Content = sanitizeForWriter(post.Content, canWriteUnsafeHTML(principal))
		if err := h.store.Update(ctx, post, ids(post.Categories), tagIDs(post.Tags), true, principal.UserID()); err != nil {
			return nil, mapError(err)
		}
		return &postOutput{Body: *post}, nil
	}
}

// ---------- Public 处理器 ----------

func (h *Handler) publicList(k *kind) func(context.Context, *publicListInput) (*postPageOutput, error) {
	return func(ctx context.Context, in *publicListInput) (*postPageOutput, error) {
		principal, _ := auth.FromContext(ctx)
		f := &Filter{Type: k.typ, PublicOnly: true, ViewerID: principal.UserID()}
		if k.hasTerms {
			f.CategorySlug, f.TagSlug = in.Category, in.Tag
		}
		if in.Author != "" {
			authorID, err := h.authorID(ctx, in.Author)
			if err != nil {
				return nil, err
			}
			f.AuthorID = authorID
		}
		items, total, err := h.store.Page(ctx, f, in.PageParams)
		if err != nil {
			return nil, err
		}
		return &postPageOutput{Body: api.NewPage(items, in.PageParams, total)}, nil
	}
}

func (h *Handler) publicGet(k *kind) func(context.Context, *slugInput) (*postOutput, error) {
	return func(ctx context.Context, in *slugInput) (*postOutput, error) {
		post, err := h.store.GetBySlug(ctx, k.typ, in.Slug)
		if err != nil {
			return nil, mapError(err)
		}
		principal, _ := auth.FromContext(ctx)
		// 未发布或无权查看的私密内容一律 404，不暴露其存在。
		if post.Status != StatusPublished {
			return nil, huma.Error404NotFound(ErrNotFound.Error())
		}
		if post.Visibility == VisibilityPrivate && post.AuthorID != principal.UserID() && !principal.Has(k.writeAny) {
			return nil, huma.Error404NotFound(ErrNotFound.Error())
		}
		return &postOutput{Body: *post}, nil
	}
}

// ---------- 工具 ----------

// load 取内容并执行权限判定；不存在返回 404，无权返回 403。
func (h *Handler) load(ctx context.Context, k *kind, id int64, allowed func(*auth.Principal, *Post) bool) (*Post, error) {
	post, err := h.store.Get(ctx, k.typ, id)
	if err != nil {
		return nil, mapError(err)
	}
	if !allowed(auth.MustFromContext(ctx), post) {
		return nil, auth.ForbiddenProblem(k.write, k.writeAny, k.publish, k.deleteAny)
	}
	return post, nil
}

// canWriteUnsafeHTML 报告调用者能否让正文原样输出到前台。
func canWriteUnsafeHTML(p *auth.Principal) bool {
	return p.Has(perm.ContentUnsafeHTML)
}

// authorID 把用户名解析为用户 ID；用户不存在时返回一个不可能匹配的 ID，让列表为空而非报错。
func (h *Handler) authorID(ctx context.Context, username string) (int64, error) {
	user, err := h.store.users.FindUserByLogin(ctx, username)
	if err != nil {
		if errors.Is(err, auth.ErrNotFound) {
			return -1, nil
		}
		return 0, err
	}
	return user.ID, nil
}

// applyBody 把请求体写到实体上：渲染正文、处理摘要与缺省值。
//
// unsafeHTML 为真时原样保留渲染结果（调用者持有 content:unsafe_html）；
// 否则按允许列表净化——正文是站点同源输出的，未净化的脚本会在
// 任何访客（可能是管理员）的浏览器里以本站身份运行（见 sanitize.go）。
// 净化只作用于渲染产物 content：raw 原稿保持作者写的原文，编辑器往返不丢东西。
func applyBody(post *Post, b *body, k *kind, unsafeHTML bool) error {
	title := strings.TrimSpace(b.Title)
	if title == "" {
		return huma.Error400BadRequest("标题不能为空白")
	}
	rawType := b.RawType
	if rawType == "" {
		rawType = RawHTML
	}
	content, err := Render(rawType, b.Raw)
	if err != nil {
		return huma.Error400BadRequest(err.Error())
	}
	content = sanitizeForWriter(content, unsafeHTML)

	post.Title = title
	post.RawType = rawType
	post.Raw = b.Raw
	post.Content = content
	if excerpt := strings.TrimSpace(b.Excerpt); excerpt != "" {
		post.Excerpt, post.ExcerptAuto = excerpt, false
	} else {
		post.Excerpt, post.ExcerptAuto = Excerpt(content, ExcerptLength), true
	}
	post.CoverURL = b.CoverURL
	post.Pinned = b.Pinned
	post.Visibility = b.Visibility
	if post.Visibility == "" {
		post.Visibility = VisibilityPublic
	}
	post.Template = ""
	if k.hasTemplate {
		post.Template = b.Template
	}
	post.Meta = b.Meta
	if post.Meta == nil {
		post.Meta = map[string]any{}
	}
	return nil
}

// isGenerated 报告 slug 是否将由标题自动生成（调用方未显式指定）。
func isGenerated(provided string) bool {
	return strings.TrimSpace(provided) == ""
}

// resolveSlug 决定最终 slug：显式指定则规范化；更新时留空保留原值；创建时留空按站点策略由标题生成。
func (h *Handler) resolveSlug(ctx context.Context, provided, title, current string) (string, error) {
	switch {
	case !isGenerated(provided):
		s := slug.Make(provided)
		if s == "" {
			return "", huma.Error400BadRequest("slug 不合法：须包含字母或数字")
		}
		return s, nil
	case current != "":
		return current, nil
	default:
		s := h.slugify(ctx, title)
		if s == "" {
			return "", huma.Error400BadRequest("无法从标题生成 slug，请手动指定")
		}
		return s, nil
	}
}

// withSlugRetry 执行 create；自动生成的 slug 冲突时依次追加 -2、-3…… 重试，
// 用户显式指定的冲突直接报错。
func withSlugRetry(generated bool, base string, create func(candidate string) error) error {
	err := create(base)
	if !generated || !errors.Is(err, ErrSlugTaken) {
		return err
	}
	for i := 2; i <= maxSlugRetries; i++ {
		err = create(withSuffix(base, "-"+strconv.Itoa(i)))
		if !errors.Is(err, ErrSlugTaken) {
			return err
		}
	}
	return err
}

// withSuffix 给 slug 追加后缀，超长时截短主体以保持在长度上限内。
func withSuffix(base, suffix string) string {
	runes := []rune(base)
	if keep := slug.MaxLength - len(suffix); len(runes) > keep {
		runes = runes[:keep]
	}
	return strings.TrimRight(string(runes), "-") + suffix
}

// ids 提取分类 ID 列表。
func ids(categories []taxonomy.Category) []int64 {
	out := make([]int64, 0, len(categories))
	for i := range categories {
		out = append(out, categories[i].ID)
	}
	return out
}

// tagIDs 提取标签 ID 列表。
func tagIDs(tags []taxonomy.Tag) []int64 {
	out := make([]int64, 0, len(tags))
	for i := range tags {
		out = append(out, tags[i].ID)
	}
	return out
}

// mapError 把存储层的哨兵错误映射为 HTTP 错误；其余错误交给 huma 按 500 处理。
func mapError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrNotFound):
		return huma.Error404NotFound(err.Error())
	case errors.Is(err, ErrSlugTaken):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, ErrTermNotFound), errors.Is(err, ErrInvalid):
		return huma.Error400BadRequest(err.Error())
	}
	return err
}
