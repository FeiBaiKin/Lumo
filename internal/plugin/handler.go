package plugin

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/FeiBaiKin/lumo/internal/auth"
	"github.com/FeiBaiKin/lumo/internal/auth/perm"
	"github.com/FeiBaiKin/lumo/internal/settings"
)

var tagPlugins = []string{"plugins"}

// pathPlugins 是插件集合的接口路径。
const pathPlugins = "/plugins"

// maxPackageUpload 是插件包上传的字节上限。
//
// 与主题同一档：插件包是清单加少量声明文件，正常不会超过几兆。
const maxPackageUpload = 32 << 20

// Handler 提供插件的 Console 接口。
//
// 全部操作都要求 plugins:manage：插件能往后台加页面、改设置，门槛与主题同级。
type Handler struct {
	module *Module
}

// NewHandler 构造 Handler。
func NewHandler(module *Module) *Handler { return &Handler{module: module} }

// pluginView 是一个插件的接口视图。
type pluginView struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Author      string `json:"author"`
	Homepage    string `json:"homepage"`
	Repo        string `json:"repo"`
	License     string `json:"license"`
	Requires    string `json:"requires" doc:"所需的 Lumo 版本范围；空串表示不限制"`
	Enabled     bool   `json:"enabled"`
	// Broken 非空表示这个插件不可用及原因（目录被手工删除、清单读不出来等）。
	//
	// 报出来而不是从列表里隐掉：用户需要知道自己装过的东西出了什么问题。
	Broken string `json:"broken,omitempty"`
	// SettingGroups 是该插件声明的设置分组名。
	SettingGroups []string `json:"settingGroups"`
	// Runtime 是后端的运行方式：空串为纯声明式，wasm 为带后端。
	Runtime string `json:"runtime"`
	// Capabilities 是插件要用的能力，逐条写成给站长看的话。
	Capabilities []capabilityLine `json:"capabilities"`
	// NeedsConsent 为真表示启用前要先确认这些能力。
	NeedsConsent bool `json:"needsConsent"`
	// DisabledReason 是系统停用它的原因（连续出错、新版本多要了能力、后端加载失败）；手动停用时为空串。
	DisabledReason string `json:"disabledReason"`
	// Running 为真表示后端正在运行。
	Running bool `json:"running"`
	// SDK 是后端编译时用的 SDK 版本；没在运行时为空串。
	SDK string `json:"sdk"`
}

type pluginListBody struct {
	Items []pluginView `json:"items"`
}

type pluginListOutput struct{ Body pluginListBody }

type pluginOutput struct{ Body pluginView }

type nameInput struct {
	Name string `path:"name" minLength:"1" maxLength:"64"`
}

type enabledInput struct {
	Name string `path:"name" minLength:"1" maxLength:"64"`
	Body struct {
		Enabled bool `json:"enabled"`
		// AcceptCapabilities 表示站长已看过并同意插件要用的能力。
		AcceptCapabilities bool `json:"acceptCapabilities,omitempty" doc:"启用声明了能力的插件时须为 true，表示站长已确认"`
	}
}

type installForm struct {
	File huma.FormFile `form:"file" contentType:"application/octet-stream" required:"true" doc:"插件 zip 包"`
}

type installInput struct {
	RawBody huma.MultipartFormFiles[installForm]
}

// pluginSettingsView 是插件设置分组的接口视图。
//
// 字段与站点设置、主题设置的视图一致：Console 的通用表单引擎只认这一种形态。
//
// 类型名刻意带 plugin 前缀：huma 的 Schema 注册表按**类型名跨模块索引**，
// 两个包里各有一个 settingsGroupView 会在注册时 panic。
// 这个坑只有整机装配那一刻才炸，单模块的测试永远碰不到。
type pluginSettingsView struct {
	Name        string         `json:"name"`
	Label       string         `json:"label"`
	Description string         `json:"description"`
	Order       int            `json:"order"`
	Schema      map[string]any `json:"schema" doc:"JSON Schema 2020-12 子集 + x-widget"`
	Defaults    map[string]any `json:"defaults"`
	Values      map[string]any `json:"values" doc:"当前有效值：缺省值被已保存值覆盖"`
}

type pluginSettingsListBody struct {
	Items []pluginSettingsView `json:"items"`
}

type pluginSettingsListOutput struct{ Body pluginSettingsListBody }

type pluginSettingsUpdateInput struct {
	Name  string         `path:"name" minLength:"1" maxLength:"64"`
	Group string         `path:"group" minLength:"1" maxLength:"64"`
	Body  map[string]any `doc:"分组的值对象"`
}

type pluginSettingsGroupOutput struct{ Body pluginSettingsView }

