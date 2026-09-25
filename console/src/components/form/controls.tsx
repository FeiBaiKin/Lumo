import { IconPicker } from "@/components/form/icon-picker";
import {
  type FieldSchema,
  type FormValues,
  type GroupSchema,
  type WidgetKind,
  isVisible,
  labelFor,
  optionsFor,
  orderedFields,
  widgetFor,
} from "@/components/form/schema";
import { MediaPickerDialog } from "@/components/media/media-picker";
import { Button } from "@/components/ui/button";
import { Inset } from "@/components/ui/card";
import {
  FieldDescription,
  FieldError,
  FieldLabel,
} from "@/components/ui/field";
import { Input, Textarea } from "@/components/ui/input";
import { RadioGroup, RadioRow } from "@/components/ui/radio-group";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Slider } from "@/components/ui/slider";
import { CheckboxRow, Switch } from "@/components/ui/toggle";
import { cn } from "@/lib/utils";
import {
  ChevronRight,
  ChevronsDownUp,
  ChevronsUpDown,
  Eye,
  EyeOff,
  GripVertical,
  ImageOff,
  ImagePlus,
  Plus,
  X,
} from "lucide-react";
import { Reorder, useDragControls } from "motion/react";
import {
  type ReactNode,
  createContext,
  useContext,
  useEffect,
  useId,
  useState,
} from "react";

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

/**
 * 「哪些口令字段已经设过值」。
 *
 * 口令本身不会回传（服务端把它抹成空串，见 internal/settings 的 Group.Mask），
 * 所以「已设置」这件事只能另行告诉界面。少了它，站长面对的是一片空白，
 * 分不出「没存过」和「存过但没显示」——而这两件事的下一步动作正好相反。
 *
 * 用 context 而不是逐层传参：字段是递归渲染的，一个只对顶层有效的参数
 * 要穿过分组渲染器一路传到底，传参的路径比它的意义还长。
 */
export const SecretSetContext = createContext<readonly string[]>([]);

/**
 * 当前要显示的字段错误（键为字段路径）。
 *
 * 重复条目要知道「我这一条里有没有错」来决定能不能收起；错误表在表单顶层，
 * 与口令集合一样用 context 递下去，免得穿过分组渲染器逐层传参。
 */
export const FieldErrorsContext = createContext<Record<string, string>>({});

/**
 * 口令 / 密钥。
 *
 * 三态与后端一一对应：空串 = 不改动，null = 清除，非空 = 设为该值。
 * 界面上必须把「现在到底是哪一态」说清楚，否则站长填完口令再点一次保存，
 * 会不确定自己刚才是存进去了、还是把原来那把抹掉了。
 */
