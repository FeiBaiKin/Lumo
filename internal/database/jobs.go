package database

import (
	"context"
	"fmt"
	"time"

	"github.com/uptrace/bun"
)

// ClaimRun 为名为 name 的周期任务认领本轮执行权，返回 true 表示本实例该执行。
//
// 多实例部署时每个实例的定时器都会触发。认领是一条原子的 UPSERT：距上次执行
// 不足 every 的认领落空，谁先把 last_run_at 推进到现在谁执行，其余实例跳过。
// 用它而不是 advisory lock，是因为各实例的定时器相位不同，锁只能挡住同一时刻的并发，
// 挡不住错开几分钟的重复执行。
//
// 认领后任务失败不回滚认领：下一轮再试，失败本身由调用方记日志。
func ClaimRun(ctx context.Context, db bun.IDB, name string, every time.Duration) (bool, error) {
	// 留一成余量：定时器的抖动会让「刚好一个周期」的认领偶尔落空，
	// 于是整整跳过一轮。
	gap := every - every/10
	var claimed []string
	err := db.NewRaw(`
INSERT INTO job_runs AS j (name, last_run_at) VALUES (?, now())
ON CONFLICT (name) DO UPDATE SET last_run_at = now()
WHERE j.last_run_at <= now() - ?::interval
RETURNING name`, name, fmt.Sprintf("%d milliseconds", gap.Milliseconds())).Scan(ctx, &claimed)
	if err != nil {
		return false, fmt.Errorf("认领周期任务 %s: %w", name, err)
	}
	return len(claimed) > 0, nil
}
