import {
  CommandPalette,
  CommandPaletteProvider,
} from "@/components/layout/command-palette";
import { MobileNav, MobileTopBar } from "@/components/layout/mobile-nav";
import { Sidebar } from "@/components/layout/sidebar";
import { Outlet } from "react-router";

/**
 * 应用外壳（Halo 的 BasicLayout）。
 *
 * 常驻侧栏 + 内容区，**没有顶栏**：页面的标题与动作由各页自己的页头承担，
 * 账号、外观与搜索都收在侧栏里。窄屏改为顶部一条细栏 + 底部导航条。
 *
 * 内容区随窗口滚动而不是自己滚动：页头 sticky 在顶部，长列表滚动时它留在原地。
 */
export function AppShell() {
  return (
    <CommandPaletteProvider>
      <div className="min-h-dvh bg-chrome">
        {/* 跳转到主内容。键盘与读屏用户不必每次 Tab 过整条导航 */}
        <a
          href="#main"
          className="sr-only rounded-control bg-action px-3 py-1.5 text-action-on focus:not-sr-only focus:fixed focus:top-2 focus:left-2 focus:z-command"
        >
          跳到主内容
        </a>

        <Sidebar />

        <div className="flex min-h-dvh flex-col md:pl-sidebar">
          <MobileTopBar />
          <main id="main" className="flex-1 pb-mobile-bar md:pb-0">
            <Outlet />
          </main>
          <footer className="hidden py-4 text-center text-sm text-ink-subtle md:block">
            Powered by Lumo
          </footer>
        </div>

        <MobileNav />
        <CommandPalette />
      </div>
    </CommandPaletteProvider>
  );
}
