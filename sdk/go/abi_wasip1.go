//go:build wasip1

package lumo

import "unsafe"

// 宿主调用约定的插件一侧，见宿主的 internal/plugin/wasm。

//go:wasmimport lumo host_call
func hostCallRaw(ptr, size uint32) uint32

//go:wasmimport lumo host_result
func hostResultRaw(ptr uint32)

// inputs 持有宿主写入请求用的缓冲区，直到 lumo_call 取走；
// output 持有上一次的结果，直到下一次调用覆盖——宿主在这之间把它读走。
var (
	inputs = map[uint32][]byte{}
	output []byte
)

func addr(b []byte) uint32 { return uint32(uintptr(unsafe.Pointer(unsafe.SliceData(b)))) }

//go:wasmexport lumo_alloc
func lumoAlloc(size uint32) uint32 {
	// 零长度的切片可能共用同一个地址，至少分配一个字节才能用地址区分
	buf := make([]byte, max(size, 1))
	p := addr(buf)
	inputs[p] = buf
	return p
}

//go:wasmexport lumo_call
func lumoCall(ptr, size uint32) uint64 {
	in := inputs[ptr]
	delete(inputs, ptr)
	if uint32(len(in)) < size {
		output = encode(reply{Error: "请求缓冲区与长度不符"})
	} else {
		output = dispatch(in[:size])
	}
	return uint64(addr(output))<<32 | uint64(len(output))
}

func hostCall(req []byte) []byte {
	n := hostCallRaw(addr(req), uint32(len(req)))
	if n == 0 {
		return nil
	}
	buf := make([]byte, n)
	hostResultRaw(addr(buf))
	return buf
}
