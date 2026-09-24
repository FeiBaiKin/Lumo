import { useAuth } from "@/components/auth/auth-provider";
import { useCommandPalette } from "@/components/layout/command-palette-context";
import { Logo } from "@/components/layout/logo";
import { isActivePath } from "@/components/layout/nav";
import { useNavigation } from "@/components/layout/use-nav";
import { type ThemeChoice, useTheme } from "@/components/theme/theme-provider";
import { Avatar } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Kbd, modifierKey } from "@/components/ui/kbd";
import { cn } from "@/lib/utils";
import { useQueryClient } from "@tanstack/react-query";
import {
  ExternalLink,
  LogOut,
  Monitor,
  Moon,
  MoreHorizontal,
  Search,
  ShieldCheck,
  Sun,
  UserRound,
} from "lucide-react";
import { Link, useLocation } from "react-router";

/**
 * 侧边栏（Halo 的 BasicLayout 侧栏）。
 *
 * 自上而下：字标 → 搜索触发条（Ctrl/⌘ K）→ 七组导航 → 底部用户区。
 * 面板色、右侧一条细线，常驻 256px；窄屏不显示，改由底部导航条承担。
 *
 * 分组标题不做成可折叠的 —— 后台导航需要的是「一眼看全我在哪」，
 * 可折叠分组会藏起一半入口，反而让人多点一次。
 * 无权限的项直接不渲染：显示一个点了就 403 的入口，比没有这个入口更糟。
 */

const THEME_OPTIONS: { value: ThemeChoice; label: string; icon: typeof Sun }[] =
  [
    { value: "light", label: "浅色", icon: Sun },
    { value: "dark", label: "深色", icon: Moon },
    { value: "system", label: "跟随系统", icon: Monitor },
  ];

export function Sidebar() {
  return (
    <aside className="fixed inset-y-0 left-0 z-sticky hidden w-sidebar flex-col border-line border-r bg-surface md:flex">
      <div className="flex items-center justify-center py-4">
        <a
          href="/"
          target="_blank"
          rel="noopener noreferrer"
          title="打开站点首页"
          className="transition-ui rounded-control hover:opacity-80"
        >
          <Logo />
        </a>
      </div>

      <div className="flex min-h-0 flex-1 flex-col overflow-y-auto">
        <div className="px-3">
          <SearchTrigger />
        </div>
        <SidebarNav className="px-3 pt-1 pb-3" />
      </div>

      <UserBanner />
    </aside>
  );
}

/** 搜索触发条：点击打开命令面板。 */
export function SearchTrigger({ className }: { className?: string }) {
  const { setOpen } = useCommandPalette();
  return (
    <button
      type="button"
      onClick={() => setOpen(true)}
      className={cn(
        "transition-ui flex w-full items-center gap-3 rounded-control bg-surface-inset px-3 py-1.5 text-ink-muted hover:text-ink",
        className,
      )}
    >
      <Search aria-hidden="true" className="size-4 shrink-0" />
      <span className="flex-1 text-left text-base">搜索</span>
      <span className="flex items-center gap-0.5">
        <Kbd>{modifierKey()}</Kbd>
        <Kbd>K</Kbd>
      </span>
    </button>
  );
}

