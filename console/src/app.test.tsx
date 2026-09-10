import { App } from "@/app";
import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

// 阶段 0 的冒烟测试：确认外壳可渲染，且健康检查两种结果都被正确呈现。
describe("App", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("渲染标题", () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(() => new Promise(() => {})),
    );
    render(<App />);
    expect(
      screen.getByRole("heading", { name: "Lumo Console" }),
    ).toBeInTheDocument();
  });

  it("后端连通时展示版本", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(() =>
        Promise.resolve({
          ok: true,
          json: () => Promise.resolve({ status: "ok", version: "1.2.3" }),
        }),
      ),
    );
    render(<App />);
    expect(await screen.findByText(/版本 1\.2\.3/)).toBeInTheDocument();
  });

  it("后端不可用时展示错误", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(() => Promise.resolve({ ok: false, status: 503 })),
    );
    render(<App />);
    expect(await screen.findByText(/后端未连通/)).toBeInTheDocument();
  });
});
