import { cn } from "@/lib/utils";
import { type VariantProps, cva } from "class-variance-authority";
import type { ComponentProps } from "react";

/**
 * 状态标签。
 *
 * 关键约束（agent.md §11.3）：颜色**不得单独表意**。故本组件强制 children，
 * 且调用方要一并给出图标或文字 —— 色盲用户看不到「红」与「绿」的区别，
 * 但看得到「回收站」与「已发布」。
 */

const badgeVariants = cva(
  cn(
    "inline-flex shrink-0 items-center gap-1 rounded-control border px-1.5 py-0.5",
    "text-xs font-medium whitespace-nowrap [&_svg]:size-3",
  ),
  {
    variants: {
      tone: {
        neutral: "border-line bg-surface-raised text-ink-muted",
        seal: "border-transparent bg-seal-soft text-seal",
        ok: "border-transparent bg-ok-soft text-ok",
        warn: "border-transparent bg-warn-soft text-warn",
        danger: "border-transparent bg-danger-soft text-danger",
        outline: "border-line-strong bg-transparent text-ink-muted",
      },
    },
    defaultVariants: { tone: "neutral" },
  },
);

export type BadgeProps = ComponentProps<"span"> &
  VariantProps<typeof badgeVariants>;

export function Badge({ className, tone, ...props }: BadgeProps) {
  return <span className={cn(badgeVariants({ tone }), className)} {...props} />;
}

export { badgeVariants };
