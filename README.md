# Lumo

用 Go 编写的现代化开源 CMS，单一静态二进制，自带后台与主题系统。

> **开发中** — 当前处于阶段 0（脚手架），尚不可用于生产。

## 特性规划

- **主题生态**：主题为 zip 包，后台上传即切换，服务端 `html/template` 渲染
- **双编辑器**：块编辑器（TipTap）与 Markdown 编辑器（Milkdown），内容存规范 HTML，不绑定编辑器
- **内容管理**：文章、独立页面、树形分类、标签、评论、附件、菜单
- **REST API**：Console / Public / Extension 三平面，OpenAPI 3.1 规范由代码生成
- **全文搜索**：Go 侧分词 + PostgreSQL `tsvector`，不依赖数据库扩展
- **单一二进制**：`CGO_ENABLED=0`，Console 前端 `go:embed` 进二进制

## 环境要求

| 项 | 版本 |
|---|---|
| Go | 1.26+ |
| Node.js | 24+ |
| PostgreSQL | 17+ |

开发还需 [Task](https://taskfile.dev)：`go install github.com/go-task/task/v3/cmd/task@latest`

## 快速开始

```bash
git clone https://github.com/FeiBaiKin/lumo.git
cd lumo

# 全量构建：Console 前端 + 后端二进制
task all

# 启动
./lumo serve
```

访问 http://127.0.0.1:8080 ，会自动跳转到 `/console/`。

## 开发

```bash
task              # 列出所有任务
task run -- serve # 直接运行后端（不构建前端）
task console:dev  # 另开终端跑 Console 开发服务器（HMR，代理到 :8080）
task check        # 提交前自检：格式化 + vet + lint + 测试
```

前端未构建时后端仍可启动，`/console/` 会返回构建提示，不影响 API 开发。

## 命令

```
lumo serve                  启动 HTTP 服务
lumo migrate                执行数据库迁移
lumo admin reset-password   重置管理员密码
lumo version                输出版本信息
```

`serve` 支持 `-addr`（默认 `:8080`，环境变量 `LUMO_ADDR`）与 `-no-migrate`。

## 项目结构

```
cmd/lumo/          CLI 入口
internal/          核心实现（含 console/dist 嵌入目标）
migrations/        goose SQL 迁移
console/           Vite + React + TypeScript 后台前端
themes/default/    内置默认主题
deploy/            Dockerfile、docker-compose.yml
```

## 许可证

[GPL-3.0](./LICENSE)