// Register 挂载接口。
func (h *Handler) Register(console huma.API) {
	manage := huma.Middlewares{auth.RequirePermission(perm.PluginsManage)}

	huma.Register(console, huma.Operation{
		OperationID: "plugin-list",
		Method:      http.MethodGet,
		Path:        pathPlugins,
		Summary:     "列出已安装的插件",
		Description: "包含加载失败或目录被手工删除的插件，其 broken 字段说明原因。",
		Tags:        tagPlugins,
		Middlewares: manage,
	}, h.list)

	huma.Register(console, huma.Operation{
		OperationID:   "plugin-install",
		Method:        http.MethodPost,
		Path:          pathPlugins,
		Summary:       "安装或升级插件",
		Description:   "multipart/form-data 上传 zip 包。同名插件已存在时按升级处理，并保持它原来的启用状态。",
		Tags:          tagPlugins,
		DefaultStatus: http.StatusCreated,
		Middlewares:   manage,
		Errors:        []int{http.StatusBadRequest, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity},
	}, h.install)

	huma.Register(console, huma.Operation{
		OperationID: "plugin-set-enabled",
		Method:      http.MethodPut,
		Path:        "/plugins/{name}/enabled",
		Summary:     "启用或停用插件",
		Description: "启用后插件的设置分组立即可用，不需要重启。插件声明了能力而站长还没确认过时，" +
			"启用请求须带 acceptCapabilities: true，否则返回 409；带后端的插件在启用时编译加载，加载失败返回 422。",
		Tags:        tagPlugins,
		Middlewares: manage,
		Errors:      []int{http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity},
	}, h.setEnabled)

	huma.Register(console, huma.Operation{
		OperationID: "plugin-uninstall",
		Method:      http.MethodDelete,
		Path:        "/plugins/{name}",
		Summary:     "卸载插件",
		Description: "删除插件目录及其状态与设置值。不可撤销。",
		Tags:        tagPlugins,
		Middlewares: manage,
		Errors:      []int{http.StatusNotFound},
	}, h.uninstall)

	huma.Register(console, huma.Operation{
		OperationID: "plugin-list-settings",
		Method:      http.MethodGet,
		Path:        "/plugins/{name}/settings",
		Summary:     "列出插件的设置分组",
		Tags:        tagPlugins,
		Middlewares: manage,
		Errors:      []int{http.StatusNotFound},
	}, h.listSettings)

	huma.Register(console, huma.Operation{
		OperationID: "plugin-update-settings",
		Method:      http.MethodPut,
		Path:        "/plugins/{name}/settings/{group}",
		Summary:     "更新插件的设置分组",
		Description: "请求体为该分组的值对象，与缺省值按顶层键合并后整体校验并保存。",
		Tags:        tagPlugins,
		Middlewares: manage,
		Errors:      []int{http.StatusNotFound, http.StatusUnprocessableEntity},
	}, h.updateSettings)
}

// viewOf 把注册表里的插件转成接口视图。
func (h *Handler) viewOf(loaded *Loaded) pluginView {
	groups := make([]string, 0, len(loaded.Groups))
	for i := range loaded.Groups {
		groups = append(groups, loaded.Groups[i].Name)
	}
	view := pluginView{
		Name:           loaded.ID(),
		DisplayName:    loaded.Manifest.Spec.DisplayName,
		Version:        loaded.Manifest.Spec.Version,
		Description:    loaded.Manifest.Spec.Description,
		Author:         loaded.Manifest.Spec.Author.Name,
		Homepage:       loaded.Manifest.Spec.Homepage,
		Repo:           loaded.Manifest.Spec.Repo,
		License:        loaded.Manifest.Spec.License,
		Requires:       loaded.Manifest.Spec.Requires,
		Enabled:        loaded.Enabled,
		SettingGroups:  groups,
		Runtime:        loaded.Manifest.Spec.Runtime,
		Capabilities:   h.module.describeCapabilities(&loaded.Manifest.Spec.Capabilities),
		NeedsConsent:   loaded.NeedsConsent(),
		DisabledReason: loaded.DisabledReason,
	}
	if backend, ok := h.module.registry.Backend(loaded.ID()); ok {
		view.Running, view.SDK = true, backend.Description().SDK
	}
	return view
}

func (h *Handler) list(_ context.Context, _ *struct{}) (*pluginListOutput, error) {
	loaded := h.module.registry.List()
	broken := h.module.registry.Broken()
	items := make([]pluginView, 0, len(loaded)+len(broken))
	for _, item := range loaded {
		view := h.viewOf(item)
		if reason, ok := broken[item.ID()]; ok {
			view.Broken = reason
		}
		items = append(items, view)
	}
	// 库里有、目录不在的插件没有 Loaded，只有一条原因。
	// 它们也要列出来——用户需要知道自己装过的东西出了什么问题。
	for name, reason := range broken {
		if _, ok := h.module.registry.Get(name); ok {
			continue
		}
		items = append(items, pluginView{Name: name, DisplayName: name, Broken: reason})
	}
	return &pluginListOutput{Body: pluginListBody{Items: items}}, nil
}

