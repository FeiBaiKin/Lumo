import { PageBody, PageHeader } from "@/components/layout/page-header";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { EmptyState, Skeleton } from "@/components/ui/states";
import { useDocumentTitle } from "@/lib/use-document-title";
import { MenusPage } from "@/pages/appearance/menus";
import { ThemesPage } from "@/pages/appearance/themes";
import { CommentsPage } from "@/pages/content/comments";
import { PagesPage } from "@/pages/content/pages";
import { PostsPage } from "@/pages/content/posts";
import { MediaPage } from "@/pages/media/media";
import { ProfilePage } from "@/pages/profile";
import { SettingsPage } from "@/pages/settings/settings";
import { AboutPage } from "@/pages/system/about";
import { CategoriesPage } from "@/pages/taxonomy/categories";
import { TagsPage } from "@/pages/taxonomy/tags";
import { RolesPage } from "@/pages/users/roles";
import { UsersPage } from "@/pages/users/users";
import { Construction, ScrollText } from "lucide-react";
import { Suspense, lazy } from "react";
import { Link } from "react-router";

/**
 * 内容编辑器按需加载。
 *
 * 它同时拖进 TipTap 与 Milkdown 两套 ProseMirror 生态，minify 后约 900 KB。
 * 静态引入会让**每一次**打开后台都要先下载这两个编辑器 ——
 * 而站长一天里绝大多数时间在看列表与评论，不在写文章。
 */
const ContentEditor = lazy(() =>
  import("@/pages/content/editor").then((m) => ({ default: m.ContentEditor })),
);

/** 编辑器加载中的占位。用骨架屏而不是转圈：高度与真实编辑器接近，加载完不跳。 */
function EditorFallback() {
  return (
    <div className="flex flex-col" aria-busy="true">
      <span className="sr-only">正在载入编辑器</span>
      <div className="h-page-header border-line border-b bg-surface" />
      <div className="mx-auto flex w-full max-w-measure flex-col gap-4 px-6 py-10">
        <Skeleton className="h-10 w-2/3" />
        <Skeleton className="h-[28rem] w-full" />
      </div>
    </div>
  );
}

function LazyEditor({ kind }: { kind: "post" | "page" }) {
  return (
    <Suspense fallback={<EditorFallback />}>
      <ContentEditor kind={kind} />
    </Suspense>
  );
}

/**
 * 「日志」仍是占位 —— 服务端没有日志接口，它需要先有一条读取日志的 API。
 * 在此之前不给它做一个假的页面。
 */
function Placeholder({
  title,
  note,
}: {
  title: string;
  note: string;
}) {
  useDocumentTitle(title);
  return (
    <>
      <PageHeader icon={ScrollText} title={title} />
      <PageBody>
        <Card>
          <EmptyState
            icon={Construction}
            title="这一页还没有做"
            description={note}
            action={
              <Button variant="secondary" size="sm" asChild>
                <Link to="/">返回仪表盘</Link>
              </Button>
            }
          />
        </Card>
      </PageBody>
    </>
  );
}

/**
 * 路由表。
 *
 * 用数组而非手写一堆 `<Route>`：路径必须与 agent.md §8 的页面地图一致，
 * 集中在一处才看得出漏了哪一页，也才能一眼看出还有哪几页没实现。
 */
export const APP_ROUTES = [
  // ---- 仪表盘 ----
  { path: "/", element: null }, // 由 app.tsx 的 index 路由处理

  // ---- 内容 ----
  { path: "posts", element: PostsPage },
  // 新建与编辑共用同一个页面：它们共享同一份表单状态，
  // 拆成两个路由会让「改完标题再点发布」变成两次导航。
  { path: "posts/:id", element: () => <LazyEditor kind="post" /> },
  { path: "pages", element: PagesPage },
  { path: "pages/:id", element: () => <LazyEditor kind="page" /> },
  { path: "categories", element: CategoriesPage },
  { path: "tags", element: TagsPage },
  { path: "comments", element: CommentsPage },

  // ---- 媒体 ----
  { path: "media", element: MediaPage },

  // ---- 外观 ----
  { path: "themes", element: ThemesPage },
  { path: "menus", element: MenusPage },

  // ---- 用户 ----
  { path: "users", element: UsersPage },
  { path: "roles", element: RolesPage },
  { path: "profile", element: ProfilePage },

  // ---- 设置 ----
  { path: "settings", element: () => <SettingsPage defaultGroup="site" /> },
  { path: "settings/:group", element: SettingsPage },

  // ---- 系统 ----
  { path: "about", element: AboutPage },
  {
    path: "logs",
    element: () => (
      <Placeholder
        title="日志"
        note="服务端尚无日志读取接口。要做这一页，得先有一条按级别与时间取日志的 API，并想清楚日志落在文件还是库里。"
      />
    ),
  },
];
