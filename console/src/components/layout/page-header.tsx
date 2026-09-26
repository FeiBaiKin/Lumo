import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { ChevronLeft, type LucideIcon } from "lucide-react";
import type { ReactNode } from "react";
import { Link } from "react-router";

/**
 * 页头：图标 + 标题在左，动作在右。
 *
 * 56px 高、面板色、直接落在浅灰工作区上；每个页面顶部都用它，
 * 标题层级与间距才能全站一致。h1 每页唯一 —— 屏幕阅读器靠它确认「现在在哪一页」。
 */
export function PageHeader({
  icon: Icon,
  title,
  description,
  actions,
  back,
  sticky = true,
  className,
}: {
  icon?: LucideIcon | undefined;
  title: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  /** 返回按钮（编辑页用）。 */
  back?: { to: string; label?: string } | undefined;
  sticky?: boolean;
  className?: string;
}) {
  return (
    <header
      className={cn(
        "z-sticky border-line border-b bg-surface",
        sticky && "sticky top-0",
        className,
      )}
    >
      <div className="flex min-h-page-header flex-wrap items-center justify-between gap-x-4 gap-y-2 px-4 py-2">
        <div className="flex min-w-0 items-center gap-3">
          {back ? (
            <Button variant="ghost" size="icon-sm" asChild>
              <Link
                to={back.to}
                aria-label={back.label ?? "返回"}
                title={back.label ?? "返回"}
              >
                <ChevronLeft aria-hidden="true" />
              </Link>
            </Button>
          ) : null}
          {Icon ? (
            <Icon
              aria-hidden="true"
              className="size-5 shrink-0 text-ink-muted"
            />
          ) : null}
          <div className="flex min-w-0 flex-col">
            <h1 className="truncate text-xl font-semibold text-ink">{title}</h1>
            {description ? (
              <p className="truncate text-xs text-ink-muted">{description}</p>
            ) : null}
          </div>
        </div>
        {actions ? (
          <div className="flex flex-wrap items-center gap-2">{actions}</div>
        ) : null}
      </div>
    </header>
  );
}

/**
 * 页面主体：桌面四周 16px 外边距，移动端贴边。
 */
export function PageBody({
  className,
  children,
}: {
  className?: string;
  children: ReactNode;
}) {
  return (
    <div className={cn("flex flex-col gap-4 p-0 md:p-4", className)}>
      {children}
    </div>
  );
}
