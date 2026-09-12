import { api } from "@/api/client";
import { runMutation } from "@/api/mutation";
import type { components } from "@/api/schema";
import { useAuth } from "@/components/auth/auth-provider";
import {
  Entity,
  EntityActions,
  EntityCheckbox,
  EntityEnd,
  EntityField,
  EntityMeta,
  EntityStart,
  EntityThumb,
  FilterMenu,
  type FilterOption,
  ListBody,
  ListEmpty,
  ListToolbar,
} from "@/components/data/entity";
import { PageBody, PageHeader } from "@/components/layout/page-header";
import { Avatar } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardHeader } from "@/components/ui/card";
import { ConfirmDialog } from "@/components/ui/dialog";
import {
  DropdownMenuItem,
  DropdownMenuSeparator,
} from "@/components/ui/dropdown-menu";
import { SearchInput } from "@/components/ui/input";
import { Pagination } from "@/components/ui/pagination";
import { StatusDot } from "@/components/ui/status-dot";
import { absoluteDate, relativeTime } from "@/lib/format";
import { useDocumentTitle } from "@/lib/use-document-title";
import { useDebouncedSearch, useListParams } from "@/lib/use-list-params";
import { useMutation, useQuery } from "@tanstack/react-query";
import {
  BookOpen,
  CalendarClock,
  Eye,
  EyeOff,
  FolderTree,
  Lock,
  PenLine,
  Pin,
  Plus,
  RotateCcw,
  Send,
  Tags,
  Trash2,
  Undo2,
} from "lucide-react";
import { useEffect, useState } from "react";
import { Link } from "react-router";
import { toast } from "sonner";

/**
 * 文章列表（形态对齐 Halo 的 PostList）。
 *
 * 一张卡片：标题栏是工具条（全选、搜索、文字式筛选），主体是实体行，底部是分页。
 * 勾选任意行后，搜索框原位换成批量按钮组。
 *
 * 文章与页面在数据上是同一张表（agent.md §3.3），但列表界面刻意做了两处不同：
 *   - 文章有分类、标签、置顶、定时发布；页面没有（服务端也忽略这些字段）
 *   - 文章按发布时间倒序；页面按标题排
 * 两处差异来自内容形态本身。行渲染与批量逻辑由本文件导出，页面列表复用。
 */

export type Post = components["schemas"]["Post"];
export type ContentKind = "post" | "page";
export type Status = "draft" | "published" | "scheduled" | "trashed";

export const STATUS_META: Record<
  Status,
  { label: string; state: "neutral" | "ok" | "warn" | "danger" }
> = {
  draft: { label: "草稿", state: "neutral" },
  published: { label: "已发布", state: "ok" },
  scheduled: { label: "定时发布", state: "warn" },
  trashed: { label: "回收站", state: "danger" },
};

const STATUS_OPTIONS: FilterOption[] = [
  { value: "", label: "全部" },
  { value: "draft", label: "草稿" },
  { value: "published", label: "已发布" },
  { value: "scheduled", label: "定时发布" },
  { value: "trashed", label: "回收站" },
];

/** 把分类树摊平成带缩进的筛选项，值是 slug（接口的 category 参数按 slug 匹配）。 */
function flattenCategories(
  nodes: components["schemas"]["CategoryNode"][],
  depth = 0,
): FilterOption[] {
  const out: FilterOption[] = [];
  for (const node of nodes) {
    out.push({ value: node.slug, label: `${"　".repeat(depth)}${node.name}` });
    if (node.children?.length) {
      out.push(...flattenCategories(node.children, depth + 1));
    }
  }
  return out;
}

export type BulkAction =
  | "publish"
  | "unpublish"
  | "trash"
  | "restore"
  | "destroy";

const BULK_LABEL: Record<BulkAction, string> = {
  publish: "发布",
  unpublish: "撤回",
  trash: "移入回收站",
  restore: "还原",
  destroy: "彻底删除",
};

