import { cn } from "@/lib/utils";
import { Slot } from "@radix-ui/react-slot";
import { type VariantProps, cva } from "class-variance-authority";
import type { ComponentProps } from "react";

/**
 * 按钮。
 *
 * 变体只有五种，每一种对应一种「这个动作有多重」：
 *   primary   —— 页面的主行动，一个界面里最多一个
 *   secondary —— 次行动（取消、返回、导出）
 *   ghost     —— 工具栏、行内操作，无边无底，只在悬停时浮出
 *   danger    —— 不可撤销的破坏性动作
 *   link      —— 看起来是链接的按钮（导航语义但触发动作）
 *
 * 尺寸只有三档。图标按钮与文字按钮同高，混排时不会错位。
 */

const buttonVariants = cva(
  cn(
    "transition-ui inline-flex shrink-0 items-center justify-center gap-2 whitespace-nowrap",
    "rounded-control font-medium select-none",
    // 禁用态保留可读性：把整个按钮压到半透明会让文字对比度掉到看不出写的什么，
    // 而后台里「为什么这个按钮不能点」恰恰需要看清它的文案。
    "disabled:cursor-not-allowed disabled:opacity-55",
    "[&_svg]:pointer-events-none [&_svg]:shrink-0",
  ),
  {
    variants: {
      variant: {
        primary: "bg-seal text-seal-on hover:bg-seal-hover",
        secondary:
          "border border-line-strong bg-surface text-ink hover:bg-surface-active",
        ghost: "text-ink hover:bg-surface-active",
        danger: "bg-danger text-danger-on hover:bg-danger-hover",
        link: "text-seal underline-offset-4 hover:underline",
      },
      size: {
        sm: "h-7 px-2.5 text-sm [&_svg]:size-3.5",
        md: "h-9 px-3 text-base [&_svg]:size-4",
        lg: "h-10 px-4 text-md [&_svg]:size-4",
        // 正方形图标按钮。44px 是触控最小命中区，但后台以鼠标为主，
        // 32/36px 能在不牺牲可用性的前提下多放一列操作。
        icon: "size-9 [&_svg]:size-4",
        "icon-sm": "size-7 [&_svg]:size-3.5",
      },
    },
    defaultVariants: { variant: "secondary", size: "md" },
  },
);

export type ButtonProps = ComponentProps<"button"> &
  VariantProps<typeof buttonVariants> & {
    /** 用子元素作为宿主（如把按钮样式套到 `<a>` 上）。 */
    asChild?: boolean;
  };

export function Button({
  className,
  variant,
  size,
  asChild = false,
  type,
  ...props
}: ButtonProps) {
  const Component = asChild ? Slot : "button";
  return (
    <Component
      className={cn(buttonVariants({ variant, size }), className)}
      // 不写 type 的 button 在 form 内默认是 submit —— 工具栏里的「取消」
      // 会意外提交表单。默认给 button，需要提交时显式写 type="submit"。
      type={asChild ? undefined : (type ?? "button")}
      {...props}
    />
  );
}

export { buttonVariants };
