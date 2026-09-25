import { api, problemMessage } from "@/api/client";
import { runMutation } from "@/api/mutation";
import type { components } from "@/api/schema";
import {
  Entity,
  EntityActions,
  EntityEnd,
  EntityField,
  EntityMeta,
  EntityStart,
  ListBody,
  ListEmpty,
  ListToolbar,
} from "@/components/data/entity";
import {
  type FieldErrors,
  type FieldSchema,
  type FormValues,
  type GroupSchema,
  errorsFromServer,
  optionsFor,
} from "@/components/form/schema";
import { SchemaForm } from "@/components/form/schema-form";
import { PageBody, PageHeader } from "@/components/layout/page-header";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardHeader,
  DescriptionDetail,
  DescriptionList,
  DescriptionTerm,
} from "@/components/ui/card";
import {
  ConfirmDialog,
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { DropdownMenuItem } from "@/components/ui/dropdown-menu";
import { SearchInput } from "@/components/ui/input";
import { Pagination } from "@/components/ui/pagination";
import { EmptyState, ErrorState, Skeleton } from "@/components/ui/states";
import { relativeTime } from "@/lib/format";
import { ICONS } from "@/lib/icons";
import { useDocumentTitle } from "@/lib/use-document-title";
import { useDebouncedSearch, useListParams } from "@/lib/use-list-params";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Eye, PenLine, Plus, Puzzle, Trash2 } from "lucide-react";
import { useState } from "react";
import { useParams } from "react-router";

/**
 * 插件资源页：插件在清单里声明一类数据，这一页按声明自动出列表与编辑表单。
 *
 * 字段、列、标题、能不能手工改，全来自声明；表单与站点设置、插件设置是同一个引擎。
 * 地址是 /plugins/<插件>/<资源的复数段>，侧栏入口由插件模块按启用状态现算。
 * 权限由服务端按资源声明的权限串判定，这一页只负责把 403 说清楚。
 */

type ResourceView = components["schemas"]["ResourceView"];
type RecordView = components["schemas"]["ResourceRecord"];

