package settings

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/form"
	"github.com/FeiBaiKin/lumo/internal/secret"
)

// memStore 是内存版的设置存储，同时留一份「库里到底长什么样」的原始值，
// 供测试断言密文——这是本组测试的重点，而它只能从写下去的那一刻看。
type memStore struct {
	mu   sync.Mutex
	data map[string]map[string]any
}

func newMemStore() *memStore { return &memStore{data: map[string]map[string]any{}} }

func (m *memStore) Load(_ context.Context, name string) (map[string]any, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.data[name]
	if !ok {
		return map[string]any{}, nil
	}
	out := make(map[string]any, len(row))
	for k, v := range row {
		out[k] = v
	}
	return out, nil
}

func (m *memStore) Save(_ context.Context, name string, values map[string]any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	row := make(map[string]any, len(values))
	for k, v := range values {
		row[k] = v
	}
	m.data[name] = row
	return nil
}

// raw 返回库里那一行的原始值，测试用它确认落库的是密文而不是明文。
func (m *memStore) raw(name string) map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.data[name]
}

func testKeyring(t *testing.T, fill byte) *secret.Keyring {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = fill
	}
	k, err := secret.New(key)
	if err != nil {
		t.Fatalf("secret.New: %v", err)
	}
	return k
}

// secretGroup 是一份带口令字段的演示分组，形状与 mail / storage 一致。
func secretGroup(public []string) app.SettingGroup {
	f := form.New(
		form.NewSection("发信",
			form.Bool("enabled").Label("启用").Default(true),
			form.Text("host").Label("服务器").Required().Default("smtp.example.com"),
			form.Secret("password").Label("口令").MaxLen(512).Default("").
				ShowIf(form.Eq("enabled", true)),
		),
	).Named("demo")
	return app.SettingGroup{Name: "demo", Label: "演示", Form: f, Public: public}
}

func newSecretService(t *testing.T, store ValueStore, k *secret.Keyring) *Service {
	t.Helper()
	svc := NewService(store)
	svc.SetKeyring(k)
	if err := svc.RegisterGroups([]app.SettingGroup{secretGroup(nil)}); err != nil {
		t.Fatalf("注册分组: %v", err)
	}
	return svc
}

