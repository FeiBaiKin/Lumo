import { SettingsPage } from "@/pages/settings/settings";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { RouterProvider, createMemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

/**
 * 站点设置页：多个分组一页、折叠、整页保存。
 *
 * 这一页的风险不在某个控件长什么样（那是表单引擎的测试），而在**折叠与整页保存
 * 这两件事叠起来之后的几条边**：收起的区块会不会被漏掉、主开关会不会画两个、
 * 部分保存失败时失败的那一组会不会连坐被重置。三条都发生在用户看不见的地方。
 */

const { getMock, putMock } = vi.hoisted(() => ({
  getMock: vi.fn(),
  putMock: vi.fn(),
}));

vi.mock("@/api/client", () => ({
  api: { GET: getMock, PUT: putMock, POST: vi.fn() },
  problemMessage: (error: { detail?: string } | undefined) =>
    error?.detail ?? "请求失败",
}));

/** 两个分组：一个没有主开关（site），一个有且带条件字段（mail）。 */
const groups = [
  {
    name: "site",
    label: "站点",
    description: "站点的基本信息",
    order: 0,
    icon: "settings",
    toggle: "",
    public: [],
    secretSet: [],
    defaults: { title: "Lumo" },
    values: { title: "Lumo" },
    schema: {
      type: "object",
      properties: { title: { type: "string", title: "站点标题" } },
      required: ["title"],
      "x-sections": [{ title: "基本信息", fields: ["title"] }],
    },
  },
  {
    name: "mail",
    label: "邮件发送",
    description: "SMTP 发信配置",
    order: 30,
    icon: "mail",
    toggle: "enabled",
    public: ["enabled"],
    secretSet: [],
    defaults: { enabled: false, host: "" },
    values: { enabled: false, host: "smtp.example.com" },
    schema: {
      type: "object",
      properties: {
        enabled: { type: "boolean", title: "启用邮件发送" },
        host: {
          type: "string",
          title: "SMTP 服务器",
          "x-show-if": { field: "enabled", op: "eq", value: true },
        },
      },
      required: ["host"],
      "x-sections": [
        { title: "发信", fields: ["enabled"] },
        { title: "SMTP 服务器", fields: ["host"] },
      ],
    },
  },
];

const ok = (data: unknown) => ({
  data,
  response: new Response(null, { status: 200 }),
});

function renderPage(path = "/settings") {
  const router = createMemoryRouter(
    [
      { path: "/settings", element: <SettingsPage /> },
      { path: "/settings/:group", element: <SettingsPage /> },
    ],
    { initialEntries: [path] },
  );
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
}

/**
 * 取某个分组的标题按钮。
 *
 * 按 aria-expanded 挑出来，而不是直接按名字取：分组名还会出现在保存条的
 * 「哪几项改了」与错误摘要的每一条里，按名字取会撞上三个。
 */
function sectionButton(label: string): HTMLElement {
  const found = screen
    .getAllByRole("button", { name: new RegExp(label) })
    .find((element) => element.hasAttribute("aria-expanded"));
  if (!found) {
    throw new Error(`没有找到分组「${label}」的标题按钮`);
  }
  return found;
}

/**
 * 服务端的当前值。
 *
 * 保存成功后由 PUT 更新，GET 随后返回新值——这一步必须模拟出来：
 * 「成功的分组重新对齐基线、失败的分组保持改动」正是部分失败那条路径的要害，
 * 而固定不变的 GET 会让两者看起来一样脏。
 */
let serverState: typeof groups;

beforeEach(() => {
  serverState = structuredClone(groups);
  // 每次都返回一份新拷贝：直接交出同一个对象的话，React Query 的结构共享会认出
  // 「还是上次那个引用」而不触发重算，于是保存成功后基线永远不更新。
  // 真实接口每次都是新解析出来的 JSON，这里要照着来。
  getMock.mockImplementation(async () =>
    ok({ items: structuredClone(serverState) }),
  );
  putMock.mockImplementation(
    async (
      _path: string,
      init: { params: { path: { group: string } }; body: object },
    ) => {
      const item = serverState.find((g) => g.name === init.params.path.group);
      if (item) {
        item.values = { ...item.values, ...init.body };
      }
      return { response: new Response(null, { status: 200 }) };
    },
  );
});

afterEach(() => {
  vi.clearAllMocks();
});

describe("站点设置页的版面", () => {
  it("所有分组都在同一页上，且默认一个都不展开", async () => {
    renderPage();

    // 两组的标题都在，不必切换菜单才能看见另一组
    await screen.findByText("站点");
    expect(screen.getByText("邮件发送")).toBeInTheDocument();

    // 展开第一组也算「默认展开」：它有十个字段，两屏高，后面几组全被挤出视口，
    // 折叠就白折了。第一屏要的是一张目录。
    expect(sectionButton("站点")).toHaveAttribute("aria-expanded", "false");
    expect(sectionButton("邮件发送")).toHaveAttribute("aria-expanded", "false");
  });

  it("点标题展开与收起", async () => {
    renderPage();
    await screen.findByText("邮件发送");
    const mail = sectionButton("邮件发送");

    fireEvent.click(mail);
    expect(mail).toHaveAttribute("aria-expanded", "true");
    fireEvent.click(mail);
    expect(mail).toHaveAttribute("aria-expanded", "false");
  });

  it("地址点名的分组直接展开", async () => {
    renderPage("/settings/mail");

    await waitFor(() =>
      expect(sectionButton("邮件发送")).toHaveAttribute(
        "aria-expanded",
        "true",
      ),
    );
    // 点名一组就只展开那一组，否则「从命令面板跳过来」与「随便打开一页」没有区别
    expect(sectionButton("站点")).toHaveAttribute("aria-expanded", "false");
  });

  it("主开关在标题栏上，展开后的表单里不再出现第二个", async () => {
    renderPage();
    const toggle = await screen.findByRole("switch", {
      name: "启用邮件发送",
    });
    expect(toggle).toBeInTheDocument();

    fireEvent.click(sectionButton("邮件发送"));
    // 画两个开关的后果不是难看，而是两个都是活的：改一个，另一个不动，
    // 站长无从知道保存下去的是哪一个。
    expect(
      screen.getAllByRole("switch", { name: "启用邮件发送" }),
    ).toHaveLength(1);
  });

  it("条件字段全被藏起来时说明原因，而不是留一块空白", async () => {
    renderPage();
    await screen.findByText("邮件发送");
    fireEvent.click(sectionButton("邮件发送"));
    expect(screen.getByText(/开启上面的开关后/)).toBeInTheDocument();
  });
});

describe("整页保存", () => {
  it("只提交改动过的分组", async () => {
    renderPage();
    await screen.findByText("站点");
    fireEvent.click(sectionButton("站点"));
    const title = screen.getByLabelText("站点标题");
    fireEvent.change(title, { target: { value: "新站名" } });

    // 有改动才出现保存条：没改过东西时它只是一条占地方的横线
    const bar = await screen.findByText(/1 项设置有未保存的改动/);
    expect(bar).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "保存修改" }));

    await waitFor(() => expect(putMock).toHaveBeenCalledTimes(1));
    expect(putMock).toHaveBeenCalledWith(
      "/api/v1/console/settings/{group}",
      expect.objectContaining({
        params: { path: { group: "site" } },
        body: expect.objectContaining({ title: "新站名" }),
      }),
    );
  });

  it("收起的区块改动了照样一起保存，并在标题上标出来", async () => {
    renderPage();
    // 标题栏上的主开关：不展开就能改
    const toggle = await screen.findByRole("switch", { name: "启用邮件发送" });
    fireEvent.click(toggle);

    // 收起状态下改的东西必须说出来，否则保存时站长不知道自己提交了什么
    expect(sectionButton("邮件发送")).toHaveAttribute("aria-expanded", "false");
    expect(within(sectionButton("邮件发送")).getByText("已修改")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "保存修改" }));
    await waitFor(() => expect(putMock).toHaveBeenCalledTimes(1));
    expect(putMock.mock.calls[0]?.[1]?.params.path.group).toBe("mail");
  });

  it("部分失败时失败的那一组保持改动，成功的那组不受影响", async () => {
    // 邮件那一组被服务端拒掉，站点那一组照常存下（并落进 serverState）
    putMock.mockImplementation(
      async (
        _path: string,
        init: { params: { path: { group: string } }; body: object },
      ) => {
        if (init.params.path.group === "mail") {
          return {
            error: { detail: "SMTP 服务器不能为空" },
            response: new Response(null, { status: 422 }),
          };
        }
        const item = serverState.find((g) => g.name === init.params.path.group);
        if (item) {
          item.values = { ...item.values, ...init.body };
        }
        return { response: new Response(null, { status: 200 }) };
      },
    );

    renderPage();
    await screen.findByText("站点");
    fireEvent.click(sectionButton("站点"));
    fireEvent.change(screen.getByLabelText("站点标题"), {
      target: { value: "新站名" },
    });
    fireEvent.click(screen.getByRole("switch", { name: "启用邮件发送" }));

    expect(await screen.findByText(/2 项设置有未保存的改动/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "保存修改" }));

    // 失败的那组展开、标出来，成功的那组退出脏清单
    await waitFor(() =>
      expect(sectionButton("邮件发送")).toHaveAttribute(
        "aria-expanded",
        "true",
      ),
    );
    expect(
      within(sectionButton("邮件发送")).getByText("需要修改"),
    ).toBeTruthy();
    // 服务端那句话要原样出现在摘要里，而不是被包装成「保存失败」
    expect(screen.getByText(/SMTP 服务器不能为空/)).toBeTruthy();
    // 站点那一组存上了，脏清单里只剩邮件
    expect(await screen.findByText(/1 项设置有未保存的改动/)).toBeTruthy();
  });

  it("放弃修改把所有分组一起还原", async () => {
    renderPage();
    await screen.findByText("站点");
    fireEvent.click(sectionButton("站点"));
    fireEvent.change(screen.getByLabelText("站点标题"), {
      target: { value: "新站名" },
    });
    fireEvent.click(screen.getByRole("switch", { name: "启用邮件发送" }));

    fireEvent.click(screen.getByRole("button", { name: "放弃修改" }));

    await waitFor(() =>
      expect(screen.queryByText(/项设置有未保存的改动/)).toBeNull(),
    );
    expect(screen.getByLabelText("站点标题")).toHaveValue("Lumo");
  });
});
