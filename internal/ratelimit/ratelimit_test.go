package ratelimit_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/FeiBaiKin/lumo/internal/ratelimit"
	"github.com/FeiBaiKin/lumo/internal/testdb"
)

func TestCounterWindow(t *testing.T) {
	db := testdb.OpenCore(t)
	c := ratelimit.New(db.DB)
	ctx := context.Background()

	for want := 1; want <= 3; want++ {
		got, err := c.Hit(ctx, "k", time.Hour)
		if err != nil || got != want {
			t.Fatalf("第 %d 次计数得到 %d, %v", want, got, err)
		}
	}
	if n, _ := c.Count(ctx, "k"); n != 3 {
		t.Fatalf("Count = %d，应为 3", n)
	}
	if n, _ := c.Count(ctx, "never-hit"); n != 0 {
		t.Fatalf("没计过数的键 Count = %d，应为 0", n)
	}
	if err := c.Reset(ctx, "k"); err != nil {
		t.Fatal(err)
	}
	if n, _ := c.Count(ctx, "k"); n != 0 {
		t.Fatalf("Reset 之后 Count = %d，应为 0", n)
	}
}

func TestCounterExpiry(t *testing.T) {
	db := testdb.OpenCore(t)
	c := ratelimit.New(db.DB)
	ctx := context.Background()

	if _, err := c.Hit(ctx, "short", 200*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Hit(ctx, "short", 200*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if n, _ := c.Count(ctx, "short"); n != 0 {
		t.Fatalf("窗口过期后 Count = %d，应为 0", n)
	}
	if got, _ := c.Hit(ctx, "short", time.Hour); got != 1 {
		t.Fatalf("窗口过期后重新计数得到 %d，应从 1 开始", got)
	}

	if _, err := c.Hit(ctx, "stale", 100*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	removed, err := c.Sweep(ctx)
	if err != nil || removed != 1 {
		t.Fatalf("Sweep 删除 %d 行, %v；应只删掉过期的那一行", removed, err)
	}
}

// 多个实例同时给同一个键计数，结果必须是精确的总数：这正是计数放进数据库的理由。
func TestCounterConcurrentHits(t *testing.T) {
	db := testdb.OpenCore(t)
	c := ratelimit.New(db.DB)
	ctx := context.Background()

	const workers = 20
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.Hit(ctx, "shared", time.Hour); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if n, _ := c.Count(ctx, "shared"); n != workers {
		t.Fatalf("并发计数得到 %d，应为 %d", n, workers)
	}
}
