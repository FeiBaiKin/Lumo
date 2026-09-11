import { api, problemMessage } from "@/api/client";
import { runMutation } from "@/api/mutation";
import type { components } from "@/api/schema";
import { ListEmpty, ListPanel } from "@/components/data/list-panel";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  ConfirmDialog,
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
} from "@/components/ui/field";
import { Input, Textarea } from "@/components/ui/input";
import { PageHeader } from "@/components/ui/panel";
import { ErrorState, Skeleton } from "@/components/ui/states";
import { useDocumentTitle } from "@/lib/use-document-title";
import { cn } from "@/lib/utils";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Lock, Pencil, Plus, ShieldCheck, Trash2 } from "lucide-react";
import { useMemo, useState } from "react";

/**
 * 角色管理。
 *
 * 角色编辑器由**服务端的权限清单**驱动（`/permissions`），不是前端写死的勾选框。
 * 这一点是刻意的：权限串由各模块声明并随版本演进，前端写死一份就会
 * 在新增权限时静默漏掉它 —— 而「角色编辑器里看不到某条权限」
 * 会让站长以为那条权限不存在。
 *
 * 清单按**资源**分组。检索的 UX 准则里有一条：一屏几十个勾选框会让人
 * 无从下手；按资源分组后，每一组都是「这个角色能不能碰这一类东西」，
 * 是可以逐组决策的。
 *
 * 内置角色不可改删（每次启动以代码为准播种），故它们的勾选框是只读的，
 * 并明确标出原因 —— 一个点了没反应的复选框比一个禁用的复选框更让人困惑。
 */

type Role = components["schemas"]["Role"];
type PermissionView = components["schemas"]["PermissionView"];

const RESOURCE_LABELS: Record<string, string> = {
  posts: "文章",
  pages: "页面",
  taxonomies: "分类与标签",
  comments: "评论",
  media: "附件",
  menus: "菜单",
  users: "用户",
  roles: "角色",
  themes: "主题",
  settings: "设置",
  extensions: "扩展记录",
  site: "站点",
};

export function RolesPage() {
  useDocumentTitle("角色");

  const [editing, setEditing] = useState<Role | null>(null);
  const [creating, setCreating] = useState(false);
  const [deleting, setDeleting] = useState<Role | null>(null);

  const rolesQuery = useQuery({
    queryKey: ["roles"],
    queryFn: async () => {
      const { data, response } = await api.GET("/api/v1/console/roles");
      if (!response.ok) {
        throw new Error(`载入角色失败（HTTP ${response.status}）`);
      }
      return data?.items ?? [];
    },
  });

  const permsQuery = useQuery({
    queryKey: ["permissions"],
    // 权限清单不常变，缓存久一点
    staleTime: 5 * 60_000,
    queryFn: async () => {
      const { data, response } = await api.GET("/api/v1/console/permissions");
      if (!response.ok) {
        throw new Error(`载入权限清单失败（HTTP ${response.status}）`);
      }
      return data?.items ?? [];
    },
  });

  const roles = rolesQuery.data ?? [];
  const permissions = permsQuery.data ?? [];

  if (rolesQuery.error) {
    return (
      <>
        <PageHeader title="角色" />
        <ErrorState
          message={rolesQuery.error.message}
          onRetry={() => void rolesQuery.refetch()}
        />
      </>
    );
  }

  return (
    <>
      <PageHeader
        title="角色"
        description="角色即一组权限串。内置角色随代码播种，不可修改；自定义角色可以自由组合"
        actions={
          <Button
            variant="primary"
            disabled={permissions.length === 0}
            onClick={() => setCreating(true)}
          >
            <Plus aria-hidden="true" />
            新建角色
          </Button>
        }
      />

      {rolesQuery.isLoading || permsQuery.isLoading ? (
        <div className="flex flex-col gap-3">
          <Skeleton className="h-28 w-full" />
          <Skeleton className="h-28 w-full" />
        </div>
      ) : roles.length === 0 ? (
        <ListPanel>
          <ListEmpty
            icon={ShieldCheck}
            title="没有角色"
            description="这不太正常 —— 内置角色应当随启动播种。请检查服务端日志。"
          />
        </ListPanel>
      ) : (
        <div className="flex flex-col gap-4">
          {roles.map((role) => (
            <RoleCard
              key={role.id}
              role={role}
              permissions={permissions}
              onEdit={() => setEditing(role)}
              onDelete={() => setDeleting(role)}
            />
          ))}
        </div>
      )}

      <RoleDialog
        open={creating || editing !== null}
        role={editing}
        permissions={permissions}
        onClose={() => {
          setCreating(false);
          setEditing(null);
        }}
        onDone={() => void rolesQuery.refetch()}
      />

      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
        title={`删除角色「${deleting?.label || deleting?.name}」？`}
        consequence={
          <p>
            <strong className="font-medium text-ink">这一步无法撤销。</strong>
            持有这个角色的用户会失去它带来的权限。
            若仍有用户持有它，删除会被拒绝 —— 那样应该先把用户改到别的角色。
          </p>
        }
        confirmLabel="删除"
        onConfirm={async () => {
          if (!deleting) {
            return;
          }
          await runMutation(
            () =>
              api.DELETE("/api/v1/console/roles/{id}", {
                params: { path: { id: deleting.id } },
              }),
            { success: "角色已删除", invalidate: ["roles"] },
          ).catch(() => {});
          setDeleting(null);
        }}
      />
    </>
  );
}

