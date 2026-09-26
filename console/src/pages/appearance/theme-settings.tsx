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
import { Card } from "@/components/ui/card";
import { EmptyState, ErrorState, Skeleton } from "@/components/ui/states";
import { Tabbar } from "@/components/ui/tabs";
import { useDocumentTitle } from "@/lib/use-document-title";
import { themeSettingsPath, useThemes } from "@/pages/appearance/themes";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Palette, SlidersHorizontal } from "lucide-react";
import { useState } from "react";
import { useParams } from "react-router";
import { toast } from "sonner";

/**
 * 主题设置。
 *
 * **与站点设置共用同一个表单引擎** —— 这正是声明式 Schema、
 * 并在阶段 4 让主题通过 `settings.yaml` 声明设置项的全部理由。
 * 主题设置与站点设置的唯一差别是数据来源（安装时编译 vs 启动期登记）
 * 与存储位置（theme_settings vs settings 两张表），
 * 而这两件事都发生在服务端，前端一行都不用区分。
 *
 * 主题页只负责挑主题，设置是单独一页，
 * 每个分组一个标签，各有地址（/themes/<主题>/<分组>），刷新与分享都回到同一处。
 */

type ThemeView = components["schemas"]["View"];
type GroupView = components["schemas"]["SettingsGroupView"];

/** 主题设置页。不带分组的地址落在第一组。 */
export function ThemeSettingsPage() {
  const { name = "", group } = useParams<{ name: string; group: string }>();
  const themes = useThemes();
  const theme = themes.data?.items?.find((item) => item.name === name);
  const settings = useThemeSettings(name, theme !== undefined);
  const label = theme ? theme.label || theme.name : name;
  useDocumentTitle("主题设置");

  const groups = theme?.settingGroups ?? [];
  const current = group ?? groups[0] ?? "";
  // 分组的显示名来自设置接口；没拿到之前先用分组名顶着
  const groupLabel = (value: string) =>
    (settings.data ?? []).find((item) => item.name === value)?.label || value;

  return (
    <>
      <PageHeader
        back={{ to: "/themes", label: "返回主题" }}
        title="主题设置"
        description={
          theme && !theme.active
            ? `正在设置「${label}」。它还没启用，保存的值在启用后生效`
            : `正在设置「${label}」，保存后前台立即生效`
        }
      />
      <PageBody>
        {themes.isLoading ? (
          <Card className="flex flex-col gap-3 p-4" aria-busy="true">
            <Skeleton className="h-8 w-64" />
            <Skeleton className="h-64 w-full" />
          </Card>
        ) : themes.error ? (
          <Card>
            <ErrorState
              message={themes.error.message}
              onRetry={() => void themes.refetch()}
            />
          </Card>
        ) : !theme ? (
          <Card>
            <EmptyState
              icon={Palette}
              title="没有这个主题"
              description="它可能已被卸载。回到主题页看看已安装的主题。"
            />
          </Card>
        ) : groups.length === 0 ? (
          <Card>
            <EmptyState
              icon={SlidersHorizontal}
              title="这个主题没有设置项"
              description="它的 settings.yaml 没有声明任何分组，外观全由模板决定。"
            />
          </Card>
        ) : (
          <Card>
            <Tabbar
              ariaLabel="设置分组"
              items={groups.map((item) => ({
                value: item,
                label: groupLabel(item),
                to: `${themeSettingsPath(theme.name)}/${encodeURIComponent(item)}`,
              }))}
              value={current}
              className="px-2"
            />
            <ThemeSettingsPanel
              key={`${theme.name}:${current}`}
              theme={theme}
              group={current}
            />
          </Card>
        )}
      </PageBody>
    </>
  );
}

/** 某个主题的全部设置分组：标签栏取分组的显示名，面板取 Schema 与当前值。 */
function useThemeSettings(name: string, enabled = true) {
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

/** 某个主题的某个设置分组，渲染在设置页的标签下。 */
function ThemeSettingsPanel({
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
     * 不给这层面板封顶：主题设置与站点设置是同一个引擎、同一套排版，都铺满工作区；
     * 窄的时候由表单自己的容器查询收回单列。
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
