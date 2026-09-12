import { cn } from "@/lib/utils";
import * as SliderPrimitive from "@radix-ui/react-slider";
import type { ComponentProps } from "react";

/**
 * 拖动条。
 *
 * 适用于「有明确上下限、且用户会来回试着调」的数值（栏宽、每页条数）——
 * 拖动时能立刻看到结果，比在输入框里敲数字再回车快得多。
 *
 * 上下限跨度很大或精度要求高的字段不要用它：1 到 10000 的滑动条拖不准也看不出当前值，
 * 那种场合用数字输入框。
 */
export function Slider({
  className,
  ariaLabel,
  ...props
}: ComponentProps<typeof SliderPrimitive.Root> & {
  /**
   * 滑块的无障碍名。
   *
   * 显式传而不是靠 Root 上的 aria-label：Radix 只在多滑块时才把它转给 Thumb，
   * 单滑块时留在 Root 上，而读屏用户操作的是 Thumb。这个差别决定了
   * 「用键盘调整栏宽」时听得到听不到自己在调什么。
   */
  ariaLabel?: string;
}) {
  return (
    <SliderPrimitive.Root
      className={cn(
        "relative flex w-full max-w-md touch-none select-none items-center py-1.5",
        "disabled:cursor-not-allowed disabled:opacity-55",
        className,
      )}
      {...props}
    >
      <SliderPrimitive.Track className="relative h-1 w-full grow overflow-hidden rounded-full bg-line-strong">
        <SliderPrimitive.Range className="absolute h-full bg-action" />
      </SliderPrimitive.Track>
      <SliderPrimitive.Thumb
        aria-label={ariaLabel}
        className={cn(
          "transition-ui block size-4 rounded-full border border-line-strong bg-surface shadow-card",
          "hover:border-action",
        )}
      />
    </SliderPrimitive.Root>
  );
}
