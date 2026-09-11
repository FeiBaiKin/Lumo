import { cn } from "@/lib/utils";
import * as CheckboxPrimitive from "@radix-ui/react-checkbox";
import * as SwitchPrimitive from "@radix-ui/react-switch";
import { Check, Minus } from "lucide-react";
import type { ComponentProps } from "react";

/**
 * 复选框与开关。
 *
 * 两者的分工是刻意区分的：
 *   复选框 —— 一组选项里的多选，或表格里的批量选择（提交时才生效）
 *   开关 —— 单个设置项的即时生效（拨动即保存）
 * 混用会让用户不知道「这个改动什么时候生效」。
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
        "hover:border-seal",
        "data-[state=checked]:border-seal data-[state=checked]:bg-seal data-[state=checked]:text-seal-on",
        "data-[state=indeterminate]:border-seal data-[state=indeterminate]:bg-seal data-[state=indeterminate]:text-seal-on",
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
        "data-[state=checked]:bg-seal",
        "disabled:cursor-not-allowed disabled:opacity-55",
        className,
      )}
      {...props}
    >
      <SwitchPrimitive.Thumb
        className={cn(
          "transition-ui pointer-events-none block size-4 rounded-full bg-white",
          "translate-x-0.5 data-[state=checked]:translate-x-[1.125rem]",
        )}
      />
    </SwitchPrimitive.Root>
  );
}

/** 开关 + 标签 + 说明的整行，用于设置页。点击整行即可切换。 */
export function SwitchRow({
  id,
  checked,
  onCheckedChange,
  label,
  description,
  disabled,
}: {
  id: string;
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
  label: string;
  description?: string | undefined;
  disabled?: boolean;
}) {
  return (
    <div className="flex items-start justify-between gap-4 py-3">
      <div className="flex min-w-0 flex-col gap-0.5">
        {/* label 与开关关联，点文字也能切换 */}
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
