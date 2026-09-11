import {
  type GroupSchema,
  errorsFromServer,
  initialValues,
  optionsFor,
  validateField,
  validateGroup,
  widgetFor,
} from "@/components/form/schema";
import { describe, expect, it } from "vitest";

/**
 * 表单引擎的纯逻辑测试。
 *
 * 引擎的价值全在「Schema 怎么翻译成控件」与「什么值算合法」这两件事上，
 * 两者都是纯函数，正该有覆盖。控件渲染与交互由页面层的测试覆盖。
 */

describe("widgetFor 推断控件", () => {
  it("显式 x-widget 优先于类型推断", () => {
    // 字符串 + 枚举本该是 select，但作者显式要求 textarea 时以作者为准
    expect(
      widgetFor({ type: "string", enum: ["a", "b"], "x-widget": "textarea" }),
    ).toBe("textarea");
  });

  it("按类型与枚举推断", () => {
    expect(widgetFor({ type: "boolean" })).toBe("switch");
    expect(widgetFor({ type: "string", enum: ["zh-CN", "en-US"] })).toBe(
      "select",
    );
    expect(widgetFor({ type: "integer" })).toBe("number");
    expect(widgetFor({ type: "number" })).toBe("number");
    expect(widgetFor({ type: "string" })).toBe("text");
    expect(widgetFor({ type: "array", items: { type: "string" } })).toBe(
      "list",
    );
    expect(widgetFor({ type: "array", items: { type: "object" } })).toBe(
      "repeater",
    );
    expect(widgetFor({ type: "object" })).toBe("group");
  });

  it("未知的 x-widget 退回按类型推断，而不是渲染成空白", () => {
    // 主题作者写错一个词，不该让设置页少一个字段或直接打不开
    expect(widgetFor({ type: "string", "x-widget": "markdown" })).toBe("text");
    expect(widgetFor({ type: "integer", "x-widget": "slider" })).toBe("number");
    expect(widgetFor({ type: "boolean", "x-widget": "checkbox" })).toBe(
      "switch",
    );
  });

  it("支持 agent.md §5 列出的全部 widget", () => {
    for (const widget of [
      "textarea",
      "color",
      "image",
      "select",
      "switch",
      "repeater",
      "code",
    ]) {
      expect(widgetFor({ type: "string", "x-widget": widget })).toBe(widget);
    }
  });
});

describe("optionsFor 枚举选项", () => {
  it("无 enumNames 时用原始值作标签", () => {
    expect(optionsFor({ enum: ["none", "starttls", "tls"] })).toEqual([
      { value: "none", label: "none" },
      { value: "starttls", label: "starttls" },
      { value: "tls", label: "tls" },
    ]);
  });

  it("enumNames 数量匹配时用作标签", () => {
    expect(optionsFor({ enum: ["a", "b"], enumNames: ["甲", "乙"] })).toEqual([
      { value: "a", label: "甲" },
      { value: "b", label: "乙" },
    ]);
  });

  it("enumNames 数量不匹配时整组忽略，避免选项与标签错位", () => {
    expect(optionsFor({ enum: ["a", "b", "c"], enumNames: ["甲"] })).toEqual([
      { value: "a", label: "a" },
      { value: "b", label: "b" },
      { value: "c", label: "c" },
    ]);
  });

  it("非字符串枚举值转成字符串", () => {
    expect(optionsFor({ enum: [1, 2] })).toEqual([
      { value: "1", label: "1" },
      { value: "2", label: "2" },
    ]);
  });
});

describe("validateField 单字段校验", () => {
  it("必填：空值报错，switch 例外（关就是关，不是没填）", () => {
    expect(
      validateField({ title: "标题", type: "string" }, "", true),
    ).toContain("不能为空");
    expect(
      validateField({ title: "标题", type: "string" }, undefined, true),
    ).toContain("不能为空");
    expect(
      validateField({ title: "开关", type: "boolean" }, false, true),
    ).toBeUndefined();
  });

  it("非必填的空值一律放过", () => {
    expect(validateField({ type: "string" }, "", false)).toBeUndefined();
    expect(
      validateField({ type: "integer" }, undefined, false),
    ).toBeUndefined();
    expect(
      validateField({ type: "string", minLength: 5 }, "", false),
    ).toBeUndefined();
  });

  it("长度上限：报错文案带上当前长度", () => {
    // 超长是最常见的服务端 422，先说清「超了多少」能省一次往返
    const message = validateField({ maxLength: 3 }, "abcd", false);
    expect(message).toContain("最多 3");
    expect(message).toContain("当前 4");
    expect(validateField({ maxLength: 3 }, "abc", false)).toBeUndefined();
  });

  it("长度下限", () => {
    expect(validateField({ minLength: 3 }, "ab", false)).toContain("至少 3");
    expect(validateField({ minLength: 3 }, "abc", false)).toBeUndefined();
  });

  it("枚举之外的取值被拒", () => {
    expect(validateField({ enum: ["a", "b"] }, "c", false)).toContain("a / b");
    expect(validateField({ enum: ["a", "b"] }, "a", false)).toBeUndefined();
  });

  it("数值范围与整数约束", () => {
    const field = { type: "integer", minimum: 1, maximum: 65535 } as const;
    expect(validateField(field, 0, false)).toContain("不能小于 1");
    expect(validateField(field, 70000, false)).toContain("不能大于 65535");
    expect(validateField(field, 1.5, false)).toContain("整数");
    expect(validateField(field, 587, false)).toBeUndefined();
  });

  it("number 类型接受小数", () => {
    expect(validateField({ type: "number" }, 1.5, false)).toBeUndefined();
  });

  it("非数字内容被拒", () => {
    expect(validateField({ type: "integer" }, "abc", false)).toContain("数字");
  });

  it("pattern 不匹配时报格式错误", () => {
    expect(validateField({ pattern: "^\\d{4}$" }, "12", false)).toContain(
      "格式",
    );
    expect(
      validateField({ pattern: "^\\d{4}$" }, "1234", false),
    ).toBeUndefined();
  });

  it("Schema 里的正则本身非法时不拦用户", () => {
    // 那是声明方的错，不该让填表的人卡在这里
    expect(validateField({ pattern: "([" }, "任意值", false)).toBeUndefined();
  });
});

