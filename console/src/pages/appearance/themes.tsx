import { api, problemMessage } from "@/api/client";
import { runMutation } from "@/api/mutation";
import type { components } from "@/api/schema";
import {
  Entity,
  EntityEnd,
  EntityField,
  EntityList,
  EntityStart,
  ListEmpty,
} from "@/components/data/entity";
import { PageBody, PageHeader } from "@/components/layout/page-header";
import { Alert } from "@/components/ui/alert";
import { Avatar } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
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
import { EntitySkeleton, ErrorState, Skeleton } from "@/components/ui/states";
import { StatusDot } from "@/components/ui/status-dot";
import { Tabbar } from "@/components/ui/tabs";
import { CheckboxRow } from "@/components/ui/toggle";
import { fileSize } from "@/lib/format";
import { useDocumentTitle } from "@/lib/use-document-title";
import {
  ThemeSettingsPanel,
  useThemeSettings,
} from "@/pages/appearance/theme-settings";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Check,
  ExternalLink,
  Lock,
  Palette,
  RefreshCw,
  Trash2,
  Upload,
} from "lucide-react";
import { useRef, useState } from "react";
import { toast } from "sonner";

/**
 * 主题管理（形态对齐 Halo 的主题页：左列表、右详情）。
 *
 * 主题能执行任意模板逻辑并决定整站外观，故**全部**操作（含列表）
 * 都要求 themes:manage（见 internal/theme/handler.go）。
 *
 * 一张卡片分两栏：左边是已安装主题的实体行，点一下就切到它；右边是该主题的
 * 详情与设置分组，用标签栏切换。详情里明确标出两类信息，它们决定了
 * 这个主题能不能真的用起来：必需四模板是否齐备、它声明了几组设置。
 *
 * 「内置」主题不可删除，当前启用的主题也不可删除 —— 后者会让站点当场换皮
 * 而站长未必意识到。这两条在按钮的位置上就要说清楚。
 */

type ThemeView = components["schemas"]["View"];

const DETAIL_TAB = "detail";

