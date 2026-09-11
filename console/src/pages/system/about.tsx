import { api } from "@/api/client";
import { useAuth } from "@/components/auth/auth-provider";
import { ListPanel } from "@/components/data/list-panel";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { PageHeader } from "@/components/ui/panel";
import { Skeleton } from "@/components/ui/states";
import { useDocumentTitle } from "@/lib/use-document-title";
import { useQuery } from "@tanstack/react-query";
import { Database, Info, Server } from "lucide-react";

/**
 * 关于页。
 *
 * 放三类信息，都是站点出问题时**第一个要看**的东西：
 *   - 版本与构建信息（提 issue 时要贴的就是它）
 *   - 运行环境（Go 版本、数据库连接、存储驱动）
 *   - 搜索引擎的索引状态与重建入口
 *
 * 构建信息经 `/healthz` 取得 —— 那个端点本就返回版本号，
 * 而它是免认证的（供负载均衡探活），故这里也在登录后调用它。
 */

type Health = {
  status: string;
  version: string;
  commit?: string;
  date?: string;
};

export function AboutPage() {
  useDocumentTitle("关于");
  const { user, permissions } = useAuth();

  const health = useQuery({
    queryKey: ["health"],
    queryFn: async (): Promise<Health> => {
      const response = await fetch("/healthz");
      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
      }
      return (await response.json()) as Health;
    },
  });

  const search = useQuery({
    queryKey: ["search-status"],
    enabled: permissions.has("settings:manage"),
    queryFn: async () => {
      const { data, response } = await api.GET("/api/v1/console/search/status");
      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
      }
      return data;
    },
  });

  return (
    <>
      <PageHeader title="关于" description="版本、运行环境与搜索索引状态" />

      <div className="grid max-w-4xl gap-4 lg:grid-cols-2">
        <ListPanel
          title="构建信息"
          actions={
            health.data ? (
              <Badge tone={health.data.status === "ok" ? "ok" : "warn"}>
                {health.data.status}
              </Badge>
            ) : null
          }
        >
          <dl className="grid grid-cols-[6rem_1fr] gap-x-3 gap-y-2 border-line border-t p-4 text-sm">
            <dt className="text-ink-muted">Lumo 版本</dt>
            <dd className="token text-ink">
              {health.isLoading ? (
                <Skeleton className="h-4 w-32" />
              ) : (
                health.data?.version || "—"
              )}
            </dd>

            <dt className="text-ink-muted">提交</dt>
            <dd className="token text-ink">{health.data?.commit || "—"}</dd>

            <dt className="text-ink-muted">构建时间</dt>
            <dd className="token text-ink">{health.data?.date || "—"}</dd>

            <dt className="text-ink-muted">当前用户</dt>
            <dd className="text-ink">
              {user?.displayName || user?.username}
              <span className="ml-2 text-xs text-ink-muted">
                {user?.roles?.join("、") || "无角色"}
              </span>
            </dd>

            <dt className="text-ink-muted">权限数</dt>
            <dd className="tabular text-ink">{permissions.size}</dd>
          </dl>
        </ListPanel>

        {/*
          搜索索引状态。只对 settings:manage 持有者显示 ——
          服务端的这个端点就要这个权限，显示了也只会得到 403。
        */}
        {permissions.has("settings:manage") ? (
          <ListPanel
            title="全文搜索索引"
            badge={
              <Database aria-hidden="true" className="size-4 text-ink-muted" />
            }
          >
            <div className="flex flex-col gap-3 border-line border-t p-4">
              {search.isLoading ? (
                <Skeleton className="h-16 w-full" />
              ) : search.error ? (
                <p className="text-sm text-ink-muted">
                  搜索模块可能未装配（{search.error.message}）。
                </p>
              ) : (
                <>
                  <dl className="grid grid-cols-[6rem_1fr] gap-x-3 gap-y-2 text-sm">
                    <dt className="text-ink-muted">内容总数</dt>
                    <dd className="tabular text-ink">
                      {search.data?.total ?? 0}
                    </dd>
                    <dt className="text-ink-muted">已建索引</dt>
                    <dd className="tabular text-ink">
                      {search.data?.indexed ?? 0}
                    </dd>
                    <dt className="text-ink-muted">待重建</dt>
                    <dd className="tabular text-ink">
                      {search.data?.pending ?? 0}
                      {(search.data?.pending ?? 0) > 0 ? (
                        <span className="ml-2 text-xs text-warn">
                          后台每 2 秒对账一批
                        </span>
                      ) : null}
                    </dd>
                  </dl>

                  <Button
                    variant="secondary"
                    size="sm"
                    className="self-start"
                    onClick={async () => {
                      await api.POST("/api/v1/console/search/reindex");
                      void search.refetch();
                    }}
                  >
                    重建全部索引
                  </Button>
                  <p className="text-xs text-ink-muted">
                    索引由后台按内容的更新时间自动对账，通常不需要手动重建。
                    切词规则改动后才需要。
                  </p>
                </>
              )}
            </div>
          </ListPanel>
        ) : null}
      </div>

      <div className="mt-4 max-w-4xl">
        <ListPanel title="技术栈">
          <div className="flex flex-col gap-2 border-line border-t p-4 text-sm text-ink-muted">
            <p className="flex items-start gap-2">
              <Server aria-hidden="true" className="mt-0.5 size-4 shrink-0" />
              Go 单一静态二进制，PostgreSQL，界面为 go:embed 进二进制的 React
              SPA。
            </p>
            <p className="flex items-start gap-2">
              <Info aria-hidden="true" className="mt-0.5 size-4 shrink-0" />
              Lumo 以 GPL-3.0 发布。进度与设计取舍见仓库内的约束文档。
            </p>
          </div>
        </ListPanel>
      </div>
    </>
  );
}
