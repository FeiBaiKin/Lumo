package account

import (
	"sync"
	"time"
)

// sweepInterval 是陈旧计数器的最短清理间隔。
//
// 每次 Allow 都全量扫一遍 map 是浪费；按时间间隔扫，代价与请求量脱钩。
const sweepInterval = 10 * time.Minute

// Limiter 是窗口计数限流器。
//
// 不复用 auth.LoginLimiter：那个的语义是「**失败**计数，成功即清零」，
// 而这里要的是「总次数」——注册成功之后当然还要继续计数，
// 否则一个脚本每成功注册一次就把自己的额度刷回来。套用会让注释和代码互相矛盾。
//
// **内存实现、单进程有效**（与 LoginLimiter 同样的限制）：多实例部署时
// 每个实例各记一份，实际额度被摊薄成 N 倍。真正的修复要在网关或 Redis 上做，
// 这里挡的是单机脚本，不是分布式压测。
type Limiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	// lastSweep 记录上次清理时刻，避免每次 Allow 都遍历整张表。
	lastSweep time.Time
	// now 可被测试替换，用于把时间拨到窗口之后。
	now func() time.Time
}

// bucket 是一个键在某个时间窗内的计数。
type bucket struct {
	count   int
	expires time.Time
}

// NewLimiter 构造限流器。
func NewLimiter() *Limiter {
	return &Limiter{
		buckets: map[string]*bucket{},
		now:     time.Now,
	}
}

// SetClock 替换时间源，仅供测试。
func (l *Limiter) SetClock(now func() time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.now = now
}

// Allow 记一次计数并报告是否仍在额度内。
//
// 窗口从**第一次**命中开始算，之后窗口内的每次调用都累加到同一个计数上；
// 窗口过期则重新开一个。这是最朴素的固定窗口，够用且没有值得解释的参数。
func (l *Limiter) Allow(key string, limit int, per time.Duration) bool {
	if limit <= 0 {
		return true
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.sweepLocked(now)

	current, ok := l.buckets[key]
	if !ok || !now.Before(current.expires) {
		l.buckets[key] = &bucket{count: 1, expires: now.Add(per)}
		return true
	}
	current.count++
	return current.count <= limit
}

// Reset 清掉某个键的计数，供测试与「用户做对了某件事」的场景使用。
func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.buckets, key)
}

// sweepLocked 按间隔清理已过期的计数器。调用方须已持锁。
func (l *Limiter) sweepLocked(now time.Time) {
	if now.Sub(l.lastSweep) < sweepInterval {
		return
	}
	l.lastSweep = now
	for key, b := range l.buckets {
		if !now.Before(b.expires) {
			delete(l.buckets, key)
		}
	}
}
