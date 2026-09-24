import { api, problemMessage } from "@/api/client";
import type { components } from "@/api/schema";
import { useAuth } from "@/components/auth/auth-provider";
import {
  Entity,
  EntityEnd,
  EntityField,
  EntityList,
  EntityStart,
} from "@/components/data/entity";
import { PageHeader } from "@/components/layout/page-header";
import { MediaPickerDialog } from "@/components/media/media-picker";
import { UnsavedChangesGuard } from "@/components/navigation/unsaved-guard";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
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
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";
import { Input, Textarea } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { EmptyState, ErrorState, Skeleton } from "@/components/ui/states";
import { type DotState, StatusDot } from "@/components/ui/status-dot";
import { Tabbar } from "@/components/ui/tabs";
import { CheckboxRow, SwitchRow } from "@/components/ui/toggle";
import {
  absoluteDate,
  fromLocalInput,
  relativeTime,
  toLocalInput,
} from "@/lib/format";
import { flattenCategories, useTaxonomyOptions } from "@/lib/taxonomy";
import { useDocumentTitle } from "@/lib/use-document-title";
import { cn } from "@/lib/utils";
import { useMutation, useQuery } from "@tanstack/react-query";
import {
  CalendarClock,
  Check,
  Eye,
  FileCode2,
  FileText,
  History,
  Image as ImageIcon,
  ListTree,
  Save,
  Settings,
  Undo2,
  X,
} from "lucide-react";
import {
  type ReactNode,
  Suspense,
  lazy,
  useCallback,
  useDeferredValue,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { Link, useNavigate, useParams } from "react-router";
import { toast } from "sonner";

/*
 * 两个编辑器各是一整套 ProseMirror 生态（TipTap 带着代码着色的语法，Milkdown 带着自己的解析器），
 * 一篇文章只会用到其中一个，故各自按需加载，不让写 Markdown 的人也下载一遍 TipTap。
 */
const HtmlEditor = lazy(() =>
  import("@/components/editor/html-editor").then((m) => ({
    default: m.HtmlEditor,
  })),
);
const MarkdownEditor = lazy(() =>
  import("@/components/editor/markdown-editor").then((m) => ({
    default: m.MarkdownEditor,
  })),
);

/**
 * 内容编辑器（文章与独立页面共用），形态对齐 Halo 的 PostEditor：
 *
 *   - 页头承担全部动作：格式、预览、修订历史、保存、设置、发布
 *   - 页头之下是一条全宽的工具条带（TipTap 的格式工具条经 portal 挂在这里）
 *   - 再往下整块是「纸」：封面、标题输入与正文都写在同一张纸上，
 *     内容列居中限宽；右侧一条附栏放大纲与详情
 *   - 发布设置（分类、标签、摘要、封面、可见性、定时）收在弹窗里，不占正文的宽度
 *
 * 一个页面同时承担新建、编辑、发布、定时、撤回 —— 因为它们共享同一份表单状态，
 * 拆成多个路由会让「改完标题再点发布」变成两次导航。
 *
 * **两个编辑器产出同一对字段**：`raw` + `rawType`。
 * 切换**不是无损的**：格式标记一变，原稿就会被另一种编辑器按自己的语法重新解析，
 * 故切换时弹确认把这件事说清楚。但**绝不会清空正文**——过去的实现在切到
 * Markdown 时直接把 raw 置空，等于一键删掉整篇内容。
 *
 * 外层按 `kind + id` 加 key：从文章 A 跳到文章 B 时组件必须重建，
 * 否则表单里还留着 A 的内容、loaded 也是 true，B 的数据到了会被丢掉，
 * 下一次保存就把 A 的正文写到了 B 上。
 */
type ContentType = "post" | "page";
type Post = components["schemas"]["Post"];
type RevisionSummary = components["schemas"]["RevisionSummary"];
type RawType = "html" | "markdown";
type Status = "draft" | "published" | "scheduled" | "trashed";

/** 空文档的初始内容。两个编辑器各自需要一点种子才不会渲染出空白。 */
const EMPTY_HTML = "<p></p>";
const EMPTY_MARKDOWN = "";

const STATUS_META: Record<
  Status,
  { label: string; state: DotState; pulse?: boolean }
> = {
  draft: { label: "草稿", state: "neutral" },
  published: { label: "已发布", state: "ok" },
  scheduled: { label: "定时发布", state: "warn", pulse: true },
  trashed: { label: "回收站", state: "danger" },
};

const FORMAT_ITEMS = [
  { value: "html", label: "富文本", icon: FileText },
  { value: "markdown", label: "Markdown", icon: FileCode2 },
];

type Heading = { level: number; text: string };

/** 从原稿里取出标题做大纲。HTML 交给 DOMParser，Markdown 按行首的 # 规则。 */
function parseOutline(raw: string, rawType: RawType): Heading[] {
  if (rawType === "markdown") {
    const out: Heading[] = [];
    let inFence = false;
    for (const line of raw.split("\n")) {
      if (/^\s*(```|~~~)/.test(line)) {
        // 代码块里的 # 是注释不是标题
        inFence = !inFence;
        continue;
      }
      if (inFence) {
        continue;
      }
      const match = /^(#{1,3})\s+(.+?)\s*#*\s*$/.exec(line);
      if (match) {
        out.push({
          level: (match[1] ?? "#").length,
          text: match[2] ?? "",
        });
      }
    }
    return out;
  }
  if (typeof DOMParser === "undefined") {
    return [];
  }
  const doc = new DOMParser().parseFromString(raw, "text/html");
  return [...doc.body.querySelectorAll("h1, h2, h3")].map((element) => ({
    level: Number(element.tagName.slice(1)),
    text: element.textContent?.trim() || "（无标题）",
  }));
}

/** 去掉标记后的纯文本，用于字数统计。 */
function plainText(raw: string, rawType: RawType): string {
  if (rawType === "markdown") {
    return raw;
  }
  if (typeof DOMParser === "undefined") {
    return raw.replace(/<[^>]+>/g, "");
  }
  return (
    new DOMParser().parseFromString(raw, "text/html").body.textContent ?? ""
  );
}

/** 中文按字算、拉丁也按字符算，不数空白 —— 与「字数」一词的日常含义一致。 */
function charCount(text: string): number {
  return text.replace(/\s+/g, "").length;
}

export function ContentEditor({ kind }: { kind: ContentType }) {
  const params = useParams<{ id: string }>();
  return <EditorSession key={`${kind}:${params.id ?? "new"}`} kind={kind} />;
}

/** 一次编辑会话；外层 key 变化即整体重建。 */
function EditorSession({ kind }: { kind: ContentType }) {
  const params = useParams<{ id: string }>();
  const navigate = useNavigate();
  const { can } = useAuth();

  const isNew = params.id === "new" || params.id === undefined;
  const id = isNew ? null : Number(params.id);

  const listPath = kind === "post" ? "/posts" : "/pages";
  const label = kind === "post" ? "文章" : "页面";

  const [title, setTitle] = useState("");
  const [slug, setSlug] = useState("");
  const [rawType, setRawType] = useState<RawType>("html");
  const [raw, setRaw] = useState(EMPTY_HTML);
  const [excerpt, setExcerpt] = useState("");
  const [excerptAuto, setExcerptAuto] = useState(true);
  const [categoryIds, setCategoryIds] = useState<number[]>([]);
  const [tagIds, setTagIds] = useState<number[]>([]);
  const [coverUrl, setCoverUrl] = useState("");
  const [pinned, setPinned] = useState(false);
  const [visibility, setVisibility] = useState<"public" | "private">("public");
  const [template, setTemplate] = useState("");
  const [publishAt, setPublishAt] = useState("");
  const [loaded, setLoaded] = useState(isNew);
  const [dirty, setDirty] = useState(false);
  /**
   * 脏状态的同步副本。
   *
   * setState 是异步的，而保存成功的回调里要紧接着 navigate（新建后换地址），
   * 那一刻 dirty 还是旧值；导航拦截器读的必须是「此刻」的值，所以用 ref。
   */
  const dirtyRef = useRef(false);
  /** 同时更新状态与 ref：脏状态的每一次变更都必须走这里。 */
  const setDirtyFlag = useCallback((value: boolean) => {
    dirtyRef.current = value;
    setDirty(value);
  }, []);
  const [switchOpen, setSwitchOpen] = useState(false);
  const [pendingType, setPendingType] = useState<RawType | null>(null);
  const [settingsOpen, setSettingsOpen] = useState(false);
  /** 封面图的附件选择器。与设置弹窗并列挂载，不套在它里面。 */
  const [coverPicker, setCoverPicker] = useState(false);
  const [revisionsOpen, setRevisionsOpen] = useState(false);
  const [restoring, setRestoring] = useState<RevisionSummary | null>(null);
  const [sideTab, setSideTab] = useState("outline");
  /** 编辑器实例的 key。切换格式或恢复修订后自增，让编辑器整体重建以载入新内容。 */
  const [editorKey, setEditorKey] = useState(0);
  /** 工具条的挂载点：页头之下那条白带。用 state 存元素，挂上后才把工具条送过去。 */
  const [toolbarSlot, setToolbarSlot] = useState<HTMLDivElement | null>(null);
  const surfaceRef = useRef<HTMLDivElement>(null);
  const titleRef = useRef<HTMLInputElement>(null);

  useDocumentTitle(isNew ? `新建${label}` : title || label);

  // 新建时把光标放进标题：这一页的唯一目的就是开始写，不该先让人点一下
  useEffect(() => {
    if (isNew) {
      titleRef.current?.focus();
    }
  }, [isNew]);

  // ---- 载入已有内容 ----
  const query = useQuery({
    queryKey: ["content", kind, id],
    enabled: id !== null,
    queryFn: async () => {
      const { data, response } =
        kind === "post"
          ? await api.GET("/api/v1/console/posts/{id}", {
              params: { path: { id: id as number } },
            })
          : await api.GET("/api/v1/console/pages/{id}", {
              params: { path: { id: id as number } },
            });
      if (!response.ok) {
        throw new Error(`载入${label}失败（HTTP ${response.status}）`);
      }
      return data;
    },
  });

  /** 把一条记录填进表单。首次载入与恢复修订都走这里。 */
  const fillFrom = useCallback((post: Post) => {
    setTitle(post.title);
    setSlug(post.slug);
    setRawType(post.rawType as RawType);
    setRaw(
      post.raw || (post.rawType === "markdown" ? EMPTY_MARKDOWN : EMPTY_HTML),
    );
    setExcerpt(post.excerpt);
    setExcerptAuto(post.excerptAuto);
    setCategoryIds((post.categories ?? []).map((c) => c.id));
    setTagIds((post.tags ?? []).map((t) => t.id));
    setCoverUrl(post.coverUrl);
    setPinned(post.pinned);
    setVisibility(post.visibility as "public" | "private");
    setTemplate(post.template);
    setPublishAt(toLocalInput(post.publishedAt));
  }, []);

  // 数据到达后填表。只在首次填（loaded 为 false 时），
  // 否则每次重新拉取都会把用户正在编辑的内容冲掉。
  useEffect(() => {
    const post = query.data;
    if (!post || loaded) {
      return;
    }
    fillFrom(post);
    setLoaded(true);
  }, [query.data, loaded, fillFrom]);

  // ---- 离开前提醒 ----
  // 刷新与关闭标签页走 beforeunload；站内导航（Link、navigate、浏览器后退）
  // 不会触发它，由下方渲染的 UnsavedChangesGuard 负责。
  useEffect(() => {
    if (!dirty) {
      return;
    }
    const onBeforeUnload = (event: BeforeUnloadEvent) => {
      event.preventDefault();
    };
    window.addEventListener("beforeunload", onBeforeUnload);
    return () => window.removeEventListener("beforeunload", onBeforeUnload);
  }, [dirty]);

  const markDirty = useCallback(() => setDirtyFlag(true), [setDirtyFlag]);

  // ---- 分类与标签候选 ----
  // 与文章列表共用同一份缓存（lib/taxonomy.ts）：缓存里是原始 DTO，
  // 各页面自己转换。曾经两边共用同一个 key 却写入不同结构，
  // 结果编辑器拿到的是列表页的 { value, label }，分类 ID 变成 undefined。
  const taxonomy = useTaxonomyOptions(kind === "post");
  const categoryOptions = useMemo(
    () => flattenCategories(taxonomy.data?.categories ?? []),
    [taxonomy.data],
  );
  const tagOptions = useMemo(() => taxonomy.data?.tags ?? [], [taxonomy.data]);

  /** 页面模板候选：来自当前主题提供的 page-*.html。 */
  const pageTemplates = useQuery({
    queryKey: ["page-templates"],
    enabled: kind === "page",
    queryFn: async () => {
      const { data } = await api.GET("/api/v1/console/themes");
      const active = data?.items?.find((theme) => theme.active);
      return active?.pageTemplates ?? [];
    },
  });

  /** 修订历史。新建时还没有记录，不查。 */
  const revisions = useQuery({
    queryKey: ["revisions", kind, id],
    enabled: id !== null,
    queryFn: async () => {
      const { data, response } =
        kind === "post"
          ? await api.GET("/api/v1/console/posts/{id}/revisions", {
              params: { path: { id: id as number } },
            })
          : await api.GET("/api/v1/console/pages/{id}/revisions", {
              params: { path: { id: id as number } },
            });
      if (!response.ok) {
        throw new Error(`载入修订历史失败（HTTP ${response.status}）`);
      }
      return data?.items ?? [];
    },
  });

  /** 提交给接口的请求体。两个编辑器产出同一对字段 raw + rawType。 */
  const payload = useMemo(() => {
    const out: Record<string, unknown> = {
      title: title.trim(),
      slug: slug.trim(),
      raw,
      rawType,
      excerpt: excerptAuto ? "" : excerpt.trim(),
      coverUrl: coverUrl.trim(),
      visibility,
    };
    if (kind === "post") {
      out.categoryIds = categoryIds;
      out.tagIds = tagIds;
      out.pinned = pinned;
    } else {
      out.template = template.trim();
    }
    return out;
  }, [
    title,
    slug,
    raw,
    rawType,
    excerpt,
    excerptAuto,
    coverUrl,
    visibility,
    kind,
    categoryIds,
    tagIds,
    pinned,
    template,
  ]);

  const save = useMutation({
    mutationFn: async () => {
      const { data, error, response } =
        kind === "post"
          ? id === null
            ? await api.POST("/api/v1/console/posts", {
                body: payload as never,
              })
            : await api.PUT("/api/v1/console/posts/{id}", {
                params: { path: { id } },
                body: payload as never,
              })
          : id === null
            ? await api.POST("/api/v1/console/pages", {
                body: payload as never,
              })
            : await api.PUT("/api/v1/console/pages/{id}", {
                params: { path: { id } },
                body: payload as never,
              });
      if (!response.ok) {
        throw new Error(problemMessage(error));
      }
      return data;
    },
    onSuccess: (data) => {
      setDirtyFlag(false);
      // 新建后把地址换成 /posts/<id>，否则再点保存会又建一篇
      if (id === null && data?.id) {
        navigate(`${listPath}/${data.id}`, { replace: true });
        return;
      }
      // 已有记录：重拉一次拿到服务端生成的 slug、摘要与新的修订；
      // 表单不会被冲掉，因为 loaded 已为 true
      void query.refetch();
      void revisions.refetch();
    },
    onError: (err) =>
      toast.error(err instanceof Error ? err.message : "保存失败"),
  });

  const publish = useMutation({
    // 目标 ID 由调用方给出而不是读闭包：新建内容要先保存拿到 ID 才能发布。
    mutationFn: async (vars: {
      action: "publish" | "unpublish";
      id: number;
    }) => {
      const { action, id: targetID } = vars;
      const scheduled =
        action === "publish" && publishAt && new Date(publishAt) > new Date();
      const iso = scheduled ? fromLocalInput(publishAt) : undefined;
      // 只有确实要定时才带 publishAt 字段。传 undefined 会被
      // exactOptionalPropertyTypes 与服务的严格校验同时拒绝。
      const publishBody = iso ? { publishAt: iso } : {};

      const { error, response } =
        action === "publish"
          ? kind === "post"
            ? await api.POST("/api/v1/console/posts/{id}/publish", {
                params: { path: { id: targetID } },
                body: publishBody,
              })
            : await api.POST("/api/v1/console/pages/{id}/publish", {
                params: { path: { id: targetID } },
                body: publishBody,
              })
          : kind === "post"
            ? await api.POST("/api/v1/console/posts/{id}/unpublish", {
                params: { path: { id: targetID } },
              })
            : await api.POST("/api/v1/console/pages/{id}/unpublish", {
                params: { path: { id: targetID } },
              });
      if (!response.ok) {
        throw new Error(problemMessage(error));
      }
      return action;
    },
    onSuccess: (action) => {
      if (action === "publish") {
        toast.success(
          publishAt && new Date(publishAt) > new Date()
            ? "已加入定时发布"
            : "已发布",
        );
      } else if (action === "unpublish") {
        toast.success("已撤回为草稿");
      }
      void query.refetch();
    },
    onError: (err) =>
      toast.error(err instanceof Error ? err.message : "操作失败"),
  });

  /**
   * 写操作闸门。
   *
   * `mutate` 在 pending 期间再调用会**再发一个请求**，而按钮的 disabled 依赖
   * `isPending`——state 更新是异步的，同一帧里的连点（或在慢网下反复点）
   * 全部会在闸门生效前穿过去，结果是重复创建内容、重复发布。
   * 这里用 ref 做同步判定，和按钮的 loading 一起构成两道闸门。
   */
  const writeRef = useRef(false);

  /** 保存；失败时静默返回（错误提示由 save.onError 负责）。 */
  const saveNow = useCallback(async () => {
    if (writeRef.current) {
      return;
    }
    writeRef.current = true;
    try {
      await save.mutateAsync();
    } catch {
      // 已经 toast 过了，这里只需要「不再往下走」。
    } finally {
      writeRef.current = false;
    }
  }, [save]);

  /** 发起发布或撤回；闸门占用中则忽略本次调用。 */
  const runPublish = useCallback(
    (action: "publish" | "unpublish", targetID: number) => {
      if (writeRef.current) {
        return;
      }
      writeRef.current = true;
      publish.mutate(
        { action, id: targetID },
        {
          onSettled: () => {
            writeRef.current = false;
          },
        },
      );
    },
    [publish],
  );

  /**
   * 先保存再发布。
   *
   * 两步必须是一个整体：过去保存的异常被 `.catch(() => {})` 吞掉后仍然继续发布，
   * 于是「slug 撞车导致保存 409」会变成「发布了服务器上那份旧内容」——
   * 用户看到的是发布成功，站点上是另一篇文章。
   */
  const saveAndPublish = useCallback(async () => {
    if (writeRef.current) {
      return;
    }
    writeRef.current = true;

    let saved: Post | undefined;
    if (dirtyRef.current || isNew) {
      try {
        saved = await save.mutateAsync();
      } catch {
        // 保存失败：错误已由 save.onError 提示，必须在此终止，
        // 不能退回「发布服务器上的旧内容」这条更糟的路径。
        writeRef.current = false;
        return;
      }
    }

    // 新建内容的 id 在此刻才存在，用闭包里的旧 id 发布等于什么都没做。
    const targetID = saved?.id ?? id;
    if (targetID === null) {
      writeRef.current = false;
      return;
    }
    publish.mutate(
      { action: "publish", id: targetID },
      {
        onSettled: () => {
          writeRef.current = false;
        },
      },
    );
  }, [id, isNew, save, publish]);

  const restore = useMutation({
    mutationFn: async (revisionId: number) => {
      if (id === null) {
        return undefined;
      }
      const { data, error, response } =
        kind === "post"
          ? await api.POST(
              "/api/v1/console/posts/{id}/revisions/{revisionId}/restore",
              { params: { path: { id, revisionId } } },
            )
          : await api.POST(
              "/api/v1/console/pages/{id}/revisions/{revisionId}/restore",
              { params: { path: { id, revisionId } } },
            );
      if (!response.ok) {
        throw new Error(problemMessage(error));
      }
      return data;
    },
    onSuccess: (data) => {
      if (data) {
        // 用接口返回的记录重填，并让编辑器整体重建：
        // 两个编辑器都不会可靠地响应「外部换掉全部内容」
        fillFrom(data);
        setEditorKey((k) => k + 1);
      }
      setDirtyFlag(false);
      setRestoring(null);
      setRevisionsOpen(false);
      void query.refetch();
      void revisions.refetch();
      toast.success("已恢复到该版本");
    },
    onError: (err) =>
      toast.error(err instanceof Error ? err.message : "恢复失败"),
  });

  // ---- 大纲与字数：正文每敲一个字都会变，用 deferred 值避免拖慢输入 ----
  const deferredRaw = useDeferredValue(raw);
  const outline = useMemo(
    () => parseOutline(deferredRaw, rawType),
    [deferredRaw, rawType],
  );
  const words = useMemo(
    () => charCount(plainText(deferredRaw, rawType)),
    [deferredRaw, rawType],
  );

  /** 把焦点交给正文。两个编辑器渲染出的都是 ProseMirror 视图。 */
  const focusBody = useCallback(() => {
    surfaceRef.current?.querySelector<HTMLElement>(".ProseMirror")?.focus();
  }, []);

  /** 点大纲里的某一条，滚到正文里对应的标题。 */
  const jumpToHeading = useCallback((index: number) => {
    const headings =
      surfaceRef.current?.querySelectorAll<HTMLElement>(
        ".ProseMirror h1, .ProseMirror h2, .ProseMirror h3",
      ) ?? [];
    headings[index]?.scrollIntoView({ block: "center", behavior: "smooth" });
  }, []);

  function requestFormat(next: RawType) {
    if (next === rawType) {
      return;
    }
    // 空内容时切换是无损的，不必打扰用户
    const isEmpty = !raw.trim() || raw === EMPTY_HTML;
    if (isEmpty) {
      setRaw(next === "markdown" ? EMPTY_MARKDOWN : EMPTY_HTML);
      setRawType(next);
      setEditorKey((k) => k + 1);
      markDirty();
      return;
    }
    setPendingType(next);
    setSwitchOpen(true);
  }

  if (query.isLoading && !isNew) {
    return (
      <>
        <PageHeader
          back={{ to: listPath, label: `返回${label}列表` }}
          title={`载入${label}`}
        />
        <div className="h-12 border-line border-b bg-surface" />
        <div className="bg-paper">
          <div
            className="mx-auto flex w-full max-w-measure flex-col gap-4 px-6 py-8 sm:px-14"
            aria-busy="true"
          >
            <Skeleton className="h-10 w-2/3" />
            <Skeleton className="h-[28rem] w-full" />
          </div>
        </div>
      </>
    );
  }

  if (query.error) {
    return (
      <>
        <PageHeader
          back={{ to: listPath, label: `返回${label}列表` }}
          title={label}
        />
        <ErrorState
          message={query.error.message}
          onRetry={() => void query.refetch()}
        />
      </>
    );
  }

  const post: Post | undefined = query.data;
  const status = (post?.status ?? "draft") as Status;
  const meta = STATUS_META[status];
  const canPublish = can(kind === "post" ? "posts:publish" : "pages:publish");
  // 没有这个权限时，正文保存到服务端会被按允许列表净化（见 internal/content/sanitize.go）。
  // 编辑器里不拦人，但要说清楚，否则作者会以为「iframe 明明写进去了」。
  const canUnsafeHTML = can("content:unsafe_html");
  const scheduledLater = Boolean(publishAt) && new Date(publishAt) > new Date();
  const previewHref =
    !isNew && post?.slug
      ? kind === "post"
        ? `/posts/${post.slug}`
        : `/${post.slug}`
      : null;

  return (
    <>
      <PageHeader
        back={{ to: listPath, label: `返回${label}列表` }}
        title={isNew ? `新建${label}` : title || "（无标题）"}
        description={
          <span className="flex flex-wrap items-center gap-x-3 gap-y-1">
            <StatusDot state={meta.state} pulse={meta.pulse ?? false}>
              {meta.label}
            </StatusDot>
            {post?.publishedAt ? (
              <span>
                {status === "scheduled" ? "预定于" : "发布于"}{" "}
                {absoluteDate(post.publishedAt)}
              </span>
            ) : null}
            {dirty ? <span>有未保存的修改</span> : null}
          </span>
        }
        actions={
          <>
            <Tabbar
              ariaLabel="正文格式"
              variant="pills"
              size="sm"
              value={rawType}
              onChange={(value) => requestFormat(value as RawType)}
              items={FORMAT_ITEMS}
            />

            {previewHref ? (
              <Button variant="secondary" size="sm" asChild>
                <a href={previewHref} target="_blank" rel="noopener noreferrer">
                  <Eye aria-hidden="true" />
                  预览
                </a>
              </Button>
            ) : null}

            {!isNew ? (
              <Button
                variant="secondary"
                size="sm"
                onClick={() => setRevisionsOpen(true)}
              >
                <History aria-hidden="true" />
                修订历史
              </Button>
            ) : null}

            <Button
              variant="secondary"
              size="sm"
              onClick={() => void saveNow()}
              loading={save.isPending}
              disabled={!title.trim() || publish.isPending}
            >
              <Save aria-hidden="true" />
              {dirty ? "保存" : "已保存"}
            </Button>

            <Button
              variant="secondary"
              size="sm"
              onClick={() => setSettingsOpen(true)}
            >
              <Settings aria-hidden="true" />
              设置
            </Button>

            {canPublish ? (
              status === "published" ? (
                <Button
                  variant="secondary"
                  size="sm"
                  onClick={() => {
                    if (id !== null) {
                      runPublish("unpublish", id);
                    }
                  }}
                  loading={publish.isPending}
                  disabled={dirty}
                  title={dirty ? "先保存再撤回" : "撤回为草稿"}
                >
                  <Undo2 aria-hidden="true" />
                  撤回
                </Button>
              ) : (
                <Button
                  variant="primary"
                  size="sm"
                  onClick={() => void saveAndPublish()}
                  // 保存阶段也要显示进行中：否则点完发布按钮看起来毫无反应，
                  // 用户会再点一次。
                  loading={save.isPending || publish.isPending}
                  disabled={!title.trim() || (!dirty && !isNew && id === null)}
                >
                  {scheduledLater ? (
                    <>
                      <CalendarClock aria-hidden="true" />
                      定时发布
                    </>
                  ) : (
                    <>
                      <Check aria-hidden="true" />
                      发布
                    </>
                  )}
                </Button>
              )
            ) : null}
          </>
        }
      />

      <div className="flex min-h-[calc(100dvh-var(--page-header-height))] items-stretch">
        <div className="flex min-w-0 flex-1 flex-col">
          {/* 工具条带：页头之下、正文之上的一条全宽白带，随页头一起吸顶 */}
          <div className="sticky top-page-header z-sticky border-line border-b bg-surface">
            {rawType === "html" ? (
              <div ref={setToolbarSlot} />
            ) : (
              <p className="flex h-12 items-center justify-center px-4 text-center text-sm text-ink-muted">
                Markdown 源码，语法与前台一致（CommonMark 与 GFM）
              </p>
            )}
          </div>

          {/*
            纸：外壳走冷调中性色，只有正在写的这块用主题的纸色。
            封面、标题与正文写在同一张纸上，内容列居中限宽。
          */}
          <div className="flex-1 bg-paper text-paper-ink">
            <div
              ref={surfaceRef}
              className="mx-auto flex w-full max-w-measure flex-col px-6 py-8 sm:px-14"
            >
              {coverUrl ? (
                <img
                  src={coverUrl}
                  alt="封面"
                  className="mb-6 aspect-[16/6] w-full rounded-widget border border-line object-cover"
                />
              ) : null}

              <label htmlFor="content-title" className="sr-only">
                标题
              </label>
              <input
                id="content-title"
                value={title}
                onChange={(e) => {
                  setTitle(e.target.value);
                  markDirty();
                }}
                onKeyDown={(e) => {
                  // 回车不是标题的一部分：把焦点交给正文，与 Halo 的编辑器一致
                  if (e.key === "Enter") {
                    e.preventDefault();
                    focusBody();
                  }
                }}
                placeholder={`${label}标题`}
                className="w-full border-line border-b bg-transparent py-2 text-3xl font-semibold leading-tight text-paper-ink outline-none placeholder:text-ink-subtle"
                // 标题是这条内容最重要的字段，且长度有 256 上限
                maxLength={256}
                ref={titleRef}
              />

              {/*
                编辑器只在数据填好后挂载：两个编辑器都不会可靠地响应
                「外部换掉全部内容」，先挂一个空的再换内容会让 Markdown 编辑器停在空白。
                key 让切换格式与恢复修订时编辑器整体重建。
              */}
              {loaded ? (
                <div key={editorKey}>
                  <Suspense
                    fallback={
                      <div className="flex min-h-[60vh] items-center justify-center text-sm text-ink-muted">
                        正在准备编辑器
                      </div>
                    }
                  >
                    {rawType === "html" ? (
                      <HtmlEditor
                        initialContent={raw || EMPTY_HTML}
                        toolbarContainer={toolbarSlot}
                        onChange={(html) => {
                          setRaw(html);
                          markDirty();
                        }}
                      />
                    ) : (
                      <MarkdownEditor
                        initialContent={raw}
                        onChange={(markdown) => {
                          setRaw(markdown);
                          markDirty();
                        }}
                      />
                    )}
                  </Suspense>
                </div>
              ) : null}
            </div>
          </div>
        </div>

        {/* 右侧附栏：大纲与详情。窄屏不显示 —— 那时正文的宽度更要紧 */}
        <aside className="hidden w-72 shrink-0 flex-col border-line border-l bg-surface lg:flex">
          <div className="sticky top-page-header flex flex-col gap-3 p-3">
            <Tabbar
              ariaLabel="附栏"
              variant="pills"
              size="sm"
              value={sideTab}
              onChange={setSideTab}
              items={[
                { value: "outline", label: "大纲", icon: ListTree },
                { value: "details", label: "详情", icon: FileText },
              ]}
            />
            {sideTab === "outline" ? (
              outline.length === 0 ? (
                <p className="px-2 py-6 text-center text-sm text-ink-muted">
                  正文里还没有标题。加上一级到三级标题，这里会列出它们。
                </p>
              ) : (
                <ol className="flex flex-col gap-0.5">
                  {outline.map((heading, index) => (
                    <li
                      // 同名标题可以重复出现，位置才是唯一标识
                      // biome-ignore lint/suspicious/noArrayIndexKey: 大纲按位置对应正文里的标题
                      key={index}
                    >
                      <button
                        type="button"
                        onClick={() => jumpToHeading(index)}
                        className={cn(
                          "transition-ui w-full truncate rounded-control px-2 py-1 text-left text-sm text-ink hover:bg-surface-active",
                          heading.level === 1 && "font-medium",
                          heading.level === 2 && "pl-5 text-ink-muted",
                          heading.level === 3 && "pl-8 text-ink-muted",
                        )}
                        title={heading.text}
                      >
                        {heading.text}
                      </button>
                    </li>
                  ))}
                </ol>
              )
            ) : (
              <DescriptionList className="grid-cols-[5rem_minmax(0,1fr)] px-2 py-1 text-sm">
                <DescriptionTerm>字数</DescriptionTerm>
                <DescriptionDetail className="tabular">
                  {words}
                </DescriptionDetail>
                <DescriptionTerm>状态</DescriptionTerm>
                <DescriptionDetail>
                  <StatusDot state={meta.state} pulse={meta.pulse ?? false}>
                    {meta.label}
                  </StatusDot>
                </DescriptionDetail>
                <DescriptionTerm>发布时间</DescriptionTerm>
                <DescriptionDetail>
                  {post?.publishedAt
                    ? absoluteDate(post.publishedAt)
                    : "尚未发布"}
                </DescriptionDetail>
                <DescriptionTerm>作者</DescriptionTerm>
                <DescriptionDetail>
                  {post?.author?.displayName || post?.author?.username || "你"}
                </DescriptionDetail>
                <DescriptionTerm>地址</DescriptionTerm>
                <DescriptionDetail>
                  <code className="token text-xs">
                    {kind === "post" ? "/posts/" : "/"}
                    {slug || post?.slug || "（由标题生成）"}
                  </code>
                </DescriptionDetail>
                <DescriptionTerm>修订</DescriptionTerm>
                <DescriptionDetail className="tabular">
                  {isNew ? "尚无" : `${revisions.data?.length ?? 0} 个版本`}
                </DescriptionDetail>
              </DescriptionList>
            )}
          </div>
        </aside>
      </div>

      {/* ---- 发布设置 ---- */}
      <Dialog open={settingsOpen} onOpenChange={setSettingsOpen}>
        <DialogContent size="lg">
          <DialogHeader>
            <DialogTitle>{label}设置</DialogTitle>
            <DialogDescription>
              这里的改动与正文一起保存；点「保存」写入整篇内容。
            </DialogDescription>
          </DialogHeader>
          <DialogBody className="divide-y divide-line">
            <SettingGroup
              title="常规"
              description="地址、归类与摘要"
              className="pb-5"
            >
              <SettingRow>
                <Field>
                  <FieldLabel htmlFor="content-slug">slug</FieldLabel>
                  <Input
                    id="content-slug"
                    value={slug}
                    onChange={(e) => {
                      setSlug(e.target.value);
                      markDirty();
                    }}
                    placeholder="留空则由标题生成"
                  />
                  <FieldDescription>
                    {kind === "post"
                      ? "文章地址为 /posts/<slug>"
                      : "页面地址为 /<slug>"}
                    。留空时按站点设置的策略从标题生成。
                  </FieldDescription>
                </Field>
              </SettingRow>

              {kind === "post" ? (
                <>
                  <SettingRow>
                    <Field>
                      <FieldLabel>分类</FieldLabel>
                      {taxonomy.isLoading ? (
                        <Skeleton className="h-20 w-full" />
                      ) : categoryOptions.length === 0 ? (
                        <p className="text-xs text-ink-muted">
                          还没有分类。
                          <Link
                            to="/categories"
                            className="text-seal hover:underline"
                          >
                            去创建
                          </Link>
                        </p>
                      ) : (
                        <div className="-mx-2 flex max-h-56 flex-col overflow-y-auto">
                          {categoryOptions.map((category) => (
                            <div
                              key={category.id}
                              style={{
                                paddingLeft: `${category.depth * 1}rem`,
                              }}
                            >
                              <CheckboxRow
                                id={`content-category-${category.id}`}
                                checked={categoryIds.includes(category.id)}
                                onCheckedChange={(checked) => {
                                  setCategoryIds((prev) =>
                                    checked
                                      ? [...prev, category.id]
                                      : prev.filter((v) => v !== category.id),
                                  );
                                  markDirty();
                                }}
                                label={category.name}
                                className="py-1"
                              />
                            </div>
                          ))}
                        </div>
                      )}
                    </Field>
                  </SettingRow>

                  <SettingRow>
                    <Field>
                      <FieldLabel>标签</FieldLabel>
                      {(taxonomy.data?.tags.length ?? 0) === 0 ? (
                        <p className="text-xs text-ink-muted">
                          还没有标签。
                          <Link
                            to="/tags"
                            className="text-seal hover:underline"
                          >
                            去创建
                          </Link>
                        </p>
                      ) : (
                        <div className="flex flex-wrap gap-1.5">
                          {tagOptions.map((tag) => {
                            const active = tagIds.includes(tag.id);
                            return (
                              <button
                                key={tag.id}
                                type="button"
                                onClick={() => {
                                  setTagIds((prev) =>
                                    active
                                      ? prev.filter((v) => v !== tag.id)
                                      : [...prev, tag.id],
                                  );
                                  markDirty();
                                }}
                                aria-pressed={active}
                                className={cn(
                                  "transition-ui rounded-full border px-2.5 py-0.5 text-xs",
                                  active
                                    ? "border-seal bg-seal-soft font-medium text-seal"
                                    : "border-line-strong bg-surface text-ink-muted hover:border-ink-subtle hover:text-ink",
                                )}
                              >
                                {tag.name}
                              </button>
                            );
                          })}
                        </div>
                      )}
                    </Field>
                  </SettingRow>
                </>
              ) : null}

              <SettingRow>
                <SwitchRow
                  id="content-auto-excerpt"
                  checked={excerptAuto}
                  onCheckedChange={(checked) => {
                    setExcerptAuto(checked);
                    markDirty();
                  }}
                  label="自动生成摘要"
                  description="保存后由服务端从正文取前 200 字。列表页与订阅源都用它"
                  className="py-0"
                />
                {excerptAuto ? null : (
                  <Textarea
                    rows={4}
                    value={excerpt}
                    onChange={(e) => {
                      setExcerpt(e.target.value);
                      markDirty();
                    }}
                    maxLength={1000}
                    placeholder="写一段摘要，显示在列表与搜索结果里"
                    aria-label="摘要"
                    className="mt-3"
                  />
                )}
              </SettingRow>

              <SettingRow>
                <Field>
                  <FieldLabel htmlFor="content-cover">封面图</FieldLabel>
                  <div className="flex items-center gap-2">
                    <Input
                      id="content-cover"
                      value={coverUrl}
                      onChange={(e) => {
                        setCoverUrl(e.target.value);
                        markDirty();
                      }}
                      placeholder="https://… 或 /uploads/…"
                    />
                    <Button
                      variant="secondary"
                      size="sm"
                      onClick={() => setCoverPicker(true)}
                      className="shrink-0"
                    >
                      <ImageIcon aria-hidden="true" />
                      从附件库选
                    </Button>
                  </div>
                  <FieldDescription>
                    用于列表卡片与社交分享。也可以直接填地址。
                  </FieldDescription>
                  {coverUrl ? (
                    <div className="mt-1 flex items-start gap-2">
                      <img
                        src={coverUrl}
                        alt="封面预览"
                        className="aspect-video w-full max-w-xs rounded-control border border-line object-cover"
                      />
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        aria-label="清除封面图"
                        title="清除封面图"
                        onClick={() => {
                          setCoverUrl("");
                          markDirty();
                        }}
                        className="hover:text-danger"
                      >
                        <X aria-hidden="true" />
                      </Button>
                    </div>
                  ) : null}
                </Field>
              </SettingRow>
            </SettingGroup>

            <SettingGroup
              title="高级"
              description="可见性、置顶与发布时间"
              className="pt-5"
            >
              {canUnsafeHTML ? null : (
                <SettingRow>
                  <Field>
                    <FieldLabel>正文净化</FieldLabel>
                    <FieldDescription>
                      你的角色没有「发布未净化的正文」权限：保存时脚本、事件属性与站内
                      iframe 会被移除，正常排版不受影响。这里的原稿不会被改动。
                      需要嵌入视频或保留自定义 HTML，请让管理员授予该权限。
                    </FieldDescription>
                  </Field>
                </SettingRow>
              )}

              <SettingRow>
                <Field>
                  <FieldLabel htmlFor="content-visibility">可见性</FieldLabel>
                  <Select
                    value={visibility}
                    onValueChange={(value) => {
                      setVisibility(value as "public" | "private");
                      markDirty();
                    }}
                  >
                    <SelectTrigger id="content-visibility">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="public">公开，所有人可见</SelectItem>
                      <SelectItem value="private">
                        私密，仅作者与编辑可见
                      </SelectItem>
                    </SelectContent>
                  </Select>
                </Field>
              </SettingRow>

              {kind === "post" ? (
                <SettingRow>
                  <SwitchRow
                    id="content-pinned"
                    checked={pinned}
                    onCheckedChange={(checked) => {
                      setPinned(checked);
                      markDirty();
                    }}
                    label="置顶"
                    description="置顶内容在列表首页优先显示"
                    className="py-0"
                  />
                </SettingRow>
              ) : null}

              <SettingRow>
                <Field>
                  <FieldLabel htmlFor="content-publish-at">发布时间</FieldLabel>
                  <Input
                    id="content-publish-at"
                    type="datetime-local"
                    value={publishAt}
                    onChange={(e) => {
                      setPublishAt(e.target.value);
                      markDirty();
                    }}
                    className="max-w-xs"
                  />
                  <FieldDescription>
                    留空则点「发布」时立即发布；填将来的时间则定时发布。
                  </FieldDescription>
                </Field>
              </SettingRow>

              {kind === "page" ? (
                <SettingRow>
                  <Field>
                    <FieldLabel htmlFor="content-template">页面模板</FieldLabel>
                    <Select
                      value={template || "__default__"}
                      onValueChange={(value) => {
                        setTemplate(value === "__default__" ? "" : value);
                        markDirty();
                      }}
                    >
                      <SelectTrigger id="content-template">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="__default__">
                          默认（page.html）
                        </SelectItem>
                        {(pageTemplates.data ?? []).map((name) => (
                          <SelectItem key={name} value={name}>
                            {name}.html
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                    <FieldDescription>
                      当前主题提供的专属模板。选中的模板不存在时会回退到
                      page.html，不会报错。
                    </FieldDescription>
                  </Field>
                </SettingRow>
              ) : null}
            </SettingGroup>
          </DialogBody>
          <DialogFooter>
            <Button variant="secondary" onClick={() => setSettingsOpen(false)}>
              关闭
            </Button>
            <Button
              variant="primary"
              onClick={() => save.mutate()}
              loading={save.isPending}
              disabled={!title.trim()}
            >
              保存
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* ---- 封面图：从附件库选 ---- */}
      <MediaPickerDialog
        open={coverPicker}
        onOpenChange={setCoverPicker}
        onSelect={(picks) => {
          const pick = picks[0];
          if (pick) {
            setCoverUrl(pick.url);
            markDirty();
          }
        }}
        kind="image"
        title="选择封面图"
        confirmLabel="用作封面"
      />

      {/* ---- 修订历史 ---- */}
      <Dialog open={revisionsOpen} onOpenChange={setRevisionsOpen}>
        <DialogContent size="md">
          <DialogHeader>
            <DialogTitle>修订历史</DialogTitle>
            <DialogDescription>
              每次保存都会留下一个版本。恢复会用该版本替换当前的标题与正文。
            </DialogDescription>
          </DialogHeader>
          <DialogBody className="p-0">
            {revisions.isLoading ? (
              <div className="flex flex-col gap-3 p-4" aria-busy="true">
                <Skeleton className="h-10 w-full" />
                <Skeleton className="h-10 w-full" />
                <Skeleton className="h-10 w-full" />
              </div>
            ) : revisions.error ? (
              <ErrorState
                message={revisions.error.message}
                onRetry={() => void revisions.refetch()}
              />
            ) : (revisions.data?.length ?? 0) === 0 ? (
              <EmptyState
                icon={History}
                title="还没有修订版本"
                description="保存一次之后，这里会出现第一个版本。"
                className="py-10"
              />
            ) : (
              <EntityList>
                {(revisions.data ?? []).map((revision) => (
                  <Entity key={revision.id}>
                    <EntityStart>
                      <EntityField
                        width="max-w-sm"
                        title={revision.title || "（无标题）"}
                        description={
                          <time
                            dateTime={revision.createdAt}
                            title={absoluteDate(revision.createdAt)}
                          >
                            {relativeTime(revision.createdAt)}
                          </time>
                        }
                      />
                    </EntityStart>
                    <EntityEnd>
                      <Badge tone="outline">
                        {revision.rawType === "markdown"
                          ? "Markdown"
                          : "富文本"}
                      </Badge>
                      <Button
                        variant="secondary"
                        size="xs"
                        onClick={() => setRestoring(revision)}
                      >
                        恢复
                      </Button>
                    </EntityEnd>
                  </Entity>
                ))}
              </EntityList>
            )}
          </DialogBody>
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={restoring !== null}
        onOpenChange={(open) => !open && setRestoring(null)}
        title={`恢复到「${restoring?.title || "（无标题）"}」这个版本？`}
        consequence={
          <p>
            当前的标题与正文会被该版本替换，尚未保存的修改会丢失。
            恢复后仍需点「保存」才会写入。
          </p>
        }
        confirmLabel="恢复"
        destructive={false}
        pending={restore.isPending}
        onConfirm={() => {
          if (restoring) {
            restore.mutate(restoring.id);
          }
        }}
      />

      {/*
        未保存修改的导航保护。放在页面里而不是 AppShell：只有真正持有表单
        状态的页面才知道自己脏不脏。beforeunload 管刷新与关标签页，
        这里管站内导航（侧栏、命令面板、浏览器后退）。
      */}
      <UnsavedChangesGuard
        when={() => dirtyRef.current}
        title="离开前要先保存吗？"
        consequence={
          <p>
            这篇内容有尚未保存的修改，离开后这些修改会丢失。
            已保存的版本与修订历史不受影响。
          </p>
        }
      />

      {/* 格式切换确认。如实说明会发生什么，而不是假装能无损来回换。 */}
      <Dialog open={switchOpen} onOpenChange={setSwitchOpen}>
        <DialogContent size="sm">
          <DialogHeader>
            <DialogTitle>
              切换到{pendingType === "markdown" ? " Markdown" : "富文本"}？
            </DialogTitle>
            <DialogDescription>
              正文**不会被清空**：原稿原样交给另一个编辑器重新解析。
              但解析结果不是无损的。
            </DialogDescription>
          </DialogHeader>
          <DialogBody>
            <ul className="flex list-disc flex-col gap-1.5 pl-5 text-sm text-ink-muted">
              {pendingType === "markdown" ? (
                <>
                  <li>HTML 标签会作为原始 HTML 保留在 Markdown 源里</li>
                  <li>富文本编辑器里的排版调整会退化成标签本身</li>
                  <li>行内样式与 data-* 属性按原样保留</li>
                </>
              ) : (
                <>
                  <li>Markdown 的标记（#、*、[ ]）会被当成普通文字显示</li>
                  <li>保存后这些标记就固定成文字，不再具有 Markdown 语义</li>
                  <li>再切回 Markdown 需要手动改回写法</li>
                </>
              )}
            </ul>
            <p className="mt-3 text-sm text-ink-muted">
              建议先保存一份，再切换 —— 每次保存都会留下一个修订版本。
            </p>
          </DialogBody>
          <DialogFooter>
            <Button variant="secondary" onClick={() => setSwitchOpen(false)}>
              取消
            </Button>
            <Button
              variant="primary"
              onClick={() => {
                if (!pendingType) {
                  return;
                }
                // 只换格式标记并重建编辑器，**不动 raw**。
                // 真正的解析由新编辑器完成：TipTap 会把 Markdown 文本按字面
                // 当段落（安全，不猜作者意图），Milkdown 把 HTML 当原始 HTML 保留。
                // 过去的实现在切到 Markdown 时把 raw 置空，等于一键删掉整篇正文。
                setRawType(pendingType);
                setEditorKey((k) => k + 1);
                setSwitchOpen(false);
                setPendingType(null);
                markDirty();
              }}
            >
              切换
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}

/**
 * 设置弹窗里的一组：左侧组名，右侧字段（Halo 的 PostSettingModal 同形）。
 * 组名在窄屏折到字段上方。
 */
function SettingGroup({
  title,
  description,
  className,
  children,
}: {
  title: string;
  description?: string;
  className?: string;
  children: ReactNode;
}) {
  return (
    <section
      className={cn(
        "grid gap-3 md:grid-cols-[7rem_minmax(0,1fr)] md:gap-6",
        className,
      )}
    >
      <div className="md:sticky md:top-0 md:self-start">
        <h3 className="text-sm font-medium text-ink">{title}</h3>
        {description ? (
          <p className="text-xs text-ink-muted">{description}</p>
        ) : null}
      </div>
      <div className="divide-y divide-line">{children}</div>
    </section>
  );
}

/** 组内的一行字段。 */
function SettingRow({ children }: { children: ReactNode }) {
  return <div className="py-4 first:pt-0 last:pb-0">{children}</div>;
}
