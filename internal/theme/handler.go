package theme

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/FeiBaiKin/lumo/internal/auth"
	"github.com/FeiBaiKin/lumo/internal/auth/perm"
	"github.com/FeiBaiKin/lumo/internal/settings"
)

// fsStat 是 fs.Stat 的本地别名，避免与本包的 Store 方法名混淆。
func fsStat(fsys fs.FS, name string) (fs.FileInfo, error) { return fs.Stat(fsys, name) }

// tagThemes 是 OpenAPI 分组标签。
var tagThemes = []string{"themes"}

// 路由路径。前缀由平面的分组附加。
const (
	pathThemes         = "/themes"
	pathThemesActive   = "/themes/active"
	pathThemeByName    = "/themes/{name}"
	pathThemeActivate  = "/themes/{name}/activate"
	pathThemeReload    = "/themes/{name}/reload"
	pathThemeSettings  = "/themes/{name}/settings"
	pathThemeSettingsG = "/themes/{name}/settings/{group}"
)

// Handler 提供主题的 Console 管理接口。
type Handler struct {
	module *Module
}

// NewHandler 构造 Handler。
func NewHandler(m *Module) *Handler { return &Handler{module: m} }

// Register 挂载接口。
//
// 全部管理操作要求 themes:manage：主题能执行任意模板逻辑并决定整站外观，
// 它的权限门槛与设置同级，不对编辑开放。
func (h *Handler) Register(console, public huma.API) {
	manage := huma.Middlewares{auth.RequirePermission(perm.ThemesManage)}

	huma.Register(console, huma.Operation{
		OperationID: "themes-list",
		Method:      http.MethodGet,
		Path:        pathThemes,
		Summary:     "列出已安装主题",
		Description: "含内置主题；加载失败的主题以 broken 列出并附原因。",
		Tags:        tagThemes,
		Middlewares: manage,
	}, h.list)

	huma.Register(console, huma.Operation{
		OperationID: "themes-get",
		Method:      http.MethodGet,
		Path:        pathThemeByName,
		Summary:     "获取主题详情",
		Description: "含元信息、模板提供情况与可供独立页面选择的 page-* 模板。",
		Tags:        tagThemes,
		Middlewares: manage,
		Errors:      []int{http.StatusNotFound},
	}, h.get)

	huma.Register(console, huma.Operation{
		OperationID:   "themes-install",
		Method:        http.MethodPost,
		Path:          pathThemes,
		Summary:       "上传并安装主题包",
		Description:   "multipart/form-data 上传 zip 包。安装前会校验模板可解析、必需模板齐备、设置声明可编译。",
		Tags:          tagThemes,
		DefaultStatus: http.StatusCreated,
		Middlewares:   manage,
		Errors: []int{
			http.StatusBadRequest, http.StatusConflict,
			http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity,
		},
	}, h.install)

	huma.Register(console, huma.Operation{
		OperationID: "themes-activate",
		Method:      http.MethodPost,
		Path:        pathThemeActivate,
		Summary:     "启用主题",
		Tags:        tagThemes,
		Middlewares: manage,
		Errors:      []int{http.StatusNotFound},
	}, h.activate)

	huma.Register(console, huma.Operation{
		OperationID:   "themes-delete",
		Method:        http.MethodDelete,
		Path:          pathThemeByName,
		Summary:       "卸载主题",
		Description:   "删除主题目录及其全部设置值。内置主题与当前启用的主题不可删除。",
		Tags:          tagThemes,
		DefaultStatus: http.StatusNoContent,
		Middlewares:   manage,
		Errors:        []int{http.StatusConflict, http.StatusNotFound},
	}, h.delete)

	huma.Register(console, huma.Operation{
		OperationID: "themes-reload",
		Method:      http.MethodPost,
		Path:        pathThemeReload,
		Summary:     "重新加载主题",
		Description: "重新解析模板与设置声明，用于手工改动主题目录后立即生效。",
		Tags:        tagThemes,
		Middlewares: manage,
		Errors:      []int{http.StatusNotFound, http.StatusUnprocessableEntity},
	}, h.reload)

	huma.Register(console, huma.Operation{
		OperationID: "themes-settings-list",
		Method:      http.MethodGet,
		Path:        pathThemeSettings,
		Summary:     "列出主题设置分组",
		Description: "分组形态与站点设置完全一致，Console 用同一个表单引擎渲染。",
		Tags:        tagThemes,
		Middlewares: manage,
		Errors:      []int{http.StatusNotFound},
	}, h.listSettings)

	huma.Register(console, huma.Operation{
		OperationID: "themes-settings-update",
		Method:      http.MethodPut,
		Path:        pathThemeSettingsG,
		Summary:     "更新主题设置分组",
		Tags:        tagThemes,
		Middlewares: manage,
		Errors:      []int{http.StatusNotFound, http.StatusUnprocessableEntity},
	}, h.updateSettings)

	huma.Register(public, huma.Operation{
		OperationID: "themes-public-active",
		Method:      http.MethodGet,
		Path:        pathThemesActive,
		Summary:     "当前启用的主题",
		Description: "只暴露名称、显示名与版本，供无头调用方判断前台外观。",
		Tags:        tagThemes,
	}, h.publicActive)
}

