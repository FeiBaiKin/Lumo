package plugin

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/FeiBaiKin/lumo/internal/api"
	"github.com/FeiBaiKin/lumo/internal/auth"
	"github.com/FeiBaiKin/lumo/internal/auth/perm"
	"github.com/FeiBaiKin/lumo/internal/settings"
)

// 插件数据的接口：卸载前看数据量、删掉保留的数据，以及资源页的增删改查。
//
// 保留数据的路径用 /plugin-data 而不是 /plugins/retained：后者会和 /plugins/{name}/…
// 抢同一段，一个恰好叫 retained 的插件就再也打不开了。

// 资源记录的接口路径。
const (
	pathRecords = "/plugins/{name}/resources/{resource}"
	pathRecord  = pathRecords + "/{id}"
)

type dataCountsOutput struct{ Body DataCounts }

// resourceView 是给后台渲染资源页用的声明。
type resourceView struct {
	Kind        string         `json:"kind"`
	Path        string         `json:"path" doc:"URL 里的复数段"`
	Label       string         `json:"label"`
	Description string         `json:"description"`
	Icon        string         `json:"icon"`
	Schema      map[string]any `json:"schema" doc:"JSON Schema 2020-12 子集 + x-widget"`
	Defaults    map[string]any `json:"defaults"`
	Columns     []string       `json:"columns" doc:"列表页显示的字段"`
	Title       string         `json:"title" doc:"用作记录标题的字段"`
	Editable    bool           `json:"editable" doc:"为 false 时后台只能查看"`
}

type resourceListBody struct {
	Items []resourceView `json:"items"`
}

type resourceListOutput struct{ Body resourceListBody }

type recordListInput struct {
	Name     string `path:"name" minLength:"1" maxLength:"64"`
	Resource string `path:"resource" minLength:"1" maxLength:"64"`
	api.PageParams
	Q    string `query:"q" maxLength:"100" doc:"在标题字段里模糊查找"`
	Sort string `query:"sort" maxLength:"64" doc:"排序字段；前面加 - 为倒序；留空按新建先后倒序"`
}

type recordInput struct {
	Name     string `path:"name" minLength:"1" maxLength:"64"`
	Resource string `path:"resource" minLength:"1" maxLength:"64"`
	ID       int64  `path:"id" minimum:"1"`
}

type recordWriteInput struct {
	Name     string `path:"name" minLength:"1" maxLength:"64"`
	Resource string `path:"resource" minLength:"1" maxLength:"64"`
	Body     struct {
		Data map[string]any `json:"data" doc:"记录的字段值"`
	}
}

type recordUpdateInput struct {
	Name     string `path:"name" minLength:"1" maxLength:"64"`
	Resource string `path:"resource" minLength:"1" maxLength:"64"`
	ID       int64  `path:"id" minimum:"1"`
	Body     struct {
		Data map[string]any `json:"data" doc:"记录的字段值，整体替换"`
	}
}

type recordOutput struct{ Body ResourceRecord }

type recordPageOutput struct{ Body api.Page[ResourceRecord] }

