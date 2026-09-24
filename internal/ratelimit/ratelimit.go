// Package ratelimit 提供存在 PostgreSQL 里的固定窗口计数，多实例部署时共享同一份额度。
//
// 计数放在库里而不是进程内存：内存计数在 N 个实例下会把额度放大 N 倍，
// 登录限流也就被摊薄成了摆设。代价是每次计数多一次写库，
// 而需要限流的请求（登录、注册、找回密码、上传）本来就要读写数据库。
//
// 时间一律取数据库的 now()：各实例的时钟可能相差几秒，窗口边界得有唯一的裁判。
package ratelimit

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/uptrace/bun"
)

// Counter 是固定窗口计数器。窗口从某个键的第一次计数开始，到期后重新开窗。
type Counter struct {
	db bun.IDB
}

// New 构造计数器。只做装配、不访问数据库。
func New(db bun.IDB) *Counter { return &Counter{db: db} }

// interval 把时长写成 PostgreSQL 的 interval 字面量。
func interval(d time.Duration) string {
	return fmt.Sprintf("%d milliseconds", d.Milliseconds())
}

// Hit 给 key 记一次，返回当前窗口内的累计次数（含这一次）。
func (c *Counter) Hit(ctx context.Context, key string, per time.Duration) (int, error) {
	var count int
	err := c.db.NewRaw(`
INSERT INTO rate_limits AS r (key, count, expires_at) VALUES (?, 1, now() + ?::interval)
ON CONFLICT (key) DO UPDATE SET
    count      = CASE WHEN r.expires_at <= now() THEN 1 ELSE r.count + 1 END,
    expires_at = CASE WHEN r.expires_at <= now() THEN EXCLUDED.expires_at ELSE r.expires_at END
RETURNING count`, key, interval(per)).Scan(ctx, &count)
	if err != nil {
		return 0, fmt.Errorf("限流计数 %s: %w", key, err)
	}
	return count, nil
}

// Count 返回 key 当前窗口内的次数，不计数；窗口已过期或从未计数时为 0。
func (c *Counter) Count(ctx context.Context, key string) (int, error) {
	var count int
	err := c.db.NewRaw(`SELECT count FROM rate_limits WHERE key = ? AND expires_at > now()`, key).Scan(ctx, &count)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("读取限流计数 %s: %w", key, err)
	}
	return count, nil
}

// Reset 清掉 key 的计数。
func (c *Counter) Reset(ctx context.Context, key string) error {
	if _, err := c.db.NewRaw(`DELETE FROM rate_limits WHERE key = ?`, key).Exec(ctx); err != nil {
		return fmt.Errorf("清除限流计数 %s: %w", key, err)
	}
	return nil
}

// Sweep 删除已过期的计数，返回删除条数。正确性不依赖它（过期行在读写时都被当作不存在），
// 它只负责不让表随攻击无限增长。
func (c *Counter) Sweep(ctx context.Context) (int64, error) {
	res, err := c.db.NewRaw(`DELETE FROM rate_limits WHERE expires_at <= now()`).Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("清理过期限流计数: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}
