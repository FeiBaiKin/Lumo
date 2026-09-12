package app

import (
	"fmt"
	"regexp"
	"sort"
)

// NavGroup 是 Console 侧边栏的一个分组。
//
// 分组由模块声明而不是前端写死：插件的页面要能出现在侧边栏里，
// 而它归哪一组只有插件自己知道。前端因此只负责把收到的东西画出来。
type NavGroup struct {
	// Name 是分组标识（DNS-1123），菜单项用它指认自己属于哪一组。
	Name string
	// Label 是分组显示名。
	Label string
	// Order 决定分组顺序，数值小者在前。
	Order int
}

// NavItem 是 Console 侧边栏的一个菜单项。
type NavItem struct {
	// Key 是菜单项的稳定标识，同一分组内唯一。供前端做 key 与诊断用。
	Key string
	// Label 是菜单项显示名。
	Label string
	// Path 是 Console 内的绝对路径，如 /posts。
	//
	// 必须是绝对路径：前端以 basename=/console 挂载 SPA，
	// 相对路径会被解析到当前页之下。
	Path string
	// Icon 是图标名，取值来自 console/src/lib/icons.ts 的登记表。
	//
	// 后端只能给出名字，而两侧之间没有编译期约束：写了登记表里没有的名字，
	// 前端只会画一个默认图标，界面上不会有任何提示。故由 cmd/lumo 的契约测试盯着。
	Icon string
	// Group 是所属分组的 Name。
	Group string
	// Order 决定同组内的顺序，数值小者在前。
	Order int
	// Permission 是显示所需的权限串；留空表示所有已登录用户可见。
	//
	// 这是**显示条件而非安全边界**：没有权限就不显示入口，真正的拦截在服务端。
	// 前端隐藏按钮只是少让人白跑一趟。
	Permission string
	// Keywords 是命令面板的检索关键词（含拼音），便于中文输入习惯。
	Keywords string
	// Description 是一句话说明，供命令面板与悬停提示使用；可留空。
	Description string
	// End 为真时只在路径完全相等时高亮，用于「概览」这类根路径项。
	End bool
	// Hidden 为真时不进侧边栏，但仍参与命令面板与标题映射（如个人中心）。
	Hidden bool
}

// Navigation 是一批菜单声明。
type Navigation struct {
	Groups []NavGroup
	Items  []NavItem
}

// NavigationProvider 声明模块贡献的 Console 菜单。
//
// 与 SettingsProvider 并列：模块有什么页面，就该由它自己说，
// 而不是让前端维护一份必须与后端能力保持同步的硬编码清单。
type NavigationProvider interface {
	Navigation() Navigation
}

var navNamePattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// Navigation 汇总全部模块声明的菜单，按分组顺序与组内顺序排好。
//
// 每次调用都重新问一遍各模块，而不是在注册期收集它们的返回值：
// 设置分组要等 Start 时才登记（见 internal/settings），
// 而设置项对应的菜单入口正是从那份分组列表推导出来的。注册期收集会漏掉它们。
//
// 菜单是给人看的，顺序错了顶多难看；但标识别扭了会让前端静默丢项，
// 故对声明做一遍校验，把问题在测试与启动日志里说清楚。
func (a *App) Navigation() Navigation {
	// 核心的分组与核心页面先行：它们无论如何都在，模块只是在上面添砖。
	out := CoreNavigation()
	seenGroups := make(map[string]bool, len(out.Groups))
	for _, group := range out.Groups {
		seenGroups[group.Name] = true
	}
	seenItems := make(map[string]bool, len(out.Items))
	for i := range out.Items {
		seenItems[out.Items[i].Key] = true
	}

	for _, provider := range a.navProviders {
		declared := provider.Navigation()
		for _, group := range declared.Groups {
			if err := validateNavGroup(group); err != nil {
				a.logger.Warn("忽略非法的菜单分组", "group", group.Name, "error", err)
				continue
			}
			if seenGroups[group.Name] {
				continue
			}
			seenGroups[group.Name] = true
			out.Groups = append(out.Groups, group)
		}
		// 按下标取址而不是按值取：NavItem 有一百多字节，逐项复制是白费。
		for i := range declared.Items {
			item := &declared.Items[i]
			if err := validateNavItem(item); err != nil {
				a.logger.Warn("忽略非法的菜单项", "item", item.Key, "error", err)
				continue
			}
			if seenItems[item.Key] {
				a.logger.Warn("忽略重复的菜单项", "item", item.Key)
				continue
			}
			seenItems[item.Key] = true
			out.Items = append(out.Items, *item)
		}
	}

	sort.SliceStable(out.Groups, func(i, j int) bool {
		if out.Groups[i].Order != out.Groups[j].Order {
			return out.Groups[i].Order < out.Groups[j].Order
		}
		return out.Groups[i].Name < out.Groups[j].Name
	})
	order := make(map[string]int, len(out.Groups))
	for i, group := range out.Groups {
		order[group.Name] = i
	}
	sort.SliceStable(out.Items, func(i, j int) bool {
		gi, gj := order[out.Items[i].Group], order[out.Items[j].Group]
		if gi != gj {
			return gi < gj
		}
		if out.Items[i].Order != out.Items[j].Order {
			return out.Items[i].Order < out.Items[j].Order
		}
		return out.Items[i].Key < out.Items[j].Key
	})
	return out
}

