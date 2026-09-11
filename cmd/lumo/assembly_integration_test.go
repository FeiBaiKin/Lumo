package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/FeiBaiKin/lumo/internal/api"
	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/config"
	"github.com/FeiBaiKin/lumo/internal/migrate"
	"github.com/FeiBaiKin/lumo/internal/testsupport"
)

// TestFullAssemblyOpenAPI 把 modules() 里的全部模块装到一起，再生成一次 OpenAPI。
//
// 各模块的集成测试只装配自己用得到的那几个，测不到「整机才出现」的冲突：
// huma 的 Schema 注册表按**类型名跨模块索引**，两个包里各有一个 createBody
// 就会在注册时 panic，而这在单模块测试里永远碰不到——只有 serve 启动那一刻才炸。
// 阶段 5 接入 Extension 模块时就真炸过一次，故补这条整机装配的回归测试。
func TestFullAssemblyOpenAPI(t *testing.T) {
	assembled := modules()

	sources := make([]migrate.Source, 0, len(assembled))
	for _, m := range assembled {
		migrator, ok := m.(app.Migrator)
		if !ok {
			continue
		}
		sources = append(sources, migrate.Source{Name: m.Name(), FS: migrator.Migrations()})
	}

	db := testsupport.Open(t, testsupport.Options{
		Schema:  "lumo_it_assembly",
		Migrate: true,
		Sources: sources,
	})

	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	// 装配本身就是被测对象：Register 里的名字冲突会直接 panic，测试因此失败。
	stack := testsupport.NewStackWith(t, db, &testsupport.StackOptions{
		Config:  cfg,
		Modules: assembled,
	})

	rec := stack.Do(t, &testsupport.Request{Method: http.MethodGet, Path: api.OpenAPIPath + ".json"})
	if rec.Code != http.StatusOK {
		t.Fatalf("取 OpenAPI 规范失败：%d %s", rec.Code, rec.Body.String())
	}

	var spec struct {
		OpenAPI string `json:"openapi"`
		Paths   map[string]map[string]struct {
			OperationID string `json:"operationId"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &spec); err != nil {
		t.Fatalf("规范不是合法 JSON: %v", err)
	}
	if !strings.HasPrefix(spec.OpenAPI, "3.1") {
		t.Errorf("OpenAPI 版本 = %q，期望 3.1.x（agent.md §6）", spec.OpenAPI)
	}

	// operationId 是 Console TS 客户端的方法名来源，重名会让生成出来的客户端少一个方法。
	seen := make(map[string]string, len(spec.Paths))
	planes := map[string]int{}
	for path, item := range spec.Paths {
		for method, op := range item {
			if op.OperationID == "" {
				t.Errorf("%s %s 没有 operationId", strings.ToUpper(method), path)
				continue
			}
			if prev, dup := seen[op.OperationID]; dup {
				t.Errorf("operationId %q 同时出现在 %s 与 %s", op.OperationID, prev, path)
			}
			seen[op.OperationID] = path
		}
		switch {
		case strings.HasPrefix(path, api.PrefixConsole):
			planes["console"]++
		case strings.HasPrefix(path, api.PrefixPublic):
			planes["public"]++
		case strings.HasPrefix(path, api.PrefixExtension+"/"):
			planes["extension"]++
		}
	}

	// 三个平面都得有路径；Extension 平面自阶段 5 起由 internal/extension 占用。
	for _, plane := range []string{"console", "public", "extension"} {
		if planes[plane] == 0 {
			t.Errorf("%s 平面没有任何路径", plane)
		}
	}
}
