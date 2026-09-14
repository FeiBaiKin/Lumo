package mail

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/FeiBaiKin/lumo/internal/settings"
)

// queueSize 是待发邮件的缓冲条数。
//
// 队列满时丢弃并记日志，而不是阻塞调用方：评论能不能发出去，不该取决于
// SMTP 服务器此刻是否健在。
const queueSize = 256

// 重试策略：三次尝试，间隔递增。SMTP 的失败多半是瞬时的（连接超时、限流），
// 但也可能是配置写错，因此次数有限，不做无限重试。
var retryDelays = []time.Duration{2 * time.Second, 10 * time.Second, 30 * time.Second}

// sendTimeout 是单次发信的超时。
const sendTimeout = 30 * time.Second

// drainPollInterval 是停机等待队列排空时的轮询间隔。
const drainPollInterval = 20 * time.Millisecond

// Service 是发信队列：Enqueue 立即返回，后台单协程按序发送并重试。
type Service struct {
	settings *settings.Service
	logger   *slog.Logger
	queue    chan *Message

	// closing 为真后拒绝新入队（停机第二阶段）。
	closing atomic.Bool
	// stop 由 Shutdown 关闭，通知消费协程退出（停机最后阶段）。
	stop chan struct{}
	// done 在 Run 返回时关闭。
	done chan struct{}
	// stopOnce 保证停止流程只执行一次。
	stopOnce sync.Once
	// inFlight 指向当前正在发送的邮件，用于统计停机时未投递的数量。
	inFlight atomic.Pointer[Message]
	// undelivered 是停机时快照下来的未投递数量。
	undelivered atomic.Int64

	// 发信器按配置缓存，设置改动后自动重建。
	mu       sync.Mutex
	cachedAt Settings
	cachedPw string
	cached   Sender

	// newSender 可在测试中替换，避免真的去连 SMTP。
	newSender func(cfg *Settings, password string) (Sender, error)

	// dropped 统计因队列满而丢弃的邮件数，供诊断。
	dropped atomic.Int64
}

// NewService 构造发信服务；settings 可为 nil（此时始终视为未启用）。
func NewService(svc *settings.Service, logger *slog.Logger) *Service {
	return &Service{
		settings:  svc,
		logger:    logger,
		queue:     make(chan *Message, queueSize),
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
		newSender: NewSMTPSender,
	}
}

// Settings 返回 mail 分组的有效值；未装配设置模块时返回未启用。
func (s *Service) Settings(ctx context.Context) Settings {
	var cfg Settings
	if s.settings == nil {
		return cfg
	}
	if err := s.settings.Get(ctx, GroupMail, &cfg); err != nil {
		if s.logger != nil {
			s.logger.Warn("读取邮件设置失败", slog.Any("error", err))
		}
		return Settings{}
	}
	return cfg
}

// Enabled 报告当前是否启用了发信。
func (s *Service) Enabled(ctx context.Context) bool {
	return s.Settings(ctx).Enabled
}

// Enqueue 把邮件放入队列，立即返回。
//
// 未启用发信时静默丢弃：站点没配 SMTP 是常态，不该让每次评论都在日志里刷错误。
// 停机开始后同样拒绝入队，但会记一条调试日志，便于排查停机窗口内的丢信。
func (s *Service) Enqueue(ctx context.Context, msg *Message) {
	if msg == nil || !s.Enabled(ctx) {
		return
	}
	if s.closing.Load() {
		if s.logger != nil {
			s.logger.Debug("邮件队列正在停机，拒绝新邮件", slog.String("subject", msg.Subject))
		}
		return
	}
	if err := msg.Validate(); err != nil {
		if s.logger != nil {
			s.logger.Warn("邮件格式不合法，已丢弃", slog.Any("error", err))
		}
		return
	}
	select {
	case s.queue <- msg:
	default:
		s.dropped.Add(1)
		if s.logger != nil {
			s.logger.Warn("发信队列已满，邮件被丢弃",
				slog.String("subject", msg.Subject), slog.Int64("dropped", s.dropped.Load()))
		}
	}
}

// Send 同步发送一封邮件，供「发送测试邮件」一类需要即时结果的场景使用。
func (s *Service) Send(ctx context.Context, msg *Message) error {
	sender, err := s.sender(ctx)
	if err != nil {
		return err
	}
	return sender.Send(ctx, msg)
}

