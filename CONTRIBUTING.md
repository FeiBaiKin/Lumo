# 参与贡献

感谢你愿意参与 Lumo。这份文档说明如何把开发环境跑起来，以及提交代码时需要遵守的约定。

## 先说三件事

1. **项目仍在开发中**，尚未正式发版。接口、数据结构与模块边界都还可能变动。
2. **动手前先开 Issue**。小修小补（错别字、明显 bug）直接提 PR 就好；
   涉及新功能、接口变更或架构调整的，请先开 Issue 对齐方向，避免白做。
3. **仓库当前没有自动化测试用例**。原有用例已于 2026-09-15 整体清空，
   `task test` 现在只是空跑。这意味着 CI 通过并不能证明你的改动是对的——
   请自行在本地把受影响的页面与接口走一遍。

## 环境

| 项 | 版本 |
|---|---|
| Go | 1.26+ |
| Node.js | 24+ |
| PostgreSQL | 17+ |

还需要 [Task](https://taskfile.dev) 与 [golangci-lint](https://golangci-lint.run)：

```bash
go install github.com/go-task/task/v3/cmd/task@latest
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
```

## 跑起来

```bash
git clone https://github.com/FeiBaiKin/Lumo.git
cd Lumo

# 单独建一个开发库，不要和别的项目共用
createdb lumo_dev
export LUMO_DATABASE_DSN="postgres://user:password@127.0.0.1:5432/lumo_dev?sslmode=disable"

# 全量构建：Console 前端 + 后端二进制
task all

# 启动（首次启动自动迁移）
./lumo serve

# 创建管理员（需在交互式终端运行，密码不回显）
./lumo admin create-user -username admin -email admin@example.com -role super-admin
```

后端在 8080，前台在 `/`，后台在 `/console/`。

改前端时用 `task console:dev` 起 Vite 开发服务器（带 HMR），它会把接口请求代理到后端；
后端仍需 `task run` 或 `./lumo serve` 单独跑着。

`task` 不带参数会列出全部任务。常用的几个：

| 命令 | 作用 |
|---|---|
| `task all` | 全量构建（前端 + 后端），发布前用这个 |
| `task build` | 只构建后端二进制 |
| `task run` | 本地运行后端 |
| `task check` | **提交前自检**：fmt + vet + golangci-lint + test |
| `task console:dev` | Console 开发服务器（HMR） |
| `task console:build` | 构建 Console 到 `internal/console/dist` |
| `task console:lint` | Console 检查：Biome + tsc |
| `task console:api` | 导出 OpenAPI 并重新生成 TS 客户端类型 |

## 提交前必须跑

```bash
task check && task console:lint
```

两条都要是绿的。CI 跑的是同一套检查（见 [.github/workflows/verify.yml](.github/workflows/verify.yml)）。

## 代码约定

### 通用

- **注释用中文**，且解释「为什么这么做」，不是复述代码在做什么。
  写下取舍的理由，尤其是当你排除了某个看起来更自然的做法时。
- **界面文案用中文**，不使用表情符号；图标统一用 Lucide。
- 提交前不要留 `TODO` / `FIXME`：要么做掉，要么开 Issue。

### Go

- 格式化用 `gofmt`（`task fmt`），lint 规则见 [.golangci.yml](.golangci.yml)。
- 错误用 `fmt.Errorf("...: %w", err)` 包装，错误信息用中文，不带大写开头和句号。
- **模块边界**：`internal/` 下每个业务模块自成一体，持有自己的迁移与版本表。
  模块之间不引用彼此的内部实现，只通过 `internal/app` 的 Module 契约装配。
  新模块要在 [cmd/lumo/modules.go](cmd/lumo/modules.go) 登记——那是唯一的装配点。
- **三平面**：Console（后台）、Public（公开）、Extension（插件自定义模型）。
  接口一律经 `huma.Register` 注册，鉴权与路由前缀由 `api.NewPlanes` 统一落实。
- 迁移只增不改：已合并的迁移文件不要回头编辑，新增一个文件。
- 5xx 响应不回传内部细节，错误只记日志。

### 前端（console/）

- Biome 管格式化与 lint，TypeScript 开 `strict`。
- **接口类型不手写**：改完后端接口后跑 `task console:api` 重新生成
  `console/src/api/` 下的类型与客户端。手写两遍类型是生态项目的慢性病。
- **颜色、间距、字号一律走设计 token**，组件里出现裸色值就是缺陷。
  三层 token 定义在 [console/src/styles/tokens.css](console/src/styles/tokens.css)
  （primitive → semantic → component）。
- 设置类界面复用统一的声明式表单引擎（`console/src/components/form/`），
  不要为某一处设置单写一套界面。

### 主题（internal/theme/builtin/ink/）

- 模板只消费服务端渲染好的内容，不在模板里加工 HTML。
- 除 `.Post.Content` 外，任何用户可控的值都**直接输出**，绝不 `safeHTML`。
- 不依赖任何 CDN：字体与脚本都自托管，主题必须能离线跑。
- 访客侧的关键表单（登录、注册、找回密码、账户页）是原生 POST，
  **关掉 JavaScript 也必须可用**。

## 提交信息

用 Conventional Commits，描述部分写中文：

```
feat(theme): 页眉换成方印头像，并加上亮/暗/跟随系统三态切换
fix(console): 主题页的两栏断点从 md 提到 xl，窄窗下设置面板不再被压扁
docs: 清掉删测试后遗留的文档与 CI 描述
```

常用类型：`feat` / `fix` / `docs` / `refactor` / `perf` / `chore`。
scope 用模块名（`theme` / `console` / `auth` / `account` / `media` …）。

描述写**这次改动带来的效果**，不要写「修改了若干文件」这类无信息量的话。

## Pull Request

- 从 `main` 开分支，一个 PR 只做一件事。
- PR 描述里说明：改了什么、为什么这么改、怎么验证的。
- 涉及界面的改动请附截图（亮色与暗色各一张）。
- 不要在 PR 里附带生成物：`internal/console/dist/` 不入库，
  `console/openapi/` 与 `console/src/api/` 是生成的，只有在接口确实变了时才提交。

## 许可证

Lumo 以 [AGPL-3.0](./LICENSE) 授权，并附带[插件与主题接口例外条款](./EXCEPTIONS.md)。

项目采用开源 + 商业双授权（见 [COMMERCIAL.md](./COMMERCIAL.md)），因此提交代码前
需要签署 [贡献者许可协议（CLA）](./CLA.md)——在你第一个 PR 的描述里加一行声明即可。

需要说明的是：你保留自己代码的版权，CLA 授予的是许可而非所有权。
