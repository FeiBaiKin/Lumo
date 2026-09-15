import { api } from "@/api/client";
import { runMutation } from "@/api/mutation";
import type { components } from "@/api/schema";
import { useAuth } from "@/components/auth/auth-provider";
import {
  Entity,
  EntityActions,
  EntityEnd,
  EntityField,
  EntityList,
  EntityStart,
  ListEmpty,
} from "@/components/data/entity";
import { PageBody, PageHeader } from "@/components/layout/page-header";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, Inset } from "@/components/ui/card";
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
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";
import { Input, Textarea } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { EntitySkeleton, ErrorState, Skeleton } from "@/components/ui/states";
import { CheckboxRow, Switch } from "@/components/ui/toggle";
import { useDocumentTitle } from "@/lib/use-document-title";
import { cn } from "@/lib/utils";
import { useMutation, useQuery } from "@tanstack/react-query";
import {
  ExternalLink,
  GripVertical,
  IndentDecrease,
  IndentIncrease,
  ListTree,
  Pencil,
  Plus,
  Trash2,
} from "lucide-react";
import { Reorder, useDragControls } from "motion/react";
import { useMemo, useState } from "react";

/**
 * 菜单管理（形态对齐 Halo 的菜单页：左列表、右条目树）。
 *
 * 条目树**整体替换**式保存：菜单是一次性编辑、一次性保存的表单，
 * 逐条 diff 要处理移动、重排、删除与重建的交叉情形，出错概率远大于收益。
 * 界面上因此没有「保存这一条」—— 只有一个「保存菜单」。
 *
 * 排序用**拖动**：手柄只在同一层级内排序（一级与一级之间、子项与子项之间），
 * 跨层级改结构走「缩进 / 提升」两个按钮。这么分工是因为跨级拖动要处理
 * 「拖进谁的下面、落在第几个」这类歧义，成功率低而返回键只有 Ctrl+Z 没有撤销栈；
 * 同级拖动则没有歧义。手柄本身是可聚焦的按钮，聚焦后按 ↑ ↓ 也能排序，
 * 不把键盘用户挡在外面（拖动用的是 motion 的 Reorder，Console 里已有的依赖）。
 *
 * 站内条目（文章 / 页面 / 分类 / 标签）的地址**不落库、读取时解析**：
 * 记录改名或改 slug 后菜单自动跟随，指向已删除或未发布记录的条目
 * 在前台被跳过（不留死链），在后台保留可见（便于修）。
 */

type Menu = components["schemas"]["Menu"];
type ItemNode = components["schemas"]["ItemNode"];
type MenuItemInput = components["schemas"]["MenuItemInput"];

/** 编辑中的条目：有客户端 key 以便嵌套时稳定引用。 */
type DraftItem = {
  key: string;
  label: string;
  type: MenuItemInput["type"];
  targetId: number | null;
  url: string;
  target: "" | "_blank";
  rel: string;
  visible: boolean;
  children: DraftItem[];
};

const MAX_DEPTH = 3;
const MAX_ITEMS = 200;

const TYPE_META: Record<string, { label: string; hint: string }> = {
  custom: { label: "自定义链接", hint: "地址由你填写，可以指向站外" },
  post: { label: "文章", hint: "从已发布的文章里选，改标题后菜单自动跟随" },
  page: { label: "页面", hint: "从独立页面里选" },
  category: { label: "分类", hint: "从分类里选" },
  tag: { label: "标签", hint: "从标签里选" },
};

let seq = 0;
function nextKey() {
  seq += 1;
  return `draft-${seq}`;
}

function toDraft(node: ItemNode): DraftItem {
  return {
    key: nextKey(),
    label: node.label,
    type: node.type as MenuItemInput["type"],
    targetId: node.targetId,
    url: node.url,
    target: node.target === "_blank" ? "_blank" : "",
    rel: node.rel,
    visible: node.visible,
    children: (node.children ?? []).map(toDraft),
  };
}

function emptyDraft(): DraftItem {
  return {
    key: nextKey(),
    label: "",
    type: "custom",
    targetId: null,
    url: "",
    target: "",
    rel: "",
    visible: true,
    children: [],
  };
}

/**
 * 转成服务端要的请求体。
 *
 * **必须发嵌套结构**（`children`），服务端自己摊平成深度优先序再按下标建层级
 * （见 internal/menu/handler.go 的 flatten）。前端若先摊平再发，
 * 服务端会把它们全当成顶层条目 —— 层级会整体丢掉。
 *
 * 另：`targetId` 在 schema 里是 `integer, minimum: 1`，**不接受 null**。
 * 自定义链接要**省略**这个字段而不是填 null，否则请求会被 422 拒掉
 * （`additionalProperties: false` + 类型校验）。
 */
