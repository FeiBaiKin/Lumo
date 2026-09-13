import { queryClient } from "@/api/query-client";
import { AuthProvider } from "@/components/auth/auth-provider";
import {
  type MediaPick,
  MediaPickerDialog,
} from "@/components/media/media-picker";
import { QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

/**
 * 附件选择器的回归测试。
 *
 * 盯三件事，都是换掉系统弹窗这次改造的要点：
 *   - 网格里选中一张再确认，交回去的是那一张的地址与 alt（不是缩略图地址）
 *   - 多选按「选中顺序」交回，而不是按列表顺序
 *   - 「网络地址」页签会拦住 javascript: 之类的协议，且不关闭弹窗
 *
 * 关着的时候一个请求都不该发 —— 图片字段在设置页里成片出现，
 * 每个都预先拉一页附件的话，打开设置页会发出十几个请求。
 */

type MediaRow = Record<string, unknown>;

function image(id: number, name: string): MediaRow {
  return {
    id,
    url: `/uploads/${name}`,
    kind: "image",
    mime: "image/png",
    alt: `${name} 的替代文本`,
    title: "",
    originalName: name,
    filename: name,
    storageKey: name,
    checksum: "",
    driver: "local",
    size: 1024,
    width: 800,
    height: 600,
    uploaderId: 1,
    thumbnails: [{ name: "medium", url: `/uploads/thumb-${name}` }],
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
  };
}

const listCalls: string[] = [];

vi.mock("@/api/client", () => ({
  csrfToken: () => "test-token",
  problemMessage: () => "请求失败",
  api: {
    GET: vi.fn(async (path: string) => {
      const ok = (data: unknown) => ({
        data,
        response: new Response(null, { status: 200 }),
      });
      if (path === "/api/v1/console/auth/me") {
        return ok({
          authMethod: "session",
          permissions: ["media:write"],
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
      if (path === "/api/v1/console/media") {
        listCalls.push(path);
        return ok({
          items: [image(1, "alpha.png"), image(2, "beta.png")],
          page: 1,
          size: 24,
          total: 2,
        });
      }
      return ok({ items: [], page: 1, size: 24, total: 0 });
    }),
  },
}));

function open(props: {
  multiple?: boolean;
  onSelect: (picks: MediaPick[]) => void;
  isOpen?: boolean;
}) {
  render(
    <QueryClientProvider client={queryClient}>
      <AuthProvider>
        <MediaPickerDialog
          open={props.isOpen ?? true}
          onOpenChange={() => {}}
          onSelect={props.onSelect}
          multiple={props.multiple ?? false}
          title="选择图片"
          confirmLabel="插入"
        />
      </AuthProvider>
    </QueryClientProvider>,
  );
}

afterEach(() => {
  queryClient.clear();
  listCalls.length = 0;
});

describe("附件选择器", () => {
  it("关着的时候不拉附件列表", async () => {
    const onSelect = vi.fn();
    open({ onSelect, isOpen: false });
    // 等会话查询落地，确认这段时间里确实没有人去拉附件
    await waitFor(() => expect(screen.queryByText("选择图片")).toBeNull());
    expect(listCalls).toHaveLength(0);
  });

  it("选中一张后交回原图地址与 alt", async () => {
    const onSelect = vi.fn();
    open({ onSelect });

    const tile = await screen.findByTitle("alpha.png");
    fireEvent.click(tile);
    expect(tile).toHaveAttribute("aria-pressed", "true");

    fireEvent.click(screen.getByRole("button", { name: "插入" }));

    // 交回的必须是原图地址：缩略图是给网格看的，正文里要的是原图
    expect(onSelect).toHaveBeenCalledWith([
      {
        url: "/uploads/alpha.png",
        alt: "alpha.png 的替代文本",
        title: "",
      },
    ]);
  });

  it("多选按选中顺序交回", async () => {
    const onSelect = vi.fn();
    open({ onSelect, multiple: true });

    // 先点第二张再点第一张：交回的顺序应当与点击顺序一致
    fireEvent.click(await screen.findByTitle("beta.png"));
    fireEvent.click(screen.getByTitle("alpha.png"));
    expect(screen.getByText("已选 2 张")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "插入" }));

    const picks = onSelect.mock.calls[0]?.[0] as MediaPick[];
    expect(picks.map((pick) => pick.url)).toEqual([
      "/uploads/beta.png",
      "/uploads/alpha.png",
    ]);
  });

  it("再点一次取消选中", async () => {
    const onSelect = vi.fn();
    open({ onSelect });

    const tile = await screen.findByTitle("alpha.png");
    fireEvent.click(tile);
    fireEvent.click(tile);

    expect(tile).toHaveAttribute("aria-pressed", "false");
    expect(screen.getByRole("button", { name: "插入" })).toBeDisabled();
  });

  it("网络地址页签拦住危险协议，且不关闭弹窗", async () => {
    const onSelect = vi.fn();
    open({ onSelect });

    fireEvent.click(await screen.findByRole("button", { name: "网络地址" }));

    const field = screen.getByLabelText("图片地址");
    fireEvent.change(field, { target: { value: "javascript:alert(1)" } });
    fireEvent.click(screen.getByRole("button", { name: "插入" }));

    expect(onSelect).not.toHaveBeenCalled();
    expect(screen.getByRole("alert")).toHaveTextContent("只支持 http(s)");

    // 改成正常地址后才放行
    fireEvent.change(field, {
      target: { value: "https://cdn.example.com/a.png" },
    });
    fireEvent.click(screen.getByRole("button", { name: "插入" }));
    expect(onSelect).toHaveBeenCalledWith([
      { url: "https://cdn.example.com/a.png", alt: "", title: "" },
    ]);
  });
});
