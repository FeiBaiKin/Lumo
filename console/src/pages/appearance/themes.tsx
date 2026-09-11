import { api, problemMessage } from "@/api/client";
import { runMutation } from "@/api/mutation";
import type { components } from "@/api/schema";
import { ListEmpty, ListPanel } from "@/components/data/list-panel";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/dialog";
import { PageHeader } from "@/components/ui/panel";
import { ErrorState, Skeleton } from "@/components/ui/states";
import { useDocumentTitle } from "@/lib/use-document-title";
import { cn } from "@/lib/utils";
import { ThemeSettings } from "@/pages/appearance/theme-settings";
import { useMutation, useQuery } from "@tanstack/react-query";
import {
  AlertTriangle,
  Check,
  ExternalLink,
  Lock,
  Palette,
  RefreshCw,
  Trash2,
  Upload,
} from "lucide-react";
import { useRef, useState } from "react";

/**
 * 主题管理。
 *
 * 主题能执行任意模板逻辑并决定整站外观，故**全部**操作（含列表）
 * 都要求 themes:manage（见 internal/theme/handler.go）。
 *
 * 卡片上明确标出两类信息，它们决定了这个主题能不能真的用起来：
 *   - 必需四模板是否齐备（缺一个就装不上，但列出已安装主题时仍会显示状态）
 *   - 它声明了几组设置（站长装完后要配的东西在哪）
 *
 * 「内置」主题不可删除，当前启用的主题也不可删除 —— 后者会让站点当场换皮
 * 而站长未必意识到。这两条在按钮上就要说清楚。
 */

type ThemeView = components["schemas"]["View"];

