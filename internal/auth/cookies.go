package auth

import (
	"net/http"
)

// headerOnlyWriter 是只收集响应头的 ResponseWriter，用于把 Cookie 渲染成 Set-Cookie 头值。
//
// http.SetCookie 只调用 w.Header().Add，因此这里不需要真正的响应通道。
type headerOnlyWriter struct {
	header http.Header
}

func (w headerOnlyWriter) Header() http.Header         { return w.header }
func (w headerOnlyWriter) Write(b []byte) (int, error) { return len(b), nil }
func (w headerOnlyWriter) WriteHeader(int)             {}

// Cookies 返回登录成功后应下发的会话与 CSRF Cookie，已序列化为 Set-Cookie 头值。
//
// 供 huma 操作以响应头字段输出；属性与 SetCookies 完全一致。
func (s *SessionStore) Cookies(issued *IssuedSession) []string {
	w := headerOnlyWriter{header: http.Header{}}
	s.SetCookies(w, issued)
	return w.header.Values("Set-Cookie")
}

// ClearedCookies 返回用于清除会话与 CSRF Cookie 的 Set-Cookie 头值。
func (s *SessionStore) ClearedCookies() []string {
	w := headerOnlyWriter{header: http.Header{}}
	s.ClearCookies(w)
	return w.header.Values("Set-Cookie")
}
