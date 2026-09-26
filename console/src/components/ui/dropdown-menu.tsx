import { Button, type ButtonProps } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import * as DropdownMenuPrimitive from "@radix-ui/react-dropdown-menu";
import { Check, ChevronRight, MoreHorizontal } from "lucide-react";
import type { ComponentProps } from "react";

/**
 * 下拉菜单。
 *
 * 实体行末尾的「更多」、账号菜单、筛选条的单选项都用它 ——
 * 一套浮层、一套键盘行为、一套样式。Radix 负责焦点管理与 typeahead。
 */

export const DropdownMenu = DropdownMenuPrimitive.Root;
export const DropdownMenuTrigger = DropdownMenuPrimitive.Trigger;
export const DropdownMenuGroup = DropdownMenuPrimitive.Group;
export const DropdownMenuRadioGroup = DropdownMenuPrimitive.RadioGroup;
export const DropdownMenuSub = DropdownMenuPrimitive.Sub;

export function DropdownMenuContent({
  className,
  sideOffset = 4,
  align = "end",
  ...props
}: ComponentProps<typeof DropdownMenuPrimitive.Content>) {
  return (
    <DropdownMenuPrimitive.Portal>
      <DropdownMenuPrimitive.Content
        sideOffset={sideOffset}
        align={align}
        className={cn(
          "popover-in z-popover min-w-[10rem] overflow-hidden rounded-overlay border border-line bg-surface p-1 shadow-popover",
          className,
        )}
        {...props}
      />
    </DropdownMenuPrimitive.Portal>
  );
}

const itemClass = cn(
  "transition-ui relative flex cursor-pointer items-center gap-2 rounded-control px-2 py-1.5 text-base text-ink outline-none select-none",
  "data-[highlighted]:bg-surface-active",
  "data-[disabled]:pointer-events-none data-[disabled]:opacity-50",
  "[&_svg]:size-4 [&_svg]:shrink-0 [&_svg]:text-ink-muted",
);

export function DropdownMenuItem({
  className,
  danger = false,
  ...props
}: ComponentProps<typeof DropdownMenuPrimitive.Item> & { danger?: boolean }) {
  return (
    <DropdownMenuPrimitive.Item
      className={cn(
        itemClass,
        danger &&
          "text-danger data-[highlighted]:bg-danger-soft [&_svg]:text-danger",
        className,
      )}
      {...props}
    />
  );
}

/** 子菜单的入口：长得和普通项一样，右侧一个箭头；展开时保持高亮，看得出子菜单是从哪一项出来的。 */
export function DropdownMenuSubTrigger({
  className,
  children,
  ...props
}: ComponentProps<typeof DropdownMenuPrimitive.SubTrigger>) {
  return (
    <DropdownMenuPrimitive.SubTrigger
      className={cn(
        itemClass,
        "data-[state=open]:bg-surface-active",
        className,
      )}
      {...props}
    >
      {children}
      <ChevronRight aria-hidden="true" className="ml-auto" />
    </DropdownMenuPrimitive.SubTrigger>
  );
}

export function DropdownMenuSubContent({
  className,
  sideOffset = 6,
  ...props
}: ComponentProps<typeof DropdownMenuPrimitive.SubContent>) {
  return (
    <DropdownMenuPrimitive.Portal>
      <DropdownMenuPrimitive.SubContent
        sideOffset={sideOffset}
        className={cn(
          "popover-in z-popover min-w-[10rem] overflow-hidden rounded-overlay border border-line bg-surface p-1 shadow-popover",
          className,
        )}
        {...props}
      />
    </DropdownMenuPrimitive.Portal>
  );
}

export function DropdownMenuCheckboxItem({
  className,
  children,
  ...props
}: ComponentProps<typeof DropdownMenuPrimitive.CheckboxItem>) {
  return (
    <DropdownMenuPrimitive.CheckboxItem
      className={cn(itemClass, "pr-8", className)}
      {...props}
    >
      {children}
      <DropdownMenuPrimitive.ItemIndicator className="absolute right-2 flex items-center">
        <Check aria-hidden="true" className="size-4 text-seal" />
      </DropdownMenuPrimitive.ItemIndicator>
    </DropdownMenuPrimitive.CheckboxItem>
  );
}

export function DropdownMenuRadioItem({
  className,
  children,
  ...props
}: ComponentProps<typeof DropdownMenuPrimitive.RadioItem>) {
  return (
    <DropdownMenuPrimitive.RadioItem
      className={cn(itemClass, "pr-8", className)}
      {...props}
    >
      {children}
      <DropdownMenuPrimitive.ItemIndicator className="absolute right-2 flex items-center">
        <Check aria-hidden="true" className="size-4 text-seal" />
      </DropdownMenuPrimitive.ItemIndicator>
    </DropdownMenuPrimitive.RadioItem>
  );
}

export function DropdownMenuLabel({
  className,
  ...props
}: ComponentProps<typeof DropdownMenuPrimitive.Label>) {
  return (
    <DropdownMenuPrimitive.Label
      className={cn("px-2 py-1.5 text-xs text-ink-muted", className)}
      {...props}
    />
  );
}

export function DropdownMenuSeparator({
  className,
  ...props
}: ComponentProps<typeof DropdownMenuPrimitive.Separator>) {
  return (
    <DropdownMenuPrimitive.Separator
      className={cn("-mx-1 my-1 h-px bg-line", className)}
      {...props}
    />
  );
}

/** 菜单项右侧的快捷键提示。 */
export function DropdownMenuShortcut({
  className,
  ...props
}: ComponentProps<"span">) {
  return (
    <span
      className={cn("ml-auto text-xs text-ink-subtle", className)}
      {...props}
    />
  );
}

/**
 * 「更多」按钮：实体行末尾的三个点。
 *
 * 必须带 aria-label 说明是谁的菜单 —— 一列只念「更多」的按钮对读屏用户毫无意义。
 */
export function KebabButton({
  label,
  className,
  size = "icon-sm",
  ...props
}: Omit<ButtonProps, "children" | "aria-label"> & { label: string }) {
  return (
    <Button
      variant="ghost"
      size={size}
      aria-label={label}
      title="更多操作"
      className={cn("entity-reveal", className)}
      {...props}
    >
      <MoreHorizontal aria-hidden="true" />
    </Button>
  );
}
