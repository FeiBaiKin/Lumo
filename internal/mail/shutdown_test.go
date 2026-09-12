package mail

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/config"
	"github.com/FeiBaiKin/lumo/internal/form"
	"github.com/FeiBaiKin/lumo/internal/settings"
)

// syncBuffer 是并发安全的日志缓冲，供断言停机日志使用（消费协程与测试并发写读）。
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// fakeSender 是测试用发信器：记录每封已发送邮件；block 非 nil 时阻塞到其关闭
// 或 ctx 结束，模拟 SMTP 服务器无响应。
type fakeSender struct {
	sent  chan *Message
	block chan struct{}
}

func (f *fakeSender) Send(ctx context.Context, msg *Message) error {
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	select {
	case f.sent <- msg:
	default:
	}
	return nil
}

// settingsProvider 是最小的「提供 settings 服务」模块，让 mail 模块无需真实
// settings 模块（及其数据库存储）即可装配。
type settingsProvider struct{ svc *settings.Service }

func (settingsProvider) Name() string { return settings.Name }

func (p settingsProvider) Register(a *app.App) error {
	a.Provide(settings.Name, p.svc)
	return nil
}

// enabledMailSettings 构造一个「已启用发信」的设置服务（无存储，取缺省值）。
func enabledMailSettings(t *testing.T) *settings.Service {
	t.Helper()

	svc := settings.NewService(nil)
	group := app.SettingGroup{
		Name: GroupMail,
		Form: form.New(form.NewSection("发信", form.Bool("enabled").Label("启用邮件发送").Default(true))),
	}
	if err := svc.RegisterGroups([]app.SettingGroup{group}); err != nil {
		t.Fatalf("注册 mail 设置分组失败: %v", err)
	}
	return svc
}

// newTestStack 装配一个只含 mail 消费链的最小应用，返回应用、模块与日志缓冲。
func newTestStack(t *testing.T, sender *fakeSender) (*app.App, *Module, *syncBuffer) {
	t.Helper()

	logs := &syncBuffer{}
	logger := slog.New(slog.NewTextHandler(logs, nil))
	application := app.New(&app.Options{Config: config.Default(), Logger: logger})
	mailModule := New()
	if err := application.Register(settingsProvider{svc: enabledMailSettings(t)}, mailModule); err != nil {
		t.Fatalf("装配模块失败: %v", err)
	}
	mailModule.service.newSender = func(*Settings, string) (Sender, error) { return sender, nil }
	return application, mailModule, logs
}

// waitFor 轮询等待条件成立，超时即失败。
func waitFor(t *testing.T, limit time.Duration, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("等待条件成立超时（%v）", limit)
}

// TestSignalKeepsConsumerAliveUntilQueueDrained 复现停机丢信场景：
// 信号到达后（HTTP 仍在排空）入队的邮件，消费者必须继续处理；
// Close 排空队列后未投递计数为 0，且停机后不再接受新邮件。
func TestSignalKeepsConsumerAliveUntilQueueDrained(t *testing.T) {
	const total = 5

	sender := &fakeSender{sent: make(chan *Message, total)}
	application, mailModule, logs := newTestStack(t, sender)

	signalCtx, stopSignal := context.WithCancel(context.Background())
	if err := application.Start(signalCtx); err != nil {
		t.Fatalf("启动模块失败: %v", err)
	}

	// 模拟 SIGTERM：HTTP 排空期间，在途请求仍会向队列写邮件。
	stopSignal()
	for i := range total {
		mailModule.service.Enqueue(context.Background(), &Message{
			To:      []string{"ops@example.com"},
			Subject: fmt.Sprintf("通知 %d", i),
			Text:    "正文",
		})
	}

	// 信号之后消费者必须仍然存活并把这批邮件发完。
	for i := range total {
		select {
		case <-sender.sent:
		case <-time.After(5 * time.Second):
			t.Fatalf("信号到达后消费者提前退出：只发出 %d/%d 封", i, total)
		}
	}

	closeCtx, cancelClose := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelClose()
	if err := application.Close(closeCtx); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}

	if n := mailModule.service.Undelivered(); n != 0 {
		t.Errorf("队列已排空，未投递数 = %d，期望 0", n)
	}
	if !strings.Contains(logs.String(), "邮件发送队列已排空") {
		t.Errorf("停机日志应报告队列已排空，实际日志:\n%s", logs.String())
	}

	// 停机开始后拒绝新入队。
	mailModule.service.Enqueue(context.Background(), &Message{
		To: []string{"ops@example.com"}, Subject: "迟到", Text: "正文",
	})
	if got := len(mailModule.service.queue); got != 0 {
		t.Errorf("停机后队列不应再增长，实际 %d 封", got)
	}
}

// TestShutdownReportsUndeliveredMail 验证期限用尽时：Close 不被未发送邮件
// 拖住，消费协程被停止，未投递数量（含正在发送的一封）写入日志。
func TestShutdownReportsUndeliveredMail(t *testing.T) {
	const total = 3

	blocked := make(chan struct{})
	defer close(blocked) // 测试收尾时释放，避免假发送器永久阻塞

	sender := &fakeSender{sent: make(chan *Message, total), block: blocked}
	application, mailModule, logs := newTestStack(t, sender)

	signalCtx, stopSignal := context.WithCancel(context.Background())
	if err := application.Start(signalCtx); err != nil {
		t.Fatalf("启动模块失败: %v", err)
	}
	stopSignal()

	for i := range total {
		mailModule.service.Enqueue(context.Background(), &Message{
			To:      []string{"ops@example.com"},
			Subject: fmt.Sprintf("通知 %d", i),
			Text:    "正文",
		})
	}

	// 等第一封进入发送，确保未投递计数包含正在发送的一封。
	waitFor(t, 2*time.Second, func() bool { return mailModule.service.inFlight.Load() != nil })

	closeCtx, cancelClose := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancelClose()

	start := time.Now()
	if err := application.Close(closeCtx); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("Close 被未发送邮件拖住，耗时 %v", elapsed)
	}

	if n := mailModule.service.Undelivered(); n != total {
		t.Errorf("未投递数 = %d，期望 %d（队列 2 封 + 正在发送 1 封）", n, total)
	}
	if !strings.Contains(logs.String(), "undelivered=3") {
		t.Errorf("停机日志应报告未投递数量 3，实际日志:\n%s", logs.String())
	}
}
