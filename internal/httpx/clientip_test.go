package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// newRequest 构造带指定直连地址与转发头的请求。
func newRequest(t *testing.T, remoteAddr string, forwarded ...string) *http.Request {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody)
	req.RemoteAddr = remoteAddr
	for _, value := range forwarded {
		req.Header.Add("X-Forwarded-For", value)
	}
	return req
}

// TestUntrustedPeerIgnoresForwardedHeader 是本文件最重要的测试：
// 直连对端不可信时，必须完全忽略 X-Forwarded-For，否则任何客户端
// 都能伪造 IP（GHSA-3fxj-6jh8-hvhx）。
func TestUntrustedPeerIgnoresForwardedHeader(t *testing.T) {
	t.Parallel()

	resolver, err := NewClientIPResolver(LoopbackCIDRs)
	if err != nil {
		t.Fatalf("构造解析器失败: %v", err)
	}

	// 来自公网的直连请求，自称是 1.2.3.4。
	req := newRequest(t, "203.0.113.7:44321", "1.2.3.4")
	if got := resolver.ClientIP(req); got != "203.0.113.7" {
		t.Fatalf("不可信对端的伪造头被采信: 得到 %q，期望 203.0.113.7", got)
	}
}

func TestTrustedProxySingleHop(t *testing.T) {
	t.Parallel()

	resolver, err := NewClientIPResolver(LoopbackCIDRs)
	if err != nil {
		t.Fatalf("构造解析器失败: %v", err)
	}

	req := newRequest(t, "127.0.0.1:8080", "203.0.113.7")
	if got := resolver.ClientIP(req); got != "203.0.113.7" {
		t.Errorf("ClientIP = %q，期望 203.0.113.7", got)
	}
}

// TestTrustedProxyChainSkipsTrustedHops 验证从右向左跳过连续可信代理。
// 从右向左是关键：左端条目可由客户端任意伪造。
func TestTrustedProxyChainSkipsTrustedHops(t *testing.T) {
	t.Parallel()

	resolver, err := NewClientIPResolver(PrivateCIDRs)
	if err != nil {
		t.Fatalf("构造解析器失败: %v", err)
	}

	tests := []struct {
		name      string
		remote    string
		forwarded []string
		want      string
	}{
		{
			name:      "两层内网代理",
			remote:    "10.0.0.1:8080",
			forwarded: []string{"203.0.113.7, 10.0.0.5"},
			want:      "203.0.113.7",
		},
		{
			name:      "多个头字段分开出现",
			remote:    "10.0.0.1:8080",
			forwarded: []string{"203.0.113.7", "192.168.1.1"},
			want:      "203.0.113.7",
		},
		{
			name:      "客户端伪造的左端条目不影响结果",
			remote:    "127.0.0.1:8080",
			forwarded: []string{"9.9.9.9, 203.0.113.7"},
			want:      "203.0.113.7",
		},
		{
			name:      "全链路均为可信代理时退回直连地址",
			remote:    "10.0.0.1:8080",
			forwarded: []string{"10.0.0.5, 192.168.1.1"},
			want:      "10.0.0.1",
		},
		{
			name:      "无转发头时使用直连地址",
			remote:    "10.0.0.1:8080",
			forwarded: nil,
			want:      "10.0.0.1",
		},
		{
			name:      "带端口的转发条目",
			remote:    "127.0.0.1:8080",
			forwarded: []string{"203.0.113.7:12345"},
			want:      "203.0.113.7",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := newRequest(t, tt.remote, tt.forwarded...)
			if got := resolver.ClientIP(req); got != tt.want {
				t.Errorf("ClientIP = %q，期望 %q", got, tt.want)
			}
		})
	}
}

// TestMalformedHopStopsTraversal 验证遇到无法解析的条目即停止回溯，
// 避免攻击者用垃圾数据把回溯推到伪造的左端条目。
func TestMalformedHopStopsTraversal(t *testing.T) {
	t.Parallel()

	resolver, err := NewClientIPResolver(LoopbackCIDRs)
	if err != nil {
		t.Fatalf("构造解析器失败: %v", err)
	}

	req := newRequest(t, "127.0.0.1:8080", "1.2.3.4, garbage")
	if got := resolver.ClientIP(req); got != "127.0.0.1" {
		t.Errorf("ClientIP = %q，期望回退到 127.0.0.1", got)
	}
}

