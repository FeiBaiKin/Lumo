package auth

import (
	"strings"
	"sync"
	"time"
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
	// loginWindow 是失败计数的滑动窗口。
	loginWindow = 15 * time.Minute
	// loginLimiterSweepInterval 是清理过期计数的间隔，避免 map 随攻击无限增长。
	loginLimiterSweepInterval = 5 * time.Minute
)

// ErrTooManyAttempts 表示登录尝试过于频繁。
var ErrTooManyAttempts = errTooManyAttempts{}

// errTooManyAttempts 带重试等待时长，供处理器回填 Retry-After。
type errTooManyAttempts struct{}

func (errTooManyAttempts) Error() string { return "登录尝试过于频繁，请稍后再试" }

// LoginLimiter 按账号与客户端 IP 两个维度限制登录失败次数。
//
// 内存实现，单进程有效：多实例部署时每个实例各自计数，防护会被摊薄
// （限流阈值随实例数放大）。真正的修复要在网关或共享存储上做，
// 这里先把单实例的洞堵住，并在文档里写明这一限制。
type LoginLimiter struct {
	mu      sync.Mutex
	entries map[string]*failWindow

	accountLimit int
	ipLimit      int
	window       time.Duration

	// lastSweep 是上次回收过期条目的时间。
	lastSweep time.Time
	// now 便于测试注入时间。
	now func() time.Time
}

// failWindow 是一个计数窗口。
type failWindow struct {
	count int
	// resetAt 是窗口的结束时间；到点后计数从零开始。
	resetAt time.Time
}

// NewLoginLimiter 构造使用默认阈值的限流器。
func NewLoginLimiter() *LoginLimiter {
	return NewLoginLimiterWith(loginFailuresPerAccount, loginFailuresPerIP, loginWindow)
}

// NewLoginLimiterWith 用指定阈值构造限流器。
//
// 阈值可调是为了让测试能用小额度跑完整链路；运维若要放宽或收紧，
// 也应该走这里而不是改常量（常量只是默认值）。
func NewLoginLimiterWith(accountLimit, ipLimit int, window time.Duration) *LoginLimiter {
	if accountLimit < 1 {
		accountLimit = 1
	}
	if ipLimit < 1 {
		ipLimit = 1
	}
	if window <= 0 {
		window = loginWindow
	}
	return &LoginLimiter{
		entries:      make(map[string]*failWindow),
		accountLimit: accountLimit,
		ipLimit:      ipLimit,
		window:       window,
		now:          time.Now,
	}
}

// Allow 报告给定账号与 IP 当前是否还有尝试额度。
//
// account 是提交的登录名（可以是用户名或邮箱，也可能是根本不存在的账号）——
// 按提交值计数正是我们要的：撞库与枚举都会命中。
func (l *LoginLimiter) Allow(account, ip string) error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.sweepLocked(now)

	if entry, ok := l.liveLocked("login:"+normalizeAccount(account), now); ok && entry.count >= l.accountLimit {
		return ErrTooManyAttempts
	}
	if ip = strings.TrimSpace(ip); ip != "" {
		if entry, ok := l.liveLocked("ip:"+ip, now); ok && entry.count >= l.ipLimit {
			return ErrTooManyAttempts
		}
	}
	return nil
}

// RecordFailure 记一次登录失败。只有失败才计入额度。
func (l *LoginLimiter) RecordFailure(account, ip string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	// 先清理：一次攻击会在短时间内塞进大量不同 key。
	l.sweepLocked(now)

	l.bumpLocked("login:"+normalizeAccount(account), now)
	if ip = strings.TrimSpace(ip); ip != "" {
		l.bumpLocked("ip:"+ip, now)
	}
}

// ResetAccount 清空某账号的失败计数，供成功登录与改密码后调用。
//
// IP 维度的计数**不**清：一次成功登录不该让同一 IP 上的其他猜测重新开始。
func (l *LoginLimiter) ResetAccount(account string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	delete(l.entries, "login:"+normalizeAccount(account))
}

// bumpLocked 递增计数；窗口已过期时重新开窗。
func (l *LoginLimiter) bumpLocked(key string, now time.Time) {
	entry, ok := l.entries[key]
	if !ok || !now.Before(entry.resetAt) {
		l.entries[key] = &failWindow{count: 1, resetAt: now.Add(l.window)}
		return
	}
	entry.count++
}

// liveLocked 取出未过期的计数。
func (l *LoginLimiter) liveLocked(key string, now time.Time) (*failWindow, bool) {
	entry, ok := l.entries[key]
	if !ok || !now.Before(entry.resetAt) {
		return nil, false
	}
	return entry, true
}

// sweepLocked 回收过期条目，避免 map 随攻击无限增长。
//
// 每 sweepInterval 扫一次即可：两次扫描之间的正确性由 liveLocked 的
// 过期判断保证，扫描只负责回收内存。
func (l *LoginLimiter) sweepLocked(now time.Time) {
	if now.Sub(l.lastSweep) < loginLimiterSweepInterval {
		return
	}
	for key, entry := range l.entries {
		if !now.Before(entry.resetAt) {
			delete(l.entries, key)
		}
	}
	l.lastSweep = now
}

// normalizeAccount 规范化登录名，让大小写与首尾空白的变体共享同一个计数。
func normalizeAccount(account string) string {
	return strings.ToLower(strings.TrimSpace(account))
}
