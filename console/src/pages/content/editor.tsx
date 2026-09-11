import { api, problemMessage } from "@/api/client";
import type { components } from "@/api/schema";
import { useAuth } from "@/components/auth/auth-provider";
import { HtmlEditor } from "@/components/editor/html-editor";
import { MarkdownEditor } from "@/components/editor/markdown-editor";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
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
import { PageHeader } from "@/components/ui/panel";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { ErrorState, Skeleton } from "@/components/ui/states";
import { Switch } from "@/components/ui/toggle";
import { fromLocalInput, toLocalInput } from "@/lib/format";
import { useDocumentTitle } from "@/lib/use-document-title";
import { cn } from "@/lib/utils";
import { useMutation, useQuery } from "@tanstack/react-query";
import {
  ArrowLeft,
  CalendarClock,
  Check,
  Eye,
  FileCode2,
  FileText,
  Loader2,
  Pin,
  Save,
  Undo2,
} from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { Link, useNavigate, useParams } from "react-router";

/**
 * 内容编辑器（文章与独立页面共用）。
 *
 * 一个页面同时承担新建、编辑、发布、定时、撤回、进回收站 —— 因为它们
 * 共享同一份表单状态，拆成多个路由会让「改完标题再点发布」变成两次导航。
 *
 * **两个编辑器产出同一对字段**：`raw` + `rawType`（agent.md §3.4）。
 * 默认用块编辑器（TipTap，产出规范 HTML）；想写 Markdown 就切过去。
 * 切换**不是无损的**：HTML 转 Markdown 会丢结构，故切换时弹确认并把这句话
 * 如实说出来，而不是假装能来回换。空内容时切换不弹窗 —— 那时确实无损。
 *
 * 版面：左列编辑器 + 右列侧栏（状态、分类标签、封面、摘要、SEO）。
 * 侧栏在窄屏折到正文下方。这个结构是内容型后台的通行做法，
 * 因为「正文」与「元信息」是两种不同的注意力模式。
 */

type ContentType = "post" | "page";
type Post = components["schemas"]["Post"];
type RawType = "html" | "markdown";
type Status = "draft" | "published" | "scheduled" | "trashed";

/** 空文档的初始内容。两个编辑器各自需要一点种子才不会渲染出空白。 */
const EMPTY_HTML = "<p></p>";
const EMPTY_MARKDOWN = "";

