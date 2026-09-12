import { api } from "@/api/client";
import { runMutation } from "@/api/mutation";
import type { components } from "@/api/schema";
import { useAuth } from "@/components/auth/auth-provider";
import {
  Entity,
  EntityActions,
  EntityEnd,
  EntityField,
  EntityMeta,
  EntityStart,
  ListBody,
  ListEmpty,
  ListToolbar,
} from "@/components/data/entity";
import { PageBody, PageHeader } from "@/components/layout/page-header";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import {
  ConfirmDialog,
  Dialog,
  DialogBody,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { DropdownMenuItem } from "@/components/ui/dropdown-menu";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";
import { Input, SearchInput, Textarea } from "@/components/ui/input";
import { Pagination } from "@/components/ui/pagination";
import { absoluteDate, relativeTime } from "@/lib/format";
import { useDocumentTitle } from "@/lib/use-document-title";
import { useDebouncedSearch, useListParams } from "@/lib/use-list-params";
import { cn } from "@/lib/utils";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Pencil, Plus, Tags, Trash2 } from "lucide-react";
import { useState } from "react";

/**
 * 标签管理。
 *
 * 与分类的区别不只是「有没有层级」：标签是**横向**的、数量会自然增长到几百个，
 * 故这里是分页列表 + 关键词筛选，而分类是一次性拿全的树。
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
        { success: "标签已创建", invalidate: ["tags"] },
      );
    },
  });

  const remove = useMutation({
    mutationFn: (id: number) =>
      runMutation(
        () =>
          api.DELETE("/api/v1/console/tags/{id}", { params: { path: { id } } }),
        { success: "标签已删除", invalidate: ["tags"] },
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

  return (
    <>
      <PageHeader
        icon={Tags}
        title="标签"
        description="横向的主题词，一篇文章可以带多个，与分类互补"
        actions={
          editable ? (
            <Button variant="primary" onClick={openCreate}>
              <Plus aria-hidden="true" />
              新建标签
            </Button>
          ) : null
        }
      />

      <PageBody>
        <Card>
          <ListToolbar
            className="border-line border-b"
            search={
              <SearchInput
                value={search}
                onValueChange={setSearch}
                placeholder="按名称或 slug 筛选"
                aria-label="筛选标签"
                className="max-w-xs"
              />
            }
            hasFilters={list.hasFilters}
            onClearFilters={list.reset}
            onRefresh={() => void query.refetch()}
            refreshing={query.isFetching}
          />

          <ListBody
            isLoading={query.isLoading}
            error={query.error}
            onRetry={() => void query.refetch()}
            isEmpty={items.length === 0}
            empty={
              list.hasFilters ? (
                <ListEmpty
                  icon={Tags}
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
                  icon={Tags}
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
              <Entity key={tag.id}>
                <EntityStart>
                  {/* 颜色圆点：标签在前台的展示色。没有设色时用中性点，而不是空着 */}
                  <span
                    aria-hidden="true"
                    className={cn(
                      "size-3 shrink-0 rounded-full border border-line",
                      !tag.color && "bg-neutral-dot",
                    )}
                    style={
                      tag.color ? { backgroundColor: tag.color } : undefined
                    }
                  />
                  <EntityField
                    width="max-w-md"
                    title={tag.name}
                    description={
                      <>
                        <code className="token">{tag.slug}</code>
                        {tag.description ? (
                          <span className="max-w-64 truncate">
                            {tag.description}
                          </span>
                        ) : null}
                      </>
                    }
                  />
                </EntityStart>

                <EntityEnd>
                  <EntityMeta hideOnMobile>
                    {tag.color ? (
                      <code className="token">{tag.color}</code>
                    ) : (
                      "主题默认"
                    )}
                  </EntityMeta>
                  <EntityMeta>
                    <time
                      dateTime={tag.updatedAt}
                      title={absoluteDate(tag.updatedAt)}
                    >
                      {relativeTime(tag.updatedAt)}
                    </time>
                  </EntityMeta>
                  {editable ? (
                    <EntityActions label={`标签 ${tag.name} 的操作`}>
                      <DropdownMenuItem onSelect={() => openEdit(tag)}>
                        <Pencil aria-hidden="true" />
                        编辑
                      </DropdownMenuItem>
                      <DropdownMenuItem
                        danger
                        onSelect={() => setDeleting(tag)}
                      >
                        <Trash2 aria-hidden="true" />
                        删除
                      </DropdownMenuItem>
                    </EntityActions>
                  ) : null}
                </EntityEnd>
              </Entity>
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

      <Dialog open={formOpen} onOpenChange={setFormOpen}>
        <DialogContent>
          <form onSubmit={onSubmit}>
            <DialogHeader>
              <DialogTitle>
                {editing ? `编辑「${editing.name}」` : "新建标签"}
              </DialogTitle>
            </DialogHeader>
            <DialogBody className="flex flex-col gap-4">
              {formError ? <Alert tone="danger">{formError}</Alert> : null}

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
              <Button type="submit" variant="primary" loading={save.isPending}>
                保存
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
