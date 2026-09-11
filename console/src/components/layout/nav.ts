import {
  FileText,
  Files,
  FolderTree,
  HardDrive,
  Image,
  Info,
  LayoutDashboard,
  type LucideIcon,
  Mail,
  Menu as MenuIcon,
  MessageSquare,
  Palette,
  ScrollText,
  Search,
  Settings,
  ShieldCheck,
  Tags,
  Users,
} from "lucide-react";

/**
 * 侧边栏导航地图。
 *
 * 严格照 agent.md §8 的七组页面地图，不自行增减。
 * 用分组而非扁平列表，是为了 v1.1 接入插件页时有确定的安放位置 ——
 * 届时插件只需声明自己属于哪一组，侧边栏不必改结构。
 *
 * `permission` 是显示条件而非安全边界：没有权限就不显示入口，
 * 但真正的拦截在服务端。前端隐藏按钮只是少让人白跑一趟。
 */

export type NavItem = {
  label: string;
  to: string;
  icon: LucideIcon;
  /** 需要的权限；留空表示所有已登录用户可见。 */
  permission?: string;
  /** 只在精确匹配时高亮（用于「概览」这类根路径项）。 */
  end?: boolean;
};

export type NavGroup = {
  label: string;
  items: NavItem[];
};

export const NAV_GROUPS: NavGroup[] = [
  {
    label: "仪表盘",
    items: [{ label: "概览", to: "/", icon: LayoutDashboard, end: true }],
  },
  {
    label: "内容",
    items: [
      { label: "文章", to: "/posts", icon: FileText },
      { label: "页面", to: "/pages", icon: Files },
      // 分类与标签不设权限门槛：服务端对 Console 平面的读操作对任何已认证用户开放
      // （见 internal/taxonomy/handler.go 的说明 —— 作者写文章要能选分类）。
      // 写操作在页面内部按 taxonomies:manage 收起。
      { label: "分类", to: "/categories", icon: FolderTree },
      { label: "标签", to: "/tags", icon: Tags },
      { label: "评论", to: "/comments", icon: MessageSquare },
    ],
  },
  {
    label: "媒体",
    items: [{ label: "附件", to: "/media", icon: Image }],
  },
  {
    label: "外观",
    items: [
      // 主题的**全部**操作都要求 themes:manage（含列表）——
      // 主题能执行任意模板逻辑并决定整站外观，门槛与设置同级，
      // 故未持有时整项隐藏，点进去也只会得到 403。
      {
        label: "主题",
        to: "/themes",
        icon: Palette,
        permission: "themes:manage",
      },
      {
        label: "菜单",
        to: "/menus",
        icon: MenuIcon,
        permission: "menus:manage",
      },
    ],
  },
  {
    label: "用户",
    items: [
      { label: "用户", to: "/users", icon: Users, permission: "users:manage" },
      {
        label: "角色",
        to: "/roles",
        icon: ShieldCheck,
        permission: "roles:manage",
      },
    ],
  },
  {
    label: "设置",
    items: [
      // 设置的**全部**端点（含读取）都要求 settings:manage，见 internal/settings/handler.go
      // 里的 manage 中间件 —— 未授权用户不该看到站点的 SMTP 主机与存储配置。
      // 故这一组整体按权限显隐。侧栏只列四个常用分组，
      // 其余分组（如 comment）在设置页的标签栏里可以切到。
      {
        label: "站点",
        to: "/settings/site",
        icon: Settings,
        permission: "settings:manage",
      },
      {
        label: "SEO",
        to: "/settings/seo",
        icon: Search,
        permission: "settings:manage",
      },
      {
        label: "邮件",
        to: "/settings/mail",
        icon: Mail,
        permission: "settings:manage",
      },
      {
        label: "存储",
        to: "/settings/storage",
        icon: HardDrive,
        permission: "settings:manage",
      },
    ],
  },
  {
    label: "系统",
    items: [
      { label: "关于", to: "/about", icon: Info },
      {
        label: "日志",
        to: "/logs",
        icon: ScrollText,
        permission: "settings:manage",
      },
    ],
  },
];

/**
 * 面包屑用的路径 → 名称表。
 *
 * 与 NAV_GROUPS 分开维护：导航里有「新建文章」这类不出现在侧栏的页面，
 * 而侧栏项的中文名与页面标题也可能不同（侧栏「概览」，标题「仪表盘」）。
 */
export const ROUTE_LABELS: Record<string, string> = {
  "/": "概览",
  "/posts": "文章",
  "/pages": "页面",
  "/categories": "分类",
  "/tags": "标签",
  "/comments": "评论",
  "/media": "附件",
  "/themes": "主题",
  "/menus": "菜单",
  "/users": "用户",
  "/roles": "角色",
  "/settings": "设置",
  "/about": "关于",
  "/logs": "日志",
};

/** 按当前路径找出所属分组，供侧栏在移动端折叠时显示上下文。 */
export function groupOf(pathname: string): NavGroup | undefined {
  return NAV_GROUPS.find((group) =>
    group.items.some((item) =>
      item.end ? item.to === pathname : pathname.startsWith(item.to),
    ),
  );
}
