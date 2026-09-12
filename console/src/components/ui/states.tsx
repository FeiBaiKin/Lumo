import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { AlertTriangle, Inbox, type LucideIcon, RotateCw } from "lucide-react";
import type { ComponentProps, ReactNode } from "react";

/**
 * 三种「没有正常内容可显示」的状态。
 *
 * 检索给出的 High 级要求是「空状态给方向而非留白」：一个空列表必须告诉用户
 * 下一步能做什么。EmptyState 的图标放在一枚浅色圆盘里 —— 这是 Halo 的 VEmpty
 * 在没有插图时的形态，比一个孤零零的线条图标更像「这里本该有东西」。
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

/** 一行实体的骨架：缩略图位 + 两行文字 + 右侧元信息。 */
export function EntitySkeleton({ thumb = false }: { thumb?: boolean }) {
  return (
    <div className="flex items-center gap-4 px-4 py-3" aria-hidden="true">
      <Skeleton className="size-4 shrink-0" />
      {thumb ? <Skeleton className="h-thumb-h w-thumb-w shrink-0" /> : null}
      <div className="flex min-w-0 flex-1 flex-col gap-2">
        <Skeleton className="h-4 w-2/5" />
        <Skeleton className="h-3 w-1/4" />
      </div>
      <Skeleton className="h-3 w-16" />
      <Skeleton className="h-3 w-12" />
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
  icon?: LucideIcon | undefined;
  title: string;
  description?: string | undefined;
  action?: ReactNode;
  className?: string;
}) {
  return (
    <div
      className={cn(
        "flex flex-col items-center justify-center gap-3 px-6 py-16 text-center",
        className,
      )}
    >
      <span className="flex size-14 items-center justify-center rounded-full bg-surface-active text-ink-subtle">
        <Icon aria-hidden="true" className="size-6" />
      </span>
      <div className="flex flex-col gap-1">
        <p className="text-md font-medium text-ink">{title}</p>
        {description ? (
          <p className="max-w-sm text-sm text-ink-muted">{description}</p>
        ) : null}
      </div>
      {action ? <div className="mt-1 flex gap-2">{action}</div> : null}
    </div>
  );
}

/**
 * 错误状态。
 *
 * 说明「发生了什么」并给出「怎么重试」。原始错误信息原样展示：
 * 后台的使用者多半是站长本人，含糊其辞只会让他去翻服务端日志。
 */
export function ErrorState({
  title = "载入失败",
  message,
  onRetry,
  className,
}: {
  title?: string;
  message: string;
  onRetry?: (() => void) | undefined;
  className?: string;
}) {
  return (
    <div
      role="alert"
      className={cn(
        "flex flex-col items-center justify-center gap-3 px-6 py-16 text-center",
        className,
      )}
    >
      <span className="flex size-14 items-center justify-center rounded-full bg-danger-soft text-danger">
        <AlertTriangle aria-hidden="true" className="size-6" />
      </span>
      <div className="flex flex-col gap-1">
        <p className="text-md font-medium text-ink">{title}</p>
        <p className="token max-w-md text-sm text-ink-muted">{message}</p>
      </div>
      {onRetry ? (
        <Button variant="secondary" size="sm" onClick={onRetry}>
          <RotateCw aria-hidden="true" />
          重新载入
        </Button>
      ) : null}
    </div>
  );
}