// ---------- 视图 ----------

// View 是主题的接口视图。
type View struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Author      string `json:"author"`
	Homepage    string `json:"homepage"`
	Repo        string `json:"repo"`
	License     string `json:"license"`
	// Builtin 为真表示内置主题，不可删除。
	Builtin bool `json:"builtin"`
	// Active 为真表示当前启用。
	Active bool `json:"active"`
	// Templates 列出必需与可选模板的提供情况。
	Templates []TemplateStatus `json:"templates"`
	// PageTemplates 是可供独立页面选择的模板名（page-* 形态）。
	PageTemplates []string `json:"pageTemplates"`
	// HasScreenshot 表示主题带了截图。
	HasScreenshot bool `json:"hasScreenshot"`
	// SettingGroups 是主题声明的设置分组名。
	SettingGroups []string `json:"settingGroups"`
}

// viewOf 组装主题视图。
func (h *Handler) viewOf(loaded *Loaded) View {
	groups := make([]string, 0, len(loaded.settings.groups))
	for _, g := range loaded.settings.groups {
		groups = append(groups, g.Name)
	}
	hasScreenshot := false
	if _, err := fsStat(loaded.FS, FileScreenshot); err == nil {
		hasScreenshot = true
	}
	return View{
		Name:          loaded.Manifest.Name,
		Label:         loaded.Manifest.Label,
		Version:       loaded.Manifest.Version,
		Description:   loaded.Manifest.Description,
		Author:        loaded.Manifest.Author,
		Homepage:      loaded.Manifest.Homepage,
		Repo:          loaded.Manifest.Repo,
		License:       loaded.Manifest.License,
		Builtin:       loaded.Builtin,
		Active:        loaded.Manifest.Name == h.module.registry.ActiveName(),
		Templates:     TemplateStatuses(loaded.Templates),
		PageTemplates: PageTemplates(loaded.Templates),
		HasScreenshot: hasScreenshot,
		SettingGroups: groups,
	}
}

// listBody 是主题列表的响应体。
type listBody struct {
	Items  []View `json:"items"`
	Active string `json:"active" doc:"当前启用的主题名"`
	// Broken 是加载失败的主题及其原因。
	Broken map[string]string `json:"broken"`
	// DevMode 为真表示模板改动会自动重载。
	DevMode bool `json:"devMode"`
}

type listOutput struct{ Body listBody }

type nameInput struct {
	Name string `path:"name" minLength:"1" maxLength:"64" doc:"主题标识"`
}

type viewOutput struct{ Body View }

func (h *Handler) list(_ context.Context, _ *struct{}) (*listOutput, error) {
	loaded := h.module.registry.List()
	items := make([]View, 0, len(loaded))
	for _, l := range loaded {
		items = append(items, h.viewOf(l))
	}
	return &listOutput{Body: listBody{
		Items:   items,
		Active:  h.module.registry.ActiveName(),
		Broken:  h.module.registry.Broken(),
		DevMode: h.module.registry.DevMode(),
	}}, nil
}

func (h *Handler) get(_ context.Context, in *nameInput) (*viewOutput, error) {
	loaded, ok := h.module.registry.Get(in.Name)
	if !ok {
		return nil, huma.Error404NotFound(ErrNotFound.Error())
	}
	return &viewOutput{Body: h.viewOf(loaded)}, nil
}

// ---------- 安装 ----------

// maxPackageUpload 是主题包上传的字节上限。
//
// 比附件上限小得多：主题是模板与少量静态资源，正常不会超过几兆；
// 几十兆的「主题包」通常是把 node_modules 或设计稿一起打进去了。
const maxPackageUpload = 32 << 20

