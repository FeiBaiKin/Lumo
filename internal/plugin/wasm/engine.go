// Package wasm 在进程内运行插件的 WebAssembly 后端（wazero，纯 Go、无 CGO）。
//
// 一个插件就是一个 wasip1 reactor 模块。宿主与插件之间只有一条调用约定（ABI），
// 往返的消息一律是 JSON：
//
//	插件导出 lumo_alloc(size) -> ptr          宿主往插件内存里写请求前先让插件分配
//	插件导出 lumo_call(ptr, len) -> ptr<<32|len  处理一次请求，返回结果所在的位置
//	宿主导出 lumo.host_call(ptr, len) -> len   插件发起宿主调用，宿主暂存结果、只回长度
//	宿主导出 lumo.host_result(ptr)             插件分配好缓冲区后把暂存的结果取走
//
// 宿主调用拆成两步，是为了不在宿主函数里反过来调插件的导出函数：Go 编出来的 wasm
// 不保证能重入，两步交接则全程只有一个方向的调用。
//
// 插件拿不到文件系统、网络、环境变量与命令行参数；时钟与随机数照常提供（Go 运行时离不开）。
// 标准输出与标准错误接到宿主日志上。每次调用都有时限，超时即中断并丢弃该实例。
package wasm

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// ABIVersion 是调用约定的版本。插件在 describe 里报出自己编译时的版本，对不上就不加载。
const ABIVersion = 1

const (
	// MemoryLimitPages 是每个实例的线性内存上限（64 KiB 一页，共 32 MiB）。
	MemoryLimitPages = 512
	// MaxInstances 是每个插件同时存在的实例数上限。实例是单线程的，并发请求靠多实例分担。
	MaxInstances = 4
	// maxMessageSize 是单条请求或结果的字节上限。
	maxMessageSize = 8 << 20
	// initTimeout 是实例化（Go 运行时启动、init 里登记处理器）的时限。
	initTimeout = 5 * time.Second
	// DescribeTimeout 是 describe 调用的时限。
	DescribeTimeout = 2 * time.Second
)

// 插件模块必须导出的函数。
const (
	exportAlloc = "lumo_alloc"
	exportCall  = "lumo_call"
)

// HostModule 是宿主函数所在的导入模块名。
const HostModule = "lumo"

// ErrTimeout 表示调用超时被中断。
var ErrTimeout = errors.New("插件调用超时")

// ErrClosed 表示插件已卸下。
var ErrClosed = errors.New("插件已停止运行")

// ErrBusy 表示时限内等不到空闲实例：插件没坏，只是忙不过来。
var ErrBusy = errors.New("插件正忙，这次没排上")

// GuestError 是插件的处理函数自己返回的错误。插件没有崩，只是这次没做成。
type GuestError struct{ Message string }

func (e *GuestError) Error() string { return e.Message }

// IsCrash 判断一次失败是不是插件「坏了」：超时、陷入（panic、越界）或违反调用约定。
//
// 处理函数正常返回的错误不算——网络不通、参数不对这类是插件自己该处理的，
// 拿它们去累计「连续失败」会把一个只是暂时连不上外部服务的插件停掉。
func IsCrash(err error) bool {
	if err == nil {
		return false
	}
	var guest *GuestError
	return !errors.As(err, &guest) && !errors.Is(err, context.Canceled) && !errors.Is(err, ErrClosed) &&
		!errors.Is(err, ErrBusy)
}

// Request 是宿主发给插件的一次请求。
type Request struct {
	// Type 是请求类别：describe、action、filter、route、cron、shortcode、widget、slot。
	Type string `json:"type"`
	// Name 是处理函数的名字，如动作名、过滤器名、路由名。
	Name string `json:"name,omitempty"`
	// Payload 是请求数据，结构随类别而定。
	Payload any `json:"payload,omitempty"`
}

