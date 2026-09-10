import { type ClassValue, clsx } from "clsx";
import { twMerge } from "tailwind-merge";

// shadcn/ui 约定的类名合并工具：clsx 处理条件类名，twMerge 消解 Tailwind 冲突。
export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs));
}
