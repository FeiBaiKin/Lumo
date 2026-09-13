import { api } from "@/api/client";
import { useAuth } from "@/components/auth/auth-provider";
import { FilterMenu } from "@/components/data/entity";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
} from "@/components/ui/field";
import { Input, SearchInput } from "@/components/ui/input";
import { Pagination } from "@/components/ui/pagination";
import { EmptyState, ErrorState, Skeleton } from "@/components/ui/states";
import { Tabbar } from "@/components/ui/tabs";
import {
  MEDIA_KIND_OPTIONS,
  type Media,
  type MediaKind,
  kindOf,
  previewOf,
  useMediaUpload,
} from "@/lib/media";
import { useDebouncedSearch } from "@/lib/use-list-params";
import { cn } from "@/lib/utils";
import { useQuery } from "@tanstack/react-query";
import { Check, Image as ImageIcon, Link2, Upload } from "lucide-react";
import { useRef, useState } from "react";

/**
 * 附件选择器。
 *
 * 一个弹窗管全部「要一张图」的场合：编辑器插图、封面图、设置表单的图片字段。
 * 形态照附件页的网格来 —— 同一批图在两处长得一样，站长不必重新认一遍界面。
 *
 * 三条刻意的决定：
 *
 *   1. **关着的时候不挂任何 hook**。弹窗体是独立组件，`open` 为假时整个不渲染，
 *      于是不发请求、不读会话。这让它可以无条件写在任何表单控件里 ——
 *      包括在 AuthProvider 之外被渲染的通用表单（表单引擎的测试就是这么跑的）。
 *   2. **上传后自动选中**。传图的人下一步一定是插入它，让他再去网格里找一遍
 *      是白费一次点击；多选时还要记对顺序。
 *   3. **保留「网络地址」页签**。外链图片（CDN、图床）是真实用法，
 *      只给选择器等于砍掉一半场景；但默认页签是附件库 —— 自托管才是主路径。
 */

/** 选中的结果。不直接给 Media：调用方要的只是「填哪个地址、alt 写什么」。 */
export type MediaPick = { url: string; alt: string; title: string };

/** 每页条数固定：弹窗高度是定的，24 格刚好铺满，不必再给「每页条数」选择器。 */
const PAGE_SIZE = 24;

/** 只允许 http(s) 与站内绝对路径，与正文净化的 URL 策略一致。 */
const SAFE_URL = /^(https?:\/\/|\/)/i;

export function MediaPickerDialog({
  open,
  onOpenChange,
  onSelect,
  kind = "image",
  multiple = false,
  title = "选择图片",
  confirmLabel = "插入",
  allowUrl = true,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** 确认时回调。单选时数组只有一项。 */
  onSelect: (picks: MediaPick[]) => void;
  /** 限定类型；空串表示不限，此时弹窗里多一个类型筛选。 */
  kind?: MediaKind | "";
  multiple?: boolean;
  title?: string;
  confirmLabel?: string;
  /** 是否提供「网络地址」页签。 */
  allowUrl?: boolean;
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      {open ? (
        // 高度定死：翻页时弹窗不忽高忽低，最后一页不足一屏也不会塌下去
        <DialogContent size="xl" className="h-[min(42rem,calc(100dvh-3rem))]">
          <PickerBody
            onSelect={onSelect}
            onClose={() => onOpenChange(false)}
            kind={kind}
            multiple={multiple}
            title={title}
            confirmLabel={confirmLabel}
            allowUrl={allowUrl}
          />
        </DialogContent>
      ) : null}
    </Dialog>
  );
}

