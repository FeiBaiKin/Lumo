import { api, problemMessage } from "@/api/client";
import type { components } from "@/api/schema";
import { fieldId } from "@/components/form/controls";
import {
  type FieldErrors,
  type FormValues,
  type GroupSchema,
  errorsFromServer,
  initialValues,
  labelFor,
  validateGroup,
} from "@/components/form/schema";
import { SchemaFormFields } from "@/components/form/schema-form";
import { PageBody, PageHeader } from "@/components/layout/page-header";
import { UnsavedChangesGuard } from "@/components/navigation/unsaved-guard";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardBody, CardHeader } from "@/components/ui/card";
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Field, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { EmptyState, ErrorState, Skeleton } from "@/components/ui/states";
import { useDocumentTitle } from "@/lib/use-document-title";
import {
  SettingsSection,
  sectionAnchor,
} from "@/pages/settings/settings-section";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Send, Settings, SlidersHorizontal } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useParams } from "react-router";

/**
 * 站点设置：**所有设置分组在同一页上**，各自折起，整页一个保存按钮。
 *
 * 分组曾是侧边栏里的五个菜单项（站点 / 附件存储 / 邮件发送 / 评论 / SEO），
 * 注册开关另在用户页。那种切分对着后端的分组表画，而不是对着站长的任务画：
 * 「开放注册」要同时动注册开关、邮件设置与站点对外地址三处，站长得在三个页面之间
 * 来回走，每一处各点一次保存，中途还没有任何东西告诉他这三件事是一件事。
 *
 * 现在一页装下全部，代价是这一页有四十多个字段——所以它们默认收起（见 SettingsSection），
 * 标题栏上留着主开关与「已修改」记号。第一屏因此是一张「有哪些东西可配」的目录，
 * 而不是一条长坡。
 *
 * 保存是整页一次，只提交改动过的分组：后端仍按分组存储（PUT /settings/{group}），
 * 页面把脏分组逐个提交。逐组独立按钮看似更简单，但那等于告诉站长
 * 「你刚才那三处改动要分三次保存」，而他只做了一件事。
 *
 * 地址仍认 /settings/<分组>：那些地址是接口与书签的一部分，不该因为版面合并而失效。
 * 它现在的含义从「打开那一页」变成「展开那一块并滚过去」，命令面板里的分组项走的也是它。
 */

type GroupView = components["schemas"]["GroupView"];

const serialize = (values: FormValues) => JSON.stringify(values);