/** 一个角色：元信息 + 按资源分组的权限点。 */
function RoleCard({
  role,
  permissions,
  onEdit,
  onDelete,
}: {
  role: Role;
  permissions: PermissionView[];
  onEdit: () => void;
  onDelete: () => void;
}) {
  const grouped = useMemo(
    () => groupByResource(permissions, role.permissions ?? []),
    [permissions, role.permissions],
  );

  return (
    <ListPanel
      title={role.label || role.name}
      description={role.description || `标识 ${role.name}`}
      badge={
        role.builtin ? (
          <Badge tone="outline">
            <Lock aria-hidden="true" />
            内置
          </Badge>
        ) : (
          <Badge tone="seal">自定义</Badge>
        )
      }
      actions={
        role.builtin ? (
          // 内置角色：说清为什么不能改，而不是给一个灰按钮
          <span className="text-xs text-ink-muted">
            内置角色不可修改，每次启动以代码为准播种
          </span>
        ) : (
          <>
            <Button variant="ghost" size="sm" onClick={onEdit}>
              <Pencil aria-hidden="true" />
              编辑
            </Button>
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={onDelete}
              aria-label={`删除角色 ${role.label || role.name}`}
              className="hover:text-danger"
            >
              <Trash2 aria-hidden="true" />
            </Button>
          </>
        )
      }
    >
      <div className="flex flex-col gap-3 p-4">
        {grouped.length === 0 ? (
          <p className="text-sm text-ink-muted">这个角色还没有任何权限。</p>
        ) : (
          grouped.map((group) => (
            <div
              key={group.resource}
              className="flex flex-wrap items-center gap-1.5"
            >
              <span className="w-24 shrink-0 text-sm text-ink-muted">
                {RESOURCE_LABELS[group.resource] ?? group.resource}
              </span>
              <div className="flex flex-wrap gap-1">
                {group.items.map((item) => (
                  <Badge key={item.key} tone="neutral" title={item.description}>
                    {item.label}
                  </Badge>
                ))}
              </div>
            </div>
          ))
        )}
      </div>
    </ListPanel>
  );
}

type PermissionGroup = { resource: string; items: PermissionView[] };

