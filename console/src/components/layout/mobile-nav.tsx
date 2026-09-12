import { useAuth } from "@/components/auth/auth-provider";
import { useCommandPalette } from "@/components/layout/command-palette";
import { Logo } from "@/components/layout/logo";
import { isActivePath } from "@/components/layout/nav";
import { SidebarNav, UserBanner } from "@/components/layout/sidebar";
import { cn } from "@/lib/utils";
import * as DialogPrimitive from "@radix-ui/react-dialog";
import {
  BookOpen,
  Image,
  LayoutDashboard,
  type LucideIcon,
  Menu,
  MessageSquare,
  Search,
} from "lucide-react";
import { useEffect, useState } from "react";
import { Link, useLocation } from "react-router";

/**
 * 窄屏导航（Halo 的 MobileMenu）：顶部一条细栏 + 底部固定导航条。
 *
 * 底部五格：概览、文章、评论、附件、更多。「更多」拉起一张从底部升起的抽屉，
 * 内含完整的七组导航与用户区。后台在手机上主要用来审评论与看数据，
 * 这四个入口覆盖了绝大多数场景，其余交给抽屉。
 */

const CELLS: { label: string; to: string; icon: LucideIcon; end?: boolean }[] =
  [
    { label: "概览", to: "/", icon: LayoutDashboard, end: true },
    { label: "文章", to: "/posts", icon: BookOpen },
    { label: "评论", to: "/comments", icon: MessageSquare },
    { label: "附件", to: "/media", icon: Image },
  ];

export function MobileTopBar() {
  const { setOpen } = useCommandPalette();
  return (
    <div className="flex h-mobile-bar items-center justify-between border-line border-b bg-surface px-4 md:hidden">
      <Logo />
      <button
        type="button"
        onClick={() => setOpen(true)}
        aria-label="搜索"
        className="transition-ui flex size-9 items-center justify-center rounded-control text-ink-muted hover:bg-surface-active hover:text-ink"
      >
        <Search aria-hidden="true" className="size-5" />
      </button>
    </div>
  );
}

export function MobileNav() {
  const { pathname } = useLocation();
  const [open, setOpen] = useState(false);
  const { can } = useAuth();

  // 路由一变就收起抽屉，否则点了导航项抽屉还盖在内容上。
  // biome-ignore lint/correctness/useExhaustiveDependencies: pathname 是刻意的触发条件
  useEffect(() => {
    setOpen(false);
  }, [pathname]);

  const cells = CELLS.filter(
    (cell) => cell.to !== "/comments" || can("comments:manage"),
  );

  return (
    <>
      <nav
        aria-label="底部导航"
        className="fixed inset-x-0 bottom-0 z-drawer grid h-mobile-bar border-line border-t bg-surface md:hidden"
        style={{
          gridTemplateColumns: `repeat(${cells.length + 1}, minmax(0, 1fr))`,
        }}
      >
        {cells.map((cell) => {
          const active = isActivePath(cell, pathname);
          return (
            <Link
              key={cell.to}
              to={cell.to}
              aria-current={active ? "page" : undefined}
              className={cn(
                "transition-ui flex flex-col items-center justify-center gap-0.5 text-[11px]",
                active ? "text-seal" : "text-ink-muted",
              )}
            >
              <cell.icon aria-hidden="true" className="size-5" />
              {cell.label}
            </Link>
          );
        })}
        <button
          type="button"
          onClick={() => setOpen(true)}
          aria-expanded={open}
          className="transition-ui flex flex-col items-center justify-center gap-0.5 text-[11px] text-ink-muted"
        >
          <Menu aria-hidden="true" className="size-5" />
          更多
        </button>
      </nav>

      <DialogPrimitive.Root open={open} onOpenChange={setOpen}>
        <DialogPrimitive.Portal>
          <DialogPrimitive.Overlay className="overlay-in fixed inset-0 z-overlay bg-scrim md:hidden" />
          <DialogPrimitive.Content
            className={cn(
              "sheet-in fixed inset-x-0 bottom-0 z-overlay flex max-h-[80dvh] flex-col rounded-t-overlay border-line border-t bg-surface shadow-overlay outline-none md:hidden",
            )}
          >
            <DialogPrimitive.Title className="sr-only">
              全部导航
            </DialogPrimitive.Title>
            <DialogPrimitive.Description className="sr-only">
              后台的全部页面与账号操作
            </DialogPrimitive.Description>
            <div className="flex justify-center py-2">
              <span
                aria-hidden="true"
                className="h-1 w-10 rounded-full bg-line-strong"
              />
            </div>
            <div className="min-h-0 flex-1 overflow-y-auto">
              <SidebarNav
                className="px-3 pb-3"
                onNavigate={() => setOpen(false)}
              />
            </div>
            <UserBanner />
          </DialogPrimitive.Content>
        </DialogPrimitive.Portal>
      </DialogPrimitive.Root>
    </>
  );
}
