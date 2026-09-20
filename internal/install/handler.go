package install

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

// tagInstall 是安装端点在 OpenAPI 里的分组标签。
var tagInstall = []string{"install"}

// Handler 提供安装向导的三个端点。
type Handler struct {
	svc    *Service
	logger *slog.Logger
}

// NewHandler 构造 Handler。
func NewHandler(svc *Service, logger *slog.Logger) *Handler {
	return &Handler{svc: svc, logger: logger}
}

// Register 把安装端点挂到根 huma API（planes.API()）上。
//
// 三平面都不合适：它们的解析凭据中间件要查库，而此刻库还不存在 ——
// 浏览器只要带着任何旧 Cookie 过来就会被挡成 401，向导第一步都走不到。
//
// 三个端点一律无条件注册，包括已经装好的站点：OpenAPI 规范要稳定。若按运行时状态
// 条件注册，`lumo openapi` 在不同库上会导出不同的规范，进而污染 Console 的生成类型。
// 「能不能装」放在处理器内部判断。
//
// 这些端点没有 CSRF 中间件。跨站表单发不出 application/json，而 huma 只解析 JSON
// 请求体，所以浏览器侧的跨站提交在这里自然失效；至于能直接访问服务器的攻击者，
// 他本来就能自己跑一遍安装向导，CSRF 拦不住也不增加风险。
func (h *Handler) Register(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "install-status",
		Method:      http.MethodGet,
		Path:        "/api/v1/install/status",
		Summary:     "查询安装状态",
		Description: "报告向导是否仍然可用，以及各项前置检查的结果。",
		Tags:        tagInstall,
	}, h.status)

	huma.Register(api, huma.Operation{
		OperationID: "install-test-database",
		Method:      http.MethodPost,
		Path:        "/api/v1/install/database/test",
		Summary:     "测试数据库连接",
		Description: "用给定参数连一次目标库，回报版本、编码与库里已有的用户数。",
		Tags:        tagInstall,
		Errors:      []int{http.StatusBadRequest, http.StatusConflict},
	}, h.testDatabase)

	huma.Register(api, huma.Operation{
		OperationID:   "install-apply",
		Method:        http.MethodPost,
		Path:          "/api/v1/install/apply",
		Summary:       "执行安装",
		Description:   "建表、创建初始管理员、写入站点设置，并保存安装信息。成功后向导关闭，服务自动重启进入正常模式。",
		Tags:          tagInstall,
		DefaultStatus: http.StatusCreated,
		Errors:        []int{http.StatusBadRequest, http.StatusConflict},
	}, h.apply)
}

type statusOutput struct{ Body StatusView }

func (h *Handler) status(_ context.Context, _ *struct{}) (*statusOutput, error) {
	return &statusOutput{Body: h.svc.Status()}, nil
}

type testDatabaseInput struct{ Body DatabaseInput }

type testDatabaseOutput struct{ Body DatabaseInfo }

func (h *Handler) testDatabase(ctx context.Context, in *testDatabaseInput) (*testDatabaseOutput, error) {
	info, err := h.svc.TestDatabase(ctx, in.Body)
	if err != nil {
		return nil, h.problem(err)
	}
	return &testDatabaseOutput{Body: *info}, nil
}

type applyInput struct{ Body ApplyInput }

type applyOutput struct{ Body ApplyResult }

func (h *Handler) apply(ctx context.Context, in *applyInput) (*applyOutput, error) {
	result, err := h.svc.Apply(ctx, in.Body)
	if err != nil {
		return nil, h.problem(err)
	}
	return &applyOutput{Body: *result}, nil
}

// problem 把服务错误映射成 problem+json。
//
// 三档：已安装 → 409；站长改输入就能解决 → 400（消息原样回显，附上具体原因）；
// 其余原样返回，由 api 包记日志并对内留下通用措辞 —— 5xx 的细节不外泄是那里的既定规矩。
func (h *Handler) problem(err error) error {
	if errors.Is(err, ErrAlreadyInstalled) {
		return huma.Error409Conflict(err.Error())
	}
	var bad *BadRequestError
	if errors.As(err, &bad) {
		return huma.Error400BadRequest(bad.Error())
	}
	if h.logger != nil {
		h.logger.Error("安装失败", slog.Any("error", err))
	}
	return err
}
