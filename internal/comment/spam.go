package comment

import (
	"strings"
	"time"
)

// Verdict 是一次反垃圾判定的结论。
type Verdict struct {
	// Spam 为真时直接标为垃圾，不进审核队列。
	Spam bool
	// Reason 说明判据，写进日志便于调规则。
	Reason string
}

// SpamInput 是判定所需的信息。
type SpamInput struct {
	AuthorName string
	AuthorURL  string
	Content    string
	// Honeypot 是表单里的隐藏字段：正常访客看不见也不会填，填了的必是机器人。
	Honeypot string
	// LastFromIP 是同一 IP 的上一条评论时间，零值表示没有。
	LastFromIP time.Time
	Now        time.Time
}

// SpamChecker 判定一条评论是否为垃圾。
//
// 做成接口是为了给插件留位置：v1 只做频率限制、链接数与关键词，
// Akismet 一类的服务留给扩展实现，核心不引入任何外部依赖。
type SpamChecker interface {
	Check(cfg *Settings, in *SpamInput) Verdict
}

// DefaultChecker 是内置判定器。
type DefaultChecker struct{}

// NewSpamChecker 返回内置判定器。
func NewSpamChecker() SpamChecker { return DefaultChecker{} }

// Check 实现 SpamChecker。判据从确定性最高的往下排，命中即返回。
func (DefaultChecker) Check(cfg *Settings, in *SpamInput) Verdict {
	// 蜜罐被填：访客看不见这个字段，填了就是机器人，没有第二种解释。
	if strings.TrimSpace(in.Honeypot) != "" {
		return Verdict{Spam: true, Reason: "蜜罐字段被填写"}
	}

	if cfg.IntervalSeconds > 0 && !in.LastFromIP.IsZero() {
		elapsed := in.Now.Sub(in.LastFromIP)
		if elapsed < time.Duration(cfg.IntervalSeconds)*time.Second {
			return Verdict{Spam: true, Reason: "同一 IP 发表过于频繁"}
		}
	}

	if cfg.MaxLinks > 0 && countLinks(in.Content) > cfg.MaxLinks {
		return Verdict{Spam: true, Reason: "链接数超过上限"}
	}

	// 关键词黑名单：评论者名字与主页地址一并参与匹配，
	// 垃圾评论常把关键词塞在名字里而正文写得很干净。
	if hit, ok := matchBlocklist(cfg.Blocklist, in.AuthorName, in.AuthorURL, in.Content); ok {
		return Verdict{Spam: true, Reason: "命中关键词 " + hit}
	}

	return Verdict{}
}

// countLinks 统计正文里的 http(s) 链接数。
func countLinks(text string) int {
	lower := strings.ToLower(text)
	return strings.Count(lower, "http://") + strings.Count(lower, "https://")
}

// matchBlocklist 按行解析黑名单并做大小写不敏感的匹配。
//
// 空行与 # 开头的注释行跳过：黑名单是给人手写的，得允许写注释。
func matchBlocklist(blocklist string, fields ...string) (string, bool) {
	if strings.TrimSpace(blocklist) == "" {
		return "", false
	}
	haystack := strings.ToLower(strings.Join(fields, "\n"))

	for _, line := range strings.Split(blocklist, "\n") {
		keyword := strings.TrimSpace(line)
		if keyword == "" || strings.HasPrefix(keyword, "#") {
			continue
		}
		if strings.Contains(haystack, strings.ToLower(keyword)) {
			return keyword, true
		}
	}
	return "", false
}