// Description 是插件在 describe 里报出的自我描述。
type Description struct {
	ABI int    `json:"abi"`
	SDK string `json:"sdk"`
	// Handlers 按类别列出插件登记了哪些处理函数，宿主据此核对清单里的声明。
	Handlers map[string][]string `json:"handlers"`
}

// HostFunc 处理插件发起的一次宿主调用。plugin 是调用方插件的标识。
type HostFunc func(ctx context.Context, plugin, op string, args json.RawMessage) (any, error)

// Options 是 Engine 的构造参数。
type Options struct {
	// CacheDir 是编译缓存目录；留空则不落盘，每次启动都重新编译。
	CacheDir string
	// Host 处理插件发起的宿主调用。
	Host   HostFunc
	Logger *slog.Logger
}

// Engine 持有一个 wazero 运行时，所有插件共用。
type Engine struct {
	runtime wazero.Runtime
	host    HostFunc
	logger  *slog.Logger
}

// NewEngine 构造运行时并装好 WASI 与宿主函数模块。
func NewEngine(ctx context.Context, opts Options) (*Engine, error) {
	cfg := wazero.NewRuntimeConfig().
		WithMemoryLimitPages(MemoryLimitPages).
		WithCloseOnContextDone(true)
	if opts.CacheDir != "" {
		cache, err := wazero.NewCompilationCacheWithDir(opts.CacheDir)
		if err != nil {
			return nil, fmt.Errorf("打开插件编译缓存: %w", err)
		}
		cfg = cfg.WithCompilationCache(cache)
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	e := &Engine{runtime: wazero.NewRuntimeWithConfig(ctx, cfg), host: opts.Host, logger: logger}

	if _, err := wasi_snapshot_preview1.Instantiate(ctx, e.runtime); err != nil {
		_ = e.runtime.Close(ctx)
		return nil, fmt.Errorf("装载 WASI: %w", err)
	}
	_, err := e.runtime.NewHostModuleBuilder(HostModule).
		NewFunctionBuilder().WithFunc(e.hostCall).Export("host_call").
		NewFunctionBuilder().WithFunc(e.hostResult).Export("host_result").
		Instantiate(ctx)
	if err != nil {
		_ = e.runtime.Close(ctx)
		return nil, fmt.Errorf("装载宿主函数: %w", err)
	}
	return e, nil
}

// Close 关闭运行时及其上的全部插件实例。
func (e *Engine) Close(ctx context.Context) error { return e.runtime.Close(ctx) }

// callKey 是调用状态在 context 里的键。
type callKey struct{}

// callState 是一次宿主调用往返的暂存区：host_call 写入结果，host_result 取走。
type callState struct {
	plugin  string
	pending []byte
}

// hostEnvelope 是宿主调用的结果外壳。
type hostEnvelope struct {
	OK     bool   `json:"ok"`
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

// hostRequest 是插件发来的宿主调用。
type hostRequest struct {
	Op   string          `json:"op"`
	Args json.RawMessage `json:"args"`
}

func (e *Engine) hostCall(ctx context.Context, m api.Module, ptr, size uint32) uint32 {
	state, _ := ctx.Value(callKey{}).(*callState)
	if state == nil {
		// 不在一次宿主发起的调用里（例如插件在 init 里就调宿主）：没有调用方可言。
		return 0
	}
	reply := hostEnvelope{}
	if size > maxMessageSize {
		reply.Error = "宿主调用的请求过大"
	} else if raw, ok := m.Memory().Read(ptr, size); !ok {
		reply.Error = "宿主调用的请求越界"
	} else {
		var req hostRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			reply.Error = "宿主调用的请求不是合法 JSON"
		} else if e.host == nil {
			reply.Error = "宿主没有开放任何调用"
		} else if result, err := e.host(ctx, state.plugin, req.Op, req.Args); err != nil {
			reply.Error = err.Error()
		} else {
			reply.OK, reply.Result = true, result
		}
	}
	data, err := json.Marshal(reply)
	if err == nil && len(data) > maxMessageSize {
		data, err = json.Marshal(hostEnvelope{Error: "宿主调用的结果过大"})
	}
	if err != nil {
		data = []byte(`{"ok":false,"error":"宿主调用的结果无法编码"}`)
	}
	state.pending = data
	return uint32(len(data))
}

func (e *Engine) hostResult(ctx context.Context, m api.Module, ptr uint32) {
	state, _ := ctx.Value(callKey{}).(*callState)
	if state == nil {
		return
	}
	m.Memory().Write(ptr, state.pending)
	state.pending = nil
}

// Plugin 是一个编译好的插件，持有它的实例池。
type Plugin struct {
	name     string
	engine   *Engine
	compiled wazero.CompiledModule
	desc     Description

	// idle 是空闲实例；slots 限制实例总数，创建前先占一个位。
	idle   chan api.Module
	slots  chan struct{}
	seq    atomic.Int64
	closed atomic.Bool
}

// Load 编译插件并做一次 describe，返回可调用的插件。
func (e *Engine) Load(ctx context.Context, name string, binary []byte) (*Plugin, error) {
	compiled, err := e.runtime.CompileModule(ctx, binary)
	if err != nil {
		return nil, fmt.Errorf("编译插件: %w", err)
	}
	exports := compiled.ExportedFunctions()
	for _, fn := range []string{exportAlloc, exportCall} {
		if _, ok := exports[fn]; !ok {
			_ = compiled.Close(ctx)
			return nil, fmt.Errorf("插件没有导出 %s：它不是按 Lumo 插件接口编译的", fn)
		}
	}
	for _, def := range compiled.ImportedFunctions() {
		module, fn, _ := def.Import()
		if module != HostModule && module != wasi_snapshot_preview1.ModuleName {
			_ = compiled.Close(ctx)
			return nil, fmt.Errorf("插件导入了宿主不提供的函数 %s.%s", module, fn)
		}
	}

	p := &Plugin{
		name:     name,
		engine:   e,
		compiled: compiled,
		idle:     make(chan api.Module, MaxInstances),
		slots:    make(chan struct{}, MaxInstances),
	}
	raw, err := p.Call(ctx, Request{Type: "describe"}, DescribeTimeout)
	if err != nil {
		_ = p.Close(ctx)
		return nil, fmt.Errorf("读取插件描述: %w", err)
	}
	if err := json.Unmarshal(raw, &p.desc); err != nil {
		_ = p.Close(ctx)
		return nil, fmt.Errorf("插件描述不是合法 JSON: %w", err)
	}
	if p.desc.ABI != ABIVersion {
		_ = p.Close(ctx)
		return nil, fmt.Errorf("插件按接口版本 %d 编译，宿主是 %d：请用对应版本的 SDK 重新编译", p.desc.ABI, ABIVersion)
	}
	return p, nil
}

// Name 返回插件标识。
func (p *Plugin) Name() string { return p.name }

// Description 返回插件的自我描述。
func (p *Plugin) Description() Description { return p.desc }

// callEnvelope 是插件处理结果的外壳。
type callEnvelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result"`
	Error  string          `json:"error"`
}

