import { api } from "@/api/client";
import { runMutation } from "@/api/mutation";
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
import { ConfirmDialog } from "@/components/ui/dialog";
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
import { absoluteDate, relativeTime } from "@/lib/format";
import { useDocumentTitle } from "@/lib/use-document-title";
import { useDebouncedSearch, useListParams } from "@/lib/use-list-params";
import { useMutation, useQuery } from "@tanstack/react-query";
import {
  CalendarClock,
  FileText,
  PenLine,
  Pin,
  Plus,
  RotateCcw,
  Search,
  Trash2,
  Undo2,
  X,
} from "lucide-react";
import { useState } from "react";
import { Link, useNavigate } from "react-router";

/**
 * 文章列表。
 *
 * 文章与页面在数据上是同一张表（agent.md §3.3，WordPress 模式的单表 + type 区分），
 * 但**列表界面刻意做了两处不同**：
 *
 *   - 文章有分类、标签、置顶、定时发布；页面没有（服务端也忽略这些字段）
 *   - 文章按发布时间倒序；页面按标题排（页面数量少且不随时间增长，
 *     站长找「关于我们」时是按名字找的，不是按日期）
 *
 * 两处差异来自内容形态本身，不是为了两个页面看起来不一样。
 */

type Post = components["schemas"]["Post"];
type Status = "draft" | "published" | "scheduled" | "trashed";

const STATUS_META: Record<
  Status,
  { label: string; tone: "neutral" | "ok" | "warn" | "danger" }
> = {
  draft: { label: "草稿", tone: "neutral" },
  published: { label: "已发布", tone: "ok" },
  scheduled: { label: "定时", tone: "warn" },
  trashed: { label: "回收站", tone: "danger" },
};

const STATUS_FILTERS = [
  { value: "all", label: "全部（不含回收站）" },
  { value: "draft", label: "草稿" },
  { value: "published", label: "已发布" },
  { value: "scheduled", label: "定时发布" },
  { value: "trashed", label: "回收站" },
];