/** 对单条内容执行一个动作。文章与页面的接口路径不同，按 kind 分发。 */
async function runContentAction(
  kind: ContentKind,
  action: BulkAction,
  id: number,
): Promise<Response> {
  const path = { params: { path: { id } } };
  if (kind === "post") {
    switch (action) {
      case "publish":
        return (
          await api.POST("/api/v1/console/posts/{id}/publish", {
            ...path,
            body: {},
          })
        ).response;
      case "unpublish":
        return (await api.POST("/api/v1/console/posts/{id}/unpublish", path))
          .response;
      case "trash":
        return (await api.DELETE("/api/v1/console/posts/{id}", path)).response;
      case "restore":
        return (await api.POST("/api/v1/console/posts/{id}/restore", path))
          .response;
      case "destroy":
        return (await api.DELETE("/api/v1/console/posts/{id}/permanent", path))
          .response;
    }
  }
  switch (action) {
    case "publish":
      return (
        await api.POST("/api/v1/console/pages/{id}/publish", {
          ...path,
          body: {},
        })
      ).response;
    case "unpublish":
      return (await api.POST("/api/v1/console/pages/{id}/unpublish", path))
        .response;
    case "trash":
      return (await api.DELETE("/api/v1/console/pages/{id}", path)).response;
    case "restore":
      return (await api.POST("/api/v1/console/pages/{id}/restore", path))
        .response;
    case "destroy":
      return (await api.DELETE("/api/v1/console/pages/{id}/permanent", path))
        .response;
  }
}

/**
 * 批量处理：逐条串行而不是并发。
 *
 * 发布会写库并可能触发订阅源与索引更新，一次几十个并发请求既压服务端，
 * 也让「部分成功」的中间状态难以解释。逐条之间不中断 —— 一条失败不该让剩下的都不处理。
 */
export async function runContentBulk(
  kind: ContentKind,
  action: BulkAction,
  ids: number[],
): Promise<{ ok: number; failed: number }> {
  let ok = 0;
  let failed = 0;
  for (const id of ids) {
    try {
      const response = await runContentAction(kind, action, id);
      if (response.ok) {
        ok += 1;
      } else {
        failed += 1;
      }
    } catch {
      failed += 1;
    }
  }
  return { ok, failed };
}

/** 批量结果按「N 成功 M 失败」如实报出。 */
export function reportBulk(
  action: BulkAction,
  result: { ok: number; failed: number },
) {
  const label = BULK_LABEL[action];
  if (result.failed === 0) {
    toast.success(`已${label} ${result.ok} 条`);
  } else {
    toast.warning(`${label}：${result.ok} 条成功，${result.failed} 条失败`);
  }
}

/**
 * 批量按钮组。非回收站视图给「发布 / 撤回 / 移入回收站」，回收站视图给「还原 / 彻底删除」。
 * 两个破坏性动作先经确认框 —— 批量删几十篇文章不该只是一次点击。
 */
export function ContentBulkActions({
  kind,
  ids,
  trashedView,
  canPublish,
  canDeleteAny,
  onDone,
  onClear,
}: {
  kind: ContentKind;
  ids: number[];
  trashedView: boolean;
  canPublish: boolean;
  canDeleteAny: boolean;
  onDone: () => void;
  onClear: () => void;
}) {
  const [confirm, setConfirm] = useState<"trash" | "destroy" | null>(null);
  const noun = kind === "post" ? "文章" : "页面";

  const bulk = useMutation({
    mutationFn: (action: BulkAction) => runContentBulk(kind, action, ids),
    onSuccess: (result, action) => {
      reportBulk(action, result);
      onDone();
      onClear();
    },
  });

  return (
    <>
      <span className="text-sm font-medium text-ink">已选 {ids.length} 条</span>
      {trashedView ? (
        <>
          <Button
            variant="secondary"
            size="sm"
            loading={bulk.isPending && bulk.variables === "restore"}
            disabled={bulk.isPending}
            onClick={() => bulk.mutate("restore")}
          >
            <Undo2 aria-hidden="true" />
            还原
          </Button>
          {canDeleteAny ? (
            <Button
              variant="danger"
              size="sm"
              loading={bulk.isPending && bulk.variables === "destroy"}
              disabled={bulk.isPending}
              onClick={() => setConfirm("destroy")}
            >
              <Trash2 aria-hidden="true" />
              彻底删除
            </Button>
          ) : null}
        </>
      ) : (
        <>
          {canPublish ? (
            <>
              <Button
                variant="secondary"
                size="sm"
                loading={bulk.isPending && bulk.variables === "publish"}
                disabled={bulk.isPending}
                onClick={() => bulk.mutate("publish")}
              >
                <Send aria-hidden="true" />
                发布
              </Button>
              <Button
                variant="secondary"
                size="sm"
                loading={bulk.isPending && bulk.variables === "unpublish"}
                disabled={bulk.isPending}
                onClick={() => bulk.mutate("unpublish")}
              >
                <RotateCcw aria-hidden="true" />
                撤回
              </Button>
            </>
          ) : null}
          <Button
            variant="danger"
            size="sm"
            loading={bulk.isPending && bulk.variables === "trash"}
            disabled={bulk.isPending}
            onClick={() => setConfirm("trash")}
          >
            <Trash2 aria-hidden="true" />
            移入回收站
          </Button>
        </>
      )}
      <Button
        variant="ghost"
        size="sm"
        onClick={onClear}
        disabled={bulk.isPending}
      >
        取消选择
      </Button>

      <ConfirmDialog
        open={confirm === "trash"}
        onOpenChange={(open) => !open && setConfirm(null)}
        title={`把选中的 ${ids.length} 篇${noun}移入回收站？`}
        consequence={
          <p>
            它们会从前台消失，但不会被立刻删掉。你可以在回收站里逐篇还原，
            或在那里彻底删除。
          </p>
        }
        confirmLabel="移入回收站"
        pending={bulk.isPending}
        onConfirm={async () => {
          await bulk.mutateAsync("trash").catch(() => {});
          setConfirm(null);
        }}
      />
      <ConfirmDialog
        open={confirm === "destroy"}
        onOpenChange={(open) => !open && setConfirm(null)}
        title={`彻底删除选中的 ${ids.length} 篇${noun}？`}
        consequence={
          <p>
            <strong className="font-medium text-ink">这一步无法撤销。</strong>
            它们的全部修订版本与评论都会一并消失。
          </p>
        }
        confirmLabel="彻底删除"
        pending={bulk.isPending}
        onConfirm={async () => {
          await bulk.mutateAsync("destroy").catch(() => {});
          setConfirm(null);
        }}
      />
    </>
  );
}

