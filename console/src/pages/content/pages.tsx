import { api } from "@/api/client";
import type { components } from "@/api/schema";
import { useAuth } from "@/components/auth/auth-provider";
import {
  type Column,
  ListBody,
  ListEmpty,
  ListPanel,
  ToolbarSearch,
} from "@/components/data/list-panel";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input, InputAffix } from "@/components/ui/input";
import { Pagination } from "@/components/ui/pagination";
import { PageHeader } from "@/components/ui/panel";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useDocumentTitle } from "@/lib/use-document-title";
import { useDebouncedSearch, useListParams } from "@/lib/use-list-params";
import { useQuery } from "@tanstack/react-query";
import { FileText, Plus, Search, X } from "lucide-react";
import { Link } from "react-router";

type Post = components["schemas"]["Post"];

/**
 * 独立页面列表。
 *
 * 与文章列表共用行渲染（PostRow），但**不复用**「按分类筛选」「按标签筛选」
 * 与置顶展示：页面的数据形态里没有这些（服务端对 page 忽略它们）。
 * 筛选条上留一个用不上的下拉，比没有这个下拉更糟 ——
 * 用户会以为筛选没生效。
 */

export function PagesPage() {
  useDocumentTitle("页面");
  const list = useListParams();
  const [search, setSearch] = useDebouncedSearch(list.filter("q"), (value) =>
    list.setFilter("q", value),
  );

  const query = useQuery({
    queryKey: ["pages", list.page, list.size, list.filters],
    queryFn: async () => {
      const { data, response } = await api.GET("/api/v1/console/pages", {
        params: {
          query: {
            page: list.page,
            size: list.size,
            ...(list.filter("status")
              ? {
                  status: list.filter("status") as
                    | "draft"
                    | "published"
                    | "scheduled"
                    | "trashed",
                }
              : {}),
            ...(list.filter("q") ? { q: list.filter("q") } : {}),
          },
        },
      });
      if (!response.ok) {
        throw new Error(`载入页面失败（HTTP ${response.status}）`);
      }
      return data;
    },
  });

  const items = query.data?.items ?? [];
  const total = query.data?.total ?? 0;
  const status = list.filter("status");

  const columns: Column[] = [
    { label: "标题" },
    { label: "状态" },
    { label: "作者" },
    { label: "模板" },
    { label: "更新时间" },
    { label: "" },
  ];

  return (
    <>
      <PageHeader
        title="页面"
        description="关于、联系这类不随时间增长的独立页面；地址是根路径 /<slug>"
        actions={
          <Button variant="primary" asChild>
            <Link to="/pages/new">
              <Plus aria-hidden="true" />
              新建页面
            </Link>
          </Button>
        }
      />

      <ListPanel
        toolbar={
          <>
            <ToolbarSearch>
              <Input
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder="按标题筛选"
                aria-label="筛选页面"
                className="pl-8"
              />
              <InputAffix side="left">
                <Search aria-hidden="true" />
              </InputAffix>
            </ToolbarSearch>

            <Select
              value={status || "all"}
              onValueChange={(value) =>
                list.setFilter("status", value === "all" ? "" : value)
              }
            >
              <SelectTrigger className="w-44" aria-label="按状态筛选">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">全部（不含回收站）</SelectItem>
                <SelectItem value="draft">草稿</SelectItem>
                <SelectItem value="published">已发布</SelectItem>
                <SelectItem value="trashed">回收站</SelectItem>
              </SelectContent>
            </Select>

            {list.hasFilters ? (
              <Button variant="ghost" size="sm" onClick={list.reset}>
                <X aria-hidden="true" />
                清除筛选
              </Button>
            ) : null}
          </>
        }
        footer={
          <Pagination
            page={list.page}
            size={list.size}
            total={total}
            onPageChange={list.setPage}
            onSizeChange={list.setSize}
          />
        }
      >
        <ListBody
          columns={columns}
          isLoading={query.isLoading}
          error={query.error}
          onRetry={() => void query.refetch()}
          isEmpty={items.length === 0}
          empty={
            list.hasFilters ? (
              <ListEmpty
                title="没有匹配的页面"
                description="换个关键词或状态试试。"
                action={
                  <Button variant="secondary" size="sm" onClick={list.reset}>
                    清除筛选
                  </Button>
                }
              />
            ) : (
              <ListEmpty
                icon={FileText}
                title="还没有独立页面"
                description="「关于」「联系」这类内容适合做成页面：它不出现在文章流里，地址也更短。"
                action={
                  <Button variant="primary" size="sm" asChild>
                    <Link to="/pages/new">新建页面</Link>
                  </Button>
                }
              />
            )
          }
        >
          {items.map((page) => (
            <PageRow key={page.id} page={page} />
          ))}
        </ListBody>
      </ListPanel>
    </>
  );
}

/**
 * 页面行。
 *
 * 与文章的差别只有「模板」一列：页面可以指定主题提供的 `page-*.html`，
 * 这决定了它长什么样，是页面独有的属性。故这里不复用 PostRow 的渲染，
 * 而在同一套视觉规则下手写一行。
 */
function PageRow({ page }: { page: Post }) {
  const { can } = useAuth();

  return (
    <tr className="transition-ui hover:bg-surface-hover">
      <td className="max-w-md px-4 py-2.5">
        <Link
          to={`/pages/${page.id}`}
          className="transition-ui block truncate font-medium text-ink hover:text-seal"
        >
          {page.title || "（无标题）"}
        </Link>
        <code className="token mt-0.5 block text-xs text-ink-subtle">
          /{page.slug}
        </code>
      </td>
      <td className="px-4 py-2.5">
        <PageStatusBadge status={page.status} />
      </td>
      <td className="px-4 py-2.5 text-sm whitespace-nowrap text-ink-muted">
        {page.author?.displayName || page.author?.username || "—"}
      </td>
      <td className="px-4 py-2.5">
        {page.template ? (
          <code className="text-xs text-ink-muted">{page.template}.html</code>
        ) : (
          <span className="text-xs text-ink-subtle">page.html</span>
        )}
      </td>
      <td className="px-4 py-2.5 text-sm whitespace-nowrap text-ink-muted">
        {page.publishedAt ? (
          <time dateTime={page.publishedAt}>
            {new Date(page.publishedAt).toLocaleDateString("zh-CN")}
          </time>
        ) : (
          <time dateTime={page.updatedAt}>
            更新于 {new Date(page.updatedAt).toLocaleDateString("zh-CN")}
          </time>
        )}
      </td>
      <td className="w-px px-4 py-2.5 text-right whitespace-nowrap">
        <Button
          variant="ghost"
          size="sm"
          asChild
          disabled={!can("pages:write")}
        >
          <Link to={`/pages/${page.id}`}>编辑</Link>
        </Button>
      </td>
    </tr>
  );
}

function PageStatusBadge({ status }: { status: string }) {
  const map: Record<
    string,
    { label: string; tone: "neutral" | "ok" | "warn" | "danger" }
  > = {
    draft: { label: "草稿", tone: "neutral" },
    published: { label: "已发布", tone: "ok" },
    scheduled: { label: "定时", tone: "warn" },
    trashed: { label: "回收站", tone: "danger" },
  };
  // 服务端未来新增状态时退回「草稿」而不是渲染空白：
  // 一个没有状态标签的行会让人以为数据没加载出来。
  const meta = map[status] ?? { label: status, tone: "neutral" as const };
  return <Badge tone={meta.tone}>{meta.label}</Badge>;
}
