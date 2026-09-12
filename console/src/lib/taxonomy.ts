import { api } from "@/api/client";
import type { components } from "@/api/schema";
import { useQuery } from "@tanstack/react-query";

/**
 * 分类与标签候选的共享查询。
 *
 * 文章列表与编辑器都要这份数据，但需要的形式不同：列表只要筛选项
 * （分类名 + 标签 slug/label），编辑器要完整实体（分类的 id/name/depth、
 * 标签的 id/name 用来勾选）。
 *
 * 关键约定：**缓存里只放原始 DTO**，各页面自己转换。
 * 同一个 queryKey 下放两种结构的后果是真实的：谁先请求谁说了算，
 * 后到的页面拿到形状不对的数据，渲染出 `content-category-undefined`
 * 这类控件 ID，分类关联就会以 undefined 落库。
 */
export type TaxonomyOptions = {
  categories: components["schemas"]["CategoryNode"][];
  tags: components["schemas"]["Tag"][];
};

export const TAXONOMY_QUERY_KEY = ["taxonomy-options"] as const;

/** 分类树摊平成带缩进的选项，供列表筛选与编辑器的勾选列表共用。 */
export function flattenCategories(
  nodes: components["schemas"]["CategoryNode"][],
  depth = 0,
): { id: number; name: string; slug: string; depth: number }[] {
  const out: { id: number; name: string; slug: string; depth: number }[] = [];
  for (const node of nodes) {
    out.push({ id: node.id, name: node.name, slug: node.slug, depth });
    if (node.children?.length) {
      out.push(...flattenCategories(node.children, depth + 1));
    }
  }
  return out;
}

/**
 * 读取分类与标签候选。
 *
 * enabled 为假时不请求（页面类型不参与分类与标签）。
 */
export function useTaxonomyOptions(enabled = true) {
  return useQuery({
    queryKey: TAXONOMY_QUERY_KEY,
    enabled,
    // 分类与标签变动不频繁，但改完之后列表要跟着变，
    // 所以失效由写入方负责（见 taxonomy 页面的 invalidate）。
    staleTime: 60_000,
    queryFn: async (): Promise<TaxonomyOptions> => {
      const [categories, tags] = await Promise.all([
        api.GET("/api/v1/console/categories/tree"),
        api.GET("/api/v1/console/tags", { params: { query: { size: 100 } } }),
      ]);
      return {
        categories: categories.data?.items ?? [],
        tags: tags.data?.items ?? [],
      };
    },
  });
}