function toInput(items: DraftItem[]): MenuItemInput[] {
  return items.map((item) => {
    const node: MenuItemInput = {
      label: item.label,
      type: item.type,
      target: item.target,
      rel: item.rel,
      visible: item.visible,
      url: item.type === "custom" ? item.url : "",
    };
    if (item.type !== "custom" && item.targetId) {
      node.targetId = item.targetId;
    }
    if (item.children.length > 0) {
      node.children = toInput(item.children);
    }
    return node;
  });
}

function countItems(items: DraftItem[]): number {
  return items.reduce((sum, item) => sum + 1 + countItems(item.children), 0);
}

type MenuForm = { name: string; slug: string; description: string };

const EMPTY_MENU: MenuForm = { name: "", slug: "", description: "" };

export function MenusPage() {
  useDocumentTitle("菜单");
  const { can } = useAuth();
  const editable = can("menus:manage");

  const [selectedId, setSelectedId] = useState<number | null>(null);
  const [editingId, setEditingId] = useState<number | null>(null);
  const [formOpen, setFormOpen] = useState(false);
  const [form, setForm] = useState<MenuForm>(EMPTY_MENU);
  const [formError, setFormError] = useState("");
  const [deleting, setDeleting] = useState<Menu | null>(null);

  const query = useQuery({
    queryKey: ["menus"],
    queryFn: async () => {
      const { data, response } = await api.GET("/api/v1/console/menus");
      if (!response.ok) {
        throw new Error(`载入菜单失败（HTTP ${response.status}）`);
      }
      return data?.items ?? [];
    },
  });

  const menus = query.data ?? [];
  const current = menus.find((menu) => menu.id === selectedId) ?? menus[0];

  const saveMenu = useMutation({
    mutationFn: async (payload: { id: number | null; body: MenuForm }) => {
      if (payload.id) {
        return runMutation(
          () =>
            api.PUT("/api/v1/console/menus/{id}", {
              params: { path: { id: payload.id as number } },
              body: payload.body,
            }),
          { success: "菜单已保存", invalidate: ["menus"] },
        );
      }
      return runMutation(
        () => api.POST("/api/v1/console/menus", { body: payload.body }),
        { success: "菜单已创建", invalidate: ["menus"] },
      );
    },
  });

  const removeMenu = useMutation({
    mutationFn: (id: number) =>
      runMutation(
        () =>
          api.DELETE("/api/v1/console/menus/{id}", {
            params: { path: { id } },
          }),
        { success: "菜单已删除", invalidate: ["menus"] },
      ),
  });

  function openCreate(preset: MenuForm = EMPTY_MENU) {
    setEditingId(null);
    setForm(preset);
    setFormError("");
    setFormOpen(true);
  }

  function openRename(menu: Menu) {
    setEditingId(menu.id);
    setForm({
      name: menu.name,
      slug: menu.slug,
      description: menu.description,
    });
    setFormError("");
    setFormOpen(true);
  }

  return (
    <>
      <PageHeader
        icon={ListTree}
        title="菜单"
        description="前台导航用的条目树。站内条目的地址在读取时解析，记录改名后自动跟随"
        actions={
          editable ? (
            <Button variant="primary" onClick={() => openCreate()}>
              <Plus aria-hidden="true" />
              新建菜单
            </Button>
          ) : null
        }
      />

      <PageBody>
        {query.isLoading ? (
          <Card className="flex flex-col md:flex-row" aria-busy="true">
            <div className="divide-y divide-line border-line border-b md:w-72 md:shrink-0 md:border-r md:border-b-0">
              <EntitySkeleton />
              <EntitySkeleton />
            </div>
            <div className="flex min-w-0 flex-1 flex-col gap-4 p-4">
              <Skeleton className="h-8 w-48" />
              <Skeleton className="h-40 w-full" />
            </div>
          </Card>
        ) : query.error ? (
          <Card>
            <ErrorState
              message={query.error.message}
              onRetry={() => void query.refetch()}
            />
          </Card>
        ) : menus.length === 0 || !current ? (
          <Card>
            <ListEmpty
              icon={ListTree}
              title="还没有菜单"
              description="菜单决定前台的导航结构。主题通常引用一个名为 primary 的菜单。"
              action={
                editable ? (
                  <Button
                    variant="primary"
                    size="sm"
                    onClick={() =>
                      openCreate({
                        name: "主导航",
                        slug: "primary",
                        description: "",
                      })
                    }
                  >
                    新建菜单
                  </Button>
                ) : null
              }
            />
          </Card>
        ) : (
          <Card className="flex flex-col overflow-hidden md:flex-row">
            <div className="border-line border-b md:w-72 md:shrink-0 md:border-r md:border-b-0">
              <EntityList>
                {menus.map((menu) => {
                  const selected = menu.id === current.id;
                  return (
                    <Entity
                      key={menu.id}
                      selected={selected}
                      className="cursor-pointer"
                      onClick={() => setSelectedId(menu.id)}
                    >
                      <EntityStart>
                        <EntityField
                          title={
                            <button
                              type="button"
                              onClick={() => setSelectedId(menu.id)}
                              aria-current={selected ? "true" : undefined}
                              className="truncate text-left"
                            >
                              {menu.name}
                            </button>
                          }
                          description={
                            <>
                              <code className="token">{menu.slug}</code>
                              <span className="tabular">
                                {menu.itemCount} 个条目
                              </span>
                            </>
                          }
                        />
                      </EntityStart>
                      <EntityEnd>
                        {editable ? (
                          <EntityActions label={`菜单 ${menu.name} 的操作`}>
                            <DropdownMenuItem onSelect={() => openRename(menu)}>
                              <Pencil aria-hidden="true" />
                              重命名
                            </DropdownMenuItem>
                            <DropdownMenuSeparator />
                            <DropdownMenuItem
                              danger
                              onSelect={() => setDeleting(menu)}
                            >
                              <Trash2 aria-hidden="true" />
                              删除
                            </DropdownMenuItem>
                          </EntityActions>
                        ) : null}
                      </EntityEnd>
                    </Entity>
                  );
                })}
              </EntityList>
            </div>

            <div className="min-w-0 flex-1">
              {/* key 让切换菜单时编辑器整体重建，草稿不会串到另一个菜单上 */}
              <ItemTreeEditor
                key={current.id}
                menu={current}
                editable={editable}
              />
            </div>
          </Card>
        )}
      </PageBody>

      <Dialog open={formOpen} onOpenChange={setFormOpen}>
        <DialogContent size="sm">
          <form
            onSubmit={async (event) => {
              event.preventDefault();
              setFormError("");
              if (!form.name.trim()) {
                setFormError("名称不能为空");
                return;
              }
              try {
                const saved = await saveMenu.mutateAsync({
                  id: editingId,
                  body: {
                    name: form.name.trim(),
                    slug: form.slug.trim(),
                    description: form.description.trim(),
                  },
                });
                if (!editingId && saved && typeof saved === "object") {
                  const created = saved as { id?: number };
                  if (created.id) {
                    setSelectedId(created.id);
                  }
                }
                setFormOpen(false);
              } catch {
                // 已播报
              }
            }}
          >
            <DialogHeader>
              <DialogTitle>{editingId ? "重命名菜单" : "新建菜单"}</DialogTitle>
            </DialogHeader>
            <DialogBody className="flex flex-col gap-4">
              {formError ? <Alert tone="danger">{formError}</Alert> : null}

              <Field>
                <FieldLabel htmlFor="menu-name">名称</FieldLabel>
                <Input
                  id="menu-name"
                  value={form.name}
                  onChange={(e) => setForm({ ...form, name: e.target.value })}
                  autoFocus
                  required
                />
              </Field>

              <Field>
                <FieldLabel htmlFor="menu-slug">主题引用名</FieldLabel>
                <Input
                  id="menu-slug"
                  value={form.slug}
                  onChange={(e) => setForm({ ...form, slug: e.target.value })}
                  placeholder="primary"
                />
                <FieldDescription>
                  主题里用这个值取菜单，如{" "}
                  <code>{'{{ .Find.Menus.Get "primary" }}'}</code>。
                  留空则由名称生成。
                </FieldDescription>
              </Field>

              <Field>
                <FieldLabel htmlFor="menu-desc">描述</FieldLabel>
                <Textarea
                  id="menu-desc"
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
              <Button
                type="submit"
                variant="primary"
                loading={saveMenu.isPending}
              >
                保存
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
        title={`删除菜单「${deleting?.name ?? ""}」？`}
        consequence={
          <p>
            <strong className="font-medium text-ink">这一步无法撤销。</strong>
            菜单下的 {deleting?.itemCount ?? 0} 个条目会一并删除。
            若主题正在引用它（引用名 <code>{deleting?.slug}</code>），
            前台导航会变空。
          </p>
        }
        confirmLabel="删除"
        pending={removeMenu.isPending}
        onConfirm={async () => {
          if (!deleting) {
            return;
          }
          await removeMenu.mutateAsync(deleting.id).catch(() => {});
          if (deleting.id === selectedId) {
            setSelectedId(null);
          }
          setDeleting(null);
        }}
      />
    </>
  );
}

