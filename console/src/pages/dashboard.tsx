import { api } from "@/api/client";
import { useAuth } from "@/components/auth/auth-provider";
import {
  Entity,
  EntityEnd,
  EntityField,
  EntityList,
  EntityMeta,
  EntityStart,
  EntityThumb,
} from "@/components/data/entity";
import { PageBody, PageHeader } from "@/components/layout/page-header";
import { Avatar } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import {
  DescriptionDetail,
  DescriptionList,
  DescriptionTerm,
} from "@/components/ui/card";
import { EmptyState, Skeleton } from "@/components/ui/states";
import { StatusDot } from "@/components/ui/status-dot";
import { absoluteDate, count, relativeTime } from "@/lib/format";
import { useDocumentTitle } from "@/lib/use-document-title";
import { cn } from "@/lib/utils";
import { useQuery } from "@tanstack/react-query";
import {
  ArrowUpRight,
  BookOpen,
  CheckCircle2,
  ExternalLink,
  FileText,
  FolderTree,
  Image,
  LayoutDashboard,
  type LucideIcon,
  MessageSquare,
  Palette,
  PenLine,
  RefreshCw,
  Upload,
  UserPlus,
  UserRound,
  Users,
} from "lucide-react";
import type { ReactNode } from "react";
import { Link } from "react-router";
import { toast } from "sonner";

/**
 * 仪表盘。
 *
 * 四张统计部件在上，快捷访问与新评论在中，最近文章与站点概况在下。
 * 部件是比卡片大一档圆角（8px）的容器，标题栏 40px。
 *
 * 统计部件不做趋势与图表：后台没有时间序列数据，画一条假曲线只是装饰。
 * 数字都是可点的 —— 它们是通往对应列表的入口，不是一块看板。
 */

/** 取某个列表接口的总数。size=1 足够 —— 只关心 total。 */
function useTotal(
  key: string,
  fetcher: () => Promise<{
    data?: { total?: number } | undefined;
    response: Response;
  }>,
  enabled = true,
) {
  return useQuery({
    queryKey: ["overview", key],
    enabled,
    queryFn: async () => {
      const { data, response } = await fetcher();
      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
      }
      return data?.total ?? 0;
    },
  });
}

function Widget({
  title,
  action,
  className,
  children,
}: {
  title: string;
  action?: ReactNode;
  className?: string;
  children: ReactNode;
}) {
  return (
    <section
      className={cn(
        "flex min-w-0 flex-col rounded-widget border border-line bg-surface shadow-card",
        className,
      )}
    >
      <header className="flex h-10 shrink-0 items-center justify-between gap-2 border-line border-b px-4">
        <h2 className="text-base font-medium text-ink">{title}</h2>
        {action}
      </header>
      <div className="min-h-0 flex-1">{children}</div>
    </section>
  );
}

/** 统计部件：灰色圆形图标 + 名称 + 数字。整块可点。 */
function StatWidget({
  icon: Icon,
  label,
  value,
  loading,
  to,
}: {
  icon: LucideIcon;
  label: string;
  value: number | undefined;
  loading: boolean;
  to: string;
}) {
  return (
    <Link
      to={to}
      className="transition-ui flex items-center gap-4 rounded-widget border border-line bg-surface px-4 py-3 shadow-card hover:border-line-strong"
    >
      <span className="flex size-10 shrink-0 items-center justify-center rounded-full bg-surface-active text-ink-muted">
        <Icon aria-hidden="true" className="size-5" />
      </span>
      <span className="flex min-w-0 flex-col">
        <span className="text-sm text-ink-muted">{label}</span>
        {loading ? (
          <Skeleton className="mt-1 h-6 w-12" />
        ) : (
          <span className="tabular text-2xl font-medium text-ink">
            {count(value)}
          </span>
        )}
      </span>
    </Link>
  );
}

