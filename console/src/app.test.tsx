import type { components } from "@/api/schema";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, within } from "@testing-library/react";
import { RouterProvider, createMemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";

/**
 * 等侧边栏把第一项画出来，再把它交给调用方。
 *
 * 两个坑都要绕开：
 *   - 只等 `findByRole("navigation")` 不够 —— 那个 <nav> 外壳始终存在，
 *     菜单项要等接口回来才有，断言会跑在数据到达之前；
 *   - 不能直接 `findByRole("link", { name: "文章" })` —— 窄屏的底部导航条
 *     也有一个同名链接，jsdom 不做 CSS 媒体查询判定，会撞上两个。
 */
async function sidebar() {
  const nav = await screen.findByRole("navigation", { name: "主导航" });
  await within(nav).findByRole("link", { name: "文章" });
  return nav;
}

/**
 * 应用外壳的冒烟测试。
 *
 * 只测路由守卫的三种状态 —— 这层壳最容易出的错不是样式，而是
 * 「会话还没查完就跳登录页」（刷新时闪一下登录界面）与「未登录却能进后台」。
 * 具体页面的行为在各自的测试里覆盖。
 *
 * 这里 mock 的是 `@/api/client` 而不是 `globalThis.fetch`：
 * openapi-fetch 在 `createClient()` 时就捕获了 `globalThis.fetch`
 * （见其 dist/index.mjs 的 `fetch: baseFetch = globalThis.fetch`），
 * 而 client 模块在 import 期就建立了 —— 之后再 stubGlobal 完全不起作用。
 * mock 掉 client 反而更贴合本测试的意图：只关心「会话 API 说是谁」，
 * 不关心 HTTP 细节。
 */

type GetResult = { data?: unknown; response: Response };

const me: components["schemas"]["MeView"] = {
  authMethod: "session",
  permissions: [
    "posts:write",
    "posts:publish",
    "comments:manage",
    "taxonomies:manage",
    "themes:manage",
    "menus:manage",
    "settings:manage",
    "users:manage",
    "roles:manage",
  ],
  user: {
    id: 1,
    username: "admin",
    displayName: "站长",
    email: "admin@example.com",
    avatarUrl: "",
    roles: ["super-admin"],
  },
};

/**
 * 侧边栏菜单的样例响应。
 *
 * 形状与 internal/console 的 NavView 一致。这里再写一份而不是从后端取，
 * 是因为本测试要验的是**权限过滤**这条前端逻辑；「后端声明的图标名前端是否认识」
 * 由 cmd/lumo 的 TestNavigationContract 守着——那条测试直接读前端的图标登记表。
 */
type NavItemWire = components["schemas"]["NavItemView"];

/**
 * 补全菜单项的可选字段。
 *
 * 接口把 permission / keywords / end / hidden 一律返回（Go 侧是值类型，没有 omitempty），
 * 生成出来的类型因此都标成必填。逐项手写这一堆空值会把 fixture 淹没，
 * 而它要表达的信息只有「哪一项需要权限」。
 */
function navItem(
  item: Pick<NavItemWire, "key" | "label" | "path" | "icon" | "group"> &
    Partial<NavItemWire>,
): NavItemWire {
  return {
    order: 0,
    permission: "",
    keywords: "",
    end: false,
    hidden: false,
    ...item,
  };
}

const navigation: components["schemas"]["NavView"] = {
  groups: [
    { name: "dashboard", label: "仪表盘", order: 0 },
    { name: "content", label: "内容", order: 10 },
    { name: "media", label: "媒体", order: 20 },
    { name: "appearance", label: "外观", order: 30 },
    { name: "users", label: "用户", order: 40 },
    { name: "settings", label: "设置", order: 50 },
    { name: "system", label: "系统", order: 60 },
  ],
  items: [
    navItem({
      key: "overview",
      label: "概览",
      path: "/",
      icon: "layout-dashboard",
      group: "dashboard",
      end: true,
    }),
    navItem({
      key: "posts",
      label: "文章",
      path: "/posts",
      icon: "book-open",
      group: "content",
    }),
    navItem({
      key: "pages",
      label: "页面",
      path: "/pages",
      icon: "file-text",
      group: "content",
    }),
    navItem({
      key: "categories",
      label: "分类",
      path: "/categories",
      icon: "folder-tree",
      group: "content",
    }),
    navItem({
      key: "tags",
      label: "标签",
      path: "/tags",
      icon: "tags",
      group: "content",
    }),
    navItem({
      key: "comments",
      label: "评论",
      path: "/comments",
      icon: "message-square",
      group: "content",
    }),
    navItem({
      key: "media",
      label: "附件",
      path: "/media",
      icon: "image",
      group: "media",
    }),
    navItem({
      key: "themes",
      label: "主题",
      path: "/themes",
      icon: "palette",
      group: "appearance",
      permission: "themes:manage",
    }),
    navItem({
      key: "menus",
      label: "菜单",
      path: "/menus",
      icon: "list-tree",
      group: "appearance",
      permission: "menus:manage",
    }),
    navItem({
      key: "users",
      label: "用户",
      path: "/users",
      icon: "users",
      group: "users",
      permission: "users:manage",
    }),
    navItem({
      key: "roles",
      label: "角色",
      path: "/roles",
      icon: "shield-check",
      group: "users",
      permission: "roles:manage",
    }),
    navItem({
      key: "profile",
      label: "个人中心",
      path: "/profile",
      icon: "user-round",
      group: "users",
      hidden: true,
    }),
    navItem({
      key: "settings-site",
      label: "站点",
      path: "/settings/site",
      icon: "settings",
      group: "settings",
      permission: "settings:manage",
    }),
    navItem({
      key: "settings-storage",
      label: "附件存储",
      path: "/settings/storage",
      icon: "hard-drive",
      group: "settings",
      permission: "settings:manage",
    }),
    navItem({
      key: "settings-mail",
      label: "邮件发送",
      path: "/settings/mail",
      icon: "mail",
      group: "settings",
      permission: "settings:manage",
    }),
    navItem({
      key: "settings-comment",
      label: "评论",
      path: "/settings/comment",
      icon: "message-square",
      group: "settings",
      permission: "settings:manage",
    }),
    navItem({
      key: "settings-seo",
      label: "SEO",
      path: "/settings/seo",
      icon: "search",
      group: "settings",
      permission: "settings:manage",
    }),
    navItem({
      key: "about",
      label: "关于",
      path: "/about",
      icon: "info",
      group: "system",
    }),
    navItem({
      key: "logs",
      label: "日志",
      path: "/logs",
      icon: "scroll-text",
      group: "system",
      permission: "settings:manage",
    }),
  ],
};

/** 让每个用例自己决定 /auth/me 返回什么；其余列表接口一律给空分页。 */
let session: components["schemas"]["MeView"] | null = null;

vi.mock("@/api/client", () => ({
  api: {
    GET: vi.fn(async (path: string): Promise<GetResult> => {
      if (path === "/api/v1/console/auth/me") {
        return session
          ? { data: session, response: new Response(null, { status: 200 }) }
          : {
              data: undefined,
              response: new Response(null, { status: 401 }),
            };
      }
      if (path === "/api/v1/console/navigation") {
        return {
          data: navigation,
          response: new Response(null, { status: 200 }),
        };
      }
      return {
        data: { items: [], page: 1, size: 20, total: 0 },
        response: new Response(null, { status: 200 }),
      };
    }),
    POST: vi.fn(),
  },
  csrfToken: () => "test-csrf",
  problemMessage: (problem: { detail?: string; title?: string } | undefined) =>
    problem?.detail || problem?.title || "请求失败",
}));

// 必须在 mock 声明之后动态取用：静态 import 会被提升到 vi.mock 之前。
const { appRoutes } = await import("@/app");
const { AuthProvider } = await import("@/components/auth/auth-provider");
const { ThemeProvider } = await import("@/components/theme/theme-provider");

function renderApp() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  // 用数据路由而不是 MemoryRouter + <Routes>：应用本身跑在 createBrowserRouter
  // 之下（useBlocker 需要它），测试环境要保持同一种路由形态。
  const router = createMemoryRouter(appRoutes(), { initialEntries: ["/"] });
  return render(
    <ThemeProvider>
      <QueryClientProvider client={queryClient}>
        <AuthProvider>
          <RouterProvider router={router} />
        </AuthProvider>
      </QueryClientProvider>
    </ThemeProvider>,
  );
}

