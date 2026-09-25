import { api, problemMessage } from "@/api/client";
import { queryClient } from "@/api/query-client";
import type { components } from "@/api/schema";
import {
  type FieldErrors,
  type FormValues,
  type GroupSchema,
  errorsFromServer,
} from "@/components/form/schema";
import { SchemaForm } from "@/components/form/schema-form";
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { EmptyState, Skeleton } from "@/components/ui/states";
import { Tabbar } from "@/components/ui/tabs";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Puzzle } from "lucide-react";
import { useMemo, useState } from "react";
import { toast } from "sonner";

/**
 * 插件的设置面板。
 *
 * 渲染路径与站点设置、主题设置完全相同 —— 同一份 Schema 格式、同一个表单引擎。
 * 这是声明式设置格式的全部理由：三处「设置」如果各写一套界面，
 * 站长就要学三遍，插件作者也要多学一遍。
 *
 * 与主题设置的另一个共同点：插件的设置是**插件作用域**的，不进全局设置服务。
 * 它的生命周期跟着插件走，停用时整组收起，卸载时由站长决定删掉还是保留（见插件页的卸载确认）。
 */

type GroupView = components["schemas"]["PluginSettingsView"];

/** 带上 422 明细的错误，供表单把每一条落回对应字段。 */
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

export function PluginSettingsDialog({
  plugin,
  open,
  onOpenChange,
}: {
  /** 插件的标识；为 null 时查询不发。 */
  plugin: string | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const [group, setGroup] = useState<string | null>(null);
  const [errors, setErrors] = useState<FieldErrors>({});
  const [messages, setMessages] = useState<string[]>([]);

  const query = useQuery({
    queryKey: ["plugin-settings", plugin],
    enabled: plugin !== null,
    queryFn: async (): Promise<GroupView[]> => {
      const { data, error, response } = await api.GET(
        "/api/v1/console/plugins/{name}/settings",
        { params: { path: { name: plugin ?? "" } } },
      );
      if (!response.ok) {
        throw new Error(problemMessage(error));
      }
      return data?.items ?? [];
    },
  });

  // 分组顺序由声明里的 order 决定，服务端已排好；这里只做缺省选中。
  const groups = useMemo(() => query.data ?? [], [query.data]);
  const current = groups.find((item) => item.name === group) ?? groups[0];

  const save = useMutation({
    mutationFn: async (values: FormValues) => {
      if (!plugin || !current) {
        return;
      }
      const { error, response } = await api.PUT(
        "/api/v1/console/plugins/{name}/settings/{group}",
        {
          params: { path: { name: plugin, group: current.name } },
          body: values,
        },
      );
      if (!response.ok) {
        // 服务端的 422 明细要能落回字段：服务端才是权威校验方，
        // 它的错误定位不到字段时，用户只能看到一句「校验失败」然后自己猜。
        throw new SettingsError(problemMessage(error), error?.errors);
      }
    },
    onSuccess: async () => {
      setErrors({});
      setMessages([]);
      await queryClient.invalidateQueries({
        queryKey: ["plugin-settings", plugin],
      });
      toast.success("设置已保存");
    },
    onError: (error: Error) => {
      const details =
        error instanceof SettingsError ? error.details : undefined;
      const parsed = errorsFromServer(details);
      setErrors(parsed.fields);
      setMessages(parsed.others);
    },
  });

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        onOpenChange(next);
        if (!next) {
          setGroup(null);
          setErrors({});
          setMessages([]);
        }
      }}
    >
      <DialogContent size="lg">
        <DialogHeader>
          <DialogTitle>{plugin} 的设置</DialogTitle>
          <DialogDescription>
            设置随插件走。停用插件后这一页会收起；卸载时可以选择保留，重装后接回。
          </DialogDescription>
        </DialogHeader>
        <DialogBody className="flex flex-col gap-4">
          {query.isLoading ? (
            <div className="flex flex-col gap-3" aria-busy="true">
              <Skeleton className="h-9 w-64" />
              <Skeleton className="h-72 w-full" />
            </div>
          ) : groups.length === 0 ? (
            <EmptyState
              icon={Puzzle}
              title="这个插件没有设置项"
              description="它的插件包里没有 settings.yaml，或者声明里没有分组。"
            />
          ) : (
            <>
              {/* 只有一个分组时不画标签栏：一个标签的标签栏是纯噪音 */}
              {groups.length > 1 ? (
                <Tabbar
                  ariaLabel="设置分组"
                  items={groups.map((item) => ({
                    value: item.name,
                    label: item.label,
                  }))}
                  value={current?.name ?? ""}
                  onChange={(next) => {
                    setGroup(next);
                    setErrors({});
                    setMessages([]);
                  }}
                />
              ) : null}

              {current ? (
                <SchemaForm
                  // 换分组要重建表单状态，否则会把上一组的值带过去
                  key={current.name}
                  schema={current.schema as GroupSchema}
                  values={current.values}
                  serverErrors={errors}
                  serverMessages={messages}
                  onSubmit={async (values) => {
                    await save.mutateAsync(values);
                  }}
                />
              ) : null}
            </>
          )}
        </DialogBody>
      </DialogContent>
    </Dialog>
  );
}
