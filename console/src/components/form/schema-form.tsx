import {
  type RenderGroup,
  SchemaField,
  fieldId,
} from "@/components/form/controls";
import {
  type FieldErrors,
  type FieldSchema,
  type FormValues,
  type GroupSchema,
  initialValues,
  labelFor,
  validateGroup,
} from "@/components/form/schema";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { AlertCircle, Loader2 } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

/**
 * 通用表单引擎（agent.md §5）。
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
  /** 值的比较器。设置值都是 JSON 可序列化的，默认用序列化结果比较。 */
  isEqual?: (a: FormValues, b: FormValues) => boolean;
  className?: string;
};

const serialize = (values: FormValues) => JSON.stringify(values);

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
  className,
}: SchemaFormProps) {
  const fields = useMemo(
    () => Object.entries(schema.properties ?? {}),
    [schema],
  );

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

  const renderGroup: RenderGroup = useCallback(
    ({
      basePath,
      schema: groupSchema,
      values: groupValues,
      disabled: groupDisabled,
      onChange,
    }) => (
      <div className="flex flex-col gap-4">
        {Object.entries(groupSchema.properties ?? {}).map(([key, field]) => {
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
                onChange({ ...groupValues, [key]: next });
              }}
              onBlur={() => markTouched(path)}
              renderGroup={renderGroup}
            />
          );
        })}
      </div>
    ),
    [errors, markTouched],
  );

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

  return (
    <form
      onSubmit={handleSubmit}
      className={cn("flex flex-col gap-5", className)}
      noValidate
    >
      {/*
        错误摘要。
        role="alert" 让它在出现时被播报；tabIndex={-1} 让它可被程序聚焦；
        每条都链到对应字段。这三件事缺一，摘要就只是「一段红字」。
      */}
      {hasErrors ? (
        <div
          ref={summaryRef}
          tabIndex={-1}
          role="alert"
          className="flex flex-col gap-1.5 rounded-panel border border-danger bg-danger-soft px-4 py-3"
        >
          <p className="flex items-center gap-2 text-sm font-medium text-danger">
            <AlertCircle aria-hidden="true" className="size-4" />有{" "}
            {errorPaths.length + (serverMessages?.length ?? 0)} 处需要修改
          </p>
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
        </div>
      ) : null}

      <div className="flex flex-col gap-5">
        {fields.map(([key, field]) => (
          <FieldRow
            key={key}
            path={key}
            schema={field}
            value={form[key]}
            error={errors[key]}
            disabled={disabled || pending}
            onChange={(value) => setValue(key, value)}
            onBlur={() => markTouched(key)}
            renderGroup={renderGroup}
          />
        ))}
      </div>

      <div className="flex flex-wrap items-center gap-3 border-line border-t pt-4">
        <Button
          type="submit"
          variant="primary"
          disabled={disabled || pending || (disableWhenPristine && pristine)}
        >
          {pending ? (
            <Loader2 aria-hidden="true" className="animate-spin" />
          ) : null}
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
}

/** 把「布尔字段」以外的字段包一层分隔线，让长表单有节奏。 */
function FieldRow(props: Parameters<typeof SchemaField>[0]) {
  return (
    <div className="border-line border-b pb-5 last:border-b-0 last:pb-0">
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
  return names.join(" · ");
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
