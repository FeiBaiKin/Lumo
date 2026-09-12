import { api, problemMessage } from "@/api/client";
import { runMutation } from "@/api/mutation";
import type { components } from "@/api/schema";
import { useAuth } from "@/components/auth/auth-provider";
import {
  Entity,
  EntityActions,
  EntityEnd,
  EntityField,
  EntityList,
  EntityMeta,
  EntityStart,
} from "@/components/data/entity";
import { PageBody, PageHeader } from "@/components/layout/page-header";
import { Alert } from "@/components/ui/alert";
import { Avatar } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardBody,
  CardHeader,
  DescriptionDetail,
  DescriptionList,
  DescriptionTerm,
} from "@/components/ui/card";
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
import { DropdownMenuItem } from "@/components/ui/dropdown-menu";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
} from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { EmptyState, Skeleton } from "@/components/ui/states";
import { CheckboxRow } from "@/components/ui/toggle";
import { absoluteDate, fromLocalInput, relativeTime } from "@/lib/format";
import { useDocumentTitle } from "@/lib/use-document-title";
import { useMutation, useQuery } from "@tanstack/react-query";
import {
  Copy,
  KeyRound,
  Plus,
  ShieldCheck,
  Trash2,
  UserRound,
} from "lucide-react";
import { useMemo, useState } from "react";
import { toast } from "sonner";

/**
 * 个人中心（Halo 的「个人中心」）。
 *
 * 三件事：看自己是谁（资料与角色）、改口令、管理个人访问令牌。
 * 资料本身不在这里改 —— 服务端只有管理员改任意用户的接口，没有「改自己」的接口，
 * 与其做一个只有管理员能用的表单，不如把它留在用户页。
 *
 * 令牌明文只在签发那一刻显示一次（服务端只存哈希），界面必须把这句话说在前面。
 */

type Token = components["schemas"]["AccessToken"];

const ROLE_LABELS: Record<string, string> = {
  "super-admin": "超级管理员",
  admin: "管理员",
  editor: "编辑",
  author: "作者",
};

export function ProfilePage() {
  useDocumentTitle("个人中心");
  const { user, permissions, isToken } = useAuth();

  return (
    <>
      <PageHeader icon={UserRound} title="个人中心" />
      <PageBody>
        <div className="grid gap-4 lg:grid-cols-2">
          <Card>
            <CardHeader title="资料" />
            <CardBody className="flex flex-col gap-5">
              <div className="flex items-center gap-4">
                <Avatar
                  src={user?.avatarUrl}
                  name={user?.displayName || user?.username}
                  size="lg"
                />
                <div className="flex min-w-0 flex-col">
                  <span className="truncate text-lg font-semibold text-ink">
                    {user?.displayName || user?.username}
                  </span>
                  <span className="truncate text-sm text-ink-muted">
                    @{user?.username}
                  </span>
                </div>
              </div>
              <DescriptionList>
                <DescriptionTerm>邮箱</DescriptionTerm>
                <DescriptionDetail className="token">
                  {user?.email}
                </DescriptionDetail>
                <DescriptionTerm>角色</DescriptionTerm>
                <DescriptionDetail className="flex flex-wrap gap-1">
                  {(user?.roles ?? []).length === 0 ? (
                    <span className="text-ink-subtle">无角色</span>
                  ) : (
                    (user?.roles ?? []).map((role) => (
                      <Badge key={role} tone="outline">
                        <ShieldCheck aria-hidden="true" />
                        {ROLE_LABELS[role] ?? role}
                      </Badge>
                    ))
                  )}
                </DescriptionDetail>
                <DescriptionTerm>权限</DescriptionTerm>
                <DescriptionDetail className="tabular">
                  {permissions.size} 项
                </DescriptionDetail>
                <DescriptionTerm>会话</DescriptionTerm>
                <DescriptionDetail>
                  {isToken ? "访问令牌（只读）" : "浏览器会话"}
                </DescriptionDetail>
              </DescriptionList>
              <p className="text-xs text-ink-muted">
                显示名、头像与简介由管理员在用户页修改。
              </p>
            </CardBody>
          </Card>

          <ChangePasswordCard />
        </div>

        <TokensCard permissions={[...permissions].sort()} />
      </PageBody>
    </>
  );
}

