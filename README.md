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
- **访客账户**：可选开放注册，邮箱验证 / 找回密码 / 账户页、收藏与「我的收藏」；表单全部原生提交，**关掉 JavaScript 也能用**
- **内容安全**：正文按权限净化（`content:unsafe_html` 默认仅管理员），评论一律转义后有限富化
- **附件**：本地 / S3 兼容存储、WebP 多档缩略图、EXIF 方向纠正、扩展名白名单 + 内容嗅探双向印证
- **全文搜索**：Go 侧二元组分词 + PostgreSQL `tsvector`，不依赖任何数据库扩展
- **SEO**：`robots.txt` / `sitemap.xml` / `feed.xml` / `atom.xml` + canonical / OpenGraph / JSON-LD
- **插件**：zip 包含清单与设置声明，后台安装 / 启停，目前是纯声明式、**不执行任何代码**（WASM 运行时在路线图上）
- **在线升级**：后台「关于」页检查并安装新版本，校验 SHA-256、自检新二进制、备份旧版本后替换并自动重启
- **REST API**：Console / Public / Extension 三平面，OpenAPI 3.1 由 Go 代码生成，Console 的 TS 类型自动生成
- **模块化**：16 个功能模块以「编译期插件」形态组织，各自持有迁移与独立版本表

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
git clone https://github.com/FeiBaiKin/Lumo.git
cd Lumo

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

手边还没有数据库时，可以跳过上面那行 `export` 直接 `./lumo serve`：程序会进入
**安装向导**，在浏览器里填完连接信息、站点名与管理员账号就装好了（见下一节）。

其余配置项可复制 `config.example.yaml` 为 `config.yaml` 后修改，
或用 `LUMO_*` 环境变量覆盖；优先级为 默认值 < 配置文件 < 环境变量 < 命令行参数。

## 部署到 Linux 服务器

发布包是单个静态二进制，目标机器上不需要 Go、Node 或 Docker：

```bash
tar -xf lumo_<版本>_linux_amd64.tar
cd lumo_<版本>_linux_amd64
./lumo serve
```

首次启动会打印一条日志给出向导地址（默认 `http://127.0.0.1:8080/console/install`），
在浏览器里走完四步即可：数据库连接 → 站点信息 → 管理员账号 → 执行安装。
「测试连接」会回报目标库真实的版本、编码与已有用户数；数据库里若已有 Lumo 站点，
它会明确告诉你这是在**接管**而不是新建。

装完服务会自己重启进入正常模式并永久关闭向导（再次调用安装接口返回 409），
**不需要手动重启进程**。前端产物已编译进二进制，`data/` 下的主题、上传、缓存等
目录在首次启动时自动建好。

数据库连接串保存在 `data/install.json`（权限 `0600`，含口令），**备份时请连
`data/` 一起备份**。

凭据要由编排系统统管时（systemd、Kubernetes、Ansible 等），预先设好
`LUMO_DATABASE_DSN` 再启动就能跳过向导 —— 它的优先级高于向导写入的值；
这种情形下用 `lumo admin create-user -username … -email … -role super-admin`
创建第一个管理员（口令从终端读取，需要 TTY）。

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

## 在线升级

后台「系统 → 关于」页可以检查并安装新版本，发布包取自 GitHub Releases。

一次升级按顺序做五件事，任何一步失败都不会动到正在运行的程序：

1. 下载当前平台的发布包，**按 `checksums.txt` 核对 SHA-256**，对不上直接中止
2. 从包里取出主程序，解到程序所在目录的临时文件
3. **跑一次 `lumo version -json` 自检**——确认它在这台机器上能运行，且确实是要装的那个版本
4. 把当前二进制复制一份到 `dataDir/backups`，再原地替换
5. 优雅停机后以新版本重启；数据库结构由新版本启动时自动迁移

自检这一步挡住的是「架构拿错、文件被杀软掏空、包里装错版本」这类问题——
没有它，一次坏掉的升级会让站点直接起不来，而那时后台已经打不开了。

旧版本备份留在 `dataDir/backups`，默认保留最近 3 份（`update.keepBackups`），
后台可以逐份删除。要回退到某一份，把它复制回程序目录覆盖即可。

由发行方统一升级的部署可以整块关掉：`update.enabled: false`（或 `LUMO_UPDATE_ENABLED=false`）。

### 按部署形态怎么升级

能不能就地升级，取决于**换掉的那份二进制会不会消失**。程序启动时自己判断：
看它归哪个挂载点管——落在宿主机挂进来的目录或卷上就是持久的，落在容器自己的
根文件系统（overlay）上就不是。判断结果与理由都写在「关于」页上。

