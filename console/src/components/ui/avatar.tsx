import { cn } from "@/lib/utils";
import * as AvatarPrimitive from "@radix-ui/react-avatar";

/**
 * 头像。
 *
 * 没有图片时显示名字的首字。中文名取第一个字、拉丁名取首字母大写 ——
 * 不画一个通用的人形图标：一列人形图标让用户列表里每个人长得一样。
 */

const SIZE_CLASS = {
  xs: "size-6 text-xs",
  sm: "size-8 text-sm",
  md: "size-10 text-base",
  lg: "size-14 text-lg",
} as const;

export function initialOf(name: string | undefined | null): string {
  const trimmed = (name ?? "").trim();
  if (!trimmed) {
    return "?";
  }
  const first = [...trimmed][0] ?? "?";
  return first.toUpperCase();
}

export function Avatar({
  src,
  name,
  size = "sm",
  className,
  square = false,
}: {
  src?: string | undefined | null;
  name: string | undefined | null;
  size?: keyof typeof SIZE_CLASS;
  className?: string;
  /** 站点或主题这类非人物用方形。 */
  square?: boolean;
}) {
  return (
    <AvatarPrimitive.Root
      className={cn(
        "relative flex shrink-0 overflow-hidden bg-surface-active text-ink-muted select-none",
        square ? "rounded-control" : "rounded-full",
        SIZE_CLASS[size],
        className,
      )}
    >
      {src ? (
        <AvatarPrimitive.Image
          src={src}
          alt=""
          className="size-full object-cover"
        />
      ) : null}
      <AvatarPrimitive.Fallback
        delayMs={src ? 300 : 0}
        className="flex size-full items-center justify-center font-medium"
      >
        {initialOf(name)}
      </AvatarPrimitive.Fallback>
    </AvatarPrimitive.Root>
  );
}
