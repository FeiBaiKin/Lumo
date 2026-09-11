import { useAuth } from "@/components/auth/auth-provider";
import { ROUTE_LABELS } from "@/components/layout/nav";
import { type ThemeChoice, useTheme } from "@/components/theme/theme-provider";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { useQueryClient } from "@tanstack/react-query";
import {
  ChevronRight,
  LogOut,
  Menu as MenuIcon,
  Monitor,
  Moon,
  Sun,
  User,
} from "lucide-react";
import { Fragment, useState } from "react";
import { Link, useLocation } from "react-router";

/**
 * 顶栏：面包屑 + 主题切换 + 账号。
 *
 * 面包屑按路径段从 ROUTE_LABELS 查名字，未知段落（如文章 ID）不显示 ——
 * 与其显示一串数字，不如让它成为「返回上一级」的空白。
 */

const THEME_OPTIONS: { value: ThemeChoice; label: string; icon: typeof Sun }[] =
  [
    { value: "light", label: "浅色", icon: Sun },
    { value: "dark", label: "深色", icon: Moon },
    { value: "system", label: "跟随系统", icon: Monitor },
  ];

function Breadcrumbs() {
  const { pathname } = useLocation();
  const segments = pathname.split("/").filter(Boolean);

  // 逐段累积，得到 /posts/new 这样的中间路径
  const crumbs: { label: string; to: string }[] = [];
  let acc = "";
  for (const segment of segments) {
    acc += `/${segment}`;
    const label = ROUTE_LABELS[acc];
    if (label) {
      crumbs.push({ label, to: acc });
    }
  }

  if (crumbs.length === 0) {
    return <span className="text-base font-medium text-ink">概览</span>;
  }

  return (
    <nav aria-label="面包屑" className="flex min-w-0 items-center gap-1">
      {crumbs.map((crumb, index) => (
        <Fragment key={crumb.to}>
          {index > 0 ? (
            <ChevronRight
              aria-hidden="true"
              className="size-3.5 shrink-0 text-ink-subtle"
            />
          ) : null}
          {index === crumbs.length - 1 ? (
            // 最后一段是当前位置，不是链接 —— 点它会得到一次无意义的导航
            <span aria-current="page" className="truncate font-medium text-ink">
              {crumb.label}
            </span>
          ) : (
            <Link
              to={crumb.to}
              className="transition-ui truncate rounded-control text-ink-muted hover:text-ink"
            >
              {crumb.label}
            </Link>
          )}
        </Fragment>
      ))}
    </nav>
  );
}

function ThemeSwitch() {
  const { choice, setChoice } = useTheme();
  return (
    /*
     * 用 fieldset + 屏幕阅读器可见的 legend，而不是 div[role=group] + aria-label。
     * 两者的可访问性等价，但 fieldset 是这件事情的原生元素 ——
     * 一组互斥的选项本来就该是 fieldset，不必靠 ARIA 补语义。
     * 边框与内边距由类清掉：这里的 fieldset 只借用语义，不借用外观。
     */
    <fieldset className="m-0 flex items-center gap-0.5 rounded-control border border-line bg-surface p-0.5">
      <legend className="sr-only">外观</legend>
      {THEME_OPTIONS.map((option) => {
        const active = choice === option.value;
        return (
          <button
            key={option.value}
            type="button"
            onClick={() => setChoice(option.value)}
            aria-pressed={active}
            title={option.label}
            className={cn(
              "transition-ui flex size-6 items-center justify-center rounded-[3px]",
              active
                ? "bg-seal-soft text-seal"
                : "text-ink-muted hover:bg-surface-active hover:text-ink",
            )}
          >
            <option.icon aria-hidden="true" className="size-3.5" />
            <span className="sr-only">{option.label}</span>
          </button>
        );
      })}
    </fieldset>
  );
}

function AccountMenu() {
  const { user, logout } = useAuth();
  const queryClient = useQueryClient();
  const [open, setOpen] = useState(false);

  return (
    <div className="relative">
      <Button
        variant="ghost"
        size="sm"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        aria-haspopup="menu"
        className="gap-2"
      >
        <User aria-hidden="true" />
        <span className="max-w-32 truncate">
          {user?.displayName || user?.username}
        </span>
      </Button>

      {open ? (
        <>
          {/* 点击遮罩关闭。做成按钮而非 div：键盘用户要能 Tab 到它并回车关闭 */}
          <button
            type="button"
            aria-label="关闭账号菜单"
            className="fixed inset-0 z-popover cursor-default"
            onClick={() => setOpen(false)}
          />
          <div
            role="menu"
            className="absolute right-0 z-popover mt-1 w-56 rounded-overlay border border-line bg-surface p-1 shadow-overlay"
          >
            <div className="flex flex-col gap-0.5 px-2 py-2">
              <p className="truncate text-sm font-medium text-ink">
                {user?.displayName || user?.username}
              </p>
              <p className="truncate text-xs text-ink-muted">{user?.email}</p>
            </div>
            <div className="my-1 h-px bg-line" />
            <button
              type="button"
              role="menuitem"
              onClick={async () => {
                setOpen(false);
                await logout();
                queryClient.clear();
              }}
              className="transition-ui flex w-full items-center gap-2 rounded-control px-2 py-1.5 text-left text-base text-ink hover:bg-surface-active"
            >
              <LogOut aria-hidden="true" className="size-4" />
              退出登录
            </button>
          </div>
        </>
      ) : null}
    </div>
  );
}

export function Topbar({ onToggleSidebar }: { onToggleSidebar: () => void }) {
  const { isToken } = useAuth();

  return (
    <header className="flex h-topbar shrink-0 items-center gap-3 border-line border-b bg-chrome px-4">
      {/* 窄屏才出现的侧栏开关。宽屏下侧栏常驻，这个按钮没有意义 */}
      <Button
        variant="ghost"
        size="icon-sm"
        className="lg:hidden"
        onClick={onToggleSidebar}
        aria-label="打开导航"
      >
        <MenuIcon aria-hidden="true" />
      </Button>

      <div className="min-w-0 flex-1">
        <Breadcrumbs />
      </div>

      {/*
        PAT 会话下没有 CSRF Cookie，所有写操作都会 403。
        与其让用户点了才失败，不如在这里说清楚原因。
      */}
      {isToken ? (
        <span className="hidden text-xs text-warn sm:inline">
          令牌会话 · 只读
        </span>
      ) : null}

      <ThemeSwitch />
      <AccountMenu />
    </header>
  );
}