| 部署形态 | 后台一键升级 | 说明 |
|---|---|---|
| 二进制 + systemd | ✅ | `syscall.Exec` 换进程镜像，**PID 不变**，systemd 不会把它看成一次崩溃重启 |
| 二进制 + supervisor / 裸跑 / screen | ✅ | 同上，守护进程察觉不到发生过重启 |
| **1Panel「运行环境」、宝塔「Go 项目管理器」** | ✅ | 这些**是容器**，但站点目录从宿主机挂进来，二进制躺在宿主机磁盘上——容器重建它一根汗毛不少 |
| LXC / OpenVZ 虚拟化的 VPS | ✅ | 「系统容器」，跑的是直接部署的二进制，与普通机器无异 |
| 官方 Docker 镜像 / compose / K8s | ⚠️ 默认拒绝 | 二进制打在镜像层里，见下 |
| 程序目录只读 | ❌ | 交给部署脚本或包管理器 |

K8s 建议直接关掉（`update.enabled: false`）：升级本就归编排管，多副本时一个 Pod
升了其他没升更是麻烦。

### 二进制打在镜像里时怎么办

这种情况下就地替换是白费功夫：换掉的文件活在容器的可写层，**重建**容器
（改配置、拉新镜像、`compose up --force-recreate`）就回到镜像里的版本。
后台会直接给出该换成哪个标签、以及怎么换。三条路各有取舍：

1. **手动换标签**（最稳）：把镜像改成 `ghcr.io/feibaikin/lumo:<新版本>` 后重建容器。
   1Panel 在「容器 → 编辑 → 镜像」，宝塔在 Docker 模块的容器编辑里；命令行是
   `docker compose pull && docker compose up -d`（compose 里用 `:latest` 时）。
   数据都在卷上，重建不丢。
2. **自动拉新镜像**：给容器装一个 Watchtower（1Panel 应用商店里有），它定时检查
   镜像更新并自动重建容器。这是容器世界里「自动升级」的标准做法，Lumo 这边不需要
   任何配置——发布正式版时 `:latest` 会跟着动。
3. **开 `update.allowInContainer`**：之后就能在后台一键升级，不必再碰镜像标签。
   代价写清楚：**重建容器时会退回镜像里的版本**。为此程序会把「由在线升级装到了哪个
   版本」记在 `dataDir/update-state.json`（那是挂载卷，活得比容器长），下次启动发现
   版本变旧就在日志里记一条 warn、在后台「关于」页明说，不会无声无息地退回去。
   这个开关只对这一种情形有效：它推翻不了「目录只读」，那是物理事实不是策略。

> 注意「在容器里」与「二进制会消失」不是一回事。面板类部署（1Panel 运行环境、
> 宝塔 Go 项目管理器）都是容器，但程序文件在宿主机目录上，属于上面表里的 ✅ 那一档，
> 不需要任何开关。

## 后台 Console

React SPA（`/console/`），侧栏七组导航：仪表盘 / 内容 / 媒体 / 外观 / 用户 / 设置 / 系统，
另有个人中心（资料、改密码、访问令牌）。块编辑器（TipTap v3）与 Markdown 编辑器（Milkdown 7，
语法集与服务端 goldmark + GFM 对齐）产出同一对字段；站点设置与主题设置共用同一套通用表单引擎
（JSON Schema 子集 + `x-widget`，18 种控件）；命令面板 Ctrl/⌘ K；明暗双主题默认跟随系统。
权限只决定入口显示与否，真正的拦截始终在服务端。

前端未构建时后端仍可启动，`/console/` 返回构建提示，不影响 API 开发。

## 主题

> 要写主题请直接看 **[主题开发文档](./docs/theme-development.md)**：
> 上下文字段、Finder、模板函数、设置声明与打包安装的完整参考。
> 本节只是概览。

主题是一个 zip 包，后台「外观 → 主题」上传即切换：

```
<theme>/
├── theme.yaml          # 元信息：name / label / version / author / requireLumo
├── settings.yaml       # 设置项声明（可选），与站点设置同一套表单 Schema
├── templates/          # layouts/ 与 partials/
├── static/             # 静态资源，经 /theme-assets/<主题名>/ 访问
└── screenshot.png      # 可选
```

**必需模板只有四个**：`index.html`、`post.html`、`page.html`、`404.html`。另有十一个可选模板
（分类 / 标签 / 归档 / 搜索 / 作者，登录 / 注册 / 找回密码 / 重置密码 / 账户页，以及我的收藏），
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
| Public | `/api/v1/public/**` | 匿名可读已发布内容、发表评论；收藏需登录 |
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
- `log` — 级别、格式（text / json）、是否同时写文件与日志的保留天数
- `update` — 在线升级：总开关、自动检查间隔、更新源仓库、备份保留份数
- `dataDir` — 运行时工作目录（`themes` / `uploads` / `cache` / `logs` / `backups`）

