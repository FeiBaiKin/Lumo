import type { GroupSchema } from "@/components/form/schema";
import { SchemaForm } from "@/components/form/schema-form";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

/**
 * 口令字段的渲染测试。
 *
 * 要盯的是三态在界面上分不分得清：留空 = 不改动、填了 = 设为该值、清除 = 抹掉。
 * 三态里有两态的界面表现都是「一个空输入框」，而它们的后果正好相反——
 * 站长填完口令再点一次保存，会不确定自己刚才是存进去了、还是把原来那把抹了。
 * 这类「肉眼看不出来」的错只能靠测试盯住。
 */

const schema: GroupSchema = {
  type: "object",
  properties: {
    host: { type: "string", title: "服务器" },
    password: { type: "string", title: "口令", "x-widget": "secret" },
  },
  "x-sections": [{ title: "发信", fields: ["host", "password"] }],
};

function renderForm(
  values: Record<string, unknown>,
  secretSet?: readonly string[],
) {
  const onSubmit = vi.fn(async (_values: Record<string, unknown>) => {});
  render(
    <SchemaForm
      schema={schema}
      values={values}
      secretSet={secretSet}
      onSubmit={onSubmit}
      disableWhenPristine={false}
    />,
  );
  return { onSubmit };
}

describe("口令字段", () => {
  it("输入框默认是遮蔽的，可以临时显示", () => {
    renderForm({ host: "smtp.example.com", password: "" }, ["password"]);
    const input = screen.getByLabelText("口令");
    expect(input).toHaveAttribute("type", "password");

    fireEvent.click(screen.getByRole("button", { name: "显示口令" }));
    expect(screen.getByLabelText("口令")).toHaveAttribute("type", "text");
    // 再点一次要能变回去：口令框旁边那颗眼睛如果只单向生效，
    // 站长在公共场合看过一次之后就再也遮不上了。
    fireEvent.click(screen.getByRole("button", { name: "隐藏口令" }));
    expect(screen.getByLabelText("口令")).toHaveAttribute("type", "password");
  });

  it("已设置时说明现状并给出清除入口", () => {
    renderForm({ host: "smtp.example.com", password: "" }, ["password"]);
    expect(screen.getByText("已设置，留空即不改动")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "清除" })).toBeInTheDocument();
  });

  it("没设置过时不谎称已设置，也不给清除", () => {
    renderForm({ host: "smtp.example.com", password: "" });
    expect(screen.getByText("尚未设置")).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "清除" }),
    ).not.toBeInTheDocument();
  });

  it("清除不只是清空输入框，而是提交 null", async () => {
    const { onSubmit } = renderForm(
      { host: "smtp.example.com", password: "" },
      ["password"],
    );
    fireEvent.click(screen.getByRole("button", { name: "清除" }));

    // 提交前先说清后果：一个空输入框看不出「这下要删口令了」。
    expect(screen.getByText("保存后将清除已存的口令")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "保存" }));
    await waitFor(() => expect(onSubmit).toHaveBeenCalled());
    // 空串会被服务端理解成「不改动」，清除必须用 null，否则点了清除却什么也没发生。
    expect(onSubmit.mock.calls[0]?.[0]).toMatchObject({ password: null });
  });

  it("填了新口令就提交新口令", async () => {
    const { onSubmit } = renderForm(
      { host: "smtp.example.com", password: "" },
      ["password"],
    );
    fireEvent.change(screen.getByLabelText("口令"), {
      target: { value: "hunter2" },
    });
    expect(screen.getByText("保存后生效")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "保存" }));
    await waitFor(() => expect(onSubmit).toHaveBeenCalled());
    expect(onSubmit.mock.calls[0]?.[0]).toMatchObject({ password: "hunter2" });
  });
});
