import { api } from "@/api/client";
import { useAuth } from "@/components/auth/auth-provider";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  PageHeader,
  Panel,
  PanelBody,
  PanelHeader,
  PanelTitle,
} from "@/components/ui/panel";
import { Skeleton } from "@/components/ui/states";
import { absoluteDate, count, relativeTime } from "@/lib/format";
import { useDocumentTitle } from "@/lib/use-document-title";
import { useQuery } from "@tanstack/react-query";
import {
  ArrowRight,
  CalendarClock,
  CheckCircle2,
  FileText,
  FolderTree,
  Image,
  MessageSquare,
  Tags,
  Users,
} from "lucide-react";
import { Link } from "react-router";

/**
 * 概览页。
 *
 * 刻意**不做**通用的「四张大数字卡片」。
 * 后台首页的价值是回答一个问题：**现在有什么在等我处理**。
 * 「已发布文章 32」是个永远不会催你动手的数字，而「待审评论 12」是。
 *
 * 故版面分两层：
 *   需要处理 —— 只放有明确下一步的条目，每条都带动作。没有待办时整块消失，不留空卡
 *   站点概况 —— 只读的账目，一眼扫过，不占视觉重量
 */

type Todo = {
  key: string;
  label: string;
  hint: string;
  value: number;
  to: string;
  action: string;
  icon: typeof FileText;
  tone: "warn" | "seal";
};

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

function TodoCard({ todo }: { todo: Todo }) {
  return (
    <Link
      to={todo.to}
      className="transition-ui group flex flex-col gap-2 rounded-panel border border-line bg-surface p-4 hover:border-line-strong hover:bg-surface-hover"
    >
      <div className="flex items-center gap-2">
        <todo.icon
          aria-hidden="true"
          className={
            todo.tone === "warn" ? "size-4 text-warn" : "size-4 text-seal"
          }
        />
        <span className="text-sm font-medium text-ink">{todo.label}</span>
      </div>
      {/* 数字是这一块的主角，字号与 tabular-nums 都服务于「快速比较」 */}
      <p className="tabular text-2xl font-semibold text-ink">
        {count(todo.value)}
      </p>
      <p className="text-xs text-ink-muted">{todo.hint}</p>
      <span className="transition-ui mt-1 inline-flex items-center gap-1 text-xs font-medium text-seal">
        {todo.action}
        <ArrowRight
          aria-hidden="true"
          className="size-3 transition-transform group-hover:translate-x-0.5"
        />
      </span>
    </Link>
  );
}

/** 概况里的一格：图标 + 数字 + 名称。三者的排布固定，多了也不换行成卡片墙。 */
function StatCell({
  icon: Icon,
  label,
  value,
  to,
  loading,
}: {
  icon: typeof FileText;
  label: string;
  value: number | undefined;
  to: string;
  loading: boolean;
}) {
  return (
    <Link
      to={to}
      className="transition-ui flex items-center gap-2.5 rounded-control px-2 py-1.5 hover:bg-surface-active"
    >
      <Icon aria-hidden="true" className="size-4 shrink-0 text-ink-subtle" />
      <span className="text-sm text-ink-muted">{label}</span>
      {loading ? (
        <Skeleton className="ml-auto h-4 w-8" />
      ) : (
        <span className="tabular ml-auto text-sm font-medium text-ink">
          {count(value)}
        </span>
      )}
    </Link>
  );
}

