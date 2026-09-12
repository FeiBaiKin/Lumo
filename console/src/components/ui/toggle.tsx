import { cn } from "@/lib/utils";
import * as CheckboxPrimitive from "@radix-ui/react-checkbox";
import * as SwitchPrimitive from "@radix-ui/react-switch";
import { Check, Minus } from "lucide-react";
import type { ComponentProps } from "react";

/**
 * 复选框与开关。
 *
 * 两者的分工是刻意区分的：
 *   复选框 —— 一组选项里的多选，或列表里的批量选择（提交时才生效）
 *   开关 —— 单个设置项的即时生效（拨动即保存）
 * 混用会让用户不知道「这个改动什么时候生效」。
 *
 * 选中态用「墨」（主行动色）而不是「印」：勾选是一次动作，与按下主按钮同级。
 */

export function Checkbox({
  className,
  ...props
}: ComponentProps<typeof CheckboxPrimitive.Root>) {
  return (
    <CheckboxPrimitive.Root
      className={cn(
        "transition-ui flex size-4 shrink-0 items-center justify-center rounded-[3px]",
        "border border-line-strong bg-surface",
        "hover:border-ink-muted",
        "data-[state=checked]:border-action data-[state=checked]:bg-action data-[state=checked]:text-action-on",
        "data-[state=indeterminate]:border-action data-[state=indeterminate]:bg-action data-[state=indeterminate]:text-action-on",
        "disabled:cursor-not-allowed disabled:opacity-55",
        className,
      )}
      {...props}
    >
      <CheckboxPrimitive.Indicator>
        {props.checked === "indeterminate" ? (
          <Minus aria-hidden="true" className="size-3" strokeWidth={3} />
        ) : (
          <Check aria-hidden="true" className="size-3" strokeWidth={3} />
        )}
      </CheckboxPrimitive.Indicator>
    </CheckboxPrimitive.Root>
  );
}

/** 复选框 + 文字 + 可选说明，整行可点。 */
export function CheckboxRow({
  id,
  checked,
  onCheckedChange,
  label,
  description,
  disabled,
  className,
}: {
  id: string;
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
  label: React.ReactNode;
  description?: React.ReactNode;
  disabled?: boolean;
  className?: string;
}) {
  return (
    <label
      htmlFor={id}
      className={cn(
        "transition-ui flex cursor-pointer items-start gap-2.5 rounded-control px-2 py-1.5 hover:bg-surface-hover",
        disabled && "cursor-not-allowed opacity-60",
        className,
      )}
    >
      <Checkbox
        id={id}
        checked={checked}
        disabled={disabled}
        onCheckedChange={(value) => onCheckedChange(value === true)}
        className="mt-0.5"
      />
      <span className="flex min-w-0 flex-col">
        <span className="text-base text-ink">{label}</span>
        {description ? (
          <span className="text-xs text-ink-muted">{description}</span>
        ) : null}
      </span>
    </label>
  );
}

export function Switch({
  className,
  ...props
}: ComponentProps<typeof SwitchPrimitive.Root>) {
  return (
    <SwitchPrimitive.Root
      className={cn(
        "transition-ui relative inline-flex h-5 w-9 shrink-0 items-center rounded-full",
        "border border-transparent",
        "data-[state=unchecked]:bg-line-strong",
        "data-[state=checked]:bg-action",
        "disabled:cursor-not-allowed disabled:opacity-55",
        className,
      )}
      {...props}
    >
      <SwitchPrimitive.Thumb
        className={cn(
          "transition-ui pointer-events-none block size-4 rounded-full bg-white shadow-card",
          "translate-x-0.5 data-[state=checked]:translate-x-[1.125rem]",
          "dark:data-[state=checked]:bg-black",
        )}
      />
    </SwitchPrimitive.Root>
  );
}

/** 开关 + 标签 + 说明的整行，用于设置页。点击文字即可切换。 */
export function SwitchRow({
  id,
  checked,
  onCheckedChange,
  label,
  description,
  disabled,
  className,
}: {
  id: string;
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
  label: React.ReactNode;
  description?: React.ReactNode;
  disabled?: boolean;
  className?: string;
}) {
  return (
    <div
      className={cn("flex items-start justify-between gap-4 py-2", className)}
    >
      <div className="flex min-w-0 flex-col gap-0.5">
        <label
          htmlFor={id}
          className="text-base font-medium text-ink select-none"
        >
          {label}
        </label>
        {description ? (
          <p className="text-xs text-ink-muted">{description}</p>
        ) : null}
      </div>
      <Switch
        id={id}
        checked={checked}
        onCheckedChange={onCheckedChange}
        disabled={disabled}
        className="mt-0.5"
      />
    </div>
  );
}
