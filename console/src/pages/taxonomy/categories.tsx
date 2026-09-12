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
} from "@/components/data/entity";
import { PageBody, PageHeader } from "@/components/layout/page-header";
import { Alert } from "@/components/ui/alert";
import { Avatar } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
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
import { Input, Textarea } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { absoluteDate, relativeTime } from "@/lib/format";
import { useDocumentTitle } from "@/lib/use-document-title";
import { cn } from "@/lib/utils";
import { useMutation, useQuery } from "@tanstack/react-query";
import { ChevronRight, FolderTree, Pencil, Plus, Trash2 } from "lucide-react";
import { useMemo, useState } from "react";

/**
 * 分类管理。
 *
 * 用树而不是平铺列表：分类是树形结构，"技术 > 前端 > React" 的归属关系
 * 在平铺列表里只能靠一列「父分类」的名字去脑补。树在实体行上靠缩进表达，
 * 有子分类的行前有一枚展开/收起按钮。
 *
 * 服务端同时提供平铺与树两个接口（/categories 与 /categories/tree），
 * 这里用树；树一次拿全，分类数量级（几十到几百）不需要分页。
 */

type CategoryNode = components["schemas"]["CategoryNode"];
type CategoryBody = components["schemas"]["CategoryBody"];

const ROOT = "__root__";

/** 把树摊平成带层级的数组，供逐行渲染。 */
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
        { success: "分类已删除", invalidate: ["categories"] },
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

  function toggleCollapsed(id: number) {
    setCollapsed((prev) => {
      const next = new Set(prev);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
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
        icon={FolderTree}
        title="分类"
        description="树形结构，同级按排序值排列；作者写文章时按它归类"
        actions={
          editable ? (
            <Button variant="primary" onClick={() => openCreate()}>
              <Plus aria-hidden="true" />
              新建分类
            </Button>
          ) : null
        }
      />

      <PageBody>
        <Card>
          {editable ? null : (
            <div className="border-line border-b bg-surface-raised px-4 py-2.5 text-sm text-ink-muted">
              你没有 taxonomies:manage 权限，只能查看分类。
            </div>
          )}

          <ListBody
            isLoading={query.isLoading}
            error={query.error}
            onRetry={() => void query.refetch()}
            isEmpty={rows.length === 0}
            empty={
              <ListEmpty
                icon={FolderTree}
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
            {rows.map(({ node, depth }) => (
              <CategoryRow
                key={node.id}
                node={node}
                depth={depth}
                collapsed={collapsed.has(node.id)}
                editable={editable}
                onToggle={() => toggleCollapsed(node.id)}
                onCreateChild={() => openCreate(node.id)}
                onEdit={() => openEdit(node)}
                onDelete={() => setDeleting(node)}
              />
            ))}
          </ListBody>
        </Card>
      </PageBody>

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
              {formError ? <Alert tone="danger">{formError}</Alert> : null}

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
              <Button type="submit" variant="primary" loading={save.isPending}>
                保存
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

/**
 * 一行分类。
 *
 * 缩进落在开始段的 paddingLeft 上而不是占位元素，这样整行悬停时高亮是连续的；
 * 展开/收起按钮只在有子分类时出现，否则留一个等宽的空位让名字对齐。
 */
function CategoryRow({
  node,
  depth,
  collapsed,
  editable,
  onToggle,
  onCreateChild,
  onEdit,
  onDelete,
}: {
  node: CategoryNode;
  depth: number;
  collapsed: boolean;
  editable: boolean;
  onToggle: () => void;
  onCreateChild: () => void;
  onEdit: () => void;
  onDelete: () => void;
}) {
  const childCount = node.children?.length ?? 0;
  const hasChildren = childCount > 0;

  return (
    <Entity>
      <EntityStart style={{ paddingLeft: `${depth * 1.5}rem` }}>
        {hasChildren ? (
          <button
            type="button"
            onClick={onToggle}
            aria-expanded={!collapsed}
            aria-label={collapsed ? `展开 ${node.name}` : `收起 ${node.name}`}
            className="transition-ui flex size-6 shrink-0 items-center justify-center rounded-control text-ink-muted hover:bg-surface-active hover:text-ink"
          >
            <ChevronRight
              aria-hidden="true"
              className={cn(
                "size-4 transition-transform",
                !collapsed && "rotate-90",
              )}
            />
          </button>
        ) : (
          <span className="size-6 shrink-0" aria-hidden="true" />
        )}

        <Avatar square name={node.name} size="sm" />

        <EntityField
          width="max-w-md"
          title={node.name}
          extra={
            hasChildren ? (
              <Badge tone="outline">{childCount} 个子分类</Badge>
            ) : null
          }
          description={
            <>
              <code className="token">{node.slug}</code>
              {node.description ? (
                <span className="max-w-64 truncate">{node.description}</span>
              ) : null}
            </>
          }
        />
      </EntityStart>

      <EntityEnd>
        <EntityMeta hideOnMobile className="tabular">
          排序 {node.position}
        </EntityMeta>
        <EntityMeta>
          <time dateTime={node.updatedAt} title={absoluteDate(node.updatedAt)}>
            {relativeTime(node.updatedAt)}
          </time>
        </EntityMeta>
        {editable ? (
          <EntityActions label={`分类 ${node.name} 的操作`}>
            <DropdownMenuItem onSelect={onCreateChild}>
              <Plus aria-hidden="true" />
              新建子分类
            </DropdownMenuItem>
            <DropdownMenuItem onSelect={onEdit}>
              <Pencil aria-hidden="true" />
              编辑
            </DropdownMenuItem>
            <DropdownMenuItem danger onSelect={onDelete}>
              <Trash2 aria-hidden="true" />
              删除
            </DropdownMenuItem>
          </EntityActions>
        ) : null}
      </EntityEnd>
    </Entity>
  );
}
