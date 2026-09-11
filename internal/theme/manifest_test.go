package theme

import (
	"errors"
	"strings"
	"testing"
)

// TestParseManifest 验证元信息的解析与校验。
func TestParseManifest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{name: "最小合法", yaml: "name: demo\nversion: 1.0.0\n"},
		{name: "完整", yaml: "name: demo\nlabel: 演示\nversion: 1.2.3-beta.1\n" +
			"description: 说明\nauthor: 某人\nhomepage: https://x.com\nlicense: MIT\nrequireLumo: 0.1\n"},
		{name: "缺少 name", yaml: "version: 1.0.0\n", wantErr: "name"},
		{name: "大写 name", yaml: "name: Demo\nversion: 1.0.0\n", wantErr: "name"},
		{name: "下划线 name", yaml: "name: my_theme\nversion: 1.0.0\n", wantErr: "name"},
		{name: "连字符开头", yaml: "name: -demo\nversion: 1.0.0\n", wantErr: "name"},
		{name: "缺少 version", yaml: "name: demo\n", wantErr: "version"},
		{name: "非法 version", yaml: "name: demo\nversion: 甲乙丙\n", wantErr: "version"},
		{name: "非法 requireLumo", yaml: "name: demo\nversion: 1.0.0\nrequireLumo: 最新\n", wantErr: "requireLumo"},
		// 未知字段必须报错：把 label 拼成 lable 时静默忽略会让作者找半天。
		{name: "未知字段", yaml: "name: demo\nversion: 1.0.0\nlable: 错拼\n", wantErr: "解析"},
		{name: "label 超长", yaml: "name: demo\nversion: 1.0.0\nlabel: " + strings.Repeat("字", 65) + "\n",
			wantErr: "label"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, err := parseManifest([]byte(tt.yaml))
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("不应报错: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("期望报错，实际解析成功: %+v", m)
			}
			if !errors.Is(err, ErrInvalidPackage) {
				t.Errorf("错误应包装 ErrInvalidPackage，实际 %v", err)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("错误应含 %q，实际 %v", tt.wantErr, err)
			}
		})
	}
}

// TestParseManifestDefaultsLabel 验证省略 label 时回退到 name。
func TestParseManifestDefaultsLabel(t *testing.T) {
	t.Parallel()

	m, err := parseManifest([]byte("name: demo\nversion: 1.0.0\n"))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if m.Label != "demo" {
		t.Errorf("label = %q，期望回退到 name", m.Label)
	}
}

// TestParseSettings 验证主题设置声明的解析。
func TestParseSettings(t *testing.T) {
	t.Parallel()

	const good = `
groups:
  - name: appearance
    label: 外观
    order: 0
    schema:
      type: object
      properties:
        accent:
          type: string
    defaults:
      accent: "#000"
`
	decl, err := parseSettings([]byte(good))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(decl.Groups) != 1 || decl.Groups[0].Name != "appearance" {
		t.Fatalf("分组 = %+v", decl.Groups)
	}

	bad := map[string]string{
		"非法分组名":     "groups:\n  - name: Bad_Name\n    schema:\n      type: object\n",
		"缺少 schema": "groups:\n  - name: ok\n",
		"分组重复": "groups:\n  - name: a\n    schema:\n      type: object\n" +
			"  - name: a\n    schema:\n      type: object\n",
	}
	for name, yaml := range bad {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := parseSettings([]byte(yaml)); !errors.Is(err, ErrInvalidPackage) {
				t.Fatalf("错误 = %v，期望 ErrInvalidPackage", err)
			}
		})
	}
}

// TestCompileSettingsValidatesDefaults 验证缺省值必须通过自身 Schema。
//
// 作者把 defaults 写得不符合自己的 schema 时，站长打开设置页会看到一堆
// 无法保存的初始值——这种错必须在安装时暴露。
func TestCompileSettingsValidatesDefaults(t *testing.T) {
	t.Parallel()

	const badDefaults = `
groups:
  - name: appearance
    schema:
      type: object
      properties:
        size:
          type: integer
          minimum: 10
    defaults:
      size: 1
`
	decl, err := parseSettings([]byte(badDefaults))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if _, err := compileSettings("demo", decl); !errors.Is(err, ErrInvalidPackage) {
		t.Fatalf("错误 = %v，期望 ErrInvalidPackage", err)
	}
}

