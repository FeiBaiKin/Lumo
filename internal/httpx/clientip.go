package httpx

import (
	"fmt"
	"net"
	"net/http"
	"strings"
)

// ClientIPResolver 在已知可信代理范围的前提下解析真实客户端 IP。
//
// 为什么不能直接用 chi 的 middleware.RealIP：它无条件信任 X-Forwarded-For
// 与 X-Real-IP，任何客户端都能伪造这些头（GHSA-3fxj-6jh8-hvhx）。伪造的 IP
// 会污染日志、绕过限流、并让基于 IP 的审计失效。
//
// 正确做法是：只有当直连对端本身属于可信代理时，才采信转发头；
// 并从 X-Forwarded-For 右端向左跳过连续的可信代理，取第一个非可信地址。
type ClientIPResolver struct {
	trusted []*net.IPNet
}

// LoopbackCIDRs 是仅信任本机回环的默认配置，适用于「反向代理与应用同机」的常见部署。
var LoopbackCIDRs = []string{"127.0.0.0/8", "::1/128"}

// PrivateCIDRs 是 RFC 1918 与链路本地范围，适用于代理位于内网的部署。
var PrivateCIDRs = []string{
	"127.0.0.0/8", "::1/128",
	"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16",
	"169.254.0.0/16", "fc00::/7", "fe80::/10",
}

// NewClientIPResolver 按 CIDR 列表构造解析器。
//
// 传入空列表表示不信任任何代理，此时一律使用直连对端地址——
// 这是最安全的默认值。
func NewClientIPResolver(cidrs []string) (*ClientIPResolver, error) {
	trusted := make([]*net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		cidr = strings.TrimSpace(cidr)
		if cidr == "" {
			continue
		}
		// 允许写单个 IP，自动补全为 /32 或 /128。
		if !strings.Contains(cidr, "/") {
			ip := net.ParseIP(cidr)
			if ip == nil {
				return nil, fmt.Errorf("非法的可信代理地址 %q", cidr)
			}
			bits := 32
			if ip.To4() == nil {
				bits = 128
			}
			cidr = fmt.Sprintf("%s/%d", cidr, bits)
		}
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			return nil, fmt.Errorf("非法的可信代理网段 %q: %w", cidr, err)
		}
		trusted = append(trusted, network)
	}
	return &ClientIPResolver{trusted: trusted}, nil
}

// isTrusted 报告 IP 是否属于可信代理范围。
func (r *ClientIPResolver) isTrusted(ip net.IP) bool {
	if ip == nil {
		return false
	}
	for _, network := range r.trusted {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// ClientIP 返回请求的客户端 IP。
//
// 直连对端不可信时直接返回它，忽略所有转发头。
func (r *ClientIPResolver) ClientIP(req *http.Request) string {
	remote := remoteIP(req.RemoteAddr)
	if remote == nil {
		return ""
	}
	if !r.isTrusted(remote) {
		return remote.String()
	}

	// 从右向左跳过连续可信代理，取第一个非可信地址即真实客户端。
	// 从右向左是关键：左端的条目可由客户端任意伪造。
	forwarded := req.Header.Values("X-Forwarded-For")
	hops := make([]string, 0, len(forwarded))
	for _, header := range forwarded {
		for _, part := range strings.Split(header, ",") {
			if trimmed := strings.TrimSpace(part); trimmed != "" {
				hops = append(hops, trimmed)
			}
		}
	}
	for i := len(hops) - 1; i >= 0; i-- {
		ip := net.ParseIP(stripPort(hops[i]))
		if ip == nil {
			// 出现无法解析的条目说明链路已不可信，停止回溯。
			break
		}
		if !r.isTrusted(ip) {
			return ip.String()
		}
	}

	// 全链路都是可信代理（或无转发头）时，退回直连对端。
	return remote.String()
}

// Middleware 把解析出的客户端 IP 注入请求头 X-Lumo-Client-IP，供下游读取。
//
// 不修改 r.RemoteAddr：那正是 middleware.RealIP 的问题所在——
// 覆盖原始值会让后续代码再也无法判断链路是否可信。
func (r *ClientIPResolver) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// 先清除客户端可能自行伪造的同名头。
		req.Header.Del(ClientIPHeader)
		if ip := r.ClientIP(req); ip != "" {
			req.Header.Set(ClientIPHeader, ip)
		}
		next.ServeHTTP(w, req)
	})
}

// ClientIPHeader 是内部传递客户端 IP 的请求头名。
const ClientIPHeader = "X-Lumo-Client-IP"

// ClientIPFrom 读取中间件注入的客户端 IP，缺失时回退到直连对端。
func ClientIPFrom(req *http.Request) string {
	if ip := req.Header.Get(ClientIPHeader); ip != "" {
		return ip
	}
	if ip := remoteIP(req.RemoteAddr); ip != nil {
		return ip.String()
	}
	return ""
}

// remoteIP 从 RemoteAddr 中解析 IP。
func remoteIP(remoteAddr string) net.IP {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		// RemoteAddr 也可能不带端口。
		host = remoteAddr
	}
	return net.ParseIP(strings.Trim(host, "[]"))
}

// stripPort 去掉可能存在的端口部分，兼容 "1.2.3.4:5678" 与 "[::1]:80"。
func stripPort(addr string) string {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return strings.Trim(host, "[]")
	}
	return strings.Trim(addr, "[]")
}
