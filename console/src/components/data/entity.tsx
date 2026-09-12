import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
  KebabButton,
} from "@/components/ui/dropdown-menu";
import { EmptyState, EntitySkeleton, ErrorState } from "@/components/ui/states";
import { Checkbox } from "@/components/ui/toggle";
import { cn } from "@/lib/utils";
import { ChevronDown, FilterX, type LucideIcon, RefreshCw } from "lucide-react";
import type { ComponentProps, ReactNode } from "react";
import { Link } from "react-router";

/**
 * 实体列表 —— Halo 列表页的基本单位（VEntityContainer / VEntity / VEntityField）。
 *
 * 后台的列表不是电子表格：站长看一行时要的是「这是什么、什么状态、谁、什么时候」，
 * 不是逐列对齐的数据。故一行分成三段：
 *   开始段 —— 勾选框、缩略图或头像、标题 + 一行灰色描述
 *   结束段 —— 若干条 12px 的弱化元信息（状态点、作者、时间）
 *   动作   —— 末尾的「更多」菜单，悬停时浮出，触屏常显
 *
 * 全部列表页的加载 / 空 / 错误 / 正常四态收在 ListBody 一处，
 * 否则很快会变成「有的空状态有引导、有的只有一行字」。
 */

export function EntityList({
  className,
  children,
  ...props
}: ComponentProps<"ul">) {
  return (
    <ul className={cn("divide-y divide-line", className)} {...props}>
      {children}
    </ul>
  );
}

/**
 * 一行实体。
 *
 * `selected` 时底色变为选中色并在左缘画一条 2px 的印色线 —— 与 Halo 同形，
 * 只是颜色换成了 Lumo 的「印」。
 * `footer` 渲染在主行下方（评论的回复、展开的详情）。
 */
export function Entity({
  selected = false,
  align = "center",
  footer,
  className,
  children,
  ...props
}: ComponentProps<"li"> & {
  selected?: boolean;
  /** 多行正文（评论）用 start，让右侧的状态与时间顶对齐。 */
  align?: "center" | "start";
  footer?: ReactNode;
}) {
  return (
    <li
      className={cn(
        "group/entity transition-ui relative",
        selected ? "bg-seal-soft/60" : "hover:bg-surface-hover",
        className,
      )}
      aria-selected={selected || undefined}
      {...props}
    >
      {selected ? (
        <span
          aria-hidden="true"
          className="absolute inset-y-0 left-0 w-0.5 bg-seal"
        />
      ) : null}
      <div
        className={cn(
          "flex justify-between gap-4 px-4 py-3",
          align === "start" ? "items-start" : "items-center",
        )}
      >
        {children}
      </div>
      {footer}
    </li>
  );
}

export function EntityStart({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      className={cn("flex min-w-0 flex-1 items-center gap-4", className)}
      {...props}
    />
  );
}

export function EntityEnd({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      className={cn(
        "flex shrink-0 items-center justify-end gap-4 sm:gap-6",
        className,
      )}
      {...props}
    />
  );
}

/**
 * 标题 + 描述的堆叠字段。
 *
 * 标题有 `to` 时是站内链接、有 `href` 时是外链，否则是普通文字。
 * `extra` 放在标题右侧（置顶、私密这类标记）。
 */
export function EntityField({
  title,
  description,
  to,
  href,
  extra,
  width,
  className,
  children,
}: {
  title?: ReactNode;
  description?: ReactNode;
  to?: string;
  href?: string;
  extra?: ReactNode;
  /** 最大宽度类，默认 max-w-xs；标题列用 max-w-md 等。 */
  width?: string;
  className?: string;
  children?: ReactNode;
}) {
  const titleClass = "truncate text-md font-medium text-ink";
  let heading: ReactNode = null;
  if (title !== undefined) {
    if (to) {
      heading = (
        <Link
          to={to}
          className={cn(titleClass, "transition-ui hover:text-seal")}
        >
          {title}
        </Link>
      );
    } else if (href) {
      heading = (
        <a
          href={href}
          target="_blank"
          rel="noopener noreferrer"
          className={cn(titleClass, "transition-ui hover:text-seal")}
        >
          {title}
        </a>
      );
    } else {
      heading = <span className={titleClass}>{title}</span>;
    }
  }
  return (
    <div
      className={cn(
        "flex min-w-0 flex-col gap-0.5",
        width ?? "max-w-xs",
        className,
      )}
    >
      {heading || extra ? (
        <div className="flex min-w-0 items-center gap-2">
          {heading}
          {extra}
        </div>
      ) : null}
      {description ? (
        <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-0.5 text-xs text-ink-muted">
          {description}
        </div>
      ) : null}
      {children}
    </div>
  );
}

