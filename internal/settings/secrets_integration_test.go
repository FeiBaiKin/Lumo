package settings_test

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/auth/perm"
	"github.com/FeiBaiKin/lumo/internal/config"
	"github.com/FeiBaiKin/lumo/internal/form"
	"github.com/FeiBaiKin/lumo/internal/migrate"
	"github.com/FeiBaiKin/lumo/internal/secret"
	"github.com/FeiBaiKin/lumo/internal/settings"
	"github.com/FeiBaiKin/lumo/internal/testsupport"
)

// 本组的库与 settings_integration_test.go 分开，各用独占 schema。
const secretTestSchema = "lumo_it_settings_secret"

// demoModule 只为这次测试存在：声明一个带口令字段的分组，形状与 mail / storage 一致。
//
// 用它而不是直接拿 mail 分组来验：这里要盯的是「口令字段」这套通用机制，
// 而 mail 分组还带着发信、发件人一堆无关字段，断言会被它们稀释。
type demoModule struct{}

func (demoModule) Name() string            { return "demo" }
func (demoModule) Register(*app.App) error { return nil }

func (demoModule) Settings() []app.SettingGroup {
	f := form.New(
		form.NewSection("发信",
			form.Bool("enabled").Label("启用").Default(true),
			form.Text("host").Label("服务器").Required().Default("smtp.example.com"),
			form.Secret("password").Label("口令").Default("").ShowIf(form.Eq("enabled", true)),
		),
	).Named("demo")
	return []app.SettingGroup{{
		Name: "demo", Label: "演示", Form: f, Public: []string{"enabled"},
	}}
}

// TestSecretSettingsEndToEnd 走完整条链路验口令的两条底线：
// 接口不回传明文，库里不存明文。
//
// 这两条都只在「真的过一遍 HTTP + 真的写一次库」时才算验过：
// 单测里换个内存存储也能测加密，但那测不出处理器有没有把有效值原样吐出去。
func TestSecretSettingsEndToEnd(t *testing.T) {
	ctx := context.Background()
	set := settings.New()
	db := testsupport.Open(t, testsupport.Options{
		Schema:  secretTestSchema,
		Migrate: true,
		Sources: []migrate.Source{{Name: set.Name(), FS: set.Migrations()}},
	})
	cfg := config.Default()
	cfg.DataDir = t.TempDir()

	stack := testsupport.NewStackWith(t, db, &testsupport.StackOptions{
		Config:  cfg,
		Modules: []app.Module{set, demoModule{}},
	})
	admin := stack.Bearer(t, "admin", perm.RoleAdmin)

	const put = `{"enabled":true,"host":"smtp.example.com","password":"hunter2"}`

	t.Run("保存后响应里没有明文，只报告已设置", func(t *testing.T) {
		body := mustStatus(t, req(t, stack, http.MethodPut, consolePrefix+"/settings/demo", put, admin),
			http.StatusOK)
		values, _ := body["values"].(map[string]any)
		if values["password"] != "" {
			t.Errorf("响应回传了口令：%v", values["password"])
		}
		if values["host"] != "smtp.example.com" {
			t.Errorf("同批提交的其他字段没有被保存：%v", values)
		}
		set, _ := body["secretSet"].([]any)
		if len(set) != 1 || set[0] != "password" {
			t.Errorf("secretSet = %v，期望 [password]——界面靠它区分「没存过」与「存过没显示」", set)
		}
	})

	t.Run("库里是密文", func(t *testing.T) {
		var raw string
		if err := db.DB.NewRaw("select values::text from settings where name = ?", "demo").
			Scan(ctx, &raw); err != nil {
			t.Fatalf("读取原始行: %v", err)
		}
		// 这条断言是整个改动的全部意义：设置表会随备份、导出与日志流出。
		if strings.Contains(raw, "hunter2") {
			t.Errorf("库里的口令是明文：%s", raw)
		}
		if !strings.Contains(raw, "enc:v1:") {
			t.Errorf("库里的口令没有密文前缀：%s", raw)
		}
	})

	t.Run("主密钥按需生成在工作目录", func(t *testing.T) {
		// 有口令字段的分组才去加载密钥文件：没配口令的站点不该在磁盘上
		// 多出一个「要不要跟 data/ 一起备份」的文件。
		if _, err := os.Stat(filepath.Join(cfg.DataDir, secret.KeyFileName)); err != nil {
			t.Errorf("主密钥文件没有生成: %v", err)
		}
	})

	t.Run("重新读取仍是空串，secretSet 仍在", func(t *testing.T) {
		body := mustStatus(t, req(t, stack, http.MethodGet, consolePrefix+"/settings/demo", "", admin),
			http.StatusOK)
		values, _ := body["values"].(map[string]any)
		if values["password"] != "" {
			t.Errorf("重新读取回传了口令：%v", values["password"])
		}
		set, _ := body["secretSet"].([]any)
		if len(set) != 1 || set[0] != "password" {
			t.Errorf("secretSet = %v，期望 [password]", set)
		}
	})

	t.Run("留空提交不改动已存的口令", func(t *testing.T) {
		mustStatus(t, req(t, stack, http.MethodPut, consolePrefix+"/settings/demo",
			`{"enabled":true,"host":"smtp.other.com","password":""}`, admin), http.StatusOK)
		body := mustStatus(t, req(t, stack, http.MethodGet, consolePrefix+"/settings/demo", "", admin),
			http.StatusOK)
		if set, _ := body["secretSet"].([]any); len(set) != 1 {
			t.Errorf("留空提交把已存的口令抹掉了：secretSet = %v", set)
		}
		values, _ := body["values"].(map[string]any)
		if values["host"] != "smtp.other.com" {
			t.Errorf("同批提交的其他字段没有被保存：%v", values)
		}
	})

	t.Run("提交 null 才清除", func(t *testing.T) {
		mustStatus(t, req(t, stack, http.MethodPut, consolePrefix+"/settings/demo",
			`{"password":null}`, admin), http.StatusOK)
		body := mustStatus(t, req(t, stack, http.MethodGet, consolePrefix+"/settings/demo", "", admin),
			http.StatusOK)
		if set, _ := body["secretSet"].([]any); len(set) != 0 {
			t.Errorf("清除之后 secretSet 应为空，实际 %v", set)
		}
	})

	t.Run("Public 平面读不到口令", func(t *testing.T) {
		mustStatus(t, req(t, stack, http.MethodPut, consolePrefix+"/settings/demo", put, admin), http.StatusOK)
		body := mustStatus(t, req(t, stack, http.MethodGet, publicPrefix+"/settings", "", ""), http.StatusOK)
		demo, _ := body["demo"].(map[string]any)
		if _, leaked := demo["password"]; leaked {
			t.Errorf("口令出现在 Public 平面：%v", demo)
		}
		if demo["enabled"] != true {
			t.Errorf("公开字段没跟着走：%v", demo)
		}
	})
}
