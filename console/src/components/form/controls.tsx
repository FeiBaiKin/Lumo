import {
  type FieldSchema,
  type GroupSchema,
  type WidgetKind,
  labelFor,
  optionsFor,
  widgetFor,
} from "@/components/form/schema";
import { Button } from "@/components/ui/button";
import { Inset } from "@/components/ui/card";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
} from "@/components/ui/field";
import { Input, Textarea } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Switch } from "@/components/ui/toggle";
import { cn } from "@/lib/utils";
import { ImageOff, Plus, X } from "lucide-react";
import { useEffect, useState } from "react";

/**
 * 通用表单的控件渲染。
 *
 * 每个控件只负责「把值画出来、把改动报上去」，不持有状态也不做校验 ——
 * 状态与校验在 `schema-form.tsx` 里统一处理。这样新增一个 widget
 * 只需要在这一个文件里加一个分支，字段路径、错误显示、无障碍关联全都不用重写。
 *
 * 布局上有一条固定规则：**布尔字段与普通字段分行不同**。
 * switch 自带标签且说明文字在左、开关在右；其余控件的标签在上、控件在下。
 * 混排会让设置页出现锯齿状的视觉节奏。
 */

export type ControlProps = {
  /** 字段路径，嵌套时形如 `parent.child`。用作 id 与错误关联的基础。 */
  path: string;
  schema: FieldSchema;
  value: unknown;
  error: string | undefined;
  disabled: boolean;
  onChange: (value: unknown) => void;
  /** 失焦时上报，用于「失焦即校验」。 */
  onBlur: () => void;
};

/** 由字段路径生成稳定 id。路径里可能有 `.`，而 id 里用 `.` 在 CSS 选择器里要转义。 */
export function fieldId(path: string): string {
  return `field-${path.replace(/\./g, "-")}`;
}

/**
 * 字段级错误行。
 *
 * `role="alert"` 由 FieldError 内部给出，`id` 与控件的 `aria-describedby` 对应 ——
 * 两者缺一，错误就只是「一段红字」，读屏用户听不到。
 */
function ErrorLine({
  path,
  error,
}: { path: string; error: string | undefined }) {
  return <FieldError id={`${fieldId(path)}-error`}>{error}</FieldError>;
}

/**
 * 文本类控件（text / textarea / code）共用的属性。
 *
 * 集中生成是为了保证「有错误」与「错误被关联」这两件事在每种控件上都被做到 ——
 * 分散在各分支里写，迟早会漏掉一个。
 */
function textProps(
  path: string,
  schema: FieldSchema,
  error: string | undefined,
) {
  return {
    id: fieldId(path),
    maxLength: schema.maxLength,
    placeholder: schema["x-placeholder"],
    "aria-invalid": error ? (true as const) : undefined,
    "aria-describedby": error ? `${fieldId(path)}-error` : undefined,
  };
}

function TextControl({
  path,
  schema,
  value,
  error,
  disabled,
  onChange,
  onBlur,
}: ControlProps) {
  return (
    <Input
      {...textProps(path, schema, error)}
      value={String(value ?? "")}
      disabled={disabled}
      onChange={(e) => onChange(e.target.value)}
      onBlur={onBlur}
    />
  );
}

function TextareaControl({
  path,
  schema,
  value,
  error,
  disabled,
  onChange,
  onBlur,
}: ControlProps) {
  const rows = schema["x-rows"] ?? 4;
  return (
    <Textarea
      {...textProps(path, schema, error)}
      rows={rows}
      value={String(value ?? "")}
      disabled={disabled}
      onChange={(e) => onChange(e.target.value)}
      onBlur={onBlur}
    />
  );
}

/** 代码/配置文本：等宽 + 不换行折行策略按内容。用于 robots.txt 附加内容这类字段。 */
function CodeControl({
  path,
  schema,
  value,
  error,
  disabled,
  onChange,
  onBlur,
}: ControlProps) {
  const rows = schema["x-rows"] ?? 8;
  return (
    <Textarea
      {...textProps(path, schema, error)}
      rows={rows}
      value={String(value ?? "")}
      disabled={disabled}
      onChange={(e) => onChange(e.target.value)}
      onBlur={onBlur}
      spellCheck={false}
      className="font-mono text-sm"
    />
  );
}

