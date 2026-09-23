package console

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/FeiBaiKin/lumo/internal/version"
)

// BuildView 是构建信息接口的响应体。
//
// 不直接用 version.Info 作响应类型：huma 按类型名全局登记 Schema，Info 这种名字迟早撞车。
type BuildView struct {
	Version   string `json:"version" doc:"语义化版本号；源码直接构建时为 0.0.0-dev"`
	Commit    string `json:"commit" doc:"git 提交短哈希；构建时未注入且取不到 VCS 信息时为 unknown"`
	Date      string `json:"date" doc:"构建时间（RFC 3339）；取不到时为 unknown"`
	GoVersion string `json:"goVersion" doc:"编译所用的 Go 版本"`
	Platform  string `json:"platform" doc:"操作系统/架构，如 linux/amd64"`
}

// BuildHandler 提供当前运行构建的版本信息。
//
// 提交号与构建时间不放进 /healthz：那是免认证的运维探针，只回答「活着没有」，
// 没有理由对匿名请求多给构建细节。这里对所有已登录用户开放、不要求权限串，
// 与菜单接口同一档——关于页的「构建信息」卡片人人可见，提 issue 时贴的就是它。
type BuildHandler struct {
	view BuildView
}

// NewBuildHandler 构造构建信息处理器。构建信息在进程内不变，构造时取定。
func NewBuildHandler(info version.Info) *BuildHandler {
	return &BuildHandler{view: BuildView{
		Version:   info.Version,
		Commit:    info.Commit,
		Date:      info.Date,
		GoVersion: info.GoVersion,
		Platform:  info.Platform,
	}}
}

type buildOutput struct {
	Body BuildView
}

// Register 挂载构建信息接口。
func (h *BuildHandler) Register(console huma.API) {
	huma.Register(console, huma.Operation{
		OperationID: "console-build",
		Method:      http.MethodGet,
		Path:        "/build",
		Summary:     "获取当前运行的构建信息",
		Description: "返回版本号、提交、构建时间、Go 版本与平台，对所有已登录用户开放。",
		Tags:        []string{"console"},
	}, h.get)
}

func (h *BuildHandler) get(_ context.Context, _ *struct{}) (*buildOutput, error) {
	return &buildOutput{Body: h.view}, nil
}
