//go:build !wasip1

package lumo

// 非 wasip1 平台上没有宿主：让插件代码能在普通平台上编译、跑单元测试与 go vet，
// 一切宿主调用都返回 ErrNoHost。

func hostCall([]byte) []byte { return nil }

// Dispatch 在非 wasip1 平台上把一次请求交给登记的处理函数，供插件作者写单元测试：
//
//	out := lumo.Dispatch(`{"type":"action","name":"comment.created","payload":{...}}`)
func Dispatch(req string) string { return string(dispatch([]byte(req))) }
