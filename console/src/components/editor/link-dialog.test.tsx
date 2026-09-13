import { LinkDialog, normalizeLinkHref } from "@/components/editor/link-dialog";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

/**
 * 链接弹窗的回归测试。
 *
 * 地址规范化是纯函数，单测把放行与拒绝的边界钉死 —— 这是安全相关的一条：
 * javascript: 与 data: 能在访客（可能是管理员）的浏览器里执行脚本。
 *
 * 弹窗部分只验三件过去 `window.prompt` 做不到的事：
 * 错误内联且不清空已填内容、没有选区时能连显示文本一起填、可以移除链接。
 */

describe("normalizeLinkHref", () => {
  it("放行 http(s)、站内路径、锚点与 mailto", () => {
    const cases = [
      "https://example.com",
      "http://example.com/a?b=1#c",
      "/about",
      "/uploads/a.png",
      "#section",
      "?page=2",
      "mailto:a@example.com",
    ];
    for (const input of cases) {
      expect(normalizeLinkHref(input)).toEqual({ ok: true, href: input });
    }
  });

  it("裸域名补上 https://", () => {
    expect(normalizeLinkHref("example.com")).toEqual({
      ok: true,
      href: "https://example.com",
    });
    expect(normalizeLinkHref("  sub.example.com/a  ")).toEqual({
      ok: true,
      href: "https://sub.example.com/a",
    });
  });

  it("拒绝其它协议", () => {
    for (const input of [
      "javascript:alert(1)",
      "JavaScript:alert(1)",
      "data:text/html,<script>1</script>",
      "ftp://example.com",
      "vbscript:msgbox",
    ]) {
      const result = normalizeLinkHref(input);
      expect(result.ok).toBe(false);
    }
  });

  it("拒绝空串、带空格与只有协议的输入", () => {
    expect(normalizeLinkHref("   ").ok).toBe(false);
    expect(normalizeLinkHref("https://a b.com").ok).toBe(false);
    expect(normalizeLinkHref("https://").ok).toBe(false);
    expect(normalizeLinkHref("mailto:").ok).toBe(false);
    expect(normalizeLinkHref("随便一句话").ok).toBe(false);
  });
});

describe("链接弹窗", () => {
  it("地址不合法时内联报错，已填内容不丢", () => {
    const onSubmit = vi.fn();
    render(
      <LinkDialog
        open
        onOpenChange={() => {}}
        initial={{ href: "", newWindow: false }}
        needsText={false}
        onSubmit={onSubmit}
      />,
    );

    const field = screen.getByLabelText("链接地址");
    fireEvent.change(field, { target: { value: "javascript:alert(1)" } });
    fireEvent.click(screen.getByRole("button", { name: "确定" }));

    expect(onSubmit).not.toHaveBeenCalled();
    expect(screen.getByRole("alert")).toBeInTheDocument();
    // prompt 的老毛病：校验失败就得从头再填一遍
    expect(field).toHaveValue("javascript:alert(1)");
  });

  it("没有选区时连显示文本一起填，留空则用地址", () => {
    const onSubmit = vi.fn();
    render(
      <LinkDialog
        open
        onOpenChange={() => {}}
        initial={{ href: "", newWindow: false }}
        needsText
        onSubmit={onSubmit}
      />,
    );

    fireEvent.change(screen.getByLabelText("链接地址"), {
      target: { value: "example.com" },
    });
    // 整行可点，读屏读到的名字里也带着说明那一句
    fireEvent.click(screen.getByRole("checkbox", { name: /在新窗口打开/ }));
    fireEvent.click(screen.getByRole("button", { name: "确定" }));

    expect(onSubmit).toHaveBeenCalledWith({
      href: "https://example.com",
      text: "https://example.com",
      newWindow: true,
    });
  });

  it("编辑已有链接时可以移除", () => {
    const onRemove = vi.fn();
    render(
      <LinkDialog
        open
        onOpenChange={() => {}}
        initial={{ href: "https://example.com", newWindow: true }}
        needsText={false}
        onSubmit={() => {}}
        onRemove={onRemove}
      />,
    );

    expect(screen.getByText("编辑链接")).toBeInTheDocument();
    expect(screen.getByLabelText("链接地址")).toHaveValue(
      "https://example.com",
    );
    // 有选区时不该出现显示文本那一栏
    expect(screen.queryByLabelText("显示文本")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "移除链接" }));
    expect(onRemove).toHaveBeenCalled();
  });
});
