# Lumo

用 Go 编写的现代化开源 CMS，单一静态二进制：后台是 `go:embed` 进二进制的 React SPA，
访客前台由服务端模板渲染主题。产品形态对标 [Halo](https://www.halo.run/)，目标是形成主题与插件生态。

> **开发中** — 阶段 0 至 3 已完成（脚手架、后端核心基座、认证与权限、内容模型与业务功能），
> 后端业务能力已齐备；**Console 界面与默认主题尚未开发，当前版本不可用于生产**。
> 进度明细见「[开发进度](#开发进度)」。

## 特性规划

### 已完成（阶段 0–3）

- **内容管理**：文章与独立页面（同表以 `type` 区分）、树形分类、标签、评论、附件、菜单。
  文章含状态机（草稿 / 已发布 / 定时发布 / 回收站）、置顶、封面、摘要、可见性与修订历史
- **双内容格式**：Markdown 与规范 HTML 都是一等公民，每篇内容自带 `rawType`，主题只消费渲染结果，换编辑器不伤主题
- **认证与权限**：服务端会话 Cookie（HttpOnly + SameSite + CSRF）、Personal Access Token、argon2id 口令、
  自定义角色与权限串、所有权（`_any`）规则
- **用户与角色管理**：用户分页 / 创建 / 启停 / 设角色 / 重置口令 / 删除，自定义角色 CRUD，权限清单；
  含自锁防护（不能停用或删除自己、不能让站点失去最后一名管理员）
- **附件与图片**：本地与 S3 兼容两种存储、多档 WebP 缩略图、EXIF 方向纠正、
  类型判定为「扩展名白名单 + 内容嗅探」双向印证
- **评论与邮件**：访客评论（信任模型与正文相反，先全文转义再做有限富化）、审核、回复树、
  反垃圾（蜜罐 / 频率 / 关键词）、SMTP 后台发信队列
- **菜单**：自定义链接与站内条目（文章 / 页面 / 分类 / 标签）混合的层级树，站内地址读取时解析，记录改名自动跟随
- **SEO**：`robots.txt` / `sitemap.xml` / `feed.xml` / `atom.xml` 四份根路径文档，
  以及 canonical / OpenGraph / JSON-LD 元信息端点
- **站点设置**：声明式分组设置（JSON Schema 子集 + `x-widget`），供后台通用表单引擎渲染
- **REST API**：Console / Public / Extension 三平面，OpenAPI 3.1 规范由 Go 代码生成（当前 63 条路径）
- **模块化**：8 个功能模块以「编译期插件」形态组织，各自持有迁移与独立版本表

### 尚未开始（阶段 4–9）

- **主题系统**：主题为 zip 包，后台上传即切换，服务端 `html/template` 渲染，缺模板回退到内置默认主题
- **全文搜索**：Go 侧分词 + PostgreSQL `tsvector`，不依赖数据库扩展
- **后台 Console 界面**：块编辑器（TipTap）与 Markdown 编辑器（Milkdown）、通用表单引擎、七组导航页面、明暗双主题
- **默认主题**、**Docker 部署**与**发布流水线**

## 开发进度

| 阶段 | 内容 | 状态 |
|---|---|---|
| 0 | 脚手架 | 已完成 |
| 1 | 后端核心基座 | 已完成 |
| 2 | 认证与权限 | 已完成 |
| 3 | 内容模型与业务功能 | 已完成 |
| 4 | 主题系统 | 未开始 |
| 5 | API 层与代码生成 | 未开始 |
| 6 | 全文搜索 | 未开始 |
| 7 | Console 前端 | 未开始 |
| 8 | 默认主题 | 未开始 |
| 9 | 部署与发布 | 未开始 |

阶段 5 的「三平面路由」「OpenAPI 3.1 由代码生成」「统一分页」三项已随阶段 3 落地，届时只需补 Console TS 客户端生成与 Extension CRUD。

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

访问 http://127.0.0.1:8080 ，根路径会 302 跳转到 `/console/`。

其余配置项可复制 `config.example.yaml` 为 `config.yaml` 后修改，
或用 `LUMO_*` 环境变量覆盖；优先级为 默认值 < 配置文件 < 环境变量 < 命令行参数。

## REST API

三个平面，前缀与鉴权策略固定：

| 平面 | 路径 | 鉴权 |
|---|---|---|
| Console | `/api/v1/console/**` | 会话或 PAT，**默认强制认证**，免认证端点须显式注册 |
| Public | `/api/v1/public/**` | 匿名可读已发布内容、发表评论 |
| Extension | `/apis/{group}/{version}/{kind}` | 强制认证，v1 内部使用，为插件预留 |

所有接口经 [huma](https://huma.rocks) 注册，请求校验与文档由代码直接生成：

- `/api/openapi.json` — OpenAPI 3.1 规范；`/api/openapi-3.0.json` 为 3.0 降级版本
- `/api/docs` — 交互式文档
- 错误一律为 RFC 9457 `application/problem+json`；请求校验失败返回 422 并逐条列出 `errors`，
  请求体中的未知字段会被拒绝而非静默忽略；5xx 不回传任何内部细节
- 列表接口统一 offset 分页：`page`（从 1 起）与 `size`（默认 20，最大 100），响应为 `items` / `page` / `size` / `total`
- 校验错误的 `errors[].value` 对请求体位置一律剥离，避免登录接口回显口令

根路径上另有四份**非 JSON** 文档，经专用注册口挂载（爬虫只认根路径，故不能放在 API 前缀下）：
`/robots.txt`、`/sitemap.xml`、`/feed.xml`（RSS 2.0）、`/atom.xml`（Atom 1.0）。
站点未配置对外地址（`site.url`）时，sitemap 与订阅源明确返回 503，而不是产出相对地址。

功能接口位于 Console 与 Public 两个平面；Extension 平面已建好但尚无模块注册，随阶段 5 启用。

## 认证与安全

- **Console 登录**：服务端会话 + HttpOnly Cookie（`SameSite=Lax`）+ CSRF 双提交校验。
  不使用 localStorage JWT —— 令牌无法被 XSS 直接读取。库中只存会话令牌的 SHA-256
- **无头调用**：`Authorization: Bearer lumo_pat_...`，令牌**仅存哈希**，明文只在创建时返回一次
- **口令**：argon2id（64 MiB / t=3），哈希串为 PHC 格式自带参数，
  调整默认参数不会使既有哈希失效，并会在用户下次登录时透明升级
- **令牌 scope 只能收窄权限**：始终取「用户权限 ∩ scope」，写入超出用户自身权限的
  scope 不会获得任何额外能力
- **改密码 / 重置口令 / 停用账号**会立即清除该用户的全部会话与令牌
- **上传安全**：附件类型由扩展名白名单与内容嗅探双向印证决定，客户端声明的 `Content-Type` 一概不采信；
  文件名随机化；本地附件经带 `X-Content-Type-Options: nosniff` 与 sandbox CSP 的静态路由提供
- **评论**是唯一由匿名访客写入的用户内容，一律先全文转义再做有限富化（只有 http(s) 成为带
  `nofollow ugc noopener` 的锚点）；邮箱、IP、UA 只在 Console 平面出现，前台另有一套视图类型

生产环境部署务必设置：

| 变量 | 说明 |
|---|---|
| `LUMO_SECURE_COOKIES=true` | 启用 Cookie `Secure` 与 `__Host-` 前缀（需 HTTPS） |
| `LUMO_TRUSTED_PROXIES` | 可信反向代理 CIDR，如 `127.0.0.0/8`；留空则忽略 `X-Forwarded-For` |

未配置 `LUMO_TRUSTED_PROXIES` 时一律使用直连地址，不采信任何转发头 ——
这是刻意的默认值，避免客户端伪造来源 IP。

**口令与密钥一律不进配置文件、不进设置表**（设置会随备份、日志与接口响应流出），只走环境变量：

| 变量 | 说明 |
|---|---|
| `LUMO_DATABASE_DSN` | 数据库连接串，**无任何默认值** |
| `LUMO_S3_ACCESS_KEY` / `LUMO_S3_SECRET_KEY` | S3 兼容存储的访问密钥 |
| `LUMO_SMTP_PASSWORD` | SMTP 口令 |

## 配置

完整示例见 `config.example.yaml`，主要分组：

- `server` — 监听地址、对外地址、超时、可信代理、Secure Cookie、请求体上限
- `database` — 连接池与启动时自动迁移（`--no-migrate` 可关）
- `log` — 级别（debug / info / warn / error）与格式（text / json）
- `dataDir` — 运行时工作目录，内含 `themes` / `uploads` / `cache` / `logs` / `backups`

**两条请求体上限刻意分开**：普通请求 `maxBodySize`（默认 10 MiB）与 multipart 上传
`maxUploadSize`（默认 64 MiB），按内容类型而非路径区分——JSON 接口越小越安全，
而附件动辄几十兆，用同一个值要么挡住正常上传，要么给所有接口开大口子。

附件存储在后台「设置 → 附件存储」中切换本地或 S3，密钥只从环境变量读取。

## 开发

```bash
task                   # 列出所有任务
task run -- serve      # 直接运行后端（不构建前端）
task console:dev       # 另开终端跑 Console 开发服务器（HMR，代理到 :8080）
task check             # 提交前自检：格式化 + vet + lint + 测试
task test:integration  # 集成测试（需本机 PostgreSQL，库名须含 test）
```

前端未构建时后端仍可启动，`/console/` 会返回构建提示，不影响 API 开发。
Console 目前只有脚手架（Vite 6 + React 19 + TS strict + Tailwind v4 + shadcn/ui 约定 + Biome + Vitest），
业务页面属阶段 7。

集成测试直连本机 PostgreSQL 的独立测试库，DSN 走 `LUMO_TEST_DSN`；未设置时自动跳过。
库名必须含 `test`，护栏在代码层面拦截误连 —— 测试会删除并重建 schema。

每个测试包使用**独占 schema**（连接串带 `search_path`），因此 `go test ./...`
并行执行多个包时不会互相清空数据。

### 模块与迁移

功能模块以「编译期插件」形态组织：实现 `app.Module` 及所需的可选能力接口
（迁移、设置、权限、钩子、路由、启动、关闭），在 `cmd/lumo/modules.go` 登记即可——
**该文件是核心与模块之间唯一的装配点，顺序即依赖顺序**。

`Register` 只做装配、不得访问数据库（`migrate` 命令走的是同一条注册链，那时业务表可能尚不存在）；
需要读写库的启动逻辑放 `Start`。`serve` 的顺序固定为：
装配 → 迁移 → 播种 → `Start` → 服务 →（退出时）逆序 `Close`。

每个模块自带迁移（模块目录下 `migrations/*.sql`，经 `go:embed` 收进二进制），
版本表为 `goose_db_version_<模块>`，**迁移编号只需在模块内递增**，不必全局唯一。
当前共 7 个迁移来源：core 2、settings / media / taxonomy / content / comment / menu 各 1。

### 构建标签与静态性

全链路固定 `-tags nodynamic`（Taskfile / goreleaser / CI / golangci-lint），
它禁掉 `gen2brain/webp` 的动态库回退，产物不再依赖系统 libwebp。
时区库经 `_ "time/tzdata"` 内嵌：`-trimpath` 构建不带 GOROOT，装不了 Go 的机器上
`time.LoadLocation` 会全数失败。

发布产物为六个平台的单一静态二进制（linux / windows / darwin × amd64 / arm64，
`CGO_ENABLED=0`），配置见 `.goreleaser.yaml`；Docker 镜像发布留到阶段 9。

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
cmd/lumo/          CLI 入口与模块装配（modules.go 是唯一装配点）
internal/
  app/             Module 契约、App 注册器、核心权限声明
  api/             huma 三平面装配、错误桥接、分页约定
  auth/            用户、角色、会话、令牌；认证与管理端点
  config/ database/ migrate/ logging/ httpx/ server/ workdir/ version/ slug/
  console/         SPA 的 go:embed 目标（dist/ 不进库）
  media/ taxonomy/ content/ settings/ comment/ mail/ menu/ seo/   功能模块
  testsupport/     集成测试的整机装配与库名护栏
migrations/        核心 goose SQL 迁移
console/           Vite + React + TypeScript 后台前端
themes/default/    内置默认主题（阶段 4 起）
deploy/            Dockerfile、docker-compose.yml（阶段 9 起）
```

## 许可证

[GPL-3.0](./LICENSE)
