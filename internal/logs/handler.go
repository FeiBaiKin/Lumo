package logs

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/FeiBaiKin/lumo/internal/auth"
	"github.com/FeiBaiKin/lumo/internal/auth/perm"
)

// tagLogs 是 OpenAPI 分组标签。
var tagLogs = []string{"logs"}

// Handler 提供日志的 Console 接口。
//
// 只有 Console 平面，且全部需要 settings:manage：日志里有请求路径、错误详情与
// 内部状态，不是访客该看的东西。
//
// 沿用 settings:manage 而不是新增一条 logs:read：内置角色的权限只在首次创建时
// 写入，新增权限串意味着已有站点的管理员拿不到它，站长得手动补——为一页日志
// 引入一次升级摩擦不划算。语义上也说得通：能改 SMTP 口令的人看日志不算越权。
type Handler struct {
	reader *Reader
	// fileEnabled 为假表示日志没有落盘，页面据此给出「去开 log.file」的提示，
	// 而不是显示一个空列表让人以为系统没在记日志。
	fileEnabled bool
}

// NewHandler 构造 Handler。
func NewHandler(reader *Reader, fileEnabled bool) *Handler {
	return &Handler{reader: reader, fileEnabled: fileEnabled}
}

// Register 挂载接口。
func (h *Handler) Register(console huma.API) {
	guard := huma.Middlewares{auth.RequirePermission(perm.SettingsManage)}

	huma.Register(console, huma.Operation{
		OperationID: "logs-query",
		Method:      http.MethodGet,
		Path:        "/logs",
		Summary:     "查询运行日志",
		Description: "按级别、时间范围与关键词查询，最新的在前。级别是**最低**级别：" +
			"选 warn 会同时返回 error。扫描量有上限，触顶时 truncated 为真，" +
			"这时应缩小时间范围或指定单个文件。",
		Tags:        tagLogs,
		Middlewares: guard,
		Errors:      []int{http.StatusForbidden},
	}, h.query)

	huma.Register(console, huma.Operation{
		OperationID: "logs-files",
		Method:      http.MethodGet,
		Path:        "/logs/files",
		Summary:     "列出日志文件",
		Description: "按日期倒序返回日志文件，含大小与最后写入时间。",
		Tags:        tagLogs,
		Middlewares: guard,
		Errors:      []int{http.StatusForbidden},
	}, h.files)

	huma.Register(console, huma.Operation{
		OperationID: "logs-download",
		Method:      http.MethodGet,
		Path:        "/logs/download",
		Summary:     "下载日志文件",
		Description: "下载一个日志文件的原文。文件名必须是日志目录里真实存在的那些，" +
			"其余一律拒绝。",
		Tags:        tagLogs,
		Middlewares: guard,
		Errors:      []int{http.StatusForbidden, http.StatusNotFound},
	}, h.download)
}

// ---------- 查询 ----------

type queryInput struct {
	Level string `query:"level" enum:"debug,info,warn,error" doc:"最低级别，留空不限"`
	Q     string `query:"q" maxLength:"200" doc:"关键词，匹配消息与属性值"`
	From  string `query:"from" doc:"起始时间，RFC 3339"`
	To    string `query:"to" doc:"结束时间，RFC 3339"`
	File  string `query:"file" maxLength:"64" doc:"限定单个日志文件"`
	Page  int    `query:"page" minimum:"1" default:"1"`
	Size  int    `query:"size" minimum:"1" maximum:"200" default:"50"`
}

type queryOutput struct {
	Body struct {
		Items []Entry `json:"items"`
		Total int     `json:"total" doc:"本次扫描到的匹配条数，受扫描上限约束"`
		Page  int     `json:"page"`
		Size  int     `json:"size"`
		// Truncated 为真表示触到扫描上限，更早的日志没有纳入。
		Truncated bool `json:"truncated" doc:"触到扫描上限，更早的日志未纳入"`
		// FileEnabled 为假表示 log.file 是关的，日志没有落盘。
		FileEnabled bool `json:"fileEnabled" doc:"日志是否在落盘；为假时列表必然是空的"`
	}
}

func (h *Handler) query(_ context.Context, in *queryInput) (*queryOutput, error) {
	q := Query{Level: in.Level, Q: in.Q, File: in.File, Page: in.Page, Size: in.Size}

	var err error
	if q.From, err = parseTime(in.From); err != nil {
		return nil, huma.Error422UnprocessableEntity("from 不是合法的 RFC 3339 时间")
	}
	if q.To, err = parseTime(in.To); err != nil {
		return nil, huma.Error422UnprocessableEntity("to 不是合法的 RFC 3339 时间")
	}

	res, err := h.reader.Query(q)
	if err != nil {
		return nil, huma.Error500InternalServerError("读取日志失败")
	}

	out := &queryOutput{}
	out.Body.Items = res.Items
	out.Body.Total = res.Total
	out.Body.Page = res.Page
	out.Body.Size = res.Size
	out.Body.Truncated = res.Truncated
	out.Body.FileEnabled = h.fileEnabled
	// items 恒为数组而不是 null：前端不必为「没有日志」单独判一次类型。
	if out.Body.Items == nil {
		out.Body.Items = []Entry{}
	}
	return out, nil
}

// parseTime 解析 RFC 3339 时间；空串返回零值。
func parseTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339, s)
}

// ---------- 文件列表 ----------

type filesOutput struct {
	Body struct {
		Items       []FileInfo `json:"items"`
		FileEnabled bool       `json:"fileEnabled"`
		// Dir 是日志目录，页面在「没有日志」时把它显示出来，省得站长去猜。
		Dir string `json:"dir"`
	}
}

func (h *Handler) files(_ context.Context, _ *struct{}) (*filesOutput, error) {
	items, err := h.reader.Files()
	if err != nil {
		return nil, huma.Error500InternalServerError("读取日志目录失败")
	}
	out := &filesOutput{}
	out.Body.Items = items
	if out.Body.Items == nil {
		out.Body.Items = []FileInfo{}
	}
	out.Body.FileEnabled = h.fileEnabled
	out.Body.Dir = h.reader.Dir()
	return out, nil
}

// ---------- 下载 ----------

type downloadInput struct {
	File string `query:"file" required:"true" maxLength:"64" doc:"日志文件名"`
}

func (h *Handler) download(_ context.Context, in *downloadInput) (*huma.StreamResponse, error) {
	f, st, err := h.reader.Open(in.File)
	if err != nil {
		if errors.Is(err, ErrBadFileName) {
			return nil, huma.Error422UnprocessableEntity("文件名不合法")
		}
		if os.IsNotExist(err) {
			return nil, huma.Error404NotFound("日志文件不存在")
		}
		return nil, huma.Error500InternalServerError("打开日志文件失败")
	}

	// 流式写出而不是先读进内存：单个日志文件可以有上百 MB。
	return &huma.StreamResponse{
		Body: func(ctx huma.Context) {
			defer func() { _ = f.Close() }()
			ctx.SetHeader("Content-Type", "text/plain; charset=utf-8")
			ctx.SetHeader("Content-Disposition", `attachment; filename="`+in.File+`"`)
			ctx.SetHeader("Content-Length", strconv.FormatInt(st.Size(), 10))
			// 日志随时在追加，不能让中间层缓存。
			ctx.SetHeader("Cache-Control", "no-store")
			_, _ = io.Copy(ctx.BodyWriter(), f)
		},
	}, nil
}