function SecretControl({
  path,
  schema,
  value,
  error,
  disabled,
  onChange,
  onBlur,
}: ControlProps) {
  const [revealed, setRevealed] = useState(false);
  const stored = useContext(SecretSetContext).includes(path);
  const clearing = value === null;
  const text = typeof value === "string" ? value : "";

  const status = clearing
    ? "保存后将清除已存的口令"
    : stored && text === ""
      ? "已设置，留空即不改动"
      : text !== ""
        ? "保存后生效"
        : "尚未设置";

  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex items-center gap-2">
        <Input
          {...textProps(path, schema, error)}
          type={revealed ? "text" : "password"}
          value={text}
          disabled={disabled}
          // 口令框的占位提示只说状态，不写「请输入口令」——
          // 那行字在「已设置」的情形下反而会让人以为必须重新填一遍。
          placeholder={stored ? "留空即不改动" : ""}
          autoComplete="new-password"
          onChange={(e) => onChange(e.target.value)}
          onBlur={onBlur}
        />
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          disabled={disabled}
          aria-pressed={revealed}
          aria-label={revealed ? "隐藏口令" : "显示口令"}
          onClick={() => setRevealed((v) => !v)}
        >
          {revealed ? <EyeOff /> : <Eye />}
        </Button>
        {stored && !clearing ? (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            disabled={disabled}
            onClick={() => {
              onChange(null);
              onBlur();
            }}
          >
            清除
          </Button>
        ) : null}
      </div>
      <FieldDescription>{status}</FieldDescription>
    </div>
  );
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
 * 两条路并存：从附件库选，或直接填地址。地址输入不能去掉 ——
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
  const [picker, setPicker] = useState(false);

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
          <div className="flex items-center gap-2">
            <Input
              {...textProps(path, schema, error)}
              value={current}
              disabled={disabled}
              onChange={(e) => onChange(e.target.value)}
              onBlur={onBlur}
              placeholder="https://… 或 /uploads/…"
            />
            <Button
              variant="secondary"
              size="sm"
              disabled={disabled}
              onClick={() => setPicker(true)}
              className="shrink-0"
            >
              <ImagePlus aria-hidden="true" />
              从附件库选
            </Button>
          </div>
          {broken ? (
            <p className="text-xs text-warn">
              这个地址没能加载出图片，请检查是否可公开访问。
            </p>
          ) : null}
        </div>
      </div>

      <MediaPickerDialog
        open={picker}
        onOpenChange={setPicker}
        onSelect={(picks) => {
          const pick = picks[0];
          if (pick) {
            onChange(pick.url);
            // 选完即视作一次输入完成，让「失焦即校验」照常发生
            onBlur();
          }
        }}
        kind="image"
        title="选择图片"
        confirmLabel="使用这张"
      />
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
  stacked = false,
}: ControlProps & { stacked?: boolean | undefined }) {
  const label = labelFor(schema, path);
  return (
    /*
     * 同一个元素在宽窄两种容器下都是「开关行」，只是排布不同：
     * 窄的时候（弹窗里的插件设置）标签在左、开关在右，与全站的 SwitchRow 一致；
     * 宽的时候（设置页）并入 FieldGrid 那套「标签列 + 控件列」，开关落在控件列的开头。
     * 用容器查询而不是窗口断点：弹窗的宽度与窗口无关，用断点会在宽屏上把两列塞进 672px 的弹窗。
     * stacked 的分支见 FieldGrid：卡片里不分栏。
     */
    <div
      className={cn(
        "flex items-start justify-between gap-4",
        stacked
          ? "max-w-2xl"
          : "@[44rem]:grid @[44rem]:grid-cols-[16rem_minmax(0,1fr)] @[44rem]:gap-x-8",
      )}
    >
      <div
        className={cn(
          "flex min-w-0 flex-col gap-0.5",
          !stacked && "@[44rem]:pt-2",
        )}
      >
        {/* 开关的标签在窄容器里比别的字段大一档（它是整行的标题）；
            宽容器下并入标签列，就跟其他字段的标签同号，否则同一列里两种字号会发锯齿 */}
        <FieldLabel
          htmlFor={fieldId(path)}
          className={cn("text-base", !stacked && "@[44rem]:text-sm")}
        >
          {label}
        </FieldLabel>
        {schema.description ? (
          <FieldDescription>{schema.description}</FieldDescription>
        ) : null}
        <ErrorLine path={path} error={error} />
      </div>
      <Switch
        id={fieldId(path)}
        checked={Boolean(value)}
        disabled={disabled}
        onCheckedChange={(checked) => onChange(checked)}
        className={cn("mt-0.5", !stacked && "@[44rem]:mt-2")}
        aria-describedby={error ? `${fieldId(path)}-error` : undefined}
      />
    </div>
  );
}