export function ThemesPage() {
  useDocumentTitle("主题");
  const fileInput = useRef<HTMLInputElement>(null);
  const [deleting, setDeleting] = useState<ThemeView | null>(null);
  const [settingsFor, setSettingsFor] = useState<ThemeView | null>(null);
  const [uploadError, setUploadError] = useState("");
  const [overwrite, setOverwrite] = useState(false);

  const query = useQuery({
    queryKey: ["themes"],
    queryFn: async () => {
      const { data, response } = await api.GET("/api/v1/console/themes");
      if (!response.ok) {
        throw new Error(`载入主题失败（HTTP ${response.status}）`);
      }
      return data;
    },
  });

  const themes = query.data?.items ?? [];
  const broken = Object.entries(query.data?.broken ?? {});

  const activate = useMutation({
    mutationFn: (name: string) =>
      runMutation(
        () =>
          api.POST("/api/v1/console/themes/{name}/activate", {
            params: { path: { name } },
          }),
        { success: "已切换主题，前台立即生效", invalidate: ["themes"] },
      ),
    onSuccess: () => void query.refetch(),
  });

  const reload = useMutation({
    mutationFn: (name: string) =>
      runMutation(
        () =>
          api.POST("/api/v1/console/themes/{name}/reload", {
            params: { path: { name } },
          }),
        { success: "模板已重新载入", invalidate: ["themes"] },
      ),
    onSuccess: () => void query.refetch(),
  });

  const remove = useMutation({
    mutationFn: (name: string) =>
      runMutation(
        () =>
          api.DELETE("/api/v1/console/themes/{name}", {
            params: { path: { name } },
          }),
        { success: "主题已卸载", invalidate: ["themes"] },
      ),
    onSuccess: () => void query.refetch(),
  });

  const install = useMutation({
    mutationFn: async (file: File) => {
      const body = new FormData();
      body.append("file", file);
      if (overwrite) {
        body.append("overwrite", "true");
      }
      const { error, response } = await api.POST("/api/v1/console/themes", {
        body: body as unknown as { file: string },
        bodySerializer: () => body,
      });
      if (!response.ok) {
        throw new Error(problemMessage(error));
      }
    },
    onSuccess: () => {
      setUploadError("");
      void query.refetch();
    },
    onError: (err) =>
      setUploadError(err instanceof Error ? err.message : "上传失败"),
  });

  return (
    <>
      <PageHeader
        title="主题"
        description="主题是一个 zip 包，上传即安装；切换后前台立即换皮"
        actions={
          <>
            <input
              ref={fileInput}
              type="file"
              accept=".zip,application/zip"
              hidden
              onChange={(e) => {
                const file = e.target.files?.[0];
                if (file) {
                  setUploadError("");
                  install.mutate(file);
                }
                e.target.value = "";
              }}
            />
            <label className="flex items-center gap-2 text-xs text-ink-muted">
              <input
                type="checkbox"
                checked={overwrite}
                onChange={(e) => setOverwrite(e.target.checked)}
                className="size-3.5 cursor-pointer rounded-[3px] border-line-strong accent-seal"
              />
              覆盖同名主题
            </label>
            <Button
              variant="primary"
              disabled={install.isPending}
              onClick={() => fileInput.current?.click()}
            >
              <Upload aria-hidden="true" />
              {install.isPending ? "正在安装" : "上传主题"}
            </Button>
          </>
        }
      />

      {uploadError ? (
        <div
          role="alert"
          className="mb-4 rounded-panel border border-danger bg-danger-soft px-4 py-3"
        >
          <p className="text-sm font-medium text-danger">主题包未被接受</p>
          <p className="token mt-0.5 text-xs text-danger">{uploadError}</p>
          <p className="mt-1 text-xs text-danger">
            安装是两段式的：先解到临时目录校验（模板能否解析、必需模板是否齐备、
            设置声明能否编译），通过后才就位。失败不会在 themes/
            里留下半个主题。
          </p>
        </div>
      ) : null}

      {/* 加载失败的主题单独列出：它们的目录还在，只是加载不出来 */}
      {broken.length > 0 ? (
        <div className="mb-4 rounded-panel border border-warn bg-warn-soft px-4 py-3">
          <p className="flex items-center gap-2 text-sm font-medium text-warn">
            <AlertTriangle aria-hidden="true" className="size-4" />有{" "}
            {broken.length} 个主题加载失败
          </p>
          <ul className="mt-1 flex flex-col gap-0.5">
            {broken.map(([name, reason]) => (
              <li key={name} className="text-xs text-warn">
                <code>{name}</code>：{reason}
              </li>
            ))}
          </ul>
        </div>
      ) : null}

      {query.isLoading ? (
        <div className="grid gap-4 lg:grid-cols-2">
          <Skeleton className="h-40 w-full" />
          <Skeleton className="h-40 w-full" />
        </div>
      ) : query.error ? (
        <ErrorState
          message={query.error.message}
          onRetry={() => void query.refetch()}
        />
      ) : themes.length === 0 ? (
        <ListPanel>
          <ListEmpty
            icon={Palette}
            title="没有已安装的主题"
            description="这不太正常 —— 内置主题应当始终存在。请检查服务端日志。"
          />
        </ListPanel>
      ) : (
        <div className="grid gap-4 lg:grid-cols-2">
          {themes.map((theme) => (
            <ThemeCard
              key={theme.name}
              theme={theme}
              onActivate={() => activate.mutate(theme.name)}
              onReload={() => reload.mutate(theme.name)}
              onDelete={() => setDeleting(theme)}
              onSettings={() => setSettingsFor(theme)}
              pending={activate.isPending || reload.isPending}
            />
          ))}
        </div>
      )}

      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
        title={`卸载主题「${deleting?.label || deleting?.name}」？`}
        consequence={
          <p>
            <strong className="font-medium text-ink">这一步无法撤销。</strong>
            主题目录与它的设置会一并删除。卸载后站点回退到内置主题。
          </p>
        }
        confirmLabel="卸载"
        pending={remove.isPending}
        onConfirm={async () => {
          if (!deleting) {
            return;
          }
          await remove.mutateAsync(deleting.name).catch(() => {});
          setDeleting(null);
        }}
      />

      {settingsFor ? (
        <ThemeSettings
          theme={settingsFor}
          onClose={() => setSettingsFor(null)}
        />
      ) : null}
    </>
  );
}

