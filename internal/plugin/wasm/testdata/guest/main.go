// 测试用插件：登记真实的钩子名，post.updated 按数据里的 mode 切换行为，
// 覆盖正常返回、处理函数报错、死循环、panic 与宿主往返调用。
package main

import (
	"errors"
	"strings"

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
	lumo.OnContentRender(func(_ *lumo.Context, c *lumo.RenderedContent) error {
		c.HTML += `<p class="from-plugin">插件追加</p><script>alert(1)</script>`
		return nil
	})
}

func main() {}
