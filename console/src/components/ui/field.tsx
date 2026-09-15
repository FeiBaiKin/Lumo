import { cn } from "@/lib/utils";
import type { ComponentProps } from "react";

/**
 * 表单字段容器。
 *
 * 把「标签 / 说明 / 控件 / 错误」四件套固定下来，是为了满足两条硬要求：
 *   - 错误必须内联在字段旁，并与控件通过 aria-describedby 关联
 *   - 说明文字（helper）与错误文字不能互相顶替，两者可同时存在
 *
 * 曾经想过做成 render-prop 收控件，但那样每个自定义控件都要适配一层。
 * 改为容器 + 三个可组合小组件，控件本身保持是原生元素，aria 属性由调用方显式传。
 */

export function Field({ className, ...props }: ComponentProps<"div">) {
  return <div className={cn("flex flex-col gap-1.5", className)} {...props} />;
}

export function FieldLabel({ className, ...props }: ComponentProps<"label">) {
  return (
    // 这是一个可复用的原语：控件由调用方通过 htmlFor 关联，此处无从得知是哪一个。
    // biome-ignore lint/a11y/noLabelWithoutControl: 关联由调用方经 htmlFor 提供
    <label
      className={cn("text-sm font-medium text-ink select-none", className)}
      {...props}
    />
  );
}

/** 标签右侧的可选标注：必填、单位、取值范围。 */
export function FieldHint({ className, ...props }: ComponentProps<"span">) {
  return (
    <span className={cn("text-xs text-ink-muted", className)} {...props} />
  );
}

export function FieldDescription({ className, ...props }: ComponentProps<"p">) {
  return <p className={cn("text-xs text-ink-muted", className)} {...props} />;
}

/**
 * 字段级错误。
 *
 * `role="alert"` 让它在出现时被屏幕阅读器播报；id 由调用方经
 * `aria-describedby` 接到控件上。两件事都必须做 —— 只给颜色是纯视觉提示。
 */
export function FieldError({
  className,
  children,
  ...props
}: ComponentProps<"p">) {
  if (!children) {
    return null;
  }
  return (
    <p role="alert" className={cn("text-xs text-danger", className)} {...props}>
      {children}
    </p>
  );
}