function PickerBody({
  onSelect,
  onClose,
  kind,
  multiple,
  title,
  confirmLabel,
  allowUrl,
}: {
  onSelect: (picks: MediaPick[]) => void;
  onClose: () => void;
  kind: MediaKind | "";
  multiple: boolean;
  title: string;
  confirmLabel: string;
  allowUrl: boolean;
}) {
  const { can } = useAuth();
  const canUpload = can("media:write");

  const [tab, setTab] = useState<"library" | "url">("library");
  const [page, setPage] = useState(1);
  const [query, setQuery] = useState("");
  const [search, setSearch] = useDebouncedSearch(query, (value) => {
    setQuery(value);
    // 换了关键词就回到第一页：停在第三页看一个只有一页结果的筛选，得到的是空白
    setPage(1);
  });
  // 固定了类型就不给筛选；不固定时这是弹窗内的本地筛选，不写进地址栏
  const [kindFilter, setKindFilter] = useState<string>(kind);
  const [picked, setPicked] = useState<Media[]>([]);

  const [dragging, setDragging] = useState(false);
  const dragDepth = useRef(0);
  const fileInput = useRef<HTMLInputElement>(null);

  const { uploading, uploadFiles } = useMediaUpload();

  const list = useQuery({
    // 键以 media 开头：上传后由 useMediaUpload 统一失效，这里不必自己 refetch
    queryKey: ["media", "picker", page, query, kindFilter],
    queryFn: async () => {
      const { data, response } = await api.GET("/api/v1/console/media", {
        params: {
          query: {
            page,
            size: PAGE_SIZE,
            ...(kindFilter ? { kind: kindFilter as MediaKind } : {}),
            ...(query ? { q: query } : {}),
          },
        },
      });
      if (!response.ok) {
        throw new Error(`载入附件失败（HTTP ${response.status}）`);
      }
      return data;
    },
  });

  const items = list.data?.items ?? [];
  const total = list.data?.total ?? 0;

  function toggle(media: Media) {
    setPicked((prev) => {
      const exists = prev.some((item) => item.id === media.id);
      if (!multiple) {
        return exists ? [] : [media];
      }
      return exists
        ? prev.filter((item) => item.id !== media.id)
        : [...prev, media];
    });
  }

  function commit(chosen: Media[]) {
    if (chosen.length === 0) {
      return;
    }
    onSelect(chosen.map(toPick));
    onClose();
  }

  async function accept(files: FileList | File[]) {
    const added = await uploadFiles(files);
    if (added.length === 0) {
      return;
    }
    // 刚传的排在第一页最前；回到第一页才看得见它已经被选上
    setPage(1);
    setPicked((prev) => (multiple ? [...prev, ...added] : added.slice(-1)));
  }

  return (
    <>
      <DialogHeader className="gap-3">
        <DialogTitle>{title}</DialogTitle>
        {allowUrl ? (
          <Tabbar
            items={[
              { value: "library", label: "附件库", icon: ImageIcon },
              { value: "url", label: "网络地址", icon: Link2 },
            ]}
            value={tab}
            onChange={(value) => setTab(value === "url" ? "url" : "library")}
            variant="pills"
            size="sm"
            ariaLabel="图片来源"
            className="self-start"
          />
        ) : null}
      </DialogHeader>

      {tab === "url" ? (
        <UrlPane
          confirmLabel={confirmLabel}
          onCancel={onClose}
          onInsert={(pick) => {
            onSelect([pick]);
            onClose();
          }}
        />
      ) : (
        <>
          <div className="flex flex-wrap items-center gap-3 border-line border-b px-5 py-3">
            <SearchInput
              value={search}
              onValueChange={setSearch}
              placeholder="按原始文件名筛选"
              aria-label="筛选附件"
              className="max-w-xs"
            />
            {kind === "" ? (
              <FilterMenu
                label="类型"
                value={kindFilter}
                options={MEDIA_KIND_OPTIONS}
                onChange={(value) => {
                  setKindFilter(value);
                  setPage(1);
                }}
              />
            ) : null}
            {canUpload ? (
              <>
                <input
                  ref={fileInput}
                  type="file"
                  multiple
                  hidden
                  accept={kind === "image" ? "image/*" : undefined}
                  onChange={(event) => {
                    if (event.target.files) {
                      void accept(event.target.files);
                    }
                    // 清空以便连续两次选同一个文件也能触发 change
                    event.target.value = "";
                  }}
                />
                <Button
                  variant="secondary"
                  size="sm"
                  loading={uploading}
                  onClick={() => fileInput.current?.click()}
                  className="ml-auto"
                >
                  <Upload aria-hidden="true" />
                  上传
                </Button>
              </>
            ) : null}
          </div>

          <DialogBody
            // 拖放覆盖整个网格区，与附件页同样的手感
            onDragEnter={(event) => {
              event.preventDefault();
              if (!canUpload) {
                return;
              }
              dragDepth.current += 1;
              setDragging(true);
            }}
            onDragOver={(event) => event.preventDefault()}
            onDragLeave={(event) => {
              event.preventDefault();
              // 用计数而不是直接置 false：拖过子元素时会触发 leave，
              // 直接置 false 会让遮罩不停闪烁。
              dragDepth.current -= 1;
              if (dragDepth.current <= 0) {
                dragDepth.current = 0;
                setDragging(false);
              }
            }}
            onDrop={(event) => {
              event.preventDefault();
              dragDepth.current = 0;
              setDragging(false);
              if (canUpload && event.dataTransfer.files.length > 0) {
                void accept(event.dataTransfer.files);
              }
            }}
            className="relative"
          >
            {dragging ? (
              <div className="pointer-events-none absolute inset-3 z-sticky flex items-center justify-center rounded-card border-2 border-seal border-dashed bg-seal-soft/90">
                <p className="flex items-center gap-2 font-medium text-seal">
                  <Upload aria-hidden="true" className="size-5" />
                  松手即上传
                </p>
              </div>
            ) : null}

            {list.isLoading ? (
              <ul
                className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4"
                aria-busy="true"
              >
                {Array.from({ length: 8 }, (_, i) => (
                  // biome-ignore lint/suspicious/noArrayIndexKey: 静态占位
                  <li key={i}>
                    <Skeleton className="aspect-[10/8] w-full" />
                  </li>
                ))}
              </ul>
            ) : list.error ? (
              <ErrorState
                message={list.error.message}
                onRetry={() => void list.refetch()}
              />
            ) : items.length === 0 ? (
              <EmptyState
                icon={ImageIcon}
                title={
                  query || kindFilter !== kind
                    ? "没有匹配的附件"
                    : "附件库是空的"
                }
                description={
                  canUpload
                    ? "把文件拖到这里，或点上方的「上传」。"
                    : "还没有可用的附件，且当前账号没有上传权限。"
                }
                action={
                  canUpload ? (
                    <Button
                      variant="primary"
                      size="sm"
                      loading={uploading}
                      onClick={() => fileInput.current?.click()}
                    >
                      上传文件
                    </Button>
                  ) : null
                }
              />
            ) : (
              <ul className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4">
                {items.map((media) => (
                  <li key={media.id}>
                    <Tile
                      media={media}
                      order={picked.findIndex((item) => item.id === media.id)}
                      multiple={multiple}
                      onToggle={() => toggle(media)}
                      // 单选时双击即插入，省掉「选中再按插入」那一步
                      onQuickPick={multiple ? undefined : () => commit([media])}
                    />
                  </li>
                ))}
              </ul>
            )}
          </DialogBody>

          <Pagination
            page={page}
            size={PAGE_SIZE}
            total={total}
            onPageChange={setPage}
          />

          <DialogFooter>
            {multiple ? (
              <p className="mr-auto text-sm text-ink-muted">
                {picked.length === 0
                  ? "还没有选中"
                  : `已选 ${picked.length} 张`}
              </p>
            ) : null}
            <Button variant="secondary" onClick={onClose}>
              取消
            </Button>
            <Button
              variant="primary"
              disabled={picked.length === 0}
              onClick={() => commit(picked)}
            >
              {confirmLabel}
            </Button>
          </DialogFooter>
        </>
      )}
    </>
  );
}

