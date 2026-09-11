import { api, problemMessage } from "@/api/client";
import type { components } from "@/api/schema";
import {
  type FieldErrors,
  type FormValues,
  type GroupSchema,
  errorsFromServer,
} from "@/components/form/schema";
import { SchemaForm } from "@/components/form/schema-form";
import { Button } from "@/components/ui/button";
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
import { PageHeader } from "@/components/ui/panel";
import { ErrorState, Skeleton } from "@/components/ui/states";
import { useDocumentTitle } from "@/lib/use-document-title";
import { cn } from "@/lib/utils";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Mail, RefreshCw, Send } from "lucide-react";
import { useMemo, useState } from "react";
import { Link, useParams } from "react-router";

/**
 * 设置页。
 *
 * **一个页面渲染所有分组**，而不是给 site / seo / mail / storage 各写一个页面。
 * 这正是 agent.md §5 定那套声明式 Schema 的目的：新增一个设置分组
 * （模块启动时登记，或主题安装时声明）不该需要动前端一行代码。
 *
 * 标签栏由接口返回的分组列表生成，不是写死的四个 ——
 * 写死的话，comment 分组就永远没有入口，而它在后端是存在的。
 */

type GroupView = components["schemas"]["GroupView"];

const GROUP_ICONS: Record<string, typeof Mail> = {
  mail: Mail,
};

export function SettingsPage() {
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
  const current = useMemo(
    () => groups.find((item) => item.name === group),
    [groups, group],
  );

  const update = useMutation({
    mutationFn: async (values: FormValues) => {
      const { error, response } = await api.PUT(
        "/api/v1/console/settings/{group}",
        {
          params: { path: { group: group ?? "" } },
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
        <PageHeader title="设置" />
        <div className="flex flex-col gap-3">
          <Skeleton className="h-9 w-96" />
          <Skeleton className="h-96 w-full" />
        </div>
      </>
    );
  }

  if (query.error) {
    return (
      <>
        <PageHeader title="设置" />
        <ErrorState
          message={query.error.message}
          onRetry={() => void query.refetch()}
        />
      </>
    );
  }

  if (!current) {
    return (
      <>
        <PageHeader title="设置" />
        <div className="flex flex-col items-center gap-3 rounded-panel border border-line border-dashed bg-surface px-6 py-16 text-center">
          <p className="text-sm font-medium text-ink">
            没有名为「{group}」的设置分组
          </p>
          <p className="text-sm text-ink-muted">
            它可能来自一个未启用的模块。下面是当前可用的分组。
          </p>
          <GroupTabs groups={groups} active={group} />
        </div>
      </>
    );
  }

  const schema = current.schema as GroupSchema;
  const values = current.values as FormValues;

  return (
    <>
      <PageHeader title={current.label} description={current.description} />

      <GroupTabs groups={groups} active={group} />

      <div className="mt-5 max-w-3xl rounded-panel border border-line bg-surface p-5">
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
                setMessages([err instanceof Error ? err.message : "保存失败"]);
              }
              throw err;
            }
          }}
        />
      </div>

      {/*
        分组专属的附加操作。
        这里是**唯一**一处按分组名分支的地方，且刻意如此：
        为「发送测试邮件」这一个动作设计一套通用的「分组动作」声明，
        是在为一个尚不存在的需求发明扩展点（agent.md §12 的同一取舍，
        参见 STATUS.md 里对 Hook 签名未定稿的说明）。
        等第二个这类动作出现时再抽。
      */}
      {current.name === "mail" ? <MailTestPanel /> : null}
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
 */
function GroupTabs({
  groups,
  active,
}: { groups: GroupView[]; active: string | undefined }) {
  if (groups.length <= 1) {
    return null;
  }
  return (
    <nav
      aria-label="设置分组"
      className="flex flex-wrap items-center gap-1 border-line border-b"
    >
      {groups.map((item) => {
        const Icon = GROUP_ICONS[item.name];
        const isActive = item.name === active;
        return (
          <Link
            key={item.name}
            to={`/settings/${item.name}`}
            aria-current={isActive ? "page" : undefined}
            className={cn(
              "transition-ui -mb-px flex items-center gap-2 rounded-t-control border-b-2 px-3 py-2 text-base",
              isActive
                ? "border-seal font-medium text-seal"
                : "border-transparent text-ink-muted hover:bg-surface-hover hover:text-ink",
            )}
          >
            {Icon ? <Icon aria-hidden="true" className="size-4" /> : null}
            {item.label}
          </Link>
        );
      })}
    </nav>
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
function MailTestPanel() {
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
    <div className="mt-5 max-w-3xl rounded-panel border border-line bg-surface p-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex flex-col">
          <h2 className="text-lg font-semibold text-ink">发送测试邮件</h2>
          <p className="text-xs text-ink-muted">
            用当前配置发一封信，验证 SMTP 是否真的可用。设置本身不受影响。
          </p>
        </div>
        <Button
          variant="secondary"
          onClick={() => {
            setResult(null);
            setOpen(true);
          }}
        >
          <Send aria-hidden="true" />
          发送测试邮件
        </Button>
      </div>

      {result ? (
        // <output> 是 role="status" 的原生元素，读屏会在发送结果出现时播报它
        <output
          className={cn(
            "mt-4 block rounded-control border px-3 py-2 text-sm",
            result.ok
              ? "border-ok bg-ok-soft text-ok"
              : "border-danger bg-danger-soft text-danger",
          )}
        >
          <p className="token">{result.message}</p>
          {result.ok ? (
            <p className="mt-1 text-xs">
              注意：进入队列不等于对方收到。若迟迟未到，请检查服务端日志里的投递结果。
            </p>
          ) : null}
        </output>
      ) : null}

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="max-w-sm">
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
                disabled={send.isPending || !to}
              >
                {send.isPending ? (
                  <RefreshCw aria-hidden="true" className="animate-spin" />
                ) : null}
                {send.isPending ? "正在发送" : "发送"}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  );
}
