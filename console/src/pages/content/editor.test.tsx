import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import { RouterProvider, createMemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";

/**
 * 内容编辑器的回归测试。
 *
 * 覆盖审查发现的四类问题：切换内容不重建状态、保存失败后仍发布、
 * 切换正文格式丢正文、站内导航绕过未保存提醒。
 *
 * 两个编辑器组件被替换成轻量替身：真实实现拖进 TipTap 与 Milkdown，
 * 在 jsdom 里既慢又与本测试关心的事情（表单状态与请求时序）无关。
 * 替身保留「初值进、内容出」这一对契约。
 */

type Post = Record<string, unknown>;

/** openapi-fetch 的入参形态；测试只用到路径参数。 */
type PathParams = { params?: { path?: Record<string, unknown> } };

const postA: Post = {
  id: 1,
  type: "post",
  title: "Review Alpha",
  slug: "review-alpha",
  status: "draft",
  visibility: "public",
  rawType: "html",
  raw: "<p>ALPHA BODY</p>",
  content: "<p>ALPHA BODY</p>",
  excerpt: "",
  excerptAuto: true,
  coverUrl: "",
  pinned: false,
  template: "",
  authorId: 1,
  publishedAt: null,
  trashedAt: null,
  meta: {},
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
  categories: [],
  tags: [],
};

const postB: Post = {
  ...postA,
  id: 2,
  title: "Review Beta",
  slug: "review-beta",
  raw: "<p>BETA BODY</p>",
  content: "<p>BETA BODY</p>",
};

/** 每次请求的记录，用于断言「有没有发出去」。 */
type Recorder = {
  posts: Record<number, Post>;
  saveStatus: number;
  publishCalls: number;
  saveCalls: number;
  saveBodies: unknown[];
  delaySave?: Promise<void> | undefined;
};

const state: Recorder = {
  posts: { 1: postA, 2: postB },
  saveStatus: 200,
  publishCalls: 0,
  saveCalls: 0,
  saveBodies: [],
};

vi.mock("@/api/client", () => ({
  api: {
    // 注意：openapi-fetch 传进来的是**路径模板**（/posts/{id}），
    // 具体 ID 在 params.path 里，测试要自己按参数取。
    GET: vi.fn(async (path: string, init?: PathParams) => {
      const ok = (data: unknown) => ({
        data,
        response: new Response(null, { status: 200 }),
      });
      if (path === "/api/v1/console/auth/me") {
        return ok({
          authMethod: "session",
          permissions: ["posts:write", "posts:publish", "taxonomies:manage"],
          user: {
            id: 1,
            username: "admin",
            displayName: "站长",
            email: "a@example.com",
            avatarUrl: "",
            roles: ["super-admin"],
          },
        });
      }
      if (path === "/api/v1/console/categories/tree") {
        return ok({
          items: [
            { id: 5, name: "技术", slug: "tech", parentId: null, children: [] },
          ],
        });
      }
      if (path === "/api/v1/console/tags") {
        return ok({ items: [{ id: 7, name: "Go", slug: "go" }] });
      }
      if (path === "/api/v1/console/posts/{id}") {
        const post = state.posts[Number(init?.params?.path?.id)];
        return post
          ? ok(post)
          : { data: undefined, response: new Response(null, { status: 404 }) };
      }
      if (path.endsWith("/revisions")) {
        return ok({ items: [] });
      }
      if (path === "/api/v1/console/themes") {
        return ok({ items: [] });
      }
      return ok({ items: [], page: 1, size: 20, total: 0 });
    }),
    PUT: vi.fn(async (_path: string, init?: { body?: unknown }) => {
      state.saveCalls += 1;
      state.saveBodies.push(init?.body);
      if (state.delaySave) {
        await state.delaySave;
      }
      if (state.saveStatus !== 200) {
        return {
          data: undefined,
          error: { detail: "slug 已被占用" },
          response: new Response(null, { status: state.saveStatus }),
        };
      }
      return {
        data: postA,
        error: undefined,
        response: new Response(null, { status: 200 }),
      };
    }),
    POST: vi.fn(async (path: string) => {
      if (path.endsWith("/publish")) {
        state.publishCalls += 1;
        return {
          data: postA,
          error: undefined,
          response: new Response(null, { status: 200 }),
        };
      }
      return {
        data: postA,
        error: undefined,
        response: new Response(null, { status: 200 }),
      };
    }),
    DELETE: vi.fn(),
  },
  csrfToken: () => "test-csrf",
  problemMessage: (problem: { detail?: string; title?: string } | undefined) =>
    problem?.detail || problem?.title || "请求失败",
}));

// 两个编辑器的替身：显示初值、提供一个「改内容」的入口。
vi.mock("@/components/editor/html-editor", () => ({
  HtmlEditor: ({
    initialContent,
    onChange,
  }: {
    initialContent: string;
    onChange: (html: string) => void;
  }) => (
    <div>
      <span data-testid="html-editor">{initialContent}</span>
      <button type="button" onClick={() => onChange("<p>EDITED</p>")}>
        改富文本
      </button>
    </div>
  ),
}));

vi.mock("@/components/editor/markdown-editor", () => ({
  MarkdownEditor: ({
    initialContent,
    onChange,
  }: {
    initialContent: string;
    onChange: (markdown: string) => void;
  }) => (
    <div>
      <span data-testid="markdown-editor">{initialContent}</span>
      <button type="button" onClick={() => onChange("# EDITED")}>
        改 Markdown
      </button>
    </div>
  ),
}));

const { ContentEditor } = await import("@/pages/content/editor");
const { AuthProvider } = await import("@/components/auth/auth-provider");

function renderEditor(initialPath: string) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const router = createMemoryRouter(
    [
      { path: "/posts/:id", element: <ContentEditor kind="post" /> },
      { path: "/posts", element: <div>文章列表</div> },
      { path: "/pages", element: <div>页面列表</div> },
    ],
    { initialEntries: [initialPath] },
  );
  const ui = render(
    <QueryClientProvider client={queryClient}>
      <AuthProvider>
        <RouterProvider router={router} />
      </AuthProvider>
    </QueryClientProvider>,
  );
  return { router, ui };
}