export function PluginResourcePage() {
  const { plugin = "", resource = "" } = useParams();
  const list = useListParams();
  const [search, setSearch] = useDebouncedSearch(list.filter("q"), (value) =>
    list.setFilter("q", value),
  );
  const [editing, setEditing] = useState<RecordView | "new" | null>(null);
  const [removing, setRemoving] = useState<RecordView | null>(null);
  const queryClient = useQueryClient();

  const decls = useQuery({
    queryKey: ["plugin-resources", plugin],
    queryFn: async (): Promise<ResourceView[]> => {
      const { data, error, response } = await api.GET(
        "/api/v1/console/plugins/{name}/resources",
        { params: { path: { name: plugin } } },
      );
      if (!response.ok) {
        throw new Error(problemMessage(error));
      }
      return data?.items ?? [];
    },
  });
  const res = decls.data?.find((item) => item.path === resource);
  useDocumentTitle(res?.label ?? "插件");

  const records = useQuery({
    queryKey: [
      "plugin-records",
      plugin,
      resource,
      list.page,
      list.size,
      list.filters,
    ],
    enabled: res !== undefined,
    queryFn: async () => {
      const { data, error, response } = await api.GET(
        "/api/v1/console/plugins/{name}/resources/{resource}",
        {
          params: {
            path: { name: plugin, resource },
            query: {
              page: list.page,
              size: list.size,
              ...(list.filter("q") ? { q: list.filter("q") } : {}),
            },
          },
        },
      );
      if (!response.ok) {
        throw new Error(problemMessage(error));
      }
      return data;
    },
  });

  const remove = useMutation({
    mutationFn: (id: number) =>
      runMutation(
        () =>
          api.DELETE(
            "/api/v1/console/plugins/{name}/resources/{resource}/{id}",
            { params: { path: { name: plugin, resource, id } } },
          ),
        {
          success: "已删除",
          invalidate: ["plugin-records", plugin, resource],
        },
      ),
    onSuccess: () => setRemoving(null),
  });

  if (decls.isLoading) {
    return (
      <PageBody>
        <Card className="flex flex-col gap-3 p-4" aria-busy="true">
          <Skeleton className="h-8 w-48" />
          <Skeleton className="h-64 w-full" />
        </Card>
      </PageBody>
    );
  }
  if (decls.error || !res) {
    return (
      <PageBody>
        <Card>
          {decls.error ? (
            <ErrorState
              message={decls.error.message}
              onRetry={() => void decls.refetch()}
            />
          ) : (
            <EmptyState
              icon={Puzzle}
              title="没有这个页面"
              description="提供它的插件可能已经停用或卸载，也可能你没有查看它的权限。"
            />
          )}
        </Card>
      </PageBody>
    );
  }

  const Icon = ICONS[res.icon] ?? Puzzle;
  const items = records.data?.items ?? [];
  const noun = res.label;

  return (
    <>
      <PageHeader
        icon={Icon}
        title={noun}
        description={res.description || "由插件提供的数据"}
        actions={
          res.editable ? (
            <Button variant="primary" onClick={() => setEditing("new")}>
              <Plus aria-hidden="true" />
              新建{noun}
            </Button>
          ) : undefined
        }
      />
      <PageBody>
        <Card>
          <CardHeader>
            <ListToolbar
              search={
                res.title ? (
                  <SearchInput
                    value={search}
                    onValueChange={setSearch}
                    placeholder={`按${fieldLabel(res, res.title)}搜索`}
                    aria-label={`搜索${noun}`}
                    className="max-w-xs"
                  />
                ) : undefined
              }
              hasFilters={list.hasFilters}
              onClearFilters={list.reset}
              onRefresh={() => void records.refetch()}
              refreshing={records.isFetching}
            />
          </CardHeader>
          <ListBody
            isLoading={records.isLoading}
            error={records.error}
            onRetry={() => void records.refetch()}
            isEmpty={items.length === 0}
            empty={
              list.hasFilters ? (
                <ListEmpty
                  title={`没有匹配的${noun}`}
                  description="换个关键词试试。"
                  action={
                    <Button variant="secondary" size="sm" onClick={list.reset}>
                      清除搜索
                    </Button>
                  }
                />
              ) : (
                <ListEmpty
                  icon={Icon}
                  title={`还没有${noun}`}
                  description={
                    res.editable
                      ? "新建一条，或者等插件写进来。"
                      : "这类数据由插件自己写入，运行一段时间后会出现在这里。"
                  }
                  action={
                    res.editable ? (
                      <Button
                        variant="secondary"
                        size="sm"
                        onClick={() => setEditing("new")}
                      >
                        <Plus aria-hidden="true" />
                        新建{noun}
                      </Button>
                    ) : undefined
                  }
                />
              )
            }
          >
            {items.map((record) => (
              <RecordRow
                key={record.id}
                res={res}
                record={record}
                onOpen={() => setEditing(record)}
                onRemove={() => setRemoving(record)}
              />
            ))}
          </ListBody>
          <Pagination
            page={list.page}
            size={list.size}
            total={records.data?.total ?? 0}
            onPageChange={list.setPage}
            onSizeChange={list.setSize}
          />
        </Card>
      </PageBody>

      <RecordDialog
        plugin={plugin}
        res={res}
        record={editing}
        onClose={() => setEditing(null)}
        onSaved={async () => {
          setEditing(null);
          await queryClient.invalidateQueries({
            queryKey: ["plugin-records", plugin, resource],
          });
        }}
      />

      <ConfirmDialog
        open={removing !== null}
        onOpenChange={(open) => {
          if (!open) {
            setRemoving(null);
          }
        }}
        title={`删除这条${noun}？`}
        consequence={
          <p>
            「{removing ? recordTitle(res, removing) : ""}」会被删掉，无法撤销。
          </p>
        }
        confirmLabel="删除"
        pending={remove.isPending}
        onConfirm={() => {
          if (removing) {
            remove.mutate(removing.id);
          }
        }}
      />
    </>
  );
}

/** 一条记录：标题字段做标题，其余列摊成一行说明。 */
function RecordRow({
  res,
  record,
  onOpen,
  onRemove,
}: {
  res: ResourceView;
  record: RecordView;
  onOpen: () => void;
  onRemove: () => void;
}) {
  const columns = (res.columns ?? []).filter((column) => column !== res.title);
  return (
    <Entity>
      <EntityStart>
        <EntityField
          title={
            <button
              type="button"
              onClick={onOpen}
              className="truncate text-left hover:text-seal"
            >
              {recordTitle(res, record)}
            </button>
          }
          description={
            columns.length > 0 ? (
              <span className="flex flex-wrap gap-x-4 gap-y-0.5">
                {columns.map((column) => (
                  <span key={column} className="min-w-0 truncate">
                    <span className="text-ink-subtle">
                      {fieldLabel(res, column)}
                    </span>{" "}
                    {formatValue(
                      fieldSchema(res, column),
                      record.data?.[column],
                    )}
                  </span>
                ))}
              </span>
            ) : undefined
          }
        />
      </EntityStart>
      <EntityEnd>
        <EntityMeta>更新于 {relativeTime(record.updatedAt)}</EntityMeta>
        <EntityActions label={`${recordTitle(res, record)} 的更多操作`}>
          <DropdownMenuItem onSelect={onOpen}>
            {res.editable ? (
              <PenLine aria-hidden="true" />
            ) : (
              <Eye aria-hidden="true" />
            )}
            {res.editable ? "编辑" : "查看"}
          </DropdownMenuItem>
          {res.editable ? (
            <DropdownMenuItem danger onSelect={onRemove}>
              <Trash2 aria-hidden="true" />
              删除
            </DropdownMenuItem>
          ) : null}
        </EntityActions>
      </EntityEnd>
    </Entity>
  );
}

