// Package lumo 是 Lumo 插件的 Go SDK。
//
// 插件用标准 Go 工具链编译成 wasip1 reactor 模块：
//
//	GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o plugin.wasm .
//
// 处理函数在 init 里登记，宿主按插件清单（plugin.yaml）里的声明来调用它们；
// 登记了却没声明、或声明了却没登记，插件都会被拒绝加载。
//
//	func init() {
//		lumo.OnAction("comment.created", func(ctx *lumo.Context, e *lumo.Event) error {
//			lumo.Info("收到新评论")
//			return nil
//		})
//	}
//
//	func main() {}
//
// 宿主调用（日志、设置、存储……）只能在处理函数里使用：插件没有自己的线程，
// 也拿不到网络与文件系统，一切外部能力都经宿主提供，并受插件清单里声明的权限约束。
package lumo

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

// ABIVersion 是本 SDK 实现的宿主调用约定版本，必须与宿主一致。
const ABIVersion = 1

// Version 是 SDK 版本，跟着 Lumo 的版本走，发版时改这里（宿主把它显示在插件列表里）。
// 它只用来交代插件是拿哪一版 SDK 编的，不参与兼容性判定——那是 ABIVersion 的事。
const Version = "0.2.0"

// Context 是一次调用的上下文。
type Context struct {
	// Kind 是这次调用的类别，如 action、filter。
	Kind string
	// Name 是被调用的处理函数名。
	Name string
}

// handler 是登记在册的处理函数：收到原始负载，返回可编码成 JSON 的结果。
type handler func(ctx *Context, payload json.RawMessage) (any, error)

// handlers 按类别、名字登记处理函数。只在 init 里写，之后只读。
var handlers = map[string]map[string]handler{}

// register 登记一个处理函数。同一类别下重名是编程错误，直接 panic，让插件在加载时就失败。
func register(kind, name string, h handler) {
	if name == "" {
		panic(fmt.Sprintf("lumo: %s 处理函数的名字不能为空", kind))
	}
	byName := handlers[kind]
	if byName == nil {
		byName = map[string]handler{}
		handlers[kind] = byName
	}
	if _, dup := byName[name]; dup {
		panic(fmt.Sprintf("lumo: %s %q 登记了两次", kind, name))
	}
	byName[name] = h
}

// Event 是一次动作通知。
type Event struct {
	// Name 是动作名，如 comment.created。
	Name string
	// Data 是动作的数据，结构随动作而定。
	Data json.RawMessage
}

// Decode 把动作数据解到 v 里。
func (e *Event) Decode(v any) error { return json.Unmarshal(e.Data, v) }

// OnAction 登记一个动作处理函数：宿主在对应的事情发生之后异步通知插件。
//
// 动作不能改变已经发生的事；要改数据请用过滤器。
func OnAction(name string, fn func(ctx *Context, e *Event) error) {
	register("action", name, func(ctx *Context, payload json.RawMessage) (any, error) {
		return nil, fn(ctx, &Event{Name: name, Data: payload})
	})
}

// request 是宿主发来的一次请求。
type request struct {
	Type    string          `json:"type"`
	Name    string          `json:"name"`
	Payload json.RawMessage `json:"payload"`
}

// reply 是插件对一次请求的答复。
type reply struct {
	OK     bool   `json:"ok"`
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

// description 是 describe 请求的答复：宿主据此核对插件清单。
type description struct {
	ABI      int                 `json:"abi"`
	SDK      string              `json:"sdk"`
	Handlers map[string][]string `json:"handlers"`
}

// dispatch 处理宿主的一次请求。处理函数 panic 时不拦：让实例崩掉，宿主会丢弃它并记一次失败。
func dispatch(raw []byte) []byte {
	var req request
	if err := json.Unmarshal(raw, &req); err != nil {
		return encode(reply{Error: "请求不是合法 JSON"})
	}
	if req.Type == "describe" {
		return encode(reply{OK: true, Result: describe()})
	}
	h := handlers[req.Type][req.Name]
	if h == nil {
		return encode(reply{Error: fmt.Sprintf("插件没有登记 %s %q 的处理函数", req.Type, req.Name)})
	}
	result, err := h(&Context{Kind: req.Type, Name: req.Name}, req.Payload)
	if err != nil {
		return encode(reply{Error: err.Error()})
	}
	return encode(reply{OK: true, Result: result})
}

func describe() description {
	out := description{ABI: ABIVersion, SDK: Version, Handlers: map[string][]string{}}
	for kind, byName := range handlers {
		names := make([]string, 0, len(byName))
		for name := range byName {
			names = append(names, name)
		}
		sort.Strings(names)
		out.Handlers[kind] = names
	}
	return out
}

func encode(r reply) []byte {
	data, err := json.Marshal(r)
	if err != nil {
		data, _ = json.Marshal(reply{Error: "插件的结果无法编码成 JSON：" + err.Error()})
	}
	return data
}

// hostReply 是宿主调用的答复。
type hostReply struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result"`
	Error  string          `json:"error"`
}

// ErrNoHost 表示在宿主发起的调用之外使用了宿主能力（例如在 init 里）。
var ErrNoHost = errors.New("lumo: 宿主调用只能在处理函数里使用")

// call 发起一次宿主调用，把结果解到 out 里（out 为 nil 时丢弃结果）。
func call(op string, args, out any) error {
	body, err := json.Marshal(struct {
		Op   string `json:"op"`
		Args any    `json:"args,omitempty"`
	}{op, args})
	if err != nil {
		return fmt.Errorf("lumo: 编码 %s 的参数: %w", op, err)
	}
	raw := hostCall(body)
	if raw == nil {
		return ErrNoHost
	}
	var r hostReply
	if err := json.Unmarshal(raw, &r); err != nil {
		return fmt.Errorf("lumo: 宿主答复不是合法 JSON: %w", err)
	}
	if !r.OK {
		return errors.New(r.Error)
	}
	if out == nil || len(r.Result) == 0 {
		return nil
	}
	return json.Unmarshal(r.Result, out)
}
