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
} from "@/components/data/entity";
import { PageBody, PageHeader } from "@/components/layout/page-header";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardBody, CardHeader } from "@/components/ui/card";
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
import { EmptyState, EntitySkeleton, ErrorState } from "@/components/ui/states";
import { StatusDot } from "@/components/ui/status-dot";
import { CheckboxRow, Switch } from "@/components/ui/toggle";
import { relativeTime } from "@/lib/format";
import { useDocumentTitle } from "@/lib/use-document-title";
import { cn } from "@/lib/utils";
import { PluginSettingsDialog } from "@/pages/system/plugin-settings";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AlertTriangle,
  BookOpen,
  Clock,
  ExternalLink,
  Globe,
  LayoutTemplate,
  type LucideIcon,
  Mail,
  PenLine,
  Puzzle,
  Settings,
  ShieldCheck,
  Trash2,
  Upload,
} from "lucide-react";
import { useRef, useState } from "react";
import { toast } from "sonner";

/**
 * 插件管理：装、卸、启用 / 停用、改设置。
 *
 * 插件分两种：纯声明式的只有清单、设置与静态资源；「带后端」的另有一段在沙箱里运行的代码。
 * 声明了能力（读写内容、访问网络、发邮件……）的插件，启用前先把这些能力逐条列给站长确认；
 * 被系统停用的（连续出错、新版本多要了能力、后端加载失败），行内写明原因。
 *
 * 与主题页的差别值得说明：主题**同一时刻只有一个生效**，所以那边是「选一个」；
 * 插件可以同时启用多个，所以这里是逐个开合。这个差别来自产品语义，不是排版偏好。
 */

type PluginView = components["schemas"]["PluginView"];
type PluginDep = components["schemas"]["PluginDepView"];
type Retained = components["schemas"]["Retained"];
type DataCounts = components["schemas"]["DataCounts"];

/** 一条依赖没就绪的原因；就绪时返回空串。 */
function depNote(dep: PluginDep): string {
  if (dep.satisfied) {
    return "";
  }
  if (!dep.installed) {
    return "没安装";
  }
  if (!dep.enabled) {
    return "没启用";
  }
  return `只有 ${dep.installedVersion}`;
}