/** 一个主题卡片。 */
function ThemeCard({
  theme,
  onActivate,
  onReload,
  onDelete,
  onSettings,
  pending,
}: {
  theme: ThemeView;
  onActivate: () => void;
  onReload: () => void;
  onDelete: () => void;
  onSettings: () => void;
  pending: boolean;
}) {
  const missingRequired = (theme.templates ?? []).filter(
    (t) => t.required && !t.provided,
  );
  const deletable = !theme.builtin && !theme.active;

  return (
    <ListPanel
      title={theme.label || theme.name}
      badge={
        theme.active ? (
          <Badge tone="ok">
            <Check aria-hidden="true" />
            使用中
          </Badge>
        ) : theme.builtin ? (
          <Badge tone="outline">
            <Lock aria-hidden="true" />
            内置
          </Badge>
        ) : null
      }
      description={`${theme.name} · ${theme.version}${theme.author ? ` · ${theme.author}` : ""}`}
    >
      <div className="flex flex-col gap-3 border-line border-t p-4">
        {theme.description ? (
          <p className="text-sm text-ink-muted">{theme.description}</p>
        ) : null}

        <div className="flex flex-col gap-2">
          <div className="flex flex-wrap items-center gap-1.5">
            <span className="w-20 shrink-0 text-xs text-ink-muted">
              必需模板
            </span>
            {(theme.templates ?? [])
              .filter((t) => t.required)
              .map((t) => (
                <Badge key={t.name} tone={t.provided ? "ok" : "danger"}>
                  {t.name}
                </Badge>
              ))}
          </div>

          {(theme.templates ?? []).some((t) => !t.required && t.provided) ? (
            <div className="flex flex-wrap items-center gap-1.5">
              <span className="w-20 shrink-0 text-xs text-ink-muted">
                可选模板
              </span>
              {(theme.templates ?? [])
                .filter((t) => !t.required && t.provided)
                .map((t) => (
                  <Badge key={t.name} tone="outline">
                    {t.name}
                  </Badge>
                ))}
            </div>
          ) : (
            <p className="text-xs text-ink-subtle">
              未提供可选模板，缺的页面会整页回退到内置主题
            </p>
          )}

          {(theme.settingGroups ?? []).length > 0 ? (
            <div className="flex flex-wrap items-center gap-1.5">
              <span className="w-20 shrink-0 text-xs text-ink-muted">
                设置分组
              </span>
              {(theme.settingGroups ?? []).map((group) => (
                <Badge key={group} tone="seal">
                  {group}
                </Badge>
              ))}
            </div>
          ) : null}
        </div>

        {missingRequired.length > 0 ? (
          <p className="flex items-start gap-1.5 rounded-control border border-danger bg-danger-soft px-2.5 py-1.5 text-xs text-danger">
            <AlertTriangle
              aria-hidden="true"
              className="mt-0.5 size-3.5 shrink-0"
            />
            缺少必需模板 {missingRequired.map((t) => t.name).join("、")}，
            这个主题无法启用。
          </p>
        ) : null}

        <div className="flex flex-wrap items-center gap-2">
          {theme.active ? (
            <Button
              variant="ghost"
              size="sm"
              onClick={onReload}
              disabled={pending}
            >
              <RefreshCw
                aria-hidden="true"
                className={cn(pending && "animate-spin")}
              />
              重新载入模板
            </Button>
          ) : (
            <Button
              variant="primary"
              size="sm"
              onClick={onActivate}
              disabled={pending || missingRequired.length > 0}
            >
              启用
            </Button>
          )}

          {theme.settingGroups && theme.settingGroups.length > 0 ? (
            <Button variant="secondary" size="sm" onClick={onSettings}>
              主题设置
            </Button>
          ) : null}

          {theme.homepage ? (
            <Button variant="ghost" size="sm" asChild>
              <a
                href={theme.homepage}
                target="_blank"
                rel="noopener noreferrer"
              >
                <ExternalLink aria-hidden="true" />
                主页
              </a>
            </Button>
          ) : null}

          <div className="flex-1" />

          {deletable ? (
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={onDelete}
              aria-label={`卸载 ${theme.label || theme.name}`}
              title="卸载"
              className="hover:text-danger"
            >
              <Trash2 aria-hidden="true" />
            </Button>
          ) : (
            <span
              className="text-xs text-ink-subtle"
              title={
                theme.builtin
                  ? "内置主题是回退目标，不可卸载"
                  : "当前启用的主题不可卸载 —— 那会让站点当场换皮"
              }
            >
              {theme.builtin ? "内置，不可卸载" : "使用中，不可卸载"}
            </span>
          )}
        </div>
      </div>
    </ListPanel>
  );
}
