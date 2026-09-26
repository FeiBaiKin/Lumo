package update

import (
	"context"
	"errors"
	"io/fs"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/FeiBaiKin/lumo/internal/auth"
	"github.com/FeiBaiKin/lumo/internal/auth/perm"
)

// tagUpdate 是 OpenAPI 分组标签。
var tagUpdate = []string{"update"}

// Handler 提供在线升级的 Console 接口。
//
// 四个端点全部要 settings:manage，与日志页同一个理由：沿用而不是新增一条
// update:manage —— 内置角色的权限只在首次创建时写入，新增权限串会让已有站点的
// 管理员拿不到它。语义上也说得通：能改站点设置的人，本就是那个决定「要不要升级」的人。
type Handler struct {
	service *Service
}

// NewHandler 构造 Handler。
func NewHandler(service *Service) *Handler { return &Handler{service: service} }

// Register 挂载接口。
func (h *Handler) Register(console huma.API) {
	guard := huma.Middlewares{auth.RequirePermission(perm.SettingsManage)}

	huma.Register(console, huma.Operation{
		OperationID: "update-status",
		Method:      http.MethodGet,
		Path:        "/update",
		Summary:     "查看升级状态",
		Description: "一次返回当前版本、最近一次检查的结果、能否就地升级、" +
			"正在进行的升级进度与备份列表。升级过程中轮询它即可。",
		Tags:        tagUpdate,
		Middlewares: guard,
		Errors:      []int{http.StatusForbidden},
	}, h.status)

	huma.Register(console, huma.Operation{
		OperationID: "update-check",
		Method:      http.MethodPost,
		Path:        "/update/check",
		Summary:     "立即检查新版本",
		Description: "向更新源查询最新发布并刷新缓存。10 秒内的重复调用直接复用上次结果——" +
			"更新源对未认证请求有频率限制。",
		Tags:        tagUpdate,
		Middlewares: guard,
		Errors:      []int{http.StatusForbidden, http.StatusConflict, http.StatusBadGateway},
	}, h.check)

	huma.Register(console, huma.Operation{
		OperationID:   "update-apply",
		Method:        http.MethodPost,
		Path:          "/update/apply",
		Summary:       "下载并安装新版本",
		DefaultStatus: http.StatusAccepted,
		Description: "接口立即返回，下载、校验、替换与重启在后台进行，进度经 GET /update 查询。" +
			"安装完成后服务会自动重启，其间接口短暂不可用；替换前会把当前版本备份到 data/backups。",
		Tags:        tagUpdate,
		Middlewares: guard,
		Errors:      []int{http.StatusForbidden, http.StatusConflict, http.StatusBadGateway},
	}, h.apply)

	huma.Register(console, huma.Operation{
		OperationID: "update-set-mirrors",
		Method:      http.MethodPut,
		Path:        "/update/mirrors",
		Summary:     "设置下载加速地址",
		Description: "升级时这些地址与 GitHub 直连一起先下一小段测速，按快慢依次试。地址是前缀，" +
			"下载时拼成「前缀 + GitHub 原地址」；只收 https。发布包按 GitHub 接口给的 SHA-256 校验，" +
			"加速地址改不了内容。传空列表表示只直连 GitHub。存在数据目录里，跟着这台机器走。",
		Tags:        tagUpdate,
		Middlewares: guard,
		Errors:      []int{http.StatusForbidden, http.StatusUnprocessableEntity},
	}, h.setMirrors)

	huma.Register(console, huma.Operation{
		OperationID: "update-reset-mirrors",
		Method:      http.MethodDelete,
		Path:        "/update/mirrors",
		Summary:     "恢复内置的下载加速地址",
		Tags:        tagUpdate,
		Middlewares: guard,
		Errors:      []int{http.StatusForbidden},
	}, h.resetMirrors)

	huma.Register(console, huma.Operation{
		OperationID: "update-delete-backup",
		Method:      http.MethodDelete,
		Path:        "/update/backups/{name}",
		Summary:     "删除一份升级备份",
		Description: "删除 data/backups 下的一份旧版本备份。不可撤销：删掉之后就只能重新下载那个版本了。",
		Tags:        tagUpdate,
		Middlewares: guard,
		Errors:      []int{http.StatusForbidden, http.StatusNotFound, http.StatusUnprocessableEntity},
	}, h.deleteBackup)
}