describe("validateGroup 整组校验", () => {
  const schema: GroupSchema = {
    type: "object",
    required: ["title"],
    properties: {
      title: { type: "string", title: "标题" },
      size: { type: "integer", minimum: 1, maximum: 100 },
      enabled: { type: "boolean" },
    },
  };

  it("只报出错字段，合法的字段不出现", () => {
    const errors = validateGroup(schema, {
      title: "",
      size: 200,
      enabled: true,
    });
    expect(Object.keys(errors).sort()).toEqual(["size", "title"]);
  });

  it("全部合法时返回空对象", () => {
    expect(
      validateGroup(schema, { title: "标题", size: 10, enabled: false }),
    ).toEqual({});
  });

  it("嵌套对象的错误路径用点号连起来，与服务端 location 的形态一致", () => {
    const nested: GroupSchema = {
      type: "object",
      properties: {
        smtp: {
          type: "object",
          required: ["host"],
          properties: {
            host: { type: "string", title: "服务器" },
            port: { type: "integer", maximum: 65535 },
          },
        },
      },
    };
    const errors = validateGroup(nested, { smtp: { host: "", port: 99999 } });
    expect(Object.keys(errors).sort()).toEqual(["smtp.host", "smtp.port"]);
  });
});

describe("initialValues 初始值", () => {
  it("服务端的值优先于 default", () => {
    const schema: GroupSchema = {
      type: "object",
      properties: { pageSize: { type: "integer", default: 10 } },
    };
    expect(initialValues(schema, { pageSize: 25 })).toEqual({ pageSize: 25 });
    expect(initialValues(schema, {})).toEqual({ pageSize: 10 });
    expect(initialValues(schema, undefined)).toEqual({ pageSize: 10 });
  });

  it("只取 Schema 声明过的字段", () => {
    // 服务端保存时的校验是 additionalProperties: false，
    // 把库里残留的历史键原样回传会让保存直接失败
    const schema: GroupSchema = {
      type: "object",
      properties: { title: { type: "string" } },
    };
    expect(initialValues(schema, { title: "a", legacyKey: "旧值" })).toEqual({
      title: "a",
    });
  });

  it("整数经 JSON 往返仍是整数（回归：float64 会让模板报 expected int）", () => {
    const schema: GroupSchema = {
      type: "object",
      properties: { relatedCount: { type: "integer" } },
    };
    const values = initialValues(schema, { relatedCount: 3 });
    expect(Number.isInteger(values.relatedCount)).toBe(true);
  });

  it("类型不符的值被规整到控件能用的形态", () => {
    const schema: GroupSchema = {
      type: "object",
      properties: {
        flag: { type: "boolean" },
        text: { type: "string" },
        list: { type: "array", items: { type: "string" } },
      },
    };
    const values = initialValues(schema, {
      flag: 1,
      text: 42,
      list: "不是数组",
    });
    expect(values.flag).toBe(true);
    expect(values.text).toBe("42");
    expect(values.list).toEqual([]);
  });

  it("缺省值缺失时按类型给零值（switch 给 false 而不是空串）", () => {
    const schema: GroupSchema = {
      type: "object",
      properties: {
        flag: { type: "boolean" },
        n: { type: "integer" },
        s: { type: "string" },
        arr: { type: "array", items: { type: "string" } },
      },
    };
    const values = initialValues(schema, undefined);
    expect(values.flag).toBe(false);
    expect(values.n).toBeUndefined();
    expect(values.s).toBe("");
    expect(values.arr).toEqual([]);
  });
});

describe("errorsFromServer 服务端 422 明细落回字段", () => {
  it("body.<字段> 映射到字段路径", () => {
    const { fields, others } = errorsFromServer([
      { location: "body.timezone", message: "不是有效的 IANA 时区名" },
      { location: "body.s3Endpoint", message: "选择 S3 存储时必填" },
    ]);
    expect(fields).toEqual({
      timezone: "不是有效的 IANA 时区名",
      s3Endpoint: "选择 S3 存储时必填",
    });
    expect(others).toEqual([]);
  });

  it("同一字段多条只保留第一条", () => {
    // 服务端可能同时报「必填」与「类型不对」，显示两条只会让人困惑
    const { fields } = errorsFromServer([
      { location: "body.port", message: "必填" },
      { location: "body.port", message: "类型不对" },
    ]);
    expect(fields.port).toBe("必填");
  });

  it("认不出的位置归入整体错误而不是被丢掉", () => {
    // 丢掉一条服务端错误会让「保存失败但页面毫无提示」
    const { fields, others } = errorsFromServer([
      { location: "body", message: "设置校验失败" },
      { message: "没有位置的错误" },
    ]);
    expect(fields).toEqual({});
    expect(others).toEqual(["设置校验失败", "没有位置的错误"]);
  });

  it("空消息被跳过", () => {
    expect(errorsFromServer([{ location: "body.x", message: "  " }])).toEqual({
      fields: {},
      others: [],
    });
  });

  it("未定义明细时返回空结果", () => {
    expect(errorsFromServer(undefined)).toEqual({ fields: {}, others: [] });
  });
});