export function DashboardPage() {
  useDocumentTitle("概览");
  const { can, user } = useAuth();

  const canComments = can("comments:manage");
  const canUsers = can("users:manage");

  const pendingComments = useTotal(
    "comments-pending",
    () =>
      api.GET("/api/v1/console/comments", {
        params: { query: { status: "pending", size: 1 } },
      }),
    canComments,
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
  const published = useTotal("posts-published", () =>
    api.GET("/api/v1/console/posts", {
      params: { query: { status: "published", size: 1 } },
    }),
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
  const media = useTotal("media", () =>
    api.GET("/api/v1/console/media", { params: { query: { size: 1 } } }),
  );
  const users = useTotal(
    "users",
    () => api.GET("/api/v1/console/users", { params: { query: { size: 1 } } }),
    canUsers,
  );

  const recent = useQuery({
    queryKey: ["overview", "recent"],
    queryFn: async () => {
      const { data, response } = await api.GET("/api/v1/console/posts", {
        params: { query: { status: "published", size: 5 } },
      });
      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
      }
      return data?.items ?? [];
    },
  });

  const todos: Todo[] = [];
  if (canComments && (pendingComments.data ?? 0) > 0) {
    todos.push({
      key: "comments",
      label: "待审评论",
      hint: "访客提交后需经审核才会出现在前台",
      value: pendingComments.data ?? 0,
      to: "/comments?status=pending",
      action: "去审核",
      icon: MessageSquare,
      tone: "warn",
    });
  }
  if ((drafts.data ?? 0) > 0) {
    todos.push({
      key: "drafts",
      label: "未完成的草稿",
      hint: "草稿不会出现在前台，也不进订阅源",
      value: drafts.data ?? 0,
      to: "/posts?status=draft",
      action: "继续写",
      icon: FileText,
      tone: "seal",
    });
  }
  if ((scheduled.data ?? 0) > 0) {
    todos.push({
      key: "scheduled",
      label: "定时待发",
      hint: "到点后由后台扫描自动发布",
      value: scheduled.data ?? 0,
      to: "/posts?status=scheduled",
      action: "查看排期",
      icon: CalendarClock,
      tone: "seal",
    });
  }

  const overviewLoading =
    published.isLoading ||
    pages.isLoading ||
    categories.isLoading ||
    tags.isLoading ||
    media.isLoading ||
    users.isLoading;

  return (
    <>
      <PageHeader
        title="概览"
        description={
          user ? `${user.displayName || user.username}，欢迎回来` : undefined
        }
      />

      <div className="flex flex-col gap-6">
        {/*
          需要处理。全无待办时整块不渲染 —— 一个写着「暂无待办」的空面板
          只是每天多占一行视线，而「什么都没有」本身就是最好的消息。
        */}
        {todos.length > 0 ? (
          <section className="flex flex-col gap-3">
            <h2 className="text-base font-medium text-ink">需要处理</h2>
            <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
              {todos.map((todo) => (
                <TodoCard key={todo.key} todo={todo} />
              ))}
            </div>
          </section>
        ) : null}

        <div className="grid gap-4 lg:grid-cols-[1fr_20rem]">
          {/* 最近发布。这一块是「站点还活着」的证据，也是回到最近一篇的最快路径 */}
          <Panel>
            <PanelHeader>
              <PanelTitle>最近发布</PanelTitle>
              <Button variant="ghost" size="sm" asChild>
                <Link to="/posts">
                  全部文章
                  <ArrowRight aria-hidden="true" />
                </Link>
              </Button>
            </PanelHeader>
            <PanelBody className="p-0">
              {recent.isLoading ? (
                <div className="flex flex-col gap-2 p-4">
                  {Array.from({ length: 4 }, (_, i) => (
                    // biome-ignore lint/suspicious/noArrayIndexKey: 静态占位列表
                    <Skeleton key={i} className="h-8 w-full" />
                  ))}
                </div>
              ) : recent.data && recent.data.length > 0 ? (
                <ul className="divide-y divide-line">
                  {recent.data.map((post) => (
                    <li key={post.id}>
                      <Link
                        to={`/posts/${post.id}`}
                        className="transition-ui flex items-center gap-3 px-4 py-2.5 hover:bg-surface-hover"
                      >
                        <span className="min-w-0 flex-1 truncate text-base text-ink">
                          {post.title}
                        </span>
                        {post.pinned ? <Badge tone="seal">置顶</Badge> : null}
                        <time
                          dateTime={post.publishedAt ?? undefined}
                          title={absoluteDate(post.publishedAt)}
                          className="shrink-0 text-xs text-ink-muted"
                        >
                          {relativeTime(post.publishedAt)}
                        </time>
                      </Link>
                    </li>
                  ))}
                </ul>
              ) : (
                <div className="flex flex-col items-center gap-3 px-6 py-12 text-center">
                  <CheckCircle2
                    aria-hidden="true"
                    className="size-6 text-ink-subtle"
                  />
                  <div className="flex flex-col gap-1">
                    <p className="text-sm font-medium text-ink">
                      还没有已发布的文章
                    </p>
                    <p className="text-sm text-ink-muted">
                      写完第一篇，这里就会显示它。
                    </p>
                  </div>
                  <Button variant="primary" size="sm" asChild>
                    <Link to="/posts">去写第一篇</Link>
                  </Button>
                </div>
              )}
            </PanelBody>
          </Panel>

          <Panel className="self-start">
            <PanelHeader>
              <PanelTitle>站点概况</PanelTitle>
            </PanelHeader>
            <PanelBody className="flex flex-col gap-0.5 p-2">
              <StatCell
                icon={FileText}
                label="已发布文章"
                value={published.data}
                to="/posts?status=published"
                loading={published.isLoading}
              />
              <StatCell
                icon={FileText}
                label="独立页面"
                value={pages.data}
                to="/pages"
                loading={pages.isLoading}
              />
              <StatCell
                icon={FolderTree}
                label="分类"
                value={categories.data}
                to="/categories"
                loading={categories.isLoading}
              />
              <StatCell
                icon={Tags}
                label="标签"
                value={tags.data}
                to="/tags"
                loading={tags.isLoading}
              />
              <StatCell
                icon={Image}
                label="附件"
                value={media.data}
                to="/media"
                loading={media.isLoading}
              />
              {canUsers ? (
                <StatCell
                  icon={Users}
                  label="用户"
                  value={users.data}
                  to="/users"
                  loading={users.isLoading}
                />
              ) : null}
              {overviewLoading ? (
                // <output> 是 role="status" 的原生元素，读屏会在内容出现时播报它
                <output className="sr-only">正在载入站点概况</output>
              ) : null}
            </PanelBody>
          </Panel>
        </div>
      </div>
    </>
  );
}
