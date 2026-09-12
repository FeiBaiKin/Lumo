# Lumo

用 Go 编写的现代化开源 CMS，单一静态二进制：后台是 `go:embed` 进二进制的 React SPA，
访客前台由服务端模板渲染主题。产品形态对标 [Halo](https://www.halo.run/)，目标是形成主题与插件生态。

> **开发中** — 九个阶段全部完成（脚手架、后端核心基座、认证与权限、内容模型与业务功能、
> 主题系统、API 层与代码生成、全文搜索、后台 Console、默认主题、部署与发布），
> 后端、访客前台、后台界面与部署产物均已就位；
> **尚未正式发版，也未经生产环境检验**，用于正式站点请自行充分验证。
> 进度明细见「[开发进度](#开发进度)」。

## 特性规划

### 已完成（阶段 0–8）

- **内容管理**：文章与独立页面（同表以 `type` 区分）、树形分类、标签、评论、附件、菜单。
  文章含状态机（草稿 / 已发布 / 定时发布 / 回收站）、置顶、封面、摘要、可见性与修订历史
- **双内容格式**：Markdown 与规范 HTML 都是一等公民，每篇内容自带 `rawType`，主题只消费渲染结果，换编辑器不伤主题
- **主题系统**：主题是一个 zip 包，后台上传即切换，服务端用 `html/template` 渲染；
  Hugo 风格的 layout / partial 约定 + 函数库补足模板体验；九种路由各注入固定上下文，
  Finder 函数供侧栏页脚取数；**必需模板只有四个**，其余缺失时整页回退到内置主题
- **内置主题「墨 Ink」**：随二进制分发，开箱即用，同时是所有主题的回退目标。
  为中文长文阅读设计（纸 / 墨 / 印三色、36 字栏宽、宽屏元信息栏）。
  自托管思源宋体（按 `unicode-range` 分包，每页只下载用到的几片）、GSAP + Lenis 动效
  与 SVG 方印插图系统，**全部自托管，无任何 CDN 依赖**；字体与动效都可在主题设置里关掉
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
- **站点设置与主题设置**：同一套声明式分组设置（JSON Schema 子集 + `x-widget`），供后台通用表单引擎渲染
- **REST API**：Console / Public / Extension 三平面，OpenAPI 3.1 规范由 Go 代码生成，统一 offset 分页
- **Extension 平面**：给插件预留的自定义模型通用 CRUD，`spec` 为自由 JSON，支持按字段筛选
- **类型不手写两遍**：`lumo openapi` 导出规范，Console 的 TS 类型与客户端由它生成
- **全文搜索**：Go 侧二元组分词 + PostgreSQL `tsvector`，**不依赖任何数据库扩展**；
  中文按相邻两字切词并以短语算子还原相邻关系，标题 / 摘要 / 正文三段加权排序
- **后台 Console**：React SPA，构建产物 `go:embed` 进二进制。七组导航、块编辑器（TipTap v3）
  与 Markdown 编辑器（Milkdown 7）、通用表单引擎、命令面板（Ctrl/⌘ K）、明暗双主题（默认跟随系统）。
  设计 token 为三层架构并清空了 Tailwind 内置主题键，组件里写不出裸色值
- **模块化**：11 个功能模块以「编译期插件」形态组织，各自持有迁移与独立版本表

### 推到 v1.1

- **官网**：用本 CMS 自建
- **2FA 与 OAuth 登录**：Module 边界已留
- **插件加载器**：v1 只有编译期模块，Extension 平面与设置 Schema 已为它预留接口

## 开发进度

| 阶段 | 内容 | 状态 |
|---|---|---|
| 0 | 脚手架 | 已完成 |
| 1 | 后端核心基座 | 已完成 |
| 2 | 认证与权限 | 已完成 |
| 3 | 内容模型与业务功能 | 已完成 |
| 4 | 主题系统 | 已完成 |
| 5 | API 层与代码生成 | 已完成 |
| 6 | 全文搜索 | 已完成 |
| 7 | Console 前端 | 已完成 |
| 8 | 默认主题 | 已完成 |
| 9 | 部署与发布 | 已完成 |

阶段 5 的「三平面路由」「OpenAPI 3.1 由代码生成」「统一分页」三项随阶段 3 提前落地，
其余两项（Extension CRUD、Console TS 客户端生成）于阶段 5 补齐。
阶段 8 的范围随阶段 4 收窄：内置主题在阶段 4 已把 token、版式与九个模板做完整，
阶段 8 只需补上自托管字体、动效与插图。

**已知缺口**：「系统 → 日志」页仍是占位 —— 服务端尚无日志读取接口，
在想清楚日志落文件还是落库之前，不给它做一个假的页面。

阶段 9 的部署产物（`deploy/`）与发布流水线已交付，构建链本身经交叉编译实测；
**镜像能否构建起来需要在装有 Docker 的环境验证** —— 手动触发 Release 工作流、
保持 snapshot 选项打开即可试跑，它只构建不发布。

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
docker compose exec -it lumo /lumo admin create-user   -username admin -email you@example.com -role super-admin
```

随后访问 http://127.0.0.1:8080 ，后台在 `/console/`。

镜像为 `ghcr.io/feibaikin/lumo`（linux/amd64 与 linux/arm64 多架构清单）。
想改了代码从源码构建，加 `--build` 即可，compose 会走 `deploy/Dockerfile` 重编前端与后端。

几个**刻意如此**的默认值：

| 默认 | 为什么 |
|---|---|
| 端口只绑 `127.0.0.1:8080` | TLS 交给前面的反向代理；要直接对外，改 `.env` 里的 `LUMO_BIND` |
| PostgreSQL 不映射端口 | 它只需被同一 compose 网络内的应用访问 |
| `POSTGRES_PASSWORD` 无默认值 | 没填就让 compose 当场报错，而不是用一个人人都知道的弱口令把库跑起来 |
| `LUMO_SECURE_COOKIES=false` | 纯 HTTP 下开它会让登录「成功后立刻失效」；**上了 HTTPS 必须改成 true** |
| `LUMO_TRUSTED_PROXIES` 为空 | 不采信任何 `X-Forwarded-For`。不填的话日志与评论限流看到的都是反代的 IP |

**数据分两处**：应用文件（主题、上传、插件、缓存、日志）在 `lumodata` 卷（容器内 `/data`），
数据库在 `pgdata` 卷。删容器不丢，删卷才丢。`/data/backups` 只是应用预留的目录，
**不等于已经做了备份**——数据库、应用卷与密钥三样都要自己备，流程见「[备份与恢复](#备份与恢复)」。

**数据库口令含特殊字符（`@ : / ? # %` 等）时要分开填两个变量**：`POSTGRES_PASSWORD` 保持
原始口令（PostgreSQL 直接用它的字面值建角色），`LUMO_DATABASE_DSN` 里填**只对口令段做百分号
编码**的完整 URL。把编码后的串填进 `POSTGRES_PASSWORD` 会让库保存编码串、客户端用解码值去连，
首次部署必然失败——`deploy/.env.example` 里有对照示例与两种数据卷场景的说明。
不想操心就用它附的那条命令生成 URL 安全口令。

运行镜像是 distroless，**没有 shell 也没有包管理器**。因此排障靠 `docker compose logs`
而不是 exec 进去翻文件；也无法在容器内做 HEALTHCHECK，探活请从外部请求
`/healthz`（进程存活）或 `/readyz`（额外探测数据库，可用于负载均衡摘流）。

### 备份与恢复

数据分两处：应用文件在 `lumodata` 卷（容器内 `/data`，含 `uploads/`、`themes/`、`plugins/`），
数据库在 `pgdata` 卷。compose 顶层项目名是 `lumo`，所以宿主机上这两个卷叫 `lumo_lumodata`
与 `lumo_pgdata`（`docker volume ls` 可确认；改过项目名就以实际前缀为准）。
`/data/backups` 只是应用启动时建的空目录，应用不会往里写任何东西——**备份要自己按下面做**，
数据库、应用卷、密钥三样一样都不能少。以下命令都在 `deploy/` 下执行。

**数据库：pg_dump / pg_restore**

```bash
# 逻辑备份，自定义格式（-Fc），配 pg_restore 使用。-T 关掉 TTY 才能重定向到宿主机文件；
# 用户与库名从容器内的环境变量取，跟随 .env 的配置，不必手抄。
docker compose exec -T postgres sh -c 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc' \
  > lumo-db-$(date +%Y%m%d-%H%M%S).dump
```

pg_dump 本身跑在可重复读事务里，单看数据库是自洽的；但库与上传文件分属两处存储，
要让两者落在同一时点，先停应用再做整套备份：

```bash
docker compose stop lumo
docker compose exec -T postgres sh -c 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc' > lumo-db.dump
# 接着做下面的卷备份，然后再启动
docker compose start lumo
```

恢复时删库重建，而不是只加 `--clean`：`--clean` 只清理备份中出现过的对象，
上一版本新建的表会在库里残留。

```bash
docker compose stop lumo
docker compose exec -T postgres sh -c 'dropdb --force -U "$POSTGRES_USER" "$POSTGRES_DB" && createdb -U "$POSTGRES_USER" "$POSTGRES_DB"'
docker compose exec -T postgres sh -c 'pg_restore -U "$POSTGRES_USER" -d "$POSTGRES_DB"' < lumo-db.dump
docker compose start lumo
```

用 postgres 容器自带的 pg_dump / pg_restore（与库同为 17），别用宿主机上版本不一致的客户端。
dump 里有口令哈希与会话 / 令牌哈希，按密钥对待：不要放进公开目录，传输与长期保存都要加密。

**应用文件：lumodata 卷**

运行镜像是 distroless，没有 tar 可用，借一个临时 alpine 容器挂卷打包：

```bash
# 备份：卷以只读挂载，避免边写边打包。uploads / themes / plugins 就是需要备份的全部内容，
# cache 可重建、logs 属排障、backups 是空目录，都不进归档。
docker run --rm -v lumo_lumodata:/data:ro -v "$PWD":/backup alpine:3 \
  tar czf /backup/lumodata-$(date +%Y%m%d-%H%M%S).tar.gz -C /data uploads themes plugins
```

```bash
# 恢复：先停应用，清掉旧内容再解开。应用以 nonroot（65532）运行，
# 解包后必须把属主改回去，否则文件写不进去。
docker compose stop lumo
docker run --rm -v lumo_lumodata:/data -v "$PWD":/backup alpine:3 \
  sh -c 'rm -rf /data/uploads /data/themes /data/plugins && tar xzf /backup/lumodata-20260912-120000.tar.gz -C /data && chown -R 65532:65532 /data/uploads /data/themes /data/plugins'
docker compose start lumo
```

把示例归档名换成实际文件名；`$PWD` 是宿主机上存放备份的目录（Windows 的 Git Bash 会改写
`-v` 里的路径，挂载失败时加 `MSYS_NO_PATHCONV=1`，或在 PowerShell 下用 `${PWD}`）。

**环境变量与密钥：deploy/.env**

库和卷都有了还差启动参数：`POSTGRES_PASSWORD`、`LUMO_SMTP_PASSWORD`、
`LUMO_S3_ACCESS_KEY` / `LUMO_S3_SECRET_KEY` 按设计只存在于 `deploy/.env`（与进程环境）里，
数据库和卷里都没有：

- `.env` 丢了，DSN 就拼不出来，应用连不上已有的 `pgdata`——角色口令只在首次初始化数据目录时
  写入，之后改环境变量不会改它，只能另想办法重设；
- SMTP / S3 凭据丢了，邮件发不出、对象存储读写不了，只能去服务商重新签发。

`.env` 已被 `.gitignore` 排除，不随仓库走，**必须单独备份**（密码管理器或密钥保险箱），
并且不要和数据库 dump 放在一起。若某处部署额外挂了 `config.yaml`，它同样要备份（compose 默认没挂）。

**附件存在 S3 时**

后台「设置 → 附件存储」切到 S3 后，上传文件与缩略图都在对象存储的桶里，`lumodata`
卷只剩缓存与日志——只备卷等于没备附件。二选一或都做：

- 给桶开**版本控制**并设足够长的保留期，保证数据库备份对应的对象版本还在；
- 定期导出一份：`aws s3 sync s3://<桶>/<前缀> ./s3-backup/`（用单独的备份凭据，别复用应用的密钥）。

数据库里存的是桶设置与对象键，恢复时桶里的内容必须与 dump 对应同一时点。

**固定版本升级与回滚**

先钉版本再升级：compose 里默认是 `:latest`，把 `image:` 改成具体版本或摘要，

```yaml
# deploy/docker-compose.yml
image: ghcr.io/feibaikin/lumo:1.0.0        # Release 镜像的版本 tag（不带 v）
# image: ghcr.io/feibaikin/lumo@sha256:... # 更严格：摘要不会被移动
```

```bash
cd deploy
# 升级前：备份数据库 + 卷 + .env（见上），并记录当前镜像摘要，回滚时按它钉回
docker image inspect ghcr.io/feibaikin/lumo:latest --format '{{index .RepoDigests 0}}'

# 升级：固定 tag 后拉取重建，只动 lumo，pgdata / lumodata 不动
docker compose pull lumo
docker compose up -d lumo
docker compose logs -f lumo          # 看迁移与启动日志
curl -fsS http://127.0.0.1:8080/readyz
```

回滚分两种：

- **只回滚镜像**：新版本还没跑过迁移时，把 `image:` 改回上一版 tag 或摘要，
  `docker compose up -d lumo` 即可。
- **连数据一起回滚**：新版本一旦执行迁移，就不能只靠旧镜像退回（迁移只向前）。
  停应用，恢复升级前的数据库 dump 与卷归档，再切回旧镜像启动；
  顺序是停 → 换镜像 → 恢复库 → 恢复卷 → 起。

**恢复后检查**（演练也照这份清单走）：

1. `curl -fsS http://127.0.0.1:8080/healthz` 与 `/readyz` 都返回 200，`docker compose logs lumo` 无迁移报错。
2. 打开 `/console/`，用原有账号能登录（用户、角色、会话都在数据库里）。
3. 随机抽一篇已发布文章，前台页面正常渲染，主题样式与字体加载正常。
4. 附件可访问（本地存储看 `/uploads/...`，S3 确认桶里对象仍可读），后台媒体库能列出。
5. 文章下的评论正常显示。

### 不装 Docker 也能验证镜像构建

改了 `deploy/` 下的 Dockerfile、又不想在本机装 Docker 时，让 CI 代跑一次：
在 GitHub 的 **Actions → Release → Run workflow** 里保持 `snapshot` 选项打开即可。
它会跑完整的多架构构建与装箱，但**不推 GHCR、不建 Release**，
二进制归档留在该次运行的 artifact 里（保留 7 天）。命令行等价写法：

```bash
gh workflow run release.yml -f snapshot=true
gh run watch
```

要留意的是：`dockers_v2` 的镜像在 publish 阶段才构建，所以 `goreleaser build`
和 `release --skip=publish` 都不会碰 Dockerfile —— 验证必须走上面这条
`release --snapshot` 的路径。snapshot 模式下 buildx 不建多架构清单，
改出两个带平台后缀的本地镜像（`...-amd64` 与 `...-arm64`）。

## 后台 Console

后台是 React SPA，构建产物经 `go:embed` 进二进制，访问 `/console/`。
侧栏七组导航：

| 组 | 页面 |
|---|---|
| 仪表盘 | 概览（统计部件、快捷访问、新评论、最近文章、站点概况） |
| 内容 | 文章、页面、分类、标签、评论 |
| 媒体 | 附件（网格与列表双视图） |
| 外观 | 主题（含主题设置）、菜单 |
| 用户 | 用户、角色 |
| 设置 | 站点、SEO、邮件、存储 |
| 系统 | 关于、日志（占位，见「[开发进度](#开发进度)」） |

另有不进侧栏的**个人中心**：改资料、改密码、签发与吊销访问令牌。

**两个编辑器**。块编辑器为 TipTap v3（斜杠菜单支持拼音检索、块拖拽手柄、格式工具条），
Markdown 编辑器为 Milkdown 7（语法集与服务端的 goldmark + GFM 对齐，避免「编辑器里能写、
发表后不生效」）。两者产出同一对字段，`raw` 存规范 HTML 或 Markdown 原文而非编辑器私有结构；
格式切换会如实告知不可逆并逐条列出会丢什么。编辑页为沉浸式：文章设置走弹窗，右侧是大纲栏，
并接上了阶段 3 就有的修订历史接口。

**通用表单引擎**。站点设置与主题设置共用同一套引擎，渲染 JSON Schema 子集 + `x-widget`（11 种控件）。
引擎不认识任何具体字段名——服务端新增设置分组不必改前端；服务端返回的 422 校验明细按 location
落回对应字段，并同时给出可聚焦的错误摘要。

**其他**：命令面板（Ctrl/⌘ K，跳转 / 动作 / 按标题找文章与页面）、明暗三态切换（默认跟随系统）、
列表状态同步到地址栏（后退能回到刚才那一屏）、批量动作逐条串行并如实报出「N 成功 M 失败」、
全局遵守 `prefers-reduced-motion`。权限只决定入口显示与否，真正的拦截始终在服务端。

前端未构建时后端仍可启动，`/console/` 返回构建提示，不影响 API 开发。

## 主题

访客前台由主题渲染。主题是一个 zip 包，在后台「外观 → 主题」上传即切换，结构如下：

```
<theme>/
├── theme.yaml          # 元信息：name / label / version / author / requireLumo
├── settings.yaml       # 设置项声明，走与站点设置同一套表单 Schema（可选）
├── templates/          # 模板
│   ├── layouts/        # 外层骨架，页面用 {{ template "layouts/base.html" . }} 调用
│   └── partials/       # 可复用片段
├── static/             # 静态资源，经 /theme-assets/<主题名>/ 访问
└── screenshot.png      # 后台展示用截图（可选）
```

**必需模板只有四个**：`index.html`、`post.html`、`page.html`、`404.html`。
分类、标签、归档、搜索、作者五个模板可选，缺省时**整页回退**到内置主题「墨 Ink」的同名模板，
不会因缺文件而报错或渲染空白。

模板里可用的数据：路由上下文（`.Site` / `.Post` / `.Posts` / `.Pagination` / `.Category` /
`.Tag` / `.Author` / `.Archive` / `.Query` / `.Theme.Settings`）与只读的 Finder 函数
（`{{ .Find.Posts.Recent 5 }}`、`.Find.Categories.Tree`、`.Find.Tags.Cloud 24`、
`.Find.Archives.ByMonth`、`.Find.Menus.Get "primary"`、`.Find.Comments.Tree $id`）。

前台路由约定：文章 `/posts/<slug>`、独立页面 `/<slug>`（可用 `page-*.html` 指定专属模板）、
分类 `/categories/<slug>`、标签 `/tags/<slug>`、归档 `/archives/<年>[/<月>]`、
作者 `/authors/<用户名>`、搜索 `/search?q=`。

主题模板改动后，在后台点「重新加载」即可生效；开发时设 `LUMO_THEME_DEV=true`
可让模板改动自动重载、静态资源不缓存。

内置主题「墨 Ink」把字体与动效都做成了可关的设置项：标题用随主题附带的思源宋体
（按 `unicode-range` 分包，每页只下载用到的那几片，`font-display: swap`），
动效为入场编排、插图滚动显现与文章页阅读进度线（GSAP + ScrollTrigger + Lenis，均自托管）。
访客系统开启「减少动态效果」时动效直接不执行。空状态、搜索无果与 404 由一枚 SVG 方印承担，
没有 JavaScript 也能显示。

## REST API

三个平面，前缀与鉴权策略固定：

| 平面 | 路径 | 鉴权 |
|---|---|---|
| Console | `/api/v1/console/**` | 会话或 PAT，**默认强制认证**，免认证端点须显式注册 |
| Public | `/api/v1/public/**` | 匿名可读已发布内容、发表评论 |
| Extension | `/apis/{group}/{version}/{资源段}` | 强制认证，v1 内部使用，为插件预留 |

所有接口经 [huma](https://huma.rocks) 注册，请求校验与文档由代码直接生成：

- `/api/openapi.json` — OpenAPI 3.1 规范；`/api/openapi-3.0.json` 为 3.0 降级版本
- `/api/docs` — 交互式文档
- 错误一律为 RFC 9457 `application/problem+json`；请求校验失败返回 422 并逐条列出 `errors`，
  请求体中的未知字段会被拒绝而非静默忽略；5xx 不回传任何内部细节
- 列表接口统一 offset 分页：`page`（从 1 起）与 `size`（默认 20，最大 100），响应为 `items` / `page` / `size` / `total`
- 校验错误的 `errors[].value` 对请求体位置一律剥离，避免登录接口回显口令

Console 的 API 类型由规范生成，不手写：`task console:api` 先用 `lumo openapi` 导出
`console/openapi/openapi.json`，再由 `openapi-typescript` 生成 `console/src/api/schema.d.ts`，
运行时经 `openapi-fetch` 取得类型（`console/src/api/client.ts`，自动补 CSRF 头）。
**规范与生成的类型都进版本库**，前端构建不依赖后端在跑。

根路径上另有四份**非 JSON** 文档，经专用注册口挂载（爬虫只认根路径，故不能放在 API 前缀下）：
`/robots.txt`、`/sitemap.xml`、`/feed.xml`（RSS 2.0）、`/atom.xml`（Atom 1.0）。
站点未配置对外地址（`site.url`）时，sitemap 与订阅源明确返回 503，而不是产出相对地址。

Extension 平面自阶段 5 起由 `internal/extension` 提供通用 CRUD，全部要求 `extensions:manage`：
`GET|POST /apis/{分组}/{版本}/{资源段}` 与 `GET|PUT|DELETE /apis/{分组}/{版本}/{资源段}/{名称}`。
分组为反向域名（核心用 `io.github.feibaiikin.lumo`），版本 v1 统一 `v1alpha1`，
kind 为单数 PascalCase 而地址段用它的小写复数形式（`Post` 对应 `posts`），名称为 DNS-1123。
`spec` 是自由 JSON，服务端只存取不解释；列表支持 `where=键=值`（可重复）按 spec 顶层字段筛选，走 GIN 索引。

## 全文搜索

`GET /api/v1/public/search?q=关键词`，匿名可用，只返回已发布且公开的内容；
`type=post` / `type=page` 可重复，限定内容类型。响应是统一的分页结构，
每条带 `score` 相关度（只在同一次查询内可比）。

**不依赖任何 PostgreSQL 扩展**：切词在 Go 侧完成，中日韩按**二元组**切分
（「全文搜索」→ 全文 / 文搜 / 搜索），拉丁与数字按整词并转小写，结果存成 `tsvector`
并走 GIN 索引。查询时把同一段切出的二元组用短语算子 `<->` 相连，要求原文中相邻，
效果等价于子串匹配；单字查询退化为前缀匹配。标题 / 摘要 / 正文三段加权，
标题命中排在正文命中之前。

不引分词词典是刻意的：词典一旦没收录某个新词，整篇文章就再也搜不到，且没有任何报错。

索引由后台对账维护——按 `posts.updated_at` 找出落后的行重建，不在内容的写路径上挂钩子，
因此批量导入甚至直接改库都跟得上，代价是新内容最多 2 秒后才可搜。
`GET /api/v1/console/search/status` 看进度，`POST /api/v1/console/search/reindex` 整体重建
（均需 `settings:manage`）。

## 认证与安全

- **Console 登录**：服务端会话 + HttpOnly Cookie（`SameSite=Lax`）+ CSRF 双提交校验。
  不使用 localStorage JWT —— 令牌无法被 XSS 直接读取。库中只存会话令牌的 SHA-256
- **无头调用**：`Authorization: Bearer lumo_pat_...`，令牌**仅存哈希**，明文只在创建时返回一次
- **口令**：argon2id（64 MiB / t=3），哈希串为 PHC 格式自带参数，
  调整默认参数不会使既有哈希失效，并会在用户下次登录时透明升级
- **令牌 scope 只能收窄权限**：始终取「用户权限 ∩ scope」，写入超出用户自身权限的
  scope 不会获得任何额外能力
- **令牌签发需要会话 + 重新输入密码**：令牌不能签发令牌，且省略 scope 表示继承当前全部权限
  （服务端展开为显式清单落库）。**空 scope 表示没有任何权限**，不再表示账号无限权限——
  升级后按旧语义（空 scope）创建的令牌会失效，需要重新签发
- **正文按权限净化**：正文是站点同源输出的，而 Console 与管理 API 在同一个源上。
  没有 `content:unsafe_html` 的角色（默认只有 `admin` / `super-admin`），正文在保存时按允许列表
  净化：保留排版、表格、代码块、`class`/`id`/`data-*`、行内表现性样式与远程 http(s) 的 iframe，
  去掉脚本、事件属性、`javascript:`/`data:` 协议、表单与 `srcdoc`。原稿（`raw`）不净化，
  编辑器往返不丢内容。把该权限授予 editor 等于把它提升到「可对管理员执行脚本」的信任级别
- **登录限流与哈希并发预算**：失败尝试按账号（15 分钟 8 次）与客户端 IP（15 分钟 40 次）
  两个维度计数，超出返回 429；argon2 校验受进程级并发闸门约束，额度用满返回 503
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
| `LUMO_THEME_DEV` | 设为 `true` 开启主题开发模式：模板改动自动重载、静态资源不缓存。**只在开发机开** |

## 配置

完整示例见 `config.example.yaml`，主要分组：

- `server` — 监听地址、对外地址、超时、可信代理、Secure Cookie、请求体上限
- `database` — 连接池与启动时自动迁移（`--no-migrate` 可关）
- `log` — 级别（debug / info / warn / error）与格式（text / json）
- `dataDir` — 运行时工作目录，内含 `themes` / `uploads` / `cache` / `logs` / `backups`；
  第三方主题装在 `themes/` 下

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

Console 技术栈为 Vite 6 + React 19 + TS strict + Tailwind v4 + Radix UI + react-router +
TanStack Query，检查工具为 Biome 与 Vitest：

```bash
task console:lint   # Biome + tsc --noEmit
task console:test   # Vitest
task console:api    # 重新导出 OpenAPI 规范并生成 TS 类型（需 LUMO_DATABASE_DSN）
```

`internal/console/dist` **由 `task console:build` 在构建前自动清理**：Taskfile 先删旧产物、
保留 `.gitkeep`，再跑 Vite。`vite.config.ts` 的 `emptyOutDir: false` 是为了不动 `.gitkeep`
（否则全新克隆时 `go:embed all:dist` 会编译失败）；直接 `npm run build` 会绕过这道清理，
构建 Console 请走 Task 任务。

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
当前共 10 个迁移来源：core 2，settings / media / taxonomy / content / comment / menu / extension / search / theme 各 1
（seo 与 mail 无数据表，故不出现）。

### 构建标签与静态性

全链路固定 `-tags nodynamic`（Taskfile / goreleaser / CI / golangci-lint），
它禁掉 `gen2brain/webp` 的动态库回退，产物不再依赖系统 libwebp。
时区库经 `_ "time/tzdata"` 内嵌：`-trimpath` 构建不带 GOROOT，装不了 Go 的机器上
`time.LoadLocation` 会全数失败。

发布产物为六个平台的单一静态二进制（linux / windows / darwin × amd64 / arm64，
`CGO_ENABLED=0`），配置见 `.goreleaser.yaml`。推 `v*` 标签即触发 Release 工作流：
归档、校验和、草稿 Release，以及推往 GHCR 的 linux/amd64 + linux/arm64 多架构镜像。
手动触发该工作流并保持 snapshot 选项，则只构建不发布，可用来验证镜像构建。

## 命令

```
lumo serve       启动 HTTP 服务
lumo migrate     管理数据库迁移：
                   up               应用核心与全部模块的待执行迁移（缺省）
                   status           显示各来源每个迁移的应用状态
                   version          显示各来源当前的 schema 版本
                   down [来源]      回滚指定来源（缺省 core）的最后一个迁移，仅开发排错
lumo openapi     导出 OpenAPI 规范（缺省写标准输出，-o 写文件，-3.0 出降级版本）
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
