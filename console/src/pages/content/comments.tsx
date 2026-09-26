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
  FilterMenu,
  type FilterOption,
  ListBody,
  ListEmpty,
  ListToolbar,
} from "@/components/data/entity";
import { PageBody, PageHeader } from "@/components/layout/page-header";
import { Avatar } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import { Card, CardHeader, Inset } from "@/components/ui/card";
import {
  ConfirmDialog,
  Dialog,
  DialogBody,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  DropdownMenuItem,
  DropdownMenuSeparator,
} from "@/components/ui/dropdown-menu";
import { Field, FieldError, FieldLabel } from "@/components/ui/field";
import { SearchInput, Textarea } from "@/components/ui/input";
import { Pagination } from "@/components/ui/pagination";
import { StatusDot } from "@/components/ui/status-dot";
import { absoluteDate, relativeTime } from "@/lib/format";
import { useDocumentTitle } from "@/lib/use-document-title";
import { useDebouncedSearch, useListParams } from "@/lib/use-list-params";
import { useMutation, useQuery } from "@tanstack/react-query";
import {
  Check,
  CheckCircle2,
  CornerDownRight,
  MessageSquare,
  Reply,
  ShieldAlert,
  Trash2,
} from "lucide-react";
import { useEffect, useState } from "react";
import { Link } from "react-router";
import { toast } from "sonner";

/**
 * 评论审核。
 *
 * 这是后台**使用频率最高**的页面 —— 站长每天都要来这里过一遍待审。
 * 因此它的设计重点不是功能全，而是「一眼看清 + 一次点击处理完」：
 *
 *   - 默认筛选就是「待审」而不是「全部」：进来就看见要处理的。
 *   - 一行就是「谁 评论了 哪篇」+ 正文，通过 / 回复 / 标垃圾 / 删除收在行末菜单里。
 *   - 勾选后搜索框原位换成批量按钮组，处理完即消失。
 *
 * 正文用纯文本渲染而不是 contentHtml：访客提交的内容在服务端已转义，
 * 但后台没有理由把访客控制的 HTML 塞进自己的页面 ——
 * 这里要的是「看清他写了什么」，不是「还原他想要的样式」。
 */

type Comment = components["schemas"]["Comment"];
type Status = "pending" | "approved" | "spam";
type View = Status | "all";

const STATUS_META: Record<
  Status,
  { label: string; state: "warn" | "ok" | "danger" }
> = {
  pending: { label: "待审", state: "warn" },
  approved: { label: "已通过", state: "ok" },
  spam: { label: "垃圾", state: "danger" },
};

const STATUS_OPTIONS: FilterOption[] = [
  { value: "pending", label: "待审" },
  { value: "approved", label: "已通过" },
  { value: "spam", label: "垃圾" },
  { value: "all", label: "全部" },
];

