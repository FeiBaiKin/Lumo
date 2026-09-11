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
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";
import { Input, InputAffix, Textarea } from "@/components/ui/input";
import { Pagination } from "@/components/ui/pagination";
import { PageHeader } from "@/components/ui/panel";
import { relativeTime } from "@/lib/format";
import { useDocumentTitle } from "@/lib/use-document-title";
import { useDebouncedSearch, useListParams } from "@/lib/use-list-params";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Pencil, Plus, Search, Trash2, X } from "lucide-react";
import { useState } from "react";

/**
 * 标签管理。
 *
 * 与分类的区别不只是「有没有层级」：标签是**横向**的、数量会自然增长到几百个，
 * 故这里是分页表格 + 关键词筛选，而分类是一次性拿全的树。
 * 这个差异来自数据本身，不是为了两个页面看起来不一样。
 */

type Tag = components["schemas"]["Tag"];
type TagBody = components["schemas"]["TagBody"];

type FormState = {
  name: string;
  slug: string;
  description: string;
  color: string;
};

const EMPTY_FORM: FormState = {
  name: "",
  slug: "",
  description: "",
  color: "",
};

export function TagsPage() {
  useDocumentTitle("标签");
  const { can } = useAuth();
  const editable = can("taxonomies:manage");
  const list = useListParams();

  const [editing, setEditing] = useState<Tag | null>(null);
  const [formOpen, setFormOpen] = useState(false);
  const [form, setForm] = useState<FormState>(EMPTY_FORM);
  const [formError, setFormError] = useState("");
  const [deleting, setDeleting] = useState<Tag | null>(null);

  // 搜索框本地存值、停顿后再提交，避免每敲一个字发一次请求。
  // 与地址栏的双向同步由 hook 自己处理（后退时输入框会跟着回填）。
  const [search, setSearch] = useDebouncedSearch(list.filter("q"), (value) =>
    list.setFilter("q", value),
  );

  const query = useQuery({
    queryKey: ["tags", list.page, list.size, list.filters],
    queryFn: async () => {
      const { data, response } = await api.GET("/api/v1/console/tags", {
        params: {
          query: { page: list.page, size: list.size, ...list.filters },
        },
      });
      if (!response.ok) {
        throw new Error(`载入标签失败（HTTP ${response.status}）`);
      }
      return data;
    },
  });

  const items = query.data?.items ?? [];
  const total = query.data?.total ?? 0;

  const save = useMutation({
    mutationFn: async (payload: { id: number | undefined; body: TagBody }) => {
      if (payload.id) {
        return runMutation(
          () =>
            api.PUT("/api/v1/console/tags/{id}", {
              params: { path: { id: payload.id as number } },
              body: payload.body,
            }),
          { success: "标签已保存", invalidate: ["tags"] },
        );
      }
      return runMutation(
        () => api.POST("/api/v1/console/tags", { body: payload.body }),
        {
          success: "标签已创建",
          invalidate: ["tags"],
        },
      );
    },
  });

  const remove = useMutation({
    mutationFn: (id: number) =>
      runMutation(
        () =>
          api.DELETE("/api/v1/console/tags/{id}", { params: { path: { id } } }),
        {
          success: "标签已删除",
          invalidate: ["tags"],
        },
      ),
  });

  function openCreate() {
    setEditing(null);
    setForm(EMPTY_FORM);
    setFormError("");
    setFormOpen(true);
  }

  function openEdit(tag: Tag) {
    setEditing(tag);
    setForm({
      name: tag.name,
      slug: tag.slug,
      description: tag.description,
      color: tag.color,
    });
    setFormError("");
    setFormOpen(true);
  }

  async function onSubmit(event: React.FormEvent) {
    event.preventDefault();
    setFormError("");
    if (!form.name.trim()) {
      setFormError("名称不能为空");
      return;
    }
    const body: TagBody = {
      name: form.name.trim(),
      slug: form.slug.trim(),
      description: form.description.trim(),
      color: form.color.trim(),
    };
    try {
      await save.mutateAsync({ id: editing?.id, body });
      setFormOpen(false);
    } catch {
      // 已播报
    }
  }

  const columns: Column[] = [
    { label: "名称" },
    { label: "slug" },
    { label: "颜色" },
    { label: "描述" },
    { label: "更新时间" },
    { label: "" },
  ];

  return (
    <>
      <PageHeader
        title="标签"
        description="横向的主题词，一篇文章可以带多个；与分类互补"
        actions={
          editable ? (
            <Button variant="primary" onClick={openCreate}>
              <Plus aria-hidden="true" />
              新建标签
            </Button>
          ) : null
        }
      />

      <ListPanel
        toolbar={
          <>
            <ToolbarSearch>
              <Input
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder="按名称或 slug 筛选"
                aria-label="筛选标签"
                className="pl-8"
              />
              <InputAffix side="left">
                <Search aria-hidden="true" />
              </InputAffix>
            </ToolbarSearch>
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
                title="没有匹配的标签"
                description={`当前筛选条件「${list.filter("q")}」下没有结果。`}
                action={
                  <Button variant="secondary" size="sm" onClick={list.reset}>
                    清除筛选
                  </Button>
                }
              />
            ) : (
              <ListEmpty
                title="还没有标签"
                description="标签用来给文章加横向的主题词，比如「Go」「性能优化」。"
                action={
                  editable ? (
                    <Button variant="primary" size="sm" onClick={openCreate}>
                      新建标签
                    </Button>
                  ) : null
                }
              />
            )
          }
        >
          {items.map((tag) => (
            <tr key={tag.id} className="transition-ui hover:bg-surface-hover">
              <td className="px-4 py-2.5">
                <span className="font-medium text-ink">{tag.name}</span>
              </td>
              <td className="px-4 py-2.5">
                <code className="token text-xs text-ink-muted">{tag.slug}</code>
              </td>
              <td className="px-4 py-2.5">
                {tag.color ? (
                  <span className="inline-flex items-center gap-1.5">
                    <span
                      aria-hidden="true"
                      className="size-3 rounded-full border border-line"
                      style={{ backgroundColor: tag.color }}
                    />
                    <code className="text-xs text-ink-muted">{tag.color}</code>
                  </span>
                ) : (
                  <span className="text-xs text-ink-subtle">主题默认</span>
                )}
              </td>
              <td className="max-w-64 px-4 py-2.5">
                <span className="line-clamp-1 text-sm text-ink-muted">
                  {tag.description || "—"}
                </span>
              </td>
              <td className="px-4 py-2.5 text-sm whitespace-nowrap text-ink-muted">
                {relativeTime(tag.updatedAt)}
              </td>
              <td className="w-px px-4 py-2.5 text-right whitespace-nowrap">
                {editable ? (
                  <div className="flex items-center justify-end gap-0.5">
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      onClick={() => openEdit(tag)}
                      aria-label={`编辑 ${tag.name}`}
                    >
                      <Pencil aria-hidden="true" />
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      onClick={() => setDeleting(tag)}
                      aria-label={`删除 ${tag.name}`}
                      className="hover:text-danger"
                    >
                      <Trash2 aria-hidden="true" />
                    </Button>
                  </div>
                ) : null}
              </td>
            </tr>
          ))}
        </ListBody>
      </ListPanel>

      <Dialog open={formOpen} onOpenChange={setFormOpen}>
        <DialogContent>
          <form onSubmit={onSubmit}>
            <DialogHeader>
              <DialogTitle>
                {editing ? `编辑「${editing.name}」` : "新建标签"}
              </DialogTitle>
            </DialogHeader>
            <DialogBody className="flex flex-col gap-4">
              {formError ? (
                <div
                  role="alert"
                  className="rounded-control border border-danger bg-danger-soft px-3 py-2 text-sm text-danger"
                >
                  {formError}
                </div>
              ) : null}

              <Field>
                <FieldLabel htmlFor="tag-name">名称</FieldLabel>
                <Input
                  id="tag-name"
                  value={form.name}
                  onChange={(e) => setForm({ ...form, name: e.target.value })}
                  autoFocus
                  required
                  aria-invalid={formError ? true : undefined}
                />
                <FieldDescription>
                  名称不能重复（不区分大小写）。
                </FieldDescription>
              </Field>

              <Field>
                <FieldLabel htmlFor="tag-slug">slug</FieldLabel>
                <Input
                  id="tag-slug"
                  value={form.slug}
                  onChange={(e) => setForm({ ...form, slug: e.target.value })}
                  placeholder="留空则由名称生成"
                />
              </Field>

              <Field>
                <FieldLabel htmlFor="tag-color">颜色</FieldLabel>
                <div className="flex items-center gap-2">
                  {/*
                    颜色选择用「取色器 + 文本框」两个入口，而不是只给取色器：
                    站点常要沿用品牌色，而品牌色是别人给的一串十六进制，
                    从取色器里挑一个「差不多的」永远对不上。
                  */}
                  <input
                    type="color"
                    id="tag-color"
                    value={form.color || "#1B3A6B"}
                    onChange={(e) =>
                      setForm({ ...form, color: e.target.value })
                    }
                    aria-label="选择颜色"
                    className="h-9 w-12 shrink-0 cursor-pointer rounded-control border border-line-strong bg-surface p-1"
                  />
                  <Input
                    value={form.color}
                    onChange={(e) =>
                      setForm({ ...form, color: e.target.value })
                    }
                    placeholder="留空则用主题默认色"
                    aria-label="颜色十六进制值"
                  />
                </div>
              </Field>

              <Field>
                <FieldLabel htmlFor="tag-desc">描述</FieldLabel>
                <Textarea
                  id="tag-desc"
                  rows={2}
                  value={form.description}
                  onChange={(e) =>
                    setForm({ ...form, description: e.target.value })
                  }
                />
              </Field>
            </DialogBody>
            <DialogFooter>
              <Button variant="secondary" onClick={() => setFormOpen(false)}>
                取消
              </Button>
              <Button type="submit" variant="primary" disabled={save.isPending}>
                {save.isPending ? "正在保存" : "保存"}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
        title={`删除标签「${deleting?.name ?? ""}」？`}
        consequence={<p>带这个标签的文章会失去该标签，文章本身不受影响。</p>}
        confirmLabel="删除"
        pending={remove.isPending}
        onConfirm={async () => {
          if (!deleting) {
            return;
          }
          try {
            await remove.mutateAsync(deleting.id);
            setDeleting(null);
          } catch {
            // 已播报
          }
        }}
      />
    </>
  );
}
