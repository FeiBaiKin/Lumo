import { cn } from "@/lib/utils";
import * as RadioGroupPrimitive from "@radix-ui/react-radio-group";
import type { ComponentProps } from "react";

/**
 * 单选组。
 *
 * 与下拉（Select）的分工：选项少于一屏能一眼看全时用单选，多则用下拉。
 * 设置页里它多半是「三选一」这类开关式的选择，平铺出来比藏进下拉少一次点击。
 *
 * 选中态用「墨」（主行动色），与复选框、开关一致。
 */
export function RadioGroup({
  className,
  ...props
}: ComponentProps<typeof RadioGroupPrimitive.Root>) {
  return (
    <RadioGroupPrimitive.Root
      className={cn("flex flex-col gap-0.5", className)}
      {...props}
    />
  );
}

/** 单选项 + 文字 + 可选说明，整行可点。形态对齐 CheckboxRow。 */
export function RadioRow({
  id,
  value,
  label,
  description,
  disabled,
  className,
}: {
  id: string;
  value: string;
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
      <RadioGroupPrimitive.Item
        id={id}
        value={value}
        disabled={disabled}
        className={cn(
          "transition-ui mt-0.5 flex size-4 shrink-0 items-center justify-center rounded-full",
          "border border-line-strong bg-surface",
          "hover:border-ink-muted",
          "data-[state=checked]:border-action",
          "disabled:cursor-not-allowed disabled:opacity-55",
        )}
      >
        <RadioGroupPrimitive.Indicator className="size-2 rounded-full bg-action" />
      </RadioGroupPrimitive.Item>
      <span className="flex min-w-0 flex-col">
        <span className="text-base text-ink">{label}</span>
        {description ? (
          <span className="text-xs text-ink-muted">{description}</span>
        ) : null}
      </span>
    </label>
  );
}
