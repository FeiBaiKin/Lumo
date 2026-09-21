import { api } from "@/api/client";
import {
  FilterMenu,
  ListBody,
  ListEmpty,
  ListToolbar,
} from "@/components/data/entity";
import { PageBody, PageHeader } from "@/components/layout/page-header";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { SearchInput } from "@/components/ui/input";
import { Pagination } from "@/components/ui/pagination";
import { StatusDot } from "@/components/ui/status-dot";
import { absoluteDate, fileSize } from "@/lib/format";
import { useDocumentTitle } from "@/lib/use-document-title";
import { useDebouncedSearch, useListParams } from "@/lib/use-list-params";
import { cn } from "@/lib/utils";
import { useQuery } from "@tanstack/react-query";
import { Download, FileText, ScrollText } from "lucide-react";
import { useState } from "react";

/**
 * 运行日志。
 *
 * 与附件、插件那些「实体列表」不同：日志行的主角不是名称，而是「何时发生了什么」。
 * 所以时间排在最左并用等宽对齐成一列——日志是时间序列，时间是它的坐标轴，
 * 对不齐就没法扫视。
 *
 * 级别只在 warn 与 error 上出点。一屏日志九成是 info，每行都点一下等于没点；
 * 反过来，出错的那几行要能自己从满屏里跳出来。这也是 StatusDot 的既定用法——
 * 列表里用色底会让整页发花。
 *
 * 等宽字体只给时间与属性值：它们要对齐、要能逐字符比对。消息多半是中文，
 * 用等宽反而难读，故仍用界面字体。
 */

/** 时间范围预设。排障时想的是「最近一小时」，不是「2026-09-21T13:00 到 14:00」。 */
const RANGE_OPTIONS = [
  { value: "1h", label: "最近 1 小时" },
  { value: "24h", label: "最近 24 小时" },
  { value: "7d", label: "最近 7 天" },
  { value: "30d", label: "最近 30 天" },
];

const RANGE_HOURS: Record<string, number> = {
  "1h": 1,
  "24h": 24,
  "7d": 24 * 7,
  "30d": 24 * 30,
};

const LEVEL_OPTIONS = [
  { value: "debug", label: "调试及以上" },
  { value: "info", label: "信息及以上" },
  { value: "warn", label: "警告及以上" },
  { value: "error", label: "只看错误" },
];

type Level = "debug" | "info" | "warn" | "error";

/** 级别到状态点的映射；info 与 debug 不出点。 */
function dotOf(level: string): "warn" | "danger" | null {
  const upper = level.toUpperCase();
  if (upper === "ERROR") return "danger";
  if (upper === "WARN" || upper === "WARNING") return "warn";
  return null;
}

/** 级别的显示名。日志里是英文大写，界面上给中文。 */
const LEVEL_TEXT: Record<string, string> = {
  DEBUG: "调试",
  INFO: "信息",
  WARN: "警告",
  WARNING: "警告",
  ERROR: "错误",
};

/** 只取时分秒：同一屏里的日志多半是同一天，重复的年月日只会挤掉消息的地方。 */
function clockOf(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "--:--:--";
  return date.toLocaleTimeString("zh-CN", { hour12: false });
}

function dayOf(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  return date.toLocaleDateString("zh-CN");
}

/**
 * 属性的显示顺序。
 *
 * 服务端给的是字母序（Go 的 json.Marshal 对 map 按键排序），而字母序恰好把
 * 排障最常看的 path 排到中间、把几乎用不上的 requestId 留在最后却最长——
 * 行内一截断，先没的就是 path。故按「排障时先看什么」重排。
 */
const ATTR_ORDER = ["path", "method", "status", "duration", "error", "module"];

/** requestId 最长且最少用，一律压到末尾。 */
const ATTR_LAST = ["requestId"];

function sortedAttrs(
  attrs: Record<string, unknown> | undefined,
): [string, unknown][] {
  if (!attrs) return [];
  const rank = (key: string) => {
    const first = ATTR_ORDER.indexOf(key);
    if (first >= 0) return first;
    if (ATTR_LAST.includes(key)) return 1000;
    return 100;
  };
  return Object.entries(attrs).sort(([a], [b]) => {
    const diff = rank(a) - rank(b);
    return diff !== 0 ? diff : a.localeCompare(b);
  });
}

/**
 * 属性值的显示形态。
 *
 * duration 与 bytes 在日志里是裸数字（slog 的 Duration 是纳秒），
 * 「duration=1027800」对读的人没有任何意义，得换算成人话。
 */
function attrText(key: string, value: unknown): string {
  if (key === "duration" && typeof value === "number") {
    return formatDuration(value);
  }
  if (key === "bytes" && typeof value === "number") {
    return fileSize(value);
  }
  if (typeof value === "string") return value;
  return JSON.stringify(value) ?? String(value);
}

/** 纳秒转人话。 */
function formatDuration(ns: number): string {
  if (ns < 1000) return `${ns}ns`;
  if (ns < 1_000_000) return `${Math.round(ns / 1000)}µs`;
  if (ns < 1_000_000_000) return `${(ns / 1_000_000).toFixed(1)}ms`;
  return `${(ns / 1_000_000_000).toFixed(2)}s`;
}

