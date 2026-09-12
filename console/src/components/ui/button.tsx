import { cn } from "@/lib/utils";
import { Slot } from "@radix-ui/react-slot";
import { type VariantProps, cva } from "class-variance-authority";
import { Loader2 } from "lucide-react";
import type { ComponentProps } from "react";

/**
 * 按钮。
 *
 * 变体对应「这个动作有多重」，与 Halo 的 VButton 分工一致：
 *   primary   —— 页面的主行动。墨底白字（暗色下反转），一个界面里最多一个
 *   secondary —— 次行动：白底描边（Halo 的 default）
 *   ghost     —— 工具栏、行内操作，无边无底，只在悬停时浮出
 *   danger    —— 不可撤销的破坏性动作
 *   link      —— 看起来是链接的按钮（导航语义但触发动作）
 *
 * 尺寸四档加三档图标按钮。图标按钮与同档文字按钮同高，混排时不会错位。
 * `loading` 会把前置图标换成转圈并禁用按钮 —— 各处不必再手写三元。
 */

const buttonVariants = cva(
  cn(
    "transition-ui inline-flex shrink-0 items-center justify-center gap-1.5 whitespace-nowrap",
    "rounded-control font-medium select-none",
    // 禁用态保留可读性：把整个按钮压到半透明会让文字对比度掉到看不出写的什么，
    // 而后台里「为什么这个按钮不能点」恰恰需要看清它的文案。
    "disabled:cursor-not-allowed disabled:opacity-55",
    "[&_svg]:pointer-events-none [&_svg]:shrink-0",
  ),
  {
    variants: {
      variant: {
        primary: "bg-action text-action-on hover:bg-action-hover",
        secondary:
          "border border-line-strong bg-surface text-ink hover:bg-surface-hover",
        ghost: "text-ink-muted hover:bg-surface-active hover:text-ink",
        danger: "bg-danger text-danger-on hover:bg-danger-hover",
        link: "h-auto px-0 text-seal underline-offset-4 hover:underline",
      },
      size: {
        xs: "h-7 px-2 text-xs [&_svg]:size-3.5",
        sm: "h-8 px-2.5 text-sm [&_svg]:size-3.5",
        md: "h-9 px-3 text-base [&_svg]:size-4",
        lg: "h-10 px-4 text-md [&_svg]:size-4",
        icon: "size-9 [&_svg]:size-4",
        "icon-sm": "size-8 [&_svg]:size-4",
        "icon-xs": "size-7 [&_svg]:size-3.5",
      },
    },
    defaultVariants: { variant: "secondary", size: "md" },
  },
);

export type ButtonProps = ComponentProps<"button"> &
  VariantProps<typeof buttonVariants> & {
    /** 用子元素作为宿主（如把按钮样式套到 `<a>` 上）。 */
    asChild?: boolean;
    /** 进行中：显示转圈并禁用。 */
    loading?: boolean;
  };

export function Button({
  className,
  variant,
  size,
  asChild = false,
  loading = false,
  disabled,
  type,
  children,
  ...props
}: ButtonProps) {
  const Component = asChild ? Slot : "button";
  return (
    <Component
      className={cn(buttonVariants({ variant, size }), className)}
      // 不写 type 的 button 在 form 内默认是 submit —— 工具栏里的「取消」
      // 会意外提交表单。默认给 button，需要提交时显式写 type="submit"。
      type={asChild ? undefined : (type ?? "button")}
      // 两个条件都必须进 disabled：写成 `disabled ?? loading` 时，
      // 调用方传 disabled={false} 就会让 loading 完全失效，
      // 慢网下连点「保存」等于连发多个写请求。
      disabled={asChild ? undefined : loading || disabled}
      aria-busy={loading || undefined}
      {...props}
    >
      {loading ? (
        <>
          <Loader2 aria-hidden="true" className="animate-spin" />
          {/* 转圈替代前置图标：把子节点里的第一个 svg 藏掉，避免两个图标并排 */}
          <span className="contents [&>svg:first-child]:hidden">
            {children}
          </span>
        </>
      ) : (
        children
      )}
    </Component>
  );
}

export { buttonVariants };
