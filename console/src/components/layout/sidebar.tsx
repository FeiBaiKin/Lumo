import { useAuth } from "@/components/auth/auth-provider";
import { NAV_GROUPS } from "@/components/layout/nav";
import { cn } from "@/lib/utils";
import { NavLink } from "react-router";

/**
 * 侧边栏。
 *
 * 七组导航（agent.md §8）。分组标题不做成可折叠的 —— 后台导航需要的是
 * 「一眼看全我在哪」，可折叠分组会藏起一半入口，反而让人多点一次。
 *
 * 无权限的项直接不渲染：显示一个点了就 403 的入口，比没有这个入口更糟。
 */
export function Sidebar({ className }: { className?: string }) {
  const { can } = useAuth();

  return (
    <nav
      aria-label="主导航"
      className={cn(
        "flex h-full flex-col gap-5 overflow-y-auto px-3 py-4",
        className,
      )}
    >
      {NAV_GROUPS.map((group) => {
        const items = group.items.filter(
          (item) => !item.permission || can(item.permission),
        );
        if (items.length === 0) {
          return null;
        }
        return (
          <div key={group.label} className="flex flex-col gap-0.5">
            <p className="px-2 pb-1 text-xs font-medium text-ink-muted">
              {group.label}
            </p>
            {items.map((item) => (
              <NavLink
                key={item.to}
                to={item.to}
                // 显式收敛成 boolean：exactOptionalPropertyTypes 下
                // 「未定义」与「传了 undefined」是两回事，NavLink 只接受前者
                end={item.end ?? false}
                className={({ isActive }) =>
                  cn(
                    "transition-ui flex h-8 items-center gap-2.5 rounded-control px-2 text-base",
                    "[&_svg]:size-4 [&_svg]:shrink-0",
                    isActive
                      ? "bg-seal-soft font-medium text-seal"
                      : "text-ink hover:bg-surface-active",
                  )
                }
              >
                <item.icon aria-hidden="true" />
                <span className="truncate">{item.label}</span>
              </NavLink>
            ))}
          </div>
        );
      })}
    </nav>
  );
}
