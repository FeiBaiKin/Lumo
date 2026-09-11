import { type Problem, problemMessage } from "@/api/client";
import { queryClient } from "@/api/query-client";
import { toast } from "sonner";

/**
 * 把一次写操作的结果统一成「成功提示 + 缓存失效」或「抛出一个可直接展示的错误」。
 *
 * 之所以统一到一处：后台的写操作有几十个，各处自己 `if (!response.ok) throw` 的写法
 * 迟早会出现忘记检查响应、或错误文案格式不一的情况。这里把三件事绑在一起 ——
 * 检查响应、失效缓存、播报结果 —— 调用方只关心「成功了要做什么」。
 *
 * 缓存失效用 invalidateQueries 而不是乐观更新：后台的列表有一堆筛选与分页组合，
 * 乐观更新要同时改对每一份缓存，出错的代价是列表显示一条其实不存在的记录。
 * 重新拉一次几百毫秒，但结果一定与服务端一致。
 */

/** 结果形如 openapi-fetch 的返回值。 */
export type ApiResult<T> = {
  data?: T | undefined;
  error?: Problem | undefined;
  response: Response;
};

export async function runMutation<T>(
  call: () => Promise<ApiResult<T>>,
  options: {
    /** 成功后的提示。留空则不播报（用于静默的次要操作）。 */
    success?: string;
    /** 需要失效的查询键前缀；传 ["posts"] 会失效全部以 posts 开头的查询。 */
    invalidate?: readonly unknown[];
  } = {},
): Promise<T> {
  const { data, error, response } = await call();

  if (!response.ok) {
    const message = problemMessage(error);
    // 失败也要播报：只在内联位置显示错误的话，在滚动到别处时用户看不到任何反馈
    toast.error(message);
    throw new Error(message);
  }

  if (options.invalidate) {
    await queryClient.invalidateQueries({ queryKey: options.invalidate });
  }
  if (options.success) {
    toast.success(options.success);
  }
  return data as T;
}
