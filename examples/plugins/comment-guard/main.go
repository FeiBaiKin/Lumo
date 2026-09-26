// Package main 是示例插件「评论反垃圾」。
//
// 它演示插件系统里最常用的一条路：在评论入库前用过滤器拦一道（comment.judge），
// 收到新评论后用动作记账（comment.created），把统计结果经侧栏小组件与公开接口露出来，
// 并在设置页里让站长调规则。
//
// 判定只看评论本身：命中关键词、链接太多、邮箱在拒收名单里。示例刻意不调外部服务——
// 真要接第三方反垃圾，照 capabilities.http 的声明与 lumo.Fetch 的用法替换 judge 即可。
package main

import (
	"fmt"
	"strings"
	"time"

	lumo "github.com/FeiBaiKin/lumo/sdk/go"
)

// config 是 settings.yaml 里 guard 分组的值。
type config struct {
	Keywords      string `json:"keywords"`
	MaxLinks      int    `json:"maxLinks"`
	BlockedEmails string `json:"blockedEmails"`
}

// 计数器按天分键，35 天后自动过期，不必自己清理。
const counterTTL = 35 * 24 * time.Hour

func init() {
	lumo.OnCommentJudge(judge)
	lumo.OnComment(lumo.ActionCommentCreated, tally)
	lumo.OnWidget("guard-report", report)
	lumo.OnRoute("stats", stats)
}

// main 是空的：这种模块是「反应器」，被宿主按需调用，自己不跑主循环。
// 没有它 Go 不认这是个 main 包，链接会报 function main is undeclared。
func main() {}

// judge 在评论入库前判一次：判为垃圾就把结论写回 value。
func judge(_ *lumo.Context, j *lumo.CommentJudgement) error {
	cfg := loadConfig()
	reason := reasonFor(cfg, &j.Comment)
	if reason == "" {
		return nil
	}
	j.Status = lumo.CommentSpam
	j.Reason = reason
	// 这里立刻计数：此刻还没有评论 ID，而我们要数的正是「自己拦下了多少条」。
	// 内核自己的频率与蜜罐判定走不到这里，所以这个数字只算我们的功劳。
	if _, err := lumo.KV.Incr("spam:"+today(), 1, counterTTL); err != nil {
		lumo.Warn("记拦截数失败", "error", err)
	}
	return nil
}

// tally 收到每条新评论（含被判为垃圾的）记一笔总数。
func tally(_ *lumo.Context, c *lumo.Comment) error {
	if _, err := lumo.KV.Incr("total:"+today(), 1, counterTTL); err != nil {
		return err
	}
	// 顺手留一条最近记录，后台的资源页会自动出列表。
	if c.Status == lumo.CommentSpam {
		_, err := lumo.Resources("SpamLog").Create(map[string]any{
			"author":  c.Author.Name,
			"content": truncate(c.Content, 200),
			"postId":  c.Post.ID,
			"ip":      c.IP,
		})
		return err
	}
	return nil
}

// report 是侧栏小组件：今天拦下了多少条。
func report(_ *lumo.Context, _ *lumo.PageInfo) (string, error) {
	spam, total, err := todayCounts()
	if err != nil {
		return "", err
	}
	if total == 0 {
		return `<p class="guard-report">今天还没有评论。</p>`, nil
	}
	return fmt.Sprintf(
		`<p class="guard-report">今天收到 %d 条评论，其中 <strong>%d</strong> 条判为垃圾。</p>`,
		total, spam), nil
}

// stats 是公开接口：GET /api/v1/plugins/comment-guard/stats。
func stats(_ *lumo.Context, _ *lumo.Request) (*lumo.Response, error) {
	spam, total, err := todayCounts()
	if err != nil {
		return nil, err
	}
	return lumo.JSON(200, map[string]any{"date": today(), "spam": spam, "total": total}), nil
}

// reasonFor 给出判为垃圾的理由；不判时返回空串。
func reasonFor(cfg config, c *lumo.Comment) string {
	body := strings.ToLower(c.Content)
	for _, word := range lines(cfg.Keywords) {
		if strings.Contains(body, strings.ToLower(word)) {
			return "命中关键词：" + word
		}
	}
	links := strings.Count(body, "http://") + strings.Count(body, "https://")
	if cfg.MaxLinks > 0 && links > cfg.MaxLinks {
		return fmt.Sprintf("正文里有 %d 个链接，超过 %d 个", links, cfg.MaxLinks)
	}
	email := strings.ToLower(strings.TrimSpace(c.Author.Email))
	if email != "" {
		for _, blocked := range lines(cfg.BlockedEmails) {
			if strings.EqualFold(blocked, email) {
				return "邮箱在拒收名单里"
			}
		}
	}
	return ""
}

// loadConfig 读设置；读不到（还没授予能力之类）就用缺省值。
func loadConfig() config {
	cfg := config{MaxLinks: 3}
	if err := lumo.SettingsInto("guard", &cfg); err != nil {
		lumo.Warn("读设置失败，按缺省值判定", "error", err)
	}
	return cfg
}

// todayCounts 取今天拦下与收到的条数。
func todayCounts() (spam, total int64, err error) {
	day := today()
	if spam, err = counter("spam:" + day); err != nil {
		return 0, 0, err
	}
	total, err = counter("total:" + day)
	return spam, total, err
}

// counter 读一个计数器；键不存在表示 0，不是错。
func counter(key string) (int64, error) {
	var n int64
	if _, err := lumo.KV.Get(key, &n); err != nil {
		return 0, err
	}
	return n, nil
}

// today 按 UTC 日期分键：插件拿不到站点时区，按天的计数以 UTC 零点换日。
func today() string { return time.Now().UTC().Format("2006-01-02") }

// lines 把多行文本切成去掉空行的列表。
func lines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

// truncate 按字节截断，不切坏 UTF-8 字符。
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := s[:max]
	for len(cut) > 0 && !isUTF8Start(cut[len(cut)-1]) {
		cut = cut[:len(cut)-1]
	}
	return cut
}

func isUTF8Start(b byte) bool { return b&0xC0 != 0x80 }
