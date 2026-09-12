import { cn } from "@/lib/utils";
import {
  AlertTriangle,
  CheckCircle2,
  Info,
  type LucideIcon,
  XCircle,
} from "lucide-react";
import type { ComponentProps, ReactNode } from "react";

/**
 * 行内提示条。
 *
 * 表单顶部的错误摘要、页面上的警示（主题加载失败）、操作结果（测试邮件已入队）都用它。
 * 语义按语气选：danger / warn 用 role="alert"（立即播报），ok / info 用 role="status"。
 */

const TONE: Record<
  "info" | "ok" | "warn" | "danger",
  { icon: LucideIcon; className: string; role: "alert" | "status" }
> = {
  info: {
    icon: Info,
    className: "border-seal/30 bg-seal-soft text-ink [&_svg]:text-seal",
    role: "status",
  },
  ok: {
    icon: CheckCircle2,
    className: "border-ok/30 bg-ok-soft text-ink [&_svg]:text-ok",
    role: "status",
  },
  warn: {
    icon: AlertTriangle,
    className: "border-warn/30 bg-warn-soft text-ink [&_svg]:text-warn",
    role: "alert",
  },
  danger: {
    icon: XCircle,
    className: "border-danger/30 bg-danger-soft text-ink [&_svg]:text-danger",
    role: "alert",
  },
};

export function Alert({
  tone = "info",
  title,
  className,
  children,
  ...props
}: ComponentProps<"div"> & {
  tone?: keyof typeof TONE;
  title?: ReactNode;
}) {
  const meta = TONE[tone];
  const Icon = meta.icon;
  return (
    <div
      role={meta.role}
      className={cn(
        "flex gap-2.5 rounded-control border px-3 py-2.5 text-sm",
        meta.className,
        className,
      )}
      {...props}
    >
      <Icon aria-hidden="true" className="mt-0.5 size-4 shrink-0" />
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        {title ? <p className="font-medium">{title}</p> : null}
        {children ? <div className="text-ink-muted">{children}</div> : null}
      </div>
    </div>
  );
}
