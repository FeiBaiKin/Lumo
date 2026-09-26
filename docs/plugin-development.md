# Lumo 插件开发

插件是给站点加功能的 zip 包，后台上传即可安装，**不需要重启，也不需要碰服务器上的文件**。

一个插件可以是纯声明的：只放一份清单、几个设置项和静态文件。也可以带后端代码——
一段编译成 WebAssembly 的 Go 程序，在宿主提供的沙箱里运行，用得到的每一项能力都要站长点头。

本文是插件作者的完整参考。三个可运行的示例在 [examples/plugins](../examples/plugins)，
照着改是最快的入手方式。

---

## 目录

- [插件能做什么](#插件能做什么)
- [插件包结构](#插件包结构)
- [清单](#清单)
- [后端代码与 SDK](#后端代码与-sdk)
- [钩子](#钩子)
- [自己的数据](#自己的数据)
- [宿主能力](#宿主能力)
- [定时任务](#定时任务)
- [接口](#接口)
- [前台](#前台)
- [后台页面](#后台页面)
- [设置](#设置)
- [依赖别的插件](#依赖别的插件)
- [失败与限额](#失败与限额)
- [构建与安装](#构建与安装)
- [安全约定](#安全约定)
- [常见问题](#常见问题)

---

## 插件能做什么

| 你想要 | 用什么 |
|---|---|
| 评论入库前拦一道、正文输出前改一改 | [过滤器](#过滤器) |
| 有人留言、发文章、注册时做点什么 | [动作](#动作) |
| 存自己的数据 | [键值与资源](#自己的数据) |
| 读文章评论、发文章、访问外部接口、发邮件 | [宿主能力](#宿主能力) |
| 每隔一段时间做一件事 | [定时任务](#定时任务) |
| 给自己的界面提供数据接口 | [接口](#接口) |
| 往页面里加东西、给主题提供小组件、做短代码 | [前台](#前台) |
| 往后台加一页自己的界面 | [后台页面](#后台页面) |
| 让站长调参数 | [设置](#设置) |
| 依赖另一个插件 | [依赖别的插件](#依赖别的插件) |

两条边界先讲清楚，省得白写：

- **插件之间不能互相调用。** 没有「调用另一个插件的接口」这回事，只有[依赖声明](#依赖别的插件)，
  它管的是装、启用与版本匹配。
- **插件碰不到 SQL。** 读业务数据走[宿主能力](#宿主能力)，自己的数据走[键值与资源](#自己的数据)。

---

## 插件包结构

最小可用的插件只有一个文件：

```
comment-notify/
└── plugin.yaml
```

带后端的插件多一个 `plugin.wasm`；要用设置、静态文件、后台页面时再多几个：

```
comment-guard/
├── plugin.yaml       必需，清单
├── plugin.wasm       runtime: wasm 时必需，后端代码
├── settings.yaml     可选，设置项声明
├── logo.png          可选，后台列表里的图标
└── static/           可选，静态文件
    ├── panel.html    后台自定义页
    └── guard.js      前台脚本
```

包是 zip，上面的文件都在**包的根目录**，不要多套一层同名目录。

**包里只收这几种扩展名**：`.wasm` `.yaml` `.yml` `.json` `.html` `.css` `.js` `.svg` `.png` `.jpg`
`.jpeg` `.gif` `.webp` `.ico` `.woff` `.woff2` `.ttf` `.otf` `.txt` `.md`——白名单而不是黑名单，
别的类型（`.go`、`.sql`、`.exe`……）会被拒收，整个包装不上。源码留在你的仓库里，别打进包。
`plugin.yaml` 没提到的额外文件可以有（README、字体等等），宿主不看它们，
但每个文件的大小都算在 32 MiB 的上传上限里。

---

## 清单

`plugin.yaml` 是一份 GVK 资源，格式与后台导出的 Extension 资源同一套：

```yaml
apiVersion: plugin.lumo.run/v1alpha1
kind: Plugin
metadata:
  name: comment-guard          # 标识，DNS-1123，同时是安装目录名
spec:
  displayName: 评论反垃圾       # 后台显示的名字，留空时用标识
  version: 1.0.0               # 宽松 semver
  description: 一句话说明
  author:
    name: 你的名字
    website: https://example.com
  homepage: https://example.com/comment-guard
  repo: https://github.com/you/comment-guard
  license: AGPL-3.0
  requires: ">=0.2.0"          # 所要的 Lumo 版本范围
  runtime: wasm                # 留空为纯声明式
```

**未知字段一律报错**，不是忽略。`displayname` 这种拼写错误会在安装时就被指出来——
不然你会对着一个没有显示名的插件找半天。

`spec.version` 与 `metadata.name` 这两处约束值得单独说：

- 版本号用于[依赖](#依赖别的插件)与升级判断，也带在静态文件的 `?v=` 上，所以**同一份内容不要复用同一个版本号**。
- 标识必须与安装目录名一致，否则卸载会删错目录。

后面几节逐项讲 `spec` 的其余字段：`capabilities`、`hooks`、`resources`、`cron`、`routes`、
`frontend`、`pages`、`dependencies`。

---

## 后端代码与 SDK

后端是一段 Go 程序，编译成 wasip1 上的 WebAssembly 模块。生命周期里没有 `main`：

```go
package main

import lumo "github.com/FeiBaiKin/lumo/sdk/go"

func init() {
	// 登记处理函数。init 是唯一该登记的地方：宿主在加载时就会问一遍
	lumo.OnComment(lumo.ActionCommentCreated, notify)
}

func main() {}
```

`main` 是空的，因为这个模块是「反应器」——被宿主按需叫起来，自己不跑循环。
但**它必须存在**，否则 Go 链接会报 `function main is undeclared`。

SDK 是独立模块 `github.com/FeiBaiKin/lumo/sdk/go`，没有第三方依赖。本地开发时在 `go.mod` 里指过去：

```
require github.com/FeiBaiKin/lumo/sdk/go v0.0.0

replace github.com/FeiBaiKin/lumo/sdk/go => ../path/to/lumo/sdk/go
```

登记的处理函数在加载时会与清单**双向核对**：清单里声明了却没登记的，事情派过去时报错；
登记了却没声明的，启用时直接拒绝——因为站长确认能力时看不到它，而那段代码在你看来「写了却不生效」。

SDK 里可用的东西：

| 用途 | 入口 |
|---|---|
| 钩子 | `lumo.OnComment` `OnPost` `OnUserRegistered` `OnCommentJudge` `OnContentRender` `OnAction` `OnFilter` |
| 接口 | `lumo.OnRoute` |
| 前台 | `lumo.OnSlot` `OnWidget` `OnShortcode` |
| 定时任务 | `lumo.OnCron` |
| 数据 | `lumo.KV`、`lumo.Resources(kind)` |
| 内容 | `lumo.Content` |
| 外网与邮件 | `lumo.Fetch`、`lumo.SendMail` |
| 设置与身份 | `lumo.SettingsInto`、`lumo.Self` |
| 日志 | `lumo.Debug` `Info` `Warn` `Error` |

`lumo.Content` 这些是包级变量（`var Content contentAPI`），按 `lumo.Content.Posts(...)` 这样用。

日志写在插件的处理函数里，进站点的日志，后台的日志页能查到，都带插件名。**别把密钥写进日志**。

---

## 钩子

钩子分两类，写在 `spec.hooks` 里：

```yaml
  hooks:
    actions: [comment.created, post.published]
    filters: [comment.judge]
```

订阅钩子要声明 `capabilities.content.read`（钩子的数据都属于站点内容，评论里还带评论者邮箱与 IP），
`content.render` 另外还要 `capabilities.frontend`。

### 动作

**异步执行，不拖慢发起它的那一次请求。** 队列 1024，4 个协程派发，满了就丢弃并记日志；
单次处理 5 秒。处理函数返回的 error 只进日志——评论早就入库了，动作失败不该回滚业务。

| 动作 | 数据 | 什么时候 |
|---|---|---|
| `post.published` | `Post` | 文章或页面发布（含定时发布到点） |
| `post.updated` | `Post` | 保存（任何状态都算） |
| `post.trashed` | `Post` | 移入回收站 |
| `post.deleted` | `Post` | 彻底删除 |
| `comment.created` | `Comment` | 收到新评论，含待审与判为垃圾的 |
| `comment.approved` | `Comment` | 评论通过审核 |
| `user.registered` | `User` | 新建了用户，只有公开资料 |

```go
func notify(_ *lumo.Context, c *lumo.Comment) error {
	lumo.Info("新评论", "author", c.Author.Name, "post", c.Post.Title)
	return nil
}
```

### 过滤器

**同步执行，有时限。** 出错、超时或返回 nil 就沿用上一环的值——过滤器坏了不该让页面打不开。

| 过滤器 | 改什么 | 时限 | 额外要的声明 |
|---|---|---|---|
| `comment.judge` | 评论状态（`approved` / `pending` / `spam`） | 3 秒 | — |
| `content.render` | 正文 HTML | 200 毫秒 | `capabilities.frontend` |

`comment.judge` 在新评论**入库前**跑，此时评论还没有 ID。它只改状态与理由：

```go
func judge(_ *lumo.Context, j *lumo.CommentJudgement) error {
	if strings.Contains(j.Comment.Content, "买药") {
		j.Status = lumo.CommentSpam
		j.Reason = "命中关键词"
	}
	return nil
}
```

两处要注意：

- **内核判定的改不回来。** 站点自己的频率限制与蜜罐判为垃圾的评论，插件看到时已经是垃圾，改成 approved 无效。
  （反过来说，判定顺序是「内核先、插件后」。）
- `content.render` **只在文章与页面的详情页跑**，列表页每篇都跑一遍成本太高。结果会按普通作者的规则
  **重新净化**，所以你塞不进脚本；代价是正文里只有高权限作者才能放的内容（比如 iframe）经你改写后也会被净化掉。

---

## 自己的数据

每个带后端的插件都有两处自己的存储，不需要声明能力，插件之间互相看不见。

### 键值存储

零碎状态、计数器、缓存。配额 1 万个键、单值 64 KiB。

```go
lumo.KV.Set("last-id", 12, 0)                    // 0 表示不过期
lumo.KV.Get("last-id", &id)                      // 返回 (found, error)
n, _ := lumo.KV.Incr("views:12", 1, 0)           // 原子自增，返回加完的值
entries, _ := lumo.KV.List("views:", 200)        // 按前缀列，最多 200 条，按键排序
```

`Incr` 配 TTL 是常用的组合：按天分键、设 35 天过期，日计数就自己滚动了。

```go
lumo.KV.Incr("daily:"+time.Now().UTC().Format("2006-01-02"), 1, 35*24*time.Hour)
```

### 资源记录

需要在后台有个列表页、能查能改的数据，用声明式资源。在清单里描述字段，
宿主替你建存储、出列表页与编辑页：

```yaml
  resources:
    - kind: SpamLog              # 单数 PascalCase，地址里的复数段由它推导
      label: 拦截记录
      icon: shield-alert
      editable: false            # 只由插件写，后台只能看
      columns: [author, content] # 列表显示哪几列，留空取前三个
      title: author              # 用作记录标题的字段
      permission: plugins:manage # 后台看它要的权限串，缺省即此
      schema:                    # 与设置同一套 Schema
        type: object
        x-order: [author, content, postId]
        properties:
          author: {type: string, title: 评论者, maxLength: 100}
          content: {type: string, title: 正文, maxLength: 200, x-widget: textarea}
          postId: {type: integer, title: 文章 ID, minimum: 0}
```

```go
logs := lumo.Resources("SpamLog")
logs.Create(map[string]any{"author": "访客", "content": "……"})
logs.List(lumo.Query{Match: map[string]any{"postId": 12}, Sort: "createdAt", Desc: true, Limit: 50})
logs.Patch(id, map[string]any{"content": "改一点点"})   // 局部修改
```

记录总数上限 10 万条、单条 64 KiB。字段声明在安装时编译校验，列名与标题字段必须是声明过的字段。

**卸载时**站长可以在确认框里选「保留数据」，此时设置、键值与记录都留在库里，
重装同名插件时原样接回去；缺省则一并删除。

---

## 宿主能力

需要的能力写在 `spec.capabilities` 里。启用时站长会看到逐项列出来的清单，
确认之后才记下「授予了什么」；升级后的新版本多要了能力，插件会先被停用，等站长再次确认。

```yaml
  capabilities:
    content: {read: true, write: [posts:write]}
    http: [api.example.com, "*.example.com"]
    mail: true
    cron: true
    frontend: true
```

只读不写时，`content` 写成 `{read: true}` 即可；不用内容能力时整行不写。

`content.read`、`http`、`mail`、`cron` 这些**必须有后端代码去用**，所以声明它们时
`spec.runtime` 必须是 `wasm`；否则安装时报错——纯声明式插件用不上它们，站长却要为它们点头。

### 读内容（`content.read`）

```go
posts, total, _ := lumo.Content.Posts(lumo.PostQuery{Type: "post", Status: "published", Limit: 10})
post, _ := lumo.Content.Post(12)
page, _ := lumo.Content.PostBySlug("page", "about")
comments, total, _ := lumo.Content.Comments(lumo.CommentQuery{PostID: 12, Status: "pending"})
user, _ := lumo.Content.User(3)
terms, _ := lumo.Content.Categories()
```

回收站里的内容读不到，用户只给公开资料（用户名与显示名，不给邮箱）。

### 写内容（`content.write`）

写权限按**权限串**逐条申请，也就是站长自己有的那套串：

```yaml
    content:
      read: true
      write: [posts:write, posts:publish, comments:manage]
```

能申请到的只有内容类：`posts:write` `posts:write_any` `posts:publish` `posts:delete_any`
`pages:*` `taxonomies:manage` `comments:manage` `comments:manage_any` `media:write` `media:delete_any`。
**用户、角色、主题、设置、插件、站点这几类不给**——插件拿到它们就能给自己提权。
`content:unsafe_html` 也不给，它等于允许插件往正文里塞任意脚本。

```go
p, _ := lumo.Content.CreatePost(lumo.PostInput{Title: "标题", Raw: "<p>正文</p>", Publish: true})
lumo.Content.UpdatePost(p.ID, lumo.PostInput{Raw: "<p>改过的正文</p>"})
lumo.Content.TrashPost("post", p.ID)
lumo.Content.ModerateComment(7, lumo.CommentApproved)
```

写入走的是后台同一套流程：渲染、净化、slug 生成、动作派发都一样。
**插件新建的内容记在最早的超级管理员名下**，因为插件没有自己的账号。

### 访问外部网络（`http`）

域名要逐个声明，支持 `*.example.com` 这样的一级通配。写 IP、写 `localhost` 都不行。

```go
resp, err := lumo.Fetch(lumo.FetchRequest{
	Method: "POST",
	URL:    "https://api.example.com/check",
	Body:   body,
	Timeout: 3 * time.Second,
})
```

限制是硬性的：**建连时核对解析出来的 IP**，本机、内网、链路本地、CGNAT 一律拒绝（防 SSRF），
所以域名解析到内网也过不去；不走环境代理；重定向最多 3 次且**每一跳都要在白名单里**；
请求体 1 MiB、响应体 4 MiB、最长 10 秒。响应体的 `Truncated` 为真说明被截断了。

### 发邮件（`mail`）

```go
lumo.SendMail(lumo.Mail{To: []string{"admin@example.com"}, Subject: "新评论", Text: "……"})
```

走站点配置的 SMTP 与发送队列，失败会重试。**每个插件每小时最多 30 封**，
每封最多 10 个收件人——这个上游限制由宿主把关，超了会返回错误，所以自己也要按需过滤再发。

---

## 定时任务

```yaml
  capabilities: {cron: true}
  cron:
    - {name: scan, every: 1h, description: 每小时扫一遍过期数据}
```

```go
func init() { lumo.OnCron("scan", scan) }

func scan(ctx *lumo.Context) error { /* ... */ }
```

- 间隔写法是 Go 时长：`30m`、`1h`、`24h`。**最短 1 分钟，最长 30 天**，最多 10 个任务。
- 宿主每 30 秒扫一轮到期任务；**多实例部署时同一个周期只有一个实例执行**，不会跑重。
- 同一个任务不会叠着跑：上一次没结束，下一次到点也跳过。
- 单次时限 60 秒。

要跑「每天凌晨清理」这种任务，写 `every: 24h` 然后在函数里判断当前时间，或者用键值存储记住上次
跑的是哪一天。

---

## 接口

插件可以有自己的 HTTP 接口，给前台脚本、后台自定义页或别的系统用。

```yaml
  routes:
    - name: hit                 # 处理函数名，对应 lumo.OnRoute
      method: POST              # 缺省 GET
      path: /hit                # 插件前缀之后的路径，可以有 {参数}
      public: true              # 谁都能调
    - name: report
      path: /report/{id}
      permission: plugins:manage  # 非公开接口要的权限串，缺省即此
```

地址一律是 `/api/v1/plugins/<插件标识>/<路径>`：

```
POST /api/v1/plugins/visit-stats/hit
GET  /api/v1/plugins/comment-guard/stats
GET  /api/v1/plugins/visit-stats/report/12
```

路径段的写法与限制：小写字母、数字与 `-_.`，参数写成 `{id}`；**最多 8 段、最多 50 条**；
同一种方法下不能有两条形态相同的路径（`/a/{x}` 与 `/a/{y}` 算同一条）。
命中时静态段多的优先：`/a/b` 比 `/a/{x}` 更具体。路径对得上而方法不对，返回 405 并带上 `Allow` 头。

```go
func init() { lumo.OnRoute("hit", hit) }

func hit(_ *lumo.Context, req *lumo.Request) (*lumo.Response, error) {
	var in struct{ Post int64 `json:"post"` }
	if err := req.Decode(&in); err != nil {
		return lumo.Problem(400, "请求体形如 {\"post\": 12}"), nil
	}
	return lumo.JSON(200, map[string]any{"ok": true}), nil
}
```

`req` 里有 `Method`、`Path`、`Params`、`Query`、`Headers`（名字都是小写）、`Body`、`IP`、`User`。
`User` 是[登录用户](#谁能调公开接口与后台接口)，匿名为 nil。

答复用 `lumo.JSON` / `lumo.Text` / `lumo.NoContent` / `lumo.Problem`，或者自己填 `Response`。

### 谁能调：公开接口与后台接口

| | `public: true` | 非公开 |
|---|---|---|
| 谁能调 | 任何人 | 登录并持有 `permission` 声明的权限串 |
| 限流 | 每 IP 120 次/分 | 每登录用户 600 次/分 |
| 请求体上限 | 64 KiB | 1 MiB |
| 超时 | 10 秒 | 10 秒 |

非公开接口的调用者身份在 `req.User` 里（含 `Permissions`，可以用 `req.User.Can("posts:write")` 判断）。
公开接口通常没有 `User`，但如果调用者恰好带着有效的登录凭据，也会有。

### CSRF

插件接口与站点同源，所以写请求照例要防跨站。规则比后台接口宽一点，因为**公开接口的存在意义就是
让匿名的前台脚本能调**：

- `GET` / `HEAD` / `OPTIONS` 不看令牌。
- 用非会话凭据（比如 PAT）调的，不看令牌。
- 用会话凭据的写请求：**没带令牌时，公开接口按匿名访客处理**（`req.User` 是 nil），
  非公开接口直接返回 403。

所以「访问统计」那类脚本不必管令牌；要认人的公开写接口，让前台脚本带上 `X-CSRF-Token` 即可
（页面里的会话 Cookie 与令牌都在，脚本可以读 `lumo_csrf` 那枚 Cookie 带上）。

### 交给插件什么、收回什么

请求那边**不转给你**：`Cookie`、`Authorization`、`Proxy-Authorization`、`X-CSRF-Token`
（拿到它们就能冒充当前用户），头最多 30 个。

响应那边**只放行**：`Content-Type`、`Cache-Control`、`ETag`、`Last-Modified`、`Expires`、
`Content-Disposition`、`Content-Language`、`Vary`，以及 `X-` 开头的自定义头
（`X-Frame-*` 与 `X-Content-Type-Options` 除外）。**`Set-Cookie` 与 CORS 头一律不给**：
插件接口与站点同源，写得进 Cookie 就能动会话，放得开 CORS 就能让别的站读用户的数据。

跳转也不给：3xx 会被换成 500。判断不了的插件接口，调用方本来就无从跟进。
每个响应固定加上 `X-Content-Type-Options: nosniff` 与 `Content-Security-Policy: default-src 'none'; sandbox`，
所以哪怕你回了一段 HTML，浏览器直接打开它时也跑不了脚本、拿不到 Cookie。

---

## 前台

插件往前台页面里放东西，**全靠主题在页面里留好的位置**。主题不调这些，插件就进不了前台——
这与 WordPress 里的 `wp_head()`、`wp_footer()` 是同一回事。内置主题「墨」都接上了，
你自己的主题照 [主题开发文档的「给插件留位置」](./theme-development.md#给插件留位置) 做一遍即可。

```yaml
  capabilities: {frontend: true}
  frontend:
    styles: [static/guard.css]
    scripts: [static/guard.js]
    slots: [content.after]
    widgets:
      - {name: guard-report, label: 反垃圾统计}
    shortcodes:
      - {name: hello, description: 输出一句问候}
```

### 样式与脚本

只能引用**自己包里** `static/` 下的 `.css` 与 `.js`，各最多 10 个。
样式表进 `</head>`，脚本以 `defer` 进 `</body>` 之前，都带 `?v=<插件版本>` 防旧缓存。

脚本标签上带了宿主给的两个属性，你不必自己猜地址：

```html
<script defer src="/plugin-assets/visit-stats/visit-stats.js?v=1.0.0"
        data-plugin="visit-stats"
        data-api="/api/v1/plugins/visit-stats"
        data-post="12"></script>
```

`data-post` 只在文章与页面上有。`document.currentScript.dataset` 就能读到它们，
见[「访问统计」](../examples/plugins/visit-stats) 里的用法。

**内联脚本进不来。** 片段里的 `<script>` 一律被净化掉，要跑脚本就带一个文件。

### 插槽

在 `slots` 里声明要往哪几个插槽放东西，然后：

```go
func init() { lumo.OnSlot(lumo.SlotContentAfter, related) }

func related(_ *lumo.Context, page *lumo.PageInfo) (string, error) {
	if page.Post == nil {
		return "", nil          // 空串表示这次不显示
	}
	return `<p class="related">……</p>`, nil
}
```

五个插槽：`head`（`</head>` 之前）、`footer`（`</body>` 之前）、`content.before`、`content.after`、
`comments.after`。`page` 告诉你访客在看什么：`Kind`（`index`、`post`、`page`、`category`……）、
`Path`、`Title`，文章与页面上还有 `Post`。

`head` 插槽的净化规则更严，只收 `<meta>` 与几种 `<link>`，样式表请写进 `frontend.styles`。

### 侧栏小组件

```yaml
    widgets:
      - {name: popular, label: 热门文章, description: 按浏览次数排的前几篇}
```

```go
func init() { lumo.OnWidget("popular", popular) }

func popular(ctx *lumo.Context, page *lumo.PageInfo) (string, error) {
	return `<ul>……</ul>`, nil
}
```

站长在**主题设置**里挑：把侧栏某个条目的类型选成「插件小组件」，再从下拉里选具体哪一个，
存下来的 id 形如 `visit-stats/popular`。主题侧用 `.Widget "visit-stats/popular"` 渲染。

插件停用后，设置里存的 id 保留，前台那一条暂时不显示，重新启用就回来了。

### 短代码

作者在正文里写 `[名字 参数="值"]`，由提供它的插件展开：

```go
func init() { lumo.OnShortcode("hello", hello) }

func hello(_ *lumo.Context, sc *lumo.Shortcode) (string, error) {
	return "<p>你好，" + sc.Attr("name", "世界") + "</p>", nil
}
```

- **只在文章与页面的详情页展开**，列表、摘要与订阅源里保持原样（摘要里认得的短代码会被去掉，
  免得一片方括号）。这也意味着正文里的短代码不影响列表页性能。
- 一页最多展开 20 个，多出来的原样保留。
- 同一个名字有多个插件提供时，**标识排在前面的那个**负责。
- 想在正文里写一个不展开的方括号，写成 `[[名字]]`。

### 片段的净化与限额

插槽、小组件与短代码的输出都过一遍净化：正文允许的那些标签，**再加上表单控件**
（`form` `input` `button` `select` `option` `textarea` `label` `fieldset` `legend` `output` `progress` `meter`），
所以订阅框、投票这类小表单可以直接写。事件属性（`onclick` 之类）与内联脚本照旧不给——
要交互就带一个脚本文件，按 `data-plugin` 找到自己的元素。

每段片段 200 毫秒、64 KiB。超时、出错或超长都当作**没有内容**——访客页面上少一块，
比页面卡住或者报错好。所以别在插槽里做重活：那是访客打开页面时同步跑的。

---

## 后台页面

插件可以往后台加自己的页面。页面是包里的一张 HTML，放在**隔离的 iframe** 里：

```yaml
  pages:
    - path: settings-report      # 地址里的一段
      label: 拦截报告
      description: 最近拦下了什么
      icon: shield-alert
      file: static/report.html   # static/ 下的 .html
      menu: true                 # 是否在侧栏出入口，缺省 true
      permission: plugins:manage # 打开它要的权限串，缺省即此
```

地址是 `/plugins/<插件>/p/<页面>`，最多 10 个页面。iframe 不给 `allow-same-origin`，
文件本身也带 `CSP sandbox`，所以页面跑在一个**不透明的源**上：读不到后台的 Cookie，
也调不了后台接口。它要数据，就经 `postMessage` 请这一页代为调用**本插件自己的**接口。

页面里这样用：

```html
<script>
// 1. 告诉宿主自己的高度，免得 iframe 里出现滚动条
new ResizeObserver(() => {
  parent.postMessage({lumo: "height", value: document.body.scrollHeight}, "*");
}).observe(document.body);

// 2. 请宿主代为调用本插件的接口
let seq = 0;
const waiting = new Map();
window.addEventListener("message", (event) => {
  const data = event.data;
  if (data?.lumo === "response") {
    waiting.get(data.id)?.(data);
    waiting.delete(data.id);
  }
  if (data?.lumo === "context") {
    document.documentElement.dataset.scheme = data.scheme;  // light / dark，跟着后台换
  }
});

function call(path, options = {}) {
  const id = ++seq;
  return new Promise((resolve) => {
    waiting.set(id, resolve);
    parent.postMessage({
      lumo: "request", id,
      method: options.method ?? "GET",
      path,                       // 只能落在本插件的接口前缀之下
      query: options.query,       // 查询参数放这里，不要拼进 path
      body: options.body,         // 写请求自动带上 CSRF 令牌
    }, "*");
  });
}

const {ok, status, data} = await call("/stats");
</script>
```

几条要点：

- `path` **只能落在本插件的接口前缀之下**，以 `/` 开头，不能带 `..` 或 `?`；查询参数放 `query`。
- 写请求由宿主转发，`X-CSRF-Token` 由宿主补上——**页面上要按[非公开接口](#谁能调公开接口与后台接口)来声明**，
  因为转发出去的就是管理员的会话。
- 配色变化时宿主会推一条 `{lumo: "context", scheme, plugin, page}`，跟着它换深浅色即可。
- 高度不报也行，iframe 会撑到视口高度减一点。

---

## 设置

`settings.yaml` 让站长在后台调你的插件。格式与[主题设置](./theme-development.md#主题设置)**完全一致**，
控件也是同一套 `x-widget`，所以这里只讲插件这一侧要注意的：

```yaml
groups:
  - name: guard
    label: 反垃圾规则
    description: 命中任意一条就判为垃圾
    order: 10
    icon: shield-check
    schema:
      type: object
      x-order: [keywords, maxLinks]
      properties:
        keywords:
          type: string
          title: 关键词
          maxLength: 2000
          x-widget: textarea
        maxLinks:
          type: integer
          title: 链接数上限
          minimum: 0
          maximum: 50
    defaults:
      keywords: ""
      maxLinks: 3
```

- **分组名是存储键**，改名字等于把站长填过的值丢掉，所以只增不改。最多 20 个分组。
- `x-order` 决定字段顺序（YAML 经 map 解析后键序会丢）。
- 后台会在插件页里出「设置」入口，启用中的插件才能改——停用的插件不给改，免得出现「停用了还能动」的缝。

读设置：

```go
type config struct {
	Keywords string `json:"keywords"`
	MaxLinks int    `json:"maxLinks"`
}

func load() config {
	cfg := config{MaxLinks: 3}          // 先填上缺省值
	if err := lumo.SettingsInto("guard", &cfg); err != nil {
		lumo.Warn("读设置失败，按缺省值走", "error", err)
	}
	return cfg
}
```

返回的是**已保存值覆盖缺省值**之后的完整对象，缺省值不会缺席，所以正常路径下不必判空。

---

## 依赖别的插件

```yaml
  dependencies:
    - {name: comment-guard, version: ">=1.0.0 <2.0.0"}
    - {name: visit-stats, version: "^1.2"}
```

版本范围与 `spec.requires` 同一套写法，和 node-semver 一致：

| 写法 | 含义 |
|---|---|
| `*` 或留空 | 不限 |
| `1.2.3` | 正好 1.2.3 |
| `1.2` | 1.2.x（**不是**「正好 1.2.0」） |
| `1.2.*` / `1.x` | 同上 |
| `>=1.2` `>1.0.0` `<2.0.0` `<=1.9.9` `=1.2.3` | 比较 |
| `^1.2.3` | `>=1.2.3 <2.0.0`；`^0.2.3` 是 `>=0.2.3 <0.3.0` |
| `~1.2.3` | `>=1.2.3 <1.3.0`；`~1.2` 是 `>=1.2.0 <1.3.0` |
| `>=1.0.0 <2.0.0` | 空格或逗号是「与」 |
| `>=1.0.0 \|\| >=3.0.0` | `\|\|` 是「或」 |

规则：

- **依赖没就绪就启用不了**：没装、装了没启用、版本不够，三种都拦，并在后台说清是哪一种。
  装得上但启用不了，因为依赖可以在这之后才安装。
- **被依赖的插件停用或卸载时，依赖它的插件跟着停用**，列表里写明「依赖的插件 X 已停用」。
  后台在停用 / 卸载的确认框里会先列出会被连带停用的插件。
- 依赖的插件重新启用后，上面的原因会改成「可以再次启用」，但**不会自动启用**——那一步仍由站长点。
- 启动顺序按依赖排：被依赖的先起。
- **互相依赖（环）的两个插件谁也启用不了**，报的就是「依赖的插件 X 没有启用」。宿主不专门拦这种事。
- 同一份域名/名字不能重复声明，最多 20 条，也不能依赖自己。

再强调一次：依赖**只是**安装与版本的约束。插件之间不能互相调用，
所以它不是「用另一个插件的功能」的办法——真要共享，就约定好键值或资源的格式，各自读写。

---

## 失败与限额

宿主给每个插件划了明确的边界，越界只影响自己：

| 项 | 值 |
|---|---|
| 内存 | 每实例 32 MiB |
| 实例数 | 每插件 4 个（并发超出时排队） |
| 一次调用的时限 | 过滤器见上表，动作 5 秒，接口 10 秒，定时任务 60 秒 |
| 连续崩溃 | 5 次自动停用，原因写进后台列表 |

- **「崩溃」只算超时、陷入（trap）与违反调用约定。** 你的处理函数返回 `error` 不算崩溃，
  只是这一次没做成，日志里能看到。
- **实例全忙不算崩溃。** 并发把 4 个实例占满、排队排到超时，接口回 503，插件照常运行——
  公开接口被刷的时候不该因此把插件停掉。
- 被自动停用后，后台的插件列表里会写明原因（连续出错、新版本多要了能力、后端加载失败）。
  修好后由站长手动启用。

---

## 构建与安装

```bash
GOOS=wasip1 GOARCH=wasm CGO_ENABLED=0 \
  go build -buildmode=c-shared -o plugin.wasm .
```

三个环境变量都要给：宿主只加载 wasip1 上的 c-shared 反应器模块。把 `plugin.yaml`、`plugin.wasm`
以及设置、静态文件按[包结构](#插件包结构)打成 zip，到后台「插件 → 安装插件」上传。

示例插件的打包脚本可以直接用或照改：

```bash
sh examples/plugins/build.sh visit-stats
```

装好之后：

- 新装的插件是**停用**状态。启用时，声明了能力的会把能力逐条列出来，站长确认后才加载。
- **升级同一个插件**保持原来的启用状态，除非新版本多要了能力——那就先停用、等再次确认。
- **卸载**时确认框会列出这个插件名下有多少设置、记录与键值，缺省连数据一起删，
  可以勾「保留数据」；保留的数据列在插件页里，可以单独删，重装同名插件时原样接回。

---

## 安全约定

插件代码在沙箱里跑，能碰到什么由 `capabilities` 一项项定，站长逐项确认。你要照看的几件事：

- **别把密钥写进清单或代码。** 清单是明文的，`plugin.wasm` 也能被下载解出来。
  要密钥就让站长填在设置里，用 `lumo.SettingsInto` 读。
- **净化过的片段别再拼未转义的输入。** 宿主会再净化一遍，但你写出来的 HTML 里不该直接
  塞评论正文这类外部内容——`html.EscapeString` 一下。
- **接口的响应头由宿主把关**，`Set-Cookie` 与 CORS 头写不进去。撞上这条说明设计走偏了：
  插件接口与站点同源，不该由插件来动会话或放开跨站。
- **`http.fetch` 拦的是解析出来的 IP**，所以别指望用域名绕过内网限制。
- **依赖要写版本范围。** 不写就是「任何版本都行」，下一个大版本改了数据格式，你的插件会静默出错。

---

## 常见问题

**登记了处理函数，启用时报「清单没有声明它」。**
钩子在 `spec.hooks`，定时任务在 `spec.cron`，接口在 `spec.routes`，
插槽 / 小组件 / 短代码在 `spec.frontend`。声明与登记必须两两对上。

**插槽里没有内容，代码也跑了。**
主题没在页面里调 `.Slot`。内置主题「墨」都接上了；换主题时先确认它留了口子，
见[给插件留位置](./theme-development.md#给插件留位置)。

**前台脚本没跑起来。**
内联脚本会被净化掉，要带一个 `static/` 下的 `.js` 文件并在 `spec.frontend.scripts` 里声明。
另外脚本是 `defer` 的，别指望它在页面元素之前执行。

**插件启用不了，说「依赖的插件没有就绪」。**
去插件页看它声明的依赖现状：没装就装，没启用就启用，版本不够就升/降。

**`http.fetch` 被拒。**
域名要在 `capabilities.http` 里声明（精确域名或 `*.example.com`），写 IP 不行；
重定向的每一跳都要在白名单里；解析到内网也会被拒。

**邮件发不出去。**
站点要先配好 SMTP；本插件每小时 30 封、每封最多 10 个收件人的上限也不可绕过。
具体原因在后台的日志页里。

**改了静态文件，浏览器还是旧的。**
`?v=` 取的是插件版本，所以**改了静态文件就把版本号往上走一格**再装。

**插件改完重装，设置和数据显示「接回了」。**
那是卸载时勾了「保留数据」的效果。不想保留就在卸载时不勾，或者到插件页把保留的数据删掉。