/** 条目树编辑器：右栏。顶部是菜单名与动作，下面是可嵌套的条目卡片。 */
function ItemTreeEditor({ menu, editable }: { menu: Menu; editable: boolean }) {
  /*
   * 条目要单独取：菜单列表接口只回 itemCount，不带 items（列表里带全部条目树会让
   * 十个菜单的列表拖着几百个条目）。数据到达后只填一次；之后服务端再变
   * 也不悄悄覆盖正在编辑的树，要同步由用户点「放弃修改」触发。
   */
  const itemsQuery = useQuery({
    queryKey: ["menus", menu.id, "items"],
    queryFn: async () => {
      const { data, response } = await api.GET(
        "/api/v1/console/menus/{id}/items",
        { params: { path: { id: menu.id } } },
      );
      if (!response.ok) {
        throw new Error(`载入条目失败（HTTP ${response.status}）`);
      }
      return data?.items ?? [];
    },
  });
  const [items, setItems] = useState<DraftItem[]>([]);
  const [filledFor, setFilledFor] = useState<number | null>(null);
  const [errors, setErrors] = useState<string[]>([]);
  const [dirty, setDirty] = useState(false);

  if (itemsQuery.data && filledFor !== menu.id) {
    setItems(itemsQuery.data.map(toDraft));
    setFilledFor(menu.id);
  }

  const total = useMemo(() => countItems(items), [items]);

  const save = useMutation({
    mutationFn: () =>
      runMutation(
        () =>
          api.PUT("/api/v1/console/menus/{id}/items", {
            params: { path: { id: menu.id } },
            body: { items: toInput(items) },
          }),
        { success: "菜单条目已保存", invalidate: ["menus"] },
      ),
    onSuccess: () => setDirty(false),
  });

  function validate(): string[] {
    const problems: string[] = [];
    if (total > MAX_ITEMS) {
      problems.push(`条目总数 ${total} 超过上限 ${MAX_ITEMS}`);
    }
    const walk = (list: DraftItem[], depth: number) => {
      for (const item of list) {
        if (depth > MAX_DEPTH) {
          problems.push(
            `「${item.label || "未命名"}」的层级超过 ${MAX_DEPTH} 级`,
          );
        }
        if (!item.label.trim()) {
          problems.push("有条目没有填写标题");
        }
        if (item.type === "custom" && !item.url.trim()) {
          problems.push(
            `「${item.label || "未命名"}」是自定义链接，但没有填地址`,
          );
        }
        if (item.type !== "custom" && !item.targetId) {
          problems.push(`「${item.label || "未命名"}」还没有选择要指向的内容`);
        }
        walk(item.children, depth + 1);
      }
    };
    walk(items, 1);
    return problems;
  }

  /** 在树里按 key 定位并替换/删除。 */
  function updateTree(
    list: DraftItem[],
    key: string,
    mutate: (item: DraftItem) => DraftItem | null,
  ): DraftItem[] {
    const out: DraftItem[] = [];
    for (const item of list) {
      if (item.key === key) {
        const next = mutate(item);
        if (next) {
          out.push(next);
        }
        continue;
      }
      out.push({ ...item, children: updateTree(item.children, key, mutate) });
    }
    return out;
  }

  function change(next: (prev: DraftItem[]) => DraftItem[]) {
    setItems(next);
    setDirty(true);
  }

  function patch(key: string, changes: Partial<DraftItem>) {
    change((prev) =>
      updateTree(prev, key, (item) => ({ ...item, ...changes })),
    );
  }

  function remove(key: string) {
    change((prev) => updateTree(prev, key, () => null));
  }

  function addChild(key: string) {
    change((prev) =>
      updateTree(prev, key, (item) => ({
        ...item,
        children: [...item.children, emptyDraft()],
      })),
    );
  }

  function move(key: string, direction: -1 | 1) {
    change((prev) => moveInTree(prev, key, direction));
  }

  /**
   * 接收某一层级拖动后的新顺序。
   *
   * 只按 key 重新排，不用 Reorder 给的那组对象：拖拽过程中树本身没变，
   * 但期间任何一次 setState 都会让这些对象变成上一次渲染的快照，
   * 直接采用会把那次编辑悄悄丢掉。长度对不上就整层不动，宁可这次拖动不生效。
   */
  function setLevelOrder(parentKey: string | null, orderedKeys: string[]) {
    const reorder = (level: DraftItem[]): DraftItem[] => {
      const byKey = new Map(level.map((item) => [item.key, item]));
      const next = orderedKeys
        .map((key) => byKey.get(key))
        .filter((item): item is DraftItem => item !== undefined);
      return next.length === level.length ? next : level;
    };
    if (parentKey === null) {
      change(reorder);
      return;
    }
    change((prev) =>
      updateTree(prev, parentKey, (item) => ({
        ...item,
        children: reorder(item.children),
      })),
    );
  }

  function renderLevel(
    list: DraftItem[],
    depth: number,
    parentKey: string | null,
  ): React.ReactNode {
    return (
      <Reorder.Group
        as="ul"
        axis="y"
        values={list}
        onReorder={(next) =>
          setLevelOrder(
            parentKey,
            next.map((item) => (item as DraftItem).key),
          )
        }
        className={cn("flex flex-col gap-2", depth > 1 && "mt-2 ml-6")}
      >
        {list.map((item, index) => (
          <ItemRow
            key={item.key}
            item={item}
            depth={depth}
            editable={editable}
            // 首个同级条目没有可依附的对象；顶层条目也无从「提升」
            canIndent={depth < MAX_DEPTH && index > 0}
            canOutdent={depth > 1}
            onChange={(changes) => patch(item.key, changes)}
            onRemove={() => remove(item.key)}
            onAddChild={() => addChild(item.key)}
            onMove={(direction) => move(item.key, direction)}
            onIndent={() => change((prev) => indentInTree(prev, item.key))}
            onOutdent={() => change((prev) => outdentInTree(prev, item.key))}
          >
            {item.children.length > 0
              ? renderLevel(item.children, depth + 1, item.key)
              : null}
          </ItemRow>
        ))}
      </Reorder.Group>
    );
  }

  return (
    <div className="flex flex-col">
      <div className="flex flex-wrap items-center justify-between gap-3 border-line border-b px-4 py-3">
        <div className="flex min-w-0 flex-col">
          <h2 className="truncate text-lg font-semibold text-ink">
            {menu.name}
          </h2>
          <p className="tabular text-xs text-ink-muted">
            {total} / {MAX_ITEMS} 条，最多 {MAX_DEPTH} 级
            {dirty ? "，有未保存的修改" : ""}
          </p>
        </div>
        {editable ? (
          <div className="flex flex-wrap items-center gap-2">
            <Button
              variant="secondary"
              size="sm"
              disabled={total >= MAX_ITEMS}
              onClick={() => change((prev) => [...prev, emptyDraft()])}
            >
              <Plus aria-hidden="true" />
              添加顶层条目
            </Button>
            <Button
              variant="ghost"
              size="sm"
              disabled={save.isPending || !dirty}
              onClick={() => {
                setItems((itemsQuery.data ?? []).map(toDraft));
                setErrors([]);
                setDirty(false);
              }}
            >
              放弃修改
            </Button>
            <Button
              variant="primary"
              size="sm"
              loading={save.isPending}
              onClick={async () => {
                const problems = validate();
                setErrors(problems);
                if (problems.length > 0) {
                  return;
                }
                try {
                  await save.mutateAsync();
                } catch {
                  // 已播报
                }
              }}
            >
              保存菜单
            </Button>
          </div>
        ) : (
          <p className="text-xs text-ink-muted">
            你没有 menus:manage 权限，只能查看。
          </p>
        )}
      </div>

      <div className="flex flex-col gap-4 p-4">
        {errors.length > 0 ? (
          <Alert tone="danger" title={`有 ${errors.length} 处需要修改`}>
            <ul className="flex flex-col gap-0.5">
              {errors.map((message) => (
                <li key={message}>{message}</li>
              ))}
            </ul>
          </Alert>
        ) : null}

        {itemsQuery.isLoading ? (
          <div className="flex flex-col gap-2" aria-busy="true">
            <Skeleton className="h-24 w-full" />
            <Skeleton className="h-24 w-full" />
          </div>
        ) : itemsQuery.error ? (
          <ErrorState
            message={itemsQuery.error.message}
            onRetry={() => void itemsQuery.refetch()}
          />
        ) : items.length === 0 ? (
          <div className="rounded-control border border-line-strong border-dashed px-4 py-10 text-center">
            <p className="text-sm text-ink-muted">
              这个菜单还没有条目。加一条，它就会出现在前台导航里。
            </p>
          </div>
        ) : (
          renderLevel(items, 1, null)
        )}
      </div>
    </div>
  );
}

