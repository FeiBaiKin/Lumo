import { api, problemMessage } from "@/api/client";
import type { components } from "@/api/schema";
import {
  type FieldErrors,
  type FormValues,
  type GroupSchema,
  errorsFromServer,
} from "@/components/form/schema";
import { SchemaForm } from "@/components/form/schema-form";
import { EmptyState, ErrorState, Skeleton } from "@/components/ui/states";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { SlidersHorizontal } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";

/**
 * 主题设置。
 *
 * **与站点设置共用同一个表单引擎** —— 这正是 agent.md §5 定那套声明式 Schema、
 * 并在阶段 4 让主题通过 `settings.yaml` 声明设置项的全部理由。
 * 主题设置与站点设置的唯一差别是数据来源（安装时编译 vs 启动期登记）
 * 与存储位置（theme_settings vs settings 两张表），
 * 而这两件事都发生在服务端，前端一行都不用区分。
 *
 * 形态对齐 Halo：设置不再是弹窗，而是主题详情卡片里的一个标签页，
 * 每个分组一个标签（标签栏由主题页渲染，本文件只负责某一个分组的表单）。
 */

type ThemeView = components["schemas"]["View"];
type GroupView = components["schemas"]["SettingsGroupView"];

/** 某个主题的全部设置分组。主题页用它取分组的显示名，面板用它取 Schema 与当前值。 */
export function useThemeSettings(name: string, enabled = true) {
  return useQuery({
    queryKey: ["theme-settings", name],
    enabled: enabled && name !== "",
    queryFn: async () => {
      const { data, response } = await api.GET(
        "/api/v1/console/themes/{name}/settings",
        { params: { path: { name } } },
      );
      if (!response.ok) {
        throw new Error(`载入主题设置失败（HTTP ${response.status}）`);
      }
      return data?.items ?? [];
    },
  });
}

/** 某个主题的某个设置分组：内联渲染在主题页的标签下。 */
export function ThemeSettingsPanel({
  theme,
  group,
}: {
  theme: ThemeView;
  group: string;
}) {
  const query = useThemeSettings(theme.name);

  if (query.isLoading) {
    return (
      <div className="flex flex-col gap-3 p-4" aria-busy="true">
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-64 w-full" />
      </div>
    );
  }

  if (query.error) {
    return (
      <ErrorState
        message={query.error.message}
        onRetry={() => void query.refetch()}
      />
    );
  }

  const current: GroupView | undefined = (query.data ?? []).find(
    (item) => item.name === group,
  );

  if (!current) {
    return (
      <EmptyState
        icon={SlidersHorizontal}
        title="没有这个设置分组"
        description="它可能已随主题更新被移除。切到别的标签看看。"
      />
    );
  }

  return (
    /*
     * 不再给这层面板封顶：主题设置与站点设置是同一个引擎、同一套排版，
     * 站点设置已经铺满工作区，主题设置被卡在 48rem 就会显得两半不一样。
     * 窄的时候（`lg` 以下主题列表折到上方）由表单自己的容器查询收回单列。
     */
    <div className="flex flex-col gap-4 p-4">
      {current.description ? (
        <p className="text-sm text-ink-muted">{current.description}</p>
      ) : null}
      <ThemeGroupForm
        key={`${theme.name}:${current.name}`}
        themeName={theme.name}
        group={current}
      />
    </div>
  );
}

/** 一个分组的表单。与站点设置走的是同一个 SchemaForm。 */
function ThemeGroupForm({
  themeName,
  group,
}: {
  themeName: string;
  group: GroupView;
}) {
  const queryClient = useQueryClient();
  const [errors, setErrors] = useState<FieldErrors>({});
  const [messages, setMessages] = useState<string[]>([]);

  const save = useMutation({
    mutationFn: async (values: FormValues) => {
      const { error, response } = await api.PUT(
        "/api/v1/console/themes/{name}/settings/{group}",
        {
          params: { path: { name: themeName, group: group.name } },
          body: values,
        },
      );
      if (!response.ok) {
        throw new ThemeSettingsError(problemMessage(error), error?.errors);
      }
    },
    onSuccess: () => {
      setErrors({});
      setMessages([]);
      toast.success("主题设置已保存");
      // 重取后表单的基线变成刚保存的值，「有未保存的修改」才会消失
      void queryClient.invalidateQueries({
        queryKey: ["theme-settings", themeName],
      });
    },
  });

  return (
    <SchemaForm
      schema={group.schema as GroupSchema}
      values={group.values as FormValues}
      serverErrors={errors}
      serverMessages={messages}
      onSubmit={async (values) => {
        setErrors({});
        setMessages([]);
        try {
          await save.mutateAsync(values);
        } catch (err) {
          if (err instanceof ThemeSettingsError) {
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
  );
}

/** 保存失败时携带的服务端明细，用于把 422 落回字段。 */
class ThemeSettingsError extends Error {
  details: components["schemas"]["ErrorDetail"][] | null | undefined;

  constructor(
    message: string,
    details: components["schemas"]["ErrorDetail"][] | null | undefined,
  ) {
    super(message);
    this.name = "ThemeSettingsError";
    this.details = details;
  }
}
