package wasm_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/FeiBaiKin/lumo/internal/plugin/wasm"
	"github.com/FeiBaiKin/lumo/internal/plugin/wasm/wasmtest"
)

// hostLog 记下插件发起的宿主调用。
type hostLog struct {
	mu  sync.Mutex
	ops []string
}

func (h *hostLog) call(_ context.Context, plugin, op string, _ json.RawMessage) (any, error) {
	h.mu.Lock()
	h.ops = append(h.ops, plugin+":"+op)
	h.mu.Unlock()
	if op == "settings.get" {
		return map[string]any{"answer": 42}, nil
	}
	return nil, nil
}

func (h *hostLog) seen(entry string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, op := range h.ops {
		if op == entry {
			return true
		}
	}
	return false
}

func load(t *testing.T) (*wasm.Plugin, *hostLog) {
	t.Helper()
	binary := wasmtest.Guest(t)
	ctx := context.Background()
	host := &hostLog{}
	engine, err := wasm.NewEngine(ctx, wasm.Options{CacheDir: t.TempDir(), Host: host.call})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Close(ctx) })
	p, err := engine.Load(ctx, "guest", binary)
	if err != nil {
		t.Fatalf("加载测试插件：%v", err)
	}
	return p, host
}

func action(name string) wasm.Request {
	return wasm.Request{Type: "action", Name: name, Payload: map[string]string{"hello": "世界"}}
}

func TestLoadDescribesHandlers(t *testing.T) {
	p, _ := load(t)
	desc := p.Description()
	if desc.ABI != wasm.ABIVersion || desc.SDK == "" {
		t.Fatalf("describe 不对：%+v", desc)
	}
	got := desc.Handlers["action"]
	want := []string{"test.echo", "test.fail", "test.panic", "test.settings", "test.spin"}
	if len(got) != len(want) {
		t.Fatalf("登记的动作 = %v，应为 %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("登记的动作 = %v，应为 %v", got, want)
		}
	}
}

func TestCallRoundTripsThroughHost(t *testing.T) {
	p, host := load(t)
	ctx := context.Background()
	if _, err := p.Call(ctx, action("test.echo"), time.Second); err != nil {
		t.Fatalf("正常动作失败：%v", err)
	}
	if !host.seen("guest:log") {
		t.Fatal("插件的日志没有经宿主调用送出来")
	}
	if _, err := p.Call(ctx, action("test.settings"), time.Second); err != nil {
		t.Fatalf("宿主调用的结果没回到插件里：%v", err)
	}
}

// 处理函数自己返回的错误不算崩溃；超时与 panic 算，而且实例坏了之后下一次调用照常。
func TestFailuresAreClassifiedAndRecoverable(t *testing.T) {
	p, _ := load(t)
	ctx := context.Background()

	_, err := p.Call(ctx, action("test.fail"), time.Second)
	var guest *wasm.GuestError
	if !errors.As(err, &guest) || guest.Message != "故意失败" || wasm.IsCrash(err) {
		t.Fatalf("处理函数的错误应原样返回且不算崩溃，得到 %v", err)
	}

	start := time.Now()
	_, err = p.Call(ctx, action("test.spin"), 100*time.Millisecond)
	if !errors.Is(err, wasm.ErrTimeout) || !wasm.IsCrash(err) {
		t.Fatalf("死循环应被超时中断并算作崩溃，得到 %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("超时中断用了 %s，太久", elapsed)
	}

	if _, err = p.Call(ctx, action("test.panic"), time.Second); !wasm.IsCrash(err) {
		t.Fatalf("panic 应算作崩溃，得到 %v", err)
	}

	if _, err := p.Call(ctx, action("test.echo"), time.Second); err != nil {
		t.Fatalf("实例坏掉之后应能换一个继续用：%v", err)
	}
	if _, err := p.Call(ctx, action("missing"), time.Second); !errors.As(err, &guest) {
		t.Fatalf("没登记的处理函数应返回插件错误，得到 %v", err)
	}
}

func TestConcurrentCallsShareThePool(t *testing.T) {
	p, _ := load(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := p.Call(ctx, action("test.echo"), 5*time.Second); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("并发调用失败：%v", err)
	}
}

func TestClosedPluginRefusesCalls(t *testing.T) {
	p, _ := load(t)
	ctx := context.Background()
	if err := p.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Call(ctx, action("test.echo"), time.Second); !errors.Is(err, wasm.ErrClosed) || wasm.IsCrash(err) {
		t.Fatalf("停止后的插件应返回 ErrClosed 且不算崩溃，得到 %v", err)
	}
}

// 不是按插件接口编译的模块（这里是一个合法但空的 wasm）不能加载。
func TestRejectsModuleWithoutABI(t *testing.T) {
	ctx := context.Background()
	engine, err := wasm.NewEngine(ctx, wasm.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = engine.Close(ctx) }()
	empty := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	if _, err := engine.Load(ctx, "empty", empty); err == nil {
		t.Fatal("没有导出 lumo_alloc / lumo_call 的模块应被拒绝")
	}
	if _, err := engine.Load(ctx, "junk", []byte("not wasm")); err == nil {
		t.Fatal("不是 wasm 的内容应被拒绝")
	}
}
