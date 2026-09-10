package media

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/FeiBaiKin/lumo/internal/api"
	"github.com/FeiBaiKin/lumo/internal/auth"
	"github.com/FeiBaiKin/lumo/internal/auth/perm"
)

// tagMedia 是 OpenAPI 分组标签。
var tagMedia = []string{"media"}

// 路由路径。前缀由平面的分组附加。
const (
	pathMedia     = "/media"
	pathMediaByID = "/media/{id}"
)

// Handler 提供附件的 Console 接口。
type Handler struct {
	service *Service
	store   *Store
}

// NewHandler 构造 Handler。
func NewHandler(service *Service, store *Store) *Handler {
	return &Handler{service: service, store: store}
}

// Register 挂载接口。
//
// 读操作对任何已认证用户开放：作者写文章时要能从附件库挑图，而挑图不该需要上传权限。
// 写操作要求 media:write，改动或删除他人的附件要求 media:delete_any。
func (h *Handler) Register(console huma.API) {
	writer := huma.Middlewares{auth.RequirePermission(perm.MediaWrite, perm.MediaDeleteAny)}

	huma.Register(console, huma.Operation{
		OperationID: "media-upload",
		Method:      http.MethodPost,
		Path:        pathMedia,
		Summary:     "上传附件",
		Description: "multipart/form-data 上传。类型由**文件内容与扩展名共同判定**，客户端声明的 Content-Type 不作数；" +
			"图片会自动生成多档 WebP 缩略图。允许的扩展名：" + strings.Join(AllowedExtensions(), " ") + "。",
		Tags:          tagMedia,
		DefaultStatus: http.StatusCreated,
		Middlewares:   writer,
		Errors: []int{
			http.StatusBadRequest, http.StatusForbidden, http.StatusRequestEntityTooLarge,
			http.StatusUnsupportedMediaType, http.StatusUnprocessableEntity, http.StatusServiceUnavailable,
		},
	}, h.upload)

	huma.Register(console, huma.Operation{
		OperationID: "media-page",
		Method:      http.MethodGet,
		Path:        pathMedia,
		Summary:     "分页列出附件",
		Description: "按上传时间倒序。任何已认证用户都可浏览整个附件库。",
		Tags:        tagMedia,
	}, h.page)

	huma.Register(console, huma.Operation{
		OperationID: "media-get",
		Method:      http.MethodGet,
		Path:        pathMediaByID,
		Summary:     "获取附件",
		Tags:        tagMedia,
		Errors:      []int{http.StatusNotFound},
	}, h.get)

	huma.Register(console, huma.Operation{
		OperationID: "media-update",
		Method:      http.MethodPut,
		Path:        pathMediaByID,
		Summary:     "更新附件信息",
		Description: "只改 alt 与标题，不动文件本身。他人上传的附件需要 " + perm.MediaDeleteAny.String() + "。",
		Tags:        tagMedia,
		Middlewares: writer,
		Errors:      []int{http.StatusForbidden, http.StatusNotFound},
	}, h.update)

	huma.Register(console, huma.Operation{
		OperationID:   "media-delete",
		Method:        http.MethodDelete,
		Path:          pathMediaByID,
		Summary:       "删除附件",
		Description:   "同时删除原文件与全部缩略图，不可恢复。他人上传的附件需要 " + perm.MediaDeleteAny.String() + "。",
		Tags:          tagMedia,
		DefaultStatus: http.StatusNoContent,
		Middlewares:   writer,
		Errors:        []int{http.StatusForbidden, http.StatusNotFound},
	}, h.delete)
}

// ---------- 输入输出 ----------

// uploadForm 是上传表单。
//
// 文件字段声明为 application/octet-stream：huma 对该类型不做 MIME 校验，
// 类型判定统一交给本模块的白名单（扩展名 + 内容嗅探双向印证），避免两套规则打架。
type uploadForm struct {
	File  huma.FormFile `form:"file" contentType:"application/octet-stream" required:"true" doc:"要上传的文件"`
	Alt   string        `form:"alt" required:"false" doc:"无障碍替代文本"`
	Title string        `form:"title" required:"false" doc:"标题"`
}

type uploadInput struct {
	RawBody huma.MultipartFormFiles[uploadForm]
}

type idInput struct {
	ID int64 `path:"id" minimum:"1"`
}

type pageInput struct {
	api.PageParams
	Kind       string `query:"kind" enum:"image,video,audio,document,other" doc:"按分类筛选"`
	UploaderID int64  `query:"uploaderId" minimum:"1" doc:"按上传者筛选"`
	Q          string `query:"q" maxLength:"200" doc:"按原始文件名模糊筛选"`
}

type updateBody struct {
	Alt   string `json:"alt,omitempty" maxLength:"512" doc:"无障碍替代文本"`
	Title string `json:"title,omitempty" maxLength:"256" doc:"标题"`
}

type updateInput struct {
	ID   int64 `path:"id" minimum:"1"`
	Body updateBody
}

