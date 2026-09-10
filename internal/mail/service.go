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

// Service 是发信队列：Enqueue 立即返回，后台单协程按序发送并重试。
type Service struct {
	settings *settings.Service
	logger   *slog.Logger
	queue    chan *Message

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
func (s *Service) Enqueue(ctx context.Context, msg *Message) {
	if msg == nil || !s.Enabled(ctx) {
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

// Run 启动后台发送协程，ctx 取消时退出。
func (s *Service) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-s.queue:
			s.deliver(ctx, msg)
		}
	}
}

// deliver 发送一封邮件，失败时按 retryDelays 重试。
func (s *Service) deliver(ctx context.Context, msg *Message) {
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
	password := Password()

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