export function SettingsPage() {
  const { group } = useParams<{ group: string }>();
  const queryClient = useQueryClient();

  const query = useQuery({
    queryKey: ["settings"],
    queryFn: async () => {
      const { data, response } = await api.GET("/api/v1/console/settings");
      if (!response.ok) {
        throw new Error(`载入设置失败（HTTP ${response.status}）`);
      }
      return data?.items ?? [];
    },
  });

  const groups = useMemo(() => query.data ?? [], [query.data]);

  /** 每组的基线值（服务端当前值）。草稿与它比对得出「改了没有」。 */
  const baselines = useMemo(() => {
    const out: Record<string, FormValues> = {};
    for (const item of groups) {
      out[item.name] = initialValues(
        item.schema as GroupSchema,
        item.values as FormValues,
      );
    }
    return out;
  }, [groups]);

  const [drafts, setDrafts] = useState<Record<string, FormValues>>({});
  const [touched, setTouched] = useState<
    Record<string, Record<string, boolean>>
  >({});
  const [submitted, setSubmitted] = useState(false);
  const [saving, setSaving] = useState(false);
  const [serverErrors, setServerErrors] = useState<Record<string, FieldErrors>>(
    {},
  );
  const [serverMessages, setServerMessages] = useState<
    Record<string, string[]>
  >({});
  const [savedNote, setSavedNote] = useState<string | null>(null);
  const [open, setOpen] = useState<Record<string, boolean>>({});

  const summaryRef = useRef<HTMLDivElement>(null);
  const seenRef = useRef<Record<string, string>>({});

  /*
   * 数据到达后灌草稿，但**逐组比对、只重置变了的那一组**。
   *
   * 一把全重置的话，部分保存失败时会把失败那一组里刚填的东西也抹掉——
   * 那一组的基线根本没变（服务端没收下），却要连坐。
   */
  useEffect(() => {
    const keys: Record<string, string> = {};
    for (const [name, values] of Object.entries(baselines)) {
      keys[name] = serialize(values);
    }
    /*
     * 比较基准要在 setDrafts **之前**取下来。
     *
     * updater 是渲染阶段才跑的，而 `seenRef.current = keys` 在这一轮 effect 里
     * 就同步生效了——updater 里再读 seenRef 读到的已经是新值，
     * 「基线变了吗」于是恒为否，草稿永远不会跟着服务端更新。
     */
    const previous = seenRef.current;
    seenRef.current = keys;
    setDrafts((current) => {
      let changed = false;
      const next = { ...current };
      for (const [name, key] of Object.entries(keys)) {
        if (previous[name] !== key || !(name in current)) {
          next[name] = baselines[name] as FormValues;
          changed = true;
        }
      }
      // 分组消失（模块被移除）时把它的草稿一起清掉，免得留在脏分组清单里
      for (const name of Object.keys(next)) {
        if (!(name in keys)) {
          delete next[name];
          changed = true;
        }
      }
      return changed ? next : current;
    });
  }, [baselines]);

  /** 本次渲染要显示的错误，按分组归拢。 */
  const errorsByGroup = useMemo(() => {
    const out: Record<string, FieldErrors> = {};
    for (const item of groups) {
      const schema = item.schema as GroupSchema;
      const values = drafts[item.name];
      const shown: FieldErrors = {};
      if (values) {
        for (const [path, message] of Object.entries(
          validateGroup(schema, values),
        )) {
          const top = path.split(".")[0] ?? path;
          const visible =
            submitted ||
            touched[item.name]?.[top] ||
            serverErrors[item.name]?.[path];
          if (visible) {
            shown[path] = serverErrors[item.name]?.[path] ?? message;
          }
        }
      }
      // 服务端报了而本地没检出的（如 Go 侧 Check 才知道的时区名）也要显示
      for (const [path, message] of Object.entries(
        serverErrors[item.name] ?? {},
      )) {
        if (!shown[path]) {
          shown[path] = message;
        }
      }
      out[item.name] = shown;
    }
    return out;
  }, [groups, drafts, touched, submitted, serverErrors]);

  const dirtyNames = useMemo(
    () =>
      groups
        .map((item) => item.name)
        .filter(
          (name) =>
            drafts[name] && serialize(drafts[name]) !== seenRef.current[name],
        ),
    [groups, drafts],
  );

  const dirtyRef = useRef(false);
  dirtyRef.current = dirtyNames.length > 0;

  // 刷新与关标签页的兜底；站内导航由下面的 UnsavedChangesGuard 接管。
  useEffect(() => {
    const onBeforeUnload = (event: BeforeUnloadEvent) => {
      if (dirtyRef.current) {
        event.preventDefault();
      }
    };
    window.addEventListener("beforeunload", onBeforeUnload);
    return () => window.removeEventListener("beforeunload", onBeforeUnload);
  }, []);

  /*
   * 地址决定展开哪一块：/settings 全部收起，/settings/<分组> 展开那一块并滚过去。
   *
   * **默认一个都不展开**，哪怕是第一组：展开的第一组有十个字段，两屏高，
   * 后面五组全被挤出视口——那就等于没折。第一屏要是一张六行的目录，
   * 「这个站有哪些东西可配」一眼看完，再点进去改。
   */
  useEffect(() => {
    if (groups.length === 0) {
      return;
    }
    const target = groups.find((item) => item.name === group)?.name;
    if (!target) {
      return;
    }
    setOpen({ [target]: true });
    // 布局稳定后再滚：卡片是这一帧才展开的，早滚会落在旧位置上
    requestAnimationFrame(() => {
      document
        .getElementById(sectionAnchor(target))
        ?.scrollIntoView({ block: "start", behavior: "smooth" });
    });
  }, [groups, group]);

  const setValue = useCallback((name: string, path: string, value: unknown) => {
    setSavedNote(null);
    setDrafts((current) => ({
      ...current,
      [name]: setAtPath(current[name] ?? {}, path, value),
    }));
  }, []);

  const markTouched = useCallback((name: string, path: string) => {
    const top = path.split(".")[0] ?? path;
    setTouched((current) => {
      if (current[name]?.[top]) {
        return current;
      }
      return { ...current, [name]: { ...current[name], [top]: true } };
    });
  }, []);

  useDocumentTitle("站点设置");

  if (query.isLoading) {
    return (
      <>
        <PageHeader icon={Settings} title="站点设置" />
        <PageBody>
          <div className="flex flex-col gap-3" aria-busy="true">
            {[0, 1, 2, 3].map((i) => (
              <Skeleton key={i} className="h-14 w-full" />
            ))}
          </div>
        </PageBody>
      </>
    );
  }

  if (query.error) {
    return (
      <>
        <PageHeader icon={Settings} title="站点设置" />
        <PageBody>
          <Card>
            <ErrorState
              message={query.error.message}
              onRetry={() => void query.refetch()}
            />
          </Card>
        </PageBody>
      </>
    );
  }

  if (groups.length === 0) {
    return (
      <>
        <PageHeader icon={Settings} title="站点设置" />
        <PageBody>
          <Card>
            <EmptyState
              icon={SlidersHorizontal}
              title="没有任何设置分组"
              description="设置分组由模块、主题与插件声明；当前没有模块声明过分组。"
            />
          </Card>
        </PageBody>
      </>
    );
  }

  /** 地址点名了一个不存在的分组时说一声，而不是默默展开别的。 */
  const unknownGroup = group && !groups.some((item) => item.name === group);

  const errorCount = groups.reduce(
    (sum, item) =>
      sum +
      Object.keys(errorsByGroup[item.name] ?? {}).length +
      (serverMessages[item.name]?.length ?? 0),
    0,
  );

  async function save() {
    setSubmitted(true);
    setSavedNote(null);

    // 先在本地把脏分组校验一遍：能在这里说清的错，不必先跑一趟服务端
    const invalid = dirtyNames.filter((name) => {
      const item = groups.find((g) => g.name === name);
      if (!item) {
        return false;
      }
      return (
        Object.keys(
          validateGroup(item.schema as GroupSchema, drafts[name] ?? {}),
        ).length > 0
      );
    });
    if (invalid.length > 0) {
      revealGroups(invalid);
      return;
    }

    setSaving(true);
    const nextErrors: Record<string, FieldErrors> = {};
    const nextMessages: Record<string, string[]> = {};
    const ok: string[] = [];
    const failed: string[] = [];

    /*
     * 逐组提交，而不是并发：出错时「哪一组没存上」要说得准，
     * 而并发的六个请求里，失败的那个可能是被另一个的写入挤掉的。
     * 常见情形只有一两组是脏的，串行也快。
     */
    for (const name of dirtyNames) {
      const { error, response } = await api.PUT(
        "/api/v1/console/settings/{group}",
        {
          params: { path: { group: name } },
          body: drafts[name] as Record<string, never>,
        },
      );
      if (response.ok) {
        ok.push(name);
        continue;
      }
      failed.push(name);
      const mapped = errorsFromServer(error?.errors);
      nextErrors[name] = mapped.fields;
      nextMessages[name] =
        mapped.others.length > 0 ? mapped.others : [problemMessage(error)];
    }

    setServerErrors(nextErrors);
    setServerMessages(nextMessages);
    setSaving(false);

    // 成功的组要拿服务端的新值当基线（口令字段的 secretSet 也在这一趟更新）
    await queryClient.invalidateQueries({ queryKey: ["settings"] });

    if (failed.length === 0) {
      setSubmitted(false);
      setTouched({});
      setSavedNote(
        ok.length === 1
          ? `${labelOf(groups, ok[0] as string)}已保存`
          : `已保存 ${ok.length} 项设置`,
      );
      return;
    }
    revealGroups(failed);
  }

  /** 把这些分组展开、滚到第一个上，并把焦点交给错误摘要。 */
  function revealGroups(names: string[]) {
    setOpen((current) => {
      const next = { ...current };
      for (const name of names) {
        next[name] = true;
      }
      return next;
    });
    requestAnimationFrame(() => {
      summaryRef.current?.focus();
      summaryRef.current?.scrollIntoView({
        block: "center",
        behavior: "smooth",
      });
    });
  }

  function discard() {
    setDrafts(baselines);
    setTouched({});
    setSubmitted(false);
    setServerErrors({});
    setServerMessages({});
    setSavedNote(null);
  }

  return (
    <>
      <PageHeader
        icon={Settings}
        title="站点设置"
        description="站点、附件存储、邮件发送、评论、注册与 SEO 都在这一页。点标题展开细节。"
      />

      <PageBody>
        <UnsavedChangesGuard
          when={() => dirtyRef.current}
          title="离开前要先保存吗？"
          consequence={<p>这一页有尚未保存的修改，离开后改动会丢失。</p>}
        />

        <div className="flex flex-col gap-3">
          {unknownGroup ? (
            <Alert tone="warn" title={`没有名为「${group}」的设置分组`}>
              它可能来自一个未启用的模块，或已随模块移除。下面是当前可用的全部分组。
            </Alert>
          ) : null}

          {/*
            错误摘要：整页一份。
            可聚焦、每条点得到出错的字段上，且点了会把那一组展开——
            收起的区块里躺着一个够不着的错误，是这种折叠版面唯一会新增的死路。
          */}
          {errorCount > 0 ? (
            <Alert
              ref={summaryRef}
              tabIndex={-1}
              tone="danger"
              title={`有 ${errorCount} 处需要修改`}
            >
              <ul className="flex flex-col gap-0.5">
                {groups.flatMap((item) => [
                  ...Object.entries(errorsByGroup[item.name] ?? {}).map(
                    ([path, message]) => (
                      <li key={`${item.name}.${path}`}>
                        <button
                          type="button"
                          onClick={() => {
                            setOpen((current) => ({
                              ...current,
                              [item.name]: true,
                            }));
                            requestAnimationFrame(() => focusField(path));
                          }}
                          className="text-left text-danger text-xs underline underline-offset-2"
                        >
                          {item.label} / {fieldLabel(item, path)}：{message}
                        </button>
                      </li>
                    ),
                  ),
                  ...(serverMessages[item.name] ?? []).map((message) => (
                    <li
                      key={`${item.name}!${message}`}
                      className="text-danger text-xs"
                    >
                      {item.label}：{message}
                    </li>
                  )),
                ])}
              </ul>
            </Alert>
          ) : null}

          {savedNote ? (
            <Alert tone="ok" title={savedNote}>
              改动已生效。
            </Alert>
          ) : null}

          {groups.map((item) => {
            const schema = item.schema as GroupSchema;
            const values = drafts[item.name] ?? baselines[item.name] ?? {};
            const toggleKey = item.toggle;
            return (
              <SettingsSection
                key={item.name}
                name={item.name}
                label={item.label}
                description={item.description}
                icon={item.icon}
                open={open[item.name] ?? false}
                onOpenChange={(next) =>
                  setOpen((current) => ({ ...current, [item.name]: next }))
                }
                dirty={dirtyNames.includes(item.name)}
                invalid={
                  Object.keys(errorsByGroup[item.name] ?? {}).length > 0 ||
                  (serverMessages[item.name]?.length ?? 0) > 0
                }
                disabled={saving}
                toggle={
                  toggleKey
                    ? {
                        label: toggleLabel(schema, toggleKey),
                        checked: values[toggleKey] === true,
                        onChange: (checked) =>
                          setValue(item.name, toggleKey, checked),
                      }
                    : undefined
                }
              >
                <SchemaFormFields
                  schema={schema}
                  values={values}
                  errors={errorsByGroup[item.name] ?? {}}
                  disabled={saving}
                  omit={toggleKey ? [toggleKey] : []}
                  onChange={(path, value) => setValue(item.name, path, value)}
                  onBlur={(path) => markTouched(item.name, path)}
                  empty={
                    <p className="text-ink-muted text-sm">
                      开启上面的开关后，这里会出现需要填写的配置。
                    </p>
                  }
                />
                {/*
                  分组专属的附加操作。这里是唯一一处按分组名分支的地方，且刻意如此：
                  为「发送测试邮件」这一个动作设计一套通用的「分组动作」声明，
                  是在为一个尚不存在的需求发明扩展点。等第二个这类动作出现时再抽。
                */}
                {item.name === "mail" ? <MailTest /> : null}
              </SettingsSection>
            );
          })}
        </div>

        {/*
          保存条。只在有改动时出现，吸在视口底部——这一页要滚很久，
          把保存按钮留在文档末尾等于让人每次都先滚到底。
        */}
        {dirtyNames.length > 0 ? (
          <div className="sticky bottom-0 z-sticky -mx-4 mt-3 border-line border-t bg-surface/95 px-4 py-3 backdrop-blur md:-mx-6 md:px-6">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <p className="text-ink-muted text-xs">
                <span className="font-medium text-ink">
                  {dirtyNames.length} 项设置有未保存的改动
                </span>
                <span className="ml-2">
                  {dirtyNames.map((name) => labelOf(groups, name)).join("、")}
                </span>
              </p>
              <div className="flex items-center gap-2">
                <Button variant="secondary" disabled={saving} onClick={discard}>
                  放弃修改
                </Button>
                <Button
                  variant="primary"
                  loading={saving}
                  onClick={() => void save()}
                >
                  {saving ? "正在保存" : "保存修改"}
                </Button>
              </div>
            </div>
          </div>
        ) : null}
      </PageBody>
    </>
  );
}

