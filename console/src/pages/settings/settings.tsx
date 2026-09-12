import { api, problemMessage } from "@/api/client";
import type { components } from "@/api/schema";
import {
  type FieldErrors,
  type FormValues,
  type GroupSchema,
  errorsFromServer,
} from "@/components/form/schema";
import { SchemaForm } from "@/components/form/schema-form";
import { PageBody, PageHeader } from "@/components/layout/page-header";
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
import { Tabbar } from "@/components/ui/tabs";
import { useDocumentTitle } from "@/lib/use-document-title";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Send, Settings, SlidersHorizontal } from "lucide-react";
import { useMemo, useState } from "react";
import { useParams } from "react-router";

/**
 * 设置页（形态对齐 Halo 的 SystemSettings：一张卡片，标题栏是分组标签栏）。
 *
 * **一个页面渲染所有分组**，而不是给 site / seo / mail / storage 各写一个页面。
 * 这正是 agent.md §5 定那套声明式 Schema 的目的：新增一个设置分组
 * （模块启动时登记，或主题安装时声明）不该需要动前端一行代码。
 *
 * 标签栏由接口返回的分组列表生成，不是写死的四个 ——
 * 写死的话，comment 分组就永远没有入口，而它在后端是存在的。
 */

type GroupView = components["schemas"]["GroupView"];

export function SettingsPage({ defaultGroup }: { defaultGroup?: string } = {}) {
  const { group } = useParams<{ group: string }>();
  const queryClient = useQueryClient();

  const [errors, setErrors] = useState<FieldErrors>({});
  const [messages, setMessages] = useState<string[]>([]);

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

  const groups = query.data ?? [];
  // 地址里没给分组时用 defaultGroup（`/settings` 这个入口），再退回第一个分组。
  // 用「第一个分组」兜底而不是报错：分组的集合由后端模块决定，
  // 前端写死一个名字会在模块被移除时指向一个不存在的分组。
  const wanted = group ?? defaultGroup;
  const current = useMemo(
    () =>
      groups.find((item) => item.name === wanted) ??
      (group ? undefined : groups[0]),
    [groups, wanted, group],
  );

  const update = useMutation({
    mutationFn: async (values: FormValues) => {
      const { error, response } = await api.PUT(
        "/api/v1/console/settings/{group}",
        {
          params: { path: { group: current?.name ?? "" } },
          // 请求体是自由对象（服务端按 additionalProperties: false 校验），
          // 故这里传的就是表单里的全部字段，不做裁剪。
          body: values,
        },
      );
      if (!response.ok) {
        throw new SettingsError(problemMessage(error), error?.errors);
      }
      return values;
    },
    onSuccess: () => {
      setErrors({});
      setMessages([]);
      void queryClient.invalidateQueries({ queryKey: ["settings"] });
    },
  });

  useDocumentTitle(current ? current.label : "设置");

  if (query.isLoading) {
    return (
      <>
        <PageHeader icon={Settings} title="设置" />
        <PageBody>
          <Card>
            <CardBody className="flex flex-col gap-4" aria-busy="true">
              <Skeleton className="h-9 w-80" />
              <Skeleton className="h-80 w-full max-w-2xl" />
            </CardBody>
          </Card>
        </PageBody>
      </>
    );
  }

  if (query.error) {
    return (
      <>
        <PageHeader icon={Settings} title="设置" />
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

  if (!current) {
    return (
      <>
        <PageHeader icon={Settings} title="设置" />
        <PageBody>
          <Card>
            <CardHeader>
              <GroupTabs groups={groups} active={group} />
            </CardHeader>
            <EmptyState
              icon={SlidersHorizontal}
              title={`没有名为「${group}」的设置分组`}
              description="它可能来自一个未启用的模块。上方是当前可用的分组。"
            />
          </Card>
        </PageBody>
      </>
    );
  }

  const schema = current.schema as GroupSchema;
  const values = current.values as FormValues;

  return (
    <>
      <PageHeader
        icon={Settings}
        title="设置"
        description={current.description}
      />

      <PageBody>
        <Card>
          <CardHeader>
            <GroupTabs groups={groups} active={current.name} />
          </CardHeader>
          <CardBody>
            <div className="max-w-2xl">
              <SchemaForm
                // key 让切换分组时表单整体重建：否则上一组的 touched / submitted
                // 状态会带到下一组，出现「刚打开就满屏红字」
                key={current.name}
                schema={schema}
                values={values}
                serverErrors={errors}
                serverMessages={messages}
                onSubmit={async (next) => {
                  setErrors({});
                  setMessages([]);
                  try {
                    await update.mutateAsync(next);
                  } catch (err) {
                    if (err instanceof SettingsError) {
                      // 服务端才是权威校验方：把它的 422 明细落回对应字段
                      const mapped = errorsFromServer(err.details);
                      setErrors(mapped.fields);
                      setMessages(mapped.others);
                    } else {
                      setMessages([
                        err instanceof Error ? err.message : "保存失败",
                      ]);
                    }
                    throw err;
                  }
                }}
              />
            </div>
          </CardBody>
        </Card>

        {/*
          分组专属的附加操作。
          这里是**唯一**一处按分组名分支的地方，且刻意如此：
          为「发送测试邮件」这一个动作设计一套通用的「分组动作」声明，
          是在为一个尚不存在的需求发明扩展点。等第二个这类动作出现时再抽。
        */}
        {current.name === "mail" ? <MailTestCard /> : null}
      </PageBody>
    </>
  );
}

/** 保存失败时携带的服务端明细。 */
class SettingsError extends Error {
  details: components["schemas"]["ErrorDetail"][] | null | undefined;

  constructor(
    message: string,
    details: components["schemas"]["ErrorDetail"][] | null | undefined,
  ) {
    super(message);
    this.name = "SettingsError";
    this.details = details;
  }
}

/**
 * 分组标签栏。
 *
 * 用链接而不是按钮：每个分组有自己的地址（`/settings/site`），
 * 这样刷新与分享链接都能回到同一处。用按钮就需要自己同步地址，反而更绕。
 * 只有一个分组时不显示 —— 一个标签的标签栏只是噪音。
 */
function GroupTabs({
  groups,
  active,
}: {
  groups: GroupView[];
  active: string | undefined;
}) {
  if (groups.length <= 1) {
    return null;
  }
  return (
    <Tabbar
      ariaLabel="设置分组"
      value={active ?? ""}
      items={groups.map((item) => ({
        value: item.name,
        label: item.label,
        to: `/settings/${item.name}`,
      }))}
      // 卡片标题栏自带底线，标签栏的底线去掉；pb-px 给激活项那 2px 底线留出落脚处
      className="w-full border-b-0 px-2 pb-px"
    />
  );
}

/**
 * 发送测试邮件。
 *
 * 独立于通用表单：它不改变任何设置，只验证当前配置能否真的发出信。
 * 失败时把 SMTP 的原始报错**原样**显示 —— 那句话里才有排查线索
 * （认证失败、连不上、被拒收是完全不同的三件事），
 * 包装成「发送失败」等于把唯一的线索丢掉。
 */
function MailTestCard() {
  const [open, setOpen] = useState(false);
  const [to, setTo] = useState("");
  const [result, setResult] = useState<{ ok: boolean; message: string } | null>(
    null,
  );

  const send = useMutation({
    mutationFn: async () => {
      const { data, error, response } = await api.POST(
        "/api/v1/console/mail/test",
        {
          body: { to },
        },
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
    <Card>
      <CardHeader
        title="发送测试邮件"
        description="用当前配置发一封信，验证 SMTP 是否真的可用。设置本身不受影响。"
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
