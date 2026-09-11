import { cn } from "@/lib/utils";
import { Loader2 } from "lucide-react";
import type { ComponentProps } from "react";

/**
 * 文本输入。
 *
 * 输入区字号用 text-md（15px）而非界面默认的 14px：输入区是用户真正要读要改的地方，
 * 比标签更值得多一个像素。
 */

export function Input({ className, ...props }: ComponentProps<"input">) {
  return (
    <input
      className={cn(
        "transition-ui h-9 w-full min-w-0 rounded-control border border-line-strong",
        "bg-surface px-3 text-md text-ink",
        "placeholder:text-ink-subtle",
        "hover:border-ink-subtle",
        "focus-visible:border-seal",
        "disabled:cursor-not-allowed disabled:bg-surface-raised disabled:text-ink-muted",
        // 校验失败时由 aria-invalid 驱动样式，不在各处手写红边框 ——
        // 这样「视觉上的错误」与「语义上的错误」永远同步。
        "aria-invalid:border-danger",
        className,
      )}
      {...props}
    />
  );
}

export function Textarea({ className, ...props }: ComponentProps<"textarea">) {
  return (
    <textarea
      className={cn(
        "transition-ui w-full min-w-0 rounded-control border border-line-strong",
        "bg-surface px-3 py-2 text-md text-ink",
        "placeholder:text-ink-subtle",
        "hover:border-ink-subtle",
        "focus-visible:border-seal",
        "disabled:cursor-not-allowed disabled:bg-surface-raised disabled:text-ink-muted",
        "aria-invalid:border-danger",
        "resize-y",
        className,
      )}
      {...props}
    />
  );
}

/** 输入框内嵌的前置/后置图标位，避免各页面各写各的 padding。 */
export function InputAffix({
  className,
  side,
  ...props
}: ComponentProps<"span"> & { side: "left" | "right" }) {
  return (
    <span
      className={cn(
        "pointer-events-none absolute top-1/2 -translate-y-1/2 text-ink-subtle [&_svg]:size-4",
        side === "left" ? "left-2.5" : "right-2.5",
        className,
      )}
      {...props}
    />
  );
}

/** 表单控件旁的加载指示。 */
export function Spinner({ className }: { className?: string }) {
  return (
    <Loader2
      aria-hidden="true"
      className={cn("size-4 animate-spin text-ink-muted", className)}
    />
  );
}