/** 结束段里的一条弱化元信息。 */
export function EntityMeta({
  className,
  hideOnMobile = false,
  ...props
}: ComponentProps<"div"> & { hideOnMobile?: boolean }) {
  return (
    <div
      className={cn(
        "flex shrink-0 items-center gap-1.5 text-xs whitespace-nowrap text-ink-muted",
        hideOnMobile && "hidden sm:flex",
        className,
      )}
      {...props}
    />
  );
}

/** 缩略图。固定 5:3，加载前后不跳；没有图时显示图标。 */
export function EntityThumb({
  src,
  alt = "",
  icon: Icon,
  className,
}: {
  src?: string | undefined | null;
  alt?: string;
  icon?: LucideIcon;
  className?: string;
}) {
  return (
    <span
      className={cn(
        "flex h-thumb-h w-thumb-w shrink-0 items-center justify-center overflow-hidden rounded-control bg-surface-active text-ink-subtle",
        className,
      )}
    >
      {src ? (
        <img
          src={src}
          alt={alt}
          loading="lazy"
          className="size-full object-cover"
        />
      ) : Icon ? (
        <Icon aria-hidden="true" className="size-5" />
      ) : null}
    </span>
  );
}

/** 行首的勾选框。 */
export function EntityCheckbox({
  checked,
  onCheckedChange,
  label,
  className,
}: {
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
  label: string;
  className?: string;
}) {
  return (
    <Checkbox
      checked={checked}
      onCheckedChange={(value) => onCheckedChange(value === true)}
      aria-label={label}
      className={cn("hidden sm:flex", className)}
    />
  );
}

