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
import {
  ConfirmDialog,
  Dialog,
  DialogBody,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Field, FieldError, FieldLabel } from "@/components/ui/field";
import { Input, InputAffix, Textarea } from "@/components/ui/input";
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
import { Check, Search, ShieldAlert, Trash2, X } from "lucide-react";
import { useState } from "react";
import { Link } from "react-router";

/**
 * 评论审核。
 *
 * 这是后台**使用频率最高**的页面 —— 站长每天都要来这里过一遍待审。
 * 因此它的设计重点不是功能全，而是「一眼看清 + 一次点击处理完」：
 *
 *   - 默认筛选就是「待审」而不是「全部」：进来就看见要处理的。
 *     看全部时要在一堆已通过的评论里找那几条待审的，完全是白费眼神。
 *   - 通过 / 标垃圾 / 删除直接放在行上，不进详情页。
 *     审核是批量动作，为每条评论点进点出会让日均几十条的站变成苦差。
 *   - 勾选后出现批量条，处理完即消失。
 *
 * 正文用纯文本渲染而不是 contentHtml：访客提交的内容在服务端已转义，
 * 但后台没有理由把访客控制的 HTML 塞进自己的页面 ——
 * 这里要的是「看清他写了什么」，不是「还原他想要的样式」。
 */

type Comment = components["schemas"]["Comment"];
type Status = "pending" | "approved" | "spam";

const STATUS_META: Record<
  Status,
  { label: string; tone: "warn" | "ok" | "danger" }
> = {
  pending: { label: "待审", tone: "warn" },
  approved: { label: "已通过", tone: "ok" },
  spam: { label: "垃圾", tone: "danger" },
};

