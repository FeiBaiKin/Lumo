import { api } from "@/api/client";
import {
  type NavGroup,
  type NavItem,
  buildNavigation,
  routeLabels,
} from "@/components/layout/nav";
import { resolveIcon } from "@/lib/icons";
import { useQuery } from "@tanstack/react-query";
import { useMemo } from "react";

/**
 * 侧边栏菜单的查询。
 *
 * 缓存时间设为无限：菜单只在装了插件或改了模块构成时才变，
 * 而那是要重启服务端或重新加载 Console 的操作，届时整份缓存本来就会重建。
 * 每次进入后台都重取一遍，换来的是一个「菜单偶尔闪一下」的启动过程，不值。
 */
export function useNavigation() {
  const query = useQuery({
    queryKey: ["navigation"],
    queryFn: async () => {
      const { data, response } = await api.GET("/api/v1/console/navigation");
      if (!response.ok) {
        throw new Error(`载入菜单失败（HTTP ${response.status}）`);
      }
      return data;
    },
    staleTime: Number.POSITIVE_INFINITY,
  });

  const groups: NavGroup[] = useMemo(
    () => buildNavigation(query.data?.groups, query.data?.items),
    [query.data],
  );
  // 扁平清单含隐藏项：个人中心不进侧边栏，但它确实是能跳过去的一页，
  // 命令面板里少一项就少了一条入口。
  const items: NavItem[] = useMemo(() => {
    const out: NavItem[] = [];
    for (const group of query.data?.groups ?? []) {
      for (const item of query.data?.items ?? []) {
        if (item.group !== group.name) {
          continue;
        }
        out.push({
          key: item.key,
          label: item.label,
          to: item.path,
          icon: resolveIcon(item.icon),
          permission: item.permission || undefined,
          keywords: item.keywords || undefined,
          description: item.description || undefined,
          end: item.end || undefined,
        });
      }
    }
    return out;
  }, [query.data]);
  const labels = useMemo(() => routeLabels(query.data?.items), [query.data]);

  return {
    groups,
    items,
    labels,
    isLoading: query.isLoading,
    error: query.error,
  };
}
