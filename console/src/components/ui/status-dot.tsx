import { cn } from "@/lib/utils";
import type { ComponentProps } from "react";

/**
 * 状态点（Halo 的 VStatusDot）：一枚圆点 + 文字。
 *
 * 列表里每一行都要报一次状态，一块色底会让整页发花；圆点只占 6px，
 * 而文字保证色盲用户也读得出「已发布」与「回收站」的区别。
 * `pulse` 只用于「正在进行」的状态（定时待发、处理中）。
 */

export type DotState = "neutral" | "ok" | "warn" | "danger" | "seal";

const DOT_CLASS: Record<DotState, string> = {
  neutral: "bg-neutral-dot",
  ok: "bg-ok-dot",
  warn: "bg-warn-dot",
  danger: "bg-danger-dot",
  seal: "bg-seal",
};

const TEXT_CLASS: Record<DotState, string> = {
  neutral: "text-ink-muted",
  ok: "text-ink",
  warn: "text-ink",
  danger: "text-danger",
  seal: "text-ink",
};

export function StatusDot({
  state,
  pulse = false,
  className,
  children,
  ...props
}: ComponentProps<"span"> & { state: DotState; pulse?: boolean }) {
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 text-xs whitespace-nowrap",
        TEXT_CLASS[state],
        className,
      )}
      {...props}
    >
      <span className="relative flex size-1.5 shrink-0">
        {pulse ? (
          <span
            aria-hidden="true"
            className={cn(
              "absolute inline-flex size-full animate-ping rounded-full opacity-60",
              DOT_CLASS[state],
            )}
          />
        ) : null}
        <span
          aria-hidden="true"
          className={cn(
            "relative inline-flex size-1.5 rounded-full",
            DOT_CLASS[state],
          )}
        />
      </span>
      {children}
    </span>
  );
}
