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

	// cancel 取消消费协程的 run context，Start 时创建；消费协程的退出经
	// Service.Done 观察。
	cancel context.CancelFunc
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

// Start 实现 app.Starter：启动后台发信协程。
//
// 消费协程刻意不直接绑定信号 ctx：SIGTERM 到达后 HTTP 才开始排空，在途请求
// 仍会入队，此时消费者必须继续工作，直到 Close 在 HTTP 排空后有序停止它。
// 这里用 WithoutCancel 断开取消传播，改由 Close 负责取消。
func (m *Module) Start(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	m.cancel = cancel
	go m.service.Run(runCtx)
	return nil
}

// Close 实现 app.Closer：有序停止邮件队列。
//
// app.Close 在 HTTP 排空之后执行（cmd/lumo/serve.go 的 defer），到这里不会
// 再有业务请求入队。Service.Shutdown 在 ctx 期限内排空剩余邮件，随后取消
// 消费协程；仍未投递的数量写入日志，便于评估停机窗口内丢失的邮件。
func (m *Module) Close(ctx context.Context) error {
	if m.cancel == nil {
		// Start 未执行（如仅注册模块的 migrate 命令），没有消费者需要停止。
		return nil
	}
	m.service.Shutdown(ctx)
	m.cancel()
	select {
	case <-m.service.Done():
	case <-ctx.Done():
	}
	if m.logger != nil {
		if n := m.service.Undelivered(); n > 0 {
			m.logger.Warn("停机时仍有邮件未投递", slog.Int("undelivered", n))
		} else {
			m.logger.Info("邮件发送队列已排空", slog.Int("undelivered", 0))
		}
	}
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
