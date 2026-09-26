import { api, problemMessage } from "@/api/client";
import { runMutation } from "@/api/mutation";
import type { components } from "@/api/schema";
import {
  Entity,
  EntityActions,
  EntityEnd,
  EntityField,
  EntityList,
  EntityMeta,
  EntityStart,
  ListEmpty,
} from "@/components/data/entity";
import { PageBody, PageHeader } from "@/components/layout/page-header";
import { Alert } from "@/components/ui/alert";
import { Avatar } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, Inset } from "@/components/ui/card";
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
  DropdownMenuItem,
  DropdownMenuSeparator,
} from "@/components/ui/dropdown-menu";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";
import { Input, Textarea } from "@/components/ui/input";
import { EntitySkeleton, ErrorState } from "@/components/ui/states";
import { Checkbox, CheckboxRow } from "@/components/ui/toggle";
import { useDocumentTitle } from "@/lib/use-document-title";
import { useMutation, useQuery } from "@tanstack/react-query";
import {
  Eye,
  Lock,
  Pencil,
  Plus,
  RotateCcw,
  ShieldCheck,
  Trash2,
} from "lucide-react";
import { useMemo, useState } from "react";

/**
 * 角色管理。
 *
 * 角色编辑器由**服务端的权限清单**驱动（`/permissions`），不是前端写死的勾选框。
 * 这一点是刻意的：权限串由各模块声明并随版本演进，前端写死一份就会
 * 在新增权限时静默漏掉它 —— 而「角色编辑器里看不到某条权限」
 * 会让站长以为那条权限不存在。资源分组的中文名同理，也由服务端下发。
 *
 * 清单按**资源**分组。一屏几十个勾选框会让人无从下手；按资源分组后，
 * 每一组都是「这个角色能不能碰这一类东西」，是可以逐组决策的。
 *
 * 三挡内置角色（用户 / 编辑 / 管理员）的权限**可以改**（2026-09-15 起）：
 * 改过的会在标题旁标出「已自定义」，并给出「恢复默认」写回代码里的那一套。
 * 超级管理员不在本页出现（`locked`），它不可改、不可删，也就没有可点的编辑入口 ——
 * 一个点了没反应的编辑按钮比没有这个按钮更让人困惑。
 */

type Role = components["schemas"]["Role"];
type PermissionView = components["schemas"]["PermissionView"];

type PermissionGroup = {
  resource: string;
  /** 资源段的中文名，由服务端下发；缺声明时回退到资源标识。 */
  label: string;
  items: PermissionView[];
};

/** 按资源分组；`held` 给出时只保留持有的项。 */
function groupByResource(
  permissions: PermissionView[],
  held?: string[],
): PermissionGroup[] {
  const set = held ? new Set(held) : null;
  const map = new Map<string, PermissionGroup>();
  for (const permission of permissions) {
    if (set && !set.has(permission.key)) {
      continue;
    }
    const group = map.get(permission.resource) ?? {
      resource: permission.resource,
      label: permission.resourceLabel || permission.resource,
      items: [],
    };
    group.items.push(permission);
    map.set(permission.resource, group);
  }
  return [...map.values()].sort((a, b) => a.resource.localeCompare(b.resource));
}

