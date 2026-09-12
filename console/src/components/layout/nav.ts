import type { components } from "@/api/schema";
import { type LucideIcon, resolveIcon } from "@/lib/icons";

/**
 * 侧边栏菜单的形态与换算。
 *
 * 菜单**由服务端给出**（见 internal/app/nav.go 与 internal/console/nav.go），
 * 前端不再维护一份硬编码清单。理由是插件：插件的页面要出现在侧边栏里，
 * 而前端不可能事先知道装了哪些插件。清单一旦写死在前端，就必须与后端的模块构成
 * 保持同步，而那种同步只能靠人记着。
 *
 * 本文件只做两件事：把接口给的图标名换成组件，以及判断「当前在哪一项上」。
 * 排序、分组、权限过滤都不在这里——排序由服务端排好，权限由调用方按各自的语义过滤。
 */

type NavGroupWire = components["schemas"]["NavGroupView"];
type NavItemWire = components["schemas"]["NavItemView"];

/** 渲染用的菜单项：图标已解析成组件。 */
export type NavItem = {
  key: string;
  label: string;
  to: string;
  icon: LucideIcon;
  /** 显示所需的权限；留空表示所有已登录用户可见。 */
  permission?: string | undefined;
  keywords?: string | undefined;
  description?: string | undefined;
  /** 只在路径完全相等时高亮，用于「概览」这类根路径项。 */
  end?: boolean | undefined;
};

/** 渲染用的分组。 */
export type NavGroup = {
  name: string;
  label: string;
  items: NavItem[];
};

/**
 * 把接口返回的分组与菜单项拼成渲染用的结构。
 *
 * 服务端已经排好序，这里只做两件事：解析图标，以及把不属于任何已声明分组的菜单项丢掉。
 * 丢掉而不是塞进一个「其他」组：分组没声明出来多半是声明方的疏漏，
 * 此时把它藏起来并让契约测试报错，比在界面上多出一个来路不明的分组好。
 */
export function buildNavigation(
  groups: NavGroupWire[] | null | undefined,
  items: NavItemWire[] | null | undefined,
): NavGroup[] {
  const byName = new Map<string, NavGroup>();
  const out: NavGroup[] = [];
  for (const group of groups ?? []) {
    const built: NavGroup = { name: group.name, label: group.label, items: [] };
    byName.set(group.name, built);
    out.push(built);
  }
  for (const item of items ?? []) {
    const group = byName.get(item.group);
    if (!group || item.hidden) {
      continue;
    }
    group.items.push({
      key: item.key,
      label: item.label,
      to: item.path,
      icon: resolveIcon(item.icon),
      permission: item.permission || undefined,
      keywords: item.keywords || undefined,
      description: item.description || undefined,
      end: item.end || undefined,
    });
  }
  return out;
}

/**
 * 把菜单项拍平成「路径 → 名称」，供文档标题与面包屑使用。
 *
 * 隐藏项也在内：个人中心不进侧边栏，但它确实是一个页面，需要有标题。
 */
export function routeLabels(
  items: NavItemWire[] | null | undefined,
): Record<string, string> {
  const out: Record<string, string> = {};
  for (const item of items ?? []) {
    out[item.path] = item.label;
  }
  return out;
}

/** 判断某个菜单项是否对应当前路径。 */
export function isActivePath(
  item: { to: string; end?: boolean | undefined },
  pathname: string,
): boolean {
  if (item.end) {
    return item.to === pathname;
  }
  return pathname === item.to || pathname.startsWith(`${item.to}/`);
}