func (h *Handler) install(ctx context.Context, in *installInput) (*pluginOutput, error) {
	form := in.RawBody.Data()
	if !form.File.IsSet {
		return nil, huma.Error400BadRequest("缺少上传文件")
	}
	defer func() { _ = form.File.Close() }()

	if form.File.Size > maxPackageUpload {
		return nil, huma.Error413RequestEntityTooLarge("插件包超过大小上限")
	}
	// zip.NewReader 需要 io.ReaderAt，必须先完整读入；上限已在上面卡过。
	data, err := io.ReadAll(io.LimitReader(form.File, maxPackageUpload+1))
	if err != nil {
		return nil, huma.Error400BadRequest("读取上传文件失败")
	}
	if int64(len(data)) > maxPackageUpload {
		return nil, huma.Error413RequestEntityTooLarge("插件包超过大小上限")
	}

	manifest, err := h.module.registry.Install(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, mapError(err)
	}
	loaded, ok := h.module.registry.Get(manifest.Metadata.Name)
	if !ok {
		return nil, huma.Error500InternalServerError("插件安装后加载失败")
	}
	return &pluginOutput{Body: h.viewOf(loaded)}, nil
}

func (h *Handler) setEnabled(ctx context.Context, in *enabledInput) (*pluginOutput, error) {
	if err := h.module.registry.SetEnabled(ctx, in.Name, in.Body.Enabled, in.Body.AcceptCapabilities); err != nil {
		return nil, mapError(err)
	}
	loaded, ok := h.module.registry.Get(in.Name)
	if !ok {
		return nil, huma.Error404NotFound(ErrNotFound.Error())
	}
	return &pluginOutput{Body: h.viewOf(loaded)}, nil
}

func (h *Handler) uninstall(ctx context.Context, in *nameInput) (*struct{}, error) {
	if err := h.module.registry.Uninstall(ctx, in.Name); err != nil {
		return nil, mapError(err)
	}
	return nil, nil
}

// requireEnabled 取一个插件，且要求它是启用状态。
//
// 停用的插件不给读设置：入口在界面上已经收起，接口再放行会留下一条
// 「停用了还能改」的缝，而那种缝迟早会有人用上。
func (h *Handler) requireEnabled(name string) (*Loaded, error) {
	loaded, ok := h.module.registry.Get(name)
	if !ok {
		return nil, huma.Error404NotFound(ErrNotFound.Error())
	}
	if !loaded.Enabled {
		return nil, huma.Error404NotFound("插件未启用")
	}
	return loaded, nil
}

func (h *Handler) listSettings(ctx context.Context, in *nameInput) (*pluginSettingsListOutput, error) {
	loaded, err := h.requireEnabled(in.Name)
	if err != nil {
		return nil, err
	}
	stored, err := h.module.store.LoadSettings(ctx, in.Name)
	if err != nil {
		return nil, err
	}
	items := make([]pluginSettingsView, 0, len(loaded.Groups))
	for i := range loaded.Groups {
		group := &loaded.Groups[i]
		defaults := group.validator.DefaultValues()
		items = append(items, pluginSettingsView{
			Name:        group.Name,
			Label:       group.Label,
			Description: group.Description,
			Order:       group.Order,
			Schema:      group.validator.Doc(),
			Defaults:    defaults,
			Values:      settings.Merge(defaults, stored[group.Name]),
		})
	}
	return &pluginSettingsListOutput{Body: pluginSettingsListBody{Items: items}}, nil
}

func (h *Handler) updateSettings(ctx context.Context, in *pluginSettingsUpdateInput) (*pluginSettingsGroupOutput, error) {
	loaded, err := h.requireEnabled(in.Name)
	if err != nil {
		return nil, err
	}
	group, ok := loaded.SettingGroup(in.Group)
	if !ok {
		return nil, huma.Error404NotFound("设置分组不存在")
	}

	// 与站点设置同样的语义：先合并缺省值再整体校验，保存的是完整对象。
	defaults := group.validator.DefaultValues()
	effective := settings.Merge(defaults, in.Body)
	if err := group.validator.Validate(effective); err != nil {
		return nil, validationError(err)
	}
	if err := h.module.store.SaveSettings(ctx, in.Name, in.Group, effective); err != nil {
		return nil, err
	}
	return &pluginSettingsGroupOutput{Body: pluginSettingsView{
		Name:        group.Name,
		Label:       group.Label,
		Description: group.Description,
		Order:       group.Order,
		Schema:      group.validator.Doc(),
		Defaults:    defaults,
		Values:      effective,
	}}, nil
}

// mapError 把领域错误映射成 HTTP 错误。
func mapError(err error) error {
	switch {
	case errors.Is(err, ErrNotFound):
		return huma.Error404NotFound(err.Error())
	case errors.Is(err, ErrAlreadyExists):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, ErrInvalidPackage), errors.Is(err, ErrBackend):
		return huma.Error422UnprocessableEntity(err.Error())
	case errors.Is(err, ErrConsentRequired):
		return huma.Error409Conflict(err.Error())
	default:
		return err
	}
}

// validationError 把校验明细摊成 422。
func validationError(err error) error {
	var verr *settings.ValidationError
	if !errors.As(err, &verr) {
		return err
	}
	details := make([]error, 0, len(verr.Details))
	for i := range verr.Details {
		d := verr.Details[i]
		details = append(details, &huma.ErrorDetail{Message: d.Message, Location: d.Location})
	}
	return huma.Error422UnprocessableEntity("设置校验失败", details...)
}
