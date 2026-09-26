package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// 图标名的契约：后端、内置主题、示例插件与插件文档里写到的图标名，都得是后台
// console/src/lib/icons.ts 登记过的。两侧之间没有编译期约束，写错了界面只会悄悄显示成方块。
func TestIconNamesAreRegistered(t *testing.T) {
	root := filepath.Join("..", "..")
	registered := registeredIcons(t, root)

	type use struct{ name, where string }
	var uses []use
	scan := func(dir string, exts []string, pattern *regexp.Regexp) {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !slices.Contains(exts, filepath.Ext(path)) || strings.HasSuffix(path, "_test.go") {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, m := range pattern.FindAllStringSubmatch(string(data), -1) {
				uses = append(uses, use{m[1], path})
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	goIcon := regexp.MustCompile(`Icon:\s+"([a-z0-9-]+)"`)
	yamlIcon := regexp.MustCompile(`(?m)^\s*icon:\s*([a-z0-9-]+)\s*$`)
	scan("internal", []string{".go"}, goIcon)
	scan(filepath.Join("internal", "theme", "builtin"), []string{".yaml"}, yamlIcon)
	scan("examples", []string{".yaml"}, yamlIcon)
	scan("docs", []string{".md"}, yamlIcon)
	if len(uses) == 0 {
		t.Fatal("一个图标名都没扫到，扫描规则该跟着代码改了")
	}
	for _, u := range uses {
		if !registered[u.name] {
			t.Errorf("%s 用了没登记的图标 %q", u.where, u.name)
		}
	}

	// 插件文档里列出的可用图标要与登记表一致，多了少了都不行
	doc, err := os.ReadFile(filepath.Join(root, "docs", "plugin-development.md"))
	if err != nil {
		t.Fatal(err)
	}
	listed := regexp.MustCompile(`(?s)<!-- icons:start -->(.*?)<!-- icons:end -->`).FindSubmatch(doc)
	if listed == nil {
		t.Fatal("插件文档里找不到 icons:start / icons:end 标记")
	}
	var names []string
	for _, m := range regexp.MustCompile("`([a-z0-9-]+)`").FindAllSubmatch(listed[1], -1) {
		names = append(names, string(m[1]))
	}
	var want []string
	for name := range registered {
		want = append(want, name)
	}
	slices.Sort(names)
	slices.Sort(want)
	if !slices.Equal(names, want) {
		t.Errorf("插件文档列出的图标与 icons.ts 不一致：\n文档 %v\n登记 %v", names, want)
	}
}

// registeredIcons 读出 icons.ts 里 ICONS 登记表的键。
func registeredIcons(t *testing.T, root string) map[string]bool {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "console", "src", "lib", "icons.ts"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	start := strings.Index(src, "export const ICONS")
	end := strings.Index(src[start:], "};")
	if start < 0 || end < 0 {
		t.Fatal("icons.ts 里找不到 ICONS 登记表")
	}
	out := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^\s+"?([a-z0-9-]+)"?:\s`).FindAllStringSubmatch(src[start:start+end], -1) {
		out[m[1]] = true
	}
	if len(out) < 10 {
		t.Fatalf("只从 icons.ts 读出 %d 个图标，解析规则该跟着文件改了", len(out))
	}
	return out
}