日志默认**同时写控制台与文件**：控制台那一路保持 `log.format` 指定的格式，
供 `docker logs` 与 `journalctl` 使用；文件那一路一律 JSON，按天切割写入
`dataDir/logs`，默认保留 14 天，后台「系统 → 日志」读的就是它。
不想落盘时设 `log.file: false`（或 `LUMO_LOG_FILE=false`），控制台输出不受影响。

普通请求（默认 10 MiB）与 multipart 上传（默认 64 MiB）是**两条独立上限**，按内容类型区分。
附件存储在后台「设置 → 附件存储」切换本地或 S3，密钥在同一页上填。

## 开发

```bash
task                   # 列出所有任务
task all               # 全量构建（Console 前端 + 后端）
task run -- serve      # 直接运行后端（不构建前端）
task console:dev       # Console 开发服务器（HMR，代理到 :8080）
task check             # 提交前自检：格式化 + vet + lint
task test:integration  # 集成测试（需本机 PostgreSQL；DSN 走 LUMO_TEST_DSN）
task console:lint      # Biome + tsc --noEmit
task console:test      # Vitest
task console:api       # 重新导出 OpenAPI 规范并生成 TS 类型（需 LUMO_DATABASE_DSN）
```

Console 技术栈为 Vite 6 + React 19 + TS strict + Tailwind v4 + Radix UI + TanStack Query。
`internal/console/dist` 由 `task console:build` 在构建前自动清理（保留 `.gitkeep`）；
直接 `npm run build` 会绕过清理，构建 Console 请走 Task 任务。

**本仓库当前不含任何自动化测试**：用例、集成测试的整机装配包 `testsupport`、Console 的
`src/test/setup.ts` 已于 2026-09-15 全部清空。`task test` / `task test:cover` /
`task console:test` 仍然可用，但只会报「no test files」或 0% 覆盖——留它们是为了将来加回
用例时不用重新接线，**不要当成质量门槛**。恢复集成测试时注意：原先自动核对库名含 `test`
的护栏已随用例删除，`LUMO_TEST_DSN` 指向哪个库现在只能自己核对。

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
  media/ taxonomy/ content/ settings/ comment/ favorite/ mail/ menu/ seo/   功能模块
  plugin/          插件系统：声明式插件的包格式、生命周期与设置（无代码执行）
  pkgzip/          主题与插件共用的 zip 安全解压
  logs/            后台日志页的读取端：从 dataDir/logs 的 JSON 行里查询、下载
  update/          在线升级：查 GitHub Releases、校验下载、备份替换与自重启
  form/            声明式表单 DSL：设置分组的 Go 侧声明与 settings.yaml 反解
  extension/       Extension 平面的通用 CRUD，给插件预留的自定义模型
  search/          全文搜索：Go 侧二元组分词 + tsvector 索引与后台对账
  theme/           主题系统：模板引擎、主题包、前台路由、主题设置
    builtin/ink/   内置默认主题「墨」，go:embed 进二进制，同时是所有主题的回退
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

## 参与贡献

开发环境、代码约定与提交规范见 [CONTRIBUTING.md](./docs/CONTRIBUTING.md)；
参与前请先读一遍[行为准则](./docs/CODE_OF_CONDUCT.md)。

项目采用开源 + 商业双授权，提交代码前需要签署 [CLA](./docs/CLA.md)——
你保留自己代码的版权，授予的是许可而非所有权。

发现安全问题请走 [SECURITY.md](./docs/SECURITY.md) 里的私有报告通道，不要开公开 Issue。

## 许可证

[AGPL-3.0](./LICENSE)，并附带**[插件与主题接口例外条款](./docs/EXCEPTIONS.md)**：
通过主题模板接口或插件 API 与 Lumo 交互的独立作品不构成衍生作品，
可以用任何许可证发布，包括闭源出售。

**程序本体免费，怎么用都行**——个人站、公司站、商业媒体站、企业内部部署，
不收费、不分成、不需要额外授权。只有把 Lumo 本身作为托管服务转售、或嵌入闭源产品
分发，才需要[商业授权](./docs/COMMERCIAL.md)。

- 开发与出售主题、插件，以及规划中的主题市场：[THEMES.md](./docs/THEMES.md)
- 「Lumo」名称与标识的使用规则：[商标政策](./docs/TRADEMARK.md)
