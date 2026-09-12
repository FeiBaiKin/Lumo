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
import { Switch } from "@/components/ui/toggle";
import { useDocumentTitle } from "@/lib/use-document-title";
import { PluginSettingsDialog } from "@/pages/system/plugin-settings";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AlertTriangle,
  ExternalLink,
  Puzzle,
  Settings,
  Trash2,
  Upload,
} from "lucide-react";
import { useRef, useState } from "react";
import { toast } from "sonner";

/**
 * 插件管理。
 *
 * 第一期的插件是**纯声明式**的：包内有清单与设置声明，没有可执行代码。
 * 这一页因此只做四件事：装、卸、启用/停用、改设置。插件的自定义页面与
 * 扩展点要等后面几期（agent.md §14.2）。
 *
 * 与主题页的差别值得说明：主题**同一时刻只有一个生效**，所以那边是「选一个」；
 * 插件可以同时启用多个，所以这里是逐个开合。这个差别来自产品语义，不是排版偏好。
 */

type PluginView = components["schemas"]["PluginView"];

export function PluginsPage() {
  useDocumentTitle("插件");
  const [installing, setInstalling] = useState(false);
  const [removing, setRemoving] = useState<PluginView | null>(null);
  const [settingsFor, setSettingsFor] = useState<string | null>(null);

  const query = useQuery({
    queryKey: ["plugins"],
    queryFn: async (): Promise<PluginView[]> => {
      const { data, error, response } = await api.GET(
        "/api/v1/console/plugins",
      );
      if (!response.ok) {
        throw new Error(problemMessage(error));
      }
      return data?.items ?? [];
    },
  });

  const plugins = query.data ?? [];

  const setEnabled = useMutation({
    mutationFn: async (input: { name: string; enabled: boolean }) =>
      runMutation(
        () =>
          api.PUT("/api/v1/console/plugins/{name}/enabled", {
            params: { path: { name: input.name } },
            body: { enabled: input.enabled },
          }),
        {
          success: input.enabled ? "插件已启用" : "插件已停用",
          invalidate: ["plugins"],
        },
      ),
  });

  const uninstall = useMutation({
    mutationFn: async (name: string) =>
      runMutation(
        () =>
          api.DELETE("/api/v1/console/plugins/{name}", {
            params: { path: { name } },
          }),
        { success: "插件已卸载", invalidate: ["plugins"] },
      ),
    onSuccess: () => setRemoving(null),
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
          <CardHeader>
            <h2 className="text-base font-medium text-ink">已安装</h2>
            <p className="text-xs text-ink-muted">共 {plugins.length} 个</p>
          </CardHeader>
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
                description="插件是 zip 包，里面是清单与设置声明。装好之后这一页可以启停它们并调整设置。"
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
                    onToggle={(enabled) =>
                      setEnabled.mutate({ name: plugin.name, enabled })
                    }
                    onSettings={() => setSettingsFor(plugin.name)}
                    onRemove={() => setRemoving(plugin)}
                  />
                ))}
              </EntityList>
            )}
          </CardBody>
        </Card>
      </PageBody>

      <InstallDialog open={installing} onOpenChange={setInstalling} />

      <PluginSettingsDialog
        plugin={settingsFor}
        open={settingsFor !== null}
        onOpenChange={(open) => {
          if (!open) {
            setSettingsFor(null);
          }
        }}
      />

      <ConfirmDialog
        open={removing !== null}
        onOpenChange={(open) => {
          if (!open) {
            setRemoving(null);
          }
        }}
        title={`卸载 ${removing?.displayName ?? ""}`}
        consequence="插件目录、启用状态与它的全部设置都会被删除，无法撤销。"
        confirmLabel="卸载"
        destructive
        pending={uninstall.isPending}
        onConfirm={() => {
          if (removing) {
            uninstall.mutate(removing.name);
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
        <EntityField>
          <span className="flex items-center gap-2">
            <span className="font-medium text-ink">{plugin.displayName}</span>
            <Badge tone="neutral">v{plugin.version}</Badge>
          </span>
        </EntityField>
        {plugin.description ? (
          <EntityMeta>{plugin.description}</EntityMeta>
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
