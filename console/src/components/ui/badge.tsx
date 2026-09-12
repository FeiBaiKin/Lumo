import { cn } from "@/lib/utils";
import { type VariantProps, cva } from "class-variance-authority";
import type { ComponentProps } from "react";

/**
 * 标签（Halo 的 VTag）。
 *
 * 用于分类、角色、模板名这类「贴在实体上的名词」。
 * 状态（已发布 / 待审）不用它 —— 状态用 StatusDot：圆点 + 文字，
 * 比一块色底更轻，也更符合 Halo 的列表语言。
 *
 * 关键约束（agent.md §11.3）：颜色不得单独表意，故本组件强制 children。
 */

const badgeVariants = cva(
  cn(
    "inline-flex max-w-full shrink-0 items-center gap-1 rounded-control border px-1.5",
    "font-medium whitespace-nowrap [&_svg]:size-3",
  ),
  {
    variants: {
      tone: {
        neutral: "border-transparent bg-surface-active text-ink-muted",
        seal: "border-transparent bg-seal-soft text-seal",
        ok: "border-transparent bg-ok-soft text-ok",
        warn: "border-transparent bg-warn-soft text-warn",
        danger: "border-transparent bg-danger-soft text-danger",
        outline: "border-line-strong bg-transparent text-ink-muted",
      },
      size: {
        sm: "h-5 text-xs",
        md: "h-6 text-xs px-2",
      },
    },
    defaultVariants: { tone: "neutral", size: "sm" },
  },
);

export type BadgeProps = ComponentProps<"span"> &
  VariantProps<typeof badgeVariants>;

export function Badge({ className, tone, size, ...props }: BadgeProps) {
  return (
    <span className={cn(badgeVariants({ tone, size }), className)} {...props} />
  );
}

export { badgeVariants };