/**
 * 字段的排布。
 *
 * 由上到下（窄容器）与「标签列 + 控件列」（宽容器）是**同一棵 DOM**，
 * 只是外层在容器够宽时从 flex 换成 grid：这样两种形态不可能长得不一样，
 * 也不会出现「窄屏那份忘了改」。
 *
 * 为什么按容器宽度而不是窗口宽度切换：同一套表单引擎既渲染在整页的设置页里
 * （上千像素），也渲染在 672px 的弹窗里（插件设置）。窗口宽 1440 时弹窗照样只有 672，
 * 用媒体查询会把两列硬塞进弹窗。容器查询问的是「我实际有多宽」，这才是对的问题。
 *
 * `stacked` 关掉这套自适应，一律上下排：卡片（重复条目、嵌套对象）里的字段用它。
 * 标签列是给一整页字段留的对齐位，而卡片本身就只有几百像素 —— 挤出一列之后，
 * 说明文字个个折成两行，控件也退到半宽，卡片反倒比不分栏时更挤（见 RepeaterItem）。
 */
function FieldGrid({
  label,
  description,
  children,
  stacked = false,
}: {
  label: ReactNode;
  description: ReactNode;
  children: ReactNode;
  stacked?: boolean | undefined;
}) {
  return (
    <div
      className={cn(
        "flex flex-col gap-y-1.5",
        stacked
          ? "max-w-2xl"
          : "@[44rem]:grid @[44rem]:grid-cols-[16rem_minmax(0,1fr)] @[44rem]:gap-x-8",
      )}
    >
      <div
        className={cn(
          "flex min-w-0 flex-col gap-0.5",
          !stacked && "@[44rem]:pt-2",
        )}
      >
        {label}
        {description}
      </div>
      <div className="flex min-w-0 flex-col gap-1.5">{children}</div>
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
 * 对象数组：每个条目是一个嵌套表单，条目之间可以拖动排序。
 *
 * 用于「一组结构相同的配置」，如首页模块、多条转发规则。条目名与字段说明由
 * `x-item-label` 与 `items.properties` 提供，引擎本身不认识任何具体字段。
 *
 * 排序用拖动（与「菜单」页同一套 motion Reorder）：条目数可能十几个，
 * 一组上下小箭头在那种长度下要按很多次；手柄同时是可聚焦的按钮，
 * 聚焦后按 ↑ ↓ 也能排序，键盘用户不掉队。
 *
 * 条目默认收起，标题行带一句摘要：十几个小组件全摊开时，一页要滚好几屏才找得到
 * 想改的那一个。新加的条目自动展开；含校验错误的条目强制展开，否则错误摘要里的
 * 链接会指向一个藏起来的字段。
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
  const errors = useContext(FieldErrorsContext);
  /*
   * 每条的展开状态，与条目同序。条目是纯数据、没有稳定标识，
   * 所以增删移动时这张表跟着同样地动；长度对不上（外部整体换了值）时缺的按收起算。
   */
  const [open, setOpen] = useState<boolean[]>([]);

  const hasError = (index: number) => {
    const prefix = `${path}.${index}.`;
    return Object.keys(errors).some((key) => key.startsWith(prefix));
  };
  const isOpen = (index: number) => (open[index] ?? false) || hasError(index);
  const allOpen = items.length > 0 && items.every((_, index) => isOpen(index));
  const flags = () => items.map((_, index) => open[index] ?? false);

  function commit(next: unknown[], nextOpen: boolean[]) {
    setOpen(nextOpen);
    onChange(next);
  }

  /** 上下移动一条。拖动之外的入口：键盘操作与拖动失败时的兜底。 */
  function move(from: number, to: number) {
    if (to < 0 || to >= items.length) {
      return;
    }
    const next = [...items];
    const nextOpen = flags();
    const [moved] = next.splice(from, 1);
    const [movedOpen] = nextOpen.splice(from, 1);
    next.splice(to, 0, moved);
    nextOpen.splice(to, 0, movedOpen ?? false);
    commit(next, nextOpen);
  }

  return (
    <div className="flex flex-col gap-3">
      {items.length > 1 ? (
        <Button
          variant="ghost"
          size="sm"
          onClick={() => setOpen(items.map(() => !allOpen))}
          className="self-end"
        >
          {allOpen ? (
            <ChevronsDownUp aria-hidden="true" />
          ) : (
            <ChevronsUpDown aria-hidden="true" />
          )}
          {allOpen ? "全部收起" : "全部展开"}
        </Button>
      ) : null}

      <Reorder.Group
        axis="y"
        values={items}
        onReorder={(next) => {
          // 长度对不上说明拖拽期间列表变过（删了一条），宁可这次不生效
          const order = next.map((item) => items.indexOf(item));
          if (next.length !== items.length || order.includes(-1)) {
            return;
          }
          const current = flags();
          commit(
            next,
            order.map((from) => current[from] ?? false),
          );
        }}
        className="flex flex-col gap-3"
      >
        {items.map((item, index) => (
          <RepeaterItem
            // biome-ignore lint/suspicious/noArrayIndexKey: 纯数据对象，无稳定标识可用
            key={index}
            index={index}
            label={itemLabel}
            item={item}
            itemSchema={itemSchema}
            basePath={`${path}.${index}`}
            disabled={disabled}
            expanded={isOpen(index)}
            locked={hasError(index)}
            renderGroup={renderGroup}
            onToggle={() => {
              const next = flags();
              next[index] = !isOpen(index);
              setOpen(next);
            }}
            onChange={(nextValues) => {
              const next = [...items];
              next[index] = nextValues;
              onChange(next);
            }}
            onRemove={() =>
              commit(
                items.filter((_, i) => i !== index),
                flags().filter((_, i) => i !== index),
              )
            }
            onMove={(direction) => move(index, index + direction)}
          />
        ))}
      </Reorder.Group>

      <Button
        variant="secondary"
        size="sm"
        disabled={disabled}
        onClick={() => commit([...items, {}], [...flags(), true])}
        className="self-start"
      >
        <Plus aria-hidden="true" />
        添加{itemLabel}
      </Button>
      <ErrorLine path={path} error={error} />
    </div>
  );
}

/** 摘要不取这些控件的值：地址、色值、图标名、代码放在标题行上没有辨识度，口令更不该露出来。 */
const SUMMARY_SKIP: ReadonlySet<WidgetKind> = new Set([
  "secret",
  "color",
  "image",
  "images",
  "icon",
  "code",
]);

/** 收起时标题行上的摘要：条目里第一个有值的文字或选项字段，选项显示它的名字。 */
function itemSummary(
  schema: GroupSchema,
  values: Record<string, unknown>,
): string {
  for (const [key, field] of orderedFields(schema)) {
    const raw = values[key];
    if (
      (typeof raw !== "string" && typeof raw !== "number") ||
      SUMMARY_SKIP.has(widgetFor(field))
    ) {
      continue;
    }
    const text = String(raw).replace(/\s+/g, " ").trim();
    if (text === "") {
      continue;
    }
    return field.enum
      ? (optionsFor(field).find((option) => option.value === text)?.label ??
          text)
      : text;
  }
  return "";
}

/** 一个可拖动、可收起的条目。 */
function RepeaterItem({
  index,
  label,
  item,
  itemSchema,
  basePath,
  disabled,
  expanded,
  locked,
  renderGroup,
  onToggle,
  onChange,
  onRemove,
  onMove,
}: {
  index: number;
  label: string;
  item: unknown;
  itemSchema: GroupSchema;
  basePath: string;
  disabled: boolean;
  expanded: boolean;
  /** 条目里有校验错误：必须展开，不许收起。 */
  locked: boolean;
  renderGroup: RenderGroup;
  onToggle: () => void;
  onChange: (values: Record<string, unknown>) => void;
  onRemove: () => void;
  onMove: (direction: -1 | 1) => void;
}) {
  // 拖动只从手柄发起：条目里全是输入框，整条可拖会把选词与光标一起抢走
  const controls = useDragControls();
  const bodyId = useId();
  const values =
    item && typeof item === "object" && !Array.isArray(item)
      ? (item as Record<string, unknown>)
      : {};
  const summary = expanded ? "" : itemSummary(itemSchema, values);

  return (
    <Reorder.Item
      value={item}
      dragListener={false}
      dragControls={controls}
      className="list-none"
    >
      <Inset className={cn(!expanded && "py-2")}>
        <div
          className={cn(
            "flex items-center justify-between gap-2",
            expanded && "mb-2",
          )}
        >
          <div className="flex min-w-0 flex-1 items-center gap-1">
            <button
              type="button"
              disabled={disabled}
              onPointerDown={(event) => {
                if (!disabled) {
                  controls.start(event);
                }
              }}
              onKeyDown={(event) => {
                if (event.key === "ArrowUp") {
                  event.preventDefault();
                  onMove(-1);
                } else if (event.key === "ArrowDown") {
                  event.preventDefault();
                  onMove(1);
                }
              }}
              aria-label={`拖动${label} ${index + 1} 调整顺序，按上下方向键也可以`}
              title="拖动排序（聚焦后用 ↑ ↓ 也行）"
              className="transition-ui flex size-6 shrink-0 cursor-grab touch-none items-center justify-center rounded-control text-ink-subtle hover:bg-surface-active hover:text-ink active:cursor-grabbing disabled:cursor-default disabled:opacity-40"
            >
              <GripVertical aria-hidden="true" className="size-4" />
            </button>
            <button
              type="button"
              onClick={onToggle}
              disabled={locked}
              aria-expanded={expanded}
              aria-controls={bodyId}
              title={
                locked ? "这一项里有要修改的字段，改好之前不能收起" : undefined
              }
              className="transition-ui flex min-w-0 flex-1 items-center gap-1.5 rounded-control py-0.5 pr-2 text-left hover:bg-surface-active disabled:cursor-default disabled:hover:bg-transparent"
            >
              <ChevronRight
                aria-hidden="true"
                className={cn(
                  "transition-ui size-4 shrink-0 text-ink-subtle",
                  expanded && "rotate-90",
                )}
              />
              <span className="shrink-0 text-sm font-medium text-ink">
                {label} {index + 1}
              </span>
              {summary ? (
                <span className="min-w-0 truncate text-sm text-ink-muted">
                  {summary}
                </span>
              ) : null}
            </button>
          </div>
          <Button
            variant="ghost"
            size="icon-sm"
            disabled={disabled}
            onClick={onRemove}
            aria-label={`删除${label} ${index + 1}`}
            className="hover:text-danger"
          >
            <X aria-hidden="true" />
          </Button>
        </div>
        {expanded ? (
          <div id={bodyId}>
            {renderGroup({
              basePath,
              schema: itemSchema,
              values,
              disabled,
              onChange,
              stacked: true,
            })}
          </div>
        ) : null}
      </Inset>
    </Reorder.Item>
  );
}

/** 渲染一组字段的回调。嵌套对象与重复条目都需要它，故由上层注入以免循环依赖。 */
export type RenderGroup = (props: {
  basePath: string;
  schema: GroupSchema;
  values: Record<string, unknown>;
  disabled: boolean;
  onChange: (values: Record<string, unknown>) => void;
  /**
   * 这一组画在卡片里（重复条目的每一条、嵌套对象）。
   *
   * 卡片内的字段一律上下排：标签列（16rem）在几百像素宽的卡片里会把说明文字
   * 挤成两行、控件也只剩半宽，卡片比不分栏时更挤。见 FieldGrid。
   */
  stacked?: boolean | undefined;
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
  scope,
  renderGroup,
  stacked = false,
}: ControlProps & {
  renderGroup: RenderGroup;
  /**
   * 字段所在那一层的值，用于判定条件依赖。
   *
   * 刻意是必填而不是可选：漏传时条件里的字段一律取不到值，于是
   * 「eq 为假」，字段**静默地永远不显示**——页面上什么都不缺，只是少了一项，
   * 没有任何报错。让类型检查把漏传挡在编译期，比让它在运行期变成幽灵字段好。
   */
  scope: FormValues;
  /**
   * 一律上下排（不走「标签列 + 控件列」）。卡片里的字段由 renderGroup 传 true，
   * 理由见 FieldGrid。
   */
  stacked?: boolean | undefined;
}) {
  if (!isVisible(schema["x-show-if"], scope)) {
    return null;
  }
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

  // 布尔字段自成一行，排布见 SwitchControl
  if (widget === "switch") {
    return <SwitchControl {...control} stacked={stacked} />;
  }

  /*
    整块的字段（重复条目）不套 FieldGrid 那套「标签列 + 控件列」。
    16rem 的标签列是给单行控件留的对齐位，而重复条目是一整摞卡片：
    标签列会把它挤窄一大截 —— 首页模块的卡片因此掉到容器查询的断点以下，
    连卡内的参数都从两列退成上下排，而站长进这一页要看的就是那些卡片。
    标签与说明改为压在整块上方，宽度全留给条目。

    用 fieldset + legend 而不是「div 加一个 label」：重复条目没有单一的可聚焦控件，
    给它一个指向不存在元素的 for 是假的关联；fieldset 天生就是「一组同构控件」的
    语义，读屏会把它连同标题一起念出来。fieldset 自带 min-inline-size: min-content，
    窄容器下会撑破布局，故补 min-w-0。
  */
  if (widget === "repeater") {
    return (
      <fieldset className="min-w-0">
        <legend className="text-sm font-medium text-ink select-none">
          {label}
        </legend>
        <div className="flex flex-col gap-3 pt-1">
          {schema.description ? (
            <FieldDescription>{schema.description}</FieldDescription>
          ) : null}
          {renderControl(widget, control, renderGroup)}
        </div>
      </fieldset>
    );
  }

  // 复合控件（列表 / 嵌套）自己负责显示错误，其余在这里统一显示
  // （重复条目在上面就返回了，它同样自带错误行）
  const showError = widget !== "list" && widget !== "group";

  return (
    <FieldGrid
      label={<FieldLabel htmlFor={id}>{label}</FieldLabel>}
      description={
        schema.description ? (
          <FieldDescription>{schema.description}</FieldDescription>
        ) : null
      }
      stacked={stacked}
    >
      {renderControl(widget, control, renderGroup)}
      {showError ? <ErrorLine path={path} error={error} /> : null}
    </FieldGrid>
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
    case "secret":
      return <SecretControl {...control} />;
    case "number":
      return <NumberControl {...control} />;
    case "slider":
      return <SliderControl {...control} />;
    case "select":
      return <SelectControl {...control} />;
    case "radio":
      return <RadioControl {...control} />;
    case "multiselect":
      return <MultiSelectControl {...control} />;
    case "date":
      return <DateControl {...control} />;
    case "icon":
      return <IconControl {...control} />;
    case "color":
      return <ColorControl {...control} />;
    case "image":
      return <ImageControl {...control} />;
    case "images":
      return <ImagesControl {...control} />;
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
            stacked: true,
          })}
        </Inset>
      );
    }
    default:
      return <TextControl {...control} />;
  }
}

