import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useSearchParams } from "react-router";

/**
 * 列表页的查询状态，与地址栏双向同步。
 *
 * 为什么不放在组件 state 里：后台最常见的动作是「筛出问题评论 → 点进去处理 →
 * 按浏览器后退回到刚才那一屏」。状态只存在内存里时，后退会回到未筛选的第一页，
 * 用户得重新筛一遍。同步到 URL 还有两个副作用，都是想要的：
 * 刷新不丢筛选、筛选结果的地址可以直接发给同事。
 *
 * 约定：`page` 与 `size` 是保留键，其余一律视为筛选项（status / q / category …），
 * 与 Go 接口的查询参数同名，故可以整包透传。
 */

export const PAGE_KEY = "page";
export const SIZE_KEY = "size";
export const DEFAULT_PAGE_SIZE = 20;

export type ListParams = {
  page: number;
  size: number;
  /** 除分页外的全部查询参数，可直接展开进 API 调用的 query。 */
  filters: Record<string, string>;
  /** 取某个筛选项，缺省为空串。 */
  filter: (key: string) => string;
  setFilter: (key: string, value: string) => void;
  setPage: (page: number) => void;
  setSize: (size: number) => void;
  /** 清空全部筛选并回到第一页。 */
  reset: () => void;
  /** 是否有任何筛选条件（用于决定要不要显示「清除筛选」）。 */
  hasFilters: boolean;
};

function positiveInt(raw: string | null, fallback: number): number {
  const value = Number(raw);
  return Number.isFinite(value) && value >= 1 ? Math.floor(value) : fallback;
}

export function useListParams(): ListParams {
  const [searchParams, setSearchParams] = useSearchParams();

  const page = positiveInt(searchParams.get(PAGE_KEY), 1);
  const size = positiveInt(searchParams.get(SIZE_KEY), DEFAULT_PAGE_SIZE);

  const filters = useMemo(() => {
    const out: Record<string, string> = {};
    for (const [key, value] of searchParams) {
      if (key !== PAGE_KEY && key !== SIZE_KEY && value !== "") {
        out[key] = value;
      }
    }
    return out;
  }, [searchParams]);

  const update = useCallback(
    (mutate: (next: URLSearchParams) => void) => {
      const next = new URLSearchParams(searchParams);
      mutate(next);
      // replace: 筛选与翻页不该在历史里堆成一长串，否则「后退」要点十几次才出得去。
      // 真正需要后退回来的场景，靠的是从列表跳到详情那一步（那是 push）。
      setSearchParams(next, { replace: true });
    },
    [searchParams, setSearchParams],
  );

  const setFilter = useCallback(
    (key: string, value: string) => {
      update((next) => {
        if (value === "") {
          next.delete(key);
        } else {
          next.set(key, value);
        }
        // 换了筛选条件就回到第一页：停在第三页看一个只有一页结果的筛选，
        // 得到的是空白，而用户会以为「没有匹配」。
        next.delete(PAGE_KEY);
      });
    },
    [update],
  );

  const setPage = useCallback(
    (value: number) => {
      update((next) => {
        if (value <= 1) {
          // 第一页不写 page 参数：/posts 与 /posts?page=1 是两个地址
          next.delete(PAGE_KEY);
        } else {
          next.set(PAGE_KEY, String(value));
        }
      });
    },
    [update],
  );

  const setSize = useCallback(
    (value: number) => {
      update((next) => {
        if (value === DEFAULT_PAGE_SIZE) {
          next.delete(SIZE_KEY);
        } else {
          next.set(SIZE_KEY, String(value));
        }
        next.delete(PAGE_KEY);
      });
    },
    [update],
  );

  const reset = useCallback(() => {
    update((next) => {
      for (const key of [...next.keys()]) {
        next.delete(key);
      }
    });
  }, [update]);

  const filter = useCallback((key: string) => filters[key] ?? "", [filters]);

  return {
    page,
    size,
    filters,
    filter,
    setFilter,
    setPage,
    setSize,
    reset,
    hasFilters: Object.keys(filters).length > 0,
  };
}

/**
 * 受控的搜索输入。
 *
 * 与列表状态分开的理由：打字时若每次都写进 URL，地址栏会随每个字符变一次，
 * 且每敲一个字就发一次请求。这里本地存值，停顿后再提交给列表状态。
 *
 * 三种情况的处理都在这个 hook 内，页面不必再写同步逻辑：
 *   - 打字：本地立刻更新（输入框不能卡），停顿 delay 后才提交
 *   - 外部值变化（浏览器后退、从带 q 的链接进来）：同步回输入框
 *   - 组件卸载：清掉未触发的计时器，否则会在已卸载的组件上调用 commit
 */
export function useDebouncedSearch(
  value: string,
  commit: (value: string) => void,
  delay = 300,
): [string, (next: string) => void] {
  const [local, setLocal] = useState(value);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  // 外部值变化时同步。先比较再 set，避免每次渲染都触发一次状态更新。
  useEffect(() => {
    setLocal((current) => (current === value ? current : value));
  }, [value]);

  useEffect(
    () => () => {
      if (timer.current) {
        clearTimeout(timer.current);
      }
    },
    [],
  );

  const onChange = useCallback(
    (next: string) => {
      setLocal(next);
      if (timer.current) {
        clearTimeout(timer.current);
      }
      timer.current = setTimeout(() => commit(next), delay);
    },
    [commit, delay],
  );

  return [local, onChange];
}