export function PluginsPage() {
  useDocumentTitle("插件");
  const [installing, setInstalling] = useState(false);
  const [removing, setRemoving] = useState<PluginView | null>(null);
  const [settingsFor, setSettingsFor] = useState<string | null>(null);
  const [consentFor, setConsentFor] = useState<PluginView | null>(null);
  // 被别的插件依赖着，停用前要说清会连带停掉谁
  const [stopping, setStopping] = useState<PluginView | null>(null);

  const query = useQuery({
    queryKey: ["plugins"],
    queryFn: async () => {
      const { data, error, response } = await api.GET(
        "/api/v1/console/plugins",
      );
      if (!response.ok) {
        throw new Error(problemMessage(error));
      }
      return data;
    },
  });

  const plugins = query.data?.items ?? [];
  const retained = query.data?.retained ?? [];
  const [purging, setPurging] = useState<Retained | null>(null);
  const queryClient = useQueryClient();
  // 插件的资源页入口在侧栏里，启停与卸载之后要让侧栏重取
  const refreshNav = () =>
    queryClient.invalidateQueries({ queryKey: ["navigation"] });

  const setEnabled = useMutation({
    mutationFn: async (input: {
      name: string;
      enabled: boolean;
      accept?: boolean;
    }) =>
      runMutation(
        () =>
          api.PUT("/api/v1/console/plugins/{name}/enabled", {
            params: { path: { name: input.name } },
            body: {
              enabled: input.enabled,
              ...(input.accept ? { acceptCapabilities: true } : {}),
            },
          }),
        {
          success: input.enabled ? "插件已启用" : "插件已停用",
          invalidate: ["plugins"],
        },
      ),
    onSuccess: refreshNav,
  });

  const uninstall = useMutation({
    mutationFn: async (input: { name: string; keepData: boolean }) =>
      runMutation(
        () =>
          api.DELETE("/api/v1/console/plugins/{name}", {
            params: {
              path: { name: input.name },
              query: input.keepData ? { keepData: true } : {},
            },
          }),
        {
          success: input.keepData ? "插件已卸载，数据保留着" : "插件已卸载",
          invalidate: ["plugins"],
        },
      ),
    onSuccess: () => {
      setRemoving(null);
      void refreshNav();
    },
  });

  const purge = useMutation({
    mutationFn: async (name: string) =>
      runMutation(
        () =>
          api.DELETE("/api/v1/console/plugin-data/{name}", {
            params: { path: { name } },
          }),
        { success: "保留的数据已删除", invalidate: ["plugins"] },
      ),
    onSuccess: () => setPurging(null),
  });

  return (
    <>
      <PageHeader
        icon={Puzzle}
        title="插件"
        description="安装与启停插件。启用后插件的设置立即可用，不需要重启。"
        actions={
          <Button variant="primary" onClick={() => setInstalling(true)}>
            <Upload aria-hidden="true" />
            安装插件
          </Button>
        }
      />
      <PageBody>
        <Card>
          <CardHeader title="已安装" description={`共 ${plugins.length} 个`} />
          <CardBody>
            {query.isLoading ? (
              <EntitySkeleton />
            ) : query.error ? (
              <ErrorState
                message={query.error.message}
                onRetry={() => void query.refetch()}
              />
            ) : plugins.length === 0 ? (
              <EmptyState
                icon={Puzzle}
                title="还没有安装插件"
                description="插件是一个 zip 包，里面有清单、设置声明，也可能有一段在沙箱里运行的后端代码。装好之后在这一页启用它、调整设置。"
                action={
                  <Button variant="primary" onClick={() => setInstalling(true)}>
                    <Upload aria-hidden="true" />
                    安装插件
                  </Button>
                }
              />
            ) : (
              <EntityList>
                {plugins.map((plugin) => (
                  <PluginRow
                    key={plugin.name}
                    plugin={plugin}
                    busy={setEnabled.isPending || uninstall.isPending}
                    onToggle={(enabled) => {
                      // 声明了能力而还没确认过的，先让站长看清它要做什么
                      if (enabled && plugin.needsConsent) {
                        setConsentFor(plugin);
                        return;
                      }
                      // 有插件依赖它的，先让站长看清会连带停掉谁
                      if (!enabled && (plugin.dependents?.length ?? 0) > 0) {
                        setStopping(plugin);
                        return;
                      }
                      setEnabled.mutate({ name: plugin.name, enabled });
                    }}
                    onSettings={() => setSettingsFor(plugin.name)}
                    onRemove={() => setRemoving(plugin)}
                  />
                ))}
              </EntityList>
            )}
          </CardBody>
        </Card>

        {retained.length > 0 ? (
          <Card>
            <CardHeader
              title="卸载后保留的数据"
              description="重新安装同名插件，这些数据会原样接回去"
            />
            <CardBody>
              <EntityList>
                {retained.map((item) => (
                  <Entity key={item.name}>
                    <EntityStart>
                      <EntityField>
                        <span className="flex items-center gap-2">
                          <span className="font-medium text-ink">
                            {item.displayName}
                          </span>
                          <Badge tone="neutral">v{item.version}</Badge>
                        </span>
                      </EntityField>
                      <EntityMeta>
                        {dataSummary(item.counts)}，卸载于{" "}
                        {relativeTime(item.retainedAt)}
                      </EntityMeta>
                    </EntityStart>
                    <EntityEnd>
                      <Button
                        variant="secondary"
                        size="sm"
                        onClick={() => setPurging(item)}
                      >
                        <Trash2 aria-hidden="true" />
                        删除数据
                      </Button>
                    </EntityEnd>
                  </Entity>
                ))}
              </EntityList>
            </CardBody>
          </Card>
        ) : null}
      </PageBody>

      <InstallDialog open={installing} onOpenChange={setInstalling} />

      <ConsentDialog
        plugin={consentFor}
        pending={setEnabled.isPending}
        onOpenChange={(open) => {
          if (!open) {
            setConsentFor(null);
          }
        }}
        onConfirm={(name) =>
          setEnabled.mutate(
            { name, enabled: true, accept: true },
            { onSuccess: () => setConsentFor(null) },
          )
        }
      />

      <PluginSettingsDialog
        plugin={settingsFor}
        open={settingsFor !== null}
        onOpenChange={(open) => {
          if (!open) {
            setSettingsFor(null);
          }
        }}
      />

      <UninstallDialog
        plugin={removing}
        pending={uninstall.isPending}
        onOpenChange={(open) => {
          if (!open) {
            setRemoving(null);
          }
        }}
        onConfirm={(keepData) => {
          if (removing) {
            uninstall.mutate({ name: removing.name, keepData });
          }
        }}
      />

      <ConfirmDialog
        open={stopping !== null}
        onOpenChange={(open) => {
          if (!open) {
            setStopping(null);
          }
        }}
        title={`停用「${stopping?.displayName ?? ""}」？`}
        consequence={
          <p>
            {(stopping?.dependents ?? []).join("、")}{" "}
            依赖它，会跟着一并停用。之后重新启用它，
            依赖它的插件也不会自己回来，要在这里各自打开。
          </p>
        }
        confirmLabel="停用"
        destructive={false}
        pending={setEnabled.isPending}
        onConfirm={() => {
          if (stopping) {
            setEnabled.mutate(
              { name: stopping.name, enabled: false },
              { onSuccess: () => setStopping(null) },
            );
          }
        }}
      />

      <ConfirmDialog
        open={purging !== null}
        onOpenChange={(open) => {
          if (!open) {
            setPurging(null);
          }
        }}
        title={`删除「${purging?.displayName ?? ""}」保留的数据？`}
        consequence={
          <p>
            {purging ? dataSummary(purging.counts) : ""}
            会被删掉，无法撤销。以后再装这个插件，就从空白开始。
          </p>
        }
        confirmLabel="删除数据"
        pending={purge.isPending}
        onConfirm={() => {
          if (purging) {
            purge.mutate(purging.name);
          }
        }}
      />
    </>
  );
}

