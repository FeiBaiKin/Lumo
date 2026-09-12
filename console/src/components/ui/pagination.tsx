import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { ChevronLeft, ChevronRight } from "lucide-react";

/**
 * 分页条（Halo 的 VPagination）。
 *
 * 左侧「共 N 条」，右侧「每页条数 + 上一页 / 页码 / 下一页」。
 * 页码序列的省略规则与主题前台的翻页保持同一套思路：
 * 首尾恒显、当前页前后各一页、其余折叠成省略号。
 *
 * 采用 offset 分页（agent.md §6）：后台列表需要跳页，游标分页做不到。
 */

/** 生成要显示的页码；0 表示省略号。 */
export function pageWindow(
  current: number,
  totalPages: number,
  span = 1,
): number[] {
  if (totalPages <= 7) {
    return Array.from({ length: totalPages }, (_, i) => i + 1);
  }
  const pages = new Set<number>([1, totalPages, current]);
  for (let offset = 1; offset <= span; offset += 1) {
    if (current - offset >= 1) {
      pages.add(current - offset);
    }
    if (current + offset <= totalPages) {
      pages.add(current + offset);
    }
  }
  const sorted = [...pages].sort((a, b) => a - b);
  const out: number[] = [];
  let previous = 0;
  for (const value of sorted) {
    if (previous && value - previous > 1) {
      out.push(0);
    }
    out.push(value);
    previous = value;
  }
  return out;
}

export function Pagination({
  page,
  size,
  total,
  onPageChange,
  onSizeChange,
  className,
}: {
  page: number;
  size: number;
  total: number;
  onPageChange: (page: number) => void;
  /** 省略则不显示每页条数选择器。 */
  onSizeChange?: (size: number) => void;
  className?: string;
}) {
  const totalPages = Math.max(1, Math.ceil(total / size));
  const pages = pageWindow(page, totalPages);

  return (
    <div
      className={cn(
        "flex flex-wrap items-center justify-between gap-3 border-line border-t px-4 py-2.5",
        className,
      )}
    >
      <p className="tabular text-sm text-ink-muted">
        {total === 0 ? "没有记录" : `共 ${total} 条`}
      </p>

      <div className="flex flex-wrap items-center gap-3">
        {onSizeChange ? (
          <label className="flex items-center gap-1.5 text-sm text-ink-muted">
            每页
            <select
              value={size}
              onChange={(event) => onSizeChange(Number(event.target.value))}
              className="transition-ui h-8 rounded-control border border-line-strong bg-surface px-1.5 text-sm text-ink hover:border-ink-subtle"
            >
              {[10, 20, 50, 100].map((value) => (
                <option key={value} value={value}>
                  {value}
                </option>
              ))}
            </select>
            条
          </label>
        ) : null}

        {totalPages > 1 ? (
          <nav aria-label="分页" className="flex items-center gap-0.5">
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={() => onPageChange(page - 1)}
              disabled={page <= 1}
              aria-label="上一页"
            >
              <ChevronLeft aria-hidden="true" />
            </Button>

            {pages.map((value, index) =>
              value === 0 ? (
                // 省略号不是可操作项，用文本节点而不是被禁用的按钮 ——
                // 禁用按钮仍会被 Tab 到，读屏会念出一个点不动的「…」
                <span
                  // biome-ignore lint/suspicious/noArrayIndexKey: 相邻省略号不可能同时出现
                  key={`gap-${index}`}
                  aria-hidden="true"
                  className="px-1 text-xs text-ink-subtle"
                >
                  …
                </span>
              ) : (
                <Button
                  key={value}
                  variant={value === page ? "primary" : "ghost"}
                  size="icon-sm"
                  onClick={() => onPageChange(value)}
                  aria-current={value === page ? "page" : undefined}
                  aria-label={`第 ${value} 页`}
                  className="tabular text-sm"
                >
                  {value}
                </Button>
              ),
            )}

            <Button
              variant="ghost"
              size="icon-sm"
              onClick={() => onPageChange(page + 1)}
              disabled={page >= totalPages}
              aria-label="下一页"
            >
              <ChevronRight aria-hidden="true" />
            </Button>
          </nav>
        ) : null}
      </div>
    </div>
  );
}