/** 带上 422 明细的错误，供表单把每一条落回对应字段。 */
class RecordError extends Error {
  details: components["schemas"]["ErrorDetail"][] | null | undefined;

  constructor(
    message: string,
    details: components["schemas"]["ErrorDetail"][] | null | undefined,
  ) {
    super(message);
    this.name = "RecordError";
    this.details = details;
  }
}

/** 新建、编辑或查看一条记录。只读资源不给表单，给一张清单。 */
function RecordDialog({
  plugin,
  res,
  record,
  onClose,
  onSaved,
}: {
  plugin: string;
  res: ResourceView;
  record: RecordView | "new" | null;
  onClose: () => void;
  onSaved: () => Promise<void>;
}) {
  const [errors, setErrors] = useState<FieldErrors>({});
  const [messages, setMessages] = useState<string[]>([]);
  const creating = record === "new";
  const current = record !== null && record !== "new" ? record : null;

  const save = useMutation({
    mutationFn: async (values: FormValues) => {
      const body = { data: values as Record<string, unknown> };
      const { error, response } = current
        ? await api.PUT(
            "/api/v1/console/plugins/{name}/resources/{resource}/{id}",
            {
              params: {
                path: { name: plugin, resource: res.path, id: current.id },
              },
              body,
            },
          )
        : await api.POST(
            "/api/v1/console/plugins/{name}/resources/{resource}",
            {
              params: { path: { name: plugin, resource: res.path } },
              body,
            },
          );
      if (!response.ok) {
        throw new RecordError(problemMessage(error), error?.errors);
      }
    },
    onSuccess: async () => {
      setErrors({});
      setMessages([]);
      await onSaved();
    },
    onError: (error: Error) => {
      const parsed = errorsFromServer(
        error instanceof RecordError ? error.details : undefined,
      );
      setErrors(parsed.fields);
      setMessages(parsed.others.length > 0 ? parsed.others : [error.message]);
    },
  });

  const title = creating
    ? `新建${res.label}`
    : current
      ? recordTitle(res, current)
      : "";

  return (
    <Dialog
      open={record !== null}
      onOpenChange={(open) => {
        if (!open) {
          setErrors({});
          setMessages([]);
          onClose();
        }
      }}
    >
      <DialogContent size="lg">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>
            {current
              ? `新建于 ${relativeTime(current.createdAt)}，更新于 ${relativeTime(current.updatedAt)}`
              : res.description || "填好字段后保存"}
          </DialogDescription>
        </DialogHeader>
        <DialogBody>
          {res.editable ? (
            <SchemaForm
              schema={res.schema as GroupSchema}
              values={(current?.data ?? res.defaults) as FormValues}
              serverErrors={errors}
              serverMessages={messages}
              submitLabel={creating ? "新建" : "保存"}
              disableWhenPristine={!creating}
              stacked
              onSubmit={async (values) => {
                await save.mutateAsync(values);
              }}
            />
          ) : current ? (
            <DescriptionList>
              {Object.keys((res.schema as GroupSchema).properties ?? {}).map(
                (key) => (
                  <FieldRow
                    key={key}
                    res={res}
                    field={key}
                    value={current.data?.[key]}
                  />
                ),
              )}
            </DescriptionList>
          ) : null}
        </DialogBody>
      </DialogContent>
    </Dialog>
  );
}

function FieldRow({
  res,
  field,
  value,
}: {
  res: ResourceView;
  field: string;
  value: unknown;
}) {
  return (
    <>
      <DescriptionTerm>{fieldLabel(res, field)}</DescriptionTerm>
      <DescriptionDetail className="break-words">
        {formatValue(fieldSchema(res, field), value)}
      </DescriptionDetail>
    </>
  );
}

function fieldSchema(
  res: ResourceView,
  field: string,
): FieldSchema | undefined {
  return (res.schema as GroupSchema).properties?.[field];
}

function fieldLabel(res: ResourceView, field: string): string {
  return fieldSchema(res, field)?.title?.trim() || field;
}

function recordTitle(res: ResourceView, record: RecordView): string {
  const value = res.title ? record.data?.[res.title] : undefined;
  const text = typeof value === "string" ? value.trim() : "";
  return text || `#${record.id}`;
}

/** 把字段值写成一行给人看的字。 */
function formatValue(schema: FieldSchema | undefined, value: unknown): string {
  if (value === undefined || value === null || value === "") {
    return "—";
  }
  if (typeof value === "boolean") {
    return value ? "是" : "否";
  }
  if (schema?.enum) {
    const option = optionsFor(schema).find(
      (item) => item.value === String(value),
    );
    return option?.label ?? String(value);
  }
  if (Array.isArray(value)) {
    return value.every((item) => typeof item !== "object")
      ? value.join("、")
      : `${value.length} 项`;
  }
  if (typeof value === "object") {
    return "（多个字段）";
  }
  return String(value);
}
