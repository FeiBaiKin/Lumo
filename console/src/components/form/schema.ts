/**
 * 声明式设置的 JSON Schema 子集解析与校验（agent.md §5）。
 *
 * 这是通用表单引擎的「大脑」：把后端给出的 Schema 片段翻译成
 * 「渲染哪些控件、每个控件什么形态、什么值算合法」。
 * 站点设置与主题设置走的是同一份 Schema 格式、同一个引擎 ——
 * 这正是 §5 定这套格式的全部理由，两处各写一套就等于没定。
 *
 * 只实现对设置表单有意义的关键字。JSON Schema 是个大标准，
 * 但设置表单只需要「勾选、填字、填数、选一个、嵌套一层」这五件事。
 */

/** Schema 里出现的类型。设置表单用不到 null 与联合类型。 */
export type SchemaType =
  | "string"
  | "integer"
  | "number"
  | "boolean"
  | "object"
  | "array";

/** 控件的具体形态。由 `x-widget` 指定，缺省时按类型推断。 */
export type WidgetKind =
  | "text"
  | "textarea"
  | "code"
  | "select"
  | "color"
  | "image"
  | "number"
  | "switch"
  | "repeater"
  | "list"
  | "group";

export type FieldSchema = {
  type?: SchemaType;
  title?: string;
  description?: string;
  default?: unknown;
  /** 枚举值。有它就渲染成下拉（除非 x-widget 另有指定）。 */
  enum?: unknown[];
  /** 枚举项的中文名，与 enum 一一对应。必须显式给出 —— 后端不提供。 */
  enumNames?: string[];
  minimum?: number;
  maximum?: number;
  minLength?: number;
  maxLength?: number;
  pattern?: string;
  /** 数组元素的 Schema，repeater 与 list 用。 */
  items?: FieldSchema;
  /** 嵌套对象的字段。 */
  properties?: Record<string, FieldSchema>;
  /** 嵌套对象的必填项。 */
  required?: string[];
  "x-widget"?: string;
  "x-placeholder"?: string;
  /** 文本域的可见行数。 */
  "x-rows"?: number;
  /** 数值字段后面的单位，如「秒」「条」。 */
  "x-unit"?: string;
  /** repeater 的条目名，用于「添加一项」的按钮文案。 */
  "x-item-label"?: string;
};

/** 一个分组完整的表单 Schema。 */
export type GroupSchema = FieldSchema & {
  properties?: Record<string, FieldSchema>;
  required?: string[];
};

/** 值对象。刻意用 unknown 而不是 any：这个对象来自网络，未经校验。 */
export type FormValues = Record<string, unknown>;

/** 字段路径。嵌套字段形如 `parent.child`。 */
export type FieldPath = string;

/** 校验错误：字段路径 → 文案。 */
export type FieldErrors = Record<FieldPath, string>;

/**
 * 决定用哪种控件。
 *
 * 顺序是刻意的：先看 `x-widget`（作者的显式意图），再按类型与枚举推断。
 * 反过来会让「显式声明了 x-widget: textarea 的字符串字段」被枚举规则抢走。
 *
 * 推断兜底是必须的：主题作者漏写一个 x-widget 时，
 * 正确的表现是「得到一个合理的默认控件」，而不是「表单少一个字段」或直接崩掉。
 */
export function widgetFor(schema: FieldSchema): WidgetKind {
  const explicit = schema["x-widget"];
  if (explicit) {
    // 未知的 x-widget 不报错也不渲染成空白：退回按类型推断。
    // 主题作者写错一个词，不该让整个设置页打不开。
    const known = WIDGETS.has(explicit);
    if (known) {
      return explicit as WidgetKind;
    }
  }

  if (schema.type === "boolean") {
    return "switch";
  }
  if (schema.enum && schema.enum.length > 0) {
    return "select";
  }
  if (schema.type === "integer" || schema.type === "number") {
    return "number";
  }
  if (schema.type === "object") {
    return "group";
  }
  if (schema.type === "array") {
    return schema.items?.type === "object" ? "repeater" : "list";
  }
  return "text";
}