/** 一行插件。 */
function PluginRow({
  plugin,
  busy,
  onToggle,
  onSettings,
  onRemove,
}: {
  plugin: PluginView;
  busy: boolean;
  onToggle: (enabled: boolean) => void;
  onSettings: () => void;
  onRemove: () => void;
}) {
  // 损坏的插件没有可操作的东西：它的文件已经不在了，启停与改设置都无从谈起。
  const broken = Boolean(plugin.broken);
  const hasSettings = (plugin.settingGroups?.length ?? 0) > 0;

  return (
    <Entity>
      <EntityStart>
        {/* 名字这一块不参与收缩：描述一长，flex 会把名字挤成一字一行 */}
        <EntityField className="shrink-0">
          <span className="flex items-center gap-2">
            <span className="font-medium text-ink">{plugin.displayName}</span>
            <Badge tone="neutral">v{plugin.version}</Badge>
            {plugin.runtime === "wasm" ? (
              <Badge tone="outline">带后端</Badge>
            ) : null}
          </span>
        </EntityField>
        {plugin.description ? (
          // 描述是这一行里唯一肯让位的：宽度不够就省略，其余照原样显示
          <span className="min-w-0 truncate text-xs text-ink-muted">
            {plugin.description}
          </span>
        ) : null}
        {(plugin.dependencies?.length ?? 0) > 0 ? (
          // 依赖没就绪是「启不了」的原因，与下面的停用原因同类，故紧挨着放
          <EntityMeta>
            <span className="flex flex-wrap items-center gap-x-3 gap-y-1">
              {(plugin.dependencies ?? []).map((dep) => {
                const note = depNote(dep);
                return (
                  <span
                    key={dep.name}
                    className={cn(
                      "inline-flex items-center gap-1",
                      note ? "text-warn" : "text-ink-muted",
                    )}
                  >
                    {note ? (
                      <AlertTriangle
                        aria-hidden="true"
                        className="size-3.5 shrink-0"
                      />
                    ) : null}
                    依赖 {dep.name}
                    {dep.version ? (
                      <span className="font-mono text-xs">{dep.version}</span>
                    ) : null}
                    {note ? <span>（{note}）</span> : null}
                  </span>
                );
              })}
            </span>
          </EntityMeta>
        ) : null}
        {!plugin.enabled && plugin.disabledReason ? (
          <EntityMeta>
            <span className="flex items-start gap-1.5 text-warn">
              <AlertTriangle
                aria-hidden="true"
                className="mt-0.5 size-3.5 shrink-0"
              />
              {plugin.disabledReason}
            </span>
          </EntityMeta>
        ) : null}
        <EntityMeta>
          {[plugin.author, plugin.license].filter(Boolean).join(" · ")}
        </EntityMeta>
      </EntityStart>

      <EntityEnd>
        {broken ? (
          // 原因要说出来，而不是只给一个红点：用户需要知道自己的插件出了什么事。
          <span className="flex items-center gap-1.5 text-xs text-danger">
            <AlertTriangle aria-hidden="true" className="size-4" />
            {plugin.broken}
          </span>
        ) : (
          <>
            <span className="flex items-center gap-1.5 text-xs text-ink-muted">
              <StatusDot state={plugin.enabled ? "ok" : "neutral"} />
              {plugin.enabled ? "已启用" : "已停用"}
            </span>
            <Switch
              checked={plugin.enabled}
              disabled={busy}
              onCheckedChange={onToggle}
              aria-label={`${plugin.enabled ? "停用" : "启用"} ${plugin.displayName}`}
            />
            <EntityActions label={`${plugin.displayName} 的更多操作`}>
              {hasSettings ? (
                <DropdownMenuItem onSelect={onSettings}>
                  <Settings aria-hidden="true" />
                  设置
                </DropdownMenuItem>
              ) : null}
              {plugin.homepage ? (
                <DropdownMenuItem asChild>
                  <a href={plugin.homepage} target="_blank" rel="noreferrer">
                    <ExternalLink aria-hidden="true" />
                    打开主页
                  </a>
                </DropdownMenuItem>
              ) : null}
              <DropdownMenuItem onSelect={onRemove} className="text-danger">
                <Trash2 aria-hidden="true" />
                卸载
              </DropdownMenuItem>
            </EntityActions>
          </>
        )}
      </EntityEnd>
    </Entity>
  );
}

