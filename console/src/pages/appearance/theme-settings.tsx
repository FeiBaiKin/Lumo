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
import { ErrorState, Skeleton } from "@/components/ui/states";
import { cn } from "@/lib/utils";
import { useMutation, useQuery } from "@tanstack/react-query";
import { useState } from "react";

/**
 * 主题设置。
 *
 * **与站点设置共用同一个表单引擎** —— 这正是 agent.md §5 定那套声明式 Schema、
 * 并在阶段 4 让主题通过 `settings.yaml` 声明设置项的全部理由。
 * 主题设置与站点设置的唯一差别是数据来源（安装时编译 vs 启动期登记）
 * 与存储位置（theme_settings vs settings 两张表），
 * 而这两件事都发生在服务端，前端一行都不用区分。
 *
 * 主题随时可换，故这里的分组用标签栏切换而不是一个长表单：
 * 一个主题声明十几个字段时，分组是唯一能让页面可读的结构。
 */

type ThemeView = components["schemas"]["View"];

type GroupView = components["schemas"]["SettingsGroupView"];

export function ThemeSettings({
  theme,
  onClose,
}: {
  theme: ThemeView;
  onClose: () => void;
}) {
  const [activeGroup, setActiveGroup] = useState<string | null>(null);

  const query = useQuery({
    queryKey: ["theme-settings", theme.name],
    queryFn: async () => {
      const { data, response } = await api.GET(
        "/api/v1/console/themes/{name}/settings",
        { params: { path: { name: theme.name } } },
      );
      if (!response.ok) {
        throw new Error(`载入主题设置失败（HTTP ${response.status}）`);
      }
      return data?.items ?? [];
    },
  });

  const groups: GroupView[] = query.data ?? [];
  const current = groups.find((g) => g.name === activeGroup) ?? groups[0];

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-w-3xl">
        <DialogHeader>
          <DialogTitle>{theme.label || theme.name} · 设置</DialogTitle>
          <DialogDescription>
            这些字段由主题的 settings.yaml 声明，表单由本站的通用引擎按 Schema
            生成。
          </DialogDescription>
        </DialogHeader>

        <DialogBody>
          {query.isLoading ? (
            <div className="flex flex-col gap-3">
              <Skeleton className="h-8 w-64" />
              <Skeleton className="h-64 w-full" />
            </div>
          ) : query.error ? (
            <ErrorState
              message={query.error.message}
              onRetry={() => void query.refetch()}
            />
          ) : groups.length === 0 ? (
            <p className="py-8 text-center text-sm text-ink-muted">
              这个主题没有声明任何设置项。
            </p>
          ) : (
            <>
              {/* 只有一个分组时不显示标签栏 —— 一个标签的标签栏只是噪音 */}
              {groups.length > 1 ? (
                <nav
                  aria-label="主题设置分组"
                  className="mb-4 flex flex-wrap items-center gap-1 border-line border-b"
                >
                  {groups.map((group) => {
                    const isActive = group.name === current?.name;
                    return (
                      <button
                        key={group.name}
                        type="button"
                        onClick={() => setActiveGroup(group.name)}
                        aria-current={isActive ? "true" : undefined}
                        className={cn(
                          "transition-ui -mb-px rounded-t-control border-b-2 px-3 py-2 text-base",
                          isActive
                            ? "border-seal font-medium text-seal"
                            : "border-transparent text-ink-muted hover:bg-surface-hover hover:text-ink",
                        )}
                      >
                        {group.label || group.name}
                      </button>
                    );
                  })}
                </nav>
              ) : null}

              {current ? (
                <ThemeGroupForm
                  key={`${theme.name}:${current.name}`}
                  themeName={theme.name}
                  group={current}
                />
              ) : null}
            </>
          )}
        </DialogBody>

        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>
            关闭
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
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