// Call 向插件发一次请求，返回它的结果。
func (p *Plugin) Call(ctx context.Context, req Request, timeout time.Duration) (json.RawMessage, error) {
	if p.closed.Load() {
		return nil, ErrClosed
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("编码请求: %w", err)
	}
	if len(body) > maxMessageSize {
		return nil, errors.New("发给插件的请求过大")
	}

	callCtx, cancel := context.WithTimeout(context.WithValue(ctx, callKey{}, &callState{plugin: p.name}), timeout)
	defer cancel()
	mod, err := p.acquire(callCtx)
	if err != nil {
		// 排队排到超时不算崩溃：公开接口被刷时实例全忙，不能因此把插件停掉
		if errors.Is(callCtx.Err(), context.DeadlineExceeded) {
			return nil, ErrBusy
		}
		return nil, err
	}
	out, err := invoke(callCtx, mod, body)
	if err != nil || mod.IsClosed() {
		p.discard(mod)
	} else {
		p.release(mod)
	}
	if err != nil {
		if errors.Is(callCtx.Err(), context.DeadlineExceeded) {
			return nil, ErrTimeout
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			return nil, context.Canceled
		}
		return nil, fmt.Errorf("插件运行出错: %w", err)
	}

	var envelope callEnvelope
	if err := json.Unmarshal(out, &envelope); err != nil {
		return nil, fmt.Errorf("插件返回的结果不是合法 JSON: %w", err)
	}
	if !envelope.OK {
		return nil, &GuestError{Message: envelope.Error}
	}
	return envelope.Result, nil
}