// registerData 挂载插件数据与资源页的接口。
func (h *Handler) registerData(console huma.API) {
	manage := huma.Middlewares{auth.RequirePermission(perm.PluginsManage)}

	huma.Register(console, huma.Operation{
		OperationID: "plugin-data-counts",
		Method:      http.MethodGet,
		Path:        "/plugin-data/{name}",
		Summary:     "插件的数据量",
		Description: "设置分组、键值与资源记录各有多少，卸载前给站长看。插件卸载后（保留了数据）也能查。",
		Tags:        tagPlugins,
		Middlewares: manage,
	}, h.dataCounts)

	huma.Register(console, huma.Operation{
		OperationID:   "plugin-data-purge",
		Method:        http.MethodDelete,
		Path:          "/plugin-data/{name}",
		Summary:       "删除卸载后保留的插件数据",
		Description:   "只能删已卸载插件留下的数据；已安装的插件请走卸载。不可撤销。",
		Tags:          tagPlugins,
		Middlewares:   manage,
		DefaultStatus: http.StatusNoContent,
		Errors:        []int{http.StatusConflict},
	}, h.purgeData)

	huma.Register(console, huma.Operation{
		OperationID: "plugin-resources",
		Method:      http.MethodGet,
		Path:        "/plugins/{name}/resources",
		Summary:     "插件声明的资源",
		Description: "只列出当前用户有权限查看的那些。插件须已启用。",
		Tags:        tagPlugins,
		Errors:      []int{http.StatusNotFound},
	}, h.resources)

	huma.Register(console, huma.Operation{
		OperationID: "plugin-records",
		Method:      http.MethodGet,
		Path:        pathRecords,
		Summary:     "列出资源记录",
		Tags:        tagPlugins,
		Errors:      []int{http.StatusForbidden, http.StatusNotFound},
	}, h.records)

	huma.Register(console, huma.Operation{
		OperationID:   "plugin-record-create",
		Method:        http.MethodPost,
		Path:          pathRecords,
		Summary:       "新建资源记录",
		Tags:          tagPlugins,
		DefaultStatus: http.StatusCreated,
		Errors:        []int{http.StatusForbidden, http.StatusNotFound, http.StatusUnprocessableEntity},
	}, h.createRecord)

	huma.Register(console, huma.Operation{
		OperationID: "plugin-record-get",
		Method:      http.MethodGet,
		Path:        pathRecord,
		Summary:     "读取资源记录",
		Tags:        tagPlugins,
		Errors:      []int{http.StatusForbidden, http.StatusNotFound},
	}, h.getRecord)

	huma.Register(console, huma.Operation{
		OperationID: "plugin-record-update",
		Method:      http.MethodPut,
		Path:        pathRecord,
		Summary:     "修改资源记录",
		Tags:        tagPlugins,
		Errors:      []int{http.StatusForbidden, http.StatusNotFound, http.StatusUnprocessableEntity},
	}, h.updateRecord)

	huma.Register(console, huma.Operation{
		OperationID:   "plugin-record-delete",
		Method:        http.MethodDelete,
		Path:          pathRecord,
		Summary:       "删除资源记录",
		Tags:          tagPlugins,
		DefaultStatus: http.StatusNoContent,
		Errors:        []int{http.StatusForbidden, http.StatusNotFound},
	}, h.deleteRecord)
}

func (h *Handler) dataStore() (*DataStore, error) {
	if h.module.data == nil {
		return nil, huma.Error503ServiceUnavailable("站点没有连接数据库")
	}
	return h.module.data, nil
}

func (h *Handler) dataCounts(ctx context.Context, in *nameInput) (*dataCountsOutput, error) {
	data, err := h.dataStore()
	if err != nil {
		return nil, err
	}
	counts, err := data.Counts(ctx, in.Name)
	if err != nil {
		return nil, err
	}
	return &dataCountsOutput{Body: counts}, nil
}

func (h *Handler) purgeData(ctx context.Context, in *nameInput) (*struct{}, error) {
	if _, installed := h.module.registry.Get(in.Name); installed {
		return nil, huma.Error409Conflict("这个插件还装着，要删它的数据请卸载它")
	}
	data, err := h.dataStore()
	if err != nil {
		return nil, err
	}
	if err := data.Purge(ctx, in.Name); err != nil {
		return nil, err
	}
	return nil, nil //nolint:nilnil // 无响应体，huma 按 DefaultStatus 返回 204
}

// resourceFor 取一个启用中插件的资源，并核对当前用户有没有它要的权限。
func (h *Handler) resourceFor(ctx context.Context, name, path string) (*Loaded, *Resource, error) {
	loaded, err := h.requireEnabled(name)
	if err != nil {
		return nil, nil, err
	}
	res, ok := loaded.ResourceByPath(path)
	if !ok {
		return nil, nil, huma.Error404NotFound("插件没有这种资源")
	}
	if !auth.MustFromContext(ctx).Has(res.Permission) {
		return nil, nil, auth.ForbiddenProblem(res.Permission)
	}
	return loaded, res, nil
}

// requireEditable 拒绝对只读资源的写操作。
func requireEditable(res *Resource) error {
	if !res.Editable {
		return huma.Error403Forbidden("这类数据由插件自己维护，后台只能查看")
	}
	return nil
}

