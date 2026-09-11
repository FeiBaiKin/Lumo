import { api, problemMessage } from "@/api/client";
import { runMutation } from "@/api/mutation";
import type { components } from "@/api/schema";
import { useAuth } from "@/components/auth/auth-provider";
import {
  ListEmpty,
  ListPanel,
  ToolbarSearch,
} from "@/components/data/list-panel";
import { Button } from "@/components/ui/button";
import {
  ConfirmDialog,
  Dialog,
  DialogBody,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";
import { Input, InputAffix } from "@/components/ui/input";
import { Pagination } from "@/components/ui/pagination";
import { PageHeader } from "@/components/ui/panel";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { ErrorState, Skeleton } from "@/components/ui/states";
import { absoluteDate, fileSize } from "@/lib/format";
import { useDocumentTitle } from "@/lib/use-document-title";
import { useDebouncedSearch, useListParams } from "@/lib/use-list-params";
import { useMutation, useQuery } from "@tanstack/react-query";
import {
  FileAudio,
  FileText,
  FileVideo,
  Image as ImageIcon,
  Search,
  Trash2,
  Upload,
  X,
} from "lucide-react";
import { useRef, useState } from "react";

/**
 * 附件库。
 *
 * 用**网格**而不是表格：附件是视觉资产，判断「这张图能不能用」
 * 靠的是看缩略图，不是读文件名。表格在这件事上完全帮不上忙。
 *
 * 上传是这个页面的主行动，故 drag & drop 覆盖整个内容区，
 * 而不只是一个小按钮 —— 从文件管理器拖图进来是最自然的上传方式。
 *
 * 缩略图取 medium 档（768px）而不是原图：一屏几十张原图会让页面等上好几秒，
 * 而网格里的格子最大也就两百多像素。
 */

type Media = components["schemas"]["Media"];

const KIND_META: Record<string, { label: string; icon: typeof ImageIcon }> = {
  image: { label: "图片", icon: ImageIcon },
  video: { label: "视频", icon: FileVideo },
  audio: { label: "音频", icon: FileAudio },
  document: { label: "文档", icon: FileText },
  other: { label: "其他", icon: FileText },
};

const FALLBACK_KIND = { label: "文件", icon: FileText };

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
   * 故两者共用同一个判定。
   *
   * 前端这一层只是不显示点了必然 403 的按钮；真正的拦截在服务端。
   */
  const canManage = (media: Media) =>
    (canWrite && media.uploaderId === user?.id) || canDeleteAny;

  const [search, setSearch] = useDebouncedSearch(list.filter("q"), (value) =>
    list.setFilter("q", value),
  );

  const [selected, setSelected] = useState<Media | null>(null);
  const [dragging, setDragging] = useState(false);
  const dragDepth = useRef(0);
  const fileInput = useRef<HTMLInputElement>(null);

  const query = useQuery({
    queryKey: ["media", list.page, list.size, list.filters],
    queryFn: async () => {
      const { data, response } = await api.GET("/api/v1/console/media", {
        params: {
          query: {
            page: list.page,
            size: list.size,
            ...(list.filter("kind") && list.filter("kind") !== "all"
              ? { kind: list.filter("kind") as Media["kind"] }
              : {}),
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
    onSuccess: () => void query.refetch(),
  });

  const items = query.data?.items ?? [];
  const total = query.data?.total ?? 0;

  async function uploadFiles(files: FileList | File[]) {
    const list_ = Array.from(files);
    if (list_.length === 0) {
      return;
    }
    // 逐个上传：multipart 接口一次只收一个文件，而并发上传几个大图
    // 会把上行带宽占满，反而都变慢。
    for (const file of list_) {
      try {
        await upload.mutateAsync(file);
      } catch {
        // 单个失败不阻断其余，失败原因已由 upload 的错误处理播报
      }
    }
  }

  return (
    <>
      <PageHeader
        title="附件"
        description="图片会自动生成三档 WebP 缩略图；原图与缩略图都存放在设置的存储后端"
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
                onClick={() => fileInput.current?.click()}
                disabled={upload.isPending}
              >
                <Upload aria-hidden="true" />
                {upload.isPending ? "正在上传" : "上传附件"}
              </Button>
            </>
          ) : null
        }
      />

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
        className="relative"
      >
        {dragging ? (
          <div className="pointer-events-none absolute inset-0 z-sticky flex items-center justify-center rounded-panel border-2 border-seal border-dashed bg-seal-soft/90">
            <p className="flex items-center gap-2 font-medium text-seal">
              <Upload aria-hidden="true" className="size-5" />
              松手即上传
            </p>
          </div>
        ) : null}

        <ListPanel
          toolbar={
            <>
              <Select
                value={list.filter("kind") || "all"}
                onValueChange={(value) =>
                  list.setFilter("kind", value === "all" ? "" : value)
                }
              >
                <SelectTrigger className="w-36" aria-label="按类型筛选">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">全部类型</SelectItem>
                  {Object.entries(KIND_META).map(([kind, meta]) => (
                    <SelectItem key={kind} value={kind}>
                      {meta.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>

              <ToolbarSearch>
                <Input
                  value={search}
                  onChange={(e) => setSearch(e.target.value)}
                  placeholder="按原始文件名筛选"
                  aria-label="筛选附件"
                  className="pl-8"
                />
                <InputAffix side="left">
                  <Search aria-hidden="true" />
                </InputAffix>
              </ToolbarSearch>

              {list.hasFilters ? (
                <Button variant="ghost" size="sm" onClick={list.reset}>
                  <X aria-hidden="true" />
                  清除筛选
                </Button>
              ) : null}
            </>
          }
          footer={
            <Pagination
              page={list.page}
              size={list.size}
              total={total}
              onPageChange={list.setPage}
              onSizeChange={list.setSize}
            />
          }
        >
          {query.isLoading ? (
            <div className="grid grid-cols-2 gap-3 border-line border-t p-4 sm:grid-cols-3 lg:grid-cols-5 xl:grid-cols-6">
              {Array.from({ length: 12 }, (_, i) => (
                // biome-ignore lint/suspicious/noArrayIndexKey: 静态占位
                <Skeleton key={i} className="aspect-square w-full" />
              ))}
            </div>
          ) : query.error ? (
            <ErrorState
              message={query.error.message}
              onRetry={() => void query.refetch()}
            />
          ) : items.length === 0 ? (
            <ListEmpty
              icon={ImageIcon}
              title={list.hasFilters ? "没有匹配的附件" : "附件库是空的"}
              description={
                list.hasFilters
                  ? "换个关键词或类型试试。"
                  : "把文件拖到这里，或点右上角的「上传附件」。插图、封面、分享图都从这里取。"
              }
              action={
                list.hasFilters ? (
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
          ) : (
            <ul className="grid grid-cols-2 gap-3 border-line border-t p-4 sm:grid-cols-3 lg:grid-cols-5 xl:grid-cols-6">
              {items.map((media) => (
                <MediaCell
                  key={media.id}
                  media={media}
                  canManage={canManage}
                  onOpen={() => setSelected(media)}
                  onChanged={() => void query.refetch()}
                />
              ))}
            </ul>
          )}
        </ListPanel>
      </div>

      <MediaDetail
        media={selected}
        canManage={selected ? canManage(selected) : false}
        onClose={() => setSelected(null)}
        onChanged={() => void query.refetch()}
      />
    </>
  );
}

/** 网格里的一格。 */
function MediaCell({
  media,
  canManage,
  onOpen,
  onChanged,
}: {
  media: Media;
  canManage: (media: Media) => boolean;
  onOpen: () => void;
  onChanged: () => void;
}) {
  const [confirmDelete, setConfirmDelete] = useState(false);
  const kind = kindOf(media);
  const Icon = kind.icon;
  const deletable = canManage(media);

  const remove = useMutation({
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

  return (
    <li className="group relative">
      <button
        type="button"
        onClick={onOpen}
        className="transition-ui block w-full overflow-hidden rounded-panel border border-line bg-surface-raised text-left hover:border-line-strong"
      >
        {/* 固定宽高比：图片加载前后格子不跳（CLS 是检索点名的 HIGH 项） */}
        <span className="flex aspect-square items-center justify-center overflow-hidden">
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
        <span className="block px-2 py-1.5">
          <span
            className="block truncate text-xs font-medium text-ink"
            title={media.originalName}
          >
            {media.originalName}
          </span>
          <span className="tabular block text-xs text-ink-subtle">
            {kind.label} · {fileSize(media.size)}
          </span>
        </span>
      </button>

      {deletable ? (
        <Button
          variant="secondary"
          size="icon-sm"
          onClick={() => setConfirmDelete(true)}
          aria-label={`删除 ${media.originalName}`}
          title="删除"
          /*
           * 触屏没有 hover，只在悬停时显示删除按钮等于在触屏上无法删除。
           * 故用 `opacity-0 group-hover:opacity-100` 之外再补一条
           * `focus-visible:opacity-100`，并用 max-md 让窄屏常显。
           */
          className="absolute top-1.5 right-1.5 opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100 max-md:opacity-100"
        >
          <Trash2 aria-hidden="true" />
        </Button>
      ) : null}

      <ConfirmDialog
        open={confirmDelete}
        onOpenChange={setConfirmDelete}
        title={`删除「${media.originalName}」？`}
        consequence={
          <p>
            <strong className="font-medium text-ink">这一步无法撤销。</strong>
            原图与全部缩略图会一并从存储中删除。
            已经引用它的文章里，那张图会变成死链。
          </p>
        }
        confirmLabel="删除"
        pending={remove.isPending}
        onConfirm={async () => {
          await remove.mutateAsync().catch(() => {});
          setConfirmDelete(false);
        }}
      />
    </li>
  );
}

/** 详情抽屉（用对话框承载，保持与全站浮层一致）。 */
function MediaDetail({
  media,
  canManage,
  onClose,
  onChanged,
}: {
  media: Media | null;
  canManage: boolean;
  onClose: () => void;
  onChanged: () => void;
}) {
  const [alt, setAlt] = useState("");
  const [title, setTitle] = useState("");
  const [copied, setCopied] = useState(false);
  const [initialised, setInitialised] = useState<number | null>(null);

  // 打开详情时把值填进表单。用 id 判断而不是媒体对象：
  // 列表刷新后对象引用会变，那会把用户正在编辑的内容覆盖掉。
  if (media && initialised !== media.id) {
    setAlt(media.alt);
    setTitle(media.title);
    setInitialised(media.id);
    setCopied(false);
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
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle className="truncate">{media?.originalName}</DialogTitle>
        </DialogHeader>
        <DialogBody className="flex flex-col gap-4">
          {media?.kind === "image" ? (
            <img
              src={media.url}
              alt={media.alt || media.originalName}
              className="max-h-72 w-full rounded-control border border-line bg-surface-raised object-contain"
            />
          ) : (
            <div className="flex h-32 items-center justify-center rounded-control border border-line bg-surface-raised">
              <p className="text-sm text-ink-muted">
                {media ? kindOf(media).label : "文件"}，无法预览
              </p>
            </div>
          )}

          {/* 只读元信息。用 dl 而不是表格：这是「名称—值」对，不是表格数据 */}
          <dl className="grid grid-cols-[6rem_1fr] gap-x-3 gap-y-1.5 text-sm">
            <dt className="text-ink-muted">地址</dt>
            <dd className="flex min-w-0 items-center gap-2">
              <code className="token min-w-0 flex-1 text-xs text-ink">
                {media?.url}
              </code>
              <Button
                variant="secondary"
                size="sm"
                onClick={async () => {
                  if (!media) {
                    return;
                  }
                  // 用完整地址而不是相对路径：站长多半要粘到文章正文或别处
                  const absolute = new URL(media.url, window.location.origin)
                    .href;
                  try {
                    await navigator.clipboard.writeText(absolute);
                    setCopied(true);
                  } catch {
                    // 非 HTTPS 或权限被拒时剪贴板不可用，退回让用户手动选中
                    setCopied(false);
                  }
                }}
              >
                {copied ? "已复制" : "复制"}
              </Button>
            </dd>

            <dt className="text-ink-muted">类型</dt>
            <dd className="text-ink">{media?.mime}</dd>

            {media?.kind === "image" && media.width > 0 ? (
              <>
                <dt className="text-ink-muted">尺寸</dt>
                <dd className="tabular text-ink">
                  {media.width} × {media.height}
                </dd>
              </>
            ) : null}

            <dt className="text-ink-muted">体积</dt>
            <dd className="tabular text-ink">{fileSize(media?.size)}</dd>

            <dt className="text-ink-muted">存储</dt>
            <dd className="text-ink">
              {media?.driver === "s3" ? "S3 兼容存储" : "本地"}
              {media?.thumbnails && media.thumbnails.length > 0 ? (
                <span className="ml-2 text-xs text-ink-muted">
                  {media.thumbnails.length} 档缩略图
                </span>
              ) : null}
            </dd>

            <dt className="text-ink-muted">上传者</dt>
            <dd className="text-ink">
              {media?.uploader?.displayName || media?.uploader?.username || "—"}
            </dd>

            <dt className="text-ink-muted">上传于</dt>
            <dd className="text-ink">{absoluteDate(media?.createdAt)}</dd>

            <dt className="text-ink-muted">校验和</dt>
            <dd className="token text-xs text-ink-muted">{media?.checksum}</dd>
          </dl>

          <div className="flex flex-col gap-4 border-line border-t pt-4">
            <Field>
              <FieldLabel htmlFor="media-alt">替代文本（alt）</FieldLabel>
              <Input
                id="media-alt"
                value={alt}
                onChange={(e) => setAlt(e.target.value)}
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
              />
              <FieldDescription>
                鼠标悬停时显示，也用于附件库检索。
              </FieldDescription>
            </Field>
          </div>
        </DialogBody>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>
            关闭
          </Button>
          <Button
            variant="primary"
            disabled={save.isPending || !canManage}
            onClick={() => save.mutate()}
          >
            {save.isPending ? "正在保存" : "保存"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
