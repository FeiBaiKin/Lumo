import { api, csrfToken, problemMessage } from "@/api/client";
import type { components } from "@/api/schema";
import { PageBody, PageHeader } from "@/components/layout/page-header";
import { useTheme } from "@/components/theme/theme-provider";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { EmptyState, ErrorState, Skeleton } from "@/components/ui/states";
import { ICONS } from "@/lib/icons";
import { useDocumentTitle } from "@/lib/use-document-title";
import { useQuery } from "@tanstack/react-query";
import { Lock, Puzzle } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { Link, useParams } from "react-router";

type PageView = components["schemas"]["PageView"];

/**
 * 插件的后台自定义页：插件包里的一张 HTML，放在隔离的 iframe 里。
 *
 * iframe 不给 allow-same-origin，文件本身也带 CSP sandbox：页面跑在不透明的源上，
 * 读不到后台的 Cookie，也调不了后台接口。它要数据，就经 postMessage 请这一页代为调用
 * **本插件自己的**接口（/api/v1/plugins/<插件>/…），见 docs/plugin-development.md 的「后台页面」。
 */

/** iframe 请求时用的消息。 */
type BridgeRequest = {
  lumo: "request";
  id: string | number;
  method?: string;
  path: string;
  query?: Record<string, string>;
  body?: unknown;
};

const BRIDGE_METHODS = new Set(["GET", "POST", "PUT", "PATCH", "DELETE"]);

/** 页面自己报的高度有上下限：太矮看不见，太高是写坏了。 */
const MIN_HEIGHT = 240;
const MAX_HEIGHT = 20000;

class PageError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message);
  }
}

export function PluginPage() {
  const { plugin = "", page = "" } = useParams();
  const info = useQuery({
    queryKey: ["plugin-page", plugin, page],
    retry: false,
    queryFn: async (): Promise<PageView> => {
      const { data, error, response } = await api.GET(
        "/api/v1/console/plugins/{name}/pages/{page}",
        { params: { path: { name: plugin, page } } },
      );
      if (!response.ok || !data) {
        throw new PageError(problemMessage(error), response.status);
      }
      return data;
    },
  });
  useDocumentTitle(info.data?.label ?? "插件");

  if (info.isLoading) {
    return (
      <PageBody>
        <Card className="flex flex-col gap-3 p-4" aria-busy="true">
          <Skeleton className="h-8 w-48" />
          <Skeleton className="h-96 w-full" />
        </Card>
      </PageBody>
    );
  }
  if (info.error || !info.data) {
    const status = info.error instanceof PageError ? info.error.status : 0;
    return (
      <PageBody>
        <Card>
          {status === 404 ? (
            <EmptyState
              icon={Puzzle}
              title="这个插件页面打不开"
              description="提供它的插件可能已经停用或卸载。到插件页看看它的状态。"
              action={
                <Button variant="secondary" size="sm" asChild>
                  <Link to="/plugins">去插件页</Link>
                </Button>
              }
            />
          ) : status === 403 ? (
            <EmptyState
              icon={Lock}
              title="没有打开这个页面的权限"
              description={info.error?.message}
            />
          ) : (
            <ErrorState
              message={info.error?.message ?? "读不到页面信息"}
              onRetry={() => void info.refetch()}
            />
          )}
        </Card>
      </PageBody>
    );
  }

  return <PluginFrame view={info.data} />;
}

