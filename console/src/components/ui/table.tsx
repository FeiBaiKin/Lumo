import { cn } from "@/lib/utils";
import type { ComponentProps, ReactNode } from "react";

/**
 * 表格原语。
 *
 * **为什么不用 TanStack Table**：本项目所有列表都是服务端分页 —— 排序、筛选、
 * 翻页都由 Go 接口负责（agent.md §6 的 offset 分页），客户端从不持有全量数据。
 * 表引擎最有价值的几件事（客户端排序 / 筛选 / 分页）在这里全部用不上，
 * 剩下的只有「渲染表头与单元格」，那是下面这几十行的事。
 * 若日后真出现需要客户端排序的小数据集（如角色编辑器里的权限清单），
 * 那时再按需引入，不必现在背着一个用不到的引擎。
 *
 * 结构要求（检索的 High 级条目）：必须用语义化的 thead / tbody / th，
 * 不得用 div 网格假装表格 —— 读屏用户靠这些元素理解行列关系。
 * `th` 默认带 scope="col"，调用方若要行表头需显式传 scope="row"。
 */

export function TableWrapper({ className, ...props }: ComponentProps<"div">) {
  return (
    // 窄屏横向滚动（检索明确要求 overflow-x-auto 包裹，而不是让表格撑破布局）
    <div className={cn("w-full overflow-x-auto", className)} {...props} />
  );
}

export function Table({ className, ...props }: ComponentProps<"table">) {
  return (
    <table
      className={cn("w-full border-collapse text-base", className)}
      {...props}
    />
  );
}

export function THead({ className, ...props }: ComponentProps<"thead">) {
  return <thead className={cn("border-line border-b", className)} {...props} />;
}

export function TBody({ className, ...props }: ComponentProps<"tbody">) {
  return <tbody className={cn("divide-y divide-line", className)} {...props} />;
}

export function TR({ className, ...props }: ComponentProps<"tr">) {
  return <tr className={cn("group/row", className)} {...props} />;
}

export function TH({
  className,
  scope = "col",
  ...props
}: ComponentProps<"th">) {
  return (
    <th
      scope={scope}
      className={cn(
        "h-9 px-4 text-left text-sm font-medium whitespace-nowrap text-ink-muted",
        className,
      )}
      {...props}
    />
  );
}

export function TD({ className, ...props }: ComponentProps<"td">) {
  return (
    <td
      className={cn("px-4 py-2.5 align-middle text-ink", className)}
      {...props}
    />
  );
}

/** 数字列：右对齐 + 等宽数字，否则一列计数会歪。 */
export function TDNumber({ className, ...props }: ComponentProps<"td">) {
  return <TD className={cn("tabular text-right", className)} {...props} />;
}

/** 行内操作区。默认靠右，且在没有 hover 能力的设备上始终可见。 */
export function TDActions({ className, ...props }: ComponentProps<"td">) {
  return (
    <TD
      className={cn("w-px text-right whitespace-nowrap", className)}
      {...props}
    />
  );
}

/** 空状态要横跨整个表格宽度。 */
export function TDRow({
  colSpan,
  children,
}: { colSpan: number; children: ReactNode }) {
  return (
    <tr>
      <td colSpan={colSpan} className="p-0">
        {children}
      </td>
    </tr>
  );
}
