import {
  Activity,
  AppWindow,
  Archive,
  BarChart3,
  Bell,
  Blocks,
  Book,
  BookOpen,
  Bookmark,
  Box,
  Brush,
  Calendar,
  Camera,
  ChartPie,
  Check,
  ClipboardList,
  Clock,
  Cloud,
  Code,
  Compass,
  CreditCard,
  Database,
  Download,
  Eye,
  FileCode,
  FileText,
  Film,
  Flag,
  Folder,
  FolderTree,
  Gauge,
  Globe,
  HardDrive,
  Heart,
  HelpCircle,
  Home,
  Image,
  Images,
  Info,
  KeyRound,
  Layers,
  LayoutDashboard,
  Link,
  ListTree,
  Lock,
  type LucideIcon,
  Mail,
  MapPin,
  Megaphone,
  MessageSquare,
  MessagesSquare,
  Music,
  Newspaper,
  Package,
  Palette,
  Paperclip,
  PenLine,
  Phone,
  Puzzle,
  Rss,
  ScrollText,
  Search,
  Send,
  Settings,
  Share2,
  ShieldCheck,
  ShoppingCart,
  SlidersHorizontal,
  Sparkles,
  Star,
  Tag,
  Tags,
  Terminal,
  Trash2,
  Upload,
  UserRound,
  Users,
  Wand,
  Zap,
} from "lucide-react";

// 图标组件的类型。调用方要用它标注「已解析成组件」的字段。
export type { LucideIcon };

/**
 * 图标登记表。
 *
 * 后端只能给出图标的**名字**（侧边栏菜单、插件的页面入口、主题声明的菜单项都要经过
 * JSON），而两侧之间没有编译期约束：前端不认识那个名字时，能做的只有退回一个默认图标，
 * 而界面上不会有任何提示。因此这份表是「双方约定的词汇」，
 * 由 cmd/lumo 的契约测试与 Go 侧对齐。
 *
 * 刻意用显式映射而不是 `import * as icons from "lucide-react"` 加动态查找：
 * 后者会把 Lucide 的两千多个图标全部打进产物，而一个 CMS 后台用不到两千个图标。
 *
 * 键是 kebab-case（与 Lucide 官方的图标名一致），值是组件。
 */
export const ICONS: Record<string, LucideIcon> = {
  activity: Activity,
  "app-window": AppWindow,
  archive: Archive,
  "bar-chart": BarChart3,
  bell: Bell,
  blocks: Blocks,
  book: Book,
  "book-open": BookOpen,
  bookmark: Bookmark,
  box: Box,
  brush: Brush,
  calendar: Calendar,
  camera: Camera,
  "chart-pie": ChartPie,
  check: Check,
  "clipboard-list": ClipboardList,
  clock: Clock,
  cloud: Cloud,
  code: Code,
  compass: Compass,
  "credit-card": CreditCard,
  database: Database,
  download: Download,
  eye: Eye,
  "file-code": FileCode,
  "file-text": FileText,
  film: Film,
  flag: Flag,
  folder: Folder,
  "folder-tree": FolderTree,
  gauge: Gauge,
  globe: Globe,
  "hard-drive": HardDrive,
  heart: Heart,
  "help-circle": HelpCircle,
  home: Home,
  image: Image,
  images: Images,
  info: Info,
  "key-round": KeyRound,
  layers: Layers,
  "layout-dashboard": LayoutDashboard,
  link: Link,
  "list-tree": ListTree,
  lock: Lock,
  mail: Mail,
  "map-pin": MapPin,
  megaphone: Megaphone,
  "message-square": MessageSquare,
  "messages-square": MessagesSquare,
  music: Music,
  newspaper: Newspaper,
  package: Package,
  palette: Palette,
  paperclip: Paperclip,
  "pen-line": PenLine,
  phone: Phone,
  puzzle: Puzzle,
  rss: Rss,
  "scroll-text": ScrollText,
  search: Search,
  send: Send,
  settings: Settings,
  "share-2": Share2,
  "shield-check": ShieldCheck,
  "shopping-cart": ShoppingCart,
  "sliders-horizontal": SlidersHorizontal,
  sparkles: Sparkles,
  star: Star,
  tag: Tag,
  tags: Tags,
  terminal: Terminal,
  "trash-2": Trash2,
  upload: Upload,
  "user-round": UserRound,
  users: Users,
  wand: Wand,
  zap: Zap,
};

/** 图标名清单，已按字母序排列，供选择器与测试使用。 */
export const ICON_NAMES: string[] = Object.keys(ICONS).sort();

/** 认不出的图标名退回它。选一个中性的形状，不暗示任何语义。 */
export const FALLBACK_ICON: LucideIcon = Box;

/**
 * 按名字取图标组件。
 *
 * 认不出时退回 FALLBACK_ICON 而不是抛错或返回 undefined：
 * 图标名来自后端，一个名字写错不该让整个后台白屏。
 */
export function resolveIcon(name: string | undefined | null): LucideIcon {
  if (!name) {
    return FALLBACK_ICON;
  }
  return ICONS[name] ?? FALLBACK_ICON;
}

/** 名字是否是登记表里的图标。 */
export function isKnownIcon(name: string): boolean {
  return Object.hasOwn(ICONS, name);
}