/** 数据量写成一句话：设置 2 组、记录 30 条、键值 5 个。 */
function dataSummary(counts: DataCounts): string {
  const parts: string[] = [];
  if (counts.settings > 0) {
    parts.push(`设置 ${counts.settings} 组`);
  }
  if (counts.records > 0) {
    parts.push(`记录 ${counts.records} 条`);
  }
  if (counts.kv > 0) {
    parts.push(`键值 ${counts.kv} 个`);
  }
  return parts.length > 0 ? parts.join("、") : "没有数据";
}

/**
 * 卸载确认。
 *
 * 先说清这个插件名下有多少数据，再让站长选：缺省连数据一起删；勾上「保留数据」
 * 则只删程序，数据留在库里，重装同名插件时接回去。
 */
function UninstallDialog({
  plugin,
  pending,
  onOpenChange,
  onConfirm,
}: {
  plugin: PluginView | null;
  pending: boolean;
  onOpenChange: (open: boolean) => void;
  onConfirm: (keepData: boolean) => void;
}) {
  const [keepData, setKeepData] = useState(false);
  const counts = useQuery({
    queryKey: ["plugin-data", plugin?.name],
    enabled: plugin !== null,
    queryFn: async () => {
      const { data, error, response } = await api.GET(
        "/api/v1/console/plugin-data/{name}",
        { params: { path: { name: plugin?.name ?? "" } } },
      );
      if (!response.ok) {
        throw new Error(problemMessage(error));
      }
      return data;
    },
  });
  const hasData =
    counts.data !== undefined &&
    counts.data.settings + counts.data.records + counts.data.kv > 0;
  const summary = counts.isLoading
    ? "正在统计的数据"
    : counts.data
      ? dataSummary(counts.data)
      : "读不出数据量";

  return (
    <ConfirmDialog
      open={plugin !== null}
      onOpenChange={(open) => {
        if (!open) {
          setKeepData(false);
        }
        onOpenChange(open);
      }}
      title={`卸载「${plugin?.displayName ?? ""}」？`}
      consequence={
        <div className="flex flex-col gap-3">
          <p>插件的程序与启用状态会被删掉。它名下现在有：{summary}。</p>
          {plugin && (plugin.dependents?.length ?? 0) > 0 ? (
            <p>
              <strong className="font-medium text-ink">
                {(plugin.dependents ?? []).join("、")}{" "}
                依赖它，卸载后会跟着停用。
              </strong>
              这些插件本身不会被卸掉，但要重新装上它才能再启用。
            </p>
          ) : null}
          {hasData ? (
            <CheckboxRow
              id="uninstall-keep-data"
              checked={keepData}
              onCheckedChange={setKeepData}
              label="保留数据"
              description="只删程序，设置与数据留在库里；重装同名插件时原样接回"
              className="-mx-2"
            />
          ) : null}
          {hasData && !keepData ? (
            <p>
              <strong className="font-medium text-ink">
                数据会一并删除，无法撤销。
              </strong>
            </p>
          ) : null}
        </div>
      }
      confirmLabel="卸载"
      pending={pending}
      onConfirm={() => onConfirm(keepData)}
    />
  );
}

/** 能力类别对应的图标，与后端 capabilityLine.key 一一对应。 */
const CAPABILITY_ICONS: Record<string, LucideIcon> = {
  "content.read": BookOpen,
  "content.write": PenLine,
  http: Globe,
  mail: Mail,
  cron: Clock,
  frontend: LayoutTemplate,
};

/**
 * 启用前的能力确认。
 *
 * 逐条说清插件启用后能做什么、范围到哪（哪些权限、哪些域名），按钮直接写「确认并启用」：
 * 这一步就是站长点头的那一下，不该藏在一个泛泛的「确定」后面。
 */
