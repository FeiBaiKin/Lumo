# Lumo

用 Go 编写的现代化开源 CMS，单一静态二进制：后台是 `go:embed` 进二进制的 React SPA，
访客前台由服务端模板渲染主题。产品形态对标 [Halo](https://www.halo.run/)，目标是形成主题与插件生态。

> **开发中**：功能已齐备，但**尚未正式发版、未经生产环境检验**。欢迎试用与反馈，
> 上生产请自行评估风险。

## 特性

- **内容**：文章与页面（状态机 / 置顶 / 可见性 / 修订历史）、树形分类、标签、评论（审核 / 反垃圾）、附件、菜单
- **双内容格式**：Markdown 与规范 HTML 一等公民（`rawType` 区分），块编辑器 TipTap v3 与 Markdown 编辑器 Milkdown 7
- **主题系统**：zip 上传即切换，`html/template` + Hugo 式 layout/partial 约定；必需模板只有四个，缺失整页回退内置主题
- **内置主题「墨 Ink」**：为中文长文阅读设计；自托管思源宋体与 GSAP + Lenis 动效，无任何 CDN 依赖
- **认证与权限**：会话 Cookie + CSRF、PAT（scope 只能收窄，空 scope 无权限）、argon2id、自定义角色与所有权（`_any`）规则、登录限流
- **访客账户**：可选开放注册，邮箱验证 / 找回密码 / 账户页；全部原生表单提交，**关掉 JavaScript 也能用**
- **内容安全**：正文按权限净化（`content:unsafe_html` 默认仅管理员），评论一律转义后有限富化
- **附件**：本地 / S3 兼容存储、WebP 多档缩略图、EXIF 方向纠正、扩展名白名单 + 内容嗅探双向印证
- **全文搜索**：Go 侧二元组分词 + PostgreSQL `tsvector`，不依赖任何数据库扩展
- **SEO**：`robots.txt` / `sitemap.xml` / `feed.xml` / `atom.xml` + canonical / OpenGraph / JSON-LD
- **插件**：zip 包含清单与设置声明，后台安装 / 启停，目前是纯声明式、**不执行任何代码**（WASM 运行时在路线图上）
- **REST API**：Console / Public / Extension 三平面，OpenAPI 3.1 由 Go 代码生成，Console 的 TS 类型自动生成
- **模块化**：13 个功能模块以「编译期插件」形态组织，各自持有迁移与独立版本表

**路线图**：2FA 与 OAuth 登录；让插件从声明式走向可执行（声明式页面 → 扩展点 → WASM 后端）。

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

访问 http://127.0.0.1:8080 —— 根路径由主题渲染的访客前台接管，后台在 `/console/`。

其余配置项可复制 `config.example.yaml` 为 `config.yaml` 后修改，
或用 `LUMO_*` 环境变量覆盖；优先级为 默认值 < 配置文件 < 环境变量 < 命令行参数。

## Docker 部署

`deploy/` 下是整套部署文件：多阶段构建的 `Dockerfile`、含 PostgreSQL 17 的
`docker-compose.yml`，以及环境变量样板 `.env.example`。

```bash
cd deploy
cp .env.example .env          # 至少要填 POSTGRES_PASSWORD
docker compose up -d

# 创建初始管理员。必须带 -it：口令从终端读取且不回显，没有 TTY 时命令会直接失败
docker compose exec -it lumo /lumo admin create-user -username admin -email you@example.com -role super-admin
```

随后访问 http://127.0.0.1:8080 ，后台在 `/console/`。
镜像为 `ghcr.io/feibaikin/lumo`（linux/amd64 与 linux/arm64 多架构清单）；
想从源码构建，加 `--build` 即可。

几个**刻意如此**的默认值：

| 默认 | 为什么 |
|---|---|
| 端口只绑 `127.0.0.1:8080` | TLS 交给前面的反向代理；要直接对外，改 `.env` 里的 `LUMO_BIND` |
| PostgreSQL 不映射端口 | 它只需被同一 compose 网络内的应用访问 |
| `POSTGRES_PASSWORD` 无默认值 | 没填就让 compose 当场报错，而不是用弱口令把库跑起来 |
| `LUMO_SECURE_COOKIES=false` | 纯 HTTP 下开它会让登录「成功后立刻失效」；**上了 HTTPS 必须改成 true** |
| `LUMO_TRUSTED_PROXIES` 为空 | 不采信任何 `X-Forwarded-For`，避免客户端伪造来源 IP |

**数据分两处**：应用文件（上传、主题、插件）在 `lumodata` 卷（容器内 `/data`），
数据库在 `pgdata` 卷。删容器不丢，删卷才丢。

**数据库口令含特殊字符（`@ : / ? # %` 等）时要分开填两个变量**：`POSTGRES_PASSWORD` 保持
原始口令，`LUMO_DATABASE_DSN` 单独填**只对口令段做百分号编码**的完整 URL——
把编码后的串填进 `POSTGRES_PASSWORD` 会让首次部署必然失败，`deploy/.env.example` 有对照示例。

运行镜像是 distroless，**没有 shell**：排障靠 `docker compose logs`；容器内无法做 HEALTHCHECK，
探活从外部请求 `/healthz`（进程存活）或 `/readyz`（额外探测数据库，可用于负载均衡摘流）。

本机没装 Docker 时，可在 GitHub **Actions → Release → Run workflow** 保持 `snapshot` 打开
验证镜像构建（跑完整多架构构建但不推 GHCR）：`gh workflow run release.yml -f snapshot=true`。

## 备份与恢复

数据库、应用卷、`deploy/.env` **三样都要备**（密钥只存在于 `.env`，库和卷里都没有；
`.env` 已被 `.gitignore` 排除，必须单独备份）。`/data/backups` 只是应用建的空目录，
应用不会往里写东西。以下命令都在 `deploy/` 下执行。

```bash
# 数据库逻辑备份（自定义格式；-T 关掉 TTY 才能重定向到宿主机文件）
docker compose exec -T postgres sh -c 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc' \
  > lumo-db-$(date +%Y%m%d-%H%M%S).dump

# 应用文件（uploads / themes / plugins 就是需要备份的全部内容；distroless 没有 tar，借 alpine）
docker run --rm -v lumo_lumodata:/data:ro -v "$PWD":/backup alpine:3 \
  tar czf /backup/lumodata-$(date +%Y%m%d-%H%M%S).tar.gz -C /data uploads themes plugins
```

库与上传文件分属两处存储，要让两者落在同一时点，先 `docker compose stop lumo` 再做整套备份。

**恢复**（先停应用）：删库重建而不是只加 `--clean`（`--clean` 清不掉上一版新建的表）——

```bash
docker compose exec -T postgres sh -c 'dropdb --force -U "$POSTGRES_USER" "$POSTGRES_DB" && createdb -U "$POSTGRES_USER" "$POSTGRES_DB"'
docker compose exec -T postgres sh -c 'pg_restore -U "$POSTGRES_USER" -d "$POSTGRES_DB"' < lumo-db.dump
# 卷：清掉旧内容解开归档后，必须把属主改回 nonroot(65532)，否则文件写不进去
docker run --rm -v lumo_lumodata:/data -v "$PWD":/backup alpine:3 \
  sh -c 'rm -rf /data/uploads /data/themes /data/plugins && tar xzf /backup/lumodata-*.tar.gz -C /data && chown -R 65532:65532 /data/uploads /data/themes /data/plugins'
docker compose start lumo
```

dump 里有口令哈希与会话 / 令牌哈希，按密钥对待：不要放公开目录，传输与长期保存要加密。

**附件在 S3 时**：上传文件都在对象存储里，只备卷等于没备附件——给桶开版本控制设足够保留期，
或定期 `aws s3 sync s3://<桶>/<前缀> ./s3-backup/` 导出。恢复时桶内容必须与数据库 dump 对应同一时点。

**升级与回滚**：先把 compose 里的 `image:` 钉到具体版本或摘要，备份三样后
`docker compose pull lumo && docker compose up -d lumo`。
新版本还没跑过迁移时，换回旧镜像即可回滚；**迁移只向前**——一旦跑了迁移，
必须恢复升级前的数据库 dump 与卷归档，再切回旧镜像（顺序：停 → 换镜像 → 恢复库 → 恢复卷 → 起）。

**恢复后检查**：`/healthz` 与 `/readyz` 200、日志无迁移报错；原账号能登录；
抽一篇已发布文章前台渲染正常；附件可访问、媒体库能列出；评论正常显示。

## 后台 Console

React SPA（`/console/`），侧栏七组导航：仪表盘 / 内容 / 媒体 / 外观 / 用户 / 设置 / 系统，
另有个人中心（资料、改密码、访问令牌）。块编辑器（TipTap v3）与 Markdown 编辑器（Milkdown 7，
语法集与服务端 goldmark + GFM 对齐）产出同一对字段；站点设置与主题设置共用同一套通用表单引擎
（JSON Schema 子集 + `x-widget`，17 种控件）；命令面板 Ctrl/⌘ K；明暗双主题默认跟随系统。
权限只决定入口显示与否，真正的拦截始终在服务端。

前端未构建时后端仍可启动，`/console/` 返回构建提示，不影响 API 开发。

## 主题

主题是一个 zip 包，后台「外观 → 主题」上传即切换：

```
<theme>/
├── theme.yaml          # 元信息：name / label / version / author / requireLumo
├── settings.yaml       # 设置项声明（可选），与站点设置同一套表单 Schema
├── templates/          # layouts/ 与 partials/
├── static/             # 静态资源，经 /theme-assets/<主题名>/ 访问
└── screenshot.png      # 可选
```

**必需模板只有四个**：`index.html`、`post.html`、`page.html`、`404.html`。另有十个可选模板
（分类 / 标签 / 归档 / 搜索 / 作者，以及登录 / 注册 / 找回密码 / 重置密码 / 账户页），
缺省时**整页回退**到内置主题「墨 Ink」，不会报错或渲染空白。
模板可用数据：路由上下文（`.Site` / `.Post` / `.Posts` / `.Pagination` / `.Theme.Settings` 等）
与只读 Finder 函数（`{{ .Find.Posts.Recent 5 }}`、`.Find.Categories.Tree`、`.Find.Menus.Get "primary"` 等）。

前台路由约定：文章 `/posts/<slug>`、独立页面 `/<slug>`、分类 `/categories/<slug>`、
标签 `/tags/<slug>`、归档 `/archives/<年>[/<月>]`、作者 `/authors/<用户名>`、搜索 `/search?q=`；
账户相关为 `/login`、`/register`、`/forgot-password`、`/reset-password`、`/account`。
模板改动在后台点「重新加载」即可生效；开发时设 `LUMO_THEME_DEV=true` 自动重载、静态资源不缓存。

## REST API

三个平面，前缀与鉴权策略固定：

| 平面 | 路径 | 鉴权 |
|---|---|---|
| Console | `/api/v1/console/**` | 会话或 PAT，**默认强制认证** |
| Public | `/api/v1/public/**` | 匿名可读已发布内容、发表评论 |
| Extension | `/apis/{group}/{version}/{资源段}` | 强制认证，为插件预留的自定义模型 CRUD |

- 所有接口经 [huma](https://huma.rocks) 注册，OpenAPI 3.1 由代码生成：
  `/api/openapi.json`（另有 3.0 降级版）与交互式文档 `/api/docs`
- 错误一律为 RFC 9457 `application/problem+json`；校验失败 422 逐条列 `errors`；请求体未知字段拒绝
- 列表统一 offset 分页：`page` / `size`（默认 20、上限 100），响应 `items` / `page` / `size` / `total`
- 根路径另有四份非 JSON 文档：`/robots.txt`、`/sitemap.xml`、`/feed.xml`（RSS 2.0）、`/atom.xml`（Atom 1.0）
- Console 的 TS 类型由规范生成、不手写：`task console:api` 导出规范并生成 `schema.d.ts`
  （规范与生成物都进版本库，前端构建不依赖后端在跑）

## 认证与安全

- **会话**：HttpOnly Cookie（`SameSite=Lax`）+ CSRF 双提交校验；库中只存会话与令牌的 SHA-256
- **PAT**：`Authorization: Bearer lumo_pat_...`，明文只在创建时返回一次；scope 与用户权限取交集，
  只能收窄；签发需会话 + 重新输入密码，令牌不能签发令牌；**空 scope 表示没有任何权限**
- **正文按权限净化**：没有 `content:unsafe_html`（默认仅 admin / super-admin）的角色，
  正文在保存时按允许列表净化（保留排版、表格、代码块、远程 iframe，去掉脚本与事件属性）；
  原稿 `raw` 不净化。把该权限授予 editor 等于允许其可对管理员执行脚本
- **登录限流**：失败按账号（15 分钟 8 次）与 IP（15 分钟 40 次）计数 → 429；argon2 校验受进程级
  并发闸门约束 → 503
- **凭据失效联动**：改密码 / 重置口令 / 停用账号立即清除该用户全部会话与令牌
- **上传安全**：类型由扩展名白名单与内容嗅探双向印证，客户端 `Content-Type` 一概不采信；
  文件名随机化；本地附件经带 `nosniff` 与 sandbox CSP 的静态路由提供
- **评论**：匿名访客的唯一写入面，一律先全文转义再做有限富化；邮箱 / IP / UA 不进前台响应

生产环境务必设置 `LUMO_SECURE_COOKIES=true`（HTTPS）与 `LUMO_TRUSTED_PROXIES`（可信反代 CIDR）。
**数据库 DSN 只走环境变量 `LUMO_DATABASE_DSN`**，不进配置文件。
SMTP 口令与 S3 访问密钥在后台「设置 → 邮件发送 / 附件存储」里填，**加密入库**
（AES-256-GCM，主密钥是 `data/secret.key`，可用 `LUMO_SECRET_KEY` 指定），接口不回传明文。
**备份必须连 `data/` 一起备份**：只恢复数据库，库里那些口令解不开。
`LUMO_SMTP_PASSWORD` / `LUMO_S3_ACCESS_KEY` / `LUMO_S3_SECRET_KEY` 保留为兜底，后台没填时生效。

## 配置

完整示例见 `config.example.yaml`，主要分组：

- `server` — 监听地址、对外地址、超时、可信代理、Secure Cookie、请求体上限
- `database` — 连接池与启动时自动迁移（`--no-migrate` 可关）
- `log` — 级别与格式（text / json）
- `dataDir` — 运行时工作目录（`themes` / `uploads` / `cache` / `logs` / `backups`）

普通请求（默认 10 MiB）与 multipart 上传（默认 64 MiB）是**两条独立上限**，按内容类型区分。
附件存储在后台「设置 → 附件存储」切换本地或 S3，密钥在同一页上填。

## 开发

```bash
task                   # 列出所有任务
task all               # 全量构建（Console 前端 + 后端）
task run -- serve      # 直接运行后端（不构建前端）
task console:dev       # Console 开发服务器（HMR，代理到 :8080）
task check             # 提交前自检：格式化 + vet + lint + 测试
task test:integration  # 集成测试（需本机 PostgreSQL；DSN 走 LUMO_TEST_DSN，库名须含 test）
task console:lint      # Biome + tsc --noEmit
task console:test      # Vitest
task console:api       # 重新导出 OpenAPI 规范并生成 TS 类型（需 LUMO_DATABASE_DSN）
```

Console 技术栈为 Vite 6 + React 19 + TS strict + Tailwind v4 + Radix UI + TanStack Query。
`internal/console/dist` 由 `task console:build` 在构建前自动清理（保留 `.gitkeep`）；
直接 `npm run build` 会绕过清理，构建 Console 请走 Task 任务。

集成测试按包使用**独占 schema**，`go test ./...` 并行执行多个包不会互相清空数据。

**模块化**：功能模块实现 `app.Module` 及可选能力接口（迁移 / 设置 / 权限 / 钩子 / 路由），
在 `cmd/lumo/modules.go` 登记——这是核心与模块间**唯一装配点，顺序即依赖顺序**。
`Register` 只做装配、不得访问数据库；`serve` 顺序固定为：装配 → 迁移 → 播种 → Start → 服务 → 逆序 Close。
每个模块自带迁移（`migrations/*.sql` 经 go:embed），版本表为 `goose_db_version_<模块>`，
迁移编号只需在模块内递增。

全链路固定 `-tags nodynamic`（产物不依赖系统 libwebp），时区内嵌 `time/tzdata`。
发布产物为六个平台的单一静态二进制（linux / windows / darwin × amd64 / arm64，`CGO_ENABLED=0`），
配置见 `.goreleaser.yaml`；推 `v*` 标签即触发 Release（归档、校验和、GHCR 多架构镜像）。

## 命令

```
lumo serve       启动 HTTP 服务（-addr -config -no-migrate -debug-sql）
lumo migrate     管理数据库迁移：
                   up（缺省） | status | version | down [来源]（仅开发排错）
lumo openapi     导出 OpenAPI 规范（-o 写文件，-3.0 出降级版本）
lumo admin       管理用户：
                   create-user | reset-password | list-users
lumo version     输出版本信息
```

`admin` 的密码一律从终端读取（不回显、不进 shell 历史），需在交互式终端中运行。

## 项目结构

```
cmd/lumo/          CLI 入口与模块装配（modules.go 是唯一装配点）
internal/
  app/             Module 契约、App 注册器、核心权限声明
  api/             huma 三平面装配、错误桥接、分页约定
  auth/            用户、角色、会话、令牌；认证与管理端点
  account/         访客侧账户：注册 / 邮箱验证 / 找回密码 / 账户页（原生表单，不依赖 JS）
  secret/          凭据加密保管（AES-256-GCM），口令类设置加密入库后接口不回传明文
  config/ database/ migrate/ logging/ httpx/ server/ workdir/ version/ slug/
  console/         SPA 的 go:embed 目标（dist/ 不进库）
  media/ taxonomy/ content/ settings/ comment/ mail/ menu/ seo/   功能模块
  plugin/          插件系统：声明式插件的包格式、生命周期与设置（无代码执行）
  pkgzip/          主题与插件共用的 zip 安全解压
  form/            声明式表单 DSL：设置分组的 Go 侧声明与 settings.yaml 反解
  extension/       Extension 平面的通用 CRUD，给插件预留的自定义模型
  search/          全文搜索：Go 侧二元组分词 + tsvector 索引与后台对账
  theme/           主题系统：模板引擎、主题包、前台路由、主题设置
    builtin/ink/   内置默认主题「墨」，go:embed 进二进制，同时是所有主题的回退
  testsupport/     集成测试的整机装配与库名护栏
migrations/        核心 goose SQL 迁移
console/           Vite + React + TypeScript 后台前端
  openapi/         导出的 OpenAPI 规范（生成物，进库）
  src/api/         从规范生成的类型与 openapi-fetch 客户端
  src/components/  UI 原语、实体列表系统、表单引擎、两个编辑器、应用外壳
  src/pages/       七组导航对应的页面
  src/styles/      三层设计 token（primitive → semantic → component）
data/themes/       运行时装第三方主题的位置（不进库，由 workdir 创建）
deploy/            Dockerfile（源码构建）、Dockerfile.goreleaser（发布装箱）
                   docker-compose.yml、.env.example
```

## 许可证

[GPL-3.0](./LICENSE)