/**
 * 单选组。选项少时用它，多时用下拉。
 */
function RadioControl({
  path,
  schema,
  value,
  error,
  disabled,
  onChange,
  onBlur,
}: ControlProps) {
  const options = optionsFor(schema);
  return (
    <RadioGroup
      value={String(value ?? "")}
      disabled={disabled}
      onValueChange={(next) => {
        onChange(next);
        onBlur();
      }}
      aria-invalid={error ? true : undefined}
      aria-describedby={error ? `${fieldId(path)}-error` : undefined}
    >
      {options.map((option) => (
        <RadioRow
          key={option.value}
          id={`${fieldId(path)}-${option.value}`}
          value={option.value}
          label={option.label}
          disabled={disabled}
        />
      ))}
    </RadioGroup>
  );
}

/**
 * 多选。值是一个数组。
 *
 * 候选项声明在 items 的 enum 上，而不是字段自己的 enum 上——
 * 数组字段的「取值」是数组本身，写成顶层 enum 会表达成「数组等于其中一个字符串」。
 */
function MultiSelectControl({
  path,
  schema,
  value,
  error,
  disabled,
  onChange,
}: ControlProps) {
  const options = optionsFor(schema.items ?? {});
  const selected = Array.isArray(value)
    ? value.map((item) => String(item))
    : [];
  const toggle = (option: string, checked: boolean) => {
    onChange(
      checked
        ? [...selected, option]
        : selected.filter((item) => item !== option),
    );
  };

  // 库里可能留着已经不在选项里的值（Schema 收窄过）。把它们一并显示出来，
  // 否则用户看不见、也没法取消勾选，而保存时会原样带着走。
  const known = new Set(options.map((option) => option.value));
  const stale = selected.filter((item) => !known.has(item));

  return (
    <div
      className="flex flex-col gap-0.5"
      aria-invalid={error ? true : undefined}
      aria-describedby={error ? `${fieldId(path)}-error` : undefined}
    >
      {options.map((option) => (
        <CheckboxRow
          key={option.value}
          id={`${fieldId(path)}-${option.value}`}
          checked={selected.includes(option.value)}
          onCheckedChange={(checked) => toggle(option.value, checked)}
          label={option.label}
          disabled={disabled}
        />
      ))}
      {stale.map((option) => (
        <CheckboxRow
          key={option}
          id={`${fieldId(path)}-${option}`}
          checked
          onCheckedChange={(checked) => toggle(option, checked)}
          label={`${option}（已不在选项中）`}
          disabled={disabled}
        />
      ))}
    </div>
  );
}