func (h *Handler) resources(ctx context.Context, in *nameInput) (*resourceListOutput, error) {
	loaded, err := h.requireEnabled(in.Name)
	if err != nil {
		return nil, err
	}
	principal := auth.MustFromContext(ctx)
	items := make([]resourceView, 0, len(loaded.Resources))
	for i := range loaded.Resources {
		res := &loaded.Resources[i]
		if !principal.Has(res.Permission) {
			continue
		}
		items = append(items, resourceView{
			Kind: res.Kind, Path: res.Path, Label: res.Label, Description: res.Description, Icon: res.Icon,
			Schema: res.validator.Doc(), Defaults: res.validator.DefaultValues(),
			Columns: res.Columns, Title: res.Title, Editable: res.Editable,
		})
	}
	return &resourceListOutput{Body: resourceListBody{Items: items}}, nil
}

func (h *Handler) records(ctx context.Context, in *recordListInput) (*recordPageOutput, error) {
	loaded, res, err := h.resourceFor(ctx, in.Name, in.Resource)
	if err != nil {
		return nil, err
	}
	data, err := h.dataStore()
	if err != nil {
		return nil, err
	}
	sortField, desc := in.Sort, false
	if sortField != "" && sortField[0] == '-' {
		sortField, desc = sortField[1:], true
	}
	if sortField != "" && !res.HasField(sortField) {
		return nil, huma.Error400BadRequest("不能按这个字段排序")
	}
	items, total, err := data.Records(ctx, loaded.ID(), res, &RecordQuery{
		Search: in.Q, Sort: sortField, Desc: desc, Limit: in.Limit(), Offset: in.Offset(),
	})
	if err != nil {
		return nil, err
	}
	return &recordPageOutput{Body: api.NewPage(items, in.PageParams, total)}, nil
}

func (h *Handler) getRecord(ctx context.Context, in *recordInput) (*recordOutput, error) {
	loaded, res, err := h.resourceFor(ctx, in.Name, in.Resource)
	if err != nil {
		return nil, err
	}
	data, err := h.dataStore()
	if err != nil {
		return nil, err
	}
	record, err := data.Record(ctx, loaded.ID(), res, in.ID)
	if err != nil {
		return nil, mapError(err)
	}
	return &recordOutput{Body: *record}, nil
}

func (h *Handler) createRecord(ctx context.Context, in *recordWriteInput) (*recordOutput, error) {
	loaded, res, err := h.resourceFor(ctx, in.Name, in.Resource)
	if err != nil {
		return nil, err
	}
	if editErr := requireEditable(res); editErr != nil {
		return nil, editErr
	}
	clean, err := res.Normalize(in.Body.Data)
	if err != nil {
		return nil, recordValidationError(err)
	}
	data, err := h.dataStore()
	if err != nil {
		return nil, err
	}
	record, err := data.CreateRecord(ctx, loaded.ID(), res, clean)
	if err != nil {
		return nil, mapError(err)
	}
	return &recordOutput{Body: *record}, nil
}

func (h *Handler) updateRecord(ctx context.Context, in *recordUpdateInput) (*recordOutput, error) {
	loaded, res, err := h.resourceFor(ctx, in.Name, in.Resource)
	if err != nil {
		return nil, err
	}
	if editErr := requireEditable(res); editErr != nil {
		return nil, editErr
	}
	clean, err := res.Normalize(in.Body.Data)
	if err != nil {
		return nil, recordValidationError(err)
	}
	data, err := h.dataStore()
	if err != nil {
		return nil, err
	}
	record, err := data.UpdateRecord(ctx, loaded.ID(), res, in.ID, clean)
	if err != nil {
		return nil, mapError(err)
	}
	return &recordOutput{Body: *record}, nil
}

func (h *Handler) deleteRecord(ctx context.Context, in *recordInput) (*struct{}, error) {
	loaded, res, err := h.resourceFor(ctx, in.Name, in.Resource)
	if err != nil {
		return nil, err
	}
	if editErr := requireEditable(res); editErr != nil {
		return nil, editErr
	}
	data, err := h.dataStore()
	if err != nil {
		return nil, err
	}
	if err := data.DeleteRecord(ctx, loaded.ID(), res, in.ID); err != nil {
		return nil, mapError(err)
	}
	return nil, nil //nolint:nilnil // 无响应体，huma 按 DefaultStatus 返回 204
}

// recordValidationError 把字段校验明细摊成 422，路径与设置接口同一形态，后台据此把错误落回字段。
func recordValidationError(err error) error {
	var verr *settings.ValidationError
	if !errors.As(err, &verr) {
		return err
	}
	return validationError(verr)
}
