import {
  type RenderGroup,
  SchemaField,
  SecretSetContext,
  fieldId,
} from "@/components/form/controls";
import {
  type FieldErrors,
  type FieldSchema,
  type FormValues,
  type GroupSchema,
  initialValues,
  isVisible,
  labelFor,
  orderedFields,
  sectionsOf,
  validateGroup,
} from "@/components/form/schema";
import { UnsavedChangesGuard } from "@/components/navigation/unsaved-guard";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

/**
 * 通用表单引擎。
 *
 * 输入是一份 JSON Schema 子集与一组值，输出是一个可用的表单 ——
 * 引擎本身不认识任何具体字段。站点设置、主题设置、插件设置走的是同一条路径，
 * 这是 §5 定下那套声明式格式的全部理由。
 *
 * 三件事是这个引擎存在的意义，也是它比「每个页面手写一遍表单」强的地方：
 *
 *   1. **错误摘要与逐字段内联错误并存**。摘要固定在表单顶部、可聚焦、每一条都能
 *      点到出错的字段上。只画一圈红框的话，键盘与读屏用户无从知道发生了什么，
 *      而表单长到需要滚动时，视口内的红框也救不了视口外的错。
 *   2. **失焦即校验**，而不是提交才校验。填错的那一刻就告诉他，
 *      比填完二十个字段再一次性收到二十条错误好得多。
 *   3. **服务端的 422 明细按 location 落回对应字段**。服务端才是权威校验方
 *      （JSON Schema 校验器 + Go 侧 Check），它的错误必须能定位到字段，
 *      否则用户只能看到一句「设置校验失败」然后自己猜。
 *
 * 版面按 Halo 的 FormKit 风格：字段之间一条细线，标签在上、控件在下。
 */

export type SchemaFormProps = {
  schema: GroupSchema;
  /** 当前值。切换分组或数据到达后用它重填表单。 */
  values: FormValues | undefined;
  /** 提交。抛错时引擎保持脏状态并显示错误。 */
  onSubmit: (values: FormValues) => Promise<void>;
  /** 服务端返回的 422 明细，由调用方传入；引擎负责映射到字段。 */
  serverErrors?: FieldErrors | undefined;
  /** 服务端无法归到字段上的错误。 */
  serverMessages?: string[] | undefined;
  disabled?: boolean;
  submitLabel?: string;
  /** 值未变化时禁用提交按钮。默认开启。 */
  disableWhenPristine?: boolean;
  /**
   * 已经设过值的口令字段名。
   *
   * 口令不回传（服务端抹成空串），界面靠这份名单把「已设置」画出来。
   * 数组的引用要稳定：每次渲染新建一个 `[]` 会让下面的 context 每次都是新值。
   */
  secretSet?: readonly string[] | undefined;
  /** 值的比较器。设置值都是 JSON 可序列化的，默认用序列化结果比较。 */
  isEqual?: (a: FormValues, b: FormValues) => boolean;
  /**
   * 一律上下排（不走「标签列 + 控件列」）。
   *
   * 主题设置用它：那个面板与主题列表并排、只有 800 出头，挤出一列之后说明文字
   * 个个折成两行，控件也只剩半宽。站点设置铺满工作区，两列在那儿才是成立的。
   */
  stacked?: boolean | undefined;
  className?: string;
};

const serialize = (values: FormValues) => JSON.stringify(values);

// 稳定的空数组：默认值写成 `[]` 会让 context 每次渲染都是新引用，
// 下游的 useMemo 依赖它时每次都失效。
const EMPTY_SECRETS: readonly string[] = [];