/** 单个条目。站内条目要选目标，自定义链接要填地址 —— 两者的表单不同。 */
function ItemRow({
  item,
  depth,
  editable,
  canIndent,
  canOutdent,
  onChange,
  onRemove,
  onAddChild,
  onMove,
  onIndent,
  onOutdent,
  children,
}: {
  item: DraftItem;
  depth: number;
  editable: boolean;
  canIndent: boolean;
  canOutdent: boolean;
  onChange: (changes: Partial<DraftItem>) => void;
  onRemove: () => void;
  onAddChild: () => void;
  onMove: (direction: -1 | 1) => void;
  onIndent: () => void;
  onOutdent: () => void;
  children?: React.ReactNode;
}) {
  // 拖动只从手柄发起：这一行里有输入框，整行可拖会把选词与光标一起抢走
  const controls = useDragControls();

  return (
    <Reorder.Item
      as="li"
      value={item}
      dragListener={false}
      dragControls={controls}
      className="list-none"
    >
      <Inset>
        <div className="flex flex-wrap items-start gap-2">
          {/*
            手柄做成按钮：它要能聚焦、能用键盘排序。
            这里不是「图标按钮」，是一个可拖的把手 —— 光标形状必须跟着变，
            否则站长不会想到这东西能拖。
          */}
          <div className="flex flex-col items-center gap-0.5 pt-0.5">
            <button
              type="button"
              disabled={!editable}
              onPointerDown={(event) => {
                if (editable) {
                  controls.start(event);
                }
              }}
              onKeyDown={(event) => {
                if (event.key === "ArrowUp") {
                  event.preventDefault();
                  onMove(-1);
                } else if (event.key === "ArrowDown") {
                  event.preventDefault();
                  onMove(1);
                }
              }}
              aria-label={`拖动「${item.label || "未命名"}」调整顺序，按上下方向键也可以`}
              title="拖动排序（聚焦后用 ↑ ↓ 也行）"
              className="transition-ui flex size-6 cursor-grab touch-none items-center justify-center rounded-control text-ink-subtle hover:bg-surface-active hover:text-ink active:cursor-grabbing disabled:cursor-default disabled:opacity-40"
            >
              <GripVertical aria-hidden="true" className="size-4" />
            </button>
          </div>

          <div className="grid min-w-0 flex-1 gap-2 sm:grid-cols-[1fr_10rem]">
            <div className="flex flex-col gap-1">
              <label className="sr-only" htmlFor={`label-${item.key}`}>
                条目标题
              </label>
              <Input
                id={`label-${item.key}`}
                value={item.label}
                disabled={!editable}
                placeholder="显示在导航上的文字"
                onChange={(e) => onChange({ label: e.target.value })}
              />
            </div>

            <div className="flex flex-col gap-1">
              <label className="sr-only" htmlFor={`type-${item.key}`}>
                条目类型
              </label>
              <Select
                value={item.type}
                disabled={!editable}
                onValueChange={(value) =>
                  // 换类型时把另一套字段清空：留着旧值会让服务端
                  // 同时收到 url 与 targetId，而它只认其中一套。
                  onChange({
                    type: value as DraftItem["type"],
                    targetId: null,
                    url: "",
                  })
                }
              >
                <SelectTrigger id={`type-${item.key}`}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {Object.entries(TYPE_META).map(([type, meta]) => (
                    <SelectItem key={type} value={type}>
                      {meta.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            {item.type === "custom" ? (
              <div className="sm:col-span-2">
                <label className="sr-only" htmlFor={`url-${item.key}`}>
                  链接地址
                </label>
                <Input
                  id={`url-${item.key}`}
                  value={item.url}
                  disabled={!editable}
                  placeholder="https://example.com 或 /about"
                  onChange={(e) => onChange({ url: e.target.value })}
                />
              </div>
            ) : (
              <TargetPicker
                item={item}
                editable={editable}
                onChange={onChange}
              />
            )}
          </div>

          {editable ? (
            <div className="flex items-center gap-0.5">
              {/*
              层级用按钮改，不用拖。跨级拖动要判断「拖进谁的下面、落在第几个」，
              拖歪了没有撤销栈可退；按钮则是「成为上一项的子条目 / 回到上一级」，
              结果唯一。
            */}
              <Button
                variant="ghost"
                size="icon-sm"
                disabled={!canIndent}
                onClick={onIndent}
                aria-label={`把「${item.label || "未命名"}」缩进为上一项的子条目`}
                title="缩进（成为上一项的子条目）"
              >
                <IndentIncrease aria-hidden="true" />
              </Button>
              <Button
                variant="ghost"
                size="icon-sm"
                disabled={!canOutdent}
                onClick={onOutdent}
                aria-label={`把「${item.label || "未命名"}」提升到上一级`}
                title="提升（回到上一级）"
              >
                <IndentDecrease aria-hidden="true" />
              </Button>
              {depth < MAX_DEPTH ? (
                <Button
                  variant="ghost"
                  size="icon-sm"
                  onClick={onAddChild}
                  aria-label={`在「${item.label || "未命名"}」下加子条目`}
                  title="加子条目"
                >
                  <Plus aria-hidden="true" />
                </Button>
              ) : null}
              <Button
                variant="ghost"
                size="icon-sm"
                onClick={onRemove}
                aria-label={`删除条目「${item.label || "未命名"}」`}
                title="删除"
                className="hover:text-danger"
              >
                <Trash2 aria-hidden="true" />
              </Button>
            </div>
          ) : null}
        </div>

        <div className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1 pl-7">
          {/*
          开关用显式 id + htmlFor 关联，而不是把控件包在 label 里。
          Radix 的 Switch 渲染的是 button 而非 input，包在 label 里时
          「标签与控件是否关联」在 DOM 上看不出来。
        */}
          <div className="flex items-center gap-2 text-xs text-ink-muted">
            <Switch
              id={`visible-${item.key}`}
              checked={item.visible}
              disabled={!editable}
              onCheckedChange={(checked) => onChange({ visible: checked })}
            />
            <label
              htmlFor={`visible-${item.key}`}
              className="cursor-pointer select-none"
            >
              前台可见
            </label>
          </div>

          <CheckboxRow
            id={`blank-${item.key}`}
            checked={item.target === "_blank"}
            disabled={!editable}
            onCheckedChange={(checked) =>
              onChange({ target: checked ? "_blank" : "" })
            }
            label={<span className="text-xs text-ink-muted">新窗口打开</span>}
            className="px-0 py-0 hover:bg-transparent"
          />

          {item.target === "_blank" ? (
            <div className="flex items-center gap-2 text-xs text-ink-muted">
              <label htmlFor={`rel-${item.key}`} className="whitespace-nowrap">
                rel
              </label>
              <Input
                id={`rel-${item.key}`}
                value={item.rel}
                disabled={!editable}
                placeholder="noopener noreferrer"
                onChange={(e) => onChange({ rel: e.target.value })}
                className="h-7 w-48 text-xs"
              />
              <span className="whitespace-nowrap text-ink-subtle">
                留空时前台自动补 noopener
              </span>
            </div>
          ) : null}

          {item.type !== "custom" && item.targetId ? (
            <span className="flex items-center gap-1 text-xs text-ink-subtle">
              <ExternalLink aria-hidden="true" className="size-3" />
              地址由系统解析
            </span>
          ) : null}
        </div>
      </Inset>
      {children}
    </Reorder.Item>
  );
}

/**
 * 选择站内目标。
 *
 * 从对应的列表接口拉候选。用「已发布/已存在的记录」而不是自由输入 ID：
 * 站长不知道文章 ID 是多少，而手填一个不存在的 ID 会让条目在前台被静默跳过。
 */
function TargetPicker({
  item,
  editable,
  onChange,
}: {
  item: DraftItem;
  editable: boolean;
  onChange: (changes: Partial<DraftItem>) => void;
}) {
  const [keyword, setKeyword] = useState("");

  const query = useQuery({
    queryKey: ["menu-targets", item.type, keyword],
    enabled: item.type !== "custom",
    queryFn: async () => {
      const q = keyword.trim();
      switch (item.type) {
        case "post": {
          const { data } = await api.GET("/api/v1/console/posts", {
            params: { query: { size: 50, ...(q ? { q } : {}) } },
          });
          return (data?.items ?? []).map((p) => ({ id: p.id, label: p.title }));
        }
        case "page": {
          const { data } = await api.GET("/api/v1/console/pages", {
            params: { query: { size: 50, ...(q ? { q } : {}) } },
          });
          return (data?.items ?? []).map((p) => ({ id: p.id, label: p.title }));
        }
        case "category": {
          const { data } = await api.GET("/api/v1/console/categories", {
            params: { query: { size: 100 } },
          });
          return (data?.items ?? []).map((c) => ({ id: c.id, label: c.name }));
        }
        case "tag": {
          const { data } = await api.GET("/api/v1/console/tags", {
            params: { query: { size: 100, ...(q ? { q } : {}) } },
          });
          return (data?.items ?? []).map((t) => ({ id: t.id, label: t.name }));
        }
        default:
          return [];
      }
    },
  });

  const options = query.data ?? [];

  return (
    <div className="flex flex-col gap-1 sm:col-span-2">
      <div className="flex flex-wrap items-center gap-2">
        {/* 文章与页面可能上百条，给一个关键词框缩小范围 */}
        {item.type === "post" || item.type === "page" || item.type === "tag" ? (
          <Input
            value={keyword}
            disabled={!editable}
            placeholder="筛选"
            aria-label="筛选可选内容"
            onChange={(e) => setKeyword(e.target.value)}
            className="h-9 w-40"
          />
        ) : null}

        <Select
          value={item.targetId ? String(item.targetId) : ""}
          disabled={!editable}
          onValueChange={(value) => onChange({ targetId: Number(value) })}
        >
          <SelectTrigger
            aria-label="选择指向的内容"
            className="max-w-md flex-1"
          >
            <SelectValue placeholder="选择要指向的内容" />
          </SelectTrigger>
          <SelectContent>
            {options.length === 0 ? (
              <div className="px-2 py-3 text-xs text-ink-muted">
                {query.isLoading
                  ? "正在载入"
                  : `没有可选的${TYPE_META[item.type]?.label ?? "内容"}。先创建一条。`}
              </div>
            ) : (
              options.map((option) => (
                <SelectItem key={option.id} value={String(option.id)}>
                  {option.label || "（无标题）"}
                </SelectItem>
              ))
            )}
          </SelectContent>
        </Select>
      </div>
      <FieldDescription>{TYPE_META[item.type]?.hint}</FieldDescription>
    </div>
  );
}

/** 在树里上下移动某一项（只在其所在层级内移动）。 */
function moveInTree(
  list: DraftItem[],
  key: string,
  direction: -1 | 1,
): DraftItem[] {
  const index = list.findIndex((item) => item.key === key);
  if (index >= 0) {
    const target = index + direction;
    if (target < 0 || target >= list.length) {
      return list;
    }
    const next = [...list];
    const [moved] = next.splice(index, 1);
    next.splice(target, 0, moved as DraftItem);
    return next;
  }
  return list.map((item) => ({
    ...item,
    children: moveInTree(item.children, key, direction),
  }));
}

/**
 * 把条目缩进为**上一个同级条目**的子项。
 *
 * 依附上一个同级而不是「缩进成一个新父级」：菜单里没有中间层这种东西，
 * 能当父级的只有已经存在的条目。首个同级条目没有可依附的对象，原样返回。
 */
function indentInTree(list: DraftItem[], key: string): DraftItem[] {
  const index = list.findIndex((item) => item.key === key);
  if (index > 0) {
    const moved = list[index] as DraftItem;
    const prev = list[index - 1] as DraftItem;
    const next = [...list];
    next.splice(index, 1);
    next[index - 1] = { ...prev, children: [...prev.children, moved] };
    return next;
  }
  return list.map((item) => ({
    ...item,
    children: indentInTree(item.children, key),
  }));
}

/**
 * 把条目提升一级，紧跟在原父条目之后。
 *
 * 「之后」而不是「之前」：提升一项通常是想让它和前父级平级、排在它旁边，
 * 插到父级之前会把它推到父级的父级上面去，越提升越靠前，很快就找不到东西了。
 */
function outdentInTree(list: DraftItem[], key: string): DraftItem[] {
  const index = list.findIndex((item) =>
    item.children.some((child) => child.key === key),
  );
  if (index >= 0) {
    const parent = list[index] as DraftItem;
    const moved = parent.children.find((child) => child.key === key);
    if (!moved) {
      return list;
    }
    const next = [...list];
    next[index] = {
      ...parent,
      children: parent.children.filter((child) => child.key !== key),
    };
    next.splice(index + 1, 0, moved);
    return next;
  }
  return list.map((item) => ({
    ...item,
    children: outdentInTree(item.children, key),
  }));
}