// validateNavGroup 校验分组声明的形态。
func validateNavGroup(group NavGroup) error {
	if !navNamePattern.MatchString(group.Name) {
		return fmt.Errorf("分组名须为 DNS-1123，实际 %q", group.Name)
	}
	if group.Label == "" {
		return fmt.Errorf("分组 %q 缺少显示名", group.Name)
	}
	return nil
}

// validateNavItem 校验菜单项声明的形态。
//
// 路径必须是绝对路径且不带查询串：前端拿它直接喂给 react-router，
// 相对路径会被解析到当前页之下，而其表现是「点了没反应」。
func validateNavItem(item *NavItem) error {
	if !navNamePattern.MatchString(item.Key) {
		return fmt.Errorf("标识须为 DNS-1123，实际 %q", item.Key)
	}
	if item.Label == "" {
		return fmt.Errorf("菜单项 %q 缺少显示名", item.Key)
	}
	if item.Group == "" {
		return fmt.Errorf("菜单项 %q 未指定分组", item.Key)
	}
	if item.Path == "" || item.Path[0] != '/' {
		return fmt.Errorf("菜单项 %q 的路径须以 / 开头，实际 %q", item.Key, item.Path)
	}
	if item.Icon == "" {
		return fmt.Errorf("菜单项 %q 缺少图标名", item.Key)
	}
	return nil
}

// Console 侧边栏的分组标识（agent.md §8 的七组页面地图）。
//
// 分组定义在核心而不是各模块各写一份：分组是**版面的骨架**，
// 谁往里放东西是模块的事，骨架长什么样只该有一处说了算。
// 插件要新开一组时自己声明一个 NavGroup，名称不冲突即可。
const (
	NavGroupDashboard  = "dashboard"
	NavGroupContent    = "content"
	NavGroupMedia      = "media"
	NavGroupAppearance = "appearance"
	NavGroupUsers      = "users"
	NavGroupSettings   = "settings"
	NavGroupSystem     = "system"
)

// NavGroups 返回内置的七组，顺序即侧边栏自上而下的顺序。
//
// 用分组而非扁平列表，是为了插件的页面有确定的安放位置——
// 届时插件只需声明自己属于哪一组，侧边栏不必改结构。
func NavGroups() []NavGroup {
	return []NavGroup{
		{Name: NavGroupDashboard, Label: "仪表盘", Order: 0},
		{Name: NavGroupContent, Label: "内容", Order: 10},
		{Name: NavGroupMedia, Label: "媒体", Order: 20},
		{Name: NavGroupAppearance, Label: "外观", Order: 30},
		{Name: NavGroupUsers, Label: "用户", Order: 40},
		{Name: NavGroupSettings, Label: "设置", Order: 50},
		{Name: NavGroupSystem, Label: "系统", Order: 60},
	}
}

// CoreNavigation 返回核心自身的菜单声明。
//
// 用户、角色、关于、日志这几页由核心实现，没有对应的功能模块，故在这里声明——
// 与 app.CorePermissions 同一个理由。其余各页由各自的功能模块声明。
func CoreNavigation() Navigation {
	return Navigation{
		Groups: NavGroups(),
		Items: []NavItem{
			{
				Key: "overview", Label: "概览", Path: "/", Icon: "layout-dashboard",
				Group: NavGroupDashboard, End: true, Keywords: "dashboard gailan shouye home",
			},
			{
				Key: "users", Label: "用户", Path: "/users", Icon: "users",
				Group: NavGroupUsers, Order: 10, Permission: "users:manage",
				Keywords: "users yonghu",
			},
			{
				Key: "roles", Label: "角色", Path: "/roles", Icon: "shield-check",
				Group: NavGroupUsers, Order: 20, Permission: "roles:manage",
				Keywords: "roles juese quanxian",
			},
			{
				Key: "about", Label: "关于", Path: "/about", Icon: "info",
				Group: NavGroupSystem, Order: 10, Keywords: "about guanyu banben",
			},
			{
				Key: "logs", Label: "日志", Path: "/logs", Icon: "scroll-text",
				Group: NavGroupSystem, Order: 20, Permission: "settings:manage",
				Keywords: "logs rizhi",
			},
			{
				Key: "profile", Label: "个人中心", Path: "/profile", Icon: "user-round",
				Group: NavGroupUsers, Hidden: true,
				Keywords: "profile geren zhanghao mima lingpai token",
			},
		},
	}
}
