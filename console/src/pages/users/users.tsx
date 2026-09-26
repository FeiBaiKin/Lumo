import { api, problemMessage } from "@/api/client";
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
  FilterMenu,
  ListBody,
  ListEmpty,
  ListToolbar,
} from "@/components/data/entity";
import { PageBody, PageHeader } from "@/components/layout/page-header";
import { Alert } from "@/components/ui/alert";
import { Avatar } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardHeader } from "@/components/ui/card";
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
import {
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
} from "@/components/ui/field";
import { Input, SearchInput, Textarea } from "@/components/ui/input";
import { Pagination } from "@/components/ui/pagination";
import { StatusDot } from "@/components/ui/status-dot";
import { CheckboxRow } from "@/components/ui/toggle";
import { absoluteDate, relativeTime } from "@/lib/format";
import { useDocumentTitle } from "@/lib/use-document-title";
import { useDebouncedSearch, useListParams } from "@/lib/use-list-params";
import { useMutation, useQuery } from "@tanstack/react-query";
import {
  KeyRound,
  Mail,
  MailCheck,
  Pencil,
  Plus,
  ShieldCheck,
  Trash2,
  UserCheck,
  UserX,
  Users,
} from "lucide-react";
import { useEffect, useState } from "react";
import { Link, useSearchParams } from "react-router";

/**
 * 用户管理。
 *
 * 三处**自锁防护**必须在界面上说清楚，不能只靠服务端返回 409：
 * 不能停用或删除自己、不能摘掉自己的管理角色、不能让站点失去最后一名管理员。
 * 这类自锁没有后门，一旦发生只能改库恢复 —— 所以对自己的「停用」与「删除」
 * 在菜单里直接禁用，并把原因写在菜单项上。
 *
 * 口令是头等公民：创建时只接收一次明文，重置时同理，
 * 响应与列表绝不回显（服务端已保证，前端也不给它任何显示位置）。
 */

type User = components["schemas"]["User"];
type Role = components["schemas"]["Role"];

const STATUS_OPTIONS = [
  { value: "", label: "全部状态" },
  // 服务端的取值是 enabled / disabled（与 User.disabled 布尔字段相反），
  // 界面文案用「正常 / 已停用」——站长不关心字段叫什么
  { value: "enabled", label: "正常" },
  { value: "disabled", label: "已停用" },
];

const EMAIL_OPTIONS = [
  { value: "", label: "全部" },
  { value: "verified", label: "已验证" },
  { value: "unverified", label: "未验证" },
];