function toPick(media: Media): MediaPick {
  return { url: media.url, alt: media.alt, title: media.title };
}

/** 网格里的一格。选中态用印色描边加角标，不靠改变亮度 —— 深色下看不出来。 */
function Tile({
  media,
  order,
  multiple,
  onToggle,
  onQuickPick,
}: {
  media: Media;
  /** 在已选列表里的位置，-1 表示未选。 */
  order: number;
  multiple: boolean;
  onToggle: () => void;
  onQuickPick?: (() => void) | undefined;
}) {
  const selected = order >= 0;
  const kind = kindOf(media);
  const Icon = kind.icon;

  return (
    <button
      type="button"
      onClick={onToggle}
      onDoubleClick={onQuickPick}
      aria-pressed={selected}
      title={media.originalName}
      className={cn(
        "transition-ui relative block w-full overflow-hidden rounded-control border text-left",
        selected
          ? "border-seal ring-2 ring-seal"
          : "border-line hover:border-line-strong hover:shadow-popover",
      )}
    >
      {/* 固定宽高比：图片加载前后格子不跳 */}
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
      <span className="block bg-surface px-2 py-1.5">
        <span className="block truncate text-center text-xs font-medium text-ink">
          {media.originalName}
        </span>
      </span>
      {selected ? (
        <span className="absolute top-1.5 left-1.5 flex size-5 items-center justify-center rounded-full bg-seal text-xs font-semibold text-seal-on">
          {multiple ? (
            order + 1
          ) : (
            <Check aria-hidden="true" className="size-3.5" strokeWidth={3} />
          )}
        </span>
      ) : null}
    </button>
  );
}

