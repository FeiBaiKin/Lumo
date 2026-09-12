import { cn } from "@/lib/utils";
import type { ComponentProps } from "react";

/** 键盘按键提示。用于命令面板入口与菜单里的快捷键。 */
export function Kbd({ className, ...props }: ComponentProps<"kbd">) {
  return (
    <kbd
      className={cn(
        "inline-flex h-5 min-w-5 items-center justify-center rounded-[3px] border border-line bg-surface px-1 font-sans text-[11px] whitespace-nowrap text-ink-muted",
        className,
      )}
      {...props}
    />
  );
}

/** 当前平台的修饰键名。 */
export function modifierKey(): string {
  if (typeof navigator === "undefined") {
    return "Ctrl";
  }
  return /Mac|iPhone|iPad/.test(navigator.platform) ? "⌘" : "Ctrl";
}