// ---------- 输入输出 ----------

type statusOutput struct {
	Body UpdateStatus
}

type applyOutput struct {
	Body UpdateProgress
}

type mirrorsInput struct {
	Body struct {
		Mirrors []string `json:"mirrors" maxItems:"8" doc:"加速地址前缀，如 https://ghfast.top/"`
	}
}

type backupNameInput struct {
	Name string `path:"name" maxLength:"128" doc:"备份文件名，取自状态里的 backups[].name"`
}

// ---------- 处理器 ----------

func (h *Handler) status(_ context.Context, _ *struct{}) (*statusOutput, error) {
	return &statusOutput{Body: h.service.Status()}, nil
}

func (h *Handler) setMirrors(_ context.Context, in *mirrorsInput) (*statusOutput, error) {
	if err := h.service.SetMirrors(in.Body.Mirrors); err != nil {
		return nil, mapError(err)
	}
	return &statusOutput{Body: h.service.Status()}, nil
}

func (h *Handler) resetMirrors(_ context.Context, _ *struct{}) (*statusOutput, error) {
	if err := h.service.ResetMirrors(); err != nil {
		return nil, mapError(err)
	}
	return &statusOutput{Body: h.service.Status()}, nil
}

// check 同步查询更新源，随后返回完整状态。
//
// 返回整个 Status 而不是只返回查到的版本：前端拿到的就是它轮询用的那个形状，
// 少一处需要单独对付的响应类型。
func (h *Handler) check(ctx context.Context, _ *struct{}) (*statusOutput, error) {
	if _, err := h.service.Check(ctx); err != nil {
		if errors.Is(err, ErrDisabled) {
			return nil, huma.Error409Conflict(err.Error())
		}
		// 这条路径上的失败几乎都在对端：断网、限流、仓库还没发版。
		// 归给 502 而不是 500，站长看到的才是「更新源那头的问题」。
		return nil, huma.Error502BadGateway(err.Error())
	}
	return &statusOutput{Body: h.service.Status()}, nil
}

func (h *Handler) apply(ctx context.Context, _ *struct{}) (*applyOutput, error) {
	progress, err := h.service.Apply(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	return &applyOutput{Body: progress}, nil
}

func (h *Handler) deleteBackup(_ context.Context, in *backupNameInput) (*struct{}, error) {
	if err := h.service.DeleteBackup(in.Name); err != nil {
		return nil, mapError(err)
	}
	return nil, nil
}

// mapError 把领域错误翻成 HTTP。
//
// 原则是分清「谁需要做点什么」：409 是本机状态不对（功能关着、正在升级、
// 已是最新），502 是更新源那头的问题（限流、还没发版、没打这个平台的包），
// 两者给站长的下一步动作完全不同。
func mapError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrDisabled), errors.Is(err, ErrBusy),
		errors.Is(err, ErrNotUpgradable), errors.Is(err, ErrNoUpdate):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, ErrNoRelease), errors.Is(err, ErrRateLimited), errors.Is(err, ErrNoAsset):
		return huma.Error502BadGateway(err.Error())
	case errors.Is(err, ErrBadBackupName), errors.Is(err, ErrInvalidMirror):
		return huma.Error422UnprocessableEntity(err.Error())
	case errors.Is(err, fs.ErrNotExist):
		return huma.Error404NotFound("备份不存在")
	default:
		return huma.Error500InternalServerError(err.Error())
	}
}