/** 行末的「更多」菜单。children 是 DropdownMenuItem 列表。 */
export function EntityActions({
  label,
  children,
}: {
  label: string;
  children: ReactNode;
}) {
  return (
    <DropdownMenu modal={false}>
      <DropdownMenuTrigger asChild>
        <KebabButton label={label} />
      </DropdownMenuTrigger>
      <DropdownMenuContent className="min-w-[11rem]">
        {children}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

/**
 * 列表工具条：卡片标题栏下方的那条浅灰带。
 *
 * 左侧是全选 + 搜索框；勾选任意行后，搜索框原位被批量按钮组替换 ——
 * 不另起一条吸底的批量条（Halo 的做法，也省一行高度）。
 * 右侧是文字式筛选下拉、清除筛选与刷新。
 */
export function ListToolbar({
  selectAll,
  search,
  bulk,
  filters,
  hasFilters = false,
  onClearFilters,
  onRefresh,
  refreshing = false,
  className,
}: {
  selectAll?: {
    checked: boolean;
    indeterminate: boolean;
    onChange: (checked: boolean) => void;
    disabled?: boolean;
  };
  search?: ReactNode;
  /** 有选中项时替换搜索框的批量动作区。为空表示当前没有选中项。 */
  bulk?: ReactNode;
  filters?: ReactNode;
  hasFilters?: boolean;
  onClearFilters?: () => void;
  onRefresh?: () => void;
  refreshing?: boolean;
  className?: string;
}) {
  return (
    <div
      className={cn(
        "flex flex-wrap items-center gap-3 bg-surface-raised px-4 py-2.5 sm:gap-4",
        className,
      )}
    >
      {selectAll ? (
        <Checkbox
          checked={
            selectAll.indeterminate ? "indeterminate" : selectAll.checked
          }
          disabled={selectAll.disabled}
          onCheckedChange={(value) => selectAll.onChange(value === true)}
          aria-label="选择本页全部"
          className="hidden sm:flex"
        />
      ) : null}

      <div className="flex min-w-0 flex-1 items-center gap-2">
        {bulk ? (
          <div className="flex flex-wrap items-center gap-2">{bulk}</div>
        ) : (
          search
        )}
      </div>

      <div className="flex flex-wrap items-center gap-3 sm:gap-4">
        {filters}
        {hasFilters && onClearFilters ? (
          <Button
            variant="ghost"
            size="icon-xs"
            onClick={onClearFilters}
            aria-label="清除筛选"
            title="清除筛选"
            className="rounded-full bg-seal-soft text-seal hover:bg-seal-soft-strong hover:text-seal"
          >
            <FilterX aria-hidden="true" />
          </Button>
        ) : null}
        {onRefresh ? (
          <Button
            variant="ghost"
            size="icon-xs"
            onClick={onRefresh}
            aria-label="刷新列表"
            title="刷新"
          >
            <RefreshCw
              aria-hidden="true"
              className={cn(refreshing && "animate-spin")}
            />
          </Button>
        ) : null}
      </div>
    </div>
  );
}

export type FilterOption = { value: string; label: string };

/**
 * 文字式筛选下拉：「状态」→ 选中后加粗成「状态：草稿」。
 *
 * 值为空串表示「全部」，此时只显示名字。选项里的第一项通常就是全部。
 */
export function FilterMenu({
  label,
  value,
  options,
  onChange,
}: {
  label: string;
  value: string;
  options: FilterOption[];
  onChange: (value: string) => void;
}) {
  const current = options.find((option) => option.value === value);
  const active = value !== "" && current !== undefined;
  return (
    <DropdownMenu modal={false}>
      <DropdownMenuTrigger asChild>
        <button
          type="button"
          className={cn(
            "transition-ui flex items-center gap-1 rounded-control text-sm whitespace-nowrap",
            active ? "font-semibold text-ink" : "text-ink-muted hover:text-ink",
          )}
        >
          {active ? `${label}：${current.label}` : label}
          <ChevronDown aria-hidden="true" className="size-3.5" />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuRadioGroup value={value} onValueChange={onChange}>
          {options.map((option) => (
            <DropdownMenuRadioItem key={option.value} value={option.value}>
              {option.label}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

/**
 * 列表主体：负责四态。
 *
 * 加载态给出与真实行等高的骨架，而不是一个居中的转圈：
 * 转圈会让整块区域在数据到达时跳一下，且看不出「这里将会是一列几行的东西」。
 */
export function ListBody({
  isLoading,
  error,
  onRetry,
  isEmpty,
  empty,
  children,
  skeletonRows = 6,
  thumb = false,
}: {
  isLoading: boolean;
  error?: Error | null;
  onRetry?: (() => void) | undefined;
  isEmpty: boolean;
  empty: ReactNode;
  children: ReactNode;
  skeletonRows?: number;
  /** 骨架里是否留缩略图位。 */
  thumb?: boolean;
}) {
  if (isLoading) {
    return (
      <div className="divide-y divide-line" aria-busy="true">
        <span className="sr-only">正在载入</span>
        {Array.from({ length: skeletonRows }, (_, i) => (
          // biome-ignore lint/suspicious/noArrayIndexKey: 骨架行是静态占位，不会重排
          <EntitySkeleton key={i} thumb={thumb} />
        ))}
      </div>
    );
  }
  if (error) {
    return <ErrorState message={error.message} onRetry={onRetry} />;
  }
  if (isEmpty) {
    return <>{empty}</>;
  }
  return <EntityList>{children}</EntityList>;
}

/** 列表为空时的默认内容，供各页在同一形态下改文案与动作。 */
export function ListEmpty({
  icon,
  title,
  description,
  action,
}: {
  icon?: LucideIcon | undefined;
  title: string;
  description?: string | undefined;
  action?: ReactNode;
}) {
  return (
    <EmptyState
      icon={icon}
      title={title}
      description={description}
      action={action}
    />
  );
}