type mediaOutput struct {
	Body Media
}

type pageOutput struct {
	Body api.Page[Media]
}

// ---------- 处理器 ----------

// 表单字段的长度上限。multipart 的普通字段不走 JSON Schema 校验，只能在这里挡。
const (
	maxAltLength   = 512
	maxTitleLength = 256
)

func (h *Handler) upload(ctx context.Context, in *uploadInput) (*mediaOutput, error) {
	form := in.RawBody.Data()
	if !form.File.IsSet {
		return nil, huma.Error400BadRequest("缺少上传文件")
	}
	defer func() { _ = form.File.Close() }()

	if len([]rune(form.Alt)) > maxAltLength {
		return nil, huma.Error422UnprocessableEntity("alt 超过 " + strconv.Itoa(maxAltLength) + " 个字符")
	}
	if len([]rune(form.Title)) > maxTitleLength {
		return nil, huma.Error422UnprocessableEntity("标题超过 " + strconv.Itoa(maxTitleLength) + " 个字符")
	}

	principal := auth.MustFromContext(ctx)
	m, err := h.service.Upload(ctx, &UploadParams{
		OriginalName: form.File.Filename,
		Size:         form.File.Size,
		Content:      form.File,
		Alt:          strings.TrimSpace(form.Alt),
		Title:        strings.TrimSpace(form.Title),
		UploaderID:   principal.UserID(),
	})
	if err != nil {
		return nil, mapError(err)
	}
	return &mediaOutput{Body: *m}, nil
}

func (h *Handler) page(ctx context.Context, in *pageInput) (*pageOutput, error) {
	items, total, err := h.store.Page(ctx, Filter{
		Kind:       Kind(in.Kind),
		UploaderID: in.UploaderID,
		Q:          in.Q,
	}, in.PageParams)
	if err != nil {
		return nil, err
	}
	return &pageOutput{Body: api.NewPage(items, in.PageParams, total)}, nil
}

func (h *Handler) get(ctx context.Context, in *idInput) (*mediaOutput, error) {
	m, err := h.store.Get(ctx, in.ID)
	if err != nil {
		return nil, mapError(err)
	}
	return &mediaOutput{Body: *m}, nil
}

func (h *Handler) update(ctx context.Context, in *updateInput) (*mediaOutput, error) {
	m, err := h.store.Get(ctx, in.ID)
	if err != nil {
		return nil, mapError(err)
	}
	if !canManage(auth.MustFromContext(ctx), m) {
		return nil, auth.ForbiddenProblem(perm.MediaDeleteAny)
	}

	m.Alt = strings.TrimSpace(in.Body.Alt)
	m.Title = strings.TrimSpace(in.Body.Title)
	if err := h.store.UpdateMeta(ctx, m); err != nil {
		return nil, mapError(err)
	}
	return &mediaOutput{Body: *m}, nil
}

func (h *Handler) delete(ctx context.Context, in *idInput) (*struct{}, error) {
	m, err := h.store.Get(ctx, in.ID)
	if err != nil {
		return nil, mapError(err)
	}
	if !canManage(auth.MustFromContext(ctx), m) {
		return nil, auth.ForbiddenProblem(perm.MediaDeleteAny)
	}
	if err := h.service.Delete(ctx, m); err != nil {
		return nil, mapError(err)
	}
	return nil, nil //nolint:nilnil // 无响应体，huma 按 DefaultStatus 返回 204
}

// canManage 报告调用者能否修改或删除该附件。
//
// 自己上传的需 media:write，他人上传的需 media:delete_any——附件只有这一条 _any 权限，
// 它表达的是「不限所有权地管理附件」，改与删共用同一条。
func canManage(p *auth.Principal, m *Media) bool {
	if m.UploaderID == p.UserID() && p.Has(perm.MediaWrite) {
		return true
	}
	return p.Has(perm.MediaDeleteAny)
}

// mapError 把存储层与服务层的哨兵错误映射为 HTTP 错误；其余错误交给 huma 按 500 处理。
func mapError(err error) error {
	var unsupported *ErrUnsupportedType
	var mismatch *ErrContentMismatch
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrNotFound):
		return huma.Error404NotFound(err.Error())
	case errors.As(err, &unsupported):
		return huma.Error415UnsupportedMediaType(unsupported.Error())
	case errors.As(err, &mismatch):
		return huma.Error415UnsupportedMediaType(mismatch.Error())
	case errors.Is(err, ErrTooLarge):
		return huma.Error413RequestEntityTooLarge(err.Error())
	case errors.Is(err, ErrKeyTaken):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, ErrInvalid), errors.Is(err, ErrInvalidKey):
		return huma.Error400BadRequest(err.Error())
	case errors.Is(err, ErrMissingS3Credentials):
		// 这是部署配置问题而非调用方的错，但把原因说清楚才不至于让人对着 500 猜。
		return huma.Error503ServiceUnavailable("附件存储不可用：" + err.Error())
	}
	return err
}