export function CommentsPage() {
  useDocumentTitle("评论");
  const { can } = useAuth();
  const canManageAny = can("comments:manage_any");
  const list = useListParams();

  // 默认进「待审」：这是这个页面存在的理由。地址里不带 status 就是待审
  const view = (list.filter("status") || "pending") as View;

  const [search, setSearch] = useDebouncedSearch(list.filter("q"), (value) =>
    list.setFilter("q", value),
  );
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [replying, setReplying] = useState<Comment | null>(null);

  const query = useQuery({
    queryKey: ["comments", list.page, list.size, list.filters, view],
    queryFn: async () => {
      const { data, response } = await api.GET("/api/v1/console/comments", {
        params: {
          query: {
            page: list.page,
            size: list.size,
            ...(view === "all" ? {} : { status: view }),
            ...(list.filter("q") ? { q: list.filter("q") } : {}),
            ...(list.filter("postId")
              ? { postId: Number(list.filter("postId")) }
              : {}),
          },
        },
      });
      if (!response.ok) {
        throw new Error(`载入评论失败（HTTP ${response.status}）`);
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

  const refresh = () => {
    setSelected(new Set());
    void query.refetch();
  };

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

  const allSelected =
    items.length > 0 && items.every((item) => selected.has(item.id));
  const someSelected = items.some((item) => selected.has(item.id));

  return (
    <>
      <PageHeader
        icon={MessageSquare}
        title="评论"
        description={canManageAny ? undefined : "你只能看到自己内容下的评论"}
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
                  placeholder="按正文或评论者搜索"
                  aria-label="搜索评论"
                  className="max-w-xs"
                />
              }
              bulk={
                selected.size > 0 ? (
                  <BulkActions
                    ids={[...selected]}
                    onDone={refresh}
                    onClear={() => setSelected(new Set())}
                  />
                ) : undefined
              }
              filters={
                <FilterMenu
                  label="状态"
                  value={view}
                  options={STATUS_OPTIONS}
                  onChange={(value) =>
                    // 待审是缺省值，不写进地址：/comments 与 /comments?status=pending 是两个地址
                    list.setFilter("status", value === "pending" ? "" : value)
                  }
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
            empty={
              view === "pending" && !list.hasFilters ? (
                <ListEmpty
                  icon={CheckCircle2}
                  title="没有待审评论"
                  description="访客新提交的评论会出现在这里。现在没有积压。"
                  action={
                    <Button
                      variant="secondary"
                      size="sm"
                      onClick={() => list.setFilter("status", "all")}
                    >
                      查看全部评论
                    </Button>
                  }
                />
              ) : (
                <ListEmpty
                  title="没有匹配的评论"
                  description="换个状态或关键词试试。"
                  action={
                    <Button variant="secondary" size="sm" onClick={list.reset}>
                      重置筛选
                    </Button>
                  }
                />
              )
            }
          >
            {items.map((comment) => (
              <CommentRow
                key={comment.id}
                comment={comment}
                checked={selected.has(comment.id)}
                onToggle={(checked) => toggle(comment.id, checked)}
                onReply={() => setReplying(comment)}
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

      <ReplyDialog
        comment={replying}
        onClose={() => setReplying(null)}
        onDone={refresh}
      />
    </>
  );
}

/**
 * 一行评论：「评论者 评论了 文章」+ 正文 + 邮箱。
 *
 * 每行自持动作（与文章页同一形态）：把 mutation 提到页面级会让「哪一行在转」
 * 变成一个需要额外状态去追踪的问题。
 */
function CommentRow({
  comment,
  checked,
  onToggle,
  onReply,
  onChanged,
}: {
  comment: Comment;
  checked: boolean;
  onToggle: (checked: boolean) => void;
  onReply: () => void;
  onChanged: () => void;
}) {
  const [confirmDelete, setConfirmDelete] = useState(false);
  const author = comment.authorName || "匿名";

  const approve = useMutation({
    mutationFn: () =>
      runMutation(
        () =>
          api.POST("/api/v1/console/comments/{id}/approve", {
            params: { path: { id: comment.id } },
          }),
        { success: "已通过", invalidate: ["comments"] },
      ),
    onSuccess: onChanged,
  });

  const markSpam = useMutation({
    mutationFn: () =>
      runMutation(
        () =>
          api.POST("/api/v1/console/comments/{id}/spam", {
            params: { path: { id: comment.id } },
          }),
        { success: "已标为垃圾", invalidate: ["comments"] },
      ),
    onSuccess: onChanged,
  });

  const destroy = useMutation({
    mutationFn: () =>
      runMutation(
        () =>
          api.DELETE("/api/v1/console/comments/{id}", {
            params: { path: { id: comment.id } },
          }),
        { success: "评论已删除", invalidate: ["comments"] },
      ),
    onSuccess: onChanged,
  });

  const meta = STATUS_META[comment.status as Status];
  const busy = approve.isPending || markSpam.isPending || destroy.isPending;

  return (
    <Entity selected={checked} align="start">
      <EntityStart className="items-start">
        <EntityCheckbox
          checked={checked}
          onCheckedChange={onToggle}
          label={`选择 ${author} 的评论`}
          className="mt-2"
        />
        <Avatar name={author} size="sm" className="mt-0.5" />
        <EntityField
          width="max-w-2xl"
          title={
            <>
              <span>{author}</span>
              <span className="font-normal text-ink-muted"> 评论了 </span>
              {comment.post ? (
                <Link
                  to={`/posts/${comment.post.id}`}
                  className="transition-ui font-normal text-seal hover:underline"
                >
                  {comment.post.title}
                </Link>
              ) : (
                <span className="font-normal text-ink-subtle">
                  已删除的内容
                </span>
              )}
            </>
          }
        >
          {/* 纯文本渲染：要看清他写了什么，不还原他想要的样式 */}
          <p className="line-clamp-2 text-sm whitespace-pre-wrap text-ink">
            {comment.content}
          </p>
          {/* 邮箱与站外链接只在 Console 平面出现——站长排查需要它们 */}
          <div className="flex flex-wrap items-center gap-x-3 gap-y-0.5 text-xs text-ink-muted">
            <span className="token">{comment.authorEmail || "无邮箱"}</span>
            {comment.authorUrl ? (
              <a
                href={comment.authorUrl}
                target="_blank"
                // noopener 必须带上：不带时新页面能通过 window.opener 操作本站
                rel="noopener noreferrer nofollow"
                className="token transition-ui max-w-48 truncate hover:text-seal"
              >
                {comment.authorUrl}
              </a>
            ) : null}
            {comment.parentId !== null ? (
              <span className="inline-flex items-center gap-1">
                <CornerDownRight aria-hidden="true" className="size-3" />
                这是一条回复
              </span>
            ) : null}
          </div>
        </EntityField>
      </EntityStart>

      <EntityEnd className="pt-1">
        <StatusDot
          state={meta?.state ?? "neutral"}
          pulse={comment.status === "pending"}
        >
          {meta?.label ?? comment.status}
        </StatusDot>
        <EntityMeta>
          <time
            dateTime={comment.createdAt}
            title={absoluteDate(comment.createdAt)}
          >
            {relativeTime(comment.createdAt)}
          </time>
        </EntityMeta>

        <EntityActions label={`${author} 的评论的操作`}>
          {comment.status !== "approved" ? (
            <DropdownMenuItem disabled={busy} onSelect={() => approve.mutate()}>
              <Check aria-hidden="true" />
              通过
            </DropdownMenuItem>
          ) : null}
          <DropdownMenuItem onSelect={onReply}>
            <Reply aria-hidden="true" />
            回复
          </DropdownMenuItem>
          {comment.status !== "spam" ? (
            <DropdownMenuItem
              disabled={busy}
              onSelect={() => markSpam.mutate()}
            >
              <ShieldAlert aria-hidden="true" />
              标为垃圾
            </DropdownMenuItem>
          ) : null}
          <DropdownMenuSeparator />
          <DropdownMenuItem danger onSelect={() => setConfirmDelete(true)}>
            <Trash2 aria-hidden="true" />
            删除
          </DropdownMenuItem>
        </EntityActions>
      </EntityEnd>

      <ConfirmDialog
        open={confirmDelete}
        onOpenChange={setConfirmDelete}
        title="删除这条评论？"
        consequence={
          <p>
            <strong className="font-medium text-ink">这一步无法撤销。</strong>
            {comment.parentId === null
              ? "它下面的回复也会一并删除。"
              : "删除的是这一条回复。"}
          </p>
        }
        confirmLabel="删除"
        pending={destroy.isPending}
        onConfirm={async () => {
          await destroy.mutateAsync().catch(() => {});
          setConfirmDelete(false);
        }}
      />
    </Entity>
  );
}

/**
 * 批量处理。
 *
 * 逐条串行而不是并发：审核动作会写库并可能触发邮件通知，
 * 一次几十个并发请求既压服务端，也让「部分成功」的中间状态难以解释。
 * 逐条之间不中断 —— 一条失败不该让剩下的都不处理。删除先经确认框。
 */
function BulkActions({
  ids,
  onDone,
  onClear,
}: {
  ids: number[];
  onDone: () => void;
  onClear: () => void;
}) {
  const [confirmDelete, setConfirmDelete] = useState(false);

  const bulk = useMutation({
    mutationFn: async (action: "approve" | "spam" | "delete") => {
      let ok = 0;
      let failed = 0;
      for (const id of ids) {
        try {
          const path = { params: { path: { id } } };
          const { response } =
            action === "approve"
              ? await api.POST("/api/v1/console/comments/{id}/approve", path)
              : action === "spam"
                ? await api.POST("/api/v1/console/comments/{id}/spam", path)
                : await api.DELETE("/api/v1/console/comments/{id}", path);
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
    },
    onSuccess: ({ ok, failed }, action) => {
      const label = { approve: "通过", spam: "标为垃圾", delete: "删除" }[
        action
      ];
      if (failed === 0) {
        toast.success(`已${label} ${ok} 条`);
      } else {
        toast.warning(`${label}：${ok} 条成功，${failed} 条失败`);
      }
      onDone();
      onClear();
    },
  });

  return (
    <>
      <span className="text-sm font-medium text-ink">已选 {ids.length} 条</span>
      <Button
        variant="secondary"
        size="sm"
        loading={bulk.isPending && bulk.variables === "approve"}
        disabled={bulk.isPending}
        onClick={() => bulk.mutate("approve")}
      >
        <Check aria-hidden="true" />
        通过
      </Button>
      <Button
        variant="secondary"
        size="sm"
        loading={bulk.isPending && bulk.variables === "spam"}
        disabled={bulk.isPending}
        onClick={() => bulk.mutate("spam")}
      >
        <ShieldAlert aria-hidden="true" />
        标为垃圾
      </Button>
      <Button
        variant="danger"
        size="sm"
        loading={bulk.isPending && bulk.variables === "delete"}
        disabled={bulk.isPending}
        onClick={() => setConfirmDelete(true)}
      >
        <Trash2 aria-hidden="true" />
        删除
      </Button>
      <Button
        variant="ghost"
        size="sm"
        onClick={onClear}
        disabled={bulk.isPending}
      >
        取消选择
      </Button>

      <ConfirmDialog
        open={confirmDelete}
        onOpenChange={setConfirmDelete}
        title={`删除选中的 ${ids.length} 条评论？`}
        consequence={
          <p>
            <strong className="font-medium text-ink">这一步无法撤销。</strong>
            顶层评论下面的回复也会一并删除。
          </p>
        }
        confirmLabel="删除"
        pending={bulk.isPending}
        onConfirm={async () => {
          await bulk.mutateAsync("delete").catch(() => {});
          setConfirmDelete(false);
        }}
      />
    </>
  );
}

/** 管理员回复。回复直接通过审核。 */
function ReplyDialog({
  comment,
  onClose,
  onDone,
}: {
  comment: Comment | null;
  onClose: () => void;
  onDone: () => void;
}) {
  const [content, setContent] = useState("");
  const [error, setError] = useState("");

  const reply = useMutation({
    mutationFn: async () => {
      if (!comment) {
        return;
      }
      await runMutation(
        () =>
          api.POST("/api/v1/console/comments/{id}/replies", {
            params: { path: { id: comment.id } },
            body: { content },
          }),
        { success: "回复已发布", invalidate: ["comments"] },
      );
    },
    onSuccess: () => {
      setContent("");
      onDone();
      onClose();
    },
  });

  return (
    <Dialog
      open={comment !== null}
      onOpenChange={(open) => {
        if (!open) {
          setContent("");
          setError("");
          onClose();
        }
      }}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle>回复 {comment?.authorName || "匿名"}</DialogTitle>
        </DialogHeader>
        <DialogBody className="flex flex-col gap-4">
          {/* 引用原评论：回复框里看不到对方说了什么，就得回去翻 */}
          <Inset>
            <p className="line-clamp-4 text-sm whitespace-pre-wrap text-ink-muted">
              {comment?.content}
            </p>
          </Inset>

          <Field>
            <FieldLabel htmlFor="reply-content">回复内容</FieldLabel>
            <Textarea
              id="reply-content"
              rows={4}
              value={content}
              onChange={(e) => {
                setContent(e.target.value);
                setError("");
              }}
              autoFocus
              aria-invalid={error ? true : undefined}
              aria-describedby={error ? "reply-content-error" : undefined}
            />
            <FieldError id="reply-content-error">{error}</FieldError>
          </Field>
        </DialogBody>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>
            取消
          </Button>
          <Button
            variant="primary"
            loading={reply.isPending}
            disabled={!content.trim()}
            onClick={() => {
              if (!content.trim()) {
                setError("回复内容不能为空");
                return;
              }
              reply.mutate();
            }}
          >
            发布回复
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
