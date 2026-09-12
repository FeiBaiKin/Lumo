import {
  BookOpen,
  FileText,
  FolderTree,
  HardDrive,
  Image,
  Info,
  LayoutDashboard,
  ListTree,
  type LucideIcon,
  Mail,
  MessageSquare,
  Palette,
  ScrollText,
  Search,
  Settings,
  ShieldCheck,
  Tags,
  UserRound,
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
  /** 命令面板里的检索关键词（含拼音），便于用中文输入法习惯查找。 */
  keywords?: string;
};

export type NavGroup = {
  label: string;
  items: NavItem[];
};

export const NAV_GROUPS: NavGroup[] = [
  {
    label: "仪表盘",
    items: [
      {
        label: "概览",
        to: "/",
        icon: LayoutDashboard,
        end: true,
        keywords: "dashboard gailan shouye home",
      },
    ],
  },
  {
    label: "内容",
    items: [
      {
        label: "文章",
        to: "/posts",
        icon: BookOpen,
        keywords: "posts wenzhang",
      },
      { label: "页面", to: "/pages", icon: FileText, keywords: "pages yemian" },
      // 分类与标签不设权限门槛：服务端对 Console 平面的读操作对任何已认证用户开放
      // （见 internal/taxonomy/handler.go 的说明 —— 作者写文章要能选分类）。
      // 写操作在页面内部按 taxonomies:manage 收起。
      {
        label: "分类",
        to: "/categories",
        icon: FolderTree,
        keywords: "categories fenlei",
      },
      { label: "标签", to: "/tags", icon: Tags, keywords: "tags biaoqian" },
      {
        label: "评论",
        to: "/comments",
        icon: MessageSquare,
        keywords: "comments pinglun",
      },
    ],
  },
  {
    label: "媒体",
    items: [
      {
        label: "附件",
        to: "/media",
        icon: Image,
        keywords: "media fujian tupian",
      },
    ],
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
        keywords: "themes zhuti",
      },
      {
        label: "菜单",
        to: "/menus",
        icon: ListTree,
        permission: "menus:manage",
        keywords: "menus caidan daohang",
      },
    ],
  },
  {
    label: "用户",
    items: [
      {
        label: "用户",
        to: "/users",
        icon: Users,
        permission: "users:manage",
        keywords: "users yonghu",
      },
      {
        label: "角色",
        to: "/roles",
        icon: ShieldCheck,
        permission: "roles:manage",
        keywords: "roles juese quanxian",
      },
    ],
  },
  {
    label: "设置",
    items: [
      // 设置的**全部**端点（含读取）都要求 settings:manage，见 internal/settings/handler.go
      // 里的 manage 中间件 —— 未授权用户不该看到站点的 SMTP 主机与存储配置。
      {
        label: "站点",
        to: "/settings/site",
        icon: Settings,
        permission: "settings:manage",
        keywords: "settings site zhandian shezhi",
      },
      {
        label: "SEO",
        to: "/settings/seo",
        icon: Search,
        permission: "settings:manage",
        keywords: "seo sousuo",
      },
      {
        label: "邮件",
        to: "/settings/mail",
        icon: Mail,
        permission: "settings:manage",
        keywords: "mail smtp youjian",
      },
      {
        label: "存储",
        to: "/settings/storage",
        icon: HardDrive,
        permission: "settings:manage",
        keywords: "storage s3 cunchu",
      },
    ],
  },
  {
    label: "系统",
    items: [
      {
        label: "关于",
        to: "/about",
        icon: Info,
        keywords: "about guanyu banben",
      },
      {
        label: "日志",
        to: "/logs",
        icon: ScrollText,
        permission: "settings:manage",
        keywords: "logs rizhi",
      },
    ],
  },
];

/** 不进侧栏、但要在命令面板与面包屑里出现的页面。 */
export const EXTRA_ROUTES: NavItem[] = [
  {
    label: "个人中心",
    to: "/profile",
    icon: UserRound,
    keywords: "profile geren zhanghao mima lingpai token",
  },
];

/**
 * 路径 → 名称表，供文档标题与移动端顶栏使用。
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
  "/profile": "个人中心",
};

/** 判断某个导航项是否对应当前路径。 */
export function isActivePath(item: NavItem, pathname: string): boolean {
  if (item.end) {
    return item.to === pathname;
  }
  return pathname === item.to || pathname.startsWith(`${item.to}/`);
}

/** 按当前路径找出所属分组，供侧栏在移动端折叠时显示上下文。 */
export function groupOf(pathname: string): NavGroup | undefined {
  return NAV_GROUPS.find((group) =>
    group.items.some((item) => isActivePath(item, pathname)),
  );
}
