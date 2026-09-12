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
  EntityThumb,
  FilterMenu,
  ListBody,
  ListEmpty,
  ListToolbar,
} from "@/components/data/entity";
import { PageBody, PageHeader } from "@/components/layout/page-header";
import { Button } from "@/components/ui/button";
import {
  Card,
  DescriptionDetail,
  DescriptionList,
  DescriptionTerm,
  Inset,
} from "@/components/ui/card";
import {
  ConfirmDialog,
  Dialog,
  DialogBody,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { DropdownMenuItem } from "@/components/ui/dropdown-menu";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";
import { Input, SearchInput } from "@/components/ui/input";
import { Pagination } from "@/components/ui/pagination";
import { ErrorState, Skeleton } from "@/components/ui/states";
import { absoluteDate, fileSize, relativeTime } from "@/lib/format";
import { useDocumentTitle } from "@/lib/use-document-title";
import { useDebouncedSearch, useListParams } from "@/lib/use-list-params";
import { cn } from "@/lib/utils";
import { useMutation, useQuery } from "@tanstack/react-query";
import {
  ChevronLeft,
  ChevronRight,
  Copy,
  Eye,
  FileAudio,
  FileText,
  FileVideo,
  Image as ImageIcon,
  LayoutGrid,
  List as ListIcon,
  Trash2,
  Upload,
} from "lucide-react";
import { useRef, useState } from "react";
import { toast } from "sonner";

/**
 * 附件库（形态对齐 Halo 的附件页）。
 *
 * 两种视图：网格看图、列表看信息。判断「这张图能不能用」靠的是看缩略图，
 * 而找「上周谁传了那个 PDF」靠的是文件名、上传者与时间 —— 两件事各需一种形态，
 * 视图选择记在本机，下次进来还是上次的样子。
 *
 * 上传是这个页面的主行动，故 drag & drop 覆盖整个内容区，
 * 而不只是一个小按钮 —— 从文件管理器拖图进来是最自然的上传方式。
 *
 * 缩略图取 medium 档（768px）而不是原图：一屏几十张原图会让页面等上好几秒，
 * 而网格里的格子最大也就两百多像素。
 */

type Media = components["schemas"]["Media"];
type ViewMode = "grid" | "list";

const VIEW_KEY = "lumo-console-media-view";

const KIND_META: Record<string, { label: string; icon: typeof ImageIcon }> = {
  image: { label: "图片", icon: ImageIcon },
  video: { label: "视频", icon: FileVideo },
  audio: { label: "音频", icon: FileAudio },
  document: { label: "文档", icon: FileText },
  other: { label: "其他", icon: FileText },
};

const FALLBACK_KIND = { label: "文件", icon: FileText };

const KIND_OPTIONS = [
  { value: "", label: "全部类型" },
  ...Object.entries(KIND_META).map(([value, meta]) => ({
    value,
    label: meta.label,
  })),
];

/** 服务端未来新增 kind 时退回「文件」而不是渲染空白。 */
function kindOf(media: Media) {
  return KIND_META[media.kind] ?? FALLBACK_KIND;
}

/** 取缩略图：优先 medium 档，退回原图。 */
function previewOf(media: Media): string {
  const thumbs = media.thumbnails ?? [];
  const medium = thumbs.find((t) => t.name === "medium");
  return medium?.url || thumbs[0]?.url || media.url;
}

function readView(): ViewMode {
  try {
    return localStorage.getItem(VIEW_KEY) === "list" ? "list" : "grid";
  } catch {
    // 隐私模式下 localStorage 会抛异常；退回默认值即可
    return "grid";
  }
}

function storeView(view: ViewMode) {
  try {
    localStorage.setItem(VIEW_KEY, view);
  } catch {
    // 存不下就只在本次会话内生效
  }
}

/** 复制完整地址：站长多半要粘到文章正文或别处，相对路径在那里不可用。 */
async function copyUrl(media: Media) {
  const absolute = new URL(media.url, window.location.origin).href;
  try {
    await navigator.clipboard.writeText(absolute);
    toast.success("已复制地址");
  } catch {
    // 非 HTTPS 或权限被拒时剪贴板不可用
    toast.error("剪贴板不可用，请在详情里手动选中地址复制");
  }
}

export function MediaPage() {
  useDocumentTitle("附件");
  const { can, user } = useAuth();
  const canWrite = can("media:write");
  const canDeleteAny = can("media:delete_any");
  const list = useListParams();

  /**
   * 能否改动某条附件。
   *
   * 与服务端 internal/media/handler.go 的 canManage 逐字对应：
   * 自己的附件需 media:write，别人的需 media:delete_any。
   * 附件是唯一一条 `_any` 权限同时管「改」与「删」的资源（agent.md §7.2），
   * 故两者共用同一个判定。前端这一层只是不显示点了必然 403 的按钮。
   */
  const canManage = (media: Media) =>
    (canWrite && media.uploaderId === user?.id) || canDeleteAny;

  const [search, setSearch] = useDebouncedSearch(list.filter("q"), (value) =>
    list.setFilter("q", value),
  );

  const [view, setView] = useState<ViewMode>(readView);
  const [selectedId, setSelectedId] = useState<number | null>(null);
  const [dragging, setDragging] = useState(false);
  const dragDepth = useRef(0);
  const fileInput = useRef<HTMLInputElement>(null);

  // `upload=1` 是从仪表盘「上传附件」与命令面板进来时带的提示参数，不是筛选条件
  const uploadHint = list.filter("upload") === "1";
  const kind = list.filter("kind");
  const hasFilters = Boolean(list.filter("q") || kind);

  const query = useQuery({
    queryKey: ["media", list.page, list.size, list.filter("q"), kind],
    queryFn: async () => {
      const { data, response } = await api.GET("/api/v1/console/media", {
        params: {
          query: {
            page: list.page,
            size: list.size,
            ...(kind ? { kind: kind as Media["kind"] } : {}),
            ...(list.filter("q") ? { q: list.filter("q") } : {}),
          },
        },
      });
      if (!response.ok) {
        throw new Error(`载入附件失败（HTTP ${response.status}）`);
      }
      return data;
    },
  });

  const upload = useMutation({
    mutationFn: async (file: File) => {
      const body = new FormData();
      body.append("file", file);
      const { data, error, response } = await api.POST(
        "/api/v1/console/media",
        {
          /*
           * openapi-fetch 在解析 multipart 请求体时按 schema 生成对象，
           * 但真实上传必须传 FormData 才能带上文件流。
           * `bodySerializer` 直接返回 FormData 即绕开序列化 ——
           * 类型上需要两次断言，因为生成的类型是「字段名 → 值」的对象。
           */
          body: body as unknown as { file: string },
          bodySerializer: () => body,
        },
      );
      if (!response.ok) {
        throw new Error(problemMessage(error));
      }
      return data;
    },
  });

  const items = query.data?.items ?? [];
  const total = query.data?.total ?? 0;

  const selectedIndex = items.findIndex((item) => item.id === selectedId);
  const selected = selectedIndex >= 0 ? (items[selectedIndex] ?? null) : null;

  async function uploadFiles(files: FileList | File[]) {
    const queue = Array.from(files);
    if (queue.length === 0) {
      return;
    }
    let succeeded = 0;
    // 逐个上传：multipart 接口一次只收一个文件，而并发上传几个大图
    // 会把上行带宽占满，反而都变慢。
    for (const file of queue) {
      try {
        await upload.mutateAsync(file);
        succeeded += 1;
      } catch (err) {
        toast.error(
          `${file.name}：${err instanceof Error ? err.message : "上传失败"}`,
        );
      }
    }
    if (succeeded > 0) {
      toast.success(
        queue.length === 1 ? "已上传" : `已上传 ${succeeded} 个文件`,
      );
      void query.refetch();
      if (uploadHint) {
        // 提示框只为「第一次上传」而存在，传过一次就撤掉
        list.setFilter("upload", "");
      }
    }
  }

  function changeView(next: ViewMode) {
    setView(next);
    storeView(next);
  }

  const emptyState = (
    <ListEmpty
      icon={ImageIcon}
      title={hasFilters ? "没有匹配的附件" : "附件库是空的"}
      description={
        hasFilters
          ? "换个关键词或类型试试。"
          : "把文件拖到这里，或点右上角的「上传」。插图、封面、分享图都从这里取。"
      }
      action={
        hasFilters ? (
          <Button variant="secondary" size="sm" onClick={list.reset}>
            清除筛选
          </Button>
        ) : canWrite ? (
          <Button
            variant="primary"
            size="sm"
            onClick={() => fileInput.current?.click()}
          >
            上传附件
          </Button>
        ) : null
      }
    />
  );

  return (
    <>
      <PageHeader
        icon={ImageIcon}
        title="附件"
        description="图片会自动生成三档 WebP 缩略图，与原图一起存放在设置的存储后端"
        actions={
          canWrite ? (
            <>
              <input
                ref={fileInput}
                type="file"
                multiple
                hidden
                onChange={(e) => {
                  if (e.target.files) {
                    void uploadFiles(e.target.files);
                  }
                  // 清空以便连续两次选同一个文件也能触发 change
                  e.target.value = "";
                }}
              />
              <Button
                variant="primary"
                loading={upload.isPending}
                onClick={() => fileInput.current?.click()}
              >
                <Upload aria-hidden="true" />
                上传
              </Button>
            </>
          ) : null
        }
      />

      <PageBody>
        {/* 拖放区覆盖整个内容区，而不只是一个小方框 */}
        <div
          onDragEnter={(e) => {
            e.preventDefault();
            if (!canWrite) {
              return;
            }
            dragDepth.current += 1;
            setDragging(true);
          }}
          onDragOver={(e) => e.preventDefault()}
          onDragLeave={(e) => {
            e.preventDefault();
            // 用计数而不是直接置 false：拖过子元素时会触发 leave，
            // 直接置 false 会让遮罩不停闪烁。
            dragDepth.current -= 1;
            if (dragDepth.current <= 0) {
              dragDepth.current = 0;
              setDragging(false);
            }
          }}
          onDrop={(e) => {
            e.preventDefault();
            dragDepth.current = 0;
            setDragging(false);
            if (canWrite && e.dataTransfer.files.length > 0) {
              void uploadFiles(e.dataTransfer.files);
            }
          }}
          className="relative flex flex-col gap-4"
        >
          {dragging ? (
            <div className="pointer-events-none absolute inset-0 z-sticky flex items-center justify-center rounded-card border-2 border-seal border-dashed bg-seal-soft/90">
              <p className="flex items-center gap-2 font-medium text-seal">
                <Upload aria-hidden="true" className="size-5" />
                松手即上传
              </p>
            </div>
          ) : null}

          {uploadHint && canWrite ? (
            <Inset className="flex flex-col items-center gap-3 border-dashed py-8 text-center">
              <span className="flex size-12 items-center justify-center rounded-full bg-surface-active text-ink-muted">
                <Upload aria-hidden="true" className="size-5" />
              </span>
              <div className="flex flex-col gap-1">
                <p className="text-md font-medium text-ink">
                  把文件拖到这里，或从电脑里选
                </p>
                <p className="text-sm text-ink-muted">
                  图片会自动生成三档 WebP
                  缩略图；单个文件的大小上限由服务端设置决定。
                </p>
              </div>
              <Button
                variant="secondary"
                size="sm"
                loading={upload.isPending}
                onClick={() => fileInput.current?.click()}
              >
                选择文件
              </Button>
            </Inset>
          ) : null}

          <Card>
            <ListToolbar
              className="border-line border-b"
              search={
                <SearchInput
                  value={search}
                  onValueChange={setSearch}
                  placeholder="按原始文件名筛选"
                  aria-label="筛选附件"
                  className="max-w-xs"
                />
              }
              filters={
                <>
                  <FilterMenu
                    label="类型"
                    value={kind}
                    options={KIND_OPTIONS}
                    onChange={(value) => list.setFilter("kind", value)}
                  />
                  <ViewToggle value={view} onChange={changeView} />
                </>
              }
              hasFilters={hasFilters}
              onClearFilters={list.reset}
              onRefresh={() => void query.refetch()}
              refreshing={query.isFetching}
            />

            {view === "grid" ? (
              query.isLoading ? (
                <ul
                  className="grid grid-cols-2 gap-3 p-4 sm:grid-cols-3 lg:grid-cols-5 xl:grid-cols-6"
                  aria-busy="true"
                >
                  {Array.from({ length: 12 }, (_, i) => (
                    // biome-ignore lint/suspicious/noArrayIndexKey: 静态占位
                    <li key={i}>
                      <Skeleton className="aspect-[10/8] w-full" />
                    </li>
                  ))}
                </ul>
              ) : query.error ? (
                <ErrorState
                  message={query.error.message}
                  onRetry={() => void query.refetch()}
                />
              ) : items.length === 0 ? (
                emptyState
              ) : (
                <ul className="grid grid-cols-2 gap-3 p-4 sm:grid-cols-3 lg:grid-cols-5 xl:grid-cols-6">
                  {items.map((media) => (
                    <MediaCard
                      key={media.id}
                      media={media}
                      deletable={canManage(media)}
                      onOpen={() => setSelectedId(media.id)}
                      onChanged={() => void query.refetch()}
                    />
                  ))}
                </ul>
              )
            ) : (
              <ListBody
                isLoading={query.isLoading}
                error={query.error}
                onRetry={() => void query.refetch()}
                isEmpty={items.length === 0}
                empty={emptyState}
                thumb
              >
                {items.map((media) => (
                  <MediaRow
                    key={media.id}
                    media={media}
                    deletable={canManage(media)}
                    onOpen={() => setSelectedId(media.id)}
                    onChanged={() => void query.refetch()}
                  />
                ))}
              </ListBody>
            )}

            <Pagination
              page={list.page}
              size={list.size}
              total={total}
              onPageChange={list.setPage}
              onSizeChange={list.setSize}
            />
          </Card>
        </div>
      </PageBody>

      <MediaDetail
        media={selected}
        canManage={selected ? canManage(selected) : false}
        hasPrev={selectedIndex > 0}
        hasNext={selectedIndex >= 0 && selectedIndex < items.length - 1}
        onNavigate={(direction) => {
          const next = items[selectedIndex + direction];
          if (next) {
            setSelectedId(next.id);
          }
        }}
        onClose={() => setSelectedId(null)}
        onChanged={() => void query.refetch()}
      />
    </>
  );
}

/** 网格 / 列表切换。两枚带 aria-pressed 的图标按钮。 */
function ViewToggle({
  value,
  onChange,
}: {
  value: ViewMode;
  onChange: (value: ViewMode) => void;
}) {
  const options = [
    { mode: "grid" as const, icon: LayoutGrid, label: "网格视图" },
    { mode: "list" as const, icon: ListIcon, label: "列表视图" },
  ];
  return (
    <fieldset className="m-0 flex items-center gap-0.5 rounded-control border-0 bg-surface-inset p-0.5">
      <legend className="sr-only">视图</legend>
      {options.map((option) => {
        const active = value === option.mode;
        return (
          <Button
            key={option.mode}
            variant="ghost"
            size="icon-xs"
            aria-pressed={active}
            aria-label={option.label}
            title={option.label}
            onClick={() => onChange(option.mode)}
            className={cn(
              "rounded-[3px]",
              active && "bg-surface text-ink shadow-card hover:bg-surface",
            )}
          >
            <option.icon aria-hidden="true" />
          </Button>
        );
      })}
    </fieldset>
  );
}

/** 删除附件的 mutation，网格与列表两种形态共用。 */
function useRemoveMedia(media: Media, onChanged: () => void) {
  return useMutation({
    mutationFn: () =>
      runMutation(
        () =>
          api.DELETE("/api/v1/console/media/{id}", {
            params: { path: { id: media.id } },
          }),
        { success: "附件已删除", invalidate: ["media"] },
      ),
    onSuccess: onChanged,
  });
}

/** 删除确认，两种形态共用同一段后果说明。 */
function RemoveMediaDialog({
  media,
  open,
  onOpenChange,
  pending,
  onConfirm,
}: {
  media: Media;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  pending: boolean;
  onConfirm: () => void;
}) {
  return (
    <ConfirmDialog
      open={open}
      onOpenChange={onOpenChange}
      title={`删除「${media.originalName}」？`}
      consequence={
        <p>
          <strong className="font-medium text-ink">这一步无法撤销。</strong>
          原图与全部缩略图会一并从存储中删除。
          已经引用它的文章里，那张图会变成死链。
        </p>
      }
      confirmLabel="删除"
      pending={pending}
      onConfirm={onConfirm}
    />
  );
}

/** 网格里的一格：固定 10:8 的缩略图区 + 居中截断的文件名。 */
function MediaCard({
  media,
  deletable,
  onOpen,
  onChanged,
}: {
  media: Media;
  deletable: boolean;
  onOpen: () => void;
  onChanged: () => void;
}) {
  const [confirmDelete, setConfirmDelete] = useState(false);
  const kind = kindOf(media);
  const Icon = kind.icon;
  const remove = useRemoveMedia(media, onChanged);

  return (
    <li className="group/entity relative">
      <button
        type="button"
        onClick={onOpen}
        className="transition-ui block w-full overflow-hidden rounded-control border border-line bg-surface text-left hover:shadow-popover"
      >
        {/* 固定宽高比：图片加载前后格子不跳（CLS 是检索点名的 HIGH 项） */}
        <span className="flex aspect-[10/8] items-center justify-center overflow-hidden bg-surface-active">
          {media.kind === "image" ? (
            <img
              src={previewOf(media)}
              alt={media.alt || media.originalName}
              loading="lazy"
              className="size-full object-cover"
            />
          ) : (
            <Icon aria-hidden="true" className="size-8 text-ink-subtle" />
          )}
        </span>
        <span className="block px-2 py-1.5 text-center">
          <span
            className="block truncate text-xs font-medium text-ink"
            title={media.originalName}
          >
            {media.originalName}
          </span>
        </span>
      </button>

      {deletable ? (
        <Button
          variant="secondary"
          size="icon-xs"
          onClick={() => setConfirmDelete(true)}
          aria-label={`删除 ${media.originalName}`}
          title="删除"
          // 悬停或聚焦时浮出；触屏与窄屏常显（entity-reveal 的规则）
          className="entity-reveal absolute top-1.5 right-1.5 shadow-card"
        >
          <Trash2 aria-hidden="true" />
        </Button>
      ) : null}

      <RemoveMediaDialog
        media={media}
        open={confirmDelete}
        onOpenChange={setConfirmDelete}
        pending={remove.isPending}
        onConfirm={async () => {
          await remove.mutateAsync().catch(() => {});
          setConfirmDelete(false);
        }}
      />
    </li>
  );
}

/** 列表里的一行：缩略图 + 文件名 + 类型与体积，右侧上传者与时间。 */
function MediaRow({
  media,
  deletable,
  onOpen,
  onChanged,
}: {
  media: Media;
  deletable: boolean;
  onOpen: () => void;
  onChanged: () => void;
}) {
  const [confirmDelete, setConfirmDelete] = useState(false);
  const kind = kindOf(media);
  const remove = useRemoveMedia(media, onChanged);

  return (
    <Entity>
      <EntityStart>
        <EntityThumb
          src={media.kind === "image" ? previewOf(media) : undefined}
          icon={kind.icon}
        />
        <EntityField
          width="max-w-md"
          title={
            <button
              type="button"
              onClick={onOpen}
              title={media.originalName}
              className="transition-ui max-w-full truncate text-left hover:text-seal"
            >
              {media.originalName}
            </button>
          }
          description={
            <>
              <span>{kind.label}</span>
              <span className="tabular">{fileSize(media.size)}</span>
              {media.kind === "image" && media.width > 0 ? (
                <span className="tabular">
                  {media.width} × {media.height}
                </span>
              ) : null}
            </>
          }
        />
      </EntityStart>

      <EntityEnd>
        <EntityMeta hideOnMobile>
          {media.uploader?.displayName || media.uploader?.username || "—"}
        </EntityMeta>
        <EntityMeta>
          <time
            dateTime={media.createdAt}
            title={absoluteDate(media.createdAt)}
          >
            {relativeTime(media.createdAt)}
          </time>
        </EntityMeta>
        <EntityActions label={`附件 ${media.originalName} 的操作`}>
          <DropdownMenuItem onSelect={onOpen}>
            <Eye aria-hidden="true" />
            查看详情
          </DropdownMenuItem>
          <DropdownMenuItem onSelect={() => void copyUrl(media)}>
            <Copy aria-hidden="true" />
            复制地址
          </DropdownMenuItem>
          {deletable ? (
            <DropdownMenuItem danger onSelect={() => setConfirmDelete(true)}>
              <Trash2 aria-hidden="true" />
              删除
            </DropdownMenuItem>
          ) : null}
        </EntityActions>
      </EntityEnd>

      <RemoveMediaDialog
        media={media}
        open={confirmDelete}
        onOpenChange={setConfirmDelete}
        pending={remove.isPending}
        onConfirm={async () => {
          await remove.mutateAsync().catch(() => {});
          setConfirmDelete(false);
        }}
      />
    </Entity>
  );
}

/**
 * 详情对话框。可在当前页的条目间前后翻阅（Halo 的附件详情同样带上一张 / 下一张）。
 *
 * 表单值按附件 id 重填而不是按对象引用：列表刷新后引用会变，
 * 那会把用户正在编辑的内容覆盖掉。翻到另一张时 id 变了，重填才是对的。
 */
function MediaDetail({
  media,
  canManage,
  hasPrev,
  hasNext,
  onNavigate,
  onClose,
  onChanged,
}: {
  media: Media | null;
  canManage: boolean;
  hasPrev: boolean;
  hasNext: boolean;
  onNavigate: (direction: -1 | 1) => void;
  onClose: () => void;
  onChanged: () => void;
}) {
  const [alt, setAlt] = useState("");
  const [title, setTitle] = useState("");
  const [initialised, setInitialised] = useState<number | null>(null);

  if (media && initialised !== media.id) {
    setAlt(media.alt);
    setTitle(media.title);
    setInitialised(media.id);
  }

  const save = useMutation({
    mutationFn: () =>
      runMutation(
        () =>
          api.PUT("/api/v1/console/media/{id}", {
            params: { path: { id: media?.id ?? 0 } },
            body: { alt, title },
          }),
        { success: "已保存", invalidate: ["media"] },
      ),
    onSuccess: () => {
      onChanged();
      onClose();
    },
  });

  return (
    <Dialog open={media !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent size="lg">
        <DialogHeader>
          <DialogTitle className="truncate">{media?.originalName}</DialogTitle>
        </DialogHeader>
        <DialogBody className="flex flex-col gap-4">
          {media?.kind === "image" ? (
            <img
              src={media.url}
              alt={media.alt || media.originalName}
              className="max-h-72 w-full rounded-control border border-line bg-surface-active object-contain"
            />
          ) : (
            <div className="flex h-32 items-center justify-center rounded-control border border-line bg-surface-active">
              <p className="text-sm text-ink-muted">
                {media ? kindOf(media).label : "文件"}，无法预览
              </p>
            </div>
          )}

          {/* 只读元信息。用 dl 而不是表格：这是「名称—值」对，不是表格数据 */}
          <DescriptionList>
            <DescriptionTerm>地址</DescriptionTerm>
            <DescriptionDetail className="flex min-w-0 items-center gap-2">
              <code className="token min-w-0 flex-1 text-xs">{media?.url}</code>
              <Button
                variant="secondary"
                size="xs"
                onClick={() => {
                  if (media) {
                    void copyUrl(media);
                  }
                }}
              >
                <Copy aria-hidden="true" />
                复制
              </Button>
            </DescriptionDetail>

            <DescriptionTerm>类型</DescriptionTerm>
            <DescriptionDetail>{media?.mime}</DescriptionDetail>

            {media?.kind === "image" && media.width > 0 ? (
              <>
                <DescriptionTerm>尺寸</DescriptionTerm>
                <DescriptionDetail className="tabular">
                  {media.width} × {media.height}
                </DescriptionDetail>
              </>
            ) : null}

            <DescriptionTerm>体积</DescriptionTerm>
            <DescriptionDetail className="tabular">
              {fileSize(media?.size)}
            </DescriptionDetail>

            <DescriptionTerm>存储</DescriptionTerm>
            <DescriptionDetail>
              {media?.driver === "s3" ? "S3 兼容存储" : "本地"}
              {media?.thumbnails && media.thumbnails.length > 0 ? (
                <span className="ml-2 text-xs text-ink-muted">
                  {media.thumbnails.length} 档缩略图
                </span>
              ) : null}
            </DescriptionDetail>

            <DescriptionTerm>上传者</DescriptionTerm>
            <DescriptionDetail>
              {media?.uploader?.displayName || media?.uploader?.username || "—"}
            </DescriptionDetail>

            <DescriptionTerm>上传于</DescriptionTerm>
            <DescriptionDetail>
              {absoluteDate(media?.createdAt)}
            </DescriptionDetail>

            <DescriptionTerm>校验和</DescriptionTerm>
            <DescriptionDetail className="token text-xs text-ink-muted">
              {media?.checksum}
            </DescriptionDetail>
          </DescriptionList>

          <div className="flex flex-col gap-4 border-line border-t pt-4">
            <Field>
              <FieldLabel htmlFor="media-alt">替代文本（alt）</FieldLabel>
              <Input
                id="media-alt"
                value={alt}
                onChange={(e) => setAlt(e.target.value)}
                disabled={!canManage}
              />
              <FieldDescription>
                图加载失败或读屏时显示这句话。描述图里有什么，不要重复文件名。
              </FieldDescription>
            </Field>

            <Field>
              <FieldLabel htmlFor="media-title">标题</FieldLabel>
              <Input
                id="media-title"
                value={title}
                onChange={(e) => setTitle(e.target.value)}
                disabled={!canManage}
              />
              <FieldDescription>
                鼠标悬停时显示，也用于附件库检索。
              </FieldDescription>
            </Field>
          </div>
        </DialogBody>
        <DialogFooter>
          <div className="mr-auto flex items-center gap-1">
            <Button
              variant="ghost"
              size="icon-sm"
              disabled={!hasPrev}
              onClick={() => onNavigate(-1)}
              aria-label="上一张"
              title="上一张"
            >
              <ChevronLeft aria-hidden="true" />
            </Button>
            <Button
              variant="ghost"
              size="icon-sm"
              disabled={!hasNext}
              onClick={() => onNavigate(1)}
              aria-label="下一张"
              title="下一张"
            >
              <ChevronRight aria-hidden="true" />
            </Button>
          </div>
          <Button variant="secondary" onClick={onClose}>
            关闭
          </Button>
          <Button
            variant="primary"
            loading={save.isPending}
            disabled={!canManage}
            onClick={() => save.mutate()}
          >
            保存
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