// Run 运行后台发送协程，直到 Shutdown 停止服务。
//
// ctx 只用于单封邮件的发送超时，不作为退出信号：进程收到 SIGTERM 后 HTTP
// 还要排空，在途请求仍可能入队，消费者若随之退出，这些邮件就无人消费。
// 退出统一走 Shutdown（由 app.Closer 在 HTTP 排空之后调用），
// 由它保证「先排空、后停止」的顺序。
func (s *Service) Run(ctx context.Context) {
	defer close(s.done)
	for {
		select {
		case msg := <-s.queue:
			s.deliver(ctx, msg)
		case <-s.stop:
			return
		}
	}
}

// Shutdown 有序停止发信队列：拒绝新入队 → 在 ctx 期限内排空 → 停止消费协程。
//
// 必须在 HTTP 服务器排空之后调用（app.Closer 的时机），此时不会再有业务请求
// 入队。ctx 由 app 的 ShutdownTimeout 约束，等待有界，不会拖住进程退出。
// 未投递数量在停止时快照，由 Undelivered 读取；调用方负责记录。
func (s *Service) Shutdown(ctx context.Context) {
	s.closing.Store(true)

	ticker := time.NewTicker(drainPollInterval)
	defer ticker.Stop()
	for s.pending() > 0 {
		select {
		case <-ctx.Done():
			s.stopConsumer()
			return
		case <-ticker.C:
		}
	}
	s.stopConsumer()
}

// stopConsumer 关闭消费协程，幂等。
func (s *Service) stopConsumer() {
	s.stopOnce.Do(func() {
		// 快照此刻仍未发出的数量。停机后 Enqueue 已被拒绝，队列不再增长；
		// 正在发送的那一封若随后侥幸成功会多计 1，宁可多报不可漏报。
		s.undelivered.Store(int64(s.pending()))
		close(s.stop)
	})
}

// Done 在消费协程退出后关闭，供停机流程等待其收尾。
func (s *Service) Done() <-chan struct{} { return s.done }

// Undelivered 返回停机时未能投递的邮件数；Shutdown 完成后调用才有意义。
func (s *Service) Undelivered() int { return int(s.undelivered.Load()) }

// pending 返回当前尚未发出的邮件数：队列中排着的，加上正在发送的一封。
func (s *Service) pending() int {
	n := len(s.queue)
	if s.inFlight.Load() != nil {
		n++
	}
	return n
}

// deliver 发送一封邮件，失败时按 retryDelays 重试。
func (s *Service) deliver(ctx context.Context, msg *Message) {
	s.inFlight.Store(msg)
	defer s.inFlight.Store(nil)

	var lastErr error
	for attempt := 0; attempt <= len(retryDelays); attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(retryDelays[attempt-1]):
			}
		}

		sendCtx, cancel := context.WithTimeout(ctx, sendTimeout)
		err := s.sendOnce(sendCtx, msg)
		cancel()
		if err == nil {
			return
		}
		if errors.Is(err, ErrDisabled) || errors.Is(err, ErrMissingPassword) {
			// 配置问题重试多少次都一样，直接放弃。
			lastErr = err
			break
		}
		lastErr = err
	}
	if s.logger != nil && lastErr != nil {
		s.logger.Error("邮件发送失败，已放弃",
			slog.String("subject", msg.Subject), slog.Any("error", lastErr))
	}
}

// sendOnce 取发信器并发送一次。
func (s *Service) sendOnce(ctx context.Context, msg *Message) error {
	sender, err := s.sender(ctx)
	if err != nil {
		return err
	}
	return sender.Send(ctx, msg)
}

// sender 返回与当前设置匹配的发信器，配置未变时复用。
func (s *Service) sender(ctx context.Context) (Sender, error) {
	cfg := s.Settings(ctx)
	// 口令算进 cfg 的一部分再进缓存键：换口令就是要重建发信器，
	// 而这个比较是结构体相等，只要口令在结构体里，这一条就自动成立。
	password := cfg.Credential()

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cached != nil && s.cachedAt == cfg && s.cachedPw == password {
		return s.cached, nil
	}
	built, err := s.newSender(&cfg, password)
	if err != nil {
		return nil, err
	}
	s.cachedAt, s.cachedPw, s.cached = cfg, password, built
	return built, nil
}