function ChangePasswordCard() {
  const [oldPassword, setOldPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [error, setError] = useState("");

  const change = useMutation({
    mutationFn: async () => {
      const { error: problem, response } = await api.POST(
        "/api/v1/console/auth/change-password",
        { body: { oldPassword, newPassword } },
      );
      if (!response.ok) {
        throw new Error(problemMessage(problem));
      }
    },
    onSuccess: () => {
      setOldPassword("");
      setNewPassword("");
      setConfirm("");
      toast.success("密码已修改");
    },
    onError: (err) => setError(err instanceof Error ? err.message : "修改失败"),
  });

  const mismatch = confirm !== "" && newPassword !== confirm;

  return (
    <Card>
      <CardHeader
        title="修改密码"
        description="至少 8 个字节；修改后其他设备需要重新登录"
      />
      <CardBody>
        <form
          className="flex max-w-md flex-col gap-4"
          onSubmit={(event) => {
            event.preventDefault();
            setError("");
            if (newPassword !== confirm) {
              setError("两次输入的新密码不一致");
              return;
            }
            change.mutate();
          }}
        >
          {error ? (
            <Alert tone="danger" title="没有修改">
              {error}
            </Alert>
          ) : null}
          <Field>
            <FieldLabel htmlFor="pw-old">当前密码</FieldLabel>
            <Input
              id="pw-old"
              type="password"
              autoComplete="current-password"
              value={oldPassword}
              onChange={(e) => setOldPassword(e.target.value)}
              required
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="pw-new">新密码</FieldLabel>
            <Input
              id="pw-new"
              type="password"
              autoComplete="new-password"
              value={newPassword}
              onChange={(e) => setNewPassword(e.target.value)}
              required
              minLength={8}
              maxLength={128}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="pw-confirm">再输一次新密码</FieldLabel>
            <Input
              id="pw-confirm"
              type="password"
              autoComplete="new-password"
              value={confirm}
              onChange={(e) => setConfirm(e.target.value)}
              required
              aria-invalid={mismatch ? true : undefined}
              aria-describedby={mismatch ? "pw-confirm-error" : undefined}
            />
            <FieldError id="pw-confirm-error">
              {mismatch ? "两次输入不一致" : ""}
            </FieldError>
          </Field>
          <div>
            <Button
              type="submit"
              variant="primary"
              loading={change.isPending}
              disabled={!oldPassword || !newPassword || !confirm || mismatch}
            >
              修改密码
            </Button>
          </div>
        </form>
      </CardBody>
    </Card>
  );
}

function TokensCard({ permissions }: { permissions: string[] }) {
  const [creating, setCreating] = useState(false);
  const [revoking, setRevoking] = useState<Token | null>(null);
  const [issued, setIssued] = useState<{
    name: string;
    plaintext: string;
  } | null>(null);

  const query = useQuery({
    queryKey: ["tokens"],
    queryFn: async () => {
      const { data, response } = await api.GET("/api/v1/console/auth/tokens");
      if (!response.ok) {
        throw new Error(`载入令牌失败（HTTP ${response.status}）`);
      }
      return data?.items ?? [];
    },
  });

  const revoke = useMutation({
    mutationFn: (id: number) =>
      runMutation(
        () =>
          api.DELETE("/api/v1/console/auth/tokens/{id}", {
            params: { path: { id } },
          }),
        { success: "令牌已吊销", invalidate: ["tokens"] },
      ),
  });

  const tokens = query.data ?? [];

  return (
    <Card>
      <CardHeader
        title="访问令牌"
        description="供脚本与无头调用使用，请求头带 Authorization: Bearer 即可；权限只能收窄，不能放大"
        actions={
          <Button variant="primary" size="sm" onClick={() => setCreating(true)}>
            <Plus aria-hidden="true" />
            新建令牌
          </Button>
        }
      />

      {issued ? (
        <div className="border-line border-b p-4">
          <Alert tone="ok" title={`令牌「${issued.name}」已签发`}>
            <p>
              明文只显示这一次，关掉这条提示后无法再查看。请立即复制到安全的地方。
            </p>
            <div className="mt-2 flex flex-wrap items-center gap-2">
              <code className="token rounded-control bg-surface-inset px-2 py-1 text-xs text-ink">
                {issued.plaintext}
              </code>
              <Button
                variant="secondary"
                size="xs"
                onClick={async () => {
                  try {
                    await navigator.clipboard.writeText(issued.plaintext);
                    toast.success("已复制");
                  } catch {
                    toast.error("剪贴板不可用，请手动选中复制");
                  }
                }}
              >
                <Copy aria-hidden="true" />
                复制
              </Button>
              <Button variant="ghost" size="xs" onClick={() => setIssued(null)}>
                我已保存
              </Button>
            </div>
          </Alert>
        </div>
      ) : null}

      {query.isLoading ? (
        <div className="flex flex-col gap-3 p-4">
          <Skeleton className="h-10 w-full" />
          <Skeleton className="h-10 w-full" />
        </div>
      ) : tokens.length === 0 ? (
        <EmptyState
          icon={KeyRound}
          title="还没有访问令牌"
          description="令牌用于脚本、发布工具或第三方客户端，每个令牌的权限都可以单独收窄。"
          action={
            <Button
              variant="secondary"
              size="sm"
              onClick={() => setCreating(true)}
            >
              新建令牌
            </Button>
          }
        />
      ) : (
        <EntityList>
          {tokens.map((token) => (
            <Entity key={token.id}>
              <EntityStart>
                <span className="flex size-8 shrink-0 items-center justify-center rounded-full bg-surface-active text-ink-muted">
                  <KeyRound aria-hidden="true" className="size-4" />
                </span>
                <EntityField
                  width="max-w-md"
                  title={token.name}
                  description={
                    <>
                      <code className="token">{token.tokenHint}</code>
                      <span>
                        {(token.scopes ?? []).length === 0
                          ? "继承全部权限"
                          : `限 ${(token.scopes ?? []).length} 项权限`}
                      </span>
                    </>
                  }
                />
              </EntityStart>
              <EntityEnd>
                <EntityMeta hideOnMobile>
                  {token.lastUsedAt
                    ? `最近使用 ${relativeTime(token.lastUsedAt)}`
                    : "从未使用"}
                </EntityMeta>
                <EntityMeta>
                  {token.expiresAt ? (
                    <span title={absoluteDate(token.expiresAt)}>
                      {new Date(token.expiresAt) < new Date()
                        ? "已过期"
                        : `${relativeTime(token.expiresAt)}过期`}
                    </span>
                  ) : (
                    "永不过期"
                  )}
                </EntityMeta>
                <EntityActions label={`令牌 ${token.name} 的操作`}>
                  <DropdownMenuItem danger onSelect={() => setRevoking(token)}>
                    <Trash2 aria-hidden="true" />
                    吊销
                  </DropdownMenuItem>
                </EntityActions>
              </EntityEnd>
            </Entity>
          ))}
        </EntityList>
      )}

      <CreateTokenDialog
        open={creating}
        permissions={permissions}
        onClose={() => setCreating(false)}
        onIssued={(name, plaintext) => {
          setIssued({ name, plaintext });
          void query.refetch();
        }}
      />

      <ConfirmDialog
        open={revoking !== null}
        onOpenChange={(open) => !open && setRevoking(null)}
        title={`吊销令牌「${revoking?.name ?? ""}」？`}
        consequence={
          <p>
            <strong className="font-medium text-ink">这一步无法撤销。</strong>
            正在用它的脚本会立即收到 401，需要换一枚新令牌。
          </p>
        }
        confirmLabel="吊销"
        pending={revoke.isPending}
        onConfirm={async () => {
          if (!revoking) {
            return;
          }
          await revoke.mutateAsync(revoking.id).catch(() => {});
          setRevoking(null);
        }}
      />
    </Card>
  );
}

function CreateTokenDialog({
  open,
  permissions,
  onClose,
  onIssued,
}: {
  open: boolean;
  permissions: string[];
  onClose: () => void;
  onIssued: (name: string, plaintext: string) => void;
}) {
  const [name, setName] = useState("");
  const [expiresAt, setExpiresAt] = useState("");
  const [limitScopes, setLimitScopes] = useState(false);
  const [scopes, setScopes] = useState<string[]>([]);
  const [error, setError] = useState("");

  const grouped = useMemo(() => {
    const map = new Map<string, string[]>();
    for (const key of permissions) {
      const resource = key.split(":")[0] ?? key;
      const list = map.get(resource) ?? [];
      list.push(key);
      map.set(resource, list);
    }
    return [...map.entries()];
  }, [permissions]);

  const create = useMutation({
    mutationFn: async () => {
      const body: components["schemas"]["TokenRequest"] = { name: name.trim() };
      const iso = fromLocalInput(expiresAt);
      if (iso) {
        body.expiresAt = iso;
      }
      if (limitScopes) {
        body.scopes = scopes;
      }
      const {
        data,
        error: problem,
        response,
      } = await api.POST("/api/v1/console/auth/tokens", { body });
      if (!response.ok || !data) {
        throw new Error(problemMessage(problem));
      }
      return data;
    },
    onSuccess: (data) => {
      onIssued(data.token.name, data.plaintext);
      setName("");
      setExpiresAt("");
      setLimitScopes(false);
      setScopes([]);
      onClose();
    },
    onError: (err) => setError(err instanceof Error ? err.message : "签发失败"),
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
            if (limitScopes && scopes.length === 0) {
              setError("收窄权限时至少要选一项，否则这枚令牌什么都做不了");
              return;
            }
            create.mutate();
          }}
        >
          <DialogHeader>
            <DialogTitle>新建访问令牌</DialogTitle>
            <DialogDescription>
              明文只在签发时显示一次；令牌的权限是你自己权限的子集。
            </DialogDescription>
          </DialogHeader>
          <DialogBody className="flex flex-col gap-4">
            {error ? <Alert tone="danger">{error}</Alert> : null}
            <Field>
              <FieldLabel htmlFor="token-name">名称</FieldLabel>
              <Input
                id="token-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="例如：发布脚本"
                required
                maxLength={128}
                autoFocus
              />
              <FieldDescription>只用来在列表里认出它。</FieldDescription>
            </Field>
            <Field>
              <FieldLabel htmlFor="token-expires">过期时间</FieldLabel>
              <Input
                id="token-expires"
                type="datetime-local"
                value={expiresAt}
                onChange={(e) => setExpiresAt(e.target.value)}
              />
              <FieldDescription>留空表示永不过期。</FieldDescription>
            </Field>
            <CheckboxRow
              id="token-limit"
              checked={limitScopes}
              onCheckedChange={setLimitScopes}
              label="收窄权限"
              description="不勾选时令牌继承你的全部权限；勾选后只保留下面选中的项"
              className="-mx-2"
            />
            {limitScopes ? (
              <fieldset className="flex flex-col gap-3 rounded-control border border-line bg-surface-raised p-3">
                <legend className="px-1 text-xs text-ink-muted">
                  已选 {scopes.length} / {permissions.length}
                </legend>
                {grouped.map(([resource, keys]) => (
                  <div key={resource} className="flex flex-col">
                    <span className="px-2 text-xs font-medium text-ink-muted">
                      {resource}
                    </span>
                    {keys.map((key) => (
                      <CheckboxRow
                        key={key}
                        id={`scope-${key}`}
                        checked={scopes.includes(key)}
                        onCheckedChange={(checked) =>
                          setScopes((prev) =>
                            checked
                              ? [...prev, key]
                              : prev.filter((item) => item !== key),
                          )
                        }
                        label={<code className="text-xs">{key}</code>}
                      />
                    ))}
                  </div>
                ))}
              </fieldset>
            ) : null}
          </DialogBody>
          <DialogFooter>
            <Button variant="secondary" onClick={onClose}>
              取消
            </Button>
            <Button
              type="submit"
              variant="primary"
              loading={create.isPending}
              disabled={!name.trim()}
            >
              签发令牌
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