export function RolesPage() {
  useDocumentTitle("角色");

  const [viewing, setViewing] = useState<Role | null>(null);
  const [editing, setEditing] = useState<Role | null>(null);
  const [creating, setCreating] = useState(false);
  const [deleting, setDeleting] = useState<Role | null>(null);
  const [resetting, setResetting] = useState<Role | null>(null);

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
  const loading = rolesQuery.isLoading || permsQuery.isLoading;

  // 锁定的角色（超级管理员）不在这里出现：它不可改也不可删，
  // 列出来只会占一行。用户页仍能把它分配给账号。
  const manageable = roles.filter((role) => !role.locked);

  return (
    <>
      <PageHeader
        icon={ShieldCheck}
        title="角色"
        description="用户、编辑、管理员三挡内置角色的权限可以随时改，也可以另建角色组合出更细的分工"
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

      <PageBody>
        <Card className="overflow-hidden">
          {loading ? (
            <div className="divide-y divide-line" aria-busy="true">
              <span className="sr-only">正在载入</span>
              <EntitySkeleton />
              <EntitySkeleton />
              <EntitySkeleton />
              <EntitySkeleton />
            </div>
          ) : rolesQuery.error ? (
            <ErrorState
              message={rolesQuery.error.message}
              onRetry={() => void rolesQuery.refetch()}
            />
          ) : roles.length === 0 ? (
            <ListEmpty
              icon={ShieldCheck}
              title="没有角色"
              description="这不太正常，内置角色应当随启动播种。请检查服务端日志。"
            />
          ) : (
            <EntityList>
              {manageable.map((role) => (
                <RoleRow
                  key={role.id}
                  role={role}
                  onView={() => setViewing(role)}
                  onEdit={() => setEditing(role)}
                  onReset={() => setResetting(role)}
                  onDelete={() => setDeleting(role)}
                />
              ))}
            </EntityList>
          )}
        </Card>
      </PageBody>

      <PermissionsDialog
        role={viewing}
        permissions={permissions}
        onClose={() => setViewing(null)}
      />

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
            若仍有用户持有它，删除会被拒绝，那样应该先把用户改到别的角色。
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

      <ConfirmDialog
        open={resetting !== null}
        onOpenChange={(open) => !open && setResetting(null)}
        title={`把「${resetting?.label || resetting?.name}」恢复默认？`}
        consequence={
          <p>
            这个角色的权限会写回代码里的那一套，
            <strong className="font-medium text-ink">
              你对它做过的调整会丢失
            </strong>
            。用户不受影响 —— 变的只是角色带哪些权限。
          </p>
        }
        confirmLabel="恢复默认"
        onConfirm={async () => {
          if (!resetting) {
            return;
          }
          const role = resetting;
          await runMutation(
            () =>
              api.POST("/api/v1/console/roles/{id}/reset", {
                params: { path: { id: role.id } },
              }),
            { success: "已恢复默认权限", invalidate: ["roles"] },
          ).catch(() => {});
          setResetting(null);
        }}
      />
    </>
  );
}

/** 一行角色：方形头像取首字，标题旁标出内置或自定义。 */
function RoleRow({
  role,
  onView,
  onEdit,
  onReset,
  onDelete,
}: {
  role: Role;
  onView: () => void;
  onEdit: () => void;
  onReset: () => void;
  onDelete: () => void;
}) {
  const label = role.label || role.name;
  const total = (role.permissions ?? []).length;

  return (
    <Entity>
      <EntityStart>
        <Avatar square name={label} size="sm" />
        <EntityField
          width="max-w-lg"
          title={label}
          extra={
            <>
              {role.builtin ? (
                <Badge tone="outline">
                  <Lock aria-hidden="true" />
                  内置
                </Badge>
              ) : (
                <Badge tone="seal">自定义</Badge>
              )}
              {/* 内置角色的权限被改过：不标出来的话，「恢复默认」这个入口会显得莫名其妙 */}
              {role.customized ? <Badge tone="warn">已自定义</Badge> : null}
            </>
          }
          description={
            <>
              <code className="token">{role.name}</code>
              {role.description ? <span>{role.description}</span> : null}
            </>
          }
        />
      </EntityStart>

      <EntityEnd>
        <EntityMeta>{total === 0 ? "没有权限" : `${total} 项权限`}</EntityMeta>
        <EntityActions label={`角色 ${label} 的操作`}>
          <DropdownMenuItem onSelect={onView}>
            <Eye aria-hidden="true" />
            查看权限
          </DropdownMenuItem>
          <DropdownMenuItem onSelect={onEdit}>
            <Pencil aria-hidden="true" />
            编辑
          </DropdownMenuItem>
          {role.builtin && role.customized ? (
            <DropdownMenuItem onSelect={onReset}>
              <RotateCcw aria-hidden="true" />
              恢复默认
            </DropdownMenuItem>
          ) : null}
          {role.builtin ? null : (
            <>
              <DropdownMenuSeparator />
              <DropdownMenuItem danger onSelect={onDelete}>
                <Trash2 aria-hidden="true" />
                删除
              </DropdownMenuItem>
            </>
          )}
        </EntityActions>
      </EntityEnd>
    </Entity>
  );
}

