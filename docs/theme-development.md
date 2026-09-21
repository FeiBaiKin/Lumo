# Lumo 主题开发

Lumo 的访客前台由服务端模板渲染。一个主题就是一个 zip 包，后台上传即可切换，
**不需要重启，也不需要碰服务器上的任何文件**。

本文是主题作者的完整参考。授权、能否闭源、能否出售那些问题在 [THEMES.md](../THEMES.md)，
这里只讲怎么写。

---

## 目录

- [第一个主题](#第一个主题)
- [主题包结构](#主题包结构)
- [主题清单](#主题清单)
- [模板](#模板)
- [上下文](#上下文)
- [Finder 取数](#finder-取数)
- [模板函数](#模板函数)
- [主题设置](#主题设置)
- [静态资源](#静态资源)
- [开发模式](#开发模式)
- [打包与安装](#打包与安装)
- [安全约定](#安全约定)
- [常见问题](#常见问题)

---

## 第一个主题

最小可用的主题只需要五个文件：

```
mytheme/
├── theme.yaml
└── templates/
    ├── index.html
    ├── post.html
    ├── page.html
    └── 404.html
```

`theme.yaml`：

```yaml
name: mytheme
label: 我的主题
version: 1.0.0
```

`templates/index.html`：

```html
<!doctype html>
<html lang="{{ .Site.Language }}">
<head>
  <meta charset="utf-8">
  <title>{{ .Site.Title }}</title>
</head>
<body>
  <h1>{{ .Site.Title }}</h1>
  {{ range .Posts }}
    <article>
      <h2><a href="{{ .URL }}">{{ .Title }}</a></h2>
      <time datetime="{{ date "rfc3339" .PublishedAt }}">{{ date "chinese" .PublishedAt }}</time>
      <p>{{ .Excerpt }}</p>
    </article>
  {{ else }}
    <p>还没有内容。</p>
  {{ end }}
</body>
</html>
```

`post.html`、`page.html` 里把正文渲染出来，`404.html` 给一句「页面不存在」，
就能装上跑起来了。

把 `mytheme/` 目录**里面的内容**打成 zip（不是把目录本身打进去），
后台 → 主题 → 上传即可。

---

## 主题包结构

```
theme.yaml          必需  主题元信息
settings.yaml       可选  主题自己的设置项声明
screenshot.png      可选  后台主题列表里的预览图
templates/          必需  模板目录
  layouts/          可选  外层骨架
  partials/         可选  可复用片段
static/             可选  CSS、JS、字体、图片
```

`theme.yaml` 必须在 zip 的**根目录**，不能套一层文件夹。

---

## 主题清单

`theme.yaml` 放在包的根目录：

```yaml
name: mytheme          # 必需。小写字母、数字、连字符（DNS-1123），同时是安装目录名与 URL 片段
label: 我的主题         # 显示名。留空则用 name
version: 1.0.0         # 必需。形如 1、1.0、1.0.0，可带 -beta.1 之类的预发布标记
description: 一句话说明  # ≤ 500 字
author: 你的名字        # ≤ 64 字
homepage: https://...  # 主题主页
repo: https://...      # 源码仓库
license: MIT           # 许可证标识
requireLumo: 0.1.0     # 所需的最低 Lumo 版本，留空表示不限制
```

**未知字段一律报错。** 把 `label` 拼成 `lable` 不会被静默忽略，安装时就会告诉你——
否则你会对着一个没有显示名的主题找半天。

---

## 模板

模板用 Go 的 [`html/template`](https://pkg.go.dev/html/template)。

选它而不是别的模板语言，是因为主题是第三方代码，XSS 面就在主题里，
而 `html/template` 是少数做**上下文感知转义**的实现：同一个变量出现在
HTML 文本、属性、URL、JavaScript 里，它会各自用不同的转义规则。
代价是模板语法不如 Jinja 顺手，Lumo 用 Hugo 风格的 layout/partial 约定和一套函数库来补。

### 必需与可选模板

**必需四个**，缺任何一个主题都装不上：

| 模板 | 页面 |
|---|---|
| `index.html` | 首页 |
| `post.html` | 文章详情 |
| `page.html` | 独立页面 |
| `404.html` | 找不到 |

**可选十一个**，不提供就**整页回退**到内置主题「墨」的同名模板：

| 模板 | 页面 |
|---|---|
| `category.html` | 分类归档 |
| `tag.html` | 标签归档 |
| `archive.html` | 时间归档 |
| `search.html` | 搜索结果 |
| `author.html` | 作者归档 |
| `login.html` | 登录 |
| `register.html` | 注册 |
| `forgot-password.html` | 找回密码 |
| `reset-password.html` | 重置密码 |
| `account.html` | 个人中心 |
| `favorites.html` | 我的收藏 |

回退是**整页**而不是逐块——回退的页面会长得像另一个主题。
所以真要发布的主题应该把这十一个也写了，至少写掉前五个。

### layout 与 partial

Go 的模板没有继承，只有「定义块 + 调用块」。Lumo 的约定是：
**页面模板调用骨架，骨架回调页面定义的 `main` 块**。

`templates/layouts/base.html`：

```html
<!doctype html>
<html lang="{{ .Site.Language }}">
<head>
  <meta charset="utf-8">
  <title>{{ if .Title }}{{ .Title }} · {{ end }}{{ .Site.Title }}</title>
  <link rel="stylesheet" href="{{ .Theme.AssetsBase }}/theme.css?v={{ .Theme.AssetsVersion }}">
</head>
<body>
  {{ template "partials/header.html" . }}
  <main>{{ block "main" . }}{{ end }}</main>
  {{ template "partials/footer.html" . }}
</body>
</html>
```

`templates/index.html`：

```html
{{ template "layouts/base.html" . }}

{{ define "main" }}
  {{ range .Posts }}
    <h2><a href="{{ .URL }}">{{ .Title }}</a></h2>
  {{ end }}
{{ end }}
```

几条规则：

- 模板按**相对路径**命名。`templates/partials/header.html` 就写
  `{{ template "partials/header.html" . }}`，和 Hugo 的心智模型一致。
- `layouts/` 和 `partials/` 下的文件是**共享模板**，不作为页面入口；
  其余 `.html` 都是页面模板。
- 每个页面模板拥有**自己独立的一套模板集合**，共享模板在每套里各解析一份。
  所以 `index.html` 和 `post.html` 可以各自 `define "main"` 而不会互相覆盖——
  若全主题共用一套集合，后解析的那个 `main` 会顶掉前一个，
  表现为「所有页面都渲染成同一个」。
- **记得把 `.` 传下去。** `{{ template "partials/header.html" . }}` 里那个点不能省，
  否则 partial 拿到的是 nil，`.Site.Title` 会渲染成空。
- 在 `range` / `with` 内部，`.` 已经变成当前元素。要访问根上下文用 `$`：
  `{{ range .Posts }}{{ $.Site.Title }}{{ end }}`。

### 页面专用模板

独立页面可以在后台选一个专用模板（WordPress 的做法）。
主题提供 `templates/page-about.html`，站长在页面编辑器里选中它，
`/about` 就用这个模板渲染。

命名规则是 `page-<任意标识>.html`。模板不存在时**回退到 `page.html`** 而不是报错——
主题换了之后旧页面还得能打开。

---

## 上下文

每个页面拿到同一个结构体，模板里以 `.` 访问。
与当前路由无关的字段是零值，`{{ with }}` 会自然跳过。

所有路由共用一个结构而不是每种页面一个类型，是为了让 partial 能无差别地接收
任何页面的上下文——页眉页脚要用 `.Site` 和菜单，而它们在每种页面上都得在。

### 每种页面拿到什么

`.Kind` 是当前路由种类，partial 里可以据此分支。

| `.Kind` | 模板 | 额外填充的字段 |
|---|---|---|
| `index` | `index.html` | `.Posts` `.Pagination` |
| `post` | `post.html` | `.Post` |
| `page` | `page.html` / `page-*.html` | `.Post` |
| `category` | `category.html` | `.Category` `.Posts` `.Pagination` |
| `tag` | `tag.html` | `.Tag` `.Posts` `.Pagination` |
| `archive` | `archive.html` | `.Archive` `.Posts` `.Pagination` |
| `author` | `author.html` | `.Author` `.Posts` `.Pagination` |
| `search` | `search.html` | `.Query` `.Posts` `.Pagination` |
| `favorites` | `favorites.html` | `.Posts` `.Pagination` |
| `404` | `404.html` | — |
| `login` `register` `forgot-password` `reset-password` `account` | 同名模板 | `.Form` |

`.Site`、`.Theme`、`.SEO`、`.Path`、`.Find`、`.Public`、`.CurrentUser`
在**每个**页面上都有。

### 字段速查

**根上下文**

| 字段 | 说明 |
|---|---|
| `.Kind` | 路由种类，见上表 |
| `.Title` | 本页标题，不含站点名后缀，主题自己拼 |
| `.Description` | 本页描述，供 meta 用。为空时服务端已回落到 SEO 默认描述或站点描述 |
| `.Canonical` | 本页绝对地址；站点未配置对外地址时是相对路径 |
| `.Path` | 当前请求路径，可用来给导航标「当前项」 |
| `.Post` | 文章页与独立页面上是当前内容，其余为 nil |
| `.Posts` | 列表类页面的内容列表 |
| `.Pagination` | 翻页信息 |
| `.Category` `.Tag` `.Author` `.Archive` | 对应归档页上的当前对象 |
| `.Query` | 搜索词 |
| `.CurrentUser` | 当前登录用户，匿名时 nil |
| `.CSRFToken` | 页面自带表单要用的令牌，只对已登录访客签发 |
| `.Form` | 表单页的回填值、逐字段错误与提示 |

**`.Site`**

`Title` `Subtitle` `Description` `URL` `Language` `LogoURL` `FaviconURL`
`Now`（已按站点时区换算）`Generator`。

**`.Theme`**

`Name` `Label` `Version` `Settings` `AssetsBase` `AssetsVersion`。

**`.SEO`**

`TitleSuffix` `Image` `TwitterSite` `JSONLD`。服务端按站点设置与本页内容算好，
主题不必自己去 `.Public.seo` 里挖那几项再做回落。直接渲染：

```html
{{ with .SEO.JSONLD }}<script type="application/ld+json">{{ . }}</script>{{ end }}
```

**`.Post` 与 `.Posts` 的元素**

| 字段 | 说明 |
|---|---|
| `ID` `Type` `Title` `Slug` | |
| `URL` | 站内路径，如 `/posts/hello` |
| `Content` | 渲染后的 HTML，**必须用 `safeHTML` 输出** |
| `Excerpt` | 摘要 |
| `CoverURL` | 封面图，可能为空 |
| `Pinned` | 是否置顶 |
| `Author` | `ID` `Username` `DisplayName` `AvatarURL` `Bio` `URL`。**不含邮箱** |
| `Categories` | 分类列表 |
| `Tags` | 标签列表，每枚带 `Hue`（色相 0–359） |
| `PublishedAt` `UpdatedAt` | 已按站点时区换算 |
| `ReadingTime` | 估算阅读分钟数 |
| `WordCount` | 正文字数，CJK 按字、拉丁按词 |

**`.Pagination`**

`Page` `Size` `Total` `TotalPages` `HasPrev` `HasNext` `PrevURL` `NextURL` `BaseURL`，
外加两个方法：

- `.PageURL n` —— 第 n 页的地址
- `.Pages` —— 页码条上该显示的页码，超长时以 `0` 表示省略号

分页边界是最容易写错的地方，所以全部预先算好了。页码条这么写：

```html
{{ with .Pagination }}{{ if gt .TotalPages 1 }}
<nav>
  {{ if .HasPrev }}<a href="{{ .PrevURL }}">上一页</a>{{ end }}
  {{ range .Pages }}
    {{ if eq . 0 }}<span>…</span>
    {{ else if eq . $.Pagination.Page }}<span aria-current="page">{{ . }}</span>
    {{ else }}<a href="{{ $.Pagination.PageURL . }}">{{ . }}</a>{{ end }}
  {{ end }}
  {{ if .HasNext }}<a href="{{ .NextURL }}">下一页</a>{{ end }}
</nav>
{{ end }}{{ end }}
```

第一页不带查询参数：`/posts` 与 `/posts?page=1` 是同一个页面，
让它们有两个地址会稀释搜索引擎的权重。

---

## Finder 取数

侧栏、页脚、相关文章这些地方需要的数据不在路由上下文里。
`.Find` 是只读数据访问面：

```html
{{ range .Find.Posts.Recent 5 }}
  <a href="{{ .URL }}">{{ .Title }}</a>
{{ end }}
```

**刻意不提供任意查询。** 能调的每个函数都在下面逐个列出，各自带缓存和条数上限。
开放一个 `query` 会让主题变成应用——慢查询、N+1、越权读取都会跟着进来，
而出了问题排查成本落在站长身上。

单次调用最多返回 **100 条**，超过的部分截断。结果在**本次请求内**缓存，
页眉页脚取同一个菜单不会查两次库；不跨请求缓存，否则站长改了菜单要等一分钟才生效。

### posts

| 调用 | 返回 |
|---|---|
| `.Find.Posts.Recent n` | 最近发布的 n 篇 |
| `.Find.Posts.Pinned n` | 置顶文章 |
| `.Find.Posts.Popular n` | 按评论数排序的热门 |
| `.Find.Posts.Related $id n` | 与该文共享分类或标签最多的其他文章 |
| `.Find.Posts.ByCategory $slug n` | 某分类下的文章 |
| `.Find.Posts.ByTag $slug n` | 某标签下的文章 |
| `.Find.Posts.Adjacent $id $publishedAt` | 上一篇与下一篇，取 `.Prev` `.Next` |
| `.Find.Posts.Total` | 已发布且公开的文章总数 |

文章页的翻页链接：

```html
{{ $adj := .Find.Posts.Adjacent .Post.ID .Post.PublishedAt }}
{{ with $adj.Prev }}<a href="{{ .URL }}">← {{ .Title }}</a>{{ end }}
{{ with $adj.Next }}<a href="{{ .URL }}">{{ .Title }} →</a>{{ end }}
```

### categories / tags / archives

| 调用 | 返回 |
|---|---|
| `.Find.Categories.Tree` | 分类树，节点带 `Children` 与 `Count` |
| `.Find.Categories.All` | 平铺的分类列表 |
| `.Find.Tags.All` | 全部标签，按名称排序 |
| `.Find.Tags.Cloud n` | 按文章数倒序的前 n 个标签 |
| `.Find.Archives.ByMonth` | 按月归档，新的在前 |
| `.Find.Archives.ByYear` | 按年归档 |

分类与标签的元素有 `ID` `Name` `Slug` `Description` `URL` `Count`，
标签另有 `Color` 与 `Hue`，分类另有 `CoverURL`。

标签云的热度只由**顺序**表达，不要用字号编码——一排标签大小不一会让整屏参差。
每枚标签带 `Hue`（0–359），用它拼颜色：

```html
{{ range .Find.Tags.Cloud 20 }}
  <a href="{{ .URL }}" style="--hue: {{ .Hue }}">{{ .Name }}</a>
{{ end }}
```

站长给标签设过颜色就用那个颜色的色相，没设过由标识稳定算出。
只取色相、明度彩度由主题定，是为了让一排标签的对比度一致——
站长给的浅黄直接拿来当文字色会读不清。

### menus / comments / favorites

| 调用 | 返回 |
|---|---|
| `.Find.Menus.Get "primary"` | 按 slug 取菜单树。菜单不存在返回空切片，不报错 |
| `.Find.Comments.Tree $id` | 某条内容下已过审的评论树 |
| `.Find.Comments.Count $id` | 已过审评论数 |
| `.Find.Comments.Recent n` | 站点最近的评论，带 `PostTitle` `PostURL` |
| `.Find.Favorites.Enabled` | 站点是否装配了收藏功能 |
| `.Find.Favorites.Count $id` | 被收藏次数 |
| `.Find.Favorites.Has $id` | **当前登录用户**是否收藏过，匿名恒为 false |

菜单条目有 `Label` `URL` `Target` `Rel` `Children`。

收藏按钮先问 `Enabled`：站点没装配收藏模块时，
一个点了必然报错的按钮比没有这个按钮更糟。

评论节点有 `ID` `AuthorName` `AuthorURL` `ContentHTML` `CreatedAt` `IsAuthor` `Children`。
`ContentHTML` 入库前已转义并有限富化，用 `safeHTML` 输出。
**邮箱、IP、UA 一律不进模板**——给了主题就等于给了所有装这个主题的站点一个泄漏口子。

---

## 模板函数

**字符串**

`upper` `lower` `title` `trim` `contains` `hasPrefix` `hasSuffix` `replace`
`split` `join` `repeat` `truncate` `plainify`（去标签）`countWords` `readingTime`

**数值**

`add` `sub` `mul` `div` `mod` `min` `max` `seq` `ceil` `float`

**时间**

`now` `date` `year` `since`

`date` 接受格式名或 Go 格式串：

```
date "date"      → 2026-09-21
date "datetime"  → 2026-09-21 13:05
date "time"      → 13:05
date "chinese"   → 2026年9月21日
date "rfc3339"   → 2026-09-21T13:05:17+08:00
date "2006年1月"  → 2026年9月
```

Go 的参考时间写法（`2006-01-02`）第一次见没人猜得到，所以常用格式起了名字。
`since` 把时间差说成人话：「3 分钟前」「2 天前」。

**集合**

`first` `last` `slice` `len` `default` `dict` `list`

`dict` 用来给 partial 传多个值：

```html
{{ template "partials/card.html" (dict "Post" . "Compact" true) }}
```

partial 里就是 `.Post` 和 `.Compact`。

**URL 与转义**

`urlquery` `urlize` `absURL` `jsonify` `attrEmpty`
`safeHTML` `safeCSS` `safeURL` `safeAttr`

`safeHTML` 一族是你显式声明「这段内容我担保安全」的出口。
**只用在服务端已经处理过的内容上**：`.Post.Content` 和评论的 `.ContentHTML`
都经过按权限净化或转义富化，可以直接输出。把访客输入原样 `safeHTML` 就是开了一个 XSS。

---

## 主题设置

`settings.yaml` 让站长在后台调你的主题。用的是和站点设置**完全相同**的表单引擎，
所以只需要学一次。

```yaml
groups:
  - name: appearance
    label: 外观
    description: 配色与版式
    order: 0
    schema:
      $schema: https://json-schema.org/draft/2020-12/schema
      type: object
      x-order: [colorScheme, accent, contentWidth]
      additionalProperties: false
      properties:
        colorScheme:
          type: string
          title: 配色模式
          enum: [auto, light, dark]
          x-widget: select
          description: auto 跟随访客的系统设置
        accent:
          type: string
          title: 主题色
          x-widget: color
          maxLength: 32
        contentWidth:
          type: integer
          title: 正文栏宽
          minimum: 28
          maximum: 48
          description: 单位为汉字个数
    defaults:
      colorScheme: auto
      accent: "#2f6f4f"
      contentWidth: 36
```

Schema 是 JSON Schema 2020-12 的子集，加上 `x-` 扩展。模板里这样读：

```html
{{ .Theme.Settings.appearance.contentWidth }}
{{ with .Theme.Settings.appearance }}{{ if .motion }}data-motion="on"{{ end }}{{ end }}
```

几条要点：

- **`x-order` 决定字段顺序。** YAML 经 map 解析后键序会丢，不写这行字段顺序是随机的。
- **分组名是存储键。** 改一个名字等于把站长填过的值丢掉，所以只增不改。
- 最多 20 个分组。主题设置是给站长在一个页面里调的，
  几十个分组说明主题设计有问题。
- `x-widget` 选控件，共 18 种：`text` `textarea` `code` `secret` `select` `radio`
  `multiselect` `color` `image` `images` `date` `icon` `number` `slider` `switch`
  `list` `repeater` `group`。不写时按字段类型挑一个默认的。
- `x-show-if` 做条件显隐，语义与站点设置一致。
- `defaults` 里的值必须能通过 Schema 校验。

### 读站点的公开设置

有些开关不在你的主题里，但你得知道——比如页眉要不要显示「注册」入口。
`.Setting` 取站点各模块标为公开的字段：

```html
{{ if .Setting "account" "allowRegistration" }}<a href="/register">注册</a>{{ end }}
{{ if .Setting "comment" "enabled" }}{{ template "partials/comments.html" . }}{{ end }}
```

可读的分组与字段：

| 分组 | 字段 |
|---|---|
| `site` | `title` `subtitle` `description` `url` `language` `logoUrl` `faviconUrl` |
| `account` | `allowRegistration` `notice` |
| `comment` | `enabled` `allowAnonymous` `requireEmail` `requireApproval` `maxLength` |
| `seo` | `titleSuffix` `defaultDescription` `defaultImage` `twitterSite` |
| `mail` | `enabled` |

分组不存在或字段未公开时返回 nil，不报错——
第三方主题引用一个本项目不存在的分组时，该少显示一个链接，而不是让整站 500。

评论区是最需要它的地方：站点关掉评论、关掉访客评论、要求填邮箱，
这三种情形主题都得据此改变渲染，否则站长在后台关掉的东西前台照样显示。

---

## 静态资源

`static/` 下的文件挂在 `/theme-assets/<主题名>/` 下。模板里用 `.Theme.AssetsBase` 拼：

```html
<link rel="stylesheet" href="{{ .Theme.AssetsBase }}/theme.css?v={{ .Theme.AssetsVersion }}">
<script defer src="{{ .Theme.AssetsBase }}/main.js?v={{ .Theme.AssetsVersion }}"></script>
```

`.Theme.AssetsVersion` 是静态资源指纹，挂在 URL 上做缓存失效。
**别省掉它**，否则站长更新主题后访客拿到的还是旧 CSS。

目录不会被列出：`static/` 里可能有你不想给人看的中间产物。

---

## 开发模式

改一次模板重打一次 zip 上传，没法工作。开发模式让模板改动自动重载、静态资源不缓存：

```bash
LUMO_THEME_DEV=1 ./lumo serve
```

开着它的时候，直接改 `data/themes/<你的主题>/` 下的文件，刷新浏览器就能看到。

内置主题「墨」在首次启动时会解压一份到 `data/themes/ink`，**存在则以磁盘那份为准**。
所以可以直接改那份来试，改坏了在后台点「恢复出厂」。

> 拿「墨」当起点是最快的路径：它把九种路由、侧栏小组件、评论四态、分页、
> 标签色相、暗色模式都写全了。复制一份改个 `name` 就能开工。
>
> 注意它是 AGPL-3.0，基于它改出来的主题受同样的许可约束。
> 要写闭源或付费主题，从空白开始。

---

## 打包与安装

把主题目录**里面的内容**打成 zip：

```bash
cd mytheme
zip -r ../mytheme-1.0.0.zip . -x ".*" -x "__MACOSX/*"
```

zip 根目录下应该直接是 `theme.yaml`，不能套一层 `mytheme/`。

后台 → 主题 → 上传 → 启用。同名主题会覆盖，版本号不必递增也能装
（但发布给别人用时请递增）。

---

## 安全约定

主题包来自第三方，安装时有硬限制：

| 限制 | 值 |
|---|---|
| 包内文件数 | 2000 |
| 文件 + 目录总条目 | 2500 |
| 单文件解压后大小 | 8 MB |
| 整包解压后大小 | 64 MB |
| 路径层级 | 8 |

路径穿越（`../`）、符号链接、zip 炸弹都会被拒。

**模板渲染也有上限**：一个写坏的模板（比如对空切片无限递归）不会把内存吃光，
超出缓冲上限即视为模板有问题。

**主题不执行任何后端代码。** 能做的就是渲染模板和提供静态资源，
不能查任意数据、不能发请求、不能读文件。想要更多能力得走插件系统。

---

## 常见问题

**改了模板没反应？**
内置主题以 `data/themes/ink` 那份为准。要么开 `LUMO_THEME_DEV=1`，
要么在后台点「恢复出厂」重新解压。

**partial 里 `.Site.Title` 是空的？**
调用时漏了那个点：`{{ template "partials/header.html" . }}`。

**`range` 里访问不到根上下文？**
用 `$`：`{{ range .Posts }}{{ $.Site.Title }}{{ end }}`。

**文章正文渲染成了一堆标签源码？**
`.Post.Content` 要用 `safeHTML`：`{{ .Post.Content | safeHTML }}`。

**装不上，提示缺少必需模板？**
`index.html`、`post.html`、`page.html`、`404.html` 四个都要有，
且必须在 `templates/` 下，不能放进 `templates/pages/` 之类的子目录。

**日期显示的比实际早一天？**
`PublishedAt` 已经按站点时区换算过，直接 `date "chinese" .PublishedAt` 即可。
如果你自己做了时区转换，去掉它。
