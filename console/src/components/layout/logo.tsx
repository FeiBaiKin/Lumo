import { cn } from "@/lib/utils";

/**
 * 品牌标识：一枚「印」+ 字标。
 *
 * 图形是一方带圆角的印章，里面是 L 的负形 —— 与全站唯一的强调色「印」同源。
 * 纯 SVG 内联，不依赖任何图片资源：Console 要能离线跑，
 * 而一个内联的矢量标识在任何机器上都长一样。
 */
export function LogoMark({ className }: { className?: string }) {
  return (
    <svg
      viewBox="0 0 32 32"
      aria-hidden="true"
      className={cn("size-8 shrink-0", className)}
    >
      <rect x="2" y="2" width="28" height="28" rx="7" className="fill-seal" />
      <path
        d="M11.5 9.5v13h9"
        className="stroke-seal-on"
        strokeWidth="3.2"
        strokeLinecap="round"
        strokeLinejoin="round"
        fill="none"
      />
    </svg>
  );
}

export function Logo({
  className,
  size = "md",
}: {
  className?: string;
  size?: "md" | "lg";
}) {
  return (
    <span className={cn("inline-flex items-center gap-2.5", className)}>
      <LogoMark className={size === "lg" ? "size-10" : "size-8"} />
      <span
        className={cn(
          "font-semibold tracking-tight text-ink",
          size === "lg" ? "text-2xl" : "text-xl",
        )}
      >
        Lumo
      </span>
    </span>
  );
}
