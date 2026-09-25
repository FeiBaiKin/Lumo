import { api, problemMessage } from "@/api/client";
import { runMutation } from "@/api/mutation";
import type { components } from "@/api/schema";
import { ListEmpty } from "@/components/data/entity";
import { PageBody, PageHeader } from "@/components/layout/page-header";
import { Alert } from "@/components/ui/alert";
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
import { ErrorState, Skeleton } from "@/components/ui/states";
import { CheckboxRow } from "@/components/ui/toggle";
import { fileSize } from "@/lib/format";
import { useDocumentTitle } from "@/lib/use-document-title";
import { cn } from "@/lib/utils";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Check,
  Lock,
  Palette,
  Plus,
  RefreshCw,
  RotateCcw,
  SlidersHorizontal,
  Trash2,
  Upload,
} from "lucide-react";
import { useRef, useState } from "react";
import { Link } from "react-router";
import { toast } from "sonner";

/**
 * 主题管理（形态对齐 WordPress 的「外观 → 主题」）。
 *
 * 主题能执行任意模板逻辑并决定整站外观，故**全部**操作（含列表）
 * 都要求 themes:manage（见 internal/theme/handler.go）。
 *
 * 一页一张网格：每个主题一张卡片，特色图在上、名称栏在下；使用中的排第一，
 * 名称栏反色并带「设置」入口；末尾一格是上传。点特色图弹出详情，启用、卸载、
 * 恢复出厂都在那里。主题设置是单独一页（theme-settings.tsx），表单铺满工作区。
 *
 * 「内置」主题不可删除，当前启用的主题也不可删除 —— 后者会让站点当场换皮
 * 而站长未必意识到。这两条在按钮的位置上就要说清楚。
 */

type ThemeView = components["schemas"]["View"];

/**
 * auto-fill 而不是 auto-fit：主题只有两三个时卡片保持原尺寸，
 * 不会被拉宽成占满整行的大图。
 */
const GRID_CLASS = "grid grid-cols-[repeat(auto-fill,minmax(16rem,1fr))] gap-4";

/** 已安装主题列表。主题页与主题设置页共用这一份缓存。 */
export function useThemes() {
  return useQuery({
    queryKey: ["themes"],
    queryFn: async () => {
      const { data, response } = await api.GET("/api/v1/console/themes");
      if (!response.ok) {
        throw new Error(`载入主题失败（HTTP ${response.status}）`);
      }
      return data;
    },
  });
}

/** 主题设置页的地址。 */
export function themeSettingsPath(name: string) {
  return `/themes/${encodeURIComponent(name)}`;
}

