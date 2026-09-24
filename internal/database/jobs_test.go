package database_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FeiBaiKin/lumo/internal/database"
	"github.com/FeiBaiKin/lumo/internal/testdb"
)

func TestClaimRunOncePerPeriod(t *testing.T) {
	db := testdb.OpenCore(t)
	ctx := context.Background()

	first, err := database.ClaimRun(ctx, db.DB, "job", time.Hour)
	if err != nil || !first {
		t.Fatalf("第一次认领 = %v, %v；应当认领成功", first, err)
	}
	again, err := database.ClaimRun(ctx, db.DB, "job", time.Hour)
	if err != nil || again {
		t.Fatalf("同一周期内再认领 = %v, %v；应当落空", again, err)
	}
	other, err := database.ClaimRun(ctx, db.DB, "other-job", time.Hour)
	if err != nil || !other {
		t.Fatalf("另一个任务的认领 = %v, %v；任务之间不该互相影响", other, err)
	}

	if _, execErr := db.ExecContext(ctx, "UPDATE job_runs SET last_run_at = now() - interval '2 hours' WHERE name = 'job'"); execErr != nil {
		t.Fatal(execErr)
	}
	later, err := database.ClaimRun(ctx, db.DB, "job", time.Hour)
	if err != nil || !later {
		t.Fatalf("过了一个周期再认领 = %v, %v；应当认领成功", later, err)
	}
}

// 多个实例的定时器同时触发，只能有一个认领成功。
func TestClaimRunConcurrent(t *testing.T) {
	db := testdb.OpenCore(t)
	ctx := context.Background()

	var claimed atomic.Int32
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := database.ClaimRun(ctx, db.DB, "race", time.Hour)
			if err != nil {
				t.Error(err)
			}
			if ok {
				claimed.Add(1)
			}
		}()
	}
	wg.Wait()
	if n := claimed.Load(); n != 1 {
		t.Fatalf("并发认领成功了 %d 次，应只有 1 次", n)
	}
}