export function PostsPage() {
  useDocumentTitle("文章");
  const { can } = useAuth();
  const canWrite = can("posts:write");
  const canPublish = can("posts:publish");
  const canDeleteAny = can("posts:delete_any");
  const list = useListParams();
  const [search, setSearch] = useDebouncedSearch(list.filter("q"), (value) =>
    list.setFilter("q", value),
  );

  const status = list.filter("status") as Status | "";
  const trashedView = status === "trashed";
  const [selected, setSelected] = useState<Set<number>>(new Set());

  const query = useQuery({
    queryKey: ["posts", list.page, list.size, list.filters],
    queryFn: async () => {
      const { data, response } = await api.GET("/api/v1/console/posts", {
        params: {
          query: {
            page: list.page,
            size: list.size,
            // 不传 status 时服务端的定义是「回收站之外的全部」，正是「全部」想要的语义
            ...(status ? { status } : {}),
            ...(list.filter("q") ? { q: list.filter("q") } : {}),
            ...(list.filter("category")
              ? { category: list.filter("category") }
              : {}),
            ...(list.filter("tag") ? { tag: list.filter("tag") } : {}),
          },
        },
      });
      if (!response.ok) {
        throw new Error(`载入文章失败（HTTP ${response.status}）`);
      }
      return data;
    },
  });

  /** 筛选项候选：分类树与标签。失败不影响列表本身，只是少两个筛选项。 */
  const taxonomy = useQuery({
    queryKey: ["taxonomy-options"],
    staleTime: 60_000,
    queryFn: async () => {
      const [categories, tags] = await Promise.all([
        api.GET("/api/v1/console/categories/tree"),
        api.GET("/api/v1/console/tags", { params: { query: { size: 100 } } }),
      ]);
      return {
        categories: flattenCategories(categories.data?.items ?? []),
        tags: (tags.data?.items ?? []).map((tag) => ({
          value: tag.slug,
          label: tag.name,
        })),
      };
    },
  });

  const items = query.data?.items ?? [];
  const total = query.data?.total ?? 0;

  // 翻页或换筛选后清空选择：勾选的是上一屏的东西，留着会让批量动作打到看不见的行上
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
        icon={BookOpen}
        title="文章"
        actions={
          <>
            <Button variant="secondary" size="sm" asChild>
              <Link to="/categories">
                <FolderTree aria-hidden="true" />
                分类
              </Link>
            </Button>
            <Button variant="secondary" size="sm" asChild>
              <Link to="/tags">
                <Tags aria-hidden="true" />
                标签
              </Link>
            </Button>
            <Button variant="secondary" size="sm" asChild>
              {trashedView ? (
                <Link to="/posts">
                  <BookOpen aria-hidden="true" />
                  全部文章
                </Link>
              ) : (
                <Link to="/posts?status=trashed">
                  <Trash2 aria-hidden="true" />
                  回收站
                </Link>
              )}
            </Button>
            {canWrite ? (
              <Button variant="primary" size="sm" asChild>
                <Link to="/posts/new">
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
                  aria-label="搜索文章"
                  className="max-w-xs"
                />
              }
              bulk={
                selected.size > 0 ? (
                  <ContentBulkActions
                    kind="post"
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
                <>
                  <FilterMenu
                    label="状态"
                    value={status}
                    options={STATUS_OPTIONS}
                    onChange={(value) => list.setFilter("status", value)}
                  />
                  <FilterMenu
                    label="分类"
                    value={list.filter("category")}
                    options={[
                      { value: "", label: "全部" },
                      ...(taxonomy.data?.categories ?? []),
                    ]}
                    onChange={(value) => list.setFilter("category", value)}
                  />
                  <FilterMenu
                    label="标签"
                    value={list.filter("tag")}
                    options={[
                      { value: "", label: "全部" },
                      ...(taxonomy.data?.tags ?? []),
                    ]}
                    onChange={(value) => list.setFilter("tag", value)}
                  />
                </>
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
                  description="删除的文章会先放进回收站，可以随时还原。"
                  action={
                    <Button variant="secondary" size="sm" asChild>
                      <Link to="/posts">查看全部文章</Link>
                    </Button>
                  }
                />
              ) : list.hasFilters ? (
                <ListEmpty
                  title="没有匹配的文章"
                  description="换个关键词或筛选条件试试。"
                  action={
                    <Button variant="secondary" size="sm" onClick={list.reset}>
                      清除筛选
                    </Button>
                  }
                />
              ) : (
                <ListEmpty
                  icon={BookOpen}
                  title="还没有文章"
                  description="写第一篇吧。Markdown 与富文本两种格式随时可以切换。"
                  action={
                    canWrite ? (
                      <Button variant="primary" size="sm" asChild>
                        <Link to="/posts/new">写文章</Link>
                      </Button>
                    ) : null
                  }
                />
              )
            }
          >
            {items.map((post) => (
              <ContentRow
                key={post.id}
                kind="post"
                post={post}
                checked={selected.has(post.id)}
                onToggle={(checked) => toggle(post.id, checked)}
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

/**
 * 一行内容（文章与页面共用）。
 *
 * 开始段：勾选、封面、标题（带置顶与私密标记）、描述（文章是分类，页面是模板）与 slug。
 * 结束段：状态点、作者、时间、动作菜单。
 * 状态一律圆点 + 文字 —— 色盲用户看不到「红」与「绿」的区别，但看得到「回收站」与「已发布」。
 */
export function ContentRow({
  kind,
  post,
  checked,
  onToggle,
  canPublish,
  canDeleteAny,
  onChanged,
}: {
  kind: ContentKind;
  post: Post;
  checked: boolean;
  onToggle: (checked: boolean) => void;
  canPublish: boolean;
  canDeleteAny: boolean;
  onChanged: () => void;
}) {
  const [confirmTrash, setConfirmTrash] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);

  const status = post.status as Status;
  const meta = STATUS_META[status] ?? STATUS_META.draft;
  const trashed = status === "trashed";
  const noun = kind === "post" ? "文章" : "页面";
  const editPath = kind === "post" ? `/posts/${post.id}` : `/pages/${post.id}`;
  const previewPath = kind === "post" ? `/posts/${post.slug}` : `/${post.slug}`;
  const invalidate = kind === "post" ? ["posts"] : ["pages"];

  const act = useMutation({
    mutationFn: (action: BulkAction) =>
      runMutation(
        async () => {
          const response = await runContentAction(kind, action, post.id);
          return { response, data: undefined, error: undefined };
        },
        {
          success: {
            publish: "已发布",
            unpublish: "已撤回为草稿",
            trash: "已移入回收站",
            restore: "已还原为草稿",
            destroy: "已彻底删除",
          }[action],
          invalidate,
        },
      ),
    onSuccess: onChanged,
  });

  const title = post.title || "（无标题）";
  const authorName = post.author?.displayName || post.author?.username || "";

  return (
    <Entity selected={checked}>
      <EntityStart>
        <EntityCheckbox
          checked={checked}
          onCheckedChange={onToggle}
          label={`选择 ${title}`}
        />
        <EntityThumb
          src={post.coverUrl}
          icon={BookOpen}
          className="hidden sm:flex"
        />
        <EntityField
          width="max-w-lg"
          title={title}
          to={editPath}
          extra={
            <>
              {post.pinned ? (
                <Badge tone="seal" title="置顶">
                  <Pin aria-hidden="true" />
                  置顶
                </Badge>
              ) : null}
              {post.visibility === "private" ? (
                <Badge tone="warn" title="仅作者与编辑可见">
                  <Lock aria-hidden="true" />
                  私密
                </Badge>
              ) : null}
            </>
          }
          description={
            <>
              {kind === "post" ? (
                <span>
                  {post.categories && post.categories.length > 0
                    ? post.categories
                        .map((category) => category.name)
                        .join("，")
                    : "未分类"}
                </span>
              ) : (
                <span>
                  模板{" "}
                  <code className="token">
                    {post.template ? `${post.template}.html` : "page.html"}
                  </code>
                </span>
              )}
              <code className="token">{previewPath}</code>
            </>
          }
        />
      </EntityStart>

      <EntityEnd>
        <StatusDot state={meta.state} pulse={status === "scheduled"}>
          {meta.label}
          {status === "scheduled" && post.publishedAt ? (
            <time
              dateTime={post.publishedAt}
              title={absoluteDate(post.publishedAt)}
              className="hidden md:inline"
            >
              {absoluteDate(post.publishedAt)}
            </time>
          ) : null}
        </StatusDot>

        {authorName ? (
          <EntityMeta hideOnMobile>
            <Avatar size="xs" src={post.author?.avatarUrl} name={authorName} />
            <span className="max-w-24 truncate">{authorName}</span>
          </EntityMeta>
        ) : null}

        <EntityMeta>
          {post.publishedAt && status !== "scheduled" ? (
            <time
              dateTime={post.publishedAt}
              title={`发布于 ${absoluteDate(post.publishedAt)}`}
            >
              {relativeTime(post.publishedAt)}
            </time>
          ) : (
            <time
              dateTime={post.updatedAt}
              title={`更新于 ${absoluteDate(post.updatedAt)}`}
            >
              {relativeTime(post.updatedAt)}
            </time>
          )}
        </EntityMeta>

        <EntityActions label={`${title} 的操作`}>
          {trashed ? (
            <>
              <DropdownMenuItem
                disabled={act.isPending}
                onSelect={() => act.mutate("restore")}
              >
                <Undo2 aria-hidden="true" />
                还原为草稿
              </DropdownMenuItem>
              {canDeleteAny ? (
                <>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem
                    danger
                    onSelect={() => setConfirmDelete(true)}
                  >
                    <Trash2 aria-hidden="true" />
                    彻底删除
                  </DropdownMenuItem>
                </>
              ) : null}
            </>
          ) : (
            <>
              <DropdownMenuItem asChild>
                <Link to={editPath}>
                  <PenLine aria-hidden="true" />
                  编辑
                </Link>
              </DropdownMenuItem>
              {status === "published" ? (
                <DropdownMenuItem asChild>
                  <a
                    href={previewPath}
                    target="_blank"
                    rel="noopener noreferrer"
                  >
                    <Eye aria-hidden="true" />
                    查看
                  </a>
                </DropdownMenuItem>
              ) : null}
              {canPublish ? (
                status === "published" ? (
                  <DropdownMenuItem
                    disabled={act.isPending}
                    onSelect={() => act.mutate("unpublish")}
                  >
                    <EyeOff aria-hidden="true" />
                    撤回为草稿
                  </DropdownMenuItem>
                ) : (
                  <DropdownMenuItem
                    disabled={act.isPending}
                    onSelect={() => act.mutate("publish")}
                  >
                    {status === "scheduled" ? (
                      <CalendarClock aria-hidden="true" />
                    ) : (
                      <Send aria-hidden="true" />
                    )}
                    {status === "scheduled" ? "立即发布" : "发布"}
                  </DropdownMenuItem>
                )
              ) : null}
              <DropdownMenuSeparator />
              <DropdownMenuItem danger onSelect={() => setConfirmTrash(true)}>
                <Trash2 aria-hidden="true" />
                移入回收站
              </DropdownMenuItem>
            </>
          )}
        </EntityActions>
      </EntityEnd>

      <ConfirmDialog
        open={confirmTrash}
        onOpenChange={setConfirmTrash}
        title={`把「${title}」移入回收站？`}
        consequence={
          <p>
            {noun}会从前台消失。它不会被立刻删掉，你可以在回收站里还原，
            或在那里彻底删除。
          </p>
        }
        confirmLabel="移入回收站"
        pending={act.isPending}
        onConfirm={async () => {
          await act.mutateAsync("trash").catch(() => {});
          setConfirmTrash(false);
        }}
      />

      <ConfirmDialog
        open={confirmDelete}
        onOpenChange={setConfirmDelete}
        title={`彻底删除「${title}」？`}
        consequence={
          <p>
            <strong className="font-medium text-ink">这一步无法撤销。</strong>
            {noun}、它的全部修订版本与评论都会一并消失。
          </p>
        }
        confirmLabel="彻底删除"
        pending={act.isPending}
        onConfirm={async () => {
          await act.mutateAsync("destroy").catch(() => {});
          setConfirmDelete(false);
        }}
      />
    </Entity>
  );
}
