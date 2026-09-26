import { cn } from "@/lib/utils";
import type { LucideIcon } from "lucide-react";
import type { ReactNode } from "react";
import { Link } from "react-router";

/**
 * 标签栏。
 *
 * 两种形态：
 *   underline —— 页面级切换（设置分组、主题详情），当前项下方一条 2px 的印色线
 *   pills     —— 小型分段控件（正文格式、明暗主题），当前项有底色
 *
 * 项可以是链接（有 `to`）或按钮：设置分组各有自己的地址，刷新与分享都要能回到同一处；
 * 而弹窗里的分组切换没有地址可言。
 */

export type TabItem = {
  value: string;
  label: ReactNode;
  icon?: LucideIcon;
  /** 右侧的计数。 */
  count?: number | undefined;
  to?: string;
  disabled?: boolean;
};

export function Tabbar({
  items,
  value,
  onChange,
  variant = "underline",
  size = "md",
  ariaLabel,
  className,
}: {
  items: TabItem[];
  value: string;
  onChange?: (value: string) => void;
  variant?: "underline" | "pills";
  size?: "sm" | "md";
  ariaLabel: string;
  className?: string;
}) {
  const isPills = variant === "pills";
  return (
    <nav
      aria-label={ariaLabel}
      className={cn(
        // overflow-x-auto 会连带把 overflow-y 变成 auto，underline 形态的 -mb-px 会撑出 1px 竖向滚动条
        "flex min-w-0 items-center overflow-x-auto overflow-y-hidden",
        isPills
          ? "gap-0.5 rounded-control bg-surface-inset p-0.5"
          : "gap-1 border-line border-b",
        className,
      )}
    >
      {items.map((item) => {
        const active = item.value === value;
        const Icon = item.icon;
        const classes = cn(
          "transition-ui flex shrink-0 items-center gap-1.5 whitespace-nowrap font-medium",
          size === "sm" ? "text-sm" : "text-base",
          isPills
            ? cn(
                "rounded-[3px] px-2.5 py-1",
                active
                  ? "bg-surface text-ink shadow-card"
                  : "text-ink-muted hover:text-ink",
              )
            : cn(
                "-mb-px border-b-2 px-3 py-2",
                active
                  ? "border-seal text-ink"
                  : "border-transparent text-ink-muted hover:border-line-strong hover:text-ink",
              ),
          item.disabled && "pointer-events-none opacity-50",
        );
        const content = (
          <>
            {Icon ? <Icon aria-hidden="true" className="size-4" /> : null}
            {item.label}
            {item.count !== undefined ? (
              <span
                className={cn(
                  "tabular rounded-full px-1.5 text-xs",
                  active
                    ? "bg-seal-soft text-seal"
                    : "bg-surface-active text-ink-muted",
                )}
              >
                {item.count}
              </span>
            ) : null}
          </>
        );
        if (item.to) {
          return (
            <Link
              key={item.value}
              to={item.to}
              aria-current={active ? "page" : undefined}
              className={classes}
            >
              {content}
            </Link>
          );
        }
        return (
          <button
            key={item.value}
            type="button"
            onClick={() => onChange?.(item.value)}
            aria-pressed={active}
            disabled={item.disabled}
            className={classes}
          >
            {content}
          </button>
        );
      })}
    </nav>
  );
}
