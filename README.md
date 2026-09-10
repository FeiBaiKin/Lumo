# Lumo

用 Go 编写的现代化开源 CMS，单一静态二进制，自带后台与主题系统。

> **开发中** — 当前处于阶段 3（内容模型与业务功能），尚不可用于生产。

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

# 配置数据库（口令只走环境变量，不写入配置文件）
export LUMO_DATABASE_DSN="postgres://user:password@127.0.0.1:5432/lumo?sslmode=disable"

# 启动（首次启动自动执行迁移）
./lumo serve

# 创建初始管理员（需在交互式终端运行，密码不回显）
./lumo admin create-user -username admin -email admin@example.com -role super-admin
```

访问 http://127.0.0.1:8080 ，会自动跳转到 `/console/`。

其余配置项可复制 `config.example.yaml` 为 `config.yaml` 后修改，
或用 `LUMO_*` 环境变量覆盖；优先级为 默认值 < 配置文件 < 环境变量 < 命令行参数。

## REST API

三个平面，前缀与鉴权策略固定：

| 平面 | 路径 | 鉴权 |
|---|---|---|
| Console | `/api/v1/console/**` | 会话或 PAT，默认强制认证 |
| Public | `/api/v1/public/**` | 匿名可读已发布内容 |
| Extension | `/apis/{group}/{version}/{kind}` | 强制认证，v1 内部使用 |

所有接口经 [huma](https://huma.rocks) 注册，请求校验与文档由代码直接生成：

- `/api/openapi.json` — OpenAPI 3.1 规范；`/api/openapi-3.0.json` 为 3.0 降级版本
- `/api/docs` — 交互式文档
- 错误一律为 RFC 9457 `application/problem+json`；请求校验失败返回 422 并逐条列出 `errors`，
  请求体中的未知字段会被拒绝而非静默忽略；5xx 不回传任何内部细节
- 列表接口统一 offset 分页：`page`（从 1 起）与 `size`（默认 20，最大 100），响应为 `items` / `page` / `size` / `total`

## 认证与安全

- **Console 登录**：服务端会话 + HttpOnly Cookie（`SameSite=Lax`）+ CSRF 双提交校验。
  不使用 localStorage JWT —— 令牌无法被 XSS 直接读取。
- **无头调用**：`Authorization: Bearer lumo_pat_...`，令牌**仅存哈希**，明文只在创建时返回一次。
- **口令**：argon2id（64 MiB / t=3），哈希串为 PHC 格式自带参数，
  调整默认参数不会使既有哈希失效，并会在用户下次登录时透明升级。
- **令牌 scope 只能收窄权限**：始终取「用户权限 ∩ scope」，写入超出用户自身权限的
  scope 不会获得任何额外能力。
- **改密码 / 停用账号**会立即清除该用户的全部会话与令牌。

生产环境部署务必设置：

| 变量 | 说明 |
|---|---|
| `LUMO_SECURE_COOKIES=true` | 启用 Cookie `Secure` 与 `__Host-` 前缀（需 HTTPS） |
| `LUMO_TRUSTED_PROXIES` | 可信反向代理 CIDR，如 `127.0.0.0/8`；留空则忽略 `X-Forwarded-For` |

未配置 `LUMO_TRUSTED_PROXIES` 时一律使用直连地址，不采信任何转发头 ——
这是刻意的默认值，避免客户端伪造来源 IP。

## 开发

```bash
task                   # 列出所有任务
task run -- serve      # 直接运行后端（不构建前端）
task console:dev       # 另开终端跑 Console 开发服务器（HMR，代理到 :8080）
task check             # 提交前自检：格式化 + vet + lint + 测试
task test:integration  # 集成测试（需本机 PostgreSQL，库名须含 test）
```

前端未构建时后端仍可启动，`/console/` 会返回构建提示，不影响 API 开发。

集成测试直连本机 PostgreSQL 的独立测试库，DSN 走 `LUMO_TEST_DSN`；未设置时自动跳过。
库名必须含 `test`，且不得是 `agent.md` §13.2 列出的他项目库 —— 测试会删除并重建
schema，护栏在代码层面拦截误连。

每个测试包使用**独占 schema**（连接串带 `search_path`），因此 `go test ./...`
并行执行多个包时不会互相清空数据。

功能模块以「编译期插件」形态组织：实现 `app.Module` 及所需的可选能力接口
（迁移、设置、权限、钩子、路由、启动、关闭），在 `cmd/lumo/modules.go` 登记即可。
每个模块的迁移拥有独立的版本表，编号只需在模块内递增。

## 命令

```
lumo serve       启动 HTTP 服务
lumo migrate     管理数据库迁移：
                   up               应用核心与全部模块的待执行迁移（缺省）
                   status           显示各来源每个迁移的应用状态
                   version          显示各来源当前的 schema 版本
                   down [来源]      回滚指定来源（缺省 core）的最后一个迁移，仅开发排错
lumo admin       管理用户：
                   create-user      创建用户（-username -email -role [-display-name]）
                   reset-password   重置密码，并使该用户全部会话与令牌失效（-user）
                   list-users       列出用户
lumo version     输出版本信息
```

`serve` 支持 `-addr`、`-config`、`-no-migrate`、`-debug-sql`。

`admin` 的密码一律从终端读取（不回显、不进 shell 历史），因此需要在交互式终端中运行。

健康检查：`/healthz` 只报进程存活，`/readyz` 额外探测数据库，可用于负载均衡摘流。

## 项目结构

```
cmd/lumo/          CLI 入口与模块装配
internal/          核心实现（含 console/dist 嵌入目标）
migrations/        核心 goose SQL 迁移
console/           Vite + React + TypeScript 后台前端
themes/default/    内置默认主题
deploy/            Dockerfile、docker-compose.yml
```

## 许可证

[GPL-3.0](./LICENSE)