/**
 * 拖动条。当前值永远显示在右侧：拖动时只看滑块的相对位置，
 * 说不清「到底调到了几」——那正是设置项要回答的问题。
 */
function SliderControl({
  path,
  schema,
  value,
  error,
  disabled,
  onChange,
  onBlur,
}: ControlProps) {
  const min = schema.minimum ?? 0;
  const max = schema.maximum ?? 100;
  const step = schema.type === "integer" ? 1 : 0.1;
  const parsed = Number(value);
  const current = Number.isFinite(parsed) ? parsed : min;
  const unit = schema["x-unit"] ?? "";

  return (
    <div className="flex items-center gap-3">
      <Slider
        id={fieldId(path)}
        min={min}
        max={max}
        step={step}
        value={[current]}
        disabled={disabled}
        onValueChange={([next]) => {
          if (next !== undefined) {
            onChange(next);
          }
        }}
        // 拖动过程中每一步都写回表单，但只在松手时才标记「已触碰」并触发校验：
        // 拖到一半就报「不能小于 N」会闪个不停。
        onValueCommit={onBlur}
        ariaLabel={labelFor(schema, path)}
        aria-invalid={error ? true : undefined}
        aria-describedby={error ? `${fieldId(path)}-error` : undefined}
      />
      <output
        htmlFor={fieldId(path)}
        className="w-14 shrink-0 text-right text-sm tabular-nums text-ink-muted"
      >
        {current}
        {unit}
      </output>
    </div>
  );
}