/** 「网络地址」页签：填地址 + 替代文本，带即时预览。 */
function UrlPane({
  onInsert,
  onCancel,
  confirmLabel,
}: {
  onInsert: (pick: MediaPick) => void;
  onCancel: () => void;
  confirmLabel: string;
}) {
  const [url, setUrl] = useState("");
  const [alt, setAlt] = useState("");
  const [error, setError] = useState("");
  const [broken, setBroken] = useState(false);

  const value = url.trim();

  function submit() {
    if (!SAFE_URL.test(value)) {
      setError("只支持 http(s) 地址或站内绝对路径（以 / 开头）");
      return;
    }
    onInsert({ url: value, alt: alt.trim(), title: "" });
  }

  return (
    <>
      <DialogBody className="flex flex-col gap-4">
        <Field>
          <FieldLabel htmlFor="picker-url">图片地址</FieldLabel>
          <Input
            id="picker-url"
            value={url}
            onChange={(event) => {
              setUrl(event.target.value);
              setError("");
              setBroken(false);
            }}
            onKeyDown={(event) => {
              if (event.key === "Enter") {
                event.preventDefault();
                submit();
              }
            }}
            placeholder="https://… 或 /uploads/…"
            aria-invalid={error ? true : undefined}
            aria-describedby={error ? "picker-url-error" : undefined}
          />
          <FieldError id="picker-url-error">{error}</FieldError>
          <FieldDescription>
            外链图片由对方服务器提供，它失效时站点上的图也会跟着失效；
            自己的图建议传进附件库。
          </FieldDescription>
        </Field>

        <Field>
          <FieldLabel htmlFor="picker-alt">替代文本（alt）</FieldLabel>
          <Input
            id="picker-alt"
            value={alt}
            onChange={(event) => setAlt(event.target.value)}
            placeholder="描述图里有什么"
          />
          <FieldDescription>
            图加载失败或读屏时显示这句话。装饰性图片可以留空。
          </FieldDescription>
        </Field>

        <div className="flex aspect-video max-h-56 items-center justify-center overflow-hidden rounded-control border border-line bg-surface-active">
          {value && SAFE_URL.test(value) && !broken ? (
            <img
              src={value}
              alt=""
              className="size-full object-contain"
              onError={() => setBroken(true)}
            />
          ) : (
            <p className="px-4 text-center text-sm text-ink-muted">
              {broken ? "这个地址没能加载出图片" : "填入地址后在此预览"}
            </p>
          )}
        </div>
      </DialogBody>

      <DialogFooter>
        <Button variant="secondary" onClick={onCancel}>
          取消
        </Button>
        <Button variant="primary" disabled={!value} onClick={submit}>
          {confirmLabel}
        </Button>
      </DialogFooter>
    </>
  );
}