type installForm struct {
	File huma.FormFile `form:"file" contentType:"application/octet-stream" required:"true" doc:"主题 zip 包"`
	// Overwrite 为真时覆盖同名主题。
	Overwrite string `form:"overwrite" required:"false" doc:"传 true 时覆盖同名主题"`
}

type installInput struct {
	RawBody huma.MultipartFormFiles[installForm]
}

func (h *Handler) install(_ context.Context, in *installInput) (*viewOutput, error) {
	form := in.RawBody.Data()
	if !form.File.IsSet {
		return nil, huma.Error400BadRequest("缺少上传文件")
	}
	defer func() { _ = form.File.Close() }()

	if form.File.Size > maxPackageUpload {
		return nil, huma.Error413RequestEntityTooLarge("主题包超过大小上限")
	}

	// zip.NewReader 需要 io.ReaderAt，必须先完整读入；上限已在上面卡过。
	data, err := io.ReadAll(io.LimitReader(form.File, maxPackageUpload+1))
	if err != nil {
		return nil, huma.Error400BadRequest("读取上传文件失败")
	}
	if int64(len(data)) > maxPackageUpload {
		return nil, huma.Error413RequestEntityTooLarge("主题包超过大小上限")
	}

	manifest, err := Install(h.module.root, bytes.NewReader(data), int64(len(data)),
		form.Overwrite == "true")
	if err != nil {
		return nil, mapError(err)
	}
	if err := h.module.registry.Load(manifest.Name); err != nil {
		return nil, mapError(err)
	}

	loaded, ok := h.module.registry.Get(manifest.Name)
	if !ok {
		return nil, huma.Error500InternalServerError("主题安装后加载失败")
	}
	return &viewOutput{Body: h.viewOf(loaded)}, nil
}

func (h *Handler) activate(ctx context.Context, in *nameInput) (*viewOutput, error) {
	if err := h.module.registry.Activate(in.Name); err != nil {
		return nil, mapError(err)
	}
	if h.module.state != nil {
		if err := h.module.state.SetActive(ctx, in.Name); err != nil {
			return nil, err
		}
	}
	loaded, _ := h.module.registry.Get(in.Name)
	return &viewOutput{Body: h.viewOf(loaded)}, nil
}

func (h *Handler) delete(ctx context.Context, in *nameInput) (*struct{}, error) {
	loaded, ok := h.module.registry.Get(in.Name)
	if !ok {
		return nil, huma.Error404NotFound(ErrNotFound.Error())
	}
	if loaded.Builtin {
		return nil, huma.Error409Conflict(ErrBuiltin.Error())
	}
	// 删掉正在用的主题会让站点当场换皮，且站长未必意识到发生了什么。
	if in.Name == h.module.registry.ActiveName() {
		return nil, huma.Error409Conflict("当前启用的主题不可删除，请先切换到其他主题")
	}

	if err := Remove(h.module.root, in.Name); err != nil {
		return nil, mapError(err)
	}
	h.module.registry.Unload(in.Name)
	if h.module.settings != nil {
		if err := h.module.settings.Purge(ctx, in.Name); err != nil {
			return nil, err
		}
	}
	return nil, nil //nolint:nilnil // 无响应体，huma 按 DefaultStatus 返回 204
}

func (h *Handler) reload(_ context.Context, in *nameInput) (*viewOutput, error) {
	if _, ok := h.module.registry.Get(in.Name); !ok {
		// 目录可能是刚放进去的，先尝试首次加载。
		if err := h.module.registry.Load(in.Name); err != nil {
			return nil, mapError(err)
		}
	} else if err := h.module.registry.Load(in.Name); err != nil {
		return nil, mapError(err)
	}
	loaded, _ := h.module.registry.Get(in.Name)
	return &viewOutput{Body: h.viewOf(loaded)}, nil
}

// ---------- 主题设置 ----------

// settingsGroupView 是主题设置分组的接口视图。
//
// 字段与站点设置的 groupView 一致：Console 的通用表单引擎只认这一种形态。
type settingsGroupView struct {
	Name        string         `json:"name"`
	Label       string         `json:"label"`
	Description string         `json:"description"`
	Order       int            `json:"order"`
	Schema      map[string]any `json:"schema" doc:"JSON Schema 2020-12 子集 + x-widget"`
	Defaults    map[string]any `json:"defaults"`
	Values      map[string]any `json:"values" doc:"当前有效值：缺省值被已保存值覆盖"`
}

type settingsListBody struct {
	Items []settingsGroupView `json:"items"`
}

type settingsListOutput struct{ Body settingsListBody }