describe("App 路由守卫", () => {
  afterEach(() => {
    session = null;
  });

  it("未登录时落到登录页", async () => {
    session = null;
    renderApp();
    expect(
      await screen.findByRole("heading", { name: /登录以管理你的站点/ }),
    ).toBeInTheDocument();
  });

  it("已登录时渲染外壳与侧栏导航", async () => {
    session = me;
    renderApp();

    // 侧栏七组的组标题，来自 agent.md §8 的页面地图。
    // 断言限定在导航内：概览页的「站点概况」里也有「分类」「标签」等同样的词。
    // 用 getAllByText：「用户」既是分组标题也是组内条目名（§8 的页面地图如此），
    // 同名出现两次是预期的。
    const nav = await sidebar();
    for (const label of [
      "仪表盘",
      "内容",
      "媒体",
      "外观",
      "用户",
      "设置",
      "系统",
    ]) {
      expect(within(nav).getAllByText(label).length).toBeGreaterThan(0);
    }
  });

  it("已登录时不再显示登录表单", async () => {
    session = me;
    renderApp();

    await sidebar();
    expect(screen.queryByLabelText("密码")).not.toBeInTheDocument();
  });

  it("无权限的导航项与整组都不渲染", async () => {
    session = { ...me, permissions: ["posts:write"] };
    renderApp();

    const nav = await sidebar();

    // 始终可见：内容组（文章 / 页面 / 分类 / 标签 / 评论）与媒体组 ——
    // 服务端对这些的读操作对任何已认证用户开放
    for (const label of ["文章", "页面", "分类", "标签", "评论", "附件"]) {
      expect(within(nav).getByText(label)).toBeInTheDocument();
    }

    // 按权限隐藏：「用户」组两项、「外观」组两项、「系统」组的日志
    for (const label of ["用户", "角色", "主题", "菜单", "日志"]) {
      expect(within(nav).queryByText(label)).not.toBeInTheDocument();
    }

    // 「系统」组只剩「关于」，整组不该因此消失
    expect(within(nav).getByText("关于")).toBeInTheDocument();
  });

  it("持有 _any 权限即展示对应入口", async () => {
    // 只给 comments:manage_any，未直接持有 comments:manage
    session = {
      ...me,
      permissions: ["comments:manage_any", "taxonomies:manage"],
    };
    renderApp();

    const nav = await sidebar();
    expect(within(nav).getByText("评论")).toBeInTheDocument();
  });
});
