import {
  errorsFromServer,
  initialValues,
  optionsFor,
  validateField,
  widgetFor,
} from "@/components/form/schema";
import {
  type GroupSchema,
  isVisible,
  sectionsOf,
  validateGroup,
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
    expect(widgetFor({ type: "integer", "x-widget": "spinner" })).toBe(
      "number",
    );
    expect(widgetFor({ type: "boolean", "x-widget": "checkbox" })).toBe(
      "switch",
    );
  });

  it("支持全部已登记的 widget", () => {
    // 这份清单必须与 Go 侧 internal/form 的 Widget 常量一致，
    // 由 cmd/lumo 的契约测试盯着（它直接读本文件的 WIDGETS 集合）。
    // 这里再断言一次，是为了让「加控件忘了接线」在前端单测就暴露。
    const stringWidgets = [
      "text",
      "textarea",
      "code",
      "select",
      "radio",
      "color",
      "image",
      "date",
      "icon",
    ];
    for (const widget of stringWidgets) {
      expect(widgetFor({ type: "string", "x-widget": widget })).toBe(widget);
    }
    expect(widgetFor({ type: "integer", "x-widget": "slider" })).toBe("slider");
    expect(widgetFor({ type: "array", "x-widget": "multiselect" })).toBe(
      "multiselect",
    );
    expect(widgetFor({ type: "array", "x-widget": "images" })).toBe("images");
    expect(widgetFor({ type: "array", "x-widget": "list" })).toBe("list");
    expect(widgetFor({ type: "array", "x-widget": "repeater" })).toBe(
      "repeater",
    );
    expect(widgetFor({ type: "object", "x-widget": "group" })).toBe("group");
    expect(widgetFor({ type: "boolean", "x-widget": "switch" })).toBe("switch");
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

describe("isVisible 条件依赖", () => {
  it("没有条件时永远显示", () => {
    expect(isVisible(undefined, {})).toBe(true);
    expect(isVisible([], {})).toBe(true);
  });

  it("eq 比较，且容得下数字的类型差异", () => {
    expect(
      isVisible({ field: "driver", op: "eq", value: "s3" }, { driver: "s3" }),
    ).toBe(true);
    expect(
      isVisible(
        { field: "driver", op: "eq", value: "s3" },
        { driver: "local" },
      ),
    ).toBe(false);
    // 声明里写的是数字，值经 JSON 往返后可能变成字符串 —— 不宽容的话，
    // 条件永远不成立、字段永远不显示，而界面上没有任何报错
    expect(isVisible({ field: "n", op: "eq", value: 10 }, { n: "10" })).toBe(
      true,
    );
    expect(
      isVisible({ field: "on", op: "eq", value: true }, { on: true }),
    ).toBe(true);
  });

  it("ne / in", () => {
    expect(isVisible({ field: "a", op: "ne", value: "x" }, { a: "y" })).toBe(
      true,
    );
    expect(
      isVisible({ field: "a", op: "in", value: ["x", "y"] }, { a: "y" }),
    ).toBe(true);
    expect(
      isVisible({ field: "a", op: "in", value: ["x", "y"] }, { a: "z" }),
    ).toBe(false);
  });

  it("empty 把空串、空数组、空对象都算作空，但 0 与 false 不算", () => {
    for (const value of [undefined, null, "", [], {}]) {
      expect(isVisible({ field: "a", op: "empty" }, { a: value })).toBe(true);
    }
    for (const value of [0, false, "x", [1]]) {
      expect(isVisible({ field: "a", op: "empty" }, { a: value })).toBe(false);
    }
  });

  it("数组形式是「与」", () => {
    const conds = [
      { field: "enabled", op: "eq" as const, value: true },
      { field: "mode", op: "eq" as const, value: "advanced" },
    ];
    expect(isVisible(conds, { enabled: true, mode: "advanced" })).toBe(true);
    expect(isVisible(conds, { enabled: true, mode: "simple" })).toBe(false);
  });

  it("认不出的运算符按不成立处理，与 Go 侧一致", () => {
    // 默认显示更危险：一个被误认为可见的字段会参与必填校验，拦住一次本该成功的保存
    expect(
      isVisible({ field: "a", op: "gt" as never, value: 1 }, { a: 5 }),
    ).toBe(false);
  });
});

describe("validateGroup 跳过隐藏字段", () => {
  const schema: GroupSchema = {
    type: "object",
    properties: {
      driver: { type: "string", title: "驱动", enum: ["local", "s3"] },
      s3Endpoint: {
        type: "string",
        title: "服务地址",
        "x-show-if": { field: "driver", op: "eq", value: "s3" },
      },
    },
    required: ["driver", "s3Endpoint"],
  };

  it("条件不成立的字段不参与校验", () => {
    // 「关掉某个开关后设置反而存不进去」正是条件依赖要消灭的东西
    const errors = validateGroup(schema, { driver: "local" });
    expect(errors.s3Endpoint).toBeUndefined();
  });

  it("条件成立时才追究", () => {
    const errors = validateGroup(schema, { driver: "s3", s3Endpoint: "" });
    expect(errors.s3Endpoint).toBeDefined();
  });
});

describe("sectionsOf 分节", () => {
  it("没有 x-sections 时给一个无标题的单段", () => {
    const sections = sectionsOf({
      type: "object",
      properties: { a: { type: "string" }, b: { type: "string" } },
    });
    expect(sections).toHaveLength(1);
    expect(sections[0]?.title).toBe("");
    expect(sections[0]?.fields.map(([key]) => key)).toEqual(["a", "b"]);
  });

  it("按 x-sections 的声明顺序切分", () => {
    const sections = sectionsOf({
      type: "object",
      properties: {
        a: { type: "string" },
        b: { type: "string" },
        c: { type: "string" },
      },
      "x-sections": [
        { title: "第二", fields: ["b"] },
        { title: "第一", fields: ["a"] },
      ],
    });
    expect(sections.map((section) => section.title)).toEqual([
      "第二",
      "第一",
      "",
    ]);
    // 没被任何一段收下的字段补在末尾：漏更新分段时，少一个字段比顺序难看严重得多
    expect(sections[2]?.fields.map(([key]) => key)).toEqual(["c"]);
  });

  it("分段引用了不存在的字段时跳过，而不是整页打不开", () => {
    const sections = sectionsOf({
      type: "object",
      properties: { a: { type: "string" } },
      "x-sections": [{ title: "S", fields: ["a", "ghost"] }],
    });
    expect(sections[0]?.fields.map(([key]) => key)).toEqual(["a"]);
  });
});
