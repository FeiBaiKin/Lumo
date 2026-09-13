package account

import (
	"testing"
	"time"
)

// TestLimiterAllowsUpToLimit 验证额度内的请求全放行、超额即拒绝。
//
// 语义是「总次数」而不是 LoginLimiter 的「失败次数」：注册成功之后还要继续计数，
// 否则一个脚本每成功注册一次就把自己的额度刷回来。
func TestLimiterAllowsUpToLimit(t *testing.T) {
	t.Parallel()

	limiter := NewLimiter()
	const limit = 3
	const key = "register:ip:127.0.0.1"

	for i := 1; i <= limit; i++ {
		if !limiter.Allow(key, limit, time.Minute) {
			t.Fatalf("第 %d 次应被放行", i)
		}
	}
	if limiter.Allow(key, limit, time.Minute) {
		t.Error("第 4 次应被拒绝")
	}
	// 超额之后继续请求仍然是拒绝，不会过一会儿又放行一次。
	if limiter.Allow(key, limit, time.Minute) {
		t.Error("超额的请求应持续被拒绝")
	}
}

// TestLimiterKeysAreIndependent 验证不同键互不影响。
//
// 这是邮箱维度与 IP 维度能并存的前提：一封邮箱被限流不该连累同一个 IP 上的其他人。
func TestLimiterKeysAreIndependent(t *testing.T) {
	t.Parallel()

	limiter := NewLimiter()
	for i := 0; i < 3; i++ {
		limiter.Allow("forgot:email:a@example.com", 3, time.Hour)
	}
	if limiter.Allow("forgot:email:a@example.com", 3, time.Hour) {
		t.Fatal("a@example.com 应已超额")
	}
	if !limiter.Allow("forgot:email:b@example.com", 3, time.Hour) {
		t.Error("另一个邮箱不该被牵连")
	}
	if !limiter.Allow("forgot:ip:10.0.0.1", 10, time.Hour) {
		t.Error("另一个维度不该被牵连")
	}
}

// TestLimiterWindowExpires 验证窗口过期之后额度恢复。
//
// 时间由注入的时钟推进，不靠 sleep：靠真实时间等一小时是不可能的，
// 等几毫秒又会让这条用例在其他机器上偶发失败。
func TestLimiterWindowExpires(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	limiter := NewLimiter()
	limiter.SetClock(func() time.Time { return now })

	const key = "register:ip:127.0.0.1"
	for i := 0; i < 2; i++ {
		limiter.Allow(key, 2, 15*time.Minute)
	}
	if limiter.Allow(key, 2, 15*time.Minute) {
		t.Fatal("额度用尽后应被拒绝")
	}

	now = now.Add(16 * time.Minute)
	if !limiter.Allow(key, 2, 15*time.Minute) {
		t.Error("窗口过期后应重新放行")
	}
}

// TestLimiterReset 验证清零。
func TestLimiterReset(t *testing.T) {
	t.Parallel()

	limiter := NewLimiter()
	const key = "reset:ip:127.0.0.1"
	limiter.Allow(key, 1, time.Minute)
	if limiter.Allow(key, 1, time.Minute) {
		t.Fatal("额度应为 1")
	}
	limiter.Reset(key)
	if !limiter.Allow(key, 1, time.Minute) {
		t.Error("Reset 之后应重新放行")
	}
}

// TestLimiterSweepsExpiredBuckets 验证陈旧计数器会被清掉。
//
// 内存限流器最容易出的问题不是放过一次请求，而是键无限增长——
// 注册与找回密码的键里带邮箱与 IP，那是一份攻击者可以随意填充的字典。
func TestLimiterSweepsExpiredBuckets(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	limiter := NewLimiter()
	limiter.SetClock(func() time.Time { return now })

	for i := 0; i < 100; i++ {
		limiter.Allow("forgot:email:"+string(rune('a'+i%26))+".example.com", 3, time.Minute)
	}

	// 推过一个清理间隔，再打一次，触发 sweepLocked。
	now = now.Add(sweepInterval + time.Second)
	limiter.Allow("register:ip:127.0.0.1", 5, time.Minute)

	limiter.mu.Lock()
	remaining := len(limiter.buckets)
	limiter.mu.Unlock()

	if remaining != 1 {
		t.Errorf("清理后应只剩 1 个计数器，实际 %d", remaining)
	}
}
