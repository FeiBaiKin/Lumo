package account

import (
	"context"
	"log/slog"
	"time"

	"github.com/FeiBaiKin/lumo/internal/ratelimit"
)

// Limiter 是窗口计数限流器，计数存在数据库里，多实例共享同一份额度。
//
// 不复用 auth.LoginLimiter：那个的语义是「**失败**计数，成功即清零」，
// 而这里要的是「总次数」——注册成功之后当然还要继续计数，
// 否则一个脚本每成功注册一次就把自己的额度刷回来。
type Limiter struct {
	counter *ratelimit.Counter
	logger  *slog.Logger
}

// NewLimiter 构造限流器。
func NewLimiter(counter *ratelimit.Counter, logger *slog.Logger) *Limiter {
	return &Limiter{counter: counter, logger: logger}
}

// Allow 记一次计数并报告是否仍在额度内。
//
// 窗口从**第一次**命中开始算，之后窗口内的每次调用都累加到同一个计数上；
// 窗口过期则重新开一个。计数出错时放行并记 warn：这些表单本身也要读写数据库，
// 为限流把它们打成 500 不划算。
func (l *Limiter) Allow(ctx context.Context, key string, limit int, per time.Duration) bool {
	if limit <= 0 {
		return true
	}
	count, err := l.counter.Hit(ctx, "account:"+key, per)
	if err != nil {
		l.logger.Warn("账户限流计数失败，本次放行", slog.Any("error", err))
		return true
	}
	return count <= limit
}
