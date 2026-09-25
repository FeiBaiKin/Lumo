package plugin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/mail"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	lumomail "github.com/FeiBaiKin/lumo/internal/mail"
	"github.com/FeiBaiKin/lumo/internal/ratelimit"
)

// 外部网络与邮件：插件对外说话的两条路，都要被授予，都有上限。
func init() {
	hostOps["http.fetch"] = hostOp{
		allowed: func(g *Capabilities) bool { return len(g.HTTP) > 0 },
		denied:  "在 capabilities.http 里列出要访问的域名",
		call:    hostHTTPFetch,
	}
	hostOps["mail.send"] = hostOp{
		allowed: func(g *Capabilities) bool { return g.Mail },
		denied:  "声明 capabilities.mail: true",
		call:    hostMailSend,
	}
}

const (
	maxFetchRequestBody  = 1 << 20
	maxFetchResponseBody = 4 << 20
	maxFetchTimeout      = 10 * time.Second
	maxFetchRedirects    = 3
	maxFetchHeaders      = 50
	// mailPerHour 是每个插件每小时能发的邮件数。
	mailPerHour = 30
	// maxMailRecipients 是一封邮件的收件人上限。
	maxMailRecipients = 10
)

// blockedPrefixes 是插件连不得的地址段：本机、内网、链路本地、运营商级 NAT、组播与保留段。
//
// 只查域名挡不住「一个公网域名解析到内网地址」这种绕法，所以在建连那一刻核对解析出的 IP。
var blockedPrefixes = func() []netip.Prefix {
	var out []netip.Prefix
	for _, cidr := range []string{
		"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12",
		"192.0.0.0/24", "192.168.0.0/16", "224.0.0.0/4", "240.0.0.0/4",
		"::/128", "::1/128", "fc00::/7", "fe80::/10", "ff00::/8",
	} {
		out = append(out, netip.MustParsePrefix(cidr))
	}
	return out
}()

// publicAddr 判断一个地址是不是插件可以连的公网地址。
func publicAddr(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsValid() {
		return false
	}
	for _, prefix := range blockedPrefixes {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}

// guardedDialer 在建连前核对目标地址：解析出内网地址时拒绝连接。
var guardedDialer = &net.Dialer{
	Timeout: 5 * time.Second,
	Control: func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		addr, err := netip.ParseAddr(host)
		if err != nil || !publicAddr(addr) {
			return fmt.Errorf("插件不能连接内网或本机地址 %s", host)
		}
		return nil
	},
}

// fetchTransport 不走环境变量里的代理：代理会把「连的是谁」藏起来，上面那道地址核对就失效了。
var fetchTransport = &http.Transport{
	DialContext:           guardedDialer.DialContext,
	TLSHandshakeTimeout:   5 * time.Second,
	ResponseHeaderTimeout: maxFetchTimeout,
	MaxIdleConns:          20,
	IdleConnTimeout:       30 * time.Second,
}

// 插件不能自己写的请求头：它们属于传输层，改了要么无效，要么能拿来混淆代理与服务端。
var forbiddenFetchHeaders = map[string]bool{
	"host": true, "content-length": true, "transfer-encoding": true, "connection": true,
	"upgrade": true, "te": true, "trailer": true, "proxy-authorization": true,
}

type fetchArgs struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	// Body 是文本请求体；二进制请求体放 BodyBase64。
	Body       string `json:"body"`
	BodyBase64 string `json:"bodyBase64"`
	// Timeout 是毫秒，最长 10 秒。
	Timeout int `json:"timeout"`
}

type fetchResult struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	// Body 是 UTF-8 文本响应体；不是合法 UTF-8 时放在 BodyBase64 里。
	Body       string `json:"body,omitempty"`
	BodyBase64 string `json:"bodyBase64,omitempty"`
	// Truncated 为真表示响应体超过 4 MiB，后面的被截掉了。
	Truncated bool `json:"truncated,omitempty"`
}

// checkFetchURL 核对地址：只收 http 与 https，主机须在授予的域名里。
func checkFetchURL(granted *Capabilities, raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return nil, fmt.Errorf("地址须是 http 或 https 的绝对地址：%q", raw)
	}
	if u.User != nil {
		return nil, errors.New("地址里不能带用户名与口令，请放进请求头")
	}
	if !granted.AllowsHost(u.Hostname()) {
		return nil, fmt.Errorf("插件没有被授予访问 %s（清单的 capabilities.http 里没有它）", u.Hostname())
	}
	return u, nil
}