/** 日期。用浏览器自带的日期控件：它有原生的键盘操作与本地化，不值得自己造。 */
function DateControl({
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
      type="date"
      value={String(value ?? "")}
      disabled={disabled}
      onChange={(e) => onChange(e.target.value)}
      onBlur={onBlur}
      className="max-w-48"
    />
  );
}

/** 图标。取值是图标登记表里的名字，后端与前端共守这一份词汇（见 lib/icons.ts）。 */
function IconControl({
  path,
  value,
  disabled,
  onChange,
  onBlur,
}: ControlProps) {
  return (
    <IconPicker
      id={fieldId(path)}
      value={String(value ?? "")}
      disabled={disabled}
      onChange={(next) => {
        onChange(next);
        onBlur();
      }}
    />
  );
}

/**
 * 多张图片。每一行一个地址，与单图同样的「填地址 + 即时预览」。
 *
 * 与 ListControl 的差别只在预览：图片地址是一串看不出所以然的 URL，
 * 不给预览的话，排错了顺序、填错了链接都看不出来。
 *
 * 从附件库选时是多选：要配一组图（画廊、轮播）的人不会一张一张开弹窗。
 */
function ImagesControl({
  path,
  value,
  error,
  disabled,
  onChange,
  onBlur,
}: ControlProps) {
  const items = Array.isArray(value) ? value.map((item) => String(item)) : [];
  const update = (next: string[]) => onChange(next);
  const [picker, setPicker] = useState(false);

  return (
    <div className="flex flex-col gap-2">
      {items.map((item, index) => (
        // biome-ignore lint/suspicious/noArrayIndexKey: 完全受控的输入框，无内部状态
        <div key={index} className="flex items-center gap-2">
          <ImageThumb url={item} />
          <Input
            value={item}
            disabled={disabled}
            onChange={(e) => {
              const next = [...items];
              next[index] = e.target.value;
              update(next);
            }}
            onBlur={onBlur}
            aria-label={`第 ${index + 1} 张图片的地址`}
          />
          <Button
            variant="ghost"
            size="icon-sm"
            disabled={disabled}
            onClick={() => update(items.filter((_, i) => i !== index))}
            aria-label={`删除第 ${index + 1} 张图片`}
            className="hover:text-danger"
          >
            <X aria-hidden="true" />
          </Button>
        </div>
      ))}
      <div className="flex items-center gap-2">
        <Button
          variant="secondary"
          size="sm"
          disabled={disabled}
          onClick={() => setPicker(true)}
        >
          <ImagePlus aria-hidden="true" />
          从附件库选
        </Button>
        <Button
          variant="ghost"
          size="sm"
          disabled={disabled}
          onClick={() => update([...items, ""])}
        >
          <Plus aria-hidden="true" />
          填一个地址
        </Button>
      </div>
      <ErrorLine path={path} error={error} />

      <MediaPickerDialog
        open={picker}
        onOpenChange={setPicker}
        onSelect={(picks) => {
          if (picks.length > 0) {
            // 追加而不是替换：已经配好的几张不该因为再选一张就丢掉
            update([...items, ...picks.map((pick) => pick.url)]);
            onBlur();
          }
        }}
        kind="image"
        multiple
        title="选择图片"
        confirmLabel="添加"
      />
    </div>
  );
}

/** 小尺寸图片预览。加载失败时退回一个中性的占位图标，不显示浏览器的破图。 */
function ImageThumb({ url }: { url: string }) {
  const [broken, setBroken] = useState(false);

  // biome-ignore lint/correctness/useExhaustiveDependencies: 仅在地址变化时重置
  useEffect(() => {
    setBroken(false);
  }, [url]);

  return (
    <div
      className={cn(
        "flex size-9 shrink-0 items-center justify-center overflow-hidden",
        "rounded-control border border-line bg-surface-raised",
      )}
    >
      {url && !broken ? (
        <img
          src={url}
          alt=""
          className="size-full object-contain"
          onError={() => setBroken(true)}
        />
      ) : (
        <ImageOff aria-hidden="true" className="size-4 text-ink-subtle" />
      )}
    </div>
  );
}