// TestEmptyTrustListIgnoresHeaders 验证默认（不信任任何代理）是安全的。
func TestEmptyTrustListIgnoresHeaders(t *testing.T) {
	t.Parallel()

	resolver, err := NewClientIPResolver(nil)
	if err != nil {
		t.Fatalf("构造解析器失败: %v", err)
	}

	req := newRequest(t, "127.0.0.1:8080", "1.2.3.4")
	if got := resolver.ClientIP(req); got != "127.0.0.1" {
		t.Errorf("空信任列表下应忽略转发头，得到 %q", got)
	}
}

func TestNewClientIPResolverAcceptsBareIP(t *testing.T) {
	t.Parallel()

	resolver, err := NewClientIPResolver([]string{"192.168.1.10", "::1"})
	if err != nil {
		t.Fatalf("单个 IP 应被接受: %v", err)
	}

	req := newRequest(t, "192.168.1.10:8080", "203.0.113.7")
	if got := resolver.ClientIP(req); got != "203.0.113.7" {
		t.Errorf("ClientIP = %q，期望 203.0.113.7", got)
	}
}

func TestNewClientIPResolverRejectsBadCIDR(t *testing.T) {
	t.Parallel()

	for _, bad := range []string{"not-an-ip", "10.0.0.0/99", "300.1.1.1"} {
		if _, err := NewClientIPResolver([]string{bad}); err == nil {
			t.Errorf("非法输入 %q 应被拒绝", bad)
		}
	}
}

func TestIPv6Handling(t *testing.T) {
	t.Parallel()

	resolver, err := NewClientIPResolver([]string{"::1/128"})
	if err != nil {
		t.Fatalf("构造解析器失败: %v", err)
	}

	req := newRequest(t, "[::1]:8080", "2001:db8::1")
	if got := resolver.ClientIP(req); got != "2001:db8::1" {
		t.Errorf("ClientIP = %q，期望 2001:db8::1", got)
	}
}

// TestMiddlewareStripsSpoofedHeader 验证中间件先清除客户端伪造的内部头。
func TestMiddlewareStripsSpoofedHeader(t *testing.T) {
	t.Parallel()

	resolver, err := NewClientIPResolver(LoopbackCIDRs)
	if err != nil {
		t.Fatalf("构造解析器失败: %v", err)
	}

	var seen string
	handler := resolver.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = ClientIPFrom(r)
	}))

	req := newRequest(t, "203.0.113.7:44321")
	// 客户端自行伪造内部头，必须被覆盖。
	req.Header.Set(ClientIPHeader, "1.2.3.4")

	handler.ServeHTTP(httptest.NewRecorder(), req)

	if seen != "203.0.113.7" {
		t.Fatalf("伪造的内部头未被清除: 得到 %q", seen)
	}
}

// TestMiddlewareDoesNotMutateRemoteAddr 验证不覆盖 RemoteAddr——
// 覆盖会让后续代码再也无法判断链路是否可信。
func TestMiddlewareDoesNotMutateRemoteAddr(t *testing.T) {
	t.Parallel()

	resolver, err := NewClientIPResolver(LoopbackCIDRs)
	if err != nil {
		t.Fatalf("构造解析器失败: %v", err)
	}

	var remoteAddr string
	handler := resolver.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		remoteAddr = r.RemoteAddr
	}))

	req := newRequest(t, "127.0.0.1:8080", "203.0.113.7")
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if remoteAddr != "127.0.0.1:8080" {
		t.Errorf("RemoteAddr 被改写为 %q", remoteAddr)
	}
}

func TestClientIPFromFallsBackToRemoteAddr(t *testing.T) {
	t.Parallel()

	req := newRequest(t, "203.0.113.7:44321")
	if got := ClientIPFrom(req); got != "203.0.113.7" {
		t.Errorf("ClientIPFrom = %q，期望回退到直连地址", got)
	}
}