export function SchemaForm({
  schema,
  values,
  onSubmit,
  serverErrors,
  serverMessages,
  disabled = false,
  submitLabel = "保存",
  disableWhenPristine = true,
  isEqual,
  secretSet,
  stacked = false,
  className,
}: SchemaFormProps) {
  const [form, setForm] = useState<FormValues>(() =>
    initialValues(schema, values),
  );
  const [touched, setTouched] = useState<Record<string, boolean>>({});
  const [submitted, setSubmitted] = useState(false);
  const [pending, setPending] = useState(false);
  const summaryRef = useRef<HTMLDivElement>(null);
  const dirtyRef = useRef(false);

  // 数据到达或切换分组后重填。用 dirtyRef 而不是 form 做判断：
  // 把 form 放进依赖会让每次输入都触发一次「重置」，输入框会一直跳回旧值。
  useEffect(() => {
    setForm(initialValues(schema, values));
    setTouched({});
    setSubmitted(false);
    dirtyRef.current = false;
  }, [schema, values]);

  const pristine = useMemo(() => {
    const baseline = initialValues(schema, values);
    return isEqual
      ? isEqual(form, baseline)
      : serialize(form) === serialize(baseline);
  }, [schema, values, form, isEqual]);

  dirtyRef.current = !pristine;

  // 离开页面前提醒有未保存的改动。设置页的字段多、改动分散，
  // 误点侧栏丢掉一整页的修改是很实际的损失。
  useEffect(() => {
    const onBeforeUnload = (event: BeforeUnloadEvent) => {
      if (dirtyRef.current) {
        event.preventDefault();
      }
    };
    window.addEventListener("beforeunload", onBeforeUnload);
    return () => window.removeEventListener("beforeunload", onBeforeUnload);
  }, []);

  /** 本次渲染要显示的错误：已触碰过的字段 + 服务端返回的字段错误。 */
  const errors: FieldErrors = useMemo(() => {
    const local = validateGroup(schema, form);
    const out: FieldErrors = {};
    for (const [path, message] of Object.entries(local)) {
      // 顶层字段的「触碰」判定：嵌套字段的路径里含 `.`，
      // 用顶层键是否被触碰决定要不要显示它的错误。
      const top = path.split(".")[0] ?? path;
      const shouldShow = submitted || touched[top];
      if (shouldShow || serverErrors?.[path]) {
        out[path] = serverErrors?.[path] ?? message;
      }
    }
    // 服务端报了但本地没检出的（例如 Go 侧 Check 才知道的时区名），也要显示
    for (const [path, message] of Object.entries(serverErrors ?? {})) {
      if (!out[path]) {
        out[path] = message;
      }
    }
    return out;
  }, [schema, form, touched, submitted, serverErrors]);

  const errorPaths = useMemo(() => Object.keys(errors), [errors]);
  const hasErrors = errorPaths.length > 0 || (serverMessages?.length ?? 0) > 0;

  const setValue = useCallback((path: string, value: unknown) => {
    setForm((current) => setAtPath(current, path, value));
  }, []);

  const markTouched = useCallback((path: string) => {
    const top = path.split(".")[0] ?? path;
    setTouched((current) =>
      current[top] ? current : { ...current, [top]: true },
    );
  }, []);

  async function handleSubmit(event: React.FormEvent) {
    event.preventDefault();
    setSubmitted(true);

    if (Object.keys(validateGroup(schema, form)).length > 0) {
      // 有错就滚到摘要并把焦点移过去 —— 只把按钮抖一下，用户不知道错在哪
      summaryRef.current?.focus();
      return;
    }

    setPending(true);
    try {
      await onSubmit(form);
      setSubmitted(false);
      setTouched({});
    } catch {
      // 错误由调用方经 serverErrors 回传，这里只负责保持脏状态
      summaryRef.current?.focus();
    } finally {
      setPending(false);
    }
  }

  const formNode = (
    <form
      onSubmit={handleSubmit}
      className={cn("flex flex-col gap-5", className)}
      noValidate
    >
      {/*
        站内导航的未保存提醒。设置页字段多、改动分散，误点侧栏丢掉一整页修改
        是很实际的损失；上面那个 beforeunload 只覆盖刷新与关标签页。
      */}
      <UnsavedChangesGuard
        when={() => dirtyRef.current}
        title="离开前要先保存吗？"
        consequence={<p>这个表单有尚未保存的修改，离开后改动会丢失。</p>}
      />

      {/*
        错误摘要。
        role="alert"（由 Alert 的 danger 语气给出）让它在出现时被播报；
        tabIndex={-1} 让它可被程序聚焦；每条都链到对应字段。
        这三件事缺一，摘要就只是「一段红字」。
      */}
      {hasErrors ? (
        <Alert
          ref={summaryRef}
          tabIndex={-1}
          tone="danger"
          title={`有 ${errorPaths.length + (serverMessages?.length ?? 0)} 处需要修改`}
        >
          <ul className="flex flex-col gap-0.5">
            {errorPaths.map((path) => (
              <li key={path}>
                <a
                  href={`#${fieldId(path)}`}
                  onClick={(event) => {
                    // 默认的锚点跳转会把地址栏改掉并留下 #hash。
                    // 这里改为手动聚焦，地址栏保持干净。
                    event.preventDefault();
                    focusField(path);
                  }}
                  className="text-xs text-danger underline underline-offset-2"
                >
                  {pathLabel(schema, path)}：{errors[path]}
                </a>
              </li>
            ))}
            {(serverMessages ?? []).map((message) => (
              <li key={message} className="text-xs text-danger">
                {message}
              </li>
            ))}
          </ul>
        </Alert>
      ) : null}

      <SchemaFormFields
        schema={schema}
        values={form}
        errors={errors}
        disabled={disabled || pending}
        onChange={setValue}
        onBlur={markTouched}
        stacked={stacked}
      />

      <div className="flex flex-wrap items-center gap-3 border-line border-t pt-4">
        <Button
          type="submit"
          variant="primary"
          loading={pending}
          disabled={disabled || (disableWhenPristine && pristine)}
        >
          {pending ? "正在保存" : submitLabel}
        </Button>
        <Button
          variant="secondary"
          disabled={disabled || pending || pristine}
          onClick={() => {
            setForm(initialValues(schema, values));
            setTouched({});
            setSubmitted(false);
          }}
        >
          放弃修改
        </Button>
        {/* 有未保存改动时给一句明确的状态，比把按钮点亮更直接 */}
        {!pristine && !pending ? (
          <span className="text-xs text-ink-muted">有未保存的修改</span>
        ) : null}
      </div>
    </form>
  );

  // context 只包一层，不改变表单自身的结构：指令类字段要读「这个口令设过没有」，
  // 而它藏在递归渲染的深处，逐层传参传不到。
  return (
    <SecretSetContext.Provider value={secretSet ?? EMPTY_SECRETS}>
      {formNode}
    </SecretSetContext.Provider>
  );
}

