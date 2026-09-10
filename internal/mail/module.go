package mail

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/auth"
	"github.com/FeiBaiKin/lumo/internal/auth/perm"
	"github.com/FeiBaiKin/lumo/internal/settings"
)

// Name 是模块名，也是 App.Provide 的键。
const Name = "mail"

// Module 是邮件模块。本模块无数据库表，故不实现 Migrator。
type Module struct {
	service *Service
	logger  *slog.Logger
}

// New 构造模块。
func New() *Module {
	return &Module{}
}

// Name 实现 app.Module。
func (m *Module) Name() string { return Name }

// Register 实现 app.Module。
func (m *Module) Register(a *app.App) error {
	m.logger = a.Logger()
	m.service = NewService(settings.From(a), m.logger)
	a.Provide(Name, m.service)
	return nil
}

// Settings 实现 app.SettingsProvider。
func (m *Module) Settings() []app.SettingGroup {
	return []app.SettingGroup{group()}
}

// Routes 实现 app.RouteProvider：只挂一个「发送测试邮件」端点。
//
// SMTP 配错是最常见的运维问题，而错在哪只有真发一封才知道。
func (m *Module) Routes(r app.Router) {
	huma.Register(r.Console(), huma.Operation{
		OperationID: "mail-send-test",
		Method:      http.MethodPost,
		Path:        "/mail/test",
		Summary:     "发送测试邮件",
		Description: "用当前 SMTP 配置同步发送一封测试邮件，失败时原样返回 SMTP 的报错，便于排查。",
		Tags:        []string{"mail"},
		Middlewares: huma.Middlewares{auth.RequirePermission(perm.SettingsManage)},
		Errors:      []int{http.StatusForbidden, http.StatusServiceUnavailable},
	}, m.sendTest)
}

// Start 实现 app.Starter：启动后台发信协程，ctx 取消时退出。
func (m *Module) Start(ctx context.Context) error {
	go m.service.Run(ctx)
	return nil
}

// Service 返回发信服务，供其他模块在未经 App.Lookup 时直接取用。
func (m *Module) Service() *Service { return m.service }

// From 取回发信服务；mail 模块未装配时返回 nil。
//
// 调用方须能在 nil 时退化（不发通知即可），以便单独测试其他模块。
func From(a *app.App) *Service {
	if a == nil {
		return nil
	}
	v, ok := a.Lookup(Name)
	if !ok {
		return nil
	}
	svc, _ := v.(*Service)
	return svc
}

// ---------- 测试邮件端点 ----------

type testInput struct {
	Body struct {
		To string `json:"to" format:"email" maxLength:"256" doc:"收件地址"`
	}
}

type testOutput struct {
	Body struct {
		Sent bool   `json:"sent"`
		Note string `json:"note" doc:"补充说明"`
	}
}

func (m *Module) sendTest(ctx context.Context, in *testInput) (*testOutput, error) {
	if !m.service.Enabled(ctx) {
		return nil, huma.Error503ServiceUnavailable("尚未启用邮件发送，请先在设置中开启并填写 SMTP 配置")
	}
	msg := &Message{
		To:      []string{in.Body.To},
		Subject: "Lumo 测试邮件",
		Text:    "这是一封来自 Lumo 的测试邮件。收到它说明 SMTP 配置可用。",
		HTML:    "<p>这是一封来自 Lumo 的测试邮件。收到它说明 SMTP 配置可用。</p>",
	}
	if err := m.service.Send(ctx, msg); err != nil {
		// 直接回传 SMTP 的原始报错：这是给站点管理员看的诊断信息，
		// 而能调这个接口的人已经握有 settings:manage。
		return nil, huma.Error503ServiceUnavailable("发送失败：" + err.Error())
	}
	out := &testOutput{}
	out.Body.Sent = true
	out.Body.Note = "已提交给 SMTP 服务器，请检查收件箱与垃圾邮件目录"
	return out, nil
}
