import { cn } from "@/lib/utils";
import * as SelectPrimitive from "@radix-ui/react-select";
import { Check, ChevronDown } from "lucide-react";
import type { ComponentProps } from "react";

/**
 * 下拉选择。
 *
 * 用于「从一组固定选项里选一个」。若选项超过约 15 条或需要搜索，
 * 应改用带筛选的组合框 —— 长下拉在后台是常见的交互债。
 */

export const Select = SelectPrimitive.Root;
export const SelectValue = SelectPrimitive.Value;
export const SelectGroup = SelectPrimitive.Group;

export function SelectTrigger({
  className,
  children,
  ...props
}: ComponentProps<typeof SelectPrimitive.Trigger>) {
  return (
    <SelectPrimitive.Trigger
      className={cn(
        "transition-ui flex h-9 w-full items-center justify-between gap-2",
        "rounded-control border border-line-strong bg-surface px-3 text-md text-ink",
        "hover:border-ink-subtle",
        "data-[placeholder]:text-ink-subtle",
        "disabled:cursor-not-allowed disabled:bg-surface-raised disabled:text-ink-muted",
        "[&>span]:truncate",
        className,
      )}
      {...props}
    >
      {children}
      <SelectPrimitive.Icon asChild>
        <ChevronDown
          aria-hidden="true"
          className="size-4 shrink-0 text-ink-muted"
        />
      </SelectPrimitive.Icon>
    </SelectPrimitive.Trigger>
  );
}

export function SelectContent({
  className,
  children,
  position = "popper",
  ...props
}: ComponentProps<typeof SelectPrimitive.Content>) {
  return (
    <SelectPrimitive.Portal>
      <SelectPrimitive.Content
        position={position}
        className={cn(
          "panel-in relative z-popover max-h-72 min-w-[8rem] overflow-hidden",
          "shadow-overlay rounded-overlay border border-line bg-surface",
          position === "popper" &&
            "data-[side=bottom]:mt-1 data-[side=top]:mb-1",
          className,
        )}
        {...props}
      >
        <SelectPrimitive.Viewport
          className={cn(
            "p-1",
            position === "popper" &&
              "w-full min-w-[var(--radix-select-trigger-width)]",
          )}
        >
          {children}
        </SelectPrimitive.Viewport>
      </SelectPrimitive.Content>
    </SelectPrimitive.Portal>
  );
}

export function SelectItem({
  className,
  children,
  ...props
}: ComponentProps<typeof SelectPrimitive.Item>) {
  return (
    <SelectPrimitive.Item
      className={cn(
        "transition-ui relative flex cursor-default items-center gap-2 rounded-control py-1.5 pr-8 pl-2 text-base text-ink select-none",
        "data-[highlighted]:bg-surface-active data-[highlighted]:outline-none",
        "data-[disabled]:pointer-events-none data-[disabled]:text-ink-subtle",
        className,
      )}
      {...props}
    >
      <SelectPrimitive.ItemText>{children}</SelectPrimitive.ItemText>
      <SelectPrimitive.ItemIndicator className="absolute right-2 flex items-center">
        <Check aria-hidden="true" className="size-4 text-seal" />
      </SelectPrimitive.ItemIndicator>
    </SelectPrimitive.Item>
  );
}

export function SelectLabel({
  className,
  ...props
}: ComponentProps<typeof SelectPrimitive.Label>) {
  return (
    <SelectPrimitive.Label
      className={cn("px-2 py-1.5 text-xs text-ink-muted", className)}
      {...props}
    />
  );
}