export function ContentEditor({ kind }: { kind: ContentType }) {
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
  const [switchOpen, setSwitchOpen] = useState(false);
  const [pendingType, setPendingType] = useState<RawType | null>(null);
  /** 编辑器实例的 key。切换格式后自增，让编辑器整体重建以载入新内容。 */
  const [editorKey, setEditorKey] = useState(0);

  useDocumentTitle(isNew ? `新建${label}` : title || label);

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

  // 数据到达后填表。只在首次填（loaded 为 false 时），
  // 否则每次重新拉取都会把用户正在编辑的内容冲掉。
  useEffect(() => {
    const post = query.data;
    if (!post || loaded) {
      return;
    }
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
    setLoaded(true);
  }, [query.data, loaded]);

  // ---- 离开前提醒 ----
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

  const markDirty = useCallback(() => setDirty(true), []);

  // ---- 分类与标签候选 ----
  const taxonomy = useQuery({
    queryKey: ["taxonomy-options"],
    enabled: kind === "post",
    queryFn: async () => {
      const [categories, tags] = await Promise.all([
        api.GET("/api/v1/console/categories/tree"),
        api.GET("/api/v1/console/tags", { params: { query: { size: 100 } } }),
      ]);
      return {
        categories: flattenCategories(categories.data?.items ?? []),
        tags: tags.data?.items ?? [],
      };
    },
  });

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
      setDirty(false);
      // 新建后把地址换成 /posts/<id>，否则再点保存会又建一篇
      if (id === null && data?.id) {
        navigate(`${listPath}/${data.id}`, { replace: true });
      }
    },
  });

  const publish = useMutation({
    mutationFn: async (action: "publish" | "unpublish") => {
      if (id === null) {
        return;
      }
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
                params: { path: { id } },
                body: publishBody,
              })
            : await api.POST("/api/v1/console/pages/{id}/publish", {
                params: { path: { id } },
                body: publishBody,
              })
          : kind === "post"
            ? await api.POST("/api/v1/console/posts/{id}/unpublish", {
                params: { path: { id } },
              })
            : await api.POST("/api/v1/console/pages/{id}/unpublish", {
                params: { path: { id } },
              });
      if (!response.ok) {
        throw new Error(problemMessage(error));
      }
    },
    onSuccess: () => void query.refetch(),
  });

  if (query.isLoading && !isNew) {
    return (
      <>
        <PageHeader title={`载入${label}`} />
        <div className="flex flex-col gap-3">
          <Skeleton className="h-10 w-full" />
          <Skeleton className="h-[32rem] w-full" />
        </div>
      </>
    );
  }

  if (query.error) {
    return (
      <>
        <PageHeader title={label} />
        <ErrorState
          message={query.error.message}
          onRetry={() => void query.refetch()}
        />
      </>
    );
  }

  const post: Post | undefined = query.data;
  const status = (post?.status ?? "draft") as Status;
  const canPublish = can(kind === "post" ? "posts:publish" : "pages:publish");

  return (
    <>
      <PageHeader
        title={isNew ? `新建${label}` : title || "（无标题）"}
        description={
          status === "trashed"
            ? "这篇内容在回收站里。还原后它会变成草稿。"
            : undefined
        }
        actions={
          <div className="flex items-center gap-2">
            <Button variant="ghost" size="sm" asChild>
              <Link to={listPath}>
                <ArrowLeft aria-hidden="true" />
                返回列表
              </Link>
            </Button>

            {!isNew && post?.slug ? (
              <Button variant="ghost" size="sm" asChild>
                <a
                  href={
                    kind === "post" ? `/posts/${post.slug}` : `/${post.slug}`
                  }
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  <Eye aria-hidden="true" />
                  预览
                </a>
              </Button>
            ) : null}

            <Button
              variant="secondary"
              onClick={() => save.mutate()}
              disabled={save.isPending || !title.trim()}
            >
              {save.isPending ? (
                <Loader2 aria-hidden="true" className="animate-spin" />
              ) : (
                <Save aria-hidden="true" />
              )}
              {dirty ? "保存草稿" : "已保存"}
            </Button>

            {canPublish ? (
              status === "published" ? (
                <Button
                  variant="secondary"
                  onClick={() => publish.mutate("unpublish")}
                  disabled={publish.isPending || dirty}
                  title={dirty ? "先保存再撤回" : "撤回为草稿"}
                >
                  <Undo2 aria-hidden="true" />
                  撤回
                </Button>
              ) : (
                <Button
                  variant="primary"
                  onClick={async () => {
                    // 先保存再发布：直接发布会让「改了标题但没保存就发布」
                    // 得到一篇标题是旧的线上文章
                    if (dirty || isNew) {
                      await save.mutateAsync().catch(() => {});
                    }
                    publish.mutate("publish");
                  }}
                  disabled={publish.isPending || !title.trim()}
                >
                  {publishAt && new Date(publishAt) > new Date() ? (
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
          </div>
        }
      />

      {status !== "draft" ? (
        <div className="mb-4 flex items-center gap-2">
          <StatusBadge status={status} />
          {post?.publishedAt ? (
            <span className="text-xs text-ink-muted">
              {status === "scheduled" ? "预定于 " : "发布于 "}
              {new Date(post.publishedAt).toLocaleString("zh-CN")}
            </span>
          ) : null}
        </div>
      ) : null}

      <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_20rem]">
        {/* ---- 主列：标题 + 编辑器 ---- */}
        <div className="flex flex-col gap-4">
          <div className="rounded-panel border border-line bg-surface p-4">
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
              placeholder={`${label}标题`}
              className="w-full border-0 bg-transparent text-xl font-semibold text-ink outline-none placeholder:text-ink-subtle"
              // 标题是这条内容最重要的字段，且长度有 256 上限
              maxLength={256}
            />
          </div>

          {/*
            编辑区是「纸」（agent.md §11.3）：外壳走冷调中性色，
            只有正在写的这张纸用主题的纸色，圆角 2px 与「墨 Ink」逐字一致。
            key 让切换格式与载入新内容时编辑器整体重建 ——
            TipTap 与 Milkdown 都不会可靠地响应「外部换掉全部内容」。
          */}
          <div className="overflow-hidden rounded-paper border border-line bg-paper">
            <div className="flex flex-wrap items-center justify-between gap-2 border-line border-b bg-chrome px-3 py-1.5">
              <FormatSwitch
                value={rawType}
                onChange={(next) => {
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
                }}
              />
              <span className="text-xs text-ink-muted">
                {rawType === "html" ? "富文本，存规范 HTML" : "Markdown 源码"}
              </span>
            </div>

            <div key={editorKey} className="text-paper-ink">
              {rawType === "html" ? (
                <HtmlEditor
                  initialContent={raw || EMPTY_HTML}
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
            </div>
          </div>

          <div className="text-xs text-ink-muted">
            正文不做服务端净化（agent.md §3.4）：已认证用户的输入被信任， 这样
            iframe 嵌入与自定义 HTML 块才能用。
          </div>
        </div>

        {/* ---- 侧栏：元信息 ---- */}
        <aside className="flex flex-col gap-4">
          <SidePanel title="发布">
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
              />
              <FieldDescription>
                留空则点「发布」时立即发布；填将来的时间则定时发布。
              </FieldDescription>
            </Field>

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
                  <SelectItem value="public">公开 —— 所有人可见</SelectItem>
                  <SelectItem value="private">
                    私密 —— 仅作者与编辑可见
                  </SelectItem>
                </SelectContent>
              </Select>
            </Field>

            {kind === "post" ? (
              <div className="flex items-start justify-between gap-3">
                <div className="flex flex-col gap-0.5">
                  <label
                    htmlFor="content-pinned"
                    className="flex items-center gap-1.5 text-sm font-medium text-ink select-none"
                  >
                    <Pin aria-hidden="true" className="size-3.5" />
                    置顶
                  </label>
                  <span className="text-xs text-ink-muted">
                    置顶内容在列表首页优先显示
                  </span>
                </div>
                <Switch
                  id="content-pinned"
                  checked={pinned}
                  onCheckedChange={(checked) => {
                    setPinned(checked);
                    markDirty();
                  }}
                />
              </div>
            ) : null}
          </SidePanel>

          <SidePanel title="地址">
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
                。 留空时按站点设置的策略从标题生成。
              </FieldDescription>
            </Field>
          </SidePanel>

          {kind === "page" ? (
            <SidePanel title="模板">
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
                  当前主题提供的专属模板。选中的模板不存在时会回退到 page.html，
                  不会报错。
                </FieldDescription>
              </Field>
            </SidePanel>
          ) : null}

          <SidePanel title="摘要">
            <div className="flex items-center justify-between gap-3">
              <label
                htmlFor="content-auto-excerpt"
                className="text-sm text-ink select-none"
              >
                自动从正文提取
              </label>
              <Switch
                id="content-auto-excerpt"
                checked={excerptAuto}
                onCheckedChange={(checked) => {
                  setExcerptAuto(checked);
                  markDirty();
                }}
              />
            </div>
            {excerptAuto ? (
              <p className="text-xs text-ink-muted">
                保存后由服务端从正文取前 200 字。列表页与订阅源都用它。
              </p>
            ) : (
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
              />
            )}
          </SidePanel>

          {kind === "post" ? (
            <>
              <SidePanel title="分类">
                {taxonomy.isLoading ? (
                  <Skeleton className="h-20 w-full" />
                ) : (taxonomy.data?.categories.length ?? 0) === 0 ? (
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
                  <div className="flex max-h-56 flex-col gap-1 overflow-y-auto">
                    {(taxonomy.data?.categories ?? []).map((category) => (
                      <label
                        key={category.id}
                        className="flex cursor-pointer items-center gap-2 rounded-control px-1.5 py-1 text-sm hover:bg-surface-hover"
                      >
                        <input
                          type="checkbox"
                          checked={categoryIds.includes(category.id)}
                          onChange={(e) => {
                            setCategoryIds((prev) =>
                              e.target.checked
                                ? [...prev, category.id]
                                : prev.filter((v) => v !== category.id),
                            );
                            markDirty();
                          }}
                          className="size-4 cursor-pointer rounded-[3px] border-line-strong accent-seal"
                        />
                        <span
                          className="truncate text-ink"
                          style={{ paddingLeft: `${category.depth * 0.75}rem` }}
                        >
                          {category.name}
                        </span>
                      </label>
                    ))}
                  </div>
                )}
              </SidePanel>

              <SidePanel title="标签">
                {(taxonomy.data?.tags.length ?? 0) === 0 ? (
                  <p className="text-xs text-ink-muted">
                    还没有标签。
                    <Link to="/tags" className="text-seal hover:underline">
                      去创建
                    </Link>
                  </p>
                ) : (
                  <div className="flex flex-wrap gap-1">
                    {(taxonomy.data?.tags ?? []).map((tag) => {
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
                            "transition-ui rounded-control border px-2 py-1 text-xs",
                            active
                              ? "border-seal bg-seal-soft font-medium text-seal"
                              : "border-line bg-surface text-ink-muted hover:border-line-strong hover:text-ink",
                          )}
                        >
                          {tag.name}
                        </button>
                      );
                    })}
                  </div>
                )}
              </SidePanel>
            </>
          ) : null}

          <SidePanel title="封面">
            <Field>
              <FieldLabel htmlFor="content-cover">封面图地址</FieldLabel>
              <Input
                id="content-cover"
                value={coverUrl}
                onChange={(e) => {
                  setCoverUrl(e.target.value);
                  markDirty();
                }}
                placeholder="https://… 或 /uploads/…"
              />
              <FieldDescription>
                用于列表卡片与社交分享。可以从附件库复制地址。
              </FieldDescription>
            </Field>
            {coverUrl ? (
              <img
                src={coverUrl}
                alt="封面预览"
                className="mt-2 max-h-32 w-full rounded-control border border-line object-cover"
              />
            ) : null}
          </SidePanel>
        </aside>
      </div>

      {/* 格式切换确认。如实说明「不可逆」而不是假装能来回换。 */}
      <Dialog open={switchOpen} onOpenChange={setSwitchOpen}>
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle>
              切换到{pendingType === "markdown" ? " Markdown" : "富文本"}？
            </DialogTitle>
            <DialogDescription>
              当前正文会按现有内容重新解析。这一步**不是无损的**：
            </DialogDescription>
          </DialogHeader>
          <DialogBody>
            <ul className="flex flex-col gap-1.5 text-sm text-ink-muted">
              {pendingType === "markdown" ? (
                <>
                  <li>· 自定义 HTML 块与其中的 data-* 属性会丢失</li>
                  <li>· 复杂的表格（合并单元格）会被拆成普通表格</li>
                  <li>· 行内样式会被丢弃</li>
                </>
              ) : (
                <>
                  <li>· Markdown 的原始写法会变成等价的 HTML，源码不再保留</li>
                  <li>· 之后再切回 Markdown，排版细节可能与原来不同</li>
                </>
              )}
            </ul>
            <p className="mt-3 text-sm text-ink-muted">
              建议：先保存一份，再切换。
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
                // 这里只做格式标记的切换与编辑器重建。真正的转换由编辑器完成：
                // TipTap 会把 Markdown 文本按字面当段落（安全，不猜作者意图），
                // Milkdown 会把 HTML 当 Markdown 解析。两者都不假装理解对方的格式。
                setRawType(pendingType);
                setRaw(pendingType === "markdown" ? "" : raw);
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

function StatusBadge({ status }: { status: Status }) {
  const map: Record<
    Status,
    { label: string; tone: "neutral" | "ok" | "warn" | "danger" }
  > = {
    draft: { label: "草稿", tone: "neutral" },
    published: { label: "已发布", tone: "ok" },
    scheduled: { label: "定时发布", tone: "warn" },
    trashed: { label: "回收站", tone: "danger" },
  };
  const meta = map[status];
  return <Badge tone={meta.tone}>{meta.label}</Badge>;
}

/** 侧栏里的一块。标题小、内容直接铺开 —— 侧栏不该再套一层卡片。 */
function SidePanel({
  title,
  children,
}: { title: string; children: React.ReactNode }) {
  return (
    <section className="rounded-panel border border-line bg-surface">
      <h2 className="border-line border-b px-4 py-2 text-sm font-medium text-ink">
        {title}
      </h2>
      <div className="flex flex-col gap-3 p-4">{children}</div>
    </section>
  );
}

/** 格式切换：两个按钮组成的单选组。 */
function FormatSwitch({
  value,
  onChange,
}: {
  value: RawType;
  onChange: (next: RawType) => void;
}) {
  const options: { value: RawType; label: string; icon: typeof FileText }[] = [
    { value: "html", label: "富文本", icon: FileText },
    { value: "markdown", label: "Markdown", icon: FileCode2 },
  ];
  return (
    <fieldset className="m-0 flex items-center gap-0.5 border-0 p-0">
      <legend className="sr-only">正文格式</legend>
      {options.map((option) => (
        <button
          key={option.value}
          type="button"
          onClick={() => onChange(option.value)}
          aria-pressed={value === option.value}
          className={cn(
            "transition-ui flex items-center gap-1.5 rounded-control px-2 py-1 text-xs",
            value === option.value
              ? "bg-seal-soft font-medium text-seal"
              : "text-ink-muted hover:bg-surface-active hover:text-ink",
          )}
        >
          <option.icon aria-hidden="true" className="size-3.5" />
          {option.label}
        </button>
      ))}
    </fieldset>
  );
}

/** 分类树摊平成带缩进的选项。 */
function flattenCategories(
  nodes: components["schemas"]["CategoryNode"][],
  depth = 0,
): { id: number; name: string; depth: number }[] {
  const out: { id: number; name: string; depth: number }[] = [];
  for (const node of nodes) {
    out.push({ id: node.id, name: node.name, depth });
    if (node.children?.length) {
      out.push(...flattenCategories(node.children, depth + 1));
    }
  }
  return out;
}
