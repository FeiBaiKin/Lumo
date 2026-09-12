import { cn } from "@/lib/utils";
import type { ComponentProps, ReactNode } from "react";

/**
 * 卡片（Halo 的 VCard）。
 *
 * 内容区里的每一块都是一张卡片：白底、1px 描边、极轻的一层托底阴影、6px 圆角。
 * 卡片之间靠器底的浅灰分开，卡片内部靠分隔线分区。
 * 一页里出现几张卡片是正常的，但卡片里不再套卡片 —— 嵌套要用 `inset` 区块。
 */

export function Card({ className, ...props }: ComponentProps<"section">) {
  return (
    <section
      className={cn(
        "rounded-card border border-line bg-surface shadow-card",
        className,
      )}
      {...props}
    />
  );
}

/**
 * 卡片标题栏：标题（+ 标记）在左，动作在右。
 * `title` 保持字符串，标题层级与样式才能全站一致。
 */
export function CardHeader({
  title,
  badge,
  description,
  actions,
  className,
  children,
}: {
  title?: ReactNode;
  badge?: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  className?: string;
  /** 标题栏下方的附加内容（如筛选条）。 */
  children?: ReactNode;
}) {
  return (
    <header className={cn("flex flex-col border-line border-b", className)}>
      {title || actions ? (
        <div className="flex min-h-13 flex-wrap items-center justify-between gap-3 px-4 py-2.5">
          <div className="flex min-w-0 flex-col">
            {title ? (
              <h2 className="flex items-center gap-2 text-lg font-semibold text-ink">
                {title}
                {badge}
              </h2>
            ) : null}
            {description ? (
              <p className="text-xs text-ink-muted">{description}</p>
            ) : null}
          </div>
          {actions ? (
            <div className="flex flex-wrap items-center gap-2">{actions}</div>
          ) : null}
        </div>
      ) : null}
      {children}
    </header>
  );
}

export function CardBody({ className, ...props }: ComponentProps<"div">) {
  return <div className={cn("p-4", className)} {...props} />;
}

export function CardFooter({ className, ...props }: ComponentProps<"footer">) {
  return (
    <footer
      className={cn(
        "flex flex-wrap items-center justify-end gap-2 border-line border-t px-4 py-3",
        className,
      )}
      {...props}
    />
  );
}

/** 卡片内的嵌入区块：浅灰底、无阴影。用于分组、预览、代码这类需要「框起来」的内容。 */
export function Inset({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      className={cn(
        "rounded-control border border-line bg-surface-raised p-3",
        className,
      )}
      {...props}
    />
  );
}

/**
 * 名称—值对（详情、构建信息）。
 * 用 dl 而不是表格：这是「名称—值」对，不是表格数据。
 */
export function DescriptionList({ className, ...props }: ComponentProps<"dl">) {
  return (
    <dl
      className={cn(
        "grid grid-cols-[7rem_minmax(0,1fr)] gap-x-4 gap-y-2.5 text-base",
        className,
      )}
      {...props}
    />
  );
}

export function DescriptionTerm({ className, ...props }: ComponentProps<"dt">) {
  return <dt className={cn("text-sm text-ink-muted", className)} {...props} />;
}

export function DescriptionDetail({
  className,
  ...props
}: ComponentProps<"dd">) {
  return <dd className={cn("min-w-0 text-ink", className)} {...props} />;
}