/** 七组导航。桌面侧栏与移动端抽屉共用。 */
export function SidebarNav({
  className,
  onNavigate,
}: {
  className?: string;
  onNavigate?: () => void;
}) {
  const { can } = useAuth();
  const { pathname } = useLocation();
  const { groups } = useNavigation();

  return (
    <nav aria-label="主导航" className={cn("flex flex-col", className)}>
      {groups.map((group) => {
        const items = group.items.filter(
          (item) => !item.permission || can(item.permission),
        );
        if (items.length === 0) {
          return null;
        }
        return (
          <div key={group.label} className="flex flex-col gap-0.5">
            {/* 「仪表盘」组只有「概览」一项，组名与项名重复，视觉上省掉、读屏仍能听到 */}
            <p
              className={cn(
                "px-2 pt-2 pb-1 text-xs text-ink-muted",
                group.items.length === 1 &&
                  group.items[0]?.to === "/" &&
                  "sr-only",
              )}
            >
              {group.label}
            </p>
            {items.map((item) => {
              const active = isActivePath(item, pathname);
              return (
                <Link
                  key={item.key}
                  to={item.to}
                  onClick={onNavigate}
                  aria-current={active ? "page" : undefined}
                  className={cn(
                    "transition-ui relative flex h-7.5 items-center gap-3 rounded-control px-2 text-md",
                    "[&_svg]:size-[18px] [&_svg]:shrink-0",
                    active
                      ? "bg-surface-active font-medium text-ink [&_svg]:text-seal"
                      : "text-ink hover:bg-surface-hover [&_svg]:text-ink-muted",
                    // 当前项左缘的一条短竖线：3px 宽、26px 高，印色 —— Halo 的同款标记
                    active &&
                      "before:absolute before:top-1/2 before:-left-2 before:h-5 before:w-[3px] before:-translate-y-1/2 before:rounded-full before:bg-seal",
                  )}
                >
                  <item.icon aria-hidden="true" />
                  <span className="truncate">{item.label}</span>
                </Link>
              );
            })}
          </div>
        );
      })}
    </nav>
  );
}

/** 底部用户区（Halo 的 UserProfileBanner）：头像、名字、角色，右侧一个菜单。 */
export function UserBanner({ className }: { className?: string }) {
  const { user } = useAuth();
  // 显示名由服务端随用户一起下发：站长可以改角色的显示名，
  // 前端写死一张表就会在下一次改档时漂掉（作者角色取消那次就漂过）。
  const role = user?.roleLabels?.[0];
  return (
    <div
      className={cn(
        "flex h-[70px] shrink-0 items-center gap-3 border-line border-t px-3",
        className,
      )}
    >
      <Avatar
        src={user?.avatarUrl}
        name={user?.displayName || user?.username}
      />
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="truncate text-base font-medium text-ink">
          {user?.displayName || user?.username}
        </span>
        {role ? (
          <Badge tone="outline" className="w-fit">
            <ShieldCheck aria-hidden="true" />
            {role}
          </Badge>
        ) : null}
      </div>
      <UserMenu />
    </div>
  );
}

/** 账号菜单：个人中心、访问站点、外观三态、退出登录。 */
export function UserMenu({ align = "end" }: { align?: "start" | "end" }) {
  const { user, logout, isToken } = useAuth();
  const { choice, setChoice } = useTheme();
  const queryClient = useQueryClient();

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label="账号菜单"
          className="rounded-full"
        >
          <MoreHorizontal aria-hidden="true" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align={align} side="top" className="w-56">
        <DropdownMenuLabel className="flex flex-col gap-0.5">
          <span className="truncate text-sm font-medium text-ink">
            {user?.displayName || user?.username}
          </span>
          <span className="truncate">{user?.email}</span>
          {isToken ? (
            // PAT 会话没有 CSRF Cookie，所有写操作都会 403。与其让用户点了才失败，不如在这里说清楚
            <span className="text-warn">令牌会话，只读</span>
          ) : null}
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuItem asChild>
          <Link to="/profile">
            <UserRound aria-hidden="true" />
            个人中心
          </Link>
        </DropdownMenuItem>
        <DropdownMenuItem asChild>
          <a href="/" target="_blank" rel="noopener noreferrer">
            <ExternalLink aria-hidden="true" />
            访问站点
          </a>
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuLabel>外观</DropdownMenuLabel>
        <DropdownMenuRadioGroup
          value={choice}
          onValueChange={(value) => setChoice(value as ThemeChoice)}
        >
          {THEME_OPTIONS.map((option) => (
            <DropdownMenuRadioItem key={option.value} value={option.value}>
              <option.icon aria-hidden="true" />
              {option.label}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
        <DropdownMenuSeparator />
        <DropdownMenuItem
          onSelect={async () => {
            await logout();
            queryClient.clear();
          }}
        >
          <LogOut aria-hidden="true" />
          退出登录
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
