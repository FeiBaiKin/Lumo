package auth

import (
	"errors"
	"testing"
	"time"
)

// TestLoginLimiterWindow 验证限流器的窗口行为。
//
// 放在包内而不是外部测试包：要用可控时钟替换 now，不想为此在生产代码上
// 开一个只服务于测试的导出接口。
func TestLoginLimiterWindow(t *testing.T) {
	t.Parallel()

	limiter := NewLoginLimiterWith(3, 5, time.Minute)
	now := time.Now()
	limiter.now = func() time.Time { return now }

	// 账号维度：3 次失败后第 4 次被拒。
	for i := range 3 {
		if err := limiter.Allow("alice", "203.0.113.7"); err != nil {
			t.Fatalf("第 %d 次尝试不应被拒: %v", i+1, err)
		}
		limiter.RecordFailure("alice", "203.0.113.7")
	}
	if err := limiter.Allow("alice", "203.0.113.7"); !errors.Is(err, ErrTooManyAttempts) {
		t.Fatalf("额度用尽后应返回 ErrTooManyAttempts，实际 %v", err)
	}
	// 换 IP 也躲不过账号维度的计数。
	if err := limiter.Allow("alice", "198.51.100.9"); !errors.Is(err, ErrTooManyAttempts) {
		t.Error("账号维度应独立于 IP 计数")
	}
	// 大小写与空白变体共享同一份计数。
	if err := limiter.Allow("  ALICE ", "198.51.100.9"); !errors.Is(err, ErrTooManyAttempts) {
		t.Error("登录名变体不应绕过限流")
	}

	// 成功登录清空账号计数，IP 计数保留。
	limiter.ResetAccount("alice")
	if err := limiter.Allow("alice", "203.0.113.7"); err != nil {
		t.Errorf("成功登录后应恢复额度，实际 %v", err)
	}

	// IP 维度：40 次以外的默认值在这里被压到 5，验证它确实独立生效。
	for range 5 {
		limiter.RecordFailure("carol", "198.51.100.9")
	}
	if err := limiter.Allow("dave", "198.51.100.9"); !errors.Is(err, ErrTooManyAttempts) {
		t.Error("同一 IP 的失败总数应独立计数")
	}

	// 窗口过期后计数自动归零。
	now = now.Add(time.Minute + time.Second)
	if err := limiter.Allow("dave", "198.51.100.9"); err != nil {
		t.Errorf("窗口过期后应恢复额度，实际 %v", err)
	}
}

// TestLoginLimiterSweep 验证过期条目会被回收，map 不会随攻击无限增长。
func TestLoginLimiterSweep(t *testing.T) {
	t.Parallel()

	limiter := NewLoginLimiterWith(2, 2, time.Minute)
	now := time.Now()
	limiter.now = func() time.Time { return now }

	for i := range 50 {
		limiter.RecordFailure("user"+string(rune('a'+i%26))+string(rune('0'+i/26)), "203.0.113.7")
	}
	if got := len(limiter.entries); got == 0 {
		t.Fatal("失败记录应被记下")
	}

	// 越过扫描间隔后，过期条目应被清掉。
	now = now.Add(loginLimiterSweepInterval + time.Second)
	_ = limiter.Allow("nobody", "203.0.113.8")
	if got := len(limiter.entries); got > 1 {
		t.Errorf("过期条目应被回收，剩余 %d 条", got)
	}
}

// TestLoginLimiterNilSafe 验证 nil 限流器不会 panic（Service 未装配限流器时的兜底）。
func TestLoginLimiterNilSafe(t *testing.T) {
	t.Parallel()

	var limiter *LoginLimiter
	if err := limiter.Allow("alice", "203.0.113.7"); err != nil {
		t.Errorf("nil 限流器应放行，实际 %v", err)
	}
	limiter.RecordFailure("alice", "203.0.113.7")
	limiter.ResetAccount("alice")
}
