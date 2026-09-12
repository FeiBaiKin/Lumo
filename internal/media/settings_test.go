package media

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/settings"
)

// TestStorageGroupDefaultsPassOwnSchema 保证分组声明本身是自洽的：
// 缺省值不合法会让整个应用启动失败，必须在单测里就暴露。
func TestStorageGroupDefaultsPassOwnSchema(t *testing.T) {
	t.Parallel()

	group := storageGroup()
	svc := settings.NewService(nil)
	if err := svc.RegisterGroups([]app.SettingGroup{group}); err != nil {
		t.Fatalf("storage 分组应能注册: %v", err)
	}

	raw, err := json.Marshal(group.Form.DefaultValues())
	if err != nil {
		t.Fatalf("缺省值无法序列化: %v", err)
	}
	var defaults StorageSettings
	if err := json.Unmarshal(raw, &defaults); err != nil {
		t.Fatalf("缺省值与 StorageSettings 对不上: %v", err)
	}
	if defaults.Driver != DriverLocal {
		t.Errorf("默认驱动应为 %s，实际 %s", DriverLocal, defaults.Driver)
	}
}

// TestStorageSchemaHasNoSecretFields 是一条硬性约束：
// 设置会随备份、日志与接口响应流出，长期有效的对象存储密钥绝不能出现在这里。
//
// 检查的是编译出的整份 Schema（含每个字段的标题与说明文字），
// 因此「顺手加一个密钥输入框」和「在说明里提示填密钥」都会被抓到。
func TestStorageSchemaHasNoSecretFields(t *testing.T) {
	t.Parallel()

	raw, err := json.Marshal(storageForm.Doc())
	if err != nil {
		t.Fatalf("Schema 无法序列化: %v", err)
	}
	schema := strings.ToLower(string(raw))
	for _, word := range []string{"secret", "accesskey", "password", "credential"} {
		if strings.Contains(schema, word) {
			t.Errorf("storage 表单不应出现 %q 一类的密钥字段", word)
		}
	}
}

// TestStorageS3FieldsAreConditional 确认六个 S3 字段只在选了 S3 时出现，
// 且「选了 S3 就必须填地址与桶名」的规则成立。
//
// 这条规则此前写在 checkStorage 里，前端对它一无所知——页面上没要求，
// 保存时才说必填。现在它是字段声明的 Required + ShowIf，两边读的是同一句话。
func TestStorageS3FieldsAreConditional(t *testing.T) {
	t.Parallel()

	s3Fields := []string{"s3Endpoint", "s3Bucket", "s3Region", "s3PathStyle", "s3UseSsl", "s3PublicUrl"}
	local := map[string]any{"driver": DriverLocal}
	for _, key := range s3Fields {
		if storageForm.Visible(key, local) {
			t.Errorf("driver=%s 时 %s 不该显示", DriverLocal, key)
		}
	}
	if got := storageForm.Missing(local); len(got) != 0 {
		t.Errorf("本地存储不该追究 S3 参数，实际 %v", got)
	}

	onS3 := map[string]any{"driver": DriverS3}
	for _, key := range s3Fields {
		if !storageForm.Visible(key, onS3) {
			t.Errorf("driver=%s 时 %s 应当显示", DriverS3, key)
		}
	}
	got := storageForm.Missing(onS3)
	slices.Sort(got)
	if !slices.Equal(got, []string{"s3Bucket", "s3Endpoint"}) {
		t.Errorf("选 S3 且未填地址与桶名时，Missing = %v", got)
	}
}

// TestCheckStorage 只覆盖 Schema 与条件依赖都表达不了的校验——
// 「必填」那一条已经归声明管，见 TestStorageS3FieldsAreConditional。
func TestCheckStorage(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		values   map[string]any
		wantKeys []string
	}{
		{
			name:   "本地存储且无公开前缀",
			values: map[string]any{"driver": DriverLocal},
		},
		{
			name: "S3 参数齐全",
			values: map[string]any{
				"driver": DriverS3, "s3Endpoint": "https://s3.example.com", "s3Bucket": "assets",
			},
		},
		{
			name: "公开前缀是绝对地址",
			values: map[string]any{
				"driver": DriverLocal, "s3PublicUrl": "https://cdn.example.com",
			},
		},
		{
			name: "公开前缀不是绝对地址",
			values: map[string]any{
				"driver": DriverLocal, "s3PublicUrl": "cdn.example.com",
			},
			wantKeys: []string{"body.s3PublicUrl"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			err := checkStorage(c.values)
			if len(c.wantKeys) == 0 {
				if err != nil {
					t.Errorf("不应报错，实际 %v", err)
				}
				return
			}
			var verr *settings.ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("应返回 ValidationError，实际 %v", err)
			}
			got := make(map[string]bool, len(verr.Details))
			for _, d := range verr.Details {
				got[d.Location] = true
			}
			for _, key := range c.wantKeys {
				if !got[key] {
					t.Errorf("应定位到 %s，实际明细 %v", key, verr.Details)
				}
			}
		})
	}
}