export type SchemaFormFieldsProps = {
  schema: GroupSchema;
  /** 受控值。 */
  values: FormValues;
  /** 要显示的错误，键为字段路径。由调用方决定「什么时候该显示」。 */
  errors: FieldErrors;
  onChange: (path: string, value: unknown) => void;
  onBlur: (path: string) => void;
  disabled?: boolean;
  /**
   * 不在这里渲染的顶层字段。
   *
   * 用于把某个字段提到别处画（如设置页把主开关提到区块标题栏上）——
   * 不排除的话同一个开关会在页面上出现两个，而它们还都是活的。
   */
  omit?: readonly string[] | undefined;
  /** 没有任何字段可见时显示的内容（条件依赖把整组字段都藏起来时）。 */
  empty?: React.ReactNode | undefined;
  /** 一律上下排（不走「标签列 + 控件列」）。见 SchemaFormProps。 */
  stacked?: boolean | undefined;
  className?: string | undefined;
};

/**
 * 表单引擎的渲染内核：只画字段，不管值、不管校验、不带按钮。
 *
 * 与 SchemaForm 分开，是因为「一页上有六个分组、共用一个保存按钮」的设置页
 * 需要自己持有全部分组的值与脏状态。让它复用 SchemaForm 就得往里加
 * 「不要按钮」「不要守卫」「值交给外面」三个开关，而那样的组件已经不是同一个东西了。
 *
 * 单个表单的常规用法仍然走 SchemaForm：它在这个内核外面补齐状态、校验、
 * 错误摘要与提交，行为与拆分前完全一致。
 */
