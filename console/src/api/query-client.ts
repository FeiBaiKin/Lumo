import { QueryClient } from "@tanstack/react-query";

/**
 * 全局唯一的查询客户端。
 *
 * 单独成模块，是为了让非 React 的代码（`api/mutation.ts` 里的缓存失效）也能拿到它，
 * 而不必层层透传。Provider 与命令式调用共用同一个实例是这里的全部要点 ——
 * 各建一个的话，命令式失效的那个缓存与界面读的那个缓存根本不是同一份。
 */
export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // 后台的数据变更几乎全部由本页发起，窗口聚焦时全量重取只会造成无谓的闪烁。
      refetchOnWindowFocus: false,
      staleTime: 30_000,
      // 401 不重试：它是「未登录」这一正常状态，重试只会拖慢跳转登录页。
      retry: (failureCount, error) => {
        if (error instanceof Error && error.message.includes("401")) {
          return false;
        }
        return failureCount < 2;
      },
    },
    mutations: { retry: false },
  },
});
