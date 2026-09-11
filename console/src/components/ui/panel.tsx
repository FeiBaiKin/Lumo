import { cn } from "@/lib/utils";
import type { ComponentProps } from "react";

/**
 * 面板。
 *
 * 面板用 1px 描边表达边界，**不用阴影**（agent.md §11.3）——
 * 后台同屏有四五个区块，每个都带一层柔和阴影会让页面发灰、层级反而消失。
 * 阴影只留给真的浮起来的元素（弹窗、下拉、命令面板）。
 */

export function Panel({ className, ...props }: ComponentProps<"section">) {
  return (
    <section
      className={cn("rounded-panel border border-line bg-surface", className)}
      {...props}
    />
  );
}

export function PanelHeader({ className, ...props }: ComponentProps<"header">) {
  return (
    <header
      className={cn(
        "flex min-h-12 flex-wrap items-center justify-between gap-3 border-line border-b px-4 py-2.5",
        className,
      )}
      {...props}
    />
  );
}

export function PanelTitle({ className, ...props }: ComponentProps<"h2">) {
  return (
    <h2
      className={cn("text-lg font-semibold text-ink", className)}
      {...props}
    />
  );
}

/** 标题旁的补充说明，与标题同一行但视觉次一级。 */
export function PanelDescription({ className, ...props }: ComponentProps<"p">) {
  return <p className={cn("text-xs text-ink-muted", className)} {...props} />;
}

export function PanelBody({ className, ...props }: ComponentProps<"div">) {
  return <div className={cn("p-4", className)} {...props} />;
}

export function PanelFooter({ className, ...props }: ComponentProps<"footer">) {
  return (
    <footer
      className={cn(
        "flex flex-wrap items-center justify-end gap-2 border-line border-t px-4 py-3",
        className,
      )}
      {...props}
    />
  );
}

/** 页面级标题区。每个页面顶部用它，保证标题层级与间距全站一致。 */
export function PageHeader({
  title,
  description,
  actions,
  className,
}: {
  title: string;
  // 显式写出 `| undefined`：exactOptionalPropertyTypes 下，
  // 只写 `description?: string` 会拒绝 `description={maybeUndefined}` 这种写法，
  // 而「可能没有描述」正是这个参数最常见的用法。
  description?: string | undefined;
  actions?: React.ReactNode;
  className?: string;
}) {
  return (
    <div
      className={cn(
        "flex flex-wrap items-start justify-between gap-4 pb-5",
        className,
      )}
    >
      <div className="flex min-w-0 flex-col gap-1">
        {/* h1 每页唯一：屏幕阅读器用户靠它确认「现在在哪一页」 */}
        <h1 className="text-xl font-semibold text-ink">{title}</h1>
        {description ? (
          <p className="text-sm text-ink-muted">{description}</p>
        ) : null}
      </div>
      {actions ? (
        <div className="flex items-center gap-2">{actions}</div>
      ) : null}
    </div>
  );
}
