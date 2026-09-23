import { api } from "@/api/client";
import { runMutation } from "@/api/mutation";
import { useAuth } from "@/components/auth/auth-provider";
import { PageBody, PageHeader } from "@/components/layout/page-header";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardBody,
  CardHeader,
  DescriptionDetail,
  DescriptionList,
  DescriptionTerm,
} from "@/components/ui/card";
import { Skeleton } from "@/components/ui/states";
import { StatusDot } from "@/components/ui/status-dot";
import { count } from "@/lib/format";
import { useDocumentTitle } from "@/lib/use-document-title";
import { UpdateCard } from "@/pages/system/update-card";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Database, Info, RefreshCw, Server } from "lucide-react";

/**
 * 关于页。
 *
 * 放四类信息，都是站点出问题时**第一个要看**的东西：
 *   - 在线升级（有新版本时，这是整页最有行动价值的一块，故排在最前）
 *   - 版本与构建信息（提 issue 时要贴的就是它）
 *   - 全文搜索索引的状态与重建入口
 *   - 运行形态（单一二进制、数据库、许可证）
 *
 * 构建信息取自需登录的 `/api/v1/console/build`。`/healthz` 是免认证的探活端点，
 * 只带版本号，这里仅用它点亮「运行正常」。
 */

type Health = {
  status: string;
};

export function AboutPage() {
  useDocumentTitle("关于");
  const { user, permissions } = useAuth();
  const canManage = permissions.has("settings:manage");

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

  const build = useQuery({
    queryKey: ["build-info"],
    queryFn: async () => {
      const { data, response } = await api.GET("/api/v1/console/build");
      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
      }
      return data;
    },
  });

  const search = useQuery({
    queryKey: ["search-status"],
    enabled: canManage,
    queryFn: async () => {
      const { data, response } = await api.GET("/api/v1/console/search/status");
      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
      }
      return data;
    },
  });

  const reindex = useMutation({
    mutationFn: () =>
      runMutation(() => api.POST("/api/v1/console/search/reindex"), {
        success: "已开始重建全文索引",
        invalidate: ["search-status"],
      }),
  });

  const healthy = health.data?.status === "ok";

  return (
    <>
      <PageHeader
        icon={Info}
        title="关于"
        description="版本、在线升级、运行环境与搜索索引状态"
      />

      <PageBody>
        {/* 升级接口要 settings:manage，没有这个权限的人连状态都读不到。 */}
        {canManage ? <UpdateCard /> : null}

        <div className="grid gap-4 lg:grid-cols-2">
          <Card>
            <CardHeader
              title="构建信息"
              actions={
                health.data ? (
                  <StatusDot state={healthy ? "ok" : "warn"}>
                    {healthy ? "运行正常" : health.data.status}
                  </StatusDot>
                ) : null
              }
            />
            <CardBody>
              <DescriptionList>
                <DescriptionTerm>Lumo 版本</DescriptionTerm>
                <DescriptionDetail className="token">
                  {build.isLoading ? (
                    <Skeleton className="h-4 w-32" />
                  ) : (
                    build.data?.version || "—"
                  )}
                </DescriptionDetail>

                <DescriptionTerm>提交</DescriptionTerm>
                <DescriptionDetail className="token">
                  {build.data?.commit || "—"}
                </DescriptionDetail>

                <DescriptionTerm>构建时间</DescriptionTerm>
                <DescriptionDetail className="token">
                  {build.data?.date || "—"}
                </DescriptionDetail>

                <DescriptionTerm>Go 版本</DescriptionTerm>
                <DescriptionDetail className="token">
                  {build.data?.goVersion || "—"}
                </DescriptionDetail>

                <DescriptionTerm>平台</DescriptionTerm>
                <DescriptionDetail className="token">
                  {build.data?.platform || "—"}
                </DescriptionDetail>

                <DescriptionTerm>当前用户</DescriptionTerm>
                <DescriptionDetail className="flex flex-wrap items-baseline gap-2">
                  <span>{user?.displayName || user?.username}</span>
                  <span className="text-xs text-ink-muted">
                    {user?.roleLabels?.join("、") || "无角色"}
                  </span>
                </DescriptionDetail>

                <DescriptionTerm>权限数</DescriptionTerm>
                <DescriptionDetail className="tabular">
                  {permissions.size}
                </DescriptionDetail>
              </DescriptionList>
            </CardBody>
          </Card>

          {/*
            搜索索引状态。只对 settings:manage 持有者显示 ——
            服务端的这个端点就要这个权限，显示了也只会得到 403。
          */}
          {canManage ? (
            <Card>
              <CardHeader
                title="全文搜索索引"
                badge={
                  <Database
                    aria-hidden="true"
                    className="size-4 text-ink-muted"
                  />
                }
                actions={
                  search.data ? (
                    <Button
                      variant="secondary"
                      size="sm"
                      loading={reindex.isPending}
                      onClick={() => reindex.mutate()}
                    >
                      <RefreshCw aria-hidden="true" />
                      重建全部索引
                    </Button>
                  ) : null
                }
              />
              <CardBody className="flex flex-col gap-3">
                {search.isLoading ? (
                  <Skeleton className="h-20 w-full" />
                ) : search.error ? (
                  <p className="text-sm text-ink-muted">
                    搜索模块可能未装配（{search.error.message}）。
                  </p>
                ) : (
                  <>
                    <DescriptionList>
                      <DescriptionTerm>内容总数</DescriptionTerm>
                      <DescriptionDetail className="tabular">
                        {count(search.data?.total)}
                      </DescriptionDetail>
                      <DescriptionTerm>已建索引</DescriptionTerm>
                      <DescriptionDetail className="tabular">
                        {count(search.data?.indexed)}
                      </DescriptionDetail>
                      <DescriptionTerm>待重建</DescriptionTerm>
                      <DescriptionDetail className="flex items-center gap-2">
                        <span className="tabular">
                          {count(search.data?.pending)}
                        </span>
                        {(search.data?.pending ?? 0) > 0 ? (
                          <StatusDot state="warn" pulse>
                            后台每 2 秒对账一批
                          </StatusDot>
                        ) : (
                          <StatusDot state="ok">已全部对账</StatusDot>
                        )}
                      </DescriptionDetail>
                    </DescriptionList>
                    <p className="text-xs text-ink-muted">
                      索引由后台按内容的更新时间自动对账，通常不需要手动重建；
                      切词规则改动后才需要。
                    </p>
                  </>
                )}
              </CardBody>
            </Card>
          ) : null}
        </div>

        <Card>
          <CardHeader title="运行形态" />
          <CardBody className="flex flex-col gap-2 text-sm text-ink-muted">
            <p className="flex items-start gap-2">
              <Server aria-hidden="true" className="mt-0.5 size-4 shrink-0" />
              Go 单一静态二进制，PostgreSQL 数据库；后台界面经 go:embed
              编译进同一份二进制。
            </p>
            <p className="flex items-start gap-2">
              <Info aria-hidden="true" className="mt-0.5 size-4 shrink-0" />
              Lumo 以 AGPL-3.0 发布，并附带插件与主题接口例外条款。
            </p>
          </CardBody>
        </Card>
      </PageBody>
    </>
  );
}
