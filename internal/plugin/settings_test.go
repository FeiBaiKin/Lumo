package plugin

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writePluginDir 落一个最小的插件目录到磁盘，供只关心设置声明的用例使用。
func writePluginDir(t *testing.T, settings string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileManifest), []byte(manifestYAML("demo")), 0o640); err != nil {
		t.Fatal(err)
	}
	if settings != "" {
		if err := os.WriteFile(filepath.Join(dir, FileSettings), []byte(settings), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const settingsYAML = `
groups:
  - name: sync
    label: 同步
    description: 同步相关的设置
    order: 10
    icon: refresh
    schema:
      type: object
      additionalProperties: false
      properties:
        enabled:
          type: boolean
          title: 启用自动同步
        interval:
          type: integer
          title: 同步间隔
          minimum: 5
          maximum: 1440
          x-show-if:
            field: enabled
            op: eq
            value: true
    defaults:
      enabled: false
      interval: 30
`

func TestLoadSettingsParsesGroups(t *testing.T) {
	t.Parallel()

	dir := writePluginDir(t, settingsYAML)
	groups, err := loadSettings(os.DirFS(dir))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("分组数 = %d", len(groups))
	}
	group := groups[0]
	if group.Name != "sync" || group.Label != "同步" || group.Icon != "refresh" {
		t.Errorf("分组 = %+v", group)
	}
	if group.validator == nil {
		t.Fatal("分组没有携带校验器")
	}

	// 条件依赖必须与站点设置共用同一套语义：条件不成立时那一项不显示、不参与必填。
	if group.Form.Visible("interval", map[string]any{"enabled": false}) {
		t.Error("enabled=false 时 interval 不该显示")
	}
	if !group.Form.Visible("interval", map[string]any{"enabled": true}) {
		t.Error("enabled=true 时 interval 应当显示")
	}
}

// TestLoadSettingsIsOptional 确认没有 settings.yaml 时不算错。
//
// 绝大多数插件没有设置项，强制放一个空文件只会制造噪音。
func TestLoadSettingsIsOptional(t *testing.T) {
	t.Parallel()

	groups, err := loadSettings(os.DirFS(writePluginDir(t, "")))
	if err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	if len(groups) != 0 {
		t.Errorf("分组 = %v，期望空", groups)
	}
}

func TestLoadSettingsRejectsBadDeclarations(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		settings string
		want     string
	}{
		{
			name: "分组名非法",
			settings: `groups:
  - name: Bad Name
    schema: {type: object, properties: {}}
    defaults: {}`,
			want: "非法",
		},
		{
			name: "缺少 schema",
			settings: `groups:
  - name: sync
    defaults: {}`,
			want: "缺少 schema",
		},
		{
			// 缺省值必须自洽：否则站长打开设置页会看到一堆存不进去的初始值
			name: "缺省值不符合自己的 schema",
			settings: `groups:
  - name: sync
    schema:
      type: object
      properties:
        interval: {type: integer, title: 间隔}
    defaults: {interval: 很久}`,
			want: "未通过自身 schema",
		},
		{
			name: "分组重复",
			settings: `groups:
  - name: sync
    schema: {type: object, properties: {}}
    defaults: {}
  - name: sync
    schema: {type: object, properties: {}}
    defaults: {}`,
			want: "重复",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := loadSettings(os.DirFS(writePluginDir(t, tc.settings)))
			if err == nil {
				t.Fatal("期望报错")
			}
			if !errors.Is(err, ErrInvalidPackage) {
				t.Errorf("错误类别 = %v", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("错误信息里没有 %q：%v", tc.want, err)
			}
		})
	}
}
