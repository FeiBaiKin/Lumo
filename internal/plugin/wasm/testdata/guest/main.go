// 测试用插件：覆盖正常返回、处理函数报错、死循环、panic 与宿主往返调用。
package main

import (
	"errors"

	lumo "github.com/FeiBaiKin/lumo/sdk/go"
)

func init() {
	lumo.OnAction("test.echo", func(_ *lumo.Context, e *lumo.Event) error {
		lumo.Info("收到动作", "data", string(e.Data))
		return nil
	})
	lumo.OnAction("test.fail", func(*lumo.Context, *lumo.Event) error {
		return errors.New("故意失败")
	})
	lumo.OnAction("test.spin", func(*lumo.Context, *lumo.Event) error {
		for {
		}
	})
	lumo.OnAction("test.panic", func(*lumo.Context, *lumo.Event) error {
		panic("故意崩溃")
	})
	lumo.OnAction("test.settings", func(*lumo.Context, *lumo.Event) error {
		var s struct {
			Answer int `json:"answer"`
		}
		if err := lumo.SettingsInto("main", &s); err != nil {
			return err
		}
		if s.Answer != 42 {
			return errors.New("宿主给的设置不对")
		}
		return nil
	})
}

func main() {}