type settingsUpdateInput struct {
	Name  string         `path:"name" minLength:"1" maxLength:"64"`
	Group string         `path:"group" minLength:"1" maxLength:"64"`
	Body  map[string]any `doc:"分组的值对象"`
}

type settingsGroupOutput struct{ Body settingsGroupView }

func (h *Handler) listSettings(ctx context.Context, in *nameInput) (*settingsListOutput, error) {
	loaded, ok := h.module.registry.Get(in.Name)
	if !ok {
		return nil, huma.Error404NotFound(ErrNotFound.Error())
	}
	stored, err := h.storedSettings(ctx, in.Name)
	if err != nil {
		return nil, err
	}

	effective := loaded.settings.effectiveSettings(stored)
	items := make([]settingsGroupView, 0, len(loaded.settings.groups))
	for _, g := range loaded.settings.groups {
		items = append(items, settingsGroupView{
			Name:        g.Name,
			Label:       g.Label,
			Description: g.Description,
			Order:       g.Order,
			Schema:      g.validator.Doc(),
			Defaults:    g.defaultValues(),
			Values:      effective[g.Name],
		})
	}
	return &settingsListOutput{Body: settingsListBody{Items: items}}, nil
}

func (h *Handler) updateSettings(ctx context.Context, in *settingsUpdateInput) (*settingsGroupOutput, error) {
	loaded, ok := h.module.registry.Get(in.Name)
	if !ok {
		return nil, huma.Error404NotFound(ErrNotFound.Error())
	}
	group, ok := loaded.settings.group(in.Group)
	if !ok {
		return nil, huma.Error404NotFound("设置分组不存在")
	}

	// 与站点设置同样的语义：先合并缺省值再整体校验，保存的是完整对象。
	effective := group.coerceInts(settings.Merge(group.defaults, in.Body))
	if err := group.validator.Validate(effective); err != nil {
		return nil, validationError(err)
	}
	if h.module.settings == nil {
		return nil, huma.Error500InternalServerError("主题设置存储不可用")
	}
	if err := h.module.settings.Save(ctx, in.Name, in.Group, effective); err != nil {
		return nil, err
	}

	return &settingsGroupOutput{Body: settingsGroupView{
		Name:        group.Name,
		Label:       group.Label,
		Description: group.Description,
		Order:       group.Order,
		Schema:      group.validator.Doc(),
		Defaults:    group.defaultValues(),
		Values:      effective,
	}}, nil
}

// storedSettings 读取某主题已保存的设置值。
func (h *Handler) storedSettings(ctx context.Context, name string) (map[string]map[string]any, error) {
	if h.module.settings == nil {
		return map[string]map[string]any{}, nil
	}
	return h.module.settings.Load(ctx, name)
}

// ---------- Public ----------

// activeBody 是公开的当前主题信息。
type activeBody struct {
	Name    string `json:"name"`
	Label   string `json:"label"`
	Version string `json:"version"`
}

type activeOutput struct{ Body activeBody }

func (h *Handler) publicActive(_ context.Context, _ *struct{}) (*activeOutput, error) {
	loaded := h.module.registry.Active()
	return &activeOutput{Body: activeBody{
		Name:    loaded.Manifest.Name,
		Label:   loaded.Manifest.Label,
		Version: loaded.Manifest.Version,
	}}, nil
}

// ---------- 错误映射 ----------

// mapError 把本包的哨兵错误映射为 HTTP 错误；其余错误交给 huma 按 500 处理。
func mapError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrNotFound):
		return huma.Error404NotFound(ErrNotFound.Error())
	case errors.Is(err, ErrAlreadyExists):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, ErrBuiltin):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, ErrInvalidPackage):
		// 主题包的问题要把原因原样告诉上传者，否则他无从修：
		// 这里的细节来自他自己上传的文件，不涉及服务端内部信息。
		return huma.Error422UnprocessableEntity(err.Error())
	case errors.Is(err, ErrTemplateNotFound):
		return huma.Error422UnprocessableEntity(err.Error())
	}
	return err
}

// validationError 把设置校验失败转成 422 与逐条明细。
func validationError(err error) error {
	var verr *settings.ValidationError
	if errors.As(err, &verr) {
		details := make([]error, 0, len(verr.Details))
		for i := range verr.Details {
			d := verr.Details[i]
			details = append(details, &huma.ErrorDetail{Message: d.Message, Location: d.Location})
		}
		return huma.Error422UnprocessableEntity("主题设置校验失败", details...)
	}
	return err
}