export function PostsPage() {
  useDocumentTitle("文章");
  const { can } = useAuth();
  const canPublish = can("posts:publish");
  const list = useListParams();
  const [search, setSearch] = useDebouncedSearch(list.filter("q"), (value) =>
    list.setFilter("q", value),
  );

  const status = (list.filter("status") || "all") as Status | "all";

  const query = useQuery({
    queryKey: ["posts", list.page, list.size, list.filters],
    queryFn: async () => {
      const { data, response } = await api.GET("/api/v1/console/posts", {
        params: {
          query: {
            page: list.page,
            size: list.size,
            // status=all 时不传该参数：服务端对「缺省」的定义是
            // 「回收站之外的全部」，正是这里想要的语义。
            ...(status === "all" ? {} : { status }),
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

  const items = query.data?.items ?? [];
  const total = query.data?.total ?? 0;

  const columns: Column[] = [
    { label: "标题" },
    { label: "状态" },
    { label: "作者" },
    { label: "分类" },
    { label: "发布时间" },
    { label: "" },
  ];

  const filtersActive = list.hasFilters;

  return (
    <>
      <PageHeader
        title="文章"
        description="草稿、定时发布与回收站都在这里管理"
        actions={
          <Button variant="primary" asChild>
            <Link to="/posts/new">
              <Plus aria-hidden="true" />
              写文章
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
                aria-label="筛选文章"
                className="pl-8"
              />
              <InputAffix side="left">
                <Search aria-hidden="true" />
              </InputAffix>
            </ToolbarSearch>

            <Select
              value={status}
              onValueChange={(value) =>
                list.setFilter("status", value === "all" ? "" : value)
              }
            >
              <SelectTrigger className="w-44" aria-label="按状态筛选">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {STATUS_FILTERS.map((option) => (
                  <SelectItem key={option.value} value={option.value}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>

            {filtersActive ? (
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
            status === "trashed" ? (
              <ListEmpty
                title="回收站是空的"
                description="删除的文章会先放进回收站，30 天内可以还原。"
              />
            ) : filtersActive ? (
              <ListEmpty
                title="没有匹配的文章"
                description="换个关键词或状态试试。"
                action={
                  <Button variant="secondary" size="sm" onClick={list.reset}>
                    清除筛选
                  </Button>
                }
              />
            ) : (
              <ListEmpty
                title="还没有文章"
                description="写第一篇吧。Markdown 与富文本两种格式随时可以切换。"
                action={
                  <Button variant="primary" size="sm" asChild>
                    <Link to="/posts/new">写文章</Link>
                  </Button>
                }
              />
            )
          }
        >
          {items.map((post) => (
            <PostRow
              key={post.id}
              post={post}
              canPublish={canPublish}
              canDeleteAny={can("posts:delete_any")}
              onRefresh={() => void query.refetch()}
            />
          ))}
        </ListBody>
      </ListPanel>
    </>
  );
}

/**
 * 一行文章。
 *
 * 状态列承担的信息最多：状态、置顶、私密、定时时刻。
 * 它们都用带文字的标签而不是纯色块 —— 色盲用户看不到「红」与「绿」的区别，
 * 但看得到「回收站」与「已发布」（agent.md §11.3）。
 */
export function PostRow({
  post,
  canPublish,
  canDeleteAny,
  onRefresh,
  showCategories = true,
}: {
  post: Post;
  canPublish: boolean;
  canDeleteAny: boolean;
  onRefresh: () => void;
  showCategories?: boolean;
}) {
  const navigate = useNavigate();
  const [confirmTrash, setConfirmTrash] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);

  const status = post.status as Status;
  const meta = STATUS_META[status] ?? STATUS_META.draft;
  const trashed = status === "trashed";

  const trash = useMutation({
    mutationFn: () =>
      runMutation(
        () =>
          api.DELETE("/api/v1/console/posts/{id}", {
            params: { path: { id: post.id } },
          }),
        { success: "已移入回收站", invalidate: ["posts"] },
      ),
    onSuccess: onRefresh,
  });

  const restore = useMutation({
    mutationFn: () =>
      runMutation(
        () =>
          api.POST("/api/v1/console/posts/{id}/restore", {
            params: { path: { id: post.id } },
          }),
        { success: "已还原为草稿", invalidate: ["posts"] },
      ),
    onSuccess: onRefresh,
  });

  const destroy = useMutation({
    mutationFn: () =>
      runMutation(
        () =>
          api.DELETE("/api/v1/console/posts/{id}/permanent", {
            params: { path: { id: post.id } },
          }),
        { success: "已彻底删除", invalidate: ["posts"] },
      ),
    onSuccess: onRefresh,
  });

  const publish = useMutation({
    mutationFn: (unpublish: boolean) =>
      runMutation(
        () =>
          unpublish
            ? api.POST("/api/v1/console/posts/{id}/unpublish", {
                params: { path: { id: post.id } },
              })
            : api.POST("/api/v1/console/posts/{id}/publish", {
                params: { path: { id: post.id } },
                body: {},
              }),
        {
          success: unpublish ? "已撤回为草稿" : "已发布",
          invalidate: ["posts"],
        },
      ),
    onSuccess: onRefresh,
  });

  return (
    <tr className="transition-ui hover:bg-surface-hover">
      <td className="max-w-md px-4 py-2.5">
        <div className="flex min-w-0 items-center gap-2">
          <Link
            to={`/posts/${post.id}`}
            className="transition-ui min-w-0 truncate font-medium text-ink hover:text-seal"
          >
            {post.title || "（无标题）"}
          </Link>
          {post.pinned ? (
            <Badge tone="seal" title="置顶">
              <Pin aria-hidden="true" />
              置顶
            </Badge>
          ) : null}
          {post.visibility === "private" ? (
            <Badge tone="warn">私密</Badge>
          ) : null}
        </div>
        {/* slug 是给站长看地址用的，次要信息放第二行 */}
        <code className="token mt-0.5 block text-xs text-ink-subtle">
          /{post.slug}
        </code>
      </td>

      <td className="px-4 py-2.5">
        <div className="flex flex-col items-start gap-1">
          <Badge tone={meta.tone}>{meta.label}</Badge>
          {status === "scheduled" && post.publishedAt ? (
            <span className="flex items-center gap-1 text-xs text-ink-muted">
              <CalendarClock aria-hidden="true" className="size-3" />
              <time
                dateTime={post.publishedAt}
                title={absoluteDate(post.publishedAt)}
              >
                {absoluteDate(post.publishedAt)}
              </time>
            </span>
          ) : null}
        </div>
      </td>

      <td className="px-4 py-2.5 text-sm whitespace-nowrap text-ink-muted">
        {post.author?.displayName || post.author?.username || "—"}
      </td>

      <td className="max-w-40 px-4 py-2.5">
        {showCategories ? (
          post.categories && post.categories.length > 0 ? (
            <div className="flex flex-wrap gap-1">
              {post.categories.slice(0, 2).map((category) => (
                <Badge key={category.id} tone="outline">
                  {category.name}
                </Badge>
              ))}
              {post.categories.length > 2 ? (
                <span className="text-xs text-ink-subtle">
                  +{post.categories.length - 2}
                </span>
              ) : null}
            </div>
          ) : (
            <span className="text-xs text-ink-subtle">未分类</span>
          )
        ) : (
          <span className="text-xs text-ink-subtle">—</span>
        )}
      </td>

      <td className="px-4 py-2.5 text-sm whitespace-nowrap text-ink-muted">
        {post.publishedAt ? (
          <time
            dateTime={post.publishedAt}
            title={absoluteDate(post.publishedAt)}
          >
            {relativeTime(post.publishedAt)}
          </time>
        ) : (
          <span title={absoluteDate(post.updatedAt)}>
            更新于 {relativeTime(post.updatedAt)}
          </span>
        )}
      </td>

      <td className="w-px px-4 py-2.5 text-right whitespace-nowrap">
        <div className="flex items-center justify-end gap-0.5">
          {trashed ? (
            <>
              <Button
                variant="ghost"
                size="icon-sm"
                onClick={() => restore.mutate()}
                disabled={restore.isPending}
                aria-label={`还原 ${post.title}`}
                title="还原为草稿"
              >
                <Undo2 aria-hidden="true" />
              </Button>
              {canDeleteAny ? (
                <Button
                  variant="ghost"
                  size="icon-sm"
                  onClick={() => setConfirmDelete(true)}
                  aria-label={`彻底删除 ${post.title}`}
                  title="彻底删除"
                  className="hover:text-danger"
                >
                  <Trash2 aria-hidden="true" />
                </Button>
              ) : null}
            </>
          ) : (
            <>
              {canPublish ? (
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => publish.mutate(status === "published")}
                  disabled={publish.isPending}
                  title={status === "published" ? "撤回为草稿" : "立即发布"}
                >
                  {status === "published" ? (
                    <>
                      <RotateCcw aria-hidden="true" />
                      撤回
                    </>
                  ) : (
                    <>
                      <FileText aria-hidden="true" />
                      发布
                    </>
                  )}
                </Button>
              ) : null}
              <Button
                variant="ghost"
                size="icon-sm"
                onClick={() => navigate(`/posts/${post.id}`)}
                aria-label={`编辑 ${post.title}`}
                title="编辑"
              >
                <PenLine aria-hidden="true" />
              </Button>
              <Button
                variant="ghost"
                size="icon-sm"
                onClick={() => setConfirmTrash(true)}
                aria-label={`删除 ${post.title}`}
                title="移入回收站"
                className="hover:text-danger"
              >
                <Trash2 aria-hidden="true" />
              </Button>
            </>
          )}
        </div>

        <ConfirmDialog
          open={confirmTrash}
          onOpenChange={setConfirmTrash}
          title={`把「${post.title}」移入回收站？`}
          consequence={
            <p>
              文章会从前台消失。它不会被立刻删掉，你可以在回收站里还原，
              或在那里彻底删除。
            </p>
          }
          confirmLabel="移入回收站"
          pending={trash.isPending}
          onConfirm={async () => {
            await trash.mutateAsync().catch(() => {});
            setConfirmTrash(false);
          }}
        />

        <ConfirmDialog
          open={confirmDelete}
          onOpenChange={setConfirmDelete}
          title={`彻底删除「${post.title}」？`}
          consequence={
            <p>
              <strong className="font-medium text-ink">这一步无法撤销。</strong>
              文章、它的全部修订版本与评论都会一并消失。
            </p>
          }
          confirmLabel="彻底删除"
          pending={destroy.isPending}
          onConfirm={async () => {
            await destroy.mutateAsync().catch(() => {});
            setConfirmDelete(false);
          }}
        />
      </td>
    </tr>
  );
}