function PluginFrame({ view }: { view: PageView }) {
  const frame = useRef<HTMLIFrameElement>(null);
  const [loaded, setLoaded] = useState(false);
  const [height, setHeight] = useState<number | null>(null);
  const { resolved } = useTheme();
  const Icon = ICONS[view.icon] ?? Puzzle;

  // 配色变了告诉页面一声，插件页可以跟着换
  useEffect(() => {
    if (loaded) {
      frame.current?.contentWindow?.postMessage(
        {
          lumo: "context",
          scheme: resolved,
          plugin: view.plugin,
          page: view.path,
        },
        "*",
      );
    }
  }, [loaded, resolved, view.plugin, view.path]);

  useEffect(() => {
    const onMessage = (event: MessageEvent) => {
      const target = frame.current?.contentWindow;
      // 只认这个 iframe 发来的消息；它的源是不透明的 "null"，只能按窗口比对
      if (!target || event.source !== target) {
        return;
      }
      const data = event.data as { lumo?: unknown } | null;
      if (data?.lumo === "height") {
        const value = Number((data as { value?: unknown }).value);
        if (Number.isFinite(value)) {
          setHeight(Math.min(MAX_HEIGHT, Math.max(MIN_HEIGHT, value)));
        }
        return;
      }
      if (data?.lumo === "request") {
        void forward(view.api, data as BridgeRequest).then((reply) =>
          target.postMessage(reply, "*"),
        );
      }
    };
    window.addEventListener("message", onMessage);
    return () => window.removeEventListener("message", onMessage);
  }, [view.api]);

  return (
    <>
      <PageHeader
        icon={Icon}
        title={view.label}
        description={view.description || `插件「${view.pluginLabel}」的页面`}
      />
      <PageBody>
        <Card
          className="flex flex-col overflow-hidden"
          aria-label={`插件「${view.pluginLabel}」提供的内容`}
        >
          <p className="flex items-center gap-2 border-line border-b bg-surface-inset px-4 py-2 text-xs text-ink-muted">
            <Puzzle aria-hidden="true" className="size-3.5 shrink-0" />
            <span className="min-w-0">
              下面的内容由插件「{view.pluginLabel}
              」提供，运行在隔离的环境里，碰不到你的登录状态。
            </span>
          </p>
          <div className="relative">
            {loaded ? null : (
              <div className="absolute inset-0 p-4" aria-hidden="true">
                <Skeleton className="h-full w-full" />
              </div>
            )}
            <iframe
              ref={frame}
              title={`${view.label}（插件「${view.pluginLabel}」）`}
              src={view.src}
              sandbox="allow-scripts allow-forms allow-popups allow-modals allow-downloads"
              referrerPolicy="no-referrer"
              onLoad={() => setLoaded(true)}
              className="block w-full bg-surface"
              style={{
                height: height ? `${height}px` : "calc(100dvh - 12rem)",
                minHeight: MIN_HEIGHT,
              }}
            />
          </div>
        </Card>
      </PageBody>
    </>
  );
}

/** 代 iframe 调一次本插件的接口：路径只能落在插件前缀之下，写请求补上 CSRF 令牌。 */
async function forward(apiBase: string, req: BridgeRequest) {
  const reply = (status: number, data: unknown) => ({
    lumo: "response" as const,
    id: req.id,
    ok: status >= 200 && status < 300,
    status,
    data,
  });
  const method = (req.method ?? "GET").toUpperCase();
  const path = typeof req.path === "string" ? req.path : "";
  if (
    !BRIDGE_METHODS.has(method) ||
    !path.startsWith("/") ||
    path.startsWith("//") ||
    path.includes("..") ||
    path.includes("?") ||
    path.includes("#") ||
    path.includes("\\")
  ) {
    return reply(400, {
      detail: "只能调用本插件的接口，path 须以 / 开头，查询参数放进 query",
    });
  }
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(req.query ?? {})) {
    query.set(key, String(value));
  }
  const url = apiBase + path + (query.size > 0 ? `?${query}` : "");
  const headers: Record<string, string> = {};
  if (method !== "GET") {
    headers["X-CSRF-Token"] = csrfToken();
  }
  let body: string | null = null;
  if (req.body !== undefined && method !== "GET") {
    headers["Content-Type"] = "application/json";
    body = JSON.stringify(req.body);
  }
  try {
    const response = await fetch(url, {
      method,
      headers,
      body,
      credentials: "same-origin",
    });
    const type = response.headers.get("Content-Type") ?? "";
    const data = type.includes("json")
      ? await response.json().catch(() => null)
      : await response.text();
    return reply(response.status, data);
  } catch (err) {
    return reply(0, {
      detail: err instanceof Error ? err.message : "请求失败",
    });
  }
}