func hostHTTPFetch(ctx context.Context, m *Module, loaded *Loaded, args json.RawMessage) (any, error) {
	var in fetchArgs
	if err := decodeArgs("http.fetch", args, &in); err != nil {
		return nil, err
	}
	granted := loaded.Granted
	u, err := checkFetchURL(granted, in.URL)
	if err != nil {
		return nil, err
	}
	method := strings.ToUpper(strings.TrimSpace(in.Method))
	if method == "" {
		method = http.MethodGet
	}
	body := []byte(in.Body)
	if in.BodyBase64 != "" {
		if body, err = base64.StdEncoding.DecodeString(in.BodyBase64); err != nil {
			return nil, errors.New("bodyBase64 不是合法的 base64")
		}
	}
	if len(body) > maxFetchRequestBody {
		return nil, fmt.Errorf("请求体不能超过 %d KiB", maxFetchRequestBody>>10)
	}
	if len(in.Headers) > maxFetchHeaders {
		return nil, fmt.Errorf("请求头不能超过 %d 个", maxFetchHeaders)
	}

	timeout := maxFetchTimeout
	if in.Timeout > 0 && time.Duration(in.Timeout)*time.Millisecond < timeout {
		timeout = time.Duration(in.Timeout) * time.Millisecond
	}
	fetchCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(fetchCtx, method, u.String(), strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("构造请求: %w", err)
	}
	for key, value := range in.Headers {
		if forbiddenFetchHeaders[strings.ToLower(key)] {
			return nil, fmt.Errorf("插件不能设置请求头 %s", key)
		}
		req.Header.Set(key, value)
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "Lumo-Plugin/"+loaded.ID())
	}
	var transport http.RoundTripper = fetchTransport
	if m.transport != nil {
		transport = m.transport
	}
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(next *http.Request, via []*http.Request) error {
			if len(via) >= maxFetchRedirects {
				return fmt.Errorf("重定向超过 %d 次", maxFetchRedirects)
			}
			// 每一跳都要落在授予的域名里，否则一个开放重定向就能把请求带到别处
			_, redirectErr := checkFetchURL(granted, next.URL.String())
			return redirectErr
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求 %s 失败: %w", u.Hostname(), err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxFetchResponseBody+1))
	if err != nil {
		return nil, fmt.Errorf("读取 %s 的响应: %w", u.Hostname(), err)
	}
	out := fetchResult{Status: resp.StatusCode, Headers: map[string]string{}}
	if len(data) > maxFetchResponseBody {
		data, out.Truncated = data[:maxFetchResponseBody], true
	}
	for key := range resp.Header {
		out.Headers[strings.ToLower(key)] = resp.Header.Get(key)
	}
	if utf8.Valid(data) {
		out.Body = string(data)
	} else {
		out.BodyBase64 = base64.StdEncoding.EncodeToString(data)
	}
	return out, nil
}

func hostMailSend(ctx context.Context, m *Module, loaded *Loaded, args json.RawMessage) (any, error) {
	var in struct {
		To      []string `json:"to"`
		Subject string   `json:"subject"`
		Text    string   `json:"text"`
		HTML    string   `json:"html"`
	}
	if err := decodeArgs("mail.send", args, &in); err != nil {
		return nil, err
	}
	if len(in.To) == 0 || len(in.To) > maxMailRecipients {
		return nil, fmt.Errorf("收件人须有 1 到 %d 个", maxMailRecipients)
	}
	for _, to := range in.To {
		if _, err := mail.ParseAddress(to); err != nil {
			return nil, fmt.Errorf("收件人 %q 不是合法的邮箱地址", to)
		}
	}
	if m.mail == nil || !m.mail.Enabled(ctx) {
		return nil, errors.New("站点还没有配好邮件发送")
	}
	if m.db != nil {
		count, err := ratelimit.New(m.db).Hit(ctx, "plugin.mail:"+loaded.ID(), time.Hour)
		if err != nil {
			return nil, err
		}
		if count > mailPerHour {
			return nil, fmt.Errorf("这个小时发的邮件已达上限（%d 封）", mailPerHour)
		}
	}
	msg := &lumomail.Message{To: in.To, Subject: in.Subject, Text: in.Text, HTML: in.HTML}
	if err := msg.Validate(); err != nil {
		return nil, err
	}
	m.mail.Enqueue(ctx, msg)
	return nil, nil
}