/**
 * HTTP 状态码的色。
 *
 * 请求日志无论成败都是 INFO 级别（见 internal/server/router.go），级别点因此
 * 帮不上忙——一屏日志里找不出哪个请求失败了，而那正是排障要找的。
 * 只给这一个值上色，行的其余部分保持安静。
 */
function statusTone(key: string, value: unknown): string {
  if (key !== "status" || typeof value !== "number") return "";
  if (value >= 500) return "text-danger";
  if (value >= 400) return "text-warn";
  return "";
}

export function LogsPage() {
  useDocumentTitle("日志");
  const list = useListParams();
  const [expanded, setExpanded] = useState<string | null>(null);
  const [filesOpen, setFilesOpen] = useState(false);

  const [search, setSearch] = useDebouncedSearch(list.filter("q"), (value) =>
    list.setFilter("q", value),
  );

  const level = list.filter("level");
  const range = list.filter("range");
  const file = list.filter("file");
  const hasFilters = Boolean(list.filter("q") || level || range || file);

  const from = range
    ? new Date(Date.now() - (RANGE_HOURS[range] ?? 0) * 3600_000).toISOString()
    : "";

  const query = useQuery({
    queryKey: [
      "logs",
      list.page,
      list.size,
      list.filter("q"),
      level,
      range,
      file,
    ],
    queryFn: async () => {
      const { data, response } = await api.GET("/api/v1/console/logs", {
        params: {
          query: {
            page: list.page,
            size: list.size,
            ...(level ? { level: level as Level } : {}),
            ...(list.filter("q") ? { q: list.filter("q") } : {}),
            ...(from ? { from } : {}),
            ...(file ? { file } : {}),
          },
        },
      });
      if (!response.ok) {
        throw new Error(`载入日志失败（HTTP ${response.status}）`);
      }
      return data;
    },
  });

  const filesQuery = useQuery({
    queryKey: ["logs", "files"],
    queryFn: async () => {
      const { data, response } = await api.GET("/api/v1/console/logs/files");
      if (!response.ok) {
        throw new Error(`载入日志文件失败（HTTP ${response.status}）`);
      }
      return data;
    },
  });

  const items = query.data?.items ?? [];
  const total = query.data?.total ?? 0;
  const fileEnabled = query.data?.fileEnabled ?? true;
  const truncated = query.data?.truncated ?? false;
  const files = filesQuery.data?.items ?? [];
  const logDir = filesQuery.data?.dir ?? "";

  const emptyState = !fileEnabled ? (
    <ListEmpty
      icon={ScrollText}
      title="日志没有写到文件"
      description="配置里 log.file 是关的，所以这一页读不到东西。打开它（或设环境变量 LUMO_LOG_FILE=true）后重启，日志会同时写到控制台和文件。"
    />
  ) : hasFilters ? (
    <ListEmpty
      icon={ScrollText}
      title="没有匹配的日志"
      description="换个关键词，或把级别放宽、时间范围拉长。"
      action={
        <Button variant="secondary" size="sm" onClick={list.reset}>
          清除筛选
        </Button>
      }
    />
  ) : (
    <ListEmpty
      icon={ScrollText}
      title="还没有日志"
      description={
        logDir ? `日志会写在 ${logDir}，服务运行后这里就有内容。` : undefined
      }
    />
  );

  return (
    <>
      <PageHeader
        icon={ScrollText}
        title="日志"
        description="服务端运行日志，最新的在前"
        actions={
          <Button
            variant="secondary"
            size="sm"
            onClick={() => setFilesOpen(true)}
          >
            <FileText aria-hidden="true" />
            日志文件
          </Button>
        }
      />

      <PageBody>
        {truncated ? (
          <Alert tone="warn" title="只统计了最近的一部分">
            日志量超过了单次扫描上限，更早的没有纳入。缩小时间范围，或在「日志文件」里选一个文件单独查。
          </Alert>
        ) : null}

        <Card>
          <ListToolbar
            className="border-line border-b"
            search={
              <SearchInput
                value={search}
                onValueChange={setSearch}
                placeholder="搜索消息与属性"
                aria-label="搜索日志"
                className="max-w-xs"
              />
            }
            filters={
              <>
                <FilterMenu
                  label="级别"
                  value={level}
                  options={LEVEL_OPTIONS}
                  onChange={(value) => list.setFilter("level", value)}
                />
                <FilterMenu
                  label="时间"
                  value={range}
                  options={RANGE_OPTIONS}
                  onChange={(value) => list.setFilter("range", value)}
                />
              </>
            }
            hasFilters={hasFilters}
            onClearFilters={list.reset}
            onRefresh={() => void query.refetch()}
            refreshing={query.isFetching}
          />

          {file ? (
            <div className="flex items-center gap-2 border-line border-b bg-surface-inset px-4 py-2 text-sm">
              <span className="text-ink-muted">只看</span>
              <span className="font-mono text-ink">{file}</span>
              <button
                type="button"
                className="transition-ui text-ink-muted text-xs hover:text-ink"
                onClick={() => list.setFilter("file", "")}
              >
                取消
              </button>
            </div>
          ) : null}

          <ListBody
            isLoading={query.isLoading}
            error={query.error as Error | null}
            onRetry={() => void query.refetch()}
            isEmpty={items.length === 0}
            empty={emptyState}
          >
            <ul className="divide-y divide-line">
              {items.map((entry, index) => {
                const key = `${entry.time}-${index}`;
                const dot = dotOf(entry.level);
                const pairs = sortedAttrs(
                  entry.attrs as Record<string, unknown> | undefined,
                );
                const open = expanded === key;
                return (
                  <li key={key}>
                    <button
                      type="button"
                      className={cn(
                        "transition-ui flex w-full items-baseline gap-3 px-4 py-2 text-left",
                        "hover:bg-surface-hover",
                        open && "bg-surface-hover",
                      )}
                      onClick={() => setExpanded(open ? null : key)}
                      aria-expanded={open}
                    >
                      <time
                        dateTime={entry.time}
                        title={absoluteDate(entry.time)}
                        className="shrink-0 font-mono text-ink-subtle text-xs tabular-nums"
                      >
                        {clockOf(entry.time)}
                      </time>

                      <span className="w-10 shrink-0">
                        {dot ? (
                          <StatusDot state={dot}>
                            {LEVEL_TEXT[entry.level.toUpperCase()] ??
                              entry.level}
                          </StatusDot>
                        ) : (
                          <span className="text-ink-subtle text-xs">
                            {LEVEL_TEXT[entry.level.toUpperCase()] ??
                              entry.level}
                          </span>
                        )}
                      </span>

                      <span className="min-w-0 flex-1">
                        <span
                          className={cn(
                            "block truncate text-sm",
                            dot === "danger" ? "text-danger" : "text-ink",
                          )}
                        >
                          {entry.msg}
                        </span>
                        {pairs.length > 0 && !open ? (
                          <span className="mt-0.5 block truncate font-mono text-ink-muted text-xs">
                            {pairs.map(([attrKey, value], i) => (
                              <span key={attrKey}>
                                {i > 0 ? "  " : ""}
                                {attrKey}=
                                <span className={statusTone(attrKey, value)}>
                                  {attrText(attrKey, value)}
                                </span>
                              </span>
                            ))}
                          </span>
                        ) : null}
                      </span>
                    </button>

                    {open ? (
                      <div className="border-line border-t bg-surface-inset px-4 py-3">
                        <dl className="grid gap-x-4 gap-y-1.5 sm:grid-cols-[auto_1fr]">
                          <dt className="text-ink-muted text-xs">时间</dt>
                          <dd className="font-mono text-ink text-xs">
                            {dayOf(entry.time)} {clockOf(entry.time)}
                          </dd>
                          <dt className="text-ink-muted text-xs">来源文件</dt>
                          <dd className="font-mono text-ink text-xs">
                            {entry.file}
                          </dd>
                          {pairs.map(([attrKey, value]) => (
                            <div key={attrKey} className="contents">
                              <dt className="font-mono text-ink-muted text-xs">
                                {attrKey}
                              </dt>
                              <dd
                                className={cn(
                                  "break-all font-mono text-xs",
                                  statusTone(attrKey, value) || "text-ink",
                                )}
                              >
                                {attrText(attrKey, value)}
                              </dd>
                            </div>
                          ))}
                        </dl>
                      </div>
                    ) : null}
                  </li>
                );
              })}
            </ul>
          </ListBody>

          {items.length > 0 ? (
            <Pagination
              className="border-line border-t"
              page={list.page}
              size={list.size}
              total={total}
              onPageChange={list.setPage}
              onSizeChange={list.setSize}
            />
          ) : null}
        </Card>
      </PageBody>

      <Dialog open={filesOpen} onOpenChange={setFilesOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>日志文件</DialogTitle>
          </DialogHeader>
          <DialogBody>
            {files.length === 0 ? (
              <p className="text-ink-muted text-sm">
                还没有日志文件
                {logDir ? `。日志会写在 ${logDir}。` : "。"}
              </p>
            ) : (
              <ul className="divide-y divide-line">
                {files.map((item) => (
                  <li
                    key={item.name}
                    className="flex items-center gap-3 py-2.5"
                  >
                    <div className="min-w-0 flex-1">
                      <p className="truncate font-mono text-ink text-sm">
                        {item.name}
                      </p>
                      <p className="text-ink-muted text-xs">
                        {item.sizeHuman || fileSize(item.size)} ·{" "}
                        {absoluteDate(item.modTime)}
                      </p>
                    </div>
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => {
                        list.setFilter("file", item.name);
                        setFilesOpen(false);
                      }}
                    >
                      只看这个
                    </Button>
                    <Button variant="secondary" size="sm" asChild>
                      <a
                        href={`/api/v1/console/logs/download?file=${encodeURIComponent(item.name)}`}
                        download={item.name}
                      >
                        <Download aria-hidden="true" />
                        下载
                      </a>
                    </Button>
                  </li>
                ))}
              </ul>
            )}
          </DialogBody>
        </DialogContent>
      </Dialog>
    </>
  );
}
