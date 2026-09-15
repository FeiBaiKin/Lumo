/**
 * 声明式设置的 JSON Schema 子集解析与校验。
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
  | "secret"
  | "select"
  | "radio"
  | "multiselect"
  | "color"
  | "image"
  | "images"
  | "date"
  | "icon"
  | "number"
  | "slider"
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
  /** 条件依赖：全部成立时字段才显示。语义见 isVisible。 */
  "x-show-if"?: ShowIfCondition[] | ShowIfCondition;
};

/**
 * 一条条件依赖。
 *
 * 与 Go 侧 form.Condition 一一对应，运算符集合必须一致——
 * 多一种运算符会让声明方以为自己写了条件，而渲染方默默按「不成立」处理。
 */
export type ShowIfCondition = {
  field: string;
  op: "eq" | "ne" | "in" | "empty" | "notEmpty";
  value?: unknown;
};

/** 一个分段：标题 + 字段名列表。分节是纯呈现，不影响值与校验。 */
export type SectionSchema = {
  title: string;
  description?: string | undefined;
  fields: string[];
};

/** 一个分组完整的表单 Schema。 */
export type GroupSchema = FieldSchema & {
  properties?: Record<string, FieldSchema>;
  required?: string[];
  /** 分段。缺省时按平铺形态渲染，与主题包声明的老 Schema 兼容。 */
  "x-sections"?: SectionSchema[];
  /**
   * 字段的渲染顺序。声明来自 YAML 时键序会丢（见 orderedFields），
   * 想控制先后就得显式写一份。
   */
  "x-order"?: string[];
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
  "secret",
  "select",
  "radio",
  "multiselect",
  "color",
  "image",
  "images",
  "date",
  "icon",
  "number",
  "slider",
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
    widget === "secret" ||
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
    // 条件不成立的字段此刻并不存在，追究它会让「关掉某个开关后设置反而存不进去」。
    // 服务端用同一套判定（Go 侧 form.Missing），两边必须一致。
    if (!isVisible(field["x-show-if"], values)) {
      continue;
    }
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

/**
 * 条件依赖的判定（对应 Go 侧 form.conditionHolds）。
 *
 * 两端必须给出同样的答案：服务端拿它决定「此刻该不该追究这个必填项」，
 * 前端拿它决定「画不画、校不校」。判错一边的表现是
 * 「页面上没有这一项，保存时却说它必填」——最难自查的一类问题。
 *
 * 取值在字段所在的那一层里找（嵌套分组里引用同层的兄弟字段是常见写法），
 * 顶层字段的所在层就是表单根。找不到时按空值处理，于是 eq 为假、empty 为真。
 */
export function isVisible(
  conds: ShowIfCondition[] | ShowIfCondition | undefined,
  scope: FormValues,
): boolean {
  if (!conds) {
    return true;
  }
  const list = Array.isArray(conds) ? conds : [conds];
  return list.every((cond) => conditionHolds(cond, scope));
}

function conditionHolds(cond: ShowIfCondition, scope: FormValues): boolean {
  const value = scope[cond.field];
  switch (cond.op) {
    case "eq":
      return looseEqual(value, cond.value);
    case "ne":
      return !looseEqual(value, cond.value);
    case "in":
      return (
        Array.isArray(cond.value) &&
        cond.value.some((item) => looseEqual(value, item))
      );
    case "empty":
      return isEmptyValue(value);
    case "notEmpty":
      return !isEmptyValue(value);
    default:
      // 认不出的运算符按「不成立」处理，与 Go 侧一致。
      // 默认显示更危险：一个被误认为可见的字段会参与必填校验，
      // 拦住一次本该成功的保存。
      return false;
  }
}

/**
 * 「空」的定义：null、undefined、空串、空数组、空对象。
 *
 * 0 与 false 都**不算**空——它们是明确的取值，不是缺席。
 * 这与 Go 侧 form.isEmptyValue 保持一致。
 */
function isEmptyValue(value: unknown): boolean {
  if (value === null || value === undefined || value === "") {
    return true;
  }
  if (Array.isArray(value)) {
    return value.length === 0;
  }
  if (typeof value === "object") {
    return Object.keys(value as object).length === 0;
  }
  return false;
}

/**
 * 宽容比较。
 *
 * 必须宽容：声明里写的是 10 与 true，而值经 JSON 往返后可能变成 "10"；
 * 而布尔开关在 unchecked 时可能压根不在对象里。直接比类型会让条件
 * 永远不成立，表现是「字段永远不显示」，界面上没有任何报错。
 */
function looseEqual(a: unknown, b: unknown): boolean {
  if (a === null || a === undefined || b === null || b === undefined) {
    return a === b || (a == null && b == null);
  }
  if (typeof a === "number" || typeof b === "number") {
    const na = Number(a);
    const nb = Number(b);
    return Number.isFinite(na) && Number.isFinite(nb) && na === nb;
  }
  if (typeof a === "boolean" || typeof b === "boolean") {
    return a === b;
  }
  return String(a) === String(b);
}

/**
 * 取一个对象的字段，按 `x-order` 给的顺序；没声明就按 properties 的键序。
 *
 * 为什么需要它：主题与插件的设置声明来自 YAML，解析成 map 之后键序就丢了
 * （Go 的 map 遍历顺序不定）。后果是「模块」这个类型选择器会排在条数、来源的后面，
 * 看上去像表单排错了——而声明方在 YAML 里明明是按顺序写的。
 * 没被 x-order 提到的字段补在末尾，与分段对孤儿字段的处理一致。
 */
export function orderedFields(schema: GroupSchema): [string, FieldSchema][] {
  const properties = schema.properties ?? {};
  const order = schema["x-order"];
  if (!order || order.length === 0) {
    return Object.entries(properties);
  }
  const covered = new Set(order);
  const entries: [string, FieldSchema][] = [];
  for (const key of order) {
    const field = properties[key];
    if (field) {
      entries.push([key, field]);
    }
  }
  for (const [key, field] of Object.entries(properties)) {
    if (!covered.has(key)) {
      entries.push([key, field]);
    }
  }
  return entries;
}

/** 渲染用的一个分段：标题、说明与它收下的字段。 */
export type RenderSection = {
  title: string;
  /** 段说明。分段没写说明时是 undefined（tsconfig 开了 exactOptionalPropertyTypes）。 */
  description?: string | undefined;
  fields: [string, FieldSchema][];
};

/**
 * 把分组切成待渲染的分段。
 *
 * 没有 x-sections 时返回一个无标题的单段，按 properties 的键序渲染——
 * 主题包声明的老 Schema 就长这样，不能因为没写分段就不显示。
 *
 * 分段里引用了不存在的字段时跳过它而不是报错：那多半是 Schema 演进时
 * 删了字段忘了改分段，此时少显示一个不存在的项，远好过整页打不开。
 */
export function sectionsOf(schema: GroupSchema): RenderSection[] {
  const properties = schema.properties ?? {};
  const declared = schema["x-sections"];

  if (!declared || declared.length === 0) {
    return [{ title: "", fields: orderedFields(schema) }];
  }

  const covered = new Set<string>();
  const out: RenderSection[] = [];
  for (const section of declared) {
    const fields: [string, FieldSchema][] = [];
    for (const key of section.fields ?? []) {
      const field = properties[key];
      if (!field || covered.has(key)) {
        continue;
      }
      covered.add(key);
      fields.push([key, field]);
    }
    out.push({
      title: section.title,
      description: section.description,
      fields,
    });
  }

  // 没被任何分段收下的字段补在末尾：声明方漏更新分段时，
  // 少一个字段比顺序难看严重得多。
  const orphans = Object.entries(properties).filter(
    ([key]) => !covered.has(key),
  );
  if (orphans.length > 0) {
    out.push({ title: "", fields: orphans });
  }
  return out;
}
