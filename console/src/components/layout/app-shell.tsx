import { Sidebar } from "@/components/layout/sidebar";
import { Topbar } from "@/components/layout/topbar";
import { cn } from "@/lib/utils";
import { useEffect, useState } from "react";
import { Outlet, useLocation } from "react-router";

/**
 * 应用外壳。
 *
 * 布局是「常驻侧栏 + 顶栏 + 可滚动内容区」这一后台的标准形态，
 * 刻意不做花样（agent.md §11.3：导航走行业惯例，惯例在工具里是功能）。
 *
 * 窄屏把侧栏变成抽屉。后台在手机上主要用来审评论与看数据，
 * 侧栏常驻会挤掉本就有限的内容宽度。
 */
export function AppShell() {
  const [drawerOpen, setDrawerOpen] = useState(false);
  const { pathname } = useLocation();

  // 路由一变就收起抽屉，否则点了导航项抽屉还盖在内容上。
  // pathname 是触发条件而非被读取的值 —— 这个 effect 的语义就是「地址变了」，
  // 故不能按 linter 的建议删掉它。
  // biome-ignore lint/correctness/useExhaustiveDependencies: pathname 是刻意的触发条件
  useEffect(() => {
    setDrawerOpen(false);
  }, [pathname]);

  // 抽屉打开时锁住背景滚动，并允许 Esc 关闭 —— 键盘用户不应被困在抽屉里。
  useEffect(() => {
    if (!drawerOpen) {
      return;
    }
    const previous = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        setDrawerOpen(false);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => {
      document.body.style.overflow = previous;
      window.removeEventListener("keydown", onKey);
    };
  }, [drawerOpen]);

  return (
    <div className="flex h-dvh overflow-hidden bg-chrome">
      {/* 跳转到主内容。键盘与读屏用户不必每次 Tab 过整条导航 */}
      <a
        href="#main"
        className="sr-only rounded-control bg-seal px-3 py-1.5 text-seal-on focus:not-sr-only focus:absolute focus:top-2 focus:left-2 focus:z-command"
      >
        跳到主内容
      </a>

      {/* 宽屏常驻侧栏 */}
      <aside className="hidden w-sidebar shrink-0 border-line border-r bg-chrome lg:flex lg:flex-col">
        <Sidebar />
      </aside>

      {/* 窄屏抽屉 */}
      {drawerOpen ? (
        <div className="fixed inset-0 z-overlay lg:hidden">
          <button
            type="button"
            aria-label="关闭导航"
            className="absolute inset-0 bg-black/40"
            onClick={() => setDrawerOpen(false)}
          />
          <div className="absolute inset-y-0 left-0 w-sidebar border-line border-r bg-chrome shadow-overlay">
            <Sidebar />
          </div>
        </div>
      ) : null}

      <div className="flex min-w-0 flex-1 flex-col">
        <Topbar onToggleSidebar={() => setDrawerOpen(true)} />
        <main
          id="main"
          // 内容区自己滚动，顶栏与侧栏固定 —— 长表格滚动时导航不该跟着走
          className={cn("min-h-0 flex-1 overflow-y-auto")}
        >
          <div className="mx-auto w-full max-w-[100rem] px-4 py-6 sm:px-6 lg:px-8">
            <Outlet />
          </div>
        </main>
      </div>
    </div>
  );
}