/** 只读地看某个角色持有哪些权限，按资源分组。 */
function PermissionsDialog({
  role,
  permissions,
  onClose,
}: {
  role: Role | null;
  permissions: PermissionView[];
  onClose: () => void;
}) {
  const grouped = useMemo(
    () => groupByResource(permissions, role?.permissions ?? []),
    [permissions, role],
  );

  return (
    <Dialog open={role !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{role?.label || role?.name}</DialogTitle>
          <DialogDescription>
            {role?.builtin
              ? "内置角色。权限可以改，改过之后行尾菜单里能恢复默认。"
              : "自定义角色，可自由组合权限。"}
          </DialogDescription>
        </DialogHeader>
        <DialogBody className="flex flex-col gap-3">
          {grouped.length === 0 ? (
            <p className="py-6 text-center text-sm text-ink-muted">
              这个角色还没有任何权限。
            </p>
          ) : (
            grouped.map((group) => (
              <div
                key={group.resource}
                className="flex flex-wrap items-start gap-2"
              >
                <span className="w-24 shrink-0 pt-0.5 text-sm text-ink-muted">
                  {group.label}
                </span>
                <div className="flex min-w-0 flex-1 flex-wrap gap-1">
                  {group.items.map((item) => (
                    <Badge
                      key={item.key}
                      tone={item.dangerous ? "danger" : "neutral"}
                      title={item.description}
                    >
                      {item.label}
                      {item.dangerous ? (
                        <span className="opacity-80">高危</span>
                      ) : null}
                    </Badge>
                  ))}
                </div>
              </div>
            ))
          )}
        </DialogBody>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>
            关闭
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
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

  const groups = useMemo(() => groupByResource(permissions), [permissions]);

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
      <DialogContent size="lg">
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
              角色是一组权限的集合。用户可以有多个角色，实际权限是它们的并集。
              内置角色改过之后，行尾菜单里能恢复默认。
            </DialogDescription>
          </DialogHeader>

          <DialogBody className="flex flex-col gap-5">
            {error ? (
              <Alert tone="danger" title="没有保存">
                {error}
              </Alert>
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
              <legend className="flex w-full items-center justify-between gap-2 pb-2 text-sm font-medium text-ink">
                <span>权限</span>
                <span className="tabular text-xs text-ink-muted">
                  已选 {selected.length} / {permissions.length}
                </span>
              </legend>

              {groups.map((group) => {
                const keys = group.items.map((item) => item.key);
                const allChecked = keys.every((key) => selected.includes(key));
                const someChecked = keys.some((key) => selected.includes(key));
                const groupId = `group-${group.resource}`;
                return (
                  <Inset key={group.resource} className="flex flex-col gap-1">
                    {/* 组头：全选与半选。「这组里选了一半」必须能看出来 */}
                    <label
                      htmlFor={groupId}
                      className="flex cursor-pointer items-center gap-2.5 px-2 py-1 text-sm font-medium text-ink select-none"
                    >
                      <Checkbox
                        id={groupId}
                        checked={
                          allChecked
                            ? true
                            : someChecked
                              ? "indeterminate"
                              : false
                        }
                        onCheckedChange={(value) =>
                          toggleGroup(group.items, value === true)
                        }
                      />
                      {group.label}
                    </label>
                    <div className="grid gap-0.5 sm:grid-cols-2">
                      {group.items.map((item) => (
                        <CheckboxRow
                          key={item.key}
                          id={`perm-${item.key}`}
                          checked={selected.includes(item.key)}
                          onCheckedChange={(checked) =>
                            toggle(item.key, checked)
                          }
                          label={
                            <span className="flex items-center gap-1.5">
                              {item.label}
                              {item.dangerous ? (
                                <Badge tone="danger">高危</Badge>
                              ) : null}
                            </span>
                          }
                          // 中文说明来自服务端（各模块声明）。用英文权限串当描述的话，
                          // 站长勾选时并不知道这一条到底给了对方什么，尤其是带 _any 的那些。
                          description={item.description || item.key}
                        />
                      ))}
                    </div>
                  </Inset>
                );
              })}

              {permissions.length === 0 ? (
                <p className="text-sm text-ink-muted">暂时没有可分配的权限。</p>
              ) : null}
            </fieldset>
          </DialogBody>

          <DialogFooter>
            <Button variant="secondary" onClick={onClose}>
              取消
            </Button>
            <Button type="submit" variant="primary" loading={save.isPending}>
              保存
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
