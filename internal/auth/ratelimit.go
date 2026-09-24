package auth

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/FeiBaiKin/lumo/internal/ratelimit"
)

// 登录限流参数。
//
// 只统计**失败**的尝试：正常用户连续成功登录不该被自己的成功次数挡住，
// 而攻击者的每一次猜测都会计入。
const (
	// loginFailuresPerAccount 是单个账号在窗口内允许的失败次数。
	loginFailuresPerAccount = 8
	// loginFailuresPerIP 是单个客户端 IP 在窗口内允许的失败次数。
	//
	// 比账号阈值宽：办公室或家庭常常共用一个出口 IP，卡太紧会连累无关的人。
	// 但它必须有，否则攻击者换着账号猜（撞库）就不会触发账号级限流。
	loginFailuresPerIP = 40
	// loginWindow 是失败计数的固定窗口，从第一次失败开始算。
	loginWindow = 15 * time.Minute
)

// ErrTooManyAttempts 表示登录尝试过于频繁。
var ErrTooManyAttempts = errTooManyAttempts{}

// errTooManyAttempts 带重试等待时长，供处理器回填 Retry-After。
type errTooManyAttempts struct{}

func (errTooManyAttempts) Error() string { return "登录尝试过于频繁，请稍后再试" }

// LoginLimiter 按账号与客户端 IP 两个维度限制登录失败次数。
//
// 计数存在数据库里（见 ratelimit 包），多实例共享同一份额度。
// 读写计数出错时放行并记 warn：数据库都不可用时登录本身也会失败，
// 为限流把整条登录链路打成 500 不划算。
type LoginLimiter struct {
	counter *ratelimit.Counter
	logger  *slog.Logger

	accountLimit int
	ipLimit      int
	window       time.Duration
}

// NewLoginLimiter 构造使用默认阈值的限流器。
func NewLoginLimiter(counter *ratelimit.Counter, logger *slog.Logger) *LoginLimiter {
	return &LoginLimiter{
		counter:      counter,
		logger:       logger,
		accountLimit: loginFailuresPerAccount,
		ipLimit:      loginFailuresPerIP,
		window:       loginWindow,
	}
}

// Allow 报告给定账号与 IP 当前是否还有尝试额度。
//
// account 是提交的登录名（可以是用户名或邮箱，也可能是根本不存在的账号）——
// 按提交值计数正是我们要的：撞库与枚举都会命中。
func (l *LoginLimiter) Allow(ctx context.Context, account, ip string) error {
	if l == nil {
		return nil
	}
	if l.exceeded(ctx, accountKey(account), l.accountLimit) {
		return ErrTooManyAttempts
	}
	if ip = strings.TrimSpace(ip); ip != "" && l.exceeded(ctx, ipKey(ip), l.ipLimit) {
		return ErrTooManyAttempts
	}
	return nil
}

// RecordFailure 记一次登录失败。只有失败才计入额度。
func (l *LoginLimiter) RecordFailure(ctx context.Context, account, ip string) {
	if l == nil {
		return
	}
	l.hit(ctx, accountKey(account))
	if ip = strings.TrimSpace(ip); ip != "" {
		l.hit(ctx, ipKey(ip))
	}
}

// ResetAccount 清空某账号的失败计数，供成功登录后调用。
//
// IP 维度的计数**不**清：一次成功登录不该让同一 IP 上的其他猜测重新开始。
func (l *LoginLimiter) ResetAccount(ctx context.Context, account string) {
	if l == nil {
		return
	}
	if err := l.counter.Reset(ctx, accountKey(account)); err != nil {
		l.warn(err)
	}
}

// exceeded 报告 key 的失败次数是否已达上限。
func (l *LoginLimiter) exceeded(ctx context.Context, key string, limit int) bool {
	count, err := l.counter.Count(ctx, key)
	if err != nil {
		l.warn(err)
		return false
	}
	return count >= limit
}

// hit 给 key 记一次失败。
func (l *LoginLimiter) hit(ctx context.Context, key string) {
	if _, err := l.counter.Hit(ctx, key, l.window); err != nil {
		l.warn(err)
	}
}

func (l *LoginLimiter) warn(err error) {
	if l.logger != nil {
		l.logger.Warn("登录限流计数失败，本次放行", slog.Any("error", err))
	}
}

// accountKey 与 ipKey 是计数键。账号按规范化后的登录名计，让大小写与首尾空白的变体共享同一个计数。
func accountKey(account string) string {
	return "login:account:" + strings.ToLower(strings.TrimSpace(account))
}

func ipKey(ip string) string { return "login:ip:" + ip }