export function ThemesPage() {
  useDocumentTitle("主题");
  const [detailName, setDetailName] = useState<string | null>(null);
  const [installOpen, setInstallOpen] = useState(false);
  const [deleting, setDeleting] = useState<ThemeView | null>(null);

  const query = useThemes();
  const items = query.data?.items ?? [];
  // 使用中的排第一：站长进来多半是要调它
  const themes = [
    ...items.filter((theme) => theme.active),
    ...items.filter((theme) => !theme.active),
  ];
  const broken = Object.entries(query.data?.broken ?? {});
  // 按名字取：启用、重载之后列表重取，弹窗里跟着显示新状态
  const detail = themes.find((theme) => theme.name === detailName) ?? null;

  const remove = useMutation({
    mutationFn: (name: string) =>
      runMutation(
        () =>
          api.DELETE("/api/v1/console/themes/{name}", {
            params: { path: { name } },
          }),
        { success: "主题已卸载", invalidate: ["themes"] },
      ),
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
          <ul className={GRID_CLASS} aria-busy="true">
            {[0, 1, 2].map((i) => (
              <li
                key={i}
                className="overflow-hidden rounded-card border border-line bg-surface"
              >
                <Skeleton className="aspect-[4/3] w-full rounded-none" />
                <div className="flex flex-col gap-1.5 px-3 py-2.5">
                  <Skeleton className="h-4 w-24" />
                  <Skeleton className="h-3 w-16" />
                </div>
              </li>
            ))}
          </ul>
        ) : query.error ? (
          <Card>
            <ErrorState
              message={query.error.message}
              onRetry={() => void query.refetch()}
            />
          </Card>
        ) : themes.length === 0 ? (
          <Card>
            <ListEmpty
              icon={Palette}
              title="没有已安装的主题"
              description="这不太正常，内置主题应当始终存在。请检查服务端日志。"
            />
          </Card>
        ) : (
          <ul className={GRID_CLASS}>
            {themes.map((theme) => (
              <ThemeCard
                key={theme.name}
                theme={theme}
                onOpen={() => setDetailName(theme.name)}
              />
            ))}
            <li>
              <button
                type="button"
                onClick={() => setInstallOpen(true)}
                className="transition-ui flex size-full min-h-48 flex-col items-center justify-center gap-2 rounded-card border border-line-strong border-dashed px-4 py-8 text-center hover:border-seal"
              >
                <Plus aria-hidden="true" className="size-6 text-ink-subtle" />
                <span className="text-sm font-medium text-ink">上传主题</span>
                <span className="text-xs text-ink-muted">
                  zip 包，上传即安装
                </span>
              </button>
            </li>
          </ul>
        )}
      </PageBody>

      <Dialog
        open={detail !== null}
        onOpenChange={(open) => !open && setDetailName(null)}
      >
        {detail ? (
          <ThemeDetail
            key={detail.name}
            theme={detail}
            onDelete={() => {
              setDetailName(null);
              setDeleting(detail);
            }}
          />
        ) : null}
      </Dialog>

      <InstallDialog
        open={installOpen}
        onClose={() => setInstallOpen(false)}
        onInstalled={(name) => setDetailName(name || null)}
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

function useActivate(theme: ThemeView) {
  return useMutation({
    mutationFn: () =>
      runMutation(
        () =>
          api.POST("/api/v1/console/themes/{name}/activate", {
            params: { path: { name: theme.name } },
          }),
        { success: "已切换主题，前台立即生效", invalidate: ["themes"] },
      ),
  });
}

function missingTemplates(theme: ThemeView) {
  return (theme.templates ?? []).filter(
    (item) => item.required && !item.provided,
  );
}

/** 网格里的一张主题卡：点特色图看详情，名称栏只放最常用的一个动作。 */
function ThemeCard({
  theme,
  onOpen,
}: {
  theme: ThemeView;
  onOpen: () => void;
}) {
  const label = theme.label || theme.name;
  const activate = useActivate(theme);
  const blocked = missingTemplates(theme).length > 0;
  const hasSettings = (theme.settingGroups ?? []).length > 0;

  return (
    <li className="group/entity flex flex-col overflow-hidden rounded-card border border-line bg-surface shadow-card">
      <button
        type="button"
        onClick={onOpen}
        aria-label={`查看「${label}」的详情`}
        className="group/cover relative block focus-visible:outline-offset-[-2px]"
      >
        <ThemeCover theme={theme} className="aspect-[4/3] w-full" />
        {/* 悬停或键盘聚焦时浮出，说明点这里看详情；触屏上点一下就是详情，不需要它 */}
        <span
          aria-hidden="true"
          className="transition-ui absolute inset-0 flex items-center justify-center bg-scrim opacity-0 group-hover/cover:opacity-100 group-focus-visible/cover:opacity-100"
        >
          <span className="rounded-control bg-surface px-3 py-1.5 text-sm font-medium text-ink shadow-popover">
            主题详情
          </span>
        </span>
      </button>

      {/* 使用中的那张名称栏反色：一眼找到当前主题（WordPress 的 Active 栏） */}
      <div
        className={cn(
          "flex min-h-13 flex-1 items-center justify-between gap-2 border-t px-3 py-2",
          theme.active
            ? "border-transparent bg-action text-action-on"
            : "border-line",
        )}
      >
        <div className="min-w-0">
          <p className="truncate text-sm font-medium" title={label}>
            {theme.active ? `使用中：${label}` : label}
          </p>
          <p
            className={cn(
              "text-xs",
              theme.active ? "text-action-on/70" : "text-ink-muted",
            )}
          >
            版本 {theme.version}
            {theme.builtin ? "，内置" : ""}
          </p>
        </div>
        {theme.active ? (
          hasSettings ? (
            <Button variant="secondary" size="sm" asChild>
              <Link to={themeSettingsPath(theme.name)}>
                <SlidersHorizontal aria-hidden="true" />
                设置
              </Link>
            </Button>
          ) : null
        ) : blocked ? (
          <Badge tone="danger">缺必需模板</Badge>
        ) : (
          <Button
            variant="secondary"
            size="sm"
            loading={activate.isPending}
            onClick={() => activate.mutate()}
            className="entity-reveal"
          >
            启用
          </Button>
        )}
      </div>
    </li>
  );
}

/**
 * 主题特色图：主题包根目录的 cover.webp（或 .png、.jpg），经后台接口读出
 * （与主题页一样要 themes:manage，故不走公开的 /theme-assets）。
 * 没有图时用主题名垫底，格子尺寸不变；`hint` 为真时顺带说明怎么补上。
 */
function ThemeCover({
  theme,
  className,
  alt = "",
  hint = false,
}: {
  theme: ThemeView;
  className?: string;
  alt?: string;
  hint?: boolean;
}) {
  const [failed, setFailed] = useState(false);

  if (!theme.hasCover || failed) {
    return (
      <div
        className={cn(
          "flex flex-col items-center justify-center gap-2 bg-surface-raised px-6 text-center",
          className,
        )}
      >
        <span className="text-2xl font-semibold text-ink-subtle">
          {theme.label || theme.name}
        </span>
        {hint ? (
          <span className="text-xs text-ink-muted">
            在主题包根目录放一张 cover.webp（png、jpg 也行），4:3，建议
            1200×900，就会显示在这里
          </span>
        ) : null}
      </div>
    );
  }
  return (
    <img
      src={`/api/v1/console/themes/${encodeURIComponent(theme.name)}/cover?v=${encodeURIComponent(theme.version)}`}
      alt={alt}
      decoding="async"
      onError={() => setFailed(true)}
      className={cn("bg-surface-raised object-cover", className)}
    />
  );
}

/** 主题详情（WordPress 的 Theme Details 浮层）：特色图在左，信息在右，动作在底栏。 */
function ThemeDetail({
  theme,
  onDelete,
}: {
  theme: ThemeView;
  onDelete: () => void;
}) {
  const activate = useActivate(theme);

  const reload = useMutation({
    mutationFn: () =>
      runMutation(
        () =>
          api.POST("/api/v1/console/themes/{name}/reload", {
            params: { path: { name: theme.name } },
          }),
        { success: "模板已重新载入", invalidate: ["themes"] },
      ),
  });

  const restore = useMutation({
    mutationFn: () =>
      runMutation(
        () =>
          api.POST("/api/v1/console/themes/{name}/restore", {
            params: { path: { name: theme.name } },
          }),
        { success: "已恢复出厂", invalidate: ["themes"] },
      ),
  });

  const templates = theme.templates ?? [];
  const required = templates.filter((item) => item.required);
  const optional = templates.filter((item) => !item.required && item.provided);
  const missing = missingTemplates(theme);
  const groups = theme.settingGroups ?? [];
  const deletable = !theme.builtin && !theme.active;
  const label = theme.label || theme.name;
  // 内置主题在磁盘上有副本时才是「可改的」：改模板即时生效，改坏了恢复出厂。
  const builtinOnDisk = theme.builtin && theme.source === "disk";
  const [confirmRestore, setConfirmRestore] = useState(false);

  return (
    <DialogContent size="xl">
      <DialogHeader>
        <div className="flex flex-wrap items-center gap-2">
          <DialogTitle>{label}</DialogTitle>
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
        <DialogDescription>
          <code className="token">{theme.name}</code>
          <span className="ml-2">版本 {theme.version}</span>
          <span className="ml-2">
            {theme.author ? `作者 ${theme.author}` : "未署名"}
          </span>
        </DialogDescription>
      </DialogHeader>

      <DialogBody className="grid gap-5 md:grid-cols-[minmax(0,11fr)_minmax(0,10fr)] md:items-start">
        <ThemeCover
          theme={theme}
          hint
          alt={`${label} 的特色图`}
          className="aspect-[4/3] w-full rounded-control border border-line"
        />

        <div className="flex min-w-0 flex-col gap-4">
          {theme.description ? (
            <p className="text-sm leading-relaxed text-ink">
              {theme.description}
            </p>
          ) : (
            <p className="text-sm text-ink-subtle">主题没有写描述</p>
          )}

          {missing.length > 0 ? (
            <Alert tone="warn" title="缺少必需模板，这个主题无法启用">
              缺 {missing.map((item) => item.name).join("、")}。必需的四个模板是
              index、post、page 与 404，主题包里必须齐备。
            </Alert>
          ) : null}

          <DescriptionList className="grid-cols-[4.5rem_minmax(0,1fr)] text-sm">
            <DescriptionTerm>许可证</DescriptionTerm>
            <DescriptionDetail>{theme.license || "未声明"}</DescriptionDetail>
            <DescriptionTerm>主页</DescriptionTerm>
            <DescriptionDetail>
              <ExternalValue href={theme.homepage} />
            </DescriptionDetail>
            <DescriptionTerm>仓库</DescriptionTerm>
            <DescriptionDetail>
              <ExternalValue href={theme.repo} />
            </DescriptionDetail>
            <DescriptionTerm>设置</DescriptionTerm>
            <DescriptionDetail className="tabular">
              {groups.length === 0
                ? "这个主题没有声明设置项"
                : `${groups.length} 组`}
            </DescriptionDetail>
            {theme.builtin ? (
              <>
                <DescriptionTerm>来源</DescriptionTerm>
                <DescriptionDetail>
                  {builtinOnDisk ? (
                    <>
                      磁盘上的{" "}
                      <code className="token">data/themes/{theme.name}</code>
                      ，改模板即时生效；改坏了可以恢复出厂
                    </>
                  ) : (
                    <>
                      二进制里那份（磁盘副本缺失或加载失败）。
                      恢复出厂会重新解压一份到{" "}
                      <code className="token">data/themes</code>
                    </>
                  )}
                </DescriptionDetail>
              </>
            ) : null}
          </DescriptionList>

          <div className="flex flex-col gap-2 border-line border-t pt-4">
            <div className="flex flex-wrap items-center gap-1.5">
              <span className="w-16 shrink-0 text-xs text-ink-muted">
                必需模板
              </span>
              {required.map((item) => (
                <Badge key={item.name} tone={item.provided ? "ok" : "danger"}>
                  {item.name}
                </Badge>
              ))}
            </div>
            <div className="flex flex-wrap items-center gap-1.5">
              <span className="w-16 shrink-0 text-xs text-ink-muted">
                可选模板
              </span>
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
                <span className="w-16 shrink-0 text-xs text-ink-muted">
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
      </DialogBody>

      <DialogFooter className="justify-between">
        <div className="flex flex-wrap items-center gap-2">
          {deletable ? (
            <Button variant="danger" size="sm" onClick={onDelete}>
              <Trash2 aria-hidden="true" />
              卸载
            </Button>
          ) : theme.builtin ? (
            /*
              恢复出厂对内置主题是常驻入口，不因为「磁盘上没副本」而禁用：
              source 是 embedded 往往意味着磁盘那份加载失败（模板被改坏），
              而恢复出厂正是那种情况下的出路。
            */
            <Button
              variant="secondary"
              size="sm"
              loading={restore.isPending}
              onClick={() => setConfirmRestore(true)}
            >
              <RotateCcw aria-hidden="true" />
              恢复出厂
            </Button>
          ) : (
            <span className="text-xs text-ink-muted">
              使用中的主题不可卸载，先切到别的主题
            </span>
          )}
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {groups.length > 0 ? (
            <Button variant="secondary" size="sm" asChild>
              <Link to={themeSettingsPath(theme.name)}>
                <SlidersHorizontal aria-hidden="true" />
                设置
              </Link>
            </Button>
          ) : null}
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
        </div>
      </DialogFooter>

      <ConfirmDialog
        open={confirmRestore}
        onOpenChange={setConfirmRestore}
        title={`把「${label}」恢复出厂？`}
        consequence={
          <p>
            磁盘上的主题目录会被删掉，再用二进制里那份重新解压一遍，
            <strong className="font-medium text-ink">
              你对它做过的模板与静态资源改动会全部丢失
            </strong>
            。主题设置（配色、版式这些）不受影响，它们存在数据库里。
          </p>
        }
        confirmLabel="恢复出厂"
        pending={restore.isPending}
        onConfirm={async () => {
          await restore.mutateAsync().catch(() => {});
          setConfirmRestore(false);
        }}
      />
    </DialogContent>
  );
}

/** 详情里的外链值：有就显示成链接，没有就说未提供。 */
function ExternalValue({ href }: { href: string }) {
  if (!href) {
    return <span className="text-ink-subtle">未提供</span>;
  }
  return (
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      className="token block truncate text-seal hover:underline"
      title={href}
    >
      {href}
    </a>
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
