import { api } from "@/api/client";
import { useAuth } from "@/components/auth/auth-provider";
import {
  FilterMenu,
  type FilterOption,
  ListBody,
  ListEmpty,
  ListToolbar,
} from "@/components/data/entity";
import { PageBody, PageHeader } from "@/components/layout/page-header";
import { Button } from "@/components/ui/button";
import { Card, CardHeader } from "@/components/ui/card";
import { SearchInput } from "@/components/ui/input";
import { Pagination } from "@/components/ui/pagination";
import { useDocumentTitle } from "@/lib/use-document-title";
import { useDebouncedSearch, useListParams } from "@/lib/use-list-params";
import {
  ContentBulkActions,
  ContentRow,
  type Status,
} from "@/pages/content/posts";
import { useQuery } from "@tanstack/react-query";
import { FileText, Plus, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import { Link } from "react-router";

/**
 * 独立页面列表。
 *
 * 与文章列表共用行渲染与批量逻辑（ContentRow / ContentBulkActions），
 * 但**不复用**「按分类筛选」「按标签筛选」：页面的数据形态里没有这些
 * （服务端对 page 忽略它们）。筛选条上留一个用不上的下拉，比没有这个下拉更糟 ——
 * 用户会以为筛选没生效。
 */

const STATUS_OPTIONS: FilterOption[] = [
  { value: "", label: "全部" },
  { value: "draft", label: "草稿" },
  { value: "published", label: "已发布" },
  { value: "trashed", label: "回收站" },
];

export function PagesPage() {
  useDocumentTitle("页面");
  const { can } = useAuth();
  const canWrite = can("pages:write");
  const canPublish = can("pages:publish");
  const canDeleteAny = can("pages:delete_any");
  const list = useListParams();
  const [search, setSearch] = useDebouncedSearch(list.filter("q"), (value) =>
    list.setFilter("q", value),
  );

  const status = list.filter("status") as Status | "";
  const trashedView = status === "trashed";
  const [selected, setSelected] = useState<Set<number>>(new Set());

  const query = useQuery({
    queryKey: ["pages", list.page, list.size, list.filters],
    queryFn: async () => {
      const { data, response } = await api.GET("/api/v1/console/pages", {
        params: {
          query: {
            page: list.page,
            size: list.size,
            ...(status ? { status } : {}),
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

  // 翻页或换筛选后清空选择：勾选的是上一屏的东西
  const listKey = `${list.page}|${list.size}|${JSON.stringify(list.filters)}`;
  // biome-ignore lint/correctness/useExhaustiveDependencies: listKey 是刻意的触发条件
  useEffect(() => {
    setSelected(new Set());
  }, [listKey]);

  const allSelected =
    items.length > 0 && items.every((item) => selected.has(item.id));
  const someSelected = items.some((item) => selected.has(item.id));

  function toggle(id: number, checked: boolean) {
    setSelected((prev) => {
      const next = new Set(prev);
      if (checked) {
        next.add(id);
      } else {
        next.delete(id);
      }
      return next;
    });
  }

  const refresh = () => {
    setSelected(new Set());
    void query.refetch();
  };

  return (
    <>
      <PageHeader
        icon={FileText}
        title="页面"
        actions={
          <>
            <Button variant="secondary" size="sm" asChild>
              {trashedView ? (
                <Link to="/pages">
                  <FileText aria-hidden="true" />
                  全部页面
                </Link>
              ) : (
                <Link to="/pages?status=trashed">
                  <Trash2 aria-hidden="true" />
                  回收站
                </Link>
              )}
            </Button>
            {canWrite ? (
              <Button variant="primary" size="sm" asChild>
                <Link to="/pages/new">
                  <Plus aria-hidden="true" />
                  新建
                </Link>
              </Button>
            ) : null}
          </>
        }
      />

      <PageBody>
        <Card>
          <CardHeader>
            <ListToolbar
              selectAll={{
                checked: allSelected,
                indeterminate: someSelected && !allSelected,
                onChange: (checked) =>
                  setSelected(
                    checked ? new Set(items.map((item) => item.id)) : new Set(),
                  ),
                disabled: items.length === 0,
              }}
              search={
                <SearchInput
                  value={search}
                  onValueChange={setSearch}
                  placeholder="按标题搜索"
                  aria-label="搜索页面"
                  className="max-w-xs"
                />
              }
              bulk={
                selected.size > 0 ? (
                  <ContentBulkActions
                    kind="page"
                    ids={[...selected]}
                    trashedView={trashedView}
                    canPublish={canPublish}
                    canDeleteAny={canDeleteAny}
                    onDone={refresh}
                    onClear={() => setSelected(new Set())}
                  />
                ) : undefined
              }
              filters={
                <FilterMenu
                  label="状态"
                  value={status}
                  options={STATUS_OPTIONS}
                  onChange={(value) => list.setFilter("status", value)}
                />
              }
              hasFilters={list.hasFilters}
              onClearFilters={list.reset}
              onRefresh={refresh}
              refreshing={query.isFetching}
            />
          </CardHeader>

          <ListBody
            isLoading={query.isLoading}
            error={query.error}
            onRetry={() => void query.refetch()}
            isEmpty={items.length === 0}
            thumb
            empty={
              trashedView ? (
                <ListEmpty
                  icon={Trash2}
                  title="回收站是空的"
                  description="删除的页面会先放进回收站，可以随时还原。"
                  action={
                    <Button variant="secondary" size="sm" asChild>
                      <Link to="/pages">查看全部页面</Link>
                    </Button>
                  }
                />
              ) : list.hasFilters ? (
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
                    canWrite ? (
                      <Button variant="primary" size="sm" asChild>
                        <Link to="/pages/new">新建页面</Link>
                      </Button>
                    ) : null
                  }
                />
              )
            }
          >
            {items.map((page) => (
              <ContentRow
                key={page.id}
                kind="page"
                post={page}
                checked={selected.has(page.id)}
                onToggle={(checked) => toggle(page.id, checked)}
                canPublish={canPublish}
                canDeleteAny={canDeleteAny}
                onChanged={refresh}
              />
            ))}
          </ListBody>

          <Pagination
            page={list.page}
            size={list.size}
            total={total}
            onPageChange={list.setPage}
            onSizeChange={list.setSize}
          />
        </Card>
      </PageBody>
    </>
  );
}
