// Package main 是示例插件「新评论邮件通知」。
//
// 它演示动作钩子与宿主能力的配合：收到评论（comment.created）之后，用站点自己的 SMTP
// 给站长发一封通知信。动作是异步执行的，发信慢不会拖慢访客提交评论那一下。
//
// 邮件经站点的发送队列，失败会重试；每个插件每小时最多 30 封，这个上限由宿主把关，
// 插件自己也该按需过滤——示例用「正文长度下限」与「只通知待审的」两个设置来压低发信量。
package main

import (
	"fmt"
	"html"
	"strings"

	lumo "github.com/FeiBaiKin/lumo/sdk/go"
)

// config 是 settings.yaml 里 notify 分组的值。
type config struct {
	Recipients  string `json:"recipients"`
	OnlyPending bool   `json:"onlyPending"`
	MinLength   int    `json:"minLength"`
}

func init() {
	lumo.OnComment(lumo.ActionCommentCreated, notify)
}

func main() {}

// notify 收到新评论时给站长发一封。
//
// 这里返回的 error 只进日志，不会影响评论本身——动作是异步的，评论早就入库了。
func notify(_ *lumo.Context, c *lumo.Comment) error {
	cfg := loadConfig()
	if cfg.MinLength > 0 && len([]rune(c.Content)) < cfg.MinLength {
		return nil
	}
	if cfg.OnlyPending && c.Status != lumo.CommentPending {
		return nil
	}
	to := lines(cfg.Recipients)
	if len(to) == 0 {
		// 没填收件人就不发：与其发到某个默认地址，不如什么都不做
		return nil
	}
	link := postLink(c.Post)
	subject := fmt.Sprintf("[%s] 新评论：%s", c.Post.Title, c.Author.Name)
	text := fmt.Sprintf(`%s 在《%s》下评论：

%s

原文：%s
留言者：%s <%s>
来源 IP：%s
`, c.Author.Name, c.Post.Title, c.Content, link, c.Author.Name, c.Author.Email, c.IP)
	mail := lumo.Mail{To: to, Subject: subject, Text: text}
	if link != "" {
		mail.HTML = fmt.Sprintf(
			`<p>%s 在《<a href="%s">%s</a>》下评论：</p><blockquote>%s</blockquote>`,
			html.EscapeString(c.Author.Name), html.EscapeString(link),
			html.EscapeString(c.Post.Title), html.EscapeString(c.Content))
	}
	return lumo.SendMail(mail)
}

// postLink 拼出评论所在文章的完整地址；站点没配地址时退回相对路径。
func postLink(p lumo.PostRef) string {
	if p.Path == "" {
		return ""
	}
	info, err := lumo.Self()
	if err != nil || info.Site == "" {
		return p.Path
	}
	return strings.TrimSuffix(info.Site, "/") + p.Path
}

// loadConfig 读设置；读不到就用缺省值（不填收件人时什么也不发）。
func loadConfig() config {
	cfg := config{MinLength: 1}
	if err := lumo.SettingsInto("notify", &cfg); err != nil {
		lumo.Warn("读设置失败，这次不通知", "error", err)
	}
	return cfg
}

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
