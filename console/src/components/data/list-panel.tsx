import { EmptyState, ErrorState, Skeleton } from "@/components/ui/states";
import {
  TBody,
  TD,
  TDRow,
  TH,
  THead,
  TR,
  Table,
  TableWrapper,
} from "@/components/ui/table";
import { cn } from "@/lib/utils";
import type { ReactNode } from "react";

/**
 * 列表页的公共骨架。
 *
 * 八个列表页（文章 / 页面 / 分类 / 标签 / 评论 / 附件 / 菜单 / 用户）的加载、空、
 * 错误、正常四种状态如果各写各的，很快就会变成「有的空状态有引导、有的只有一行字」。
 * 这里把四态与表头结构固定下来，页面只提供列定义与行渲染。
 */

export type Column = {
  /** 列标题。空串表示该列不显示表头文字（如操作列）。 */
  label: string;
  /** 数字列右对齐。 */
  numeric?: boolean;
  className?: string;
};

export function ListPanel({
  title,
  description,
  actions,
  toolbar,
  footer,
  children,
  className,
}: {
  title?: string;
  description?: string;
  actions?: ReactNode;
  /** 筛选条，位于表头之上、面板之内。 */
  toolbar?: ReactNode;
  footer?: ReactNode;
  children: ReactNode;
  className?: string;
}) {
  return (
    <section
      className={cn("rounded-panel border border-line bg-surface", className)}
    >
      {title || actions ? (
        <header className="flex min-h-12 flex-wrap items-center justify-between gap-3 px-4 py-2.5">
          <div className="flex min-w-0 flex-col">
            {title ? (
              <h2 className="text-lg font-semibold text-ink">{title}</h2>
            ) : null}
            {description ? (
              <p className="text-xs text-ink-muted">{description}</p>
            ) : null}
          </div>
          {actions ? (
            <div className="flex items-center gap-2">{actions}</div>
          ) : null}
        </header>
      ) : null}

      {toolbar ? (
        <div className="flex flex-wrap items-center gap-2 border-line border-t px-4 py-2.5">
          {toolbar}
        </div>
      ) : null}

      {children}

      {footer}
    </section>
  );
}

/** 筛选条里的搜索框宽度固定，避免各页面各写各的。 */
export function ToolbarSearch({
  className,
  ...props
}: React.ComponentProps<"div">) {
  return (
    <div className={cn("relative w-full sm:w-64", className)} {...props} />
  );
}

/** 把筛选条推到右侧（用于「每页条数」之类的次要控件）。 */
export function ToolbarSpacer() {
  return <div className="flex-1" />;
}

/**
 * 列表主体：负责四态。
 *
 * 关于骨架屏：加载时给出与真实行等高的占位，而不是一个居中的转圈。
 * 转圈会让整块区域在数据到达时跳一下，且看不出「这里将会是一张几行的表」。
 */
export function ListBody({
  columns,
  isLoading,
  error,
  onRetry,
  isEmpty,
  empty,
  children,
  skeletonRows = 6,
}: {
  columns: Column[];
  isLoading: boolean;
  error?: Error | null;
  onRetry?: () => void;
  isEmpty: boolean;
  empty: ReactNode;
  children: ReactNode;
  skeletonRows?: number;
}) {
  return (
    <TableWrapper className="border-line border-t">
      <Table>
        <THead>
          <TR>
            {columns.map((column, index) => (
              <TH
                // 列标题可能重复（如两个空标题的列），故拼上位置
                key={`${column.label}-${index}`}
                className={cn(column.numeric && "text-right", column.className)}
              >
                {column.label}
              </TH>
            ))}
          </TR>
        </THead>
        <TBody>
          {isLoading ? (
            Array.from({ length: skeletonRows }, (_, rowIndex) => (
              // biome-ignore lint/suspicious/noArrayIndexKey: 骨架行是静态占位，不会重排
              <TR key={rowIndex}>
                {columns.map((column, colIndex) => (
                  <TD
                    // biome-ignore lint/suspicious/noArrayIndexKey: 同上，静态占位
                    key={colIndex}
                    className={cn(column.numeric && "text-right")}
                  >
                    <Skeleton className="h-4 w-full" />
                  </TD>
                ))}
              </TR>
            ))
          ) : error ? (
            <TDRow colSpan={columns.length}>
              <ErrorState message={error.message} onRetry={onRetry} />
            </TDRow>
          ) : isEmpty ? (
            <TDRow colSpan={columns.length}>{empty}</TDRow>
          ) : (
            children
          )}
        </TBody>
      </Table>
    </TableWrapper>
  );
}

/** 列表为空时的默认内容，供各页在同一形态下改文案与动作。 */
export function ListEmpty({
  title,
  description,
  action,
}: {
  title: string;
  description: string;
  action?: ReactNode;
}) {
  return <EmptyState title={title} description={description} action={action} />;
}