export function ThemesPage() {
  useDocumentTitle("主题");
  const [selectedName, setSelectedName] = useState<string | null>(null);
  const [tab, setTab] = useState(DETAIL_TAB);
  const [installOpen, setInstallOpen] = useState(false);
  const [deleting, setDeleting] = useState<ThemeView | null>(null);

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

  // 默认选中正在使用的主题：站长进来多半是要调它
  const current =
    themes.find((theme) => theme.name === selectedName) ??
    themes.find((theme) => theme.active) ??
    themes[0];

  // 分组的显示名来自设置接口；没拿到之前先用分组名顶着
  const settings = useThemeSettings(current?.name ?? "", current !== undefined);
  const groupLabel = (name: string) =>
    (settings.data ?? []).find((group) => group.name === name)?.label || name;

  const tabs = [
    { value: DETAIL_TAB, label: "详情" },
    ...(current?.settingGroups ?? []).map((group) => ({
      value: `group:${group}`,
      label: groupLabel(group),
    })),
  ];
  // 切换主题后上一个主题的分组标签可能不存在，退回详情
  const activeTab = tabs.some((item) => item.value === tab) ? tab : DETAIL_TAB;

  function select(name: string) {
    setSelectedName(name);
    setTab(DETAIL_TAB);
  }

  const remove = useMutation({
    mutationFn: (name: string) =>
      runMutation(
        () =>
          api.DELETE("/api/v1/console/themes/{name}", {
            params: { path: { name } },
          }),
        { success: "主题已卸载", invalidate: ["themes"] },
      ),
    onSuccess: () => {
      setSelectedName(null);
      void query.refetch();
    },
  });

  return (
    <>
      <PageHeader
        icon={Palette}
        title="主题"
        description="主题是一个 zip 包，上传即安装；切换后前台立即换皮"
        actions={
          <Button variant="primary" onClick={() => setInstallOpen(true)}>
            <Upload aria-hidden="true" />
            上传主题
          </Button>
        }
      />

      <PageBody>
        {/* 加载失败的主题单独列出：它们的目录还在，只是加载不出来 */}
        {broken.length > 0 ? (
          <Alert tone="warn" title={`有 ${broken.length} 个主题加载失败`}>
            <ul className="flex flex-col gap-0.5">
              {broken.map(([name, reason]) => (
                <li key={name} className="token">
                  <code>{name}</code>：{reason}
                </li>
              ))}
            </ul>
          </Alert>
        ) : null}

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
        ) : themes.length === 0 || !current ? (
          <Card>
            <ListEmpty
              icon={Palette}
              title="没有已安装的主题"
              description="这不太正常，内置主题应当始终存在。请检查服务端日志。"
            />
          </Card>
        ) : (
          <Card className="flex flex-col overflow-hidden md:flex-row">
            <div className="border-line border-b md:w-72 md:shrink-0 md:border-r md:border-b-0">
              <EntityList>
                {themes.map((theme) => {
                  const selected = theme.name === current.name;
                  return (
                    <Entity
                      key={theme.name}
                      selected={selected}
                      className="cursor-pointer"
                      onClick={() => select(theme.name)}
                    >
                      <EntityStart>
                        <Avatar
                          square
                          name={theme.label || theme.name}
                          size="sm"
                        />
                        <EntityField
                          title={
                            <button
                              type="button"
                              onClick={() => select(theme.name)}
                              aria-current={selected ? "true" : undefined}
                              className="truncate text-left"
                            >
                              {theme.label || theme.name}
                            </button>
                          }
                          description={<span>版本 {theme.version}</span>}
                        />
                      </EntityStart>
                      <EntityEnd>
                        {theme.active ? (
                          <StatusDot state="ok">使用中</StatusDot>
                        ) : theme.builtin ? (
                          <StatusDot state="neutral">内置</StatusDot>
                        ) : null}
                      </EntityEnd>
                    </Entity>
                  );
                })}
              </EntityList>
            </div>

            <div className="min-w-0 flex-1">
              <Tabbar
                ariaLabel="主题详情与设置"
                items={tabs}
                value={activeTab}
                onChange={setTab}
                className="px-2"
              />
              {activeTab === DETAIL_TAB ? (
                <ThemeDetail
                  theme={current}
                  onDelete={() => setDeleting(current)}
                  onChanged={() => void query.refetch()}
                />
              ) : (
                <ThemeSettingsPanel
                  key={`${current.name}:${activeTab}`}
                  theme={current}
                  group={activeTab.slice("group:".length)}
                />
              )}
            </div>
          </Card>
        )}
      </PageBody>

      <InstallDialog
        open={installOpen}
        onClose={() => setInstallOpen(false)}
        onInstalled={(name) => {
          setSelectedName(name);
          setTab(DETAIL_TAB);
          void query.refetch();
        }}
      />

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
    </>
  );
}

