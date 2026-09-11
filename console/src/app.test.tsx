import type { components } from "@/api/schema";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";

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
const { App } = await import("@/app");
const { AuthProvider } = await import("@/components/auth/auth-provider");
const { ThemeProvider } = await import("@/components/theme/theme-provider");

function renderApp() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <ThemeProvider>
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={["/"]}>
          <AuthProvider>
            <App />
          </AuthProvider>
        </MemoryRouter>
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
    const nav = await screen.findByRole("navigation", { name: "主导航" });
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

    await screen.findByRole("navigation", { name: "主导航" });
    expect(screen.queryByLabelText("密码")).not.toBeInTheDocument();
  });

  it("无权限的导航项与整组都不渲染", async () => {
    session = { ...me, permissions: ["posts:write"] };
    renderApp();

    const nav = await screen.findByRole("navigation", { name: "主导航" });

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

    const nav = await screen.findByRole("navigation", { name: "主导航" });
    expect(within(nav).getByText("评论")).toBeInTheDocument();
  });
});
