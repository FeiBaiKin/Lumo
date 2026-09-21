# 附加许可：插件与主题接口例外

Lumo —— 用 Go 编写的现代化开源 CMS
Copyright (C) 2026 FeiBaiKin

本程序是自由软件：你可以依据自由软件基金会发布的 GNU Affero 通用公共许可证
（第 3 版，或你选择的任何更新版本）的条款重新分发和/或修改它，并受本文所载
「附加许可」约束。许可证原文见 [LICENSE](../LICENSE)。

本程序的分发目的是希望它有用，但不附带任何担保；甚至不包含对适销性或特定
用途适用性的默示担保。详见 GNU Affero 通用公共许可证。

如果 AGPL-3.0 的条款不适用于你的场景，可以另行获取商业授权，
详见 [COMMERCIAL.md](./COMMERCIAL.md)。

本例外只授予代码上的权利，**不授予任何商标权利**——「Lumo」名称与标识的使用规则见
[商标政策](./TRADEMARK.md)。写给主题与插件作者的实用说明见 [THEMES.md](./THEMES.md)。

---

## 中文

依据 AGPL-3.0 第 7 条，作为附加许可，作者授予如下例外：

> 仅通过 Lumo 的插件接口或主题接口与 Lumo 交互的独立作品（下称「接口作品」），
> 不因该交互本身而被视为本程序的衍生作品或修改版本。你可以以你选择的任何条款
> 开发、分发和销售接口作品，包括专有条款，而不因此承担 AGPL-3.0 的义务。
>
> 就本例外而言：
>
> 「插件接口」指 Lumo 用于加载、配置并调用插件包的既定机制，包括插件清单格式、
> 设置声明格式，以及 Lumo 为插件定义的扩展点与运行时接口。
>
> 「主题接口」指 Lumo 的模板引擎向主题暴露的模板函数、上下文数据结构、布局与
> 片段约定，以及主题包格式。
>
> 本例外不适用于：
>
> (a) 对 Lumo 自身源代码的任何修改——这些修改仍完全受 AGPL-3.0 约束；
> (b) 与 Lumo 静态或动态链接为同一可执行程序的代码，除非该代码仅通过上述
>     插件接口或主题接口与 Lumo 交互；
> (c) 复制自或改编自 Lumo 源代码的接口作品。
>
> 若你修改了本程序，你可以选择把本例外一并延用到你的版本，但没有义务这样做。
> 如果你不愿延用，请从你的版本中删除本例外声明。

## English

As an additional permission under section 7 of the GNU Affero General Public
License version 3, the author grants the following exception:

> Independent works that interact with Lumo solely through Lumo's Plugin
> Interface or Theme Interface (each, an "Interface Work") are not, by reason
> of that interaction alone, considered derivative works or modified versions
> of this Program. You may develop, distribute, and sell Interface Works under
> terms of your choosing, including proprietary terms, without thereby becoming
> subject to the obligations of the GNU Affero General Public License.
>
> For the purposes of this exception:
>
> "Plugin Interface" means the mechanisms by which Lumo loads, configures, and
> invokes plugin packages, including the plugin manifest format, the settings
> declaration format, and the extension points and runtime interfaces that Lumo
> defines for plugins.
>
> "Theme Interface" means the template functions, context data structures,
> layout and partial conventions, and theme package format that Lumo's template
> engine exposes to themes.
>
> This exception does NOT apply to:
>
> (a) modifications to Lumo's own source code, which remain fully subject to the
>     GNU Affero General Public License;
> (b) code linked with Lumo into a single executable, whether statically or
>     dynamically, except where that code interacts with Lumo solely through the
>     Plugin Interface or Theme Interface as defined above;
> (c) Interface Works that copy from or are adapted from Lumo's source code.
>
> If you modify this Program, you may extend this exception to your version, but
> you are not obligated to do so. If you do not wish to do so, delete this
> exception statement from your version.

---

如中英文本产生歧义，以英文本为准。
In case of any discrepancy between the Chinese and English texts, the English
text shall prevail.
