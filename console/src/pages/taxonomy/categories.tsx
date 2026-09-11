import { api } from "@/api/client";
import { runMutation } from "@/api/mutation";
import type { components } from "@/api/schema";
import { useAuth } from "@/components/auth/auth-provider";
import {
  type Column,
  ListBody,
  ListEmpty,
  ListPanel,
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
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";
import { Input, Textarea } from "@/components/ui/input";
import { PageHeader } from "@/components/ui/panel";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { relativeTime } from "@/lib/format";
import { useDocumentTitle } from "@/lib/use-document-title";
import { cn } from "@/lib/utils";
import { useMutation, useQuery } from "@tanstack/react-query";
import { ChevronRight, FolderTree, Pencil, Plus, Trash2 } from "lucide-react";
import { useMemo, useState } from "react";

/**
 * 分类管理。
 *
 * 用树而不是平铺表格：分类是树形结构，"技术 > 前端 > React" 的归属关系
 * 在平铺列表里只能靠一列「父分类」的名字去脑补。
 *
 * 服务端同时提供平铺与树两个接口（/categories 与 /categories/tree），
 * 这里用树；树一次拿全，分类数量级（几十到几百）不需要分页。
 */

type CategoryNode = components["schemas"]["CategoryNode"];
type CategoryBody = components["schemas"]["CategoryBody"];

const ROOT = "__root__";

/** 把树摊平成带层级的数组，供表格渲染。 */
function flatten(
  nodes: CategoryNode[],
  depth = 0,
): { node: CategoryNode; depth: number }[] {
  const out: { node: CategoryNode; depth: number }[] = [];
  for (const node of nodes) {
    out.push({ node, depth });
    if (node.children?.length) {
      out.push(...flatten(node.children, depth + 1));
    }
  }
  return out;
}

/** 父分类下拉的选项：树形缩进，且排除自身与其后代（否则会成环）。 */
function parentOptions(
  nodes: CategoryNode[],
  excludeId?: number,
  depth = 0,
): { id: number; label: string }[] {
  const out: { id: number; label: string }[] = [];
  for (const node of nodes) {
    if (node.id === excludeId) {
      // 整棵子树都跳过 —— 把分类挂到自己的后代下会成环，服务端会拒，
      // 但不该让用户先选一次再被拒。
      continue;
    }
    out.push({ id: node.id, label: `${"— ".repeat(depth)}${node.name}` });
    if (node.children?.length) {
      out.push(...parentOptions(node.children, excludeId, depth + 1));
    }
  }
  return out;
}

type FormState = {
  name: string;
  slug: string;
  description: string;
  coverUrl: string;
  parentId: number | undefined;
  position: string;
};

const EMPTY_FORM: FormState = {
  name: "",
  slug: "",
  description: "",
  coverUrl: "",
  parentId: undefined,
  position: "",
};

export function CategoriesPage() {
  useDocumentTitle("分类");
  const { can } = useAuth();
  const editable = can("taxonomies:manage");

  const [collapsed, setCollapsed] = useState<Set<number>>(new Set());
  const [editing, setEditing] = useState<CategoryNode | null>(null);
  const [formOpen, setFormOpen] = useState(false);
  const [form, setForm] = useState<FormState>(EMPTY_FORM);
  const [formError, setFormError] = useState("");
  const [deleting, setDeleting] = useState<CategoryNode | null>(null);

  const query = useQuery({
    queryKey: ["categories", "tree"],
    queryFn: async () => {
      const { data, response } = await api.GET(
        "/api/v1/console/categories/tree",
      );
      if (!response.ok) {
        throw new Error(`载入分类失败（HTTP ${response.status}）`);
      }
      return data?.items ?? [];
    },
  });

  /** 折叠后不显示子节点。为了保持层级缩进正确，折叠是从完整树的摊平结果里过滤。 */
  const rows = useMemo(() => {
    const all = flatten(query.data ?? []);
    const out: { node: CategoryNode; depth: number }[] = [];
    const hidden = new Set<number>();
    for (const row of all) {
      if (row.node.parentId !== null && hidden.has(row.node.parentId)) {
        hidden.add(row.node.id);
        continue;
      }
      if (collapsed.has(row.node.id)) {
        hidden.add(row.node.id);
      }
      out.push(row);
    }
    return out;
  }, [query.data, collapsed]);

  const parents = useMemo(
    () => parentOptions(query.data ?? [], editing?.id),
    [query.data, editing],
  );

  const columns: Column[] = [
    { label: "名称" },
    { label: "slug" },
    { label: "描述" },
    { label: "排序", numeric: true },
    { label: "更新时间" },
    { label: "" },
  ];

  const save = useMutation({
    mutationFn: async (payload: {
      id: number | undefined;
      body: CategoryBody;
    }) => {
      if (payload.id) {
        return runMutation(
          () =>
            api.PUT("/api/v1/console/categories/{id}", {
              params: { path: { id: payload.id as number } },
              body: payload.body,
            }),
          { success: "分类已保存", invalidate: ["categories"] },
        );
      }
      return runMutation(
        () => api.POST("/api/v1/console/categories", { body: payload.body }),
        { success: "分类已创建", invalidate: ["categories"] },
      );
    },
  });

  const remove = useMutation({
    mutationFn: (id: number) =>
      runMutation(
        () =>
          api.DELETE("/api/v1/console/categories/{id}", {
            params: { path: { id } },
          }),
        {
          success: "分类已删除",
          invalidate: ["categories"],
        },
      ),
  });

  function openCreate(parentId?: number) {
    setEditing(null);
    setForm({ ...EMPTY_FORM, parentId });
    setFormError("");
    setFormOpen(true);
  }

  function openEdit(node: CategoryNode) {
    setEditing(node);
    setForm({
      name: node.name,
      slug: node.slug,
      description: node.description,
      coverUrl: node.coverUrl,
      parentId: node.parentId ?? undefined,
      position: String(node.position),
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
    const body: CategoryBody = {
      name: form.name.trim(),
      slug: form.slug.trim(),
      description: form.description.trim(),
      coverUrl: form.coverUrl.trim(),
    };
    if (form.parentId !== undefined) {
      body.parentId = form.parentId;
    }
    if (form.position.trim() !== "") {
      body.position = Number(form.position);
    }
    try {
      await save.mutateAsync({ id: editing?.id, body });
      setFormOpen(false);
    } catch {
      // 错误已由 runMutation 播报，这里只需保持对话框打开让用户改
    }
  }

  return (
    <>
      <PageHeader
        title="分类"
        description="树形结构，同级按排序值排列；作者写文章时需要按它归类"
        actions={
          editable ? (
            <Button variant="primary" onClick={() => openCreate()}>
              <Plus aria-hidden="true" />
              新建分类
            </Button>
          ) : null
        }
      />

      <ListPanel
        toolbar={
          editable ? undefined : (
            <p className="text-xs text-ink-muted">
              你没有 taxonomies:manage 权限，只能查看分类。
            </p>
          )
        }
      >
        <ListBody
          columns={columns}
          isLoading={query.isLoading}
          error={query.error}
          onRetry={() => void query.refetch()}
          isEmpty={rows.length === 0}
          empty={
            <ListEmpty
              title="还没有分类"
              description="分类用来把文章组织成层级，例如「技术 > 前端」。文章也可以不归任何分类。"
              action={
                editable ? (
                  <Button
                    variant="primary"
                    size="sm"
                    onClick={() => openCreate()}
                  >
                    新建分类
                  </Button>
                ) : null
              }
            />
          }
        >
          {rows.map(({ node, depth }) => {
            const hasChildren = (node.children?.length ?? 0) > 0;
            const isCollapsed = collapsed.has(node.id);
            return (
              <tr
                key={node.id}
                className="transition-ui hover:bg-surface-hover"
              >
                {/* 名称列承担树形缩进 —— 缩进用 padding 而不是占位元素，
                    这样选中整行时高亮是连续的 */}
                <td className="px-4 py-2.5">
                  <div
                    className="flex items-center gap-1.5"
                    style={{ paddingLeft: `${depth * 1.25}rem` }}
                  >
                    {hasChildren ? (
                      <button
                        type="button"
                        onClick={() =>
                          setCollapsed((prev) => {
                            const next = new Set(prev);
                            if (next.has(node.id)) {
                              next.delete(node.id);
                            } else {
                              next.add(node.id);
                            }
                            return next;
                          })
                        }
                        aria-expanded={!isCollapsed}
                        aria-label={
                          isCollapsed
                            ? `展开 ${node.name}`
                            : `收起 ${node.name}`
                        }
                        className="transition-ui flex size-5 shrink-0 items-center justify-center rounded-control text-ink-muted hover:bg-surface-active hover:text-ink"
                      >
                        <ChevronRight
                          aria-hidden="true"
                          className={cn(
                            "size-3.5 transition-transform",
                            !isCollapsed && "rotate-90",
                          )}
                        />
                      </button>
                    ) : (
                      <span className="size-5 shrink-0" aria-hidden="true" />
                    )}
                    <FolderTree
                      aria-hidden="true"
                      className={cn(
                        "size-4 shrink-0",
                        depth === 0 ? "text-ink-subtle" : "text-ink-subtle/60",
                      )}
                    />
                    <span className="truncate font-medium text-ink">
                      {node.name}
                    </span>
                    {hasChildren ? (
                      <Badge tone="outline">
                        {node.children?.length} 个子分类
                      </Badge>
                    ) : null}
                  </div>
                </td>
                <td className="px-4 py-2.5">
                  <code className="token text-xs text-ink-muted">
                    {node.slug}
                  </code>
                </td>
                <td className="max-w-64 px-4 py-2.5">
                  <span className="line-clamp-1 text-sm text-ink-muted">
                    {node.description || "—"}
                  </span>
                </td>
                <td className="tabular px-4 py-2.5 text-right text-sm text-ink-muted">
                  {node.position}
                </td>
                <td className="px-4 py-2.5 text-sm whitespace-nowrap text-ink-muted">
                  {relativeTime(node.updatedAt)}
                </td>
                <td className="w-px px-4 py-2.5 text-right whitespace-nowrap">
                  {editable ? (
                    <div className="flex items-center justify-end gap-0.5">
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        onClick={() => openCreate(node.id)}
                        aria-label={`在 ${node.name} 下新建子分类`}
                        title="新建子分类"
                      >
                        <Plus aria-hidden="true" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        onClick={() => openEdit(node)}
                        aria-label={`编辑 ${node.name}`}
                        title="编辑"
                      >
                        <Pencil aria-hidden="true" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        onClick={() => setDeleting(node)}
                        aria-label={`删除 ${node.name}`}
                        title="删除"
                        className="hover:text-danger"
                      >
                        <Trash2 aria-hidden="true" />
                      </Button>
                    </div>
                  ) : null}
                </td>
              </tr>
            );
          })}
        </ListBody>
      </ListPanel>

      {/* 新建 / 编辑 */}
      <Dialog open={formOpen} onOpenChange={setFormOpen}>
        <DialogContent>
          <form onSubmit={onSubmit}>
            <DialogHeader>
              <DialogTitle>
                {editing ? `编辑「${editing.name}」` : "新建分类"}
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
                <FieldLabel htmlFor="cat-name">名称</FieldLabel>
                <Input
                  id="cat-name"
                  value={form.name}
                  onChange={(e) => setForm({ ...form, name: e.target.value })}
                  autoFocus
                  required
                  aria-invalid={formError ? true : undefined}
                />
                <FieldDescription>
                  同一父分类下名称不能重复（不区分大小写）。
                </FieldDescription>
              </Field>

              <Field>
                <FieldLabel htmlFor="cat-slug">slug</FieldLabel>
                <Input
                  id="cat-slug"
                  value={form.slug}
                  onChange={(e) => setForm({ ...form, slug: e.target.value })}
                  placeholder="留空则由名称生成"
                />
                <FieldDescription>
                  地址栏里的一段，全局唯一。留空时按站点设置的 slug
                  策略从名称生成（中文保留）。
                </FieldDescription>
              </Field>

              <Field>
                <FieldLabel htmlFor="cat-parent">父分类</FieldLabel>
                <Select
                  value={
                    form.parentId === undefined ? ROOT : String(form.parentId)
                  }
                  onValueChange={(value) =>
                    setForm({
                      ...form,
                      parentId: value === ROOT ? undefined : Number(value),
                    })
                  }
                >
                  <SelectTrigger id="cat-parent">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value={ROOT}>（作为根分类）</SelectItem>
                    {parents.map((option) => (
                      <SelectItem key={option.id} value={String(option.id)}>
                        {option.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>

              <Field>
                <FieldLabel htmlFor="cat-desc">描述</FieldLabel>
                <Textarea
                  id="cat-desc"
                  rows={2}
                  value={form.description}
                  onChange={(e) =>
                    setForm({ ...form, description: e.target.value })
                  }
                />
              </Field>

              <div className="grid gap-4 sm:grid-cols-2">
                <Field>
                  <FieldLabel htmlFor="cat-cover">封面图地址</FieldLabel>
                  <Input
                    id="cat-cover"
                    value={form.coverUrl}
                    onChange={(e) =>
                      setForm({ ...form, coverUrl: e.target.value })
                    }
                    placeholder="https://…"
                  />
                  <FieldDescription>从附件库复制地址后粘贴。</FieldDescription>
                </Field>
                <Field>
                  <FieldLabel htmlFor="cat-pos">排序</FieldLabel>
                  <Input
                    id="cat-pos"
                    type="number"
                    min={0}
                    value={form.position}
                    onChange={(e) =>
                      setForm({ ...form, position: e.target.value })
                    }
                    placeholder={editing ? "留空保留原值" : "留空排在末尾"}
                  />
                </Field>
              </div>
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

      {/*
        删除确认。
        后果必须写清：服务端把子分类挂到祖父分类而不是级联删除。
        一个只问「确定吗」的确认框，用户会以为子分类也会一起没。
      */}
      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
        title={`删除分类「${deleting?.name ?? ""}」？`}
        consequence={
          <>
            <p>
              {(deleting?.children?.length ?? 0) > 0
                ? `它的 ${deleting?.children?.length} 个子分类不会被删除，会改为挂到它的上一级分类下。`
                : "它下面没有子分类。"}
            </p>
            <p className="mt-1">
              原本归入这个分类的文章会失去该归类，但文章本身不受影响。
            </p>
          </>
        }
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