const WIDGETS = new Set<string>([
  "text",
  "textarea",
  "code",
  "select",
  "color",
  "image",
  "number",
  "switch",
  "repeater",
  "list",
  "group",
]);

/** 字段的显示名。Schema 没写 title 就退回字段名 —— 不显示任何标签更糟。 */
export function labelFor(schema: FieldSchema, key: string): string {
  return schema.title?.trim() || key;
}

/** 枚举项的值与显示名。enumNames 数量不匹配时忽略它，宁可用原始值也不错位。 */
export function optionsFor(
  schema: FieldSchema,
): { value: string; label: string }[] {
  const values = schema.enum ?? [];
  const names =
    schema.enumNames && schema.enumNames.length === values.length
      ? schema.enumNames
      : null;
  return values.map((value, index) => ({
    value: String(value),
    label: names ? String(names[index]) : String(value),
  }));
}

/**
 * 按 Schema 给字段填初始值。
 *
 * 只取 Schema 声明过的字段：服务端对保存的校验是 `additionalProperties: false`，
 * 把库里可能残留的历史键原样回传会让保存直接失败。
 * 而「库里残留历史键」是真会发生的 —— 设置 Schema 会随版本演进。
 */
export function initialValues(
  schema: GroupSchema,
  current: FormValues | undefined,
): FormValues {
  const out: FormValues = {};
  for (const [key, field] of Object.entries(schema.properties ?? {})) {
    const fromServer = current?.[key];
    if (fromServer !== undefined) {
      out[key] = normalize(field, fromServer);
      continue;
    }
    out[key] = normalize(field, field.default ?? defaultFor(field));
  }
  return out;
}

/** 类型的零值。Schema 既没给 default 也没给值时用它。 */
function defaultFor(field: FieldSchema): unknown {
  switch (widgetFor(field)) {
    case "switch":
      return false;
    case "number":
      return undefined;
    case "repeater":
    case "list":
      return [];
    case "group":
      return {};
    default:
      return "";
  }
}

/**
 * 把值规整成控件期望的形态。
 *
 * 必须做这层规整：值经 JSON 往返，「整数」会变成 float64；
 * 这不是理论问题 —— 阶段 4 主题设置就因此在模板里报过
 * `expected int; got float64`。在前端做同样的规整，可以少一次「明明填的是整数却存不进去」。
 */
function normalize(field: FieldSchema, value: unknown): unknown {
  const widget = widgetFor(field);
  switch (widget) {
    case "switch":
      return Boolean(value);
    case "number": {
      if (value === "" || value === null || value === undefined) {
        return undefined;
      }
      const num = Number(value);
      return Number.isFinite(num) ? num : undefined;
    }
    case "repeater":
    case "list":
      return Array.isArray(value) ? value : [];
    case "group":
      return value && typeof value === "object" && !Array.isArray(value)
        ? value
        : {};
    default:
      if (value === null || value === undefined) {
        return "";
      }
      // 字符串字段收到数字或布尔时转成字符串：Schema 演进（enum → 自由文本）
      // 或主题作者写错类型时，表单仍能打开。
      return typeof value === "object" ? JSON.stringify(value) : String(value);
  }
}

/**
 * 校验单个字段，返回错误文案；合法时返回 undefined。
 *
 * 这是**前置提示**而不是权威判定：真正的校验在服务端（JSON Schema 校验器 + Go 侧 Check）。
 * 前端这一层存在的意义只是让用户不必提交一次才知道哪里填错。
 * 因此它宁可漏报也不误报 —— 一条前端编造出来的错误会让用户去改一个本来就对的值。
 */
