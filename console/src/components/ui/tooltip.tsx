import { cn } from "@/lib/utils";
import * as TooltipPrimitive from "@radix-ui/react-tooltip";
import type { ComponentProps, ReactNode } from "react";

/**
 * 工具提示。
 *
 * 只用于图标按钮的名字与被截断文字的全文；不放操作说明书 ——
 * 悬停才看得到的文字对触屏用户不存在（检索点名的 High 级条目）。
 */

export const TooltipProvider = TooltipPrimitive.Provider;
export const Tooltip = TooltipPrimitive.Root;
export const TooltipTrigger = TooltipPrimitive.Trigger;

export function TooltipContent({
  className,
  sideOffset = 6,
  ...props
}: ComponentProps<typeof TooltipPrimitive.Content>) {
  return (
    <TooltipPrimitive.Portal>
      <TooltipPrimitive.Content
        sideOffset={sideOffset}
        className={cn(
          "popover-in z-popover max-w-xs rounded-control bg-ink px-2 py-1 text-xs text-action-on shadow-popover",
          className,
        )}
        {...props}
      />
    </TooltipPrimitive.Portal>
  );
}

/** 最常见的用法：给一个元素加一句提示。 */
export function Tip({
  label,
  children,
  side,
}: {
  label: ReactNode;
  children: ReactNode;
  side?: "top" | "bottom" | "left" | "right";
}) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>{children}</TooltipTrigger>
      <TooltipContent side={side ?? "top"}>{label}</TooltipContent>
    </Tooltip>
  );
}
