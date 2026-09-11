import { Button } from "@/components/ui/button";
import { PageHeader } from "@/components/ui/panel";
import { useDocumentTitle } from "@/lib/use-document-title";
import { MenusPage } from "@/pages/appearance/menus";
import { ThemesPage } from "@/pages/appearance/themes";
import { CommentsPage } from "@/pages/content/comments";
import { PagesPage } from "@/pages/content/pages";
import { PostsPage } from "@/pages/content/posts";
import { MediaPage } from "@/pages/media/media";
import { SettingsPage } from "@/pages/settings/settings";
import { AboutPage } from "@/pages/system/about";
import { CategoriesPage } from "@/pages/taxonomy/categories";
import { TagsPage } from "@/pages/taxonomy/tags";
import { RolesPage } from "@/pages/users/roles";
import { UsersPage } from "@/pages/users/users";
import { Construction } from "lucide-react";
import { Link } from "react-router";

/**
 * 路由表。
 *
 * 用数组而非手写一堆 `<Route>`：路径必须与 agent.md §8 的页面地图一致，
 * 集中在一处才看得出漏了哪一页，也才能一眼看出还有哪几页没实现。
 *
 * 「日志」仍是占位 —— 服务端没有日志接口，它需要先有一条读取日志的 API
 * （见 STATUS.md 的待办）。在此之前不给它做一个假的页面。
 */
function Placeholder({
  title,
  group,
  note,
}: {
  title: string;
  group: string;
  note: string;
}) {
  useDocumentTitle(title);
  return (
    <>
      <PageHeader title={title} description={`${group} · 待实现`} />
      <div className="flex flex-col items-center justify-center gap-3 rounded-panel border border-line border-dashed bg-surface px-6 py-16 text-center">
        <Construction aria-hidden="true" className="size-7 text-ink-subtle" />
        <p className="text-sm text-ink-muted">{note}</p>
        <Button variant="secondary" size="sm" asChild>
          <Link to="/">返回概览</Link>
        </Button>
      </div>
    </>
  );
}

export const APP_ROUTES = [
  // ---- 仪表盘 ----
  { path: "/", element: null }, // 由 app.tsx 的 index 路由处理

  // ---- 内容 ----
  { path: "posts", element: PostsPage },
  { path: "pages", element: PagesPage },
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
        group="系统"
        note="服务端尚无日志读取接口。要做这一页，得先有一条按级别与时间取日志的 API，并想清楚日志落在文件还是库里。"
      />
    ),
  },
];