export function UsersPage() {
  useDocumentTitle("用户");
  const { user: me, can } = useAuth();
  const canManage = can("users:manage");
  const canManageRoles = can("roles:manage");
  const list = useListParams();
  const [searchParams, setSearchParams] = useSearchParams();
  const [search, setSearch] = useDebouncedSearch(list.filter("q"), (value) =>
    list.setFilter("q", value),
  );

  const [creating, setCreating] = useState(false);
  const [editing, setEditing] = useState<User | null>(null);
  const [resetting, setResetting] = useState<User | null>(null);
  const [deleting, setDeleting] = useState<User | null>(null);

  // 仪表盘的快捷入口带 ?create=1 进来时直接打开新建对话框
  const wantsCreate = searchParams.get("create") === "1";
  useEffect(() => {
    if (wantsCreate) {
      setCreating(true);
    }
  }, [wantsCreate]);

  function closeCreate() {
    setCreating(false);
    if (wantsCreate) {
      const next = new URLSearchParams(searchParams);
      next.delete("create");
      setSearchParams(next, { replace: true });
    }
  }

  const query = useQuery({
    queryKey: ["users", list.page, list.size, list.filters],
    queryFn: async () => {
      const { data, response } = await api.GET("/api/v1/console/users", {
        params: {
          query: {
            page: list.page,
            size: list.size,
            ...(list.filter("role") ? { role: list.filter("role") } : {}),
            ...(list.filter("status")
              ? { status: list.filter("status") as "enabled" | "disabled" }
              : {}),
            ...(list.filter("email")
              ? { email: list.filter("email") as "verified" | "unverified" }
              : {}),
            ...(list.filter("q") ? { q: list.filter("q") } : {}),
          },
        },
      });
      if (!response.ok) {
        throw new Error(`载入用户失败（HTTP ${response.status}）`);
      }
      return data;
    },
  });

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

  const items = query.data?.items ?? [];
  const total = query.data?.total ?? 0;
  const roles = rolesQuery.data ?? [];

  // `create` 也在地址栏里，但它不是筛选条件，不该点亮「清除筛选」
  const hasFilters = Boolean(
    list.filter("q") ||
      list.filter("role") ||
      list.filter("status") ||
      list.filter("email"),
  );

  return (
    <>
      <PageHeader
        icon={Users}
        title="用户"
        description="账号由管理员创建，口令只在创建与重置时接收一次"
        actions={
          <>
            {canManageRoles ? (
              <Button variant="secondary" size="sm" asChild>
                <Link to="/roles">
                  <ShieldCheck aria-hidden="true" />
                  角色
                </Link>
              </Button>
            ) : null}
            {canManage ? (
              <Button variant="primary" onClick={() => setCreating(true)}>
                <Plus aria-hidden="true" />
                新建
              </Button>
            ) : null}
          </>
        }
      />

      <PageBody>
        <Card className="overflow-hidden">
          <CardHeader>
            <ListToolbar
              search={
                <SearchInput
                  value={search}
                  onValueChange={setSearch}
                  placeholder="按用户名、昵称或邮箱筛选"
                  aria-label="筛选用户"
                  className="sm:max-w-xs"
                />
              }
              filters={
                <>
                  <FilterMenu
                    label="角色"
                    value={list.filter("role")}
                    options={[
                      { value: "", label: "全部角色" },
                      ...roles.map((role) => ({
                        value: role.name,
                        label: role.label || role.name,
                      })),
                    ]}
                    onChange={(value) => list.setFilter("role", value)}
                  />
                  <FilterMenu
                    label="状态"
                    value={list.filter("status")}
                    options={STATUS_OPTIONS}
                    onChange={(value) => list.setFilter("status", value)}
                  />
                  <FilterMenu
                    label="邮箱"
                    value={list.filter("email")}
                    options={EMAIL_OPTIONS}
                    onChange={(value) => list.setFilter("email", value)}
                  />
                </>
              }
              hasFilters={hasFilters}
              onClearFilters={list.reset}
              onRefresh={() => void query.refetch()}
              refreshing={query.isFetching}
            />
          </CardHeader>

          <ListBody
            isLoading={query.isLoading}
            error={query.error}
            onRetry={() => void query.refetch()}
            isEmpty={items.length === 0}
            empty={
              hasFilters ? (
                <ListEmpty
                  title="没有匹配的用户"
                  description="换个关键词或筛选条件试试。"
                  action={
                    <Button variant="secondary" size="sm" onClick={list.reset}>
                      清除筛选
                    </Button>
                  }
                />
              ) : (
                <ListEmpty
                  icon={UserCheck}
                  title="没有用户"
                  description="这不太正常，至少应该有一名管理员。可以用 lumo admin create-user 从命令行创建。"
                />
              )
            }
          >
            {items.map((user) => (
              <UserRow
                key={user.id}
                user={user}
                isSelf={user.id === me?.id}
                onEdit={() => setEditing(user)}
                onReset={() => setResetting(user)}
                onDelete={() => setDeleting(user)}
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

      <CreateUserDialog
        open={creating}
        roles={roles}
        canManageRoles={canManageRoles}
        onClose={closeCreate}
        onDone={() => void query.refetch()}
      />

      <EditUserDialog
        user={editing}
        onClose={() => setEditing(null)}
        onDone={() => void query.refetch()}
      />

      <ResetPasswordDialog
        user={resetting}
        onClose={() => setResetting(null)}
      />

      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
        title={`删除用户「${deleting?.displayName || deleting?.username}」？`}
        consequence={
          <p>
            <strong className="font-medium text-ink">这一步无法撤销。</strong>
            他的会话与访问令牌会立即失效。若他还拥有文章或附件，删除会被拒绝，
            那种情况应该改为停用。
          </p>
        }
        confirmLabel="删除"
        onConfirm={async () => {
          if (!deleting) {
            return;
          }
          await runMutation(
            () =>
              api.DELETE("/api/v1/console/users/{id}", {
                params: { path: { id: deleting.id } },
              }),
            { success: "用户已删除", invalidate: ["users"] },
          ).catch(() => {});
          setDeleting(null);
          void query.refetch();
        }}
      />
    </>
  );
}

/**
 * 一行用户。
 *
 * 「最后一名管理员」的判定在前端做一份：服务端会拒（409），
 * 但不显示一个点了必然失败的按钮更友好。判定只看内置管理角色，
 * 因为「管理员」的定义就是持有这些角色之一 —— 自定义角色即使凑齐了全部权限串，
 * 服务端也不把它计入管理员数量。
 */
function UserRow({
  user,
  isSelf,
  onEdit,
  onReset,
  onDelete,
}: {
  user: User;
  isSelf: boolean;
  onEdit: () => void;
  onReset: () => void;
  onDelete: () => void;
}) {
  const [confirmToggle, setConfirmToggle] = useState(false);

  const isAdmin = (user.roles ?? []).some(
    (role) => role.name === "admin" || role.name === "super-admin",
  );

  const setStatus = useMutation({
    mutationFn: (disabled: boolean) =>
      runMutation(
        () =>
          api.PUT("/api/v1/console/users/{id}/status", {
            params: { path: { id: user.id } },
            body: { disabled },
          }),
        {
          success: disabled ? "账号已停用" : "账号已恢复",
          invalidate: ["users"],
        },
      ),
  });

  const name = user.displayName || user.username;
  const selfReason = "不能停用或删除自己，这类自锁没有后门，只能改库恢复";

  return (
    <Entity>
      <EntityStart>
        <Avatar src={user.avatarUrl} name={name} size="sm" />
        <EntityField
          width="max-w-md"
          title={name}
          extra={isSelf ? <Badge tone="seal">你</Badge> : null}
          description={
            <>
              <span>@{user.username}</span>
              <span className="token">{user.email}</span>
            </>
          }
        />
      </EntityStart>

      <EntityEnd>
        <div className="hidden flex-wrap justify-end gap-1 sm:flex">
          {(user.roles ?? []).length === 0 ? (
            <span className="text-xs text-ink-subtle">无角色</span>
          ) : (
            (user.roles ?? []).map((role) => (
              <Badge key={role.id} tone="outline">
                {role.label || role.name}
              </Badge>
            ))
          )}
        </div>

        {/* 只占一个状态位，按「现在能不能登录」排优先级：停用压过未验证 */}
        {user.disabled ? (
          <StatusDot state="danger">已停用</StatusDot>
        ) : !user.emailVerified ? (
          <StatusDot state="warn" title="邮箱验证之前不能登录">
            邮箱未验证
          </StatusDot>
        ) : (
          <StatusDot state="ok">正常</StatusDot>
        )}

        <EntityMeta hideOnMobile>
          {user.lastLoginAt ? (
            <time
              dateTime={user.lastLoginAt}
              title={absoluteDate(user.lastLoginAt)}
            >
              最近登录 {relativeTime(user.lastLoginAt)}
            </time>
          ) : (
            <span>从未登录</span>
          )}
        </EntityMeta>

        <EntityActions label={`用户 ${user.username} 的操作`}>
          <DropdownMenuItem onSelect={onEdit}>
            <Pencil aria-hidden="true" />
            编辑资料
          </DropdownMenuItem>
          <DropdownMenuItem onSelect={onReset}>
            <KeyRound aria-hidden="true" />
            重置口令
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          {/*
            自锁防护：对自己禁用，而不是点了再吃一个 409。
            服务端是权威，这里只是少让人白跑一趟。
          */}
          <DropdownMenuItem
            disabled={isSelf}
            title={isSelf ? selfReason : undefined}
            onSelect={() => setConfirmToggle(true)}
          >
            {user.disabled ? (
              <UserCheck aria-hidden="true" />
            ) : (
              <UserX aria-hidden="true" />
            )}
            {user.disabled ? "恢复账号" : "停用账号"}
            {isSelf ? (
              <span className="ml-auto text-xs text-ink-subtle">自己</span>
            ) : null}
          </DropdownMenuItem>
          <DropdownMenuItem
            danger
            disabled={isSelf}
            title={isSelf ? selfReason : undefined}
            onSelect={onDelete}
          >
            <Trash2 aria-hidden="true" />
            删除
            {isSelf ? (
              <span className="ml-auto text-xs text-ink-subtle">自己</span>
            ) : null}
          </DropdownMenuItem>
        </EntityActions>
      </EntityEnd>

      <ConfirmDialog
        open={confirmToggle}
        onOpenChange={setConfirmToggle}
        destructive={!user.disabled}
        title={`${user.disabled ? "恢复" : "停用"}「${name}」？`}
        consequence={
          user.disabled ? (
            <p>恢复后他可以重新登录，权限与停用前一致。</p>
          ) : (
            <p>
              他的会话与访问令牌会
              <strong className="font-medium text-ink">立即失效</strong>
              ，已登录的页面下一次请求就会被登出。内容不受影响，随时可以恢复。
              {isAdmin ? "他是管理员，停用后站点可能失去管理能力。" : ""}
            </p>
          )
        }
        confirmLabel={user.disabled ? "恢复" : "停用"}
        pending={setStatus.isPending}
        onConfirm={async () => {
          await setStatus.mutateAsync(!user.disabled).catch(() => {});
          setConfirmToggle(false);
        }}
      />
    </Entity>
  );
}

/** 创建用户。口令只在这里输入一次，创建后无法再查看。 */
function CreateUserDialog({
  open,
  roles,
  canManageRoles,
  onClose,
  onDone,
}: {
  open: boolean;
  roles: Role[];
  canManageRoles: boolean;
  onClose: () => void;
  onDone: () => void;
}) {
  const [form, setForm] = useState({
    username: "",
    email: "",
    password: "",
    displayName: "",
    roles: [] as string[],
  });
  const [error, setError] = useState("");

  const create = useMutation({
    mutationFn: async () => {
      const { error: problem, response } = await api.POST(
        "/api/v1/console/users",
        {
          body: {
            username: form.username.trim(),
            email: form.email.trim(),
            password: form.password,
            displayName: form.displayName.trim(),
            roles: form.roles,
          },
        },
      );
      if (!response.ok) {
        throw new Error(problemMessage(problem));
      }
    },
    onSuccess: () => {
      setForm({
        username: "",
        email: "",
        password: "",
        displayName: "",
        roles: [],
      });
      onDone();
      onClose();
    },
    onError: (err) => setError(err instanceof Error ? err.message : "创建失败"),
  });

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) {
          setError("");
          onClose();
        }
      }}
    >
      <DialogContent>
        <form
          onSubmit={(event) => {
            event.preventDefault();
            setError("");
            create.mutate();
          }}
        >
          <DialogHeader>
            <DialogTitle>新建用户</DialogTitle>
            <DialogDescription>
              口令只在这一步输入，保存后服务端只留哈希，任何人都无法再读出它。
            </DialogDescription>
          </DialogHeader>

          <DialogBody className="flex flex-col gap-4">
            {error ? (
              <Alert tone="danger" title="没有创建">
                {error}
              </Alert>
            ) : null}

            <Field>
              <FieldLabel htmlFor="new-username">用户名</FieldLabel>
              <Input
                id="new-username"
                value={form.username}
                onChange={(e) => setForm({ ...form, username: e.target.value })}
                autoComplete="off"
                required
                autoFocus
              />
              <FieldDescription>登录用，创建后不建议更改。</FieldDescription>
            </Field>

            <Field>
              <FieldLabel htmlFor="new-email">邮箱</FieldLabel>
              <Input
                id="new-email"
                type="email"
                value={form.email}
                onChange={(e) => setForm({ ...form, email: e.target.value })}
                autoComplete="off"
                required
              />
            </Field>

            <Field>
              <FieldLabel htmlFor="new-display">显示名</FieldLabel>
              <Input
                id="new-display"
                value={form.displayName}
                onChange={(e) =>
                  setForm({ ...form, displayName: e.target.value })
                }
                autoComplete="off"
              />
              <FieldDescription>文章署名用；留空则用用户名。</FieldDescription>
            </Field>

            <Field>
              <FieldLabel htmlFor="new-password">初始口令</FieldLabel>
              <Input
                id="new-password"
                type="password"
                value={form.password}
                onChange={(e) => setForm({ ...form, password: e.target.value })}
                autoComplete="new-password"
                required
                minLength={8}
              />
              <FieldDescription>
                至少 8 个字节。请他登录后自行修改。
              </FieldDescription>
            </Field>

            {canManageRoles ? (
              <RolePicker
                roles={roles}
                selected={form.roles}
                onChange={(selected) => setForm({ ...form, roles: selected })}
              />
            ) : null}
          </DialogBody>

          <DialogFooter>
            <Button variant="secondary" onClick={onClose}>
              取消
            </Button>
            <Button type="submit" variant="primary" loading={create.isPending}>
              创建用户
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

/** 编辑资料与角色。口令不在这里改 —— 那是「重置口令」的事。 */
function EditUserDialog({
  user,
  onClose,
  onDone,
}: {
  user: User | null;
  onClose: () => void;
  onDone: () => void;
}) {
  const [email, setEmail] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [bio, setBio] = useState("");
  const [avatarUrl, setAvatarUrl] = useState("");
  const [initialised, setInitialised] = useState<number | null>(null);
  // 弹窗里标记为已验证之后，手上这份 user 仍是打开时的快照，靠它把区块收起来
  const [verifiedHere, setVerifiedHere] = useState(false);

  // 打开另一个用户时重填。用 id 判断而不是对象引用：
  // 列表刷新后引用会变，那会覆盖用户正在编辑的内容。
  if (user && initialised !== user.id) {
    setEmail(user.email);
    setDisplayName(user.displayName);
    setBio(user.bio);
    setAvatarUrl(user.avatarUrl);
    setVerifiedHere(false);
    setInitialised(user.id);
  }
  const verified = Boolean(user?.emailVerified) || verifiedHere;

  const save = useMutation({
    mutationFn: () =>
      runMutation(
        () =>
          api.PUT("/api/v1/console/users/{id}", {
            params: { path: { id: user?.id ?? 0 } },
            body: {
              email: email.trim(),
              displayName: displayName.trim(),
              bio: bio.trim(),
              avatarUrl: avatarUrl.trim(),
            },
          }),
        { success: "已保存", invalidate: ["users"] },
      ),
    onSuccess: () => {
      onDone();
      onClose();
    },
  });

  return (
    <Dialog open={user !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent>
        <form
          onSubmit={(event) => {
            event.preventDefault();
            save.mutate();
          }}
        >
          <DialogHeader>
            <DialogTitle>编辑 {user?.username}</DialogTitle>
            <DialogDescription>
              改口令请用「重置口令」；角色在角色页管理。
            </DialogDescription>
          </DialogHeader>

          <DialogBody className="flex flex-col gap-4">
            <Field>
              <FieldLabel htmlFor="edit-email">邮箱</FieldLabel>
              <Input
                id="edit-email"
                type="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                required
              />
              {verified ? (
                <FieldDescription>邮箱已验证。</FieldDescription>
              ) : null}
            </Field>

            {user && !verified ? (
              <EmailVerification
                user={user}
                onVerified={() => {
                  setVerifiedHere(true);
                  onDone();
                }}
              />
            ) : null}

            <Field>
              <FieldLabel htmlFor="edit-display">显示名</FieldLabel>
              <Input
                id="edit-display"
                value={displayName}
                onChange={(e) => setDisplayName(e.target.value)}
              />
            </Field>

            <Field>
              <FieldLabel htmlFor="edit-avatar">头像地址</FieldLabel>
              <Input
                id="edit-avatar"
                value={avatarUrl}
                onChange={(e) => setAvatarUrl(e.target.value)}
                placeholder="https://… 或 /uploads/…"
              />
            </Field>

            <Field>
              <FieldLabel htmlFor="edit-bio">简介</FieldLabel>
              <Textarea
                id="edit-bio"
                rows={3}
                value={bio}
                onChange={(e) => setBio(e.target.value)}
              />
              <FieldDescription>显示在作者页；访客能看到它。</FieldDescription>
            </Field>
          </DialogBody>

          <DialogFooter>
            <Button variant="secondary" onClick={onClose}>
              取消
            </Button>
            <Button variant="primary" type="submit" loading={save.isPending}>
              保存
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

/**
 * 邮箱未验证时的处置区。未验证的账号不能登录，站长能做两件事：
 * 再发一封验证信让他自己点开，或者确认过邮箱属于他之后直接放行。
 * 发不了信的原因（没配 SMTP、没配站点地址）由服务端给出，按钮旁边原样写明。
 */
function EmailVerification({
  user,
  onVerified,
}: {
  user: User;
  onVerified: () => void;
}) {
  const [confirming, setConfirming] = useState(false);

  const mailStatus = useQuery({
    queryKey: ["verification-mail"],
    queryFn: async () => {
      const { data, response } = await api.GET(
        "/api/v1/console/account/verification-mail",
      );
      if (!response.ok) {
        throw new Error(`查询发信条件失败（HTTP ${response.status}）`);
      }
      return data;
    },
  });

  const resend = useMutation({
    mutationFn: () =>
      runMutation(
        () =>
          api.POST("/api/v1/console/users/{id}/verification-mail", {
            params: { path: { id: user.id } },
          }),
        { success: `验证邮件已发往 ${user.email}` },
      ),
  });

  const markVerified = useMutation({
    mutationFn: () =>
      runMutation(
        () =>
          api.PUT("/api/v1/console/users/{id}/email-verified", {
            params: { path: { id: user.id } },
          }),
        { success: "已标记为已验证", invalidate: ["users"] },
      ),
    onSuccess: onVerified,
  });

  const blocked =
    mailStatus.data && !mailStatus.data.available ? mailStatus.data.reason : "";

  return (
    <Alert tone="warn" title="邮箱还没有验证，这个账号现在不能登录">
      <p>
        可以再发一封验证邮件让他点开链接；如果你已经确认这个邮箱属于他，也可以直接标记为已验证。
      </p>
      {blocked ? <p className="mt-1">{blocked}</p> : null}
      <div className="mt-2.5 flex flex-wrap gap-2">
        <Button
          variant="secondary"
          size="sm"
          onClick={() => resend.mutate()}
          loading={resend.isPending}
          disabled={Boolean(blocked) || mailStatus.isLoading}
        >
          <Mail aria-hidden="true" />
          重发验证邮件
        </Button>
        <Button
          variant="secondary"
          size="sm"
          onClick={() => setConfirming(true)}
        >
          <MailCheck aria-hidden="true" />
          标记为已验证
        </Button>
      </div>

      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        destructive={false}
        title={`把 ${user.email} 标记为已验证？`}
        consequence={
          <p>
            标记之后他就能登录了。只在你确认过这个邮箱确实属于他时这样做，
            否则等于让一个没人收信的邮箱拥有账号。
          </p>
        }
        confirmLabel="标记为已验证"
        pending={markVerified.isPending}
        onConfirm={async () => {
          await markVerified.mutateAsync().catch(() => {});
          setConfirming(false);
        }}
      />
    </Alert>
  );
}

/** 重置口令。成功后会清除该用户的全部会话与令牌 —— 必须说清楚。 */
function ResetPasswordDialog({
  user,
  onClose,
}: {
  user: User | null;
  onClose: () => void;
}) {
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [error, setError] = useState("");

  const reset = useMutation({
    mutationFn: async () => {
      const { error: problem, response } = await api.PUT(
        "/api/v1/console/users/{id}/password",
        {
          params: { path: { id: user?.id ?? 0 } },
          body: { password },
        },
      );
      if (!response.ok) {
        throw new Error(problemMessage(problem));
      }
    },
    onSuccess: () => {
      setPassword("");
      setConfirm("");
      onClose();
    },
    onError: (err) => setError(err instanceof Error ? err.message : "重置失败"),
  });

  const mismatch = confirm !== "" && password !== confirm;

  return (
    <Dialog
      open={user !== null}
      onOpenChange={(open) => {
        if (!open) {
          setPassword("");
          setConfirm("");
          setError("");
          onClose();
        }
      }}
    >
      <DialogContent size="sm">
        <form
          onSubmit={(event) => {
            event.preventDefault();
            setError("");
            if (password !== confirm) {
              setError("两次输入的密码不一致");
              return;
            }
            reset.mutate();
          }}
        >
          <DialogHeader>
            <DialogTitle>
              重置「{user?.displayName || user?.username}」的口令
            </DialogTitle>
            <DialogDescription>
              重置后他的全部会话与访问令牌立即失效，需要用新口令重新登录。
              已发布的文章与附件不受影响。
            </DialogDescription>
          </DialogHeader>

          <DialogBody className="flex flex-col gap-4">
            {error ? (
              <Alert tone="danger" title="没有重置">
                {error}
              </Alert>
            ) : null}

            <Field>
              <FieldLabel htmlFor="reset-password">新口令</FieldLabel>
              <Input
                id="reset-password"
                type="password"
                value={password}
                onChange={(e) => {
                  setPassword(e.target.value);
                  setError("");
                }}
                autoComplete="new-password"
                required
                minLength={8}
                autoFocus
              />
              <FieldDescription>至少 8 个字节。</FieldDescription>
            </Field>

            <Field>
              <FieldLabel htmlFor="reset-confirm">再输一次</FieldLabel>
              <Input
                id="reset-confirm"
                type="password"
                value={confirm}
                onChange={(e) => {
                  setConfirm(e.target.value);
                  setError("");
                }}
                autoComplete="new-password"
                required
                aria-invalid={mismatch ? true : undefined}
                aria-describedby={mismatch ? "reset-error" : undefined}
              />
              <FieldError id="reset-error">
                {mismatch ? "两次输入不一致" : ""}
              </FieldError>
            </Field>
          </DialogBody>

          <DialogFooter>
            <Button variant="secondary" onClick={onClose}>
              取消
            </Button>
            <Button
              type="submit"
              variant="primary"
              loading={reset.isPending}
              disabled={!password || mismatch}
            >
              重置口令
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

/** 角色多选。不折叠成权限串 —— 用户选的是角色，不是权限。 */
function RolePicker({
  roles,
  selected,
  onChange,
}: {
  roles: Role[];
  selected: string[];
  onChange: (selected: string[]) => void;
}) {
  return (
    <fieldset className="flex flex-col gap-1">
      <legend className="mb-1.5 flex items-center gap-1.5 text-sm font-medium text-ink">
        <ShieldCheck aria-hidden="true" className="size-4 text-ink-muted" />
        角色
      </legend>
      {roles.map((role) => (
        <CheckboxRow
          key={role.id}
          id={`new-role-${role.id}`}
          checked={selected.includes(role.name)}
          onCheckedChange={(checked) =>
            onChange(
              checked
                ? [...selected, role.name]
                : selected.filter((name) => name !== role.name),
            )
          }
          label={role.label || role.name}
          description={role.description || undefined}
          className="-mx-2"
        />
      ))}
    </fieldset>
  );
}