/** 详情标签：标题区与动作、元信息、模板齐备情况。 */
function ThemeDetail({
  theme,
  onDelete,
  onChanged,
}: {
  theme: ThemeView;
  onDelete: () => void;
  onChanged: () => void;
}) {
  const activate = useMutation({
    mutationFn: () =>
      runMutation(
        () =>
          api.POST("/api/v1/console/themes/{name}/activate", {
            params: { path: { name: theme.name } },
          }),
        { success: "已切换主题，前台立即生效", invalidate: ["themes"] },
      ),
    onSuccess: onChanged,
  });

  const reload = useMutation({
    mutationFn: () =>
      runMutation(
        () =>
          api.POST("/api/v1/console/themes/{name}/reload", {
            params: { path: { name: theme.name } },
          }),
        { success: "模板已重新载入", invalidate: ["themes"] },
      ),
    onSuccess: onChanged,
  });

  const templates = theme.templates ?? [];
  const required = templates.filter((item) => item.required);
  const optional = templates.filter((item) => !item.required && item.provided);
  const missing = required.filter((item) => !item.provided);
  const deletable = !theme.builtin && !theme.active;
  const label = theme.label || theme.name;

  return (
    <div className="flex flex-col gap-5 p-4">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="flex min-w-0 items-center gap-3">
          <Avatar square name={label} size="md" />
          <div className="flex min-w-0 flex-col gap-1">
            <div className="flex flex-wrap items-center gap-2">
              <h2 className="text-lg font-semibold text-ink">{label}</h2>
              {theme.active ? (
                <Badge tone="ok">
                  <Check aria-hidden="true" />
                  使用中
                </Badge>
              ) : null}
              {theme.builtin ? (
                <Badge tone="outline">
                  <Lock aria-hidden="true" />
                  内置
                </Badge>
              ) : null}
            </div>
            <p className="text-xs text-ink-muted">
              <code className="token">{theme.name}</code>
              <span className="ml-2">版本 {theme.version}</span>
            </p>
          </div>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          {theme.active ? (
            <Button
              variant="secondary"
              size="sm"
              loading={reload.isPending}
              onClick={() => reload.mutate()}
            >
              <RefreshCw aria-hidden="true" />
              重新载入模板
            </Button>
          ) : (
            <Button
              variant="primary"
              size="sm"
              loading={activate.isPending}
              disabled={missing.length > 0}
              onClick={() => activate.mutate()}
            >
              启用
            </Button>
          )}
          {theme.homepage ? (
            <Button variant="secondary" size="sm" asChild>
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
          {deletable ? (
            <Button variant="danger" size="sm" onClick={onDelete}>
              <Trash2 aria-hidden="true" />
              卸载
            </Button>
          ) : (
            <span className="self-center text-xs text-ink-muted">
              {theme.builtin
                ? "内置主题是回退目标，不可卸载"
                : "使用中的主题不可卸载，先切到别的主题"}
            </span>
          )}
        </div>
      </div>

      {missing.length > 0 ? (
        <Alert tone="warn" title="缺少必需模板，这个主题无法启用">
          缺 {missing.map((item) => item.name).join("、")}。 必需的四个模板是
          index、post、page 与 404，主题包里必须齐备。
        </Alert>
      ) : null}

      <DescriptionList>
        <DescriptionTerm>标识</DescriptionTerm>
        <DescriptionDetail>
          <code className="token">{theme.name}</code>
        </DescriptionDetail>
        <DescriptionTerm>版本</DescriptionTerm>
        <DescriptionDetail className="tabular">
          {theme.version}
        </DescriptionDetail>
        <DescriptionTerm>作者</DescriptionTerm>
        <DescriptionDetail>{theme.author || "未署名"}</DescriptionDetail>
        <DescriptionTerm>许可证</DescriptionTerm>
        <DescriptionDetail>{theme.license || "未声明"}</DescriptionDetail>
        <DescriptionTerm>主页</DescriptionTerm>
        <DescriptionDetail>
          {theme.homepage ? (
            <a
              href={theme.homepage}
              target="_blank"
              rel="noopener noreferrer"
              className="token text-seal hover:underline"
            >
              {theme.homepage}
            </a>
          ) : (
            <span className="text-ink-subtle">未提供</span>
          )}
        </DescriptionDetail>
        <DescriptionTerm>仓库</DescriptionTerm>
        <DescriptionDetail>
          {theme.repo ? (
            <a
              href={theme.repo}
              target="_blank"
              rel="noopener noreferrer"
              className="token text-seal hover:underline"
            >
              {theme.repo}
            </a>
          ) : (
            <span className="text-ink-subtle">未提供</span>
          )}
        </DescriptionDetail>
        <DescriptionTerm>描述</DescriptionTerm>
        <DescriptionDetail>
          {theme.description || <span className="text-ink-subtle">未提供</span>}
        </DescriptionDetail>
        <DescriptionTerm>设置分组</DescriptionTerm>
        <DescriptionDetail className="tabular">
          {(theme.settingGroups ?? []).length === 0
            ? "这个主题没有声明设置项"
            : `${(theme.settingGroups ?? []).length} 组，在上方标签里修改`}
        </DescriptionDetail>
      </DescriptionList>

      <div className="flex flex-col gap-2 border-line border-t pt-4">
        <div className="flex flex-wrap items-center gap-1.5">
          <span className="w-20 shrink-0 text-xs text-ink-muted">必需模板</span>
          {required.map((item) => (
            <Badge key={item.name} tone={item.provided ? "ok" : "danger"}>
              {item.name}
            </Badge>
          ))}
        </div>
        <div className="flex flex-wrap items-center gap-1.5">
          <span className="w-20 shrink-0 text-xs text-ink-muted">可选模板</span>
          {optional.length > 0 ? (
            optional.map((item) => (
              <Badge key={item.name} tone="outline">
                {item.name}
              </Badge>
            ))
          ) : (
            <span className="text-xs text-ink-subtle">
              未提供，缺的页面会整页回退到内置主题
            </span>
          )}
        </div>
        {(theme.pageTemplates ?? []).length > 0 ? (
          <div className="flex flex-wrap items-center gap-1.5">
            <span className="w-20 shrink-0 text-xs text-ink-muted">
              页面模板
            </span>
            {(theme.pageTemplates ?? []).map((name) => (
              <Badge key={name} tone="neutral">
                {name}
              </Badge>
            ))}
          </div>
        ) : null}
      </div>
    </div>
  );
}

/** 安装主题：选一个 zip 包，可选覆盖同名主题。 */
function InstallDialog({
  open,
  onClose,
  onInstalled,
}: {
  open: boolean;
  onClose: () => void;
  onInstalled: (name: string) => void;
}) {
  const queryClient = useQueryClient();
  const fileInput = useRef<HTMLInputElement>(null);
  const [file, setFile] = useState<File | null>(null);
  const [overwrite, setOverwrite] = useState(false);
  const [error, setError] = useState("");

  const install = useMutation({
    mutationFn: async () => {
      if (!file) {
        throw new Error("先选择一个 zip 文件");
      }
      const body = new FormData();
      body.append("file", file);
      if (overwrite) {
        body.append("overwrite", "true");
      }
      const {
        data,
        error: problem,
        response,
      } = await api.POST("/api/v1/console/themes", {
        /*
         * openapi-fetch 在解析 multipart 请求体时按 schema 生成对象，
         * 但真实上传必须传 FormData 才能带上文件流。
         * `bodySerializer` 直接返回 FormData 即绕开序列化。
         */
        body: body as unknown as { file: string },
        bodySerializer: () => body,
      });
      if (!response.ok) {
        throw new Error(problemMessage(problem));
      }
      return data;
    },
    onSuccess: (data) => {
      toast.success("主题已安装");
      void queryClient.invalidateQueries({ queryKey: ["themes"] });
      onInstalled(data?.name ?? "");
      reset();
      onClose();
    },
    onError: (err) => setError(err instanceof Error ? err.message : "安装失败"),
  });

  function reset() {
    setFile(null);
    setOverwrite(false);
    setError("");
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) {
          reset();
          onClose();
        }
      }}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle>上传主题</DialogTitle>
          <DialogDescription>
            主题是一个 zip 包，包含 theme.yaml、templates 与 static。
            安装是两段式的：先解到临时目录校验，通过后才就位。
          </DialogDescription>
        </DialogHeader>
        <DialogBody className="flex flex-col gap-4">
          {error ? (
            <Alert tone="danger" title="主题包未被接受">
              <p className="token">{error}</p>
              <p className="mt-1">
                校验的是模板能否解析、必需模板是否齐备、设置声明能否编译。
                失败不会在主题目录里留下半个主题。
              </p>
            </Alert>
          ) : null}

          <input
            ref={fileInput}
            type="file"
            accept=".zip,application/zip"
            hidden
            onChange={(event) => {
              const picked = event.target.files?.[0];
              if (picked) {
                setFile(picked);
                setError("");
              }
              // 清空以便连续两次选同一个文件也能触发 change
              event.target.value = "";
            }}
          />
          <button
            type="button"
            onClick={() => fileInput.current?.click()}
            className="transition-ui flex flex-col items-center gap-2 rounded-control border border-line-strong border-dashed bg-surface-raised px-4 py-8 text-center hover:border-seal"
          >
            <Upload aria-hidden="true" className="size-6 text-ink-subtle" />
            {file ? (
              <>
                <span className="token text-base font-medium text-ink">
                  {file.name}
                </span>
                <span className="text-xs text-ink-muted">
                  {fileSize(file.size)}，点击可更换
                </span>
              </>
            ) : (
              <>
                <span className="text-base font-medium text-ink">
                  选择 zip 文件
                </span>
                <span className="text-xs text-ink-muted">
                  解压后单文件不超过 8 MiB，总量不超过 64 MiB
                </span>
              </>
            )}
          </button>

          <CheckboxRow
            id="theme-overwrite"
            checked={overwrite}
            onCheckedChange={setOverwrite}
            label="覆盖同名主题"
            description="已安装同名主题时用这个包替换它；不勾选则拒绝安装"
            className="-mx-2"
          />
        </DialogBody>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>
            取消
          </Button>
          <Button
            variant="primary"
            loading={install.isPending}
            disabled={!file}
            onClick={() => install.mutate()}
          >
            安装
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
