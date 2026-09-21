import { Skeleton } from "@/components/ui/states";
import { CommentsPage } from "@/pages/content/comments";
import { PagesPage } from "@/pages/content/pages";
import { PostsPage } from "@/pages/content/posts";
import { MediaPage } from "@/pages/media/media";
import { ProfilePage } from "@/pages/profile";
import { AboutPage } from "@/pages/system/about";
import { CategoriesPage } from "@/pages/taxonomy/categories";
import { TagsPage } from "@/pages/taxonomy/tags";
import { RolesPage } from "@/pages/users/roles";
import { UsersPage } from "@/pages/users/users";
import { Suspense, lazy } from "react";

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

/**
 * 菜单页也按需加载。
 *
 * 它引入 motion 的 `Reorder` 做拖动排序，而 motion 会把整套拖拽与布局动画引擎
 * 一起带进包里（minify 后约 133 KB）。菜单是偶尔才改一次的东西，
 * 不该让每一天的每一次打开后台都先下载它 —— 与内容编辑器同一个理由。
 */
const MenusPage = lazy(() =>
  import("@/pages/appearance/menus").then((m) => ({ default: m.MenusPage })),
);

/**
 * 表单重的三页也按需加载：站点设置、主题（内含主题设置）、插件（内含插件设置）。
 *
 * 它们共用同一套表单引擎（17 种控件），而引擎里为了重复条目的拖动排序引入了
 * motion 的 Reorder（约 133 KB）。把这三页拆出去之后，拖动引擎只跟着表单走，
 * 不会挂在每一次打开后台的首屏包上。
 */
const SettingsPage = lazy(() =>
  import("@/pages/settings/settings").then((m) => ({
    default: m.SettingsPage,
  })),
);
const ThemesPage = lazy(() =>
  import("@/pages/appearance/themes").then((m) => ({ default: m.ThemesPage })),
);
const LogsPage = lazy(() =>
  import("@/pages/system/logs").then((m) => ({ default: m.LogsPage })),
);
const PluginsPage = lazy(() =>
  import("@/pages/system/plugins").then((m) => ({ default: m.PluginsPage })),
);

/** 按需加载页面的占位：与真实页面同为「页头 + 主体」两段，加载完不跳。 */
function PageFallback() {
  return (
    <div className="flex flex-col" aria-busy="true">
      <span className="sr-only">正在载入</span>
      <div className="h-page-header border-line border-b bg-surface" />
      <div className="flex flex-col gap-4 p-0 md:p-4">
        <Skeleton className="h-64 w-full" />
      </div>
    </div>
  );
}

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

/** 按需加载的一页（见上面 MenusPage 的说明）。 */
function LazyPage({ element }: { element: React.ReactNode }) {
  return <Suspense fallback={<PageFallback />}>{element}</Suspense>;
}

/**
 * 路由表。
 *
 * 用数组而非手写一堆 `<Route>`：路径必须与侧边栏的页面地图一致，
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
  { path: "themes", element: () => <LazyPage element={<ThemesPage />} /> },
  { path: "menus", element: () => <LazyPage element={<MenusPage />} /> },

  // ---- 用户 ----
  { path: "users", element: UsersPage },
  { path: "roles", element: RolesPage },
  { path: "profile", element: ProfilePage },

  // ---- 设置 ----
  { path: "settings", element: () => <LazyPage element={<SettingsPage />} /> },
  {
    path: "settings/:group",
    element: () => <LazyPage element={<SettingsPage />} />,
  },

  // ---- 系统 ----
  { path: "about", element: AboutPage },
  { path: "plugins", element: () => <LazyPage element={<PluginsPage />} /> },
  { path: "logs", element: () => <LazyPage element={<LogsPage />} /> },
];