// TestCompileSettingsOrdersGroups 验证分组按 Order 再按 Name 排序。
func TestCompileSettingsOrdersGroups(t *testing.T) {
	t.Parallel()

	const yaml = `
groups:
  - name: zzz
    order: 0
    schema: {type: object}
  - name: aaa
    order: 0
    schema: {type: object}
  - name: first
    order: -5
    schema: {type: object}
`
	decl, err := parseSettings([]byte(yaml))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	compiled, err := compileSettings("demo", decl)
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	want := []string{"first", "aaa", "zzz"}
	for i, name := range want {
		if compiled.groups[i].Name != name {
			t.Errorf("第 %d 个分组 = %q，期望 %q", i, compiled.groups[i].Name, name)
		}
	}
}

// TestIntegerSettingsStayInt 验证 integer 字段在 JSON 往返后仍是 int。
//
// 回归测试：设置值经 JSON 存取会把整数变成 float64，模板里把它当整数用
// （如 {{ .Find.Posts.Related $id .Theme.Settings.content.relatedCount }}）
// 会报「expected int; got float64」——这是主题作者无从预料也无从修复的坑。
func TestIntegerSettingsStayInt(t *testing.T) {
	t.Parallel()

	const yaml = `
groups:
  - name: content
    schema:
      type: object
      properties:
        relatedCount: {type: integer}
        ratio: {type: number}
        title: {type: string}
    defaults:
      relatedCount: 3
      ratio: 1.5
      title: 标题
`
	decl, err := parseSettings([]byte(yaml))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	compiled, err := compileSettings("demo", decl)
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}

	// 缺省值路径。
	if got := compiled.effectiveSettings(nil)["content"]["relatedCount"]; !isInt(got) {
		t.Errorf("缺省值 relatedCount 类型 = %T，期望 int", got)
	}
	// 已保存值路径：模拟从库中读出的 float64。
	stored := map[string]map[string]any{"content": {"relatedCount": float64(7)}}
	effective := compiled.effectiveSettings(stored)
	got := effective["content"]["relatedCount"]
	if !isInt(got) {
		t.Errorf("已保存 relatedCount 类型 = %T，期望 int", got)
	}
	if got != 7 {
		t.Errorf("relatedCount = %v，期望 7", got)
	}
	// number 不该被改成 int，否则 1.5 会变成 1。
	if ratio := effective["content"]["ratio"]; ratio != 1.5 {
		t.Errorf("ratio = %v（%T），期望保持 1.5", ratio, ratio)
	}
}

// isInt 报告值是否为 int。
func isInt(v any) bool {
	_, ok := v.(int)
	return ok
}

// TestEffectiveSettingsMerge 验证已保存值按顶层键覆盖缺省值。
func TestEffectiveSettingsMerge(t *testing.T) {
	t.Parallel()

	const yaml = `
groups:
  - name: appearance
    schema:
      type: object
      properties:
        a: {type: string}
        b: {type: string}
    defaults:
      a: "默认A"
      b: "默认B"
`
	decl, err := parseSettings([]byte(yaml))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	compiled, err := compileSettings("demo", decl)
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}

	effective := compiled.effectiveSettings(map[string]map[string]any{
		"appearance": {"a": "已存A"},
	})
	if got := effective["appearance"]["a"]; got != "已存A" {
		t.Errorf("a = %v，期望已保存值覆盖", got)
	}
	if got := effective["appearance"]["b"]; got != "默认B" {
		t.Errorf("b = %v，期望保留缺省值", got)
	}

	// 未保存过时全部取缺省值。
	fresh := compiled.effectiveSettings(nil)
	if got := fresh["appearance"]["a"]; got != "默认A" {
		t.Errorf("未保存时 a = %v，期望缺省值", got)
	}
}