function NumberControl({
  path,
  schema,
  value,
  error,
  disabled,
  onChange,
  onBlur,
}: ControlProps) {
  const unit = schema["x-unit"];
  return (
    <div className="flex items-center gap-2">
      <Input
        {...textProps(path, schema, error)}
        type="number"
        inputMode={schema.type === "integer" ? "numeric" : "decimal"}
        min={schema.minimum}
        max={schema.maximum}
        step={schema.type === "integer" ? 1 : "any"}
        value={value === undefined || value === null ? "" : String(value)}
        disabled={disabled}
        // 空串必须变成 undefined 而不是 0：留空表示「不设置」，
        // 变成 0 会让「端口留空」被存成 0 然后在服务端报范围错误。
        onChange={(e) =>
          onChange(e.target.value === "" ? undefined : Number(e.target.value))
        }
        onBlur={onBlur}
        className="max-w-48"
      />
      {unit ? <span className="text-sm text-ink-muted">{unit}</span> : null}
    </div>
  );
}

function SelectControl({
  path,
  schema,
  value,
  error,
  disabled,
  onChange,
}: ControlProps) {
  const options = optionsFor(schema);
  const current = String(value ?? "");

  // 当前值不在枚举里（例如 Schema 收窄过、库里还留着旧值）时，
  // 顶部补一个「当前值」项让用户看得见它，而不是显示成空白。
  const known = options.some((option) => option.value === current);

  return (
    <Select
      value={current}
      disabled={disabled}
      onValueChange={(next) => onChange(next)}
    >
      <SelectTrigger
        id={fieldId(path)}
        aria-invalid={error ? true : undefined}
        aria-describedby={error ? `${fieldId(path)}-error` : undefined}
        className="max-w-md"
      >
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {!known && current ? (
          <SelectItem value={current}>
            {current}（当前值，已不在选项中）
          </SelectItem>
        ) : null}
        {options.map((option) => (
          <SelectItem key={option.value} value={option.value}>
            {option.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

/**
 * 颜色。取色器 + 十六进制文本框两个入口。
 *
 * 与标签页同一个理由：站点常要沿用品牌色，而品牌色是别人给的一串十六进制，
 * 从取色器里挑一个「差不多的」永远对不上。反过来，只想微调时又不想手敲。
 */
function ColorControl({
  path,
  schema,
  value,
  error,
  disabled,
  onChange,
  onBlur,
}: ControlProps) {
  const current = String(value ?? "");
  // 取色器只认 #RRGGBB。值不合法时给一个中性色，避免它显示成黑色而误导
  const pickerValue = /^#[0-9a-fA-F]{6}$/.test(current) ? current : "#000000";
  return (
    <div className="flex items-center gap-2">
      <input
        type="color"
        value={pickerValue}
        disabled={disabled}
        onChange={(e) => onChange(e.target.value)}
        aria-label={`${labelFor(schema, path)}，选择颜色`}
        className="h-9 w-12 shrink-0 cursor-pointer rounded-control border border-line-strong bg-surface p-1 disabled:cursor-not-allowed"
      />
      <Input
        {...textProps(path, schema, error)}
        value={current}
        disabled={disabled}
        onChange={(e) => onChange(e.target.value)}
        onBlur={onBlur}
        placeholder="#1B3A6B"
        className="max-w-40"
      />
    </div>
  );
}

/**
 * 图片地址。
 *
 * 目前是「填地址 + 即时预览」。阶段 7 后续接入附件库后，
 * 这里会多一个「从附件库选择」的入口 —— 但地址输入要保留：
 * 外链图片（CDN、图床）是常见需求，只给选择器等于砍掉一半用法。
 */
function ImageControl({
  path,
  schema,
  value,
  error,
  disabled,
  onChange,
  onBlur,
}: ControlProps) {
  const current = String(value ?? "");
  const [broken, setBroken] = useState(false);

  // 地址变了就重置「加载失败」状态，否则换成一个能用的地址后预览仍是破的
  // biome-ignore lint/correctness/useExhaustiveDependencies: 仅在地址变化时重置
  useEffect(() => {
    setBroken(false);
  }, [current]);

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-start gap-3">
        {/* 预览框固定尺寸：图片加载前后布局不跳 */}
        <div
          className={cn(
            "flex size-16 shrink-0 items-center justify-center overflow-hidden",
            "rounded-control border border-line bg-surface-raised",
          )}
        >
          {current && !broken ? (
            <img
              src={current}
              alt=""
              className="size-full object-contain"
              onError={() => setBroken(true)}
            />
          ) : (
            <ImageOff aria-hidden="true" className="size-5 text-ink-subtle" />
          )}
        </div>
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <Input
            {...textProps(path, schema, error)}
            value={current}
            disabled={disabled}
            onChange={(e) => onChange(e.target.value)}
            onBlur={onBlur}
            placeholder="https://… 或 /uploads/…"
          />
          {broken ? (
            <p className="text-xs text-warn">
              这个地址没能加载出图片，请检查是否可公开访问。
            </p>
          ) : null}
        </div>
      </div>
    </div>
  );
}

function SwitchControl({
  path,
  schema,
  value,
  error,
  disabled,
  onChange,
}: ControlProps) {
  const label = labelFor(schema, path);
  return (
    <div className="flex items-start justify-between gap-4 py-1">
      <div className="flex min-w-0 flex-col gap-0.5">
        <label
          htmlFor={fieldId(path)}
          className="text-base font-medium text-ink select-none"
        >
          {label}
        </label>
        {schema.description ? (
          <p className="text-xs text-ink-muted">{schema.description}</p>
        ) : null}
        <ErrorLine path={path} error={error} />
      </div>
      <Switch
        id={fieldId(path)}
        checked={Boolean(value)}
        disabled={disabled}
        onCheckedChange={(checked) => onChange(checked)}
        className="mt-0.5"
        aria-describedby={error ? `${fieldId(path)}-error` : undefined}
      />
    </div>
  );
}

/** 字符串数组：一行一项，可增删。用于标签式的简单列表。 */
function ListControl({ path, value, error, disabled, onChange }: ControlProps) {
  const items = Array.isArray(value) ? (value as unknown[]) : [];
  const update = (next: unknown[]) => onChange(next);

  return (
    <div className="flex flex-col gap-2">
      {items.map((item, index) => (
        /*
         * 这里用下标作 key 是安全的：每个条目都是一个**完全受控**的输入框，
         * 不持有自己的状态。删掉中间一项后，剩余条目会按新下标重新拿到正确的值。
         * 若哪天条目里出现了带内部状态的组件（如带本地草稿的富文本），
         * 就必须换成稳定 id —— 那时下标会让状态错位到相邻条目上。
         */
        // biome-ignore lint/suspicious/noArrayIndexKey: 完全受控的输入框，见上方说明
        <div key={index} className="flex items-center gap-2">
          <Input
            value={String(item ?? "")}
            disabled={disabled}
            onChange={(e) => {
              const next = [...items];
              next[index] = e.target.value;
              update(next);
            }}
            aria-label={`第 ${index + 1} 项`}
          />
          <Button
            variant="ghost"
            size="icon-sm"
            disabled={disabled}
            onClick={() => update(items.filter((_, i) => i !== index))}
            aria-label={`删除第 ${index + 1} 项`}
            className="hover:text-danger"
          >
            <X aria-hidden="true" />
          </Button>
        </div>
      ))}
      <Button
        variant="secondary"
        size="sm"
        disabled={disabled}
        onClick={() => update([...items, ""])}
        className="self-start"
      >
        <Plus aria-hidden="true" />
        添加一项
      </Button>
      <ErrorLine path={path} error={error} />
    </div>
  );
}

/**
 * 对象数组：每个条目是一个嵌套表单。
 *
 * 用于「一组结构相同的配置」，如多条转发规则。条目名与字段说明由
 * `x-item-label` 与 `items.properties` 提供，引擎本身不认识任何具体字段。
 */
function RepeaterControl({
  path,
  schema,
  value,
  error,
  disabled,
  onChange,
  renderGroup,
}: ControlProps & { renderGroup: RenderGroup }) {
  const items = Array.isArray(value) ? (value as unknown[]) : [];
  const itemSchema = (schema.items ?? { type: "object" }) as GroupSchema;
  const itemLabel = schema["x-item-label"] ?? "一项";
  const update = (next: unknown[]) => onChange(next);

  return (
    <div className="flex flex-col gap-3">
      {items.map((item, index) => {
        const values =
          item && typeof item === "object" && !Array.isArray(item)
            ? (item as Record<string, unknown>)
            : {};
        return (
          <Inset
            /*
             * 下标作 key 在这里是安全的：条目是纯数据对象（服务端存的是普通 JSON，
             * 没有 id），而每个子控件都是完全受控的。加一个合成 id 反而会破坏提交 ——
             * 服务端按 additionalProperties: false 校验，多一个键就整组存不进去。
             */
            // biome-ignore lint/suspicious/noArrayIndexKey: 纯数据对象，无稳定标识可用
            key={index}
          >
            <div className="mb-2 flex items-center justify-between gap-2">
              <span className="text-sm font-medium text-ink">
                {itemLabel} {index + 1}
              </span>
              <Button
                variant="ghost"
                size="icon-sm"
                disabled={disabled}
                onClick={() => update(items.filter((_, i) => i !== index))}
                aria-label={`删除${itemLabel} ${index + 1}`}
                className="hover:text-danger"
              >
                <X aria-hidden="true" />
              </Button>
            </div>
            {renderGroup({
              basePath: `${path}.${index}`,
              schema: itemSchema,
              values,
              disabled,
              onChange: (nextValues) => {
                const next = [...items];
                next[index] = nextValues;
                update(next);
              },
            })}
          </Inset>
        );
      })}
      <Button
        variant="secondary"
        size="sm"
        disabled={disabled}
        onClick={() => update([...items, {}])}
        className="self-start"
      >
        <Plus aria-hidden="true" />
        添加{itemLabel}
      </Button>
      <ErrorLine path={path} error={error} />
    </div>
  );
}

/** 渲染一组字段的回调。嵌套对象与重复条目都需要它，故由上层注入以免循环依赖。 */
export type RenderGroup = (props: {
  basePath: string;
  schema: GroupSchema;
  values: Record<string, unknown>;
  disabled: boolean;
  onChange: (values: Record<string, unknown>) => void;
}) => React.ReactNode;

/**
 * 单个字段。
 *
 * 按 widget 分派到具体控件，并统一套上「标签 / 说明 / 控件 / 错误」四件套。
 * switch 例外：它的标签与开关同行，由 SwitchControl 自己画。
 */
export function SchemaField({
  path,
  schema,
  value,
  error,
  disabled,
  onChange,
  onBlur,
  renderGroup,
}: ControlProps & { renderGroup: RenderGroup }) {
  const widget: WidgetKind = widgetFor(schema);
  const label = labelFor(schema, path);
  const id = fieldId(path);

  const control: ControlProps = {
    path,
    schema,
    value,
    error,
    disabled,
    onChange,
    onBlur,
  };

  // 布尔字段自成一行，标签在左、开关在右
  if (widget === "switch") {
    return <SwitchControl {...control} />;
  }

  return (
    <Field>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      {schema.description ? (
        <FieldDescription>{schema.description}</FieldDescription>
      ) : null}
      {renderControl(widget, control, renderGroup)}
      {/* 复合控件（列表/重复/嵌套）自己负责显示错误，其余在这里统一显示 */}
      {widget !== "list" && widget !== "repeater" && widget !== "group" ? (
        <ErrorLine path={path} error={error} />
      ) : null}
    </Field>
  );
}

function renderControl(
  widget: WidgetKind,
  control: ControlProps,
  renderGroup: RenderGroup,
): React.ReactNode {
  switch (widget) {
    case "textarea":
      return <TextareaControl {...control} />;
    case "code":
      return <CodeControl {...control} />;
    case "number":
      return <NumberControl {...control} />;
    case "select":
      return <SelectControl {...control} />;
    case "color":
      return <ColorControl {...control} />;
    case "image":
      return <ImageControl {...control} />;
    case "list":
      return <ListControl {...control} />;
    case "repeater":
      return <RepeaterControl {...control} renderGroup={renderGroup} />;
    case "group": {
      const values =
        control.value &&
        typeof control.value === "object" &&
        !Array.isArray(control.value)
          ? (control.value as Record<string, unknown>)
          : {};
      return (
        <Inset>
          {renderGroup({
            basePath: control.path,
            schema: (control.schema.properties
              ? control.schema
              : {}) as GroupSchema,
            values,
            disabled: control.disabled,
            onChange: control.onChange,
          })}
        </Inset>
      );
    }
    default:
      return <TextControl {...control} />;
  }
}
