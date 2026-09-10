package app

import (
	"context"
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"
)

// stubModule 是只实现最小接口的模块。
type stubModule struct {
	name     string
	regErr   error
	closeErr error
	closed   *bool
}

func (m *stubModule) Name() string { return m.name }

func (m *stubModule) Register(*App) error { return m.regErr }

// fullModule 实现全部可选能力，用于验证类型断言收集。
type fullModule struct {
	stubModule
	fsys fs.FS
}

func (m *fullModule) Migrations() fs.FS { return m.fsys }

func (m *fullModule) Settings() []SettingGroup {
	return []SettingGroup{{Name: "site", Label: "站点"}}
}

func (m *fullModule) Permissions() []Permission {
	return []Permission{{Key: "posts:write"}}
}

func (m *fullModule) Hooks() []Hook {
	return []Hook{{Name: "late", Priority: 10}, {Name: "early", Priority: 1}}
}

func (m *fullModule) Close(context.Context) error {
	if m.closed != nil {
		*m.closed = true
	}
	return m.closeErr
}

func newApp() *App {
	return New(&Options{})
}

func TestRegisterCollectsCapabilities(t *testing.T) {
	t.Parallel()

	a := newApp()
	fsys := fstest.MapFS{"001_x.sql": {Data: []byte("-- +goose Up")}}
	module := &fullModule{stubModule: stubModule{name: "content"}, fsys: fsys}

	if err := a.Register(module); err != nil {
		t.Fatalf("Register 返回错误: %v", err)
	}

	if got := a.Modules(); len(got) != 1 || got[0] != "content" {
		t.Fatalf("Modules() = %v", got)
	}
	if len(a.Migrations()) != 1 || a.Migrations()[0].Module != "content" {
		t.Errorf("迁移未被收集: %+v", a.Migrations())
	}
	if len(a.Settings()) != 1 {
		t.Errorf("设置分组未被收集: %+v", a.Settings())
	}
	if len(a.Permissions()) != 1 {
		t.Errorf("权限未被收集: %+v", a.Permissions())
	}

	// 钩子必须按优先级升序返回。
	hooks := a.Hooks()
	if len(hooks) != 2 {
		t.Fatalf("钩子数量 = %d", len(hooks))
	}
	if hooks[0].Name != "early" || hooks[1].Name != "late" {
		t.Errorf("钩子未按优先级排序: %v, %v", hooks[0].Name, hooks[1].Name)
	}
}

// TestRegisterMinimalModule 验证只实现最小接口的模块不会因缺少可选方法而失败。
func TestRegisterMinimalModule(t *testing.T) {
	t.Parallel()

	a := newApp()
	if err := a.Register(&stubModule{name: "minimal"}); err != nil {
		t.Fatalf("最小接口模块注册失败: %v", err)
	}
	if len(a.Migrations()) != 0 || len(a.Settings()) != 0 {
		t.Error("最小接口模块不应贡献任何可选能力")
	}
}

func TestRegisterRejectsBadModules(t *testing.T) {
	t.Parallel()

	t.Run("nil 模块", func(t *testing.T) {
		t.Parallel()
		if err := newApp().Register(nil); err == nil {
			t.Fatal("应拒绝 nil 模块")
		}
	})

	t.Run("空模块名", func(t *testing.T) {
		t.Parallel()
		if err := newApp().Register(&stubModule{name: ""}); err == nil {
			t.Fatal("应拒绝空模块名")
		}
	})

	t.Run("重复注册", func(t *testing.T) {
		t.Parallel()
		a := newApp()
		if err := a.Register(&stubModule{name: "dup"}); err != nil {
			t.Fatalf("首次注册失败: %v", err)
		}
		if err := a.Register(&stubModule{name: "dup"}); err == nil {
			t.Fatal("应拒绝重复模块名")
		}
	})

	t.Run("注册失败即整体失败", func(t *testing.T) {
		t.Parallel()
		a := newApp()
		want := errors.New("boom")
		err := a.Register(&stubModule{name: "bad", regErr: want})
		if !errors.Is(err, want) {
			t.Fatalf("错误未被包装传出: %v", err)
		}
		if len(a.Modules()) != 0 {
			t.Error("注册失败的模块不应被记入")
		}
	})
}

// TestCloseReverseOrder 验证按注册逆序关闭，且单个失败不影响其余模块清理。
func TestCloseReverseOrder(t *testing.T) {
	t.Parallel()

	var order []string
	a := newApp()

	first := &recordingModule{name: "first", order: &order}
	second := &recordingModule{name: "second", order: &order, err: errors.New("close failed")}
	third := &recordingModule{name: "third", order: &order}

	if err := a.Register(first, second, third); err != nil {
		t.Fatalf("注册失败: %v", err)
	}

	err := a.Close(context.Background())
	if err == nil {
		t.Fatal("应返回模块关闭错误")
	}
	if len(order) != 3 {
		t.Fatalf("关闭顺序记录 = %v", order)
	}
	if order[0] != "third" || order[2] != "first" {
		t.Errorf("未按逆序关闭: %v", order)
	}
}

type recordingModule struct {
	name  string
	order *[]string
	err   error
}

func (m *recordingModule) Name() string        { return m.name }
func (m *recordingModule) Register(*App) error { return nil }
func (m *recordingModule) Close(context.Context) error {
	*m.order = append(*m.order, m.name)
	return m.err
}

func TestAppAccessors(t *testing.T) {
	t.Parallel()

	a := newApp()
	if a.Logger() == nil {
		t.Error("Logger 不应为 nil（应回退到 slog.Default）")
	}
	if a.DB() != nil {
		t.Error("未提供 DB 时应为 nil")
	}
	if a.Router() != nil {
		t.Error("未提供 Router 时应为 nil")
	}
}
