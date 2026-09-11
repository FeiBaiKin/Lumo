import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { AlertTriangle, Inbox, type LucideIcon, RotateCw } from "lucide-react";
import type { ComponentProps, ReactNode } from "react";

/**
 * 三种「没有正常内容可显示」的状态。
 *
 * 检索给出的 High 级要求是「空状态给方向而非留白」：一个空表格必须告诉用户
 * 下一步能做什么。故 EmptyState 的 action 不是可选项 —— 没有可执行动作的空状态
 * 就是一块空白，只是多了行字。
 */

/** 骨架屏。加载中的占位必须与真实内容的尺寸一致，否则内容到位时会跳一下。 */
export function Skeleton({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      aria-hidden="true"
      className={cn(
        "animate-pulse rounded-control bg-surface-active",
        className,
      )}
      {...props}
    />
  );
}

/** 表格骨架：默认渲染若干行，列宽由调用方给。 */
export function TableSkeleton({
  rows = 6,
  className,
}: { rows?: number; className?: string }) {
  return (
    <div className={cn("flex flex-col gap-2 p-4", className)} aria-busy="true">
      {Array.from({ length: rows }, (_, i) => (
        // 骨架行是纯装饰且数量固定，用下标作 key 不会引起重排问题。
        // biome-ignore lint/suspicious/noArrayIndexKey: 静态占位列表
        <Skeleton key={i} className="h-8 w-full" />
      ))}
    </div>
  );
}

export function EmptyState({
  icon: Icon = Inbox,
  title,
  description,
  action,
  className,
}: {
  icon?: LucideIcon;
  title: string;
  description: string;
  action?: ReactNode;
  className?: string;
}) {
  return (
    <div
      className={cn(
        "flex flex-col items-center justify-center gap-3 px-6 py-14 text-center",
        className,
      )}
    >
      <Icon aria-hidden="true" className="size-7 text-ink-subtle" />
      <div className="flex flex-col gap-1">
        <p className="font-medium text-ink">{title}</p>
        <p className="max-w-sm text-sm text-ink-muted">{description}</p>
      </div>
      {action}
    </div>
  );
}

/**
 * 错误状态。
 *
 * 说明「发生了什么」并给出「怎么重试」，不道歉、不含糊（frontend-design 的文案要求）。
 * 原始错误信息原样展示：后台的使用者多半是站长本人，含糊其辞只会让他去翻服务端日志。
 */
export function ErrorState({
  title = "载入失败",
  message,
  onRetry,
  className,
}: {
  title?: string;
  message: string;
  // 显式 `| undefined`：exactOptionalPropertyTypes 下，调用方常把
  // 「可能没有重试回调」的可选值直接透传进来
  onRetry?: (() => void) | undefined;
  className?: string;
}) {
  return (
    <div
      role="alert"
      className={cn(
        "flex flex-col items-center justify-center gap-3 px-6 py-14 text-center",
        className,
      )}
    >
      <AlertTriangle aria-hidden="true" className="size-7 text-danger" />
      <div className="flex flex-col gap-1">
        <p className="font-medium text-ink">{title}</p>
        <p className="token max-w-md text-sm text-ink-muted">{message}</p>
      </div>
      {onRetry ? (
        <Button
          variant="secondary"
          size="sm"
          onClick={onRetry}
          // 图标传达「这是重试」，文字说明重试什么
        >
          <RotateCw aria-hidden="true" />
          重新载入
        </Button>
      ) : null}
    </div>
  );
}