/** 等待表单载入完成：标题输入框出现且填好了值。 */
async function waitForTitle(title: string) {
  const input = (await screen.findByLabelText("标题")) as HTMLInputElement;
  await waitFor(() => expect(input.value).toBe(title));
  return input;
}

/** 标题输入框没有 label 关联（sr-only 的 label 用 htmlFor），按 id 取。 */
function titleInput(): HTMLInputElement {
  const input = document.getElementById("content-title");
  if (!input) {
    throw new Error("找不到标题输入框");
  }
  return input as HTMLInputElement;
}

function until(assertion: () => void, timeout = 1000) {
  return waitFor(assertion, { timeout });
}

afterEach(() => {
  state.posts = { 1: postA, 2: postB };
  state.saveStatus = 200;
  state.publishCalls = 0;
  state.saveCalls = 0;
  state.saveBodies = [];
  state.delaySave = undefined;
});

describe("内容编辑器", () => {
  it("按 id 重建编辑会话：切到另一篇时显示的是它的内容", async () => {
    const { router } = renderEditor("/posts/1");
    expect((await waitForTitle("Review Alpha")).value).toBe("Review Alpha");

    // 模拟从文章 A 用命令面板跳到文章 B。
    await act(async () => {
      await router.navigate("/posts/2");
    });

    await waitForTitle("Review Beta");
    expect(titleInput().value).toBe("Review Beta");
  });

  it("保存失败时不再继续发布", async () => {
    state.saveStatus = 409; // 例如 slug 撞车
    const { ui } = renderEditor("/posts/1");
    await waitForTitle("Review Alpha");

    // 改一下正文让表单变脏，发布按钮才会先走保存。
    (await screen.findByText("改富文本")).click();
    const publishButton = await screen.findByRole("button", { name: "发布" });
    await until(() => expect(screen.getByText("有未保存的修改")).toBeTruthy());

    publishButton.click();

    // 保存确实发出去了，并且返回 409。
    await until(() => expect(state.saveCalls).toBe(1));
    // 关键断言：保存失败后发布请求一次都不许发。
    await new Promise((resolve) => setTimeout(resolve, 50));
    expect(state.publishCalls).toBe(0);
    ui.unmount();
  });

  it("表单不脏时直接发布，不发多余的保存请求", async () => {
    const { ui } = renderEditor("/posts/1");
    await waitForTitle("Review Alpha");

    (await screen.findByRole("button", { name: "发布" })).click();

    await until(() => expect(state.publishCalls).toBe(1));
    expect(state.saveCalls).toBe(0);
    ui.unmount();
  });

  it("切换到 Markdown 不会清空正文", async () => {
    const { ui } = renderEditor("/posts/1");
    await waitForTitle("Review Alpha");
    expect(screen.getByTestId("html-editor").textContent).toBe(
      "<p>ALPHA BODY</p>",
    );

    const formatTabs = await screen.findByRole("navigation", {
      name: "正文格式",
    });
    (
      await within(formatTabs).findByRole("button", { name: "Markdown" })
    ).click();
    // 确认弹窗：说清楚会发生什么，然后原样交接给 Markdown 编辑器。
    (await screen.findByRole("button", { name: "切换" })).click();

    await waitFor(() =>
      expect(screen.getByTestId("markdown-editor").textContent).toBe(
        "<p>ALPHA BODY</p>",
      ),
    );
    ui.unmount();
  });

  it("分类控件用实体 ID 而不是 undefined", async () => {
    const { ui } = renderEditor("/posts/1");
    await waitForTitle("Review Alpha");

    (await screen.findByRole("button", { name: "设置" })).click();
    const checkbox = await screen.findByLabelText("技术");
    expect(checkbox.id).toBe("content-category-5");
    ui.unmount();
  });

  it("有未保存修改时，站内导航先弹确认", async () => {
    const { router } = renderEditor("/posts/1");
    await waitForTitle("Review Alpha");

    (await screen.findByText("改富文本")).click();
    await act(async () => {
      await router.navigate("/posts").catch(() => {});
    });

    // 没有确认就不该离开：仍然停在编辑页，并弹出对话框。
    expect(await screen.findByText("离开前要先保存吗？")).toBeInTheDocument();
    expect(screen.queryByText("文章列表")).not.toBeInTheDocument();
  });

  it("保存期间重复点击只会发出一个请求", async () => {
    let releaseSave: (() => void) | undefined;
    state.delaySave = new Promise<void>((resolve) => {
      releaseSave = resolve;
    });

    const { ui } = renderEditor("/posts/1");
    await waitForTitle("Review Alpha");
    (await screen.findByText("改富文本")).click();

    const saveButton = await screen.findByRole("button", { name: "保存" });
    saveButton.click();
    saveButton.click();
    saveButton.click();

    await until(() => expect(state.saveCalls).toBe(1));
    expect(saveButton).toBeDisabled();

    releaseSave?.();
    await until(() => expect(saveButton).not.toBeDisabled());
    expect(state.saveCalls).toBe(1);
    ui.unmount();
  });
});