function ConsentDialog({
  plugin,
  pending,
  onOpenChange,
  onConfirm,
}: {
  plugin: PluginView | null;
  pending: boolean;
  onOpenChange: (open: boolean) => void;
  onConfirm: (name: string) => void;
}) {
  const lines = plugin?.capabilities ?? [];
  return (
    <Dialog open={plugin !== null} onOpenChange={onOpenChange}>
      <DialogContent size="md">
        <DialogHeader>
          <DialogTitle>启用「{plugin?.displayName}」之前</DialogTitle>
          <DialogDescription>
            启用后它可以做下面这些事。以后升级如果多要了能力，插件会先停下来，等你再次确认。
          </DialogDescription>
        </DialogHeader>
        <DialogBody>
          <ul className="flex flex-col divide-y divide-line">
            {lines.map((line) => {
              const Icon = CAPABILITY_ICONS[line.key] ?? ShieldCheck;
              return (
                <li key={line.key} className="flex gap-3 py-2.5">
                  <Icon
                    aria-hidden="true"
                    className="mt-0.5 size-4 shrink-0 text-ink-muted"
                  />
                  <div className="flex min-w-0 flex-col gap-0.5">
                    <span className="text-sm font-medium text-ink">
                      {line.title}
                    </span>
                    <span className="text-xs break-words text-ink-muted">
                      {line.detail}
                    </span>
                  </div>
                </li>
              );
            })}
          </ul>
        </DialogBody>
        <DialogFooter>
          <Button variant="secondary" onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button
            variant="primary"
            loading={pending}
            disabled={!plugin || pending}
            onClick={() => plugin && onConfirm(plugin.name)}
          >
            确认并启用
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/**
 * 安装对话框。
 *
 * 只收 zip 与一条说明：升级走同一个入口（上传同名插件即覆盖），
 * 不额外做一个「升级」按钮——那只是同一个动作的两种说法。
 */
function InstallDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const queryClient = useQueryClient();
  const fileInput = useRef<HTMLInputElement>(null);
  const [file, setFile] = useState<File | null>(null);

  const install = useMutation({
    mutationFn: async () => {
      if (!file) {
        throw new Error("先选择一个 zip 文件");
      }
      const body = new FormData();
      body.append("file", file);
      const { error, response } = await api.POST("/api/v1/console/plugins", {
        /*
         * openapi-fetch 在解析 multipart 请求体时按 schema 生成对象，
         * 但真实上传必须传 FormData 才能带上文件流。
         * `bodySerializer` 直接返回 FormData 即绕开序列化。
         */
        body: body as unknown as { file: string },
        bodySerializer: () => body,
      });
      if (!response.ok) {
        throw new Error(problemMessage(error));
      }
    },
    onSuccess: async () => {
      toast.success("插件已安装", {
        description: "默认未启用。确认无误后再打开它。",
      });
      setFile(null);
      onOpenChange(false);
      await queryClient.invalidateQueries({ queryKey: ["plugins"] });
    },
    onError: (error: Error) => toast.error(error.message),
  });

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) {
          setFile(null);
        }
        onOpenChange(next);
      }}
    >
      <DialogContent size="md">
        <DialogHeader>
          <DialogTitle>安装插件</DialogTitle>
          <DialogDescription>
            上传插件的 zip
            包。同名插件已存在时按升级处理，并保持它原来的启用状态。
          </DialogDescription>
        </DialogHeader>
        <DialogBody className="flex flex-col gap-3">
          <input
            ref={fileInput}
            type="file"
            accept=".zip,application/zip"
            hidden
            onChange={(event) => {
              const picked = event.target.files?.[0];
              if (picked) {
                setFile(picked);
              }
              // 清空以便连续两次选同一个文件也能触发 change
              event.target.value = "";
            }}
          />
          <Button
            variant="secondary"
            onClick={() => fileInput.current?.click()}
          >
            <Upload aria-hidden="true" />
            {file ? "换一个文件" : "选择 zip 文件"}
          </Button>
          {file ? (
            <p className="text-xs text-ink-muted">
              已选择 {file.name}（{(file.size / 1024).toFixed(0)} KB）
            </p>
          ) : null}

          {/* 装完默认不启用，这条要说在前面：插件能改后台的设置与行为，
              一装就生效会让人来不及看一眼它到底装了什么。 */}
          <p className="text-xs text-ink-muted">
            安装后插件处于<span className="text-ink">停用</span>
            状态。确认它的来源与说明之后，再在列表里启用。
          </p>
        </DialogBody>
        <DialogFooter>
          <Button variant="secondary" onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button
            variant="primary"
            disabled={!file || install.isPending}
            loading={install.isPending}
            onClick={() => install.mutate()}
          >
            安装
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