function groupByResource(
  permissions: PermissionView[],
  held: string[],
): PermissionGroup[] {
  const set = new Set(held);
  const map = new Map<string, PermissionView[]>();
  for (const permission of permissions) {
    if (!set.has(permission.key)) {
      continue;
    }
    const list = map.get(permission.resource) ?? [];
    list.push(permission);
    map.set(permission.resource, list);
  }
  return [...map.entries()]
    .map(([resource, items]) => ({ resource, items }))
    .sort((a, b) => a.resource.localeCompare(b.resource));
}

/** 新建 / 编辑角色。权限按资源分组勾选。 */
function RoleDialog({
  open,
  role,
  permissions,
  onClose,
  onDone,
}: {
  open: boolean;
  role: Role | null;
  permissions: PermissionView[];
  onClose: () => void;
  onDone: () => void;
}) {
  const [name, setName] = useState("");
  const [label, setLabel] = useState("");
  const [description, setDescription] = useState("");
  const [selected, setSelected] = useState<string[]>([]);
  const [error, setError] = useState("");
  const [initialised, setInitialised] = useState<number | null | "new">(null);

  // 打开时重填。用 id（或 "new"）判断，避免列表刷新后覆盖正在编辑的内容。
  const marker: number | "new" = role ? role.id : "new";
  if (open && initialised !== marker) {
    setName(role?.name ?? "");
    setLabel(role?.label ?? "");
    setDescription(role?.description ?? "");
    setSelected(role?.permissions ?? []);
    setError("");
    setInitialised(marker);
  }
  if (!open && initialised !== null) {
    setInitialised(null);
  }

  const groups = useMemo(() => {
    const map = new Map<string, PermissionView[]>();
    for (const permission of permissions) {
      const list = map.get(permission.resource) ?? [];
      list.push(permission);
      map.set(permission.resource, list);
    }
    return [...map.entries()]
      .map(([resource, items]) => ({ resource, items }))
      .sort((a, b) => a.resource.localeCompare(b.resource));
  }, [permissions]);

  const save = useMutation({
    mutationFn: async () => {
      const body = {
        name: name.trim(),
        label: label.trim(),
        description: description.trim(),
        permissions: selected,
      };
      if (role) {
        const { error: problem, response } = await api.PUT(
          "/api/v1/console/roles/{id}",
          {
            params: { path: { id: role.id } },
            body,
          },
        );
        if (!response.ok) {
          throw new Error(problemMessage(problem));
        }
        return;
      }
      const { error: problem, response } = await api.POST(
        "/api/v1/console/roles",
        { body },
      );
      if (!response.ok) {
        throw new Error(problemMessage(problem));
      }
    },
    onSuccess: () => {
      onDone();
      onClose();
    },
    onError: (err) => setError(err instanceof Error ? err.message : "保存失败"),
  });

  function toggle(key: string, checked: boolean) {
    setSelected((prev) =>
      checked ? [...prev, key] : prev.filter((item) => item !== key),
    );
  }

  function toggleGroup(items: PermissionView[], checked: boolean) {
    const keys = items.map((item) => item.key);
    setSelected((prev) =>
      checked
        ? [...new Set([...prev, ...keys])]
        : prev.filter((key) => !keys.includes(key)),
    );
  }

  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="max-w-2xl">
        <form
          onSubmit={(event) => {
            event.preventDefault();
            setError("");
            if (!name.trim()) {
              setError("标识不能为空");
              return;
            }
            save.mutate();
          }}
        >
          <DialogHeader>
            <DialogTitle>
              {role ? `编辑「${role.label || role.name}」` : "新建角色"}
            </DialogTitle>
            <DialogDescription>
              角色是一组权限串的集合。用户可以有多个角色，实际权限是它们的并集。
            </DialogDescription>
          </DialogHeader>

          <DialogBody className="flex flex-col gap-5">
            {error ? (
              <div
                role="alert"
                className="rounded-control border border-danger bg-danger-soft px-3 py-2 text-sm text-danger"
              >
                {error}
              </div>
            ) : null}

            <div className="grid gap-4 sm:grid-cols-2">
              <Field>
                <FieldLabel htmlFor="role-name">标识</FieldLabel>
                <Input
                  id="role-name"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder="editor-assistant"
                  required
                  autoFocus
                  // 标识是权限判定用的键，改了会让既有引用失配
                  disabled={role !== null}
                />
                <FieldDescription>
                  {role ? "标识创建后不可更改。" : "小写字母、数字与连字符。"}
                </FieldDescription>
              </Field>

              <Field>
                <FieldLabel htmlFor="role-label">显示名</FieldLabel>
                <Input
                  id="role-label"
                  value={label}
                  onChange={(e) => setLabel(e.target.value)}
                  placeholder="助理编辑"
                />
              </Field>
            </div>

            <Field>
              <FieldLabel htmlFor="role-desc">描述</FieldLabel>
              <Textarea
                id="role-desc"
                rows={2}
                value={description}
                onChange={(e) => setDescription(e.target.value)}
              />
            </Field>

            <fieldset className="flex flex-col gap-3">
              <legend className="flex w-full items-center justify-between gap-2 text-sm font-medium text-ink">
                <span>权限</span>
                <span className="tabular text-xs text-ink-muted">
                  已选 {selected.length} / {permissions.length}
                </span>
              </legend>

              {groups.map((group) => {
                const keys = group.items.map((item) => item.key);
                const allChecked = keys.every((key) => selected.includes(key));
                const someChecked = keys.some((key) => selected.includes(key));
                return (
                  <div
                    key={group.resource}
                    className="rounded-panel border border-line bg-surface-raised p-3"
                  >
                    <div className="mb-2 flex items-center gap-2">
                      <input
                        type="checkbox"
                        id={`group-${group.resource}`}
                        checked={allChecked}
                        ref={(el) => {
                          // 部分选中用 indeterminate：它只能经 JS 设置，
                          // 而「这组里选了一半」必须能看出来
                          if (el) {
                            el.indeterminate = someChecked && !allChecked;
                          }
                        }}
                        onChange={(e) =>
                          toggleGroup(group.items, e.target.checked)
                        }
                        className="size-4 cursor-pointer rounded-[3px] border-line-strong accent-seal"
                      />
                      <label
                        htmlFor={`group-${group.resource}`}
                        className="cursor-pointer text-sm font-medium text-ink select-none"
                      >
                        {RESOURCE_LABELS[group.resource] ?? group.resource}
                      </label>
                    </div>

                    <div className="grid gap-1 pl-6 sm:grid-cols-2">
                      {group.items.map((item) => (
                        <label
                          key={item.key}
                          className={cn(
                            "flex cursor-pointer items-start gap-2 rounded-control px-1.5 py-1",
                            "hover:bg-surface-hover",
                          )}
                        >
                          <input
                            type="checkbox"
                            checked={selected.includes(item.key)}
                            onChange={(e) => toggle(item.key, e.target.checked)}
                            className="mt-0.5 size-4 cursor-pointer rounded-[3px] border-line-strong accent-seal"
                          />
                          <span className="flex min-w-0 flex-col">
                            <span className="text-sm text-ink">
                              {item.label}
                            </span>
                            <span className="token text-xs text-ink-subtle">
                              {item.key}
                            </span>
                          </span>
                        </label>
                      ))}
                    </div>
                  </div>
                );
              })}

              {permissions.length === 0 ? (
                <p className="text-sm text-ink-muted">暂时没有可分配的权限。</p>
              ) : null}
            </fieldset>

            <FieldError>{error}</FieldError>
          </DialogBody>

          <DialogFooter>
            <Button variant="secondary" onClick={onClose}>
              取消
            </Button>
            <Button type="submit" variant="primary" disabled={save.isPending}>
              {save.isPending ? "正在保存" : "保存"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