// invoke 把请求写进实例内存并调用 lumo_call，返回结果的副本。
func invoke(ctx context.Context, mod api.Module, body []byte) ([]byte, error) {
	res, err := mod.ExportedFunction(exportAlloc).Call(ctx, uint64(len(body)))
	if err != nil {
		return nil, err
	}
	ptr := uint32(res[0])
	if !mod.Memory().Write(ptr, body) {
		return nil, errors.New("插件分配的缓冲区越界")
	}
	res, err = mod.ExportedFunction(exportCall).Call(ctx, uint64(ptr), uint64(len(body)))
	if err != nil {
		return nil, err
	}
	outPtr, outLen := uint32(res[0]>>32), uint32(res[0])
	if outLen > maxMessageSize {
		return nil, errors.New("插件返回的结果过大")
	}
	view, ok := mod.Memory().Read(outPtr, outLen)
	if !ok {
		return nil, errors.New("插件返回的结果越界")
	}
	// Read 返回的是实例内存的视图，实例回到池里之后会被下一次调用覆盖。
	return append([]byte(nil), view...), nil
}

// acquire 取一个空闲实例；没有空闲且未到上限时新建一个，到了上限就等。
func (p *Plugin) acquire(ctx context.Context) (api.Module, error) {
	select {
	case mod := <-p.idle:
		return mod, nil
	default:
	}
	select {
	case mod := <-p.idle:
		return mod, nil
	case p.slots <- struct{}{}:
		mod, err := p.instantiate(ctx)
		if err != nil {
			<-p.slots
			return nil, err
		}
		return mod, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// release 把实例放回池里；插件已停止时直接销毁。
func (p *Plugin) release(mod api.Module) {
	if p.closed.Load() {
		p.discard(mod)
		return
	}
	p.idle <- mod
}

// discard 销毁实例并让出它占的位。
func (p *Plugin) discard(mod api.Module) {
	_ = mod.Close(context.Background())
	<-p.slots
}

// instantiate 新建一个实例。
func (p *Plugin) instantiate(ctx context.Context) (api.Module, error) {
	cfg := wazero.NewModuleConfig().
		WithName(fmt.Sprintf("%s#%d", p.name, p.seq.Add(1))).
		WithStartFunctions("_initialize").
		WithStdout(newLogWriter(p.engine.logger, p.name, slog.LevelInfo)).
		WithStderr(newLogWriter(p.engine.logger, p.name, slog.LevelWarn)).
		WithSysWalltime().
		WithSysNanotime().
		WithSysNanosleep().
		WithRandSource(rand.Reader)
	initCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), initTimeout)
	defer cancel()
	mod, err := p.engine.runtime.InstantiateModule(initCtx, p.compiled, cfg)
	if err != nil {
		return nil, fmt.Errorf("启动插件实例: %w", err)
	}
	return mod, nil
}

// Close 停止插件：销毁空闲实例、释放编译结果。正在进行的调用结束后各自销毁实例。
func (p *Plugin) Close(ctx context.Context) error {
	if !p.closed.CompareAndSwap(false, true) {
		return nil
	}
	for {
		select {
		case mod := <-p.idle:
			p.discard(mod)
		default:
			return p.compiled.Close(ctx)
		}
	}
}
