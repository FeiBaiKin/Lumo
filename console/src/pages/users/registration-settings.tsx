import { api, problemMessage } from "@/api/client";
import type { components } from "@/api/schema";
import {
  type FieldErrors,
  type FormValues,
  type GroupSchema,
  errorsFromServer,
} from "@/components/form/schema";
import { SchemaForm } from "@/components/form/schema-form";
import { Alert } from "@/components/ui/alert";
import { Card, CardBody, CardHeader } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/states";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

/**
 * 注册设置（前端账户分组的表单，承载在用户页上）。
 *
 * 服务端把这个设置分组声明为 `Hidden`（见 internal/app.SettingGroup），
 * 侧边栏因此不再有「前台账户」这一项——它说的「开放注册」与这一页说的是同一件事：
 * 谁能成为用户。分成两个入口，站长得在两个都像、又都不完全对的地方之间找。
 * 分组本身没有消失：/settings/account 仍然打得开，公开字段白名单也照旧生效。
 *
 * 这里为什么不去复用设置页那个组件：那一页是「一次取回全部分组、在客户端切」，
 * 而这张卡片只有一个固定的分组。把它的取数方式搬过来意味着要在这页把全部设置读一遍，
 * 只为显示一个开关。两处共用的是 schema-form 与错误映射，那才是该共用的部分。
 */

type GroupView = components["schemas"]["SettingsGroupView"];
type ErrorDetail = components["schemas"]["ErrorDetail"];

/** 保存失败时携带的服务端明细。 */
class SettingsError extends Error {
  details: ErrorDetail[] | null | undefined;

  constructor(message: string, details: ErrorDetail[] | null | undefined) {
    super(message);
    this.name = "SettingsError";
    this.details = details;
  }
}

export function RegistrationSettings() {
  const queryClient = useQueryClient();
  const [errors, setErrors] = useState<FieldErrors>({});
  const [messages, setMessages] = useState<string[]>([]);

  const query = useQuery({
    queryKey: ["settings", "account"],
    queryFn: async () => {
      const { data, response } = await api.GET(
        "/api/v1/console/settings/{group}",
        { params: { path: { group: "account" } } },
      );
      if (!response.ok) {
        throw new Error(`载入注册设置失败（HTTP ${response.status}）`);
      }
      return data as GroupView | undefined;
    },
  });

  const update = useMutation({
    mutationFn: async (values: FormValues) => {
      const { error, response } = await api.PUT(
        "/api/v1/console/settings/{group}",
        {
          params: { path: { group: "account" } },
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
      void queryClient.invalidateQueries({ queryKey: ["settings", "account"] });
    },
  });

  if (query.isLoading) {
    return (
      <Card>
        <CardHeader title="注册设置" />
        <CardBody aria-busy="true">
          <Skeleton className="h-24 w-full" />
        </CardBody>
      </Card>
    );
  }

  const group = query.data;
  if (query.error || !group) {
    return (
      <Card>
        <CardHeader title="注册设置" />
        <CardBody>
          <Alert tone="danger" title="载入失败">
            <p>
              {query.error instanceof Error
                ? query.error.message
                : "取不到 account 设置分组，它可能来自一个未启用的模块。"}
            </p>
          </Alert>
        </CardBody>
      </Card>
    );
  }

  return (
    <Card>
      <CardHeader
        title="注册设置"
        description="访客自助注册与账户页。开放注册前请先配好邮件发送与站点对外地址。"
      />
      <CardBody>
        <SchemaForm
          schema={group.schema as GroupSchema}
          values={group.values as FormValues}
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
      </CardBody>
    </Card>
  );
}