/** 快捷入口：浅灰底的方块，图标在左上、名字在下、右上一枚外指箭头。 */
function QuickItem({
  icon: Icon,
  label,
  to,
  href,
  onClick,
}: {
  icon: LucideIcon;
  label: string;
  to?: string;
  href?: string;
  onClick?: () => void;
}) {
  const className =
    "transition-ui group relative flex flex-col gap-6 rounded-widget bg-surface-raised p-4 text-left hover:bg-surface-active";
  const body = (
    <>
      <span className="flex size-10 items-center justify-center rounded-widget bg-seal-soft text-seal ring-4 ring-surface">
        <Icon aria-hidden="true" className="size-5" />
      </span>
      <span className="text-sm font-semibold text-ink">{label}</span>
      <ArrowUpRight
        aria-hidden="true"
        className="transition-ui absolute top-3 right-3 size-4 text-ink-subtle group-hover:text-ink-muted"
      />
    </>
  );
  if (to) {
    return (
      <Link to={to} className={className}>
        {body}
      </Link>
    );
  }
  if (href) {
    return (
      <a
        href={href}
        target="_blank"
        rel="noopener noreferrer"
        className={className}
      >
        {body}
      </a>
    );
  }
  return (
    <button type="button" onClick={onClick} className={className}>
      {body}
    </button>
  );
}

