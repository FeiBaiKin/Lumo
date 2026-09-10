package media

import (
	"encoding/json"
	"errors"
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

	var defaults StorageSettings
	if err := json.Unmarshal(group.Defaults, &defaults); err != nil {
		t.Fatalf("缺省值不是合法 JSON: %v", err)
	}
	if defaults.Driver != DriverLocal {
		t.Errorf("默认驱动应为 %s，实际 %s", DriverLocal, defaults.Driver)
	}
}

// TestStorageSchemaHasNoSecretFields 是一条硬性约束：
// 设置会随备份、日志与接口响应流出，长期有效的对象存储密钥绝不能出现在这里。
func TestStorageSchemaHasNoSecretFields(t *testing.T) {
	t.Parallel()

	schema := strings.ToLower(storageSchema)
	for _, word := range []string{"secret", "accesskey", "password", "credential", "token"} {
		if strings.Contains(schema, word) {
			t.Errorf("storage 分组的 Schema 不应出现 %q 一类的密钥字段", word)
		}
	}
}

func TestCheckStorage(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		values   map[string]any
		wantKeys []string
	}{
		{
			name:   "本地存储无需 S3 参数",
			values: map[string]any{"driver": "local"},
		},
		{
			name:     "选 S3 但缺地址与桶",
			values:   map[string]any{"driver": "s3"},
			wantKeys: []string{"body.s3Endpoint", "body.s3Bucket"},
		},
		{
			name: "S3 参数齐全",
			values: map[string]any{
				"driver": "s3", "s3Endpoint": "https://s3.example.com", "s3Bucket": "assets",
			},
		},
		{
			name: "公开前缀不是绝对地址",
			values: map[string]any{
				"driver": "local", "s3PublicUrl": "cdn.example.com",
			},
			wantKeys: []string{"body.s3PublicUrl"},
		},
	}

	for _, c := range cases {
		err := checkStorage(c.values)
		if len(c.wantKeys) == 0 {
			if err != nil {
				t.Errorf("%s：不应报错，实际 %v", c.name, err)
			}
			continue
		}
		var verr *settings.ValidationError
		if !errors.As(err, &verr) {
			t.Errorf("%s：应返回 ValidationError，实际 %v", c.name, err)
			continue
		}
		got := make(map[string]bool, len(verr.Details))
		for _, d := range verr.Details {
			got[d.Location] = true
		}
		for _, key := range c.wantKeys {
			if !got[key] {
				t.Errorf("%s：应定位到 %s，实际明细 %v", c.name, key, verr.Details)
			}
		}
	}
}
