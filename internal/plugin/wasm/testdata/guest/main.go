// 测试用插件：登记真实的钩子名，post.updated 按数据里的 mode 切换行为，
// 覆盖正常返回、处理函数报错、死循环、panic 与宿主往返调用。
package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	lumo "github.com/FeiBaiKin/lumo/sdk/go"
)

func init() {
	lumo.OnComment(lumo.ActionCommentCreated, func(_ *lumo.Context, c *lumo.Comment) error {
		lumo.Info("收到评论", "author", c.Author.Name)
		return nil
	})
	lumo.OnAction(lumo.ActionPostUpdated, func(_ *lumo.Context, e *lumo.Event) error {
		var in struct {
			Mode string `json:"mode"`
		}
		_ = e.Decode(&in)
		switch in.Mode {
		case "fail":
			return errors.New("故意失败")
		case "spin":
			for {
			}
		case "panic":
			panic("故意崩溃")
		case "data":
			return exerciseData()
		case "content":
			return exerciseContent()
		case "fetch":
			return exerciseFetch()
		case "ticks":
			var ticks int
			if found, err := lumo.KV.Get("ticks", &ticks); err != nil || !found || ticks < 1 {
				return fmt.Errorf("定时任务没有跑过：%v %v %d", found, err, ticks)
			}
		case "settings":
			var s struct {
				Answer int `json:"answer"`
			}
			if err := lumo.SettingsInto("main", &s); err != nil {
				return err
			}
			if s.Answer != 42 {
				return errors.New("宿主给的设置不对")
			}
		}
		lumo.Info("收到动作", "mode", in.Mode)
		return nil
	})
	lumo.OnCommentJudge(func(_ *lumo.Context, j *lumo.CommentJudgement) error {
		if strings.Contains(j.Comment.Content, "买药") {
			j.Status, j.Reason = lumo.CommentSpam, "含广告词"
		}
		return nil
	})
	lumo.OnCron("tick", func(*lumo.Context) error {
		_, err := lumo.KV.Incr("ticks", 1, 0)
		return err
	})
	lumo.OnContentRender(func(_ *lumo.Context, c *lumo.RenderedContent) error {
		c.HTML += `<p class="from-plugin">插件追加</p><script>alert(1)</script>`
		return nil
	})
}

func main() {}

// exerciseData 走一遍键值与资源记录的宿主能力，任何一步不对就报错。
func exerciseData() error {
	first, err := lumo.KV.Incr("hits", 1, 0)
	if err != nil {
		return err
	}
	if second, _ := lumo.KV.Incr("hits", 1, 0); second != first+1 {
		return fmt.Errorf("自增不对：%d 之后是 %d", first, second)
	}
	if err := lumo.KV.Set("greeting", map[string]string{"text": "你好"}, time.Hour); err != nil {
		return err
	}
	var greeting struct {
		Text string `json:"text"`
	}
	if found, err := lumo.KV.Get("greeting", &greeting); err != nil || !found || greeting.Text != "你好" {
		return fmt.Errorf("读回的键值不对：%v %v %+v", found, err, greeting)
	}

	notes := lumo.Resources("Note")
	rec, err := notes.Create(map[string]any{"title": "插件写的", "score": 3})
	if err != nil {
		return err
	}
	page, err := notes.List(lumo.Query{Match: map[string]any{"title": "插件写的"}})
	if err != nil || page.Total < 1 {
		return fmt.Errorf("按字段查不到刚写的记录：%v", err)
	}
	if _, err := notes.Patch(rec.ID, map[string]any{"score": 5}); err != nil {
		return err
	}
	got, err := notes.Get(rec.ID)
	if err != nil {
		return err
	}
	var note struct {
		Title string `json:"title"`
		Score int    `json:"score"`
	}
	if err := got.Decode(&note); err != nil || note.Title != "插件写的" || note.Score != 5 {
		return fmt.Errorf("局部修改后的记录不对：%+v %v", note, err)
	}
	if _, err := notes.Create(map[string]any{"score": "不是数字"}); err == nil {
		return errors.New("不合字段声明的记录应被拒绝")
	}
	return nil
}

// exerciseContent 走一遍读写站点内容：发文章、读回、改写、按标题查、审核评论。
func exerciseContent() error {
	post, err := lumo.Content.CreatePost(lumo.PostInput{
		Title: "插件发的文章", Raw: `<p>来自插件</p><script>alert(1)</script>`, Publish: true,
	})
	if err != nil {
		return fmt.Errorf("发文章：%w", err)
	}
	if post.Status != "published" || post.Path == "" {
		return fmt.Errorf("发出的文章状态不对：%+v", post)
	}
	got, err := lumo.Content.Post(post.ID)
	if err != nil {
		return err
	}
	if !strings.Contains(got.Content, "来自插件") || strings.Contains(got.Content, "<script") {
		return fmt.Errorf("插件发的正文应被净化：%s", got.Content)
	}
	if _, err := lumo.Content.UpdatePost(post.ID, lumo.PostInput{Title: "插件改过的文章", Raw: "<p>改过</p>"}); err != nil {
		return fmt.Errorf("改文章：%w", err)
	}
	items, total, err := lumo.Content.Posts(lumo.PostQuery{Search: "插件改过"})
	if err != nil || total != 1 || items[0].ID != post.ID {
		return fmt.Errorf("按标题查不到改过的文章：%v %d", err, total)
	}
	spam, _, err := lumo.Content.Comments(lumo.CommentQuery{Status: lumo.CommentSpam})
	if err != nil {
		return err
	}
	for _, c := range spam {
		if err := lumo.Content.ModerateComment(c.ID, lumo.CommentApproved); err != nil {
			return fmt.Errorf("审核评论：%w", err)
		}
	}
	return nil
}

// exerciseFetch 访问白名单里的域名应成功，白名单外的应被拒；站点没配邮件时发信应得到明确的错误。
func exerciseFetch() error {
	resp, err := lumo.Fetch(lumo.FetchRequest{URL: "https://api.example.com/ping"})
	if err != nil {
		return fmt.Errorf("访问白名单域名：%w", err)
	}
	if resp.Status != 200 || string(resp.Body) != "pong" {
		return fmt.Errorf("响应不对：%d %q", resp.Status, resp.Body)
	}
	if _, err := lumo.Fetch(lumo.FetchRequest{URL: "https://evil.example.net/"}); err == nil {
		return errors.New("白名单外的域名应被拒绝")
	}
	err = lumo.SendMail(lumo.Mail{To: []string{"owner@example.com"}, Subject: "测试", Text: "测试"})
	if err == nil || !strings.Contains(err.Error(), "邮件") {
		return fmt.Errorf("站点没配邮件时应说明原因，得到 %v", err)
	}
	return nil
}