export function DashboardPage() {
  useDocumentTitle("仪表盘");
  const { can, user, permissions } = useAuth();

  const canComments = can("comments:manage");
  const canUsers = can("users:manage");
  const canSettings = permissions.has("settings:manage");

  const posts = useTotal("posts", () =>
    api.GET("/api/v1/console/posts", { params: { query: { size: 1 } } }),
  );
  const comments = useTotal(
    "comments-all",
    () =>
      // 不传 status 即「全部」：接口对缺省的定义就是不按状态过滤
      api.GET("/api/v1/console/comments", {
        params: { query: { size: 1 } },
      }),
    canComments,
  );
  const media = useTotal("media", () =>
    api.GET("/api/v1/console/media", { params: { query: { size: 1 } } }),
  );
  const users = useTotal(
    "users",
    () => api.GET("/api/v1/console/users", { params: { query: { size: 1 } } }),
    canUsers,
  );
  const pages = useTotal("pages", () =>
    api.GET("/api/v1/console/pages", { params: { query: { size: 1 } } }),
  );
  const categories = useTotal("categories", () =>
    api.GET("/api/v1/console/categories", { params: { query: { size: 1 } } }),
  );
  const tags = useTotal("tags", () =>
    api.GET("/api/v1/console/tags", { params: { query: { size: 1 } } }),
  );
  const drafts = useTotal("posts-draft", () =>
    api.GET("/api/v1/console/posts", {
      params: { query: { status: "draft", size: 1 } },
    }),
  );
  const scheduled = useTotal("posts-scheduled", () =>
    api.GET("/api/v1/console/posts", {
      params: { query: { status: "scheduled", size: 1 } },
    }),
  );

  const recent = useQuery({
    queryKey: ["overview", "recent"],
    queryFn: async () => {
      const { data, response } = await api.GET("/api/v1/console/posts", {
        params: { query: { status: "published", size: 6 } },
      });
      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
      }
      return data?.items ?? [];
    },
  });

  const pending = useQuery({
    queryKey: ["overview", "comments-pending"],
    enabled: canComments,
    queryFn: async () => {
      const { data, response } = await api.GET("/api/v1/console/comments", {
        params: { query: { status: "pending", size: 5 } },
      });
      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
      }
      return data;
    },
  });

  const health = useQuery({
    queryKey: ["health"],
    queryFn: async (): Promise<{ version?: string }> => {
      const response = await fetch("/healthz");
      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
      }
      return (await response.json()) as { version?: string };
    },
  });

  const greeting = `${user?.displayName || user?.username || ""}，欢迎回来`;

  return (
    <>
      <PageHeader
        icon={LayoutDashboard}
        title="仪表盘"
        description={greeting}
      />

      <PageBody>
        <div className="grid grid-cols-2 gap-3 md:grid-cols-4 md:gap-4">
          <StatWidget
            icon={BookOpen}
            label="文章"
            value={posts.data}
            loading={posts.isLoading}
            to="/posts"
          />
          {canComments ? (
            <StatWidget
              icon={MessageSquare}
              label="评论"
              value={comments.data}
              loading={comments.isLoading}
              to="/comments?status=all"
            />
          ) : (
            <StatWidget
              icon={FileText}
              label="页面"
              value={pages.data}
              loading={pages.isLoading}
              to="/pages"
            />
          )}
          <StatWidget
            icon={Image}
            label="附件"
            value={media.data}
            loading={media.isLoading}
            to="/media"
          />
          {canUsers ? (
            <StatWidget
              icon={Users}
              label="用户"
              value={users.data}
              loading={users.isLoading}
              to="/users"
            />
          ) : (
            <StatWidget
              icon={FolderTree}
              label="分类"
              value={categories.data}
              loading={categories.isLoading}
              to="/categories"
            />
          )}
        </div>

        <div className="grid gap-4 lg:grid-cols-2">
          <Widget title="快捷访问">
            <div className="grid grid-cols-2 gap-2 p-3 sm:grid-cols-3">
              <QuickItem icon={UserRound} label="个人中心" to="/profile" />
              <QuickItem icon={ExternalLink} label="查看站点" href="/" />
              {can("posts:write") ? (
                <QuickItem icon={PenLine} label="写文章" to="/posts/new" />
              ) : null}
              {can("pages:write") ? (
                <QuickItem icon={FileText} label="新建页面" to="/pages/new" />
              ) : null}
              {can("media:write") ? (
                <QuickItem
                  icon={Upload}
                  label="上传附件"
                  to="/media?upload=1"
                />
              ) : null}
              {can("themes:manage") ? (
                <QuickItem icon={Palette} label="主题管理" to="/themes" />
              ) : null}
              {canUsers ? (
                <QuickItem
                  icon={UserPlus}
                  label="新建用户"
                  to="/users?create=1"
                />
              ) : null}
              {canSettings ? (
                <QuickItem
                  icon={RefreshCw}
                  label="重建搜索索引"
                  onClick={async () => {
                    const { response } = await api.POST(
                      "/api/v1/console/search/reindex",
                    );
                    if (response.ok) {
                      toast.success("已开始重建全文索引");
                    } else {
                      toast.error(`重建失败（HTTP ${response.status}）`);
                    }
                  }}
                />
              ) : null}
            </div>
          </Widget>

          {canComments ? (
            <Widget
              title="新评论"
              action={
                <Button variant="ghost" size="xs" asChild>
                  <Link to="/comments">全部待审</Link>
                </Button>
              }
            >
              {pending.isLoading ? (
                <div className="flex flex-col gap-3 p-4">
                  <Skeleton className="h-10 w-full" />
                  <Skeleton className="h-10 w-full" />
                  <Skeleton className="h-10 w-full" />
                </div>
              ) : (pending.data?.items?.length ?? 0) === 0 ? (
                <EmptyState
                  icon={CheckCircle2}
                  title="没有待审评论"
                  description="访客新提交的评论会出现在这里。"
                  className="py-10"
                />
              ) : (
                <EntityList>
                  {(pending.data?.items ?? []).map((comment) => (
                    <Entity key={comment.id}>
                      <EntityStart>
                        <Avatar name={comment.authorName || "匿名"} size="sm" />
                        <EntityField
                          width="max-w-md"
                          title={comment.authorName || "匿名"}
                          description={
                            <span className="line-clamp-1 text-ink">
                              {comment.content}
                            </span>
                          }
                        />
                      </EntityStart>
                      <EntityEnd>
                        <EntityMeta hideOnMobile>
                          {comment.post ? (
                            <Link
                              to={`/posts/${comment.post.id}`}
                              className="transition-ui max-w-40 truncate hover:text-ink"
                            >
                              {comment.post.title}
                            </Link>
                          ) : null}
                        </EntityMeta>
                        <EntityMeta>
                          <time
                            dateTime={comment.createdAt}
                            title={absoluteDate(comment.createdAt)}
                          >
                            {relativeTime(comment.createdAt)}
                          </time>
                        </EntityMeta>
                      </EntityEnd>
                    </Entity>
                  ))}
                </EntityList>
              )}
            </Widget>
          ) : (
            <Widget title="内容进度">
              <DescriptionList className="p-4">
                <DescriptionTerm>草稿</DescriptionTerm>
                <DescriptionDetail className="tabular">
                  <Link to="/posts?status=draft" className="hover:text-seal">
                    {count(drafts.data)}
                  </Link>
                </DescriptionDetail>
                <DescriptionTerm>定时待发</DescriptionTerm>
                <DescriptionDetail className="tabular">
                  <Link
                    to="/posts?status=scheduled"
                    className="hover:text-seal"
                  >
                    {count(scheduled.data)}
                  </Link>
                </DescriptionDetail>
              </DescriptionList>
            </Widget>
          )}
        </div>

        <div className="grid gap-4 lg:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]">
          <Widget
            title="最近文章"
            action={
              <Button variant="ghost" size="xs" asChild>
                <Link to="/posts">全部文章</Link>
              </Button>
            }
          >
            {recent.isLoading ? (
              <div className="flex flex-col gap-3 p-4">
                <Skeleton className="h-10 w-full" />
                <Skeleton className="h-10 w-full" />
                <Skeleton className="h-10 w-full" />
              </div>
            ) : (recent.data?.length ?? 0) === 0 ? (
              <EmptyState
                icon={BookOpen}
                title="还没有已发布的文章"
                description="写完第一篇，这里就会显示它。"
                className="py-10"
                action={
                  can("posts:write") ? (
                    <Button variant="primary" size="sm" asChild>
                      <Link to="/posts/new">写文章</Link>
                    </Button>
                  ) : null
                }
              />
            ) : (
              <EntityList>
                {(recent.data ?? []).map((post) => (
                  <Entity key={post.id}>
                    <EntityStart>
                      <EntityThumb
                        src={post.coverUrl}
                        icon={BookOpen}
                        className="hidden sm:flex"
                      />
                      <EntityField
                        width="max-w-md"
                        title={post.title || "（无标题）"}
                        to={`/posts/${post.id}`}
                        description={
                          <span>
                            {post.author?.displayName || post.author?.username}
                          </span>
                        }
                      />
                    </EntityStart>
                    <EntityEnd>
                      <StatusDot state="ok">已发布</StatusDot>
                      <EntityMeta>
                        <time
                          dateTime={post.publishedAt ?? undefined}
                          title={absoluteDate(post.publishedAt)}
                        >
                          {relativeTime(post.publishedAt)}
                        </time>
                      </EntityMeta>
                    </EntityEnd>
                  </Entity>
                ))}
              </EntityList>
            )}
          </Widget>

          <Widget title="站点概况">
            <DescriptionList className="p-4">
              <DescriptionTerm>草稿</DescriptionTerm>
              <DescriptionDetail className="tabular">
                <Link to="/posts?status=draft" className="hover:text-seal">
                  {count(drafts.data)}
                </Link>
              </DescriptionDetail>
              <DescriptionTerm>定时待发</DescriptionTerm>
              <DescriptionDetail className="tabular">
                <Link to="/posts?status=scheduled" className="hover:text-seal">
                  {count(scheduled.data)}
                </Link>
              </DescriptionDetail>
              <DescriptionTerm>独立页面</DescriptionTerm>
              <DescriptionDetail className="tabular">
                <Link to="/pages" className="hover:text-seal">
                  {count(pages.data)}
                </Link>
              </DescriptionDetail>
              <DescriptionTerm>分类</DescriptionTerm>
              <DescriptionDetail className="tabular">
                <Link to="/categories" className="hover:text-seal">
                  {count(categories.data)}
                </Link>
              </DescriptionDetail>
              <DescriptionTerm>标签</DescriptionTerm>
              <DescriptionDetail className="tabular">
                <Link to="/tags" className="hover:text-seal">
                  {count(tags.data)}
                </Link>
              </DescriptionDetail>
              <DescriptionTerm>Lumo 版本</DescriptionTerm>
              <DescriptionDetail className="token">
                {health.data?.version || "—"}
              </DescriptionDetail>
            </DescriptionList>
          </Widget>
        </div>
      </PageBody>
    </>
  );
}
