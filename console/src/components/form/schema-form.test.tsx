import type { GroupSchema } from "@/components/form/schema";
import { SchemaForm } from "@/components/form/schema-form";
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

/**
 * 表单引擎的渲染测试。
 *
 * 上面那些纯函数（isVisible / sectionsOf / validateGroup）已有单测，
 * 但它们绿着而页面仍然不对是可能的——真正要盯的是**字段有没有被画出来**：
 * 分节标题在不在、条件不成立的字段是否真的没渲染、隐藏的必填项会不会拦住提交。
 */

/** 一份带分段与条件依赖的声明，形态照抄 media 的 storage 分组。 */
const schema: GroupSchema = {
  type: "object",
  properties: {
    driver: {
      type: "string",
      title: "存储驱动",
      enum: ["local", "s3"],
      enumNames: ["本地", "S3 兼容对象存储"],
    },
    s3Endpoint: {
      type: "string",
      title: "服务地址",
      "x-show-if": { field: "driver", op: "eq", value: "s3" },
    },
    s3Bucket: {
      type: "string",
      title: "存储桶",
      "x-show-if": { field: "driver", op: "eq", value: "s3" },
    },
  },
  required: ["driver", "s3Endpoint", "s3Bucket"],
  "x-sections": [
    { title: "存储位置", description: "附件放在哪里", fields: ["driver"] },
    { title: "S3 兼容对象存储", fields: ["s3Endpoint", "s3Bucket"] },
  ],
};

function renderForm(values: Record<string, unknown>) {
  const onSubmit = vi.fn(async () => {});
  render(
    <SchemaForm
      schema={schema}
      values={values}
      onSubmit={onSubmit}
      disableWhenPristine={false}
    />,
  );
  return { onSubmit };
}

describe("SchemaForm 分段渲染", () => {
  it("画出分段标题与说明", () => {
    renderForm({ driver: "local" });
    expect(
      screen.getByRole("heading", { name: "存储位置" }),
    ).toBeInTheDocument();
    expect(screen.getByText("附件放在哪里")).toBeInTheDocument();
  });

  it("条件不成立的分段整段隐藏，连标题也不留", () => {
    renderForm({ driver: "local" });
    // 只留一个空标题的分段是多余的：它没有任何可操作的东西，
    // 却占掉版面并让人以为自己看漏了什么。
    expect(
      screen.queryByRole("heading", { name: "S3 兼容对象存储" }),
    ).not.toBeInTheDocument();
    expect(screen.queryByLabelText("服务地址")).not.toBeInTheDocument();
    // 另一个分段不受影响
    expect(
      screen.getByRole("heading", { name: "存储位置" }),
    ).toBeInTheDocument();
  });

  it("条件成立时字段出现", () => {
    renderForm({ driver: "s3", s3Endpoint: "", s3Bucket: "" });
    expect(screen.getByLabelText("服务地址")).toBeInTheDocument();
    expect(screen.getByLabelText("存储桶")).toBeInTheDocument();
  });
});

describe("SchemaForm 条件必填", () => {
  it("隐藏的必填项不拦提交", async () => {
    // 这是条件依赖要消灭的那个问题：关掉某个开关后设置反而存不进去。
    const { onSubmit } = renderForm({ driver: "local" });
    fireEvent.click(screen.getByRole("button", { name: "保存" }));
    // onSubmit 是异步的，等待它落地再断言：直接断言会撞上 React 的 act 警告，
    // 而且那 warning 出现的时机正是提交还没跑完的时候。
    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1));
  });

  it("条件成立却空着时拦住提交，并逐条列在摘要里", () => {
    const { onSubmit } = renderForm({
      driver: "s3",
      s3Endpoint: "",
      s3Bucket: "",
    });
    fireEvent.click(screen.getByRole("button", { name: "保存" }));

    expect(onSubmit).not.toHaveBeenCalled();
    // 摘要要能定位到具体字段，否则用户只知道「有错」却不知道错在哪。
    // 按标题文字定位而不是 getByRole("alert")：逐字段的错误行也是 alert，
    // 直接查角色会撞上多个。
    const summary = screen.getByText(/处需要修改/).closest('[role="alert"]');
    expect(summary).not.toBeNull();
    expect(
      within(summary as HTMLElement).getByText(/服务地址/),
    ).toBeInTheDocument();
    expect(
      within(summary as HTMLElement).getByText(/存储桶/),
    ).toBeInTheDocument();
  });
});