export function validateField(
  field: FieldSchema,
  value: unknown,
  required: boolean,
): string | undefined {
  const widget = widgetFor(field);

  // 空值先判必填，再放过 —— 否则「必填」会先被 minLength 之类的规则报成别的错
  const empty =
    value === undefined ||
    value === null ||
    value === "" ||
    (Array.isArray(value) && value.length === 0);

  if (empty) {
    if (required && widget !== "switch") {
      return `${labelFor(field, "")}不能为空`;
    }
    // 非必填的数值字段：留空即「不设置」，不是 0
    return undefined;
  }

  if (field.enum && field.enum.length > 0) {
    const allowed = field.enum.map((v) => String(v));
    if (!allowed.includes(String(value))) {
      return `只能是 ${allowed.join(" / ")} 之一`;
    }
  }

  if (
    widget === "text" ||
    widget === "textarea" ||
    widget === "code" ||
    widget === "color"
  ) {
    const text = String(value);
    if (field.minLength !== undefined && text.length < field.minLength) {
      return `至少 ${field.minLength} 个字符`;
    }
    if (field.maxLength !== undefined && text.length > field.maxLength) {
      // 超长是最常见的服务端 422，前端先说清楚能省一次往返
      return `最多 ${field.maxLength} 个字符，当前 ${text.length} 个`;
    }
    if (field.pattern) {
      try {
        if (!new RegExp(field.pattern).test(text)) {
          return "格式不符合要求";
        }
      } catch {
        // Schema 里的正则本身有问题时不要拦用户 —— 那是声明方的错
      }
    }
  }

  if (widget === "number") {
    const num = Number(value);
    if (!Number.isFinite(num)) {
      return "请填写数字";
    }
    if (field.type === "integer" && !Number.isInteger(num)) {
      return "请填写整数";
    }
    if (field.minimum !== undefined && num < field.minimum) {
      return `不能小于 ${field.minimum}`;
    }
    if (field.maximum !== undefined && num > field.maximum) {
      return `不能大于 ${field.maximum}`;
    }
  }

  return undefined;
}

/** 校验整个分组，返回全部错误。 */
export function validateGroup(
  schema: GroupSchema,
  values: FormValues,
): FieldErrors {
  const errors: FieldErrors = {};
  const required = new Set(schema.required ?? []);

  for (const [key, field] of Object.entries(schema.properties ?? {})) {
    const message = validateField(field, values[key], required.has(key));
    if (message) {
      errors[key] = message;
      continue;
    }
    // 嵌套对象：递归校验，路径用点号连起来，与服务端 errors[].location 的形态一致
    if (widgetFor(field) === "group" && field.properties) {
      const nested = values[key];
      if (nested && typeof nested === "object" && !Array.isArray(nested)) {
        const nestedErrors = validateGroup(
          field as GroupSchema,
          nested as FormValues,
        );
        for (const [subKey, subMessage] of Object.entries(nestedErrors)) {
          errors[`${key}.${subKey}`] = subMessage;
        }
      }
    }
  }

  return errors;
}

/**
 * 把服务端的 422 明细映射到字段路径。
 *
 * 服务端的 `location` 形如 `body.timezone` 或 `body.s3Endpoint`，
 * 与分组 Schema 的顶层键同名（见 internal/settings 的 Update）。
 * 认不出的位置归入 `_`，由表单顶部作为整体错误显示 ——
 * 丢掉一条服务端错误会让「保存失败但页面毫无提示」。
 */
export function errorsFromServer(
  details: { location?: string; message?: string }[] | null | undefined,
): { fields: FieldErrors; others: string[] } {
  const fields: FieldErrors = {};
  const others: string[] = [];
  for (const detail of details ?? []) {
    const message = detail.message?.trim();
    if (!message) {
      continue;
    }
    const location = detail.location?.trim() ?? "";
    if (location.startsWith("body.")) {
      const path = location.slice("body.".length);
      // 同一字段有多条时保留第一条：服务端可能同时报「必填」与「类型不对」，
      // 显示两条只会让人困惑。
      if (path && !fields[path]) {
        fields[path] = message;
        continue;
      }
    }
    if (location === "body" || !location) {
      others.push(message);
      continue;
    }
    others.push(location ? `${location}：${message}` : message);
  }
  return { fields, others };
}