func TestSecretIsEncryptedAtRestAndMaskedOnRead(t *testing.T) {
	ctx := context.Background()
	store := newMemStore()
	svc := newSecretService(t, store, testKeyring(t, 7))

	if err := svc.Update(ctx, "demo", map[string]any{
		"enabled": true, "host": "smtp.example.com", "password": "hunter2",
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	// 落库的必须是密文。这条是整个改动的全部意义所在：设置表会随备份、
	// 导出与日志流出，明文口令躺在这里等于把邮箱主密码一起送出去。
	stored, _ := store.raw("demo")["password"].(string)
	if strings.Contains(stored, "hunter2") {
		t.Errorf("库里存的是明文：%q", stored)
	}
	if !secret.Encrypted(stored) {
		t.Errorf("库里的口令没有 enc: 前缀，读的时候会被当成明文：%q", stored)
	}

	// 而真正要用它的人（mail 的发信器）读到的仍是明文。
	effective, err := svc.Effective(ctx, "demo")
	if err != nil {
		t.Fatalf("Effective: %v", err)
	}
	if effective["password"] != "hunter2" {
		t.Errorf("有效值里的口令 = %v，期望明文 hunter2", effective["password"])
	}

	// 出接口的那一份则是空的，另由 secretSet 说明「确实设过」。
	g, _ := svc.Group("demo")
	masked, set := g.Mask(effective)
	if masked["password"] != "" {
		t.Errorf("接口视图里的口令应为空串，实际 %v", masked["password"])
	}
	if !slices.Equal(set, []string{"password"}) {
		t.Errorf("secretSet = %v，期望 [password]", set)
	}
	// 抹掉的只有口令，别的字段原样。
	if masked["host"] != "smtp.example.com" {
		t.Errorf("非口令字段被一起抹掉了：%v", masked["host"])
	}
}

func TestSecretEmptyOrAbsentKeepsStoredValue(t *testing.T) {
	ctx := context.Background()
	store := newMemStore()
	svc := newSecretService(t, store, testKeyring(t, 7))
	base := map[string]any{"enabled": true, "host": "smtp.example.com", "password": "hunter2"}
	if err := svc.Update(ctx, "demo", base); err != nil {
		t.Fatalf("Update: %v", err)
	}

	// 表单上那个口令框本来就是空的，把空当成「清空」会让每次改别的字段
	// 都顺手抹掉口令——那种故障要等到下一次发信失败才被发现。
	for name, submit := range map[string]map[string]any{
		"留空":  {"enabled": true, "host": "smtp.other.com", "password": ""},
		"不提交": {"enabled": true, "host": "smtp.other.com"},
	} {
		if err := svc.Update(ctx, "demo", submit); err != nil {
			t.Fatalf("%s 时 Update: %v", name, err)
		}
		effective, err := svc.Effective(ctx, "demo")
		if err != nil {
			t.Fatalf("Effective: %v", err)
		}
		if effective["password"] != "hunter2" {
			t.Errorf("%s 提交之后口令 = %v，应当保持原值", name, effective["password"])
		}
		if effective["host"] != "smtp.other.com" {
			t.Errorf("%s 提交的同批字段没有被保存：host = %v", name, effective["host"])
		}
	}
}

func TestSecretNullClears(t *testing.T) {
	ctx := context.Background()
	store := newMemStore()
	svc := newSecretService(t, store, testKeyring(t, 7))
	if err := svc.Update(ctx, "demo", map[string]any{"password": "hunter2"}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	// JSON null 是唯一的清除方式。
	if err := svc.Update(ctx, "demo", map[string]any{"password": nil}); err != nil {
		t.Fatalf("清除时 Update: %v", err)
	}
	effective, err := svc.Effective(ctx, "demo")
	if err != nil {
		t.Fatalf("Effective: %v", err)
	}
	if effective["password"] != "" {
		t.Errorf("清除之后口令 = %v，应当为空", effective["password"])
	}
	if stored, _ := store.raw("demo")["password"].(string); stored != "" {
		t.Errorf("清除之后库里仍有残留：%q", stored)
	}
}

func TestSecretCannotBePublic(t *testing.T) {
	// 口令字段出现在 Public 白名单里，前台就能读到它。这是配置事故里
	// 最容易发生也最不该发生的一种，必须在注册这一刻拦下，而不是等它上线。
	svc := NewService(newMemStore())
	svc.SetKeyring(testKeyring(t, 7))
	err := svc.RegisterGroups([]app.SettingGroup{secretGroup([]string{"enabled", "password"})})
	if err == nil {
		t.Fatal("把口令字段列进 Public 应当注册失败")
	}
	if !strings.Contains(err.Error(), "password") {
		t.Errorf("错误信息没点名是哪个字段：%v", err)
	}
}

func TestSecretRefusesPlaintextWithoutKeyring(t *testing.T) {
	// 没有主密钥时宁可不写：悄悄退化成明文入库，就会得到一份
	// 「看起来配好了、实际谁都能读」的备份。
	svc := NewService(newMemStore())
	if err := svc.RegisterGroups([]app.SettingGroup{secretGroup(nil)}); err != nil {
		t.Fatalf("注册分组: %v", err)
	}
	if err := svc.Update(context.Background(), "demo", map[string]any{"password": "hunter2"}); err == nil {
		t.Fatal("没有主密钥时写口令应当报错")
	}
}

func TestSecretUnreadableDegradesToEmpty(t *testing.T) {
	ctx := context.Background()
	store := newMemStore()
	if err := newSecretService(t, store, testKeyring(t, 7)).Update(ctx, "demo",
		map[string]any{"password": "hunter2"}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	// 换了一把密钥（密钥文件丢了、或换了台机器）。此时要能读到「没设置」，
	// 而不是一个错误——返回错误会让设置页整页 500，站长连重填的入口都没有。
	// 所以只记 warn，让他重新填一次即可。
	other := newSecretService(t, store, testKeyring(t, 9))
	effective, err := other.Effective(ctx, "demo")
	if err != nil {
		t.Fatalf("解不开口令不该让读取整体失败: %v", err)
	}
	if effective["password"] != "" {
		t.Errorf("解不开的口令应视为未设置，实际 %v", effective["password"])
	}

	// 重填之后就恢复正常。
	if err := other.Update(ctx, "demo", map[string]any{"password": "hunter3"}); err != nil {
		t.Fatalf("重填时 Update: %v", err)
	}
	if got, _ := other.Effective(ctx, "demo"); got["password"] != "hunter3" {
		t.Errorf("重填之后口令 = %v", got["password"])
	}
}

func TestHasSecrets(t *testing.T) {
	svc := NewService(newMemStore())
	if svc.HasSecrets() {
		t.Fatal("还没注册分组时不该报告有口令字段")
	}
	plain := app.SettingGroup{
		Name: "plain", Label: "无口令",
		Form: form.New(form.NewSection("一", form.Text("a").Label("甲").Default(""))),
	}
	if err := svc.RegisterGroups([]app.SettingGroup{plain}); err != nil {
		t.Fatalf("注册分组: %v", err)
	}
	// 模块据此决定要不要去加载主密钥，判错的代价是每个目录里都多出一个 secret.key。
	if svc.HasSecrets() {
		t.Error("没有口令字段的分组不该报告有口令字段")
	}
	if err := svc.RegisterGroups([]app.SettingGroup{secretGroup(nil)}); err != nil {
		t.Fatalf("注册分组: %v", err)
	}
	if !svc.HasSecrets() {
		t.Error("有口令字段的分组应当报告出来")
	}
}

func TestRejectSecrets(t *testing.T) {
	if err := RejectSecrets("主题 x 的设置", secretGroup(nil).Form); err == nil {
		t.Fatal("作用域设置里出现口令字段应当被拒")
	} else if !strings.Contains(err.Error(), "password") {
		t.Errorf("错误信息没点名是哪个字段：%v", err)
	}
	plain := form.New(form.NewSection("一", form.Text("a").Label("甲").Default("")))
	if err := RejectSecrets("主题 x 的设置", plain); err != nil {
		t.Errorf("没有口令字段时不该报错：%v", err)
	}
	if err := RejectSecrets("主题 x 的设置", nil); err != nil {
		t.Errorf("nil 表单不该报错：%v", err)
	}
}

func TestSecretRejectsNonString(t *testing.T) {
	svc := newSecretService(t, newMemStore(), testKeyring(t, 7))
	err := svc.Update(context.Background(), "demo", map[string]any{"password": 123})
	if err == nil {
		t.Fatal("数字口令应当被拒")
	}
	if !strings.Contains(fmt.Sprint(err), "文本") {
		t.Errorf("错误信息没说清原因：%v", err)
	}
}
