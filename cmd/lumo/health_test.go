package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/FeiBaiKin/lumo/internal/version"
)

// blockingPinger 模拟数据库黑洞：探测一直阻塞到 ctx 结束才返回。
type blockingPinger struct{ calls atomic.Int64 }

func (p *blockingPinger) PingContext(ctx context.Context) error {
	p.calls.Add(1)
	<-ctx.Done()
	return ctx.Err()
}

// errorPinger 模拟数据库明确报错；err 为 nil 表示健康。
type errorPinger struct{ err error }

func (p errorPinger) PingContext(context.Context) error { return p.err }

// readyRequest 用给定探测器和期限请求 /readyz。
func readyRequest(t *testing.T, p pinger, timeout time.Duration) *httptest.ResponseRecorder {
	t.Helper()
	root := chi.NewRouter()
	registerHealth(root, p, &version.Info{Version: "test"}, timeout)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/readyz", http.NoBody)
	rec := httptest.NewRecorder()
	root.ServeHTTP(rec, req)
	return rec
}

// TestReadyzTimesOutWith503 验证阻塞的数据库探测不会挂死 /readyz：
// 必须在独立期限内返回 503，而不是无限等待连接池/数据库。
func TestReadyzTimesOutWith503(t *testing.T) {
	t.Parallel()

	start := time.Now()
	rec := readyRequest(t, &blockingPinger{}, 50*time.Millisecond)
	elapsed := time.Since(start)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("状态码 = %d，期望 503", rec.Code)
	}
	if elapsed > time.Second {
		t.Fatalf("阻塞探测未在期限内返回，耗时 %v", elapsed)
	}
}

// TestReadyzReportsDatabaseError 验证数据库报错时同样返回 503。
func TestReadyzReportsDatabaseError(t *testing.T) {
	t.Parallel()

	rec := readyRequest(t, errorPinger{err: errors.New("连接失败")}, time.Second)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("状态码 = %d，期望 503", rec.Code)
	}
}

// TestReadyzHealthy 验证数据库可用时返回 200。
func TestReadyzHealthy(t *testing.T) {
	t.Parallel()

	rec := readyRequest(t, errorPinger{}, time.Second)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "ready") {
		t.Errorf("响应体缺少 ready 状态: %s", rec.Body.String())
	}
}

// TestHealthzDoesNotTouchDatabase 验证 /healthz 只报存活，不探测数据库。
func TestHealthzDoesNotTouchDatabase(t *testing.T) {
	t.Parallel()

	probe := &blockingPinger{}
	root := chi.NewRouter()
	registerHealth(root, probe, &version.Info{Version: "test"}, 50*time.Millisecond)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", http.NoBody)
	rec := httptest.NewRecorder()
	start := time.Now()
	root.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200", rec.Code)
	}
	if probe.calls.Load() != 0 {
		t.Error("/healthz 不应探测数据库")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("/healthz 不应被数据库拖慢，耗时 %v", elapsed)
	}
}