export function CommentsPage() {
  useDocumentTitle("评论");
  const { can } = useAuth();
  const canManageAny = can("comments:manage_any");
  const list = useListParams();

  // 默认进「待审」：这是这个页面存在的理由
  const status = (list.filter("status") || "pending") as Status | "all";

  const [search, setSearch] = useDebouncedSearch(list.filter("q"), (value) =>
    list.setFilter("q", value),
  );
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [replying, setReplying] = useState<Comment | null>(null);

  const query = useQuery({
    queryKey: ["comments", list.page, list.size, list.filters, status],
    queryFn: async () => {
      const { data, response } = await api.GET("/api/v1/console/comments", {
        params: {
          query: {
            page: list.page,
            size: list.size,
            ...(status === "all" ? {} : { status }),
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

  const columns: Column[] = [
    { label: "" },
    { label: "评论者" },
    { label: "内容" },
    { label: "所属内容" },
    { label: "状态" },
    { label: "时间" },
    { label: "" },
  ];

  return (
    <>
      <PageHeader
        title="评论"
        description={
          canManageAny
            ? "审核访客评论，处理垃圾与回复"
            : "你只能看到自己内容下的评论"
        }
      />

      <ListPanel
        toolbar={
          <>
            <Select
              value={status}
              onValueChange={(value) =>
                list.setFilter("status", value === "pending" ? "" : value)
              }
            >
              <SelectTrigger className="w-40" aria-label="按状态筛选">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="pending">待审</SelectItem>
                <SelectItem value="approved">已通过</SelectItem>
                <SelectItem value="spam">垃圾</SelectItem>
                <SelectItem value="all">全部</SelectItem>
              </SelectContent>
            </Select>

            <ToolbarSearch>
              <Input
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder="按正文或评论者筛选"
                aria-label="筛选评论"
                className="pl-8"
              />
              <InputAffix side="left">
                <Search aria-hidden="true" />
              </InputAffix>
            </ToolbarSearch>

            {list.hasFilters || status !== "pending" ? (
              <Button variant="ghost" size="sm" onClick={list.reset}>
                <X aria-hidden="true" />
                重置筛选
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
        {/*
          批量操作条。勾选后才出现，处理完即消失 ——
          常驻一条「已选 0 项」的灰条只是白占一行。
        */}
        {selected.size > 0 ? (
          <div className="flex flex-wrap items-center gap-2 border-line border-t bg-seal-soft px-4 py-2">
            <span className="text-sm font-medium text-seal">
              已选 {selected.size} 条
            </span>
            <div className="flex-1" />
            <BulkActions
              ids={[...selected]}
              onDone={refresh}
              onClear={() => setSelected(new Set())}
            />
          </div>
        ) : null}

        <ListBody
          columns={columns}
          isLoading={query.isLoading}
          error={query.error}
          onRetry={() => void query.refetch()}
          isEmpty={items.length === 0}
          empty={
            status === "pending" ? (
              <ListEmpty
                title="没有待审评论"
                description="访客新提交的评论会出现在这里。现在没有积压。"
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
      </ListPanel>

      {items.length > 0 ? (
        <div className="mt-3 flex items-center gap-2">
          <input
            id="select-all-comments"
            type="checkbox"
            checked={allSelected}
            onChange={(e) => {
              setSelected(
                e.target.checked
                  ? new Set(items.map((item) => item.id))
                  : new Set(),
              );
            }}
            className="size-4 cursor-pointer rounded-[3px] border-line-strong accent-seal"
          />
          <label
            htmlFor="select-all-comments"
            className="text-sm text-ink-muted select-none"
          >
            选择本页全部 {items.length} 条
          </label>
        </div>
      ) : null}

      <ReplyDialog
        comment={replying}
        onClose={() => setReplying(null)}
        onDone={refresh}
      />
    </>
  );
}

/**
 * 一行评论。
 *
 * 每行自持动作（与文章页同一形态）：把 mutation 提到页面级会让「哪一行的按钮在转」
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

  return (
    <tr className="transition-ui hover:bg-surface-hover">
      <td className="w-px py-2.5 pl-4">
        <input
          type="checkbox"
          checked={checked}
          onChange={(e) => onToggle(e.target.checked)}
          aria-label={`选择 ${comment.authorName || "匿名"} 的评论`}
          className="size-4 cursor-pointer rounded-[3px] border-line-strong accent-seal"
        />
      </td>

      <td className="px-4 py-2.5 align-top">
        <div className="flex flex-col gap-0.5">
          <span className="font-medium text-ink">
            {comment.authorName || "匿名"}
          </span>
          {comment.authorUrl ? (
            <a
              href={comment.authorUrl}
              target="_blank"
              // noopener 必须带上：不带时新页面能通过 window.opener 操作本站
              rel="noopener noreferrer nofollow"
              className="token transition-ui max-w-40 truncate text-xs text-seal hover:underline"
            >
              {comment.authorUrl}
            </a>
          ) : null}
          {/* 邮箱与 IP 只在 Console 平面出现（agent.md §8）——站长排查需要它们 */}
          <span className="token max-w-44 truncate text-xs text-ink-subtle">
            {comment.authorEmail || "无邮箱"}
          </span>
        </div>
      </td>

      <td className="max-w-lg px-4 py-2.5 align-top">
        {/* 纯文本渲染：要看清他写了什么，不还原他想要的样式 */}
        <p className="line-clamp-3 text-sm whitespace-pre-wrap text-ink">
          {comment.content}
        </p>
        {comment.parentId ? (
          <span className="mt-1 inline-block text-xs text-ink-subtle">
            这是一条回复
          </span>
        ) : null}
      </td>

      <td className="max-w-40 px-4 py-2.5 align-top">
        {comment.post ? (
          <Link
            to={`/posts/${comment.post.id}`}
            className="transition-ui line-clamp-2 text-sm text-seal hover:underline"
          >
            {comment.post.title}
          </Link>
        ) : (
          <span className="text-xs text-ink-subtle">内容已删除</span>
        )}
      </td>

      <td className="px-4 py-2.5 align-top">
        <Badge tone={meta?.tone ?? "neutral"}>
          {meta?.label ?? comment.status}
        </Badge>
      </td>

      <td className="px-4 py-2.5 align-top text-sm whitespace-nowrap text-ink-muted">
        <time
          dateTime={comment.createdAt}
          title={absoluteDate(comment.createdAt)}
        >
          {relativeTime(comment.createdAt)}
        </time>
      </td>

      <td className="w-px px-4 py-2.5 text-right align-top whitespace-nowrap">
        <div className="flex items-center justify-end gap-0.5">
          {comment.status !== "approved" ? (
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={() => approve.mutate()}
              disabled={approve.isPending}
              aria-label={`通过 ${comment.authorName || "匿名"} 的评论`}
              title="通过"
              className="hover:text-ok"
            >
              <Check aria-hidden="true" />
            </Button>
          ) : null}
          <Button
            variant="ghost"
            size="sm"
            onClick={onReply}
            title="以管理员身份回复"
          >
            回复
          </Button>
          {comment.status !== "spam" ? (
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={() => markSpam.mutate()}
              disabled={markSpam.isPending}
              aria-label={`标记 ${comment.authorName || "匿名"} 的评论为垃圾`}
              title="标记为垃圾"
            >
              <ShieldAlert aria-hidden="true" />
            </Button>
          ) : null}
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={() => setConfirmDelete(true)}
            aria-label={`删除 ${comment.authorName || "匿名"} 的评论`}
            title="删除"
            className="hover:text-danger"
          >
            <Trash2 aria-hidden="true" />
          </Button>
        </div>

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
      </td>
    </tr>
  );
}

/**
 * 批量处理。
 *
 * 逐条串行而不是并发：审核动作会写库并可能触发邮件通知，
 * 一次几十个并发请求既压服务端，也让「部分成功」的中间状态难以解释。
 * 逐条之间不中断 —— 一条失败不该让剩下的都不处理。
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
  const [result, setResult] = useState<string>("");

  const bulk = useMutation({
    mutationFn: async (action: "approve" | "spam") => {
      let ok = 0;
      let failed = 0;
      for (const id of ids) {
        try {
          const { response } =
            action === "approve"
              ? await api.POST("/api/v1/console/comments/{id}/approve", {
                  params: { path: { id } },
                })
              : await api.POST("/api/v1/console/comments/{id}/spam", {
                  params: { path: { id } },
                });
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
    onSuccess: ({ ok, failed }) => {
      setResult(
        failed === 0 ? `已处理 ${ok} 条` : `${ok} 条成功，${failed} 条失败`,
      );
      onDone();
      onClear();
    },
  });

  return (
    <>
      {result ? <span className="text-xs text-seal">{result}</span> : null}
      <Button
        variant="secondary"
        size="sm"
        disabled={bulk.isPending}
        onClick={() => bulk.mutate("approve")}
      >
        <Check aria-hidden="true" />
        {bulk.isPending ? "处理中" : "全部通过"}
      </Button>
      <Button
        variant="secondary"
        size="sm"
        disabled={bulk.isPending}
        onClick={() => bulk.mutate("spam")}
      >
        <ShieldAlert aria-hidden="true" />
        标为垃圾
      </Button>
      <Button
        variant="ghost"
        size="sm"
        onClick={onClear}
        disabled={bulk.isPending}
      >
        取消选择
      </Button>
    </>
  );
}

/** 管理员回复。回复直接通过审核（agent.md §8）。 */
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
          <blockquote className="rounded-control border-line border-l-2 bg-surface-raised px-3 py-2">
            <p className="line-clamp-4 text-sm whitespace-pre-wrap text-ink-muted">
              {comment?.content}
            </p>
          </blockquote>

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
            disabled={reply.isPending || !content.trim()}
            onClick={() => {
              if (!content.trim()) {
                setError("回复内容不能为空");
                return;
              }
              reply.mutate();
            }}
          >
            {reply.isPending ? "正在发布" : "发布回复"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