/** 分组名 → 显示名。 */
function labelOf(groups: GroupView[], name: string): string {
  return groups.find((item) => item.name === name)?.label ?? name;
}

/** 主开关的可读名字，供 aria-label 用。 */
function toggleLabel(schema: GroupSchema, key: string): string {
  const field = schema.properties?.[key];
  return field ? labelFor(field, key) : key;
}

/** 错误摘要里的字段名。 */
function fieldLabel(item: GroupView, path: string): string {
  const schema = item.schema as GroupSchema;
  const top = path.split(".")[0] ?? path;
  const field = schema.properties?.[top];
  return field ? labelFor(field, top) : top;
}

/**
 * 聚焦到出错的字段。
 *
 * `focus({ preventScroll: true })` + 手动 scrollIntoView：浏览器默认的聚焦滚动
 * 会把字段顶到视口最上沿，标签与说明都在屏幕外，用户看到一个孤零零的输入框。
 */
function focusField(path: string) {
  const element = document.getElementById(fieldId(path));
  if (!element) {
    return;
  }
  element.focus({ preventScroll: true });
  element.scrollIntoView({ block: "center", behavior: "smooth" });
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

/**
 * 发送测试邮件。
 *
 * 独立于通用表单：它不改变任何设置，只验证当前配置能否真的发出信。
 * 失败时把 SMTP 的原始报错**原样**显示 —— 那句话里才有排查线索
 * （认证失败、连不上、被拒收是完全不同的三件事），
 * 包装成「发送失败」等于把唯一的线索丢掉。
 */
function MailTest() {
  const [open, setOpen] = useState(false);
  const [to, setTo] = useState("");
  const [result, setResult] = useState<{ ok: boolean; message: string } | null>(
    null,
  );

  const send = useMutation({
    mutationFn: async () => {
      const { data, error, response } = await api.POST(
        "/api/v1/console/mail/test",
        { body: { to } },
      );
      if (!response.ok) {
        throw new Error(problemMessage(error));
      }
      return data;
    },
    onSuccess: (data) => {
      setResult({ ok: true, message: data?.note || "测试邮件已进入发送队列" });
    },
    onError: (error) => {
      setResult({
        ok: false,
        message: error instanceof Error ? error.message : "发送失败",
      });
    },
  });

  return (
    <Card className="mt-6">
      <CardHeader
        title="发送测试邮件"
        description="用当前已保存的配置发一封信，验证 SMTP 是否真的可用。设置本身不受影响。"
        actions={
          <Button
            variant="secondary"
            size="sm"
            onClick={() => {
              setResult(null);
              setOpen(true);
            }}
          >
            <Send aria-hidden="true" />
            发送测试邮件
          </Button>
        }
      />

      {result ? (
        <CardBody>
          <Alert
            tone={result.ok ? "ok" : "danger"}
            title={result.ok ? "已进入发送队列" : "发送失败"}
          >
            <p className="token">{result.message}</p>
            {result.ok ? (
              <p className="mt-1">
                进入队列不等于对方收到。若迟迟未到，请检查服务端日志里的投递结果。
              </p>
            ) : null}
          </Alert>
        </CardBody>
      ) : null}

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent size="sm">
          <form
            onSubmit={async (event) => {
              event.preventDefault();
              await send.mutateAsync().catch(() => {
                // 已记入 result
              });
              setOpen(false);
            }}
          >
            <DialogHeader>
              <DialogTitle>发送测试邮件</DialogTitle>
              <DialogDescription>
                收件地址只用于这一次测试，不会保存。
              </DialogDescription>
            </DialogHeader>
            <DialogBody>
              <Field>
                <FieldLabel htmlFor="mail-test-to">收件地址</FieldLabel>
                <Input
                  id="mail-test-to"
                  type="email"
                  value={to}
                  onChange={(e) => setTo(e.target.value)}
                  placeholder="you@example.com"
                  autoFocus
                  required
                />
              </Field>
            </DialogBody>
            <DialogFooter>
              <Button variant="secondary" onClick={() => setOpen(false)}>
                取消
              </Button>
              <Button
                type="submit"
                variant="primary"
                loading={send.isPending}
                disabled={!to}
              >
                发送
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </Card>
  );
}