export function SchemaFormFields({
  schema,
  values,
  errors,
  onChange,
  onBlur,
  disabled = false,
  omit,
  empty,
  stacked = false,
  className,
}: SchemaFormFieldsProps) {
  /*
   * 分段与字段一起算，但**不用 useMemo 缓存可见性**：
   * 可见性随表单值变化，缓存它反而要给每个分区都带上 values 依赖。
   * 分段本身只随 Schema 变，值得缓一下。
   */
  const sections = useMemo(() => sectionsOf(schema), [schema]);

  const renderGroup: RenderGroup = useCallback(
    ({
      basePath,
      schema: groupSchema,
      values: groupValues,
      disabled: groupDisabled,
      onChange: onGroupChange,
      stacked,
    }) => (
      <div className="@container flex flex-col gap-4">
        {/*
          字段顺序走 orderedFields 而不是 Object.entries：声明来自 YAML 时键序会丢，
          嵌套对象（重复条目的每一条）里的顺序因此会变成 map 的随机序。
        */}
        {orderedFields(groupSchema)
          .filter(([, field]) => isVisible(field["x-show-if"], groupValues))
          .map(([key, field]) => {
            const path = `${basePath}.${key}`;
            return (
              <SchemaField
                key={path}
                path={path}
                schema={field}
                value={groupValues[key]}
                error={errors[path]}
                disabled={groupDisabled}
                onChange={(next) => {
                  onGroupChange({ ...groupValues, [key]: next });
                }}
                onBlur={() => onBlur(path)}
                scope={groupValues}
                renderGroup={renderGroup}
                stacked={stacked}
              />
            );
          })}
      </div>
    ),
    [errors, onBlur],
  );

  /*
    分段渲染。字段之间一条细线，段与段之间留出更大的间距并给出标题——
    设置项多起来以后，一长条没有分隔的表单会让人找不到东西。
    没有 x-sections 的老 Schema 只会得到一段无标题的，外观与改动前一致。
  */
  const rendered = sections
    .map((section) => {
      // 条件不成立的字段此刻不该出现。它的值仍然留着：
      // 站长把驱动从 s3 切回 local 再切回来，填过的地址应当还在。
      const visibleFields = section.fields.filter(
        ([key, field]) =>
          !omit?.includes(key) && isVisible(field["x-show-if"], values),
      );
      if (visibleFields.length === 0) {
        return null;
      }
      return (
        <section
          key={section.title || "__default__"}
          className="flex flex-col gap-2"
        >
          {section.title ? (
            /*
             * 分段标题要比它下面的字段标签**大一档**，再加一条细线把它与字段分开。
             * 两者同为 text-sm 时，分段读起来像是第一个字段的一部分——
             * 层级反了，而字段一多就看不出这一段从哪开始。
             */
            <header className="flex flex-col gap-0.5 border-line border-b pb-2">
              <h3 className="text-base font-medium text-ink">
                {section.title}
              </h3>
              {section.description ? (
                <p className="text-xs text-ink-muted">{section.description}</p>
              ) : null}
            </header>
          ) : null}
          <div className="@container divide-y divide-line">
            {visibleFields.map(([key, field]) => (
              <FieldRow
                key={key}
                path={key}
                schema={field}
                value={values[key]}
                error={errors[key]}
                disabled={disabled}
                onChange={(value) => onChange(key, value)}
                onBlur={() => onBlur(key)}
                scope={values}
                renderGroup={renderGroup}
                stacked={stacked}
              />
            ))}
          </div>
        </section>
      );
    })
    .filter(Boolean);

  if (rendered.length === 0 && empty) {
    return <>{empty}</>;
  }

  return <div className={cn("flex flex-col gap-8", className)}>{rendered}</div>;
}

/** 每个字段占一行，行与行之间由父级的分隔线隔开。 */
function FieldRow(props: Parameters<typeof SchemaField>[0]) {
  return (
    <div className="py-4 first:pt-0 last:pb-0">
      <SchemaField {...props} />
    </div>
  );
}

/**
 * 聚焦到出错的字段。
 *
 * 用 `focus({ preventScroll: true })` + 手动 scrollIntoView：
 * 浏览器默认的聚焦滚动会把字段顶到视口最上沿，标签与说明都在屏幕外，
 * 用户看到一个孤零零的输入框，不知道它是哪个字段。
 */
function focusField(path: string) {
  const element = document.getElementById(fieldId(path));
  if (!element) {
    return;
  }
  element.focus({ preventScroll: true });
  element.scrollIntoView({ block: "center", behavior: "smooth" });
}

/** 字段路径 → 可读的标签，用于错误摘要。嵌套时带上父字段名。 */
function pathLabel(schema: GroupSchema, path: string): string {
  const parts = path.split(".");
  let current: FieldSchema | undefined = schema;
  const names: string[] = [];
  for (const part of parts) {
    const next: FieldSchema | undefined = current?.properties?.[part];
    if (!next) {
      names.push(part);
      current = undefined;
      continue;
    }
    names.push(labelFor(next, part));
    // repeater 的路径里有纯数字下标，它没有对应的 Schema 节点
    current = Number.isNaN(Number(part)) ? next : next.items;
  }
  return names.join(" / ");
}

/** 按点号路径写值，沿途缺对象就补上。 */
function setAtPath(
  values: FormValues,
  path: string,
  value: unknown,
): FormValues {
  const parts = path.split(".");
  if (parts.length === 1) {
    return { ...values, [path]: value };
  }
  const [head, ...rest] = parts;
  const child = values[head as string];
  const childObject =
    child && typeof child === "object" && !Array.isArray(child)
      ? (child as FormValues)
      : {};
  return {
    ...values,
    [head as string]: setAtPath(childObject, rest.join("."), value),
  };
}
