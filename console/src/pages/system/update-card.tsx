import { api } from "@/api/client";
import { runMutation } from "@/api/mutation";
import { queryClient } from "@/api/query-client";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardBody, CardHeader } from "@/components/ui/card";
import { ConfirmDialog } from "@/components/ui/dialog";
import {
  Field,
  FieldError,
  FieldHint,
  FieldLabel,
} from "@/components/ui/field";
import { Textarea } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/states";
import { StatusDot } from "@/components/ui/status-dot";
import { absoluteDate, fileSize, relativeTime } from "@/lib/format";
import { cn } from "@/lib/utils";
import { useMutation, useQuery } from "@tanstack/react-query";
import {
  Archive,
  ArrowRight,
  Check,
  CircleArrowUp,
  Copy,
  Download,
  ExternalLink,
  Gauge,
  Pencil,
  RefreshCw,
  RotateCw,
  Trash2,
} from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";

/**
 * 在线升级。
 *
 * 这一块要回答的只有两个问题：**该不该升级**、**现在能不能升级**。
 * 所以版面上最大的东西是「当前版本 → 最新版本」那一行，其余都退到它后面；
 * 不能升级时按钮禁用**并原地说明原因** —— 一个没有解释的灰按钮只会让人去翻文档。
 *
 * 升级是一条长任务，界面按阶段给反馈：下载有百分比（进度条 + 字节数），
 * 校验与替换没法测量（不确定态，脉冲点 + 一句话），重启期间接口本身是断的，
 * 故那一段改为轮询 `/healthz` 等服务回来。整段状态包在一个 `role="status"` 里
 * 一次播报完整句子，而不是让读屏用户听见一串跳动的数字。
 *
 * 动词全程一致：按钮「升级到 1.2.0」→ 过程「正在升级」→ 结果「已升级到 1.2.0」。
 */

type Status = NonNullable<Awaited<ReturnType<typeof fetchStatus>>>;

async function fetchStatus() {
  const { data, response } = await api.GET("/api/v1/console/update");
  if (!response.ok) {
    throw new Error(`HTTP ${response.status}`);
  }
  return data;
}

/** 下载与安装期间的轮询间隔。进度条要看得出在动，1.5 秒够了。 */
const BUSY_POLL_MS = 1500;

/** 等服务重启回来时的探活间隔。 */
const HEALTH_POLL_MS = 2000;

/** 正在忙的几个阶段：此时不给操作入口，只给进度。 */
const BUSY_PHASES = new Set([
  "connecting",
  "downloading",
  "installing",
  "restarting",
]);

/**
 * 版本号去掉 v 前缀。
 *
 * 发布的 tag 是 `v9.9.9`，而构建期注入的版本号是 `9.9.9`——两个并排摆着时
 * 那个 v 会让人以为是两种东西。对外只显示一种写法。
 */
function plainVersion(value: string | undefined): string {
  return (value ?? "").replace(/^v/i, "");
}

/** 把 Go 的时长写法（24h0m0s）说成人话。解析不出就原样显示。 */
function humanInterval(value: string): string {
  const match = /^(?:(\d+)h)?(?:(\d+)m)?(?:[\d.]+s)?$/.exec(value.trim());
  if (!match) {
    return value;
  }
  const [, hours, minutes] = match;
  const parts: string[] = [];
  if (hours && hours !== "0") {
    parts.push(`${hours} 小时`);
  }
  if (minutes && minutes !== "0") {
    parts.push(`${minutes} 分钟`);
  }
  return parts.length > 0 ? parts.join("") : value;
}

export function UpdateCard() {
  // waitingFor 非空表示服务已经在重启，接口马上就要断了。
  const [waitingFor, setWaitingFor] = useState<string | null>(null);
  // upgradeFrom 是发起升级那一刻的版本号。
  //
  // 判断「升级完了没有」靠的是**服务报出来的版本变了**，而不是某个中间阶段：
  // 本地网络下整条链路可能在一个轮询间隔里跑完，restarting 那一拍根本看不到，
  // 于是页面会安静地换成新版本号，而浏览器里跑的仍是旧的那份前端。
  const [upgradeFrom, setUpgradeFrom] = useState<string | null>(null);
  const [confirmUpgrade, setConfirmUpgrade] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState<string | null>(null);
  const [notesOpen, setNotesOpen] = useState(false);

  const status = useQuery({
    queryKey: ["update-status"],
    queryFn: fetchStatus,
    // 服务已经在重启了，再去问它只会得到一串网络错误。
    enabled: waitingFor === null,
    refetchInterval: (query) => {
      const phase = query.state.data?.progress.phase;
      return phase && BUSY_PHASES.has(phase) ? BUSY_POLL_MS : false;
    },
  });

  const data = status.data;
  const progress = data?.progress;
  const phase = progress?.phase ?? "idle";

  // 一旦进入重启阶段就切到探活模式：接口马上就要断了。
  useEffect(() => {
    if (phase === "restarting" && progress?.version) {
      setWaitingFor(progress.version);
    }
  }, [phase, progress?.version]);

  const health = useQuery({
    queryKey: ["update-health"],
    enabled: waitingFor !== null,
    // 服务回来之后就不必再探了。
    refetchOnWindowFocus: false,
    // 服务没回来之前每次都会失败，这是预期内的，不当错误处理。
    retry: false,
    refetchInterval: HEALTH_POLL_MS,
    queryFn: async (): Promise<{ version: string }> => {
      const response = await fetch("/healthz", { cache: "no-store" });
      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
      }
      return (await response.json()) as { version: string };
    },
  });

  // 服务当前报出来的版本：重启期间只有 /healthz 答得上话，回来之后两处都能拿到。
  const liveVersion = health.data?.version ?? data?.current.version;
  const restarted =
    upgradeFrom !== null && Boolean(liveVersion) && liveVersion !== upgradeFrom;

  // 服务换了版本，同一页上的「构建信息」还在显示旧版本号——两个版本号打架
  // 比晚几秒更新更让人困惑。让那张卡与仪表盘的版本号都重新问一次。
  useEffect(() => {
    if (restarted) {
      void queryClient.invalidateQueries({ queryKey: ["build-info"] });
      void queryClient.invalidateQueries({ queryKey: ["health"] });
    }
  }, [restarted]);

  const check = useMutation({
    mutationFn: () =>
      runMutation(() => api.POST("/api/v1/console/update/check"), {
        invalidate: ["update-status"],
      }),
    onSuccess: () => {
      void status.refetch();
    },
  });

  const upgrade = useMutation({
    mutationFn: () =>
      runMutation(() => api.POST("/api/v1/console/update/apply"), {
        success: "已开始升级",
        invalidate: ["update-status"],
      }),
    onSuccess: () => setUpgradeFrom(data?.current.version ?? null),
    onSettled: () => {
      setConfirmUpgrade(false);
      void status.refetch();
    },
  });

  const remove = useMutation({
    mutationFn: (name: string) =>
      runMutation(
        () =>
          api.DELETE("/api/v1/console/update/backups/{name}", {
            params: { path: { name } },
          }),
        { success: "备份已删除", invalidate: ["update-status"] },
      ),
    onSettled: () => setConfirmDelete(null),
  });

  if (status.isLoading && !data) {
    return (
      <Card>
        <CardHeader title="在线升级" />
        <CardBody>
          <Skeleton className="h-16 w-full" />
        </CardBody>
      </Card>
    );
  }

  const latest = data?.latest;
  // 升级一旦发起就算忙，直到服务带着新版本回来：中间那几拍的阶段未必看得到，
  // 但这期间不该再让人点第二次。
  const busy =
    BUSY_PHASES.has(phase) ||
    waitingFor !== null ||
    (upgradeFrom !== null && !restarted);
  const canUpgrade = Boolean(data?.canUpdate && data?.hasUpdate && !busy);

  return (
    <>
      <Card>
        <CardHeader
          title="在线升级"
          badge={
            <CircleArrowUp
              aria-hidden="true"
              className="size-4 text-ink-muted"
            />
          }
          actions={
            <>
              <Button
                variant="secondary"
                size="sm"
                loading={check.isPending}
                disabled={busy || !data?.enabled}
                onClick={() => check.mutate()}
              >
                <RefreshCw aria-hidden="true" />
                检查更新
              </Button>
              {data?.hasUpdate ? (
                <Button
                  variant="primary"
                  size="sm"
                  disabled={!canUpgrade}
                  onClick={() => setConfirmUpgrade(true)}
                >
                  <Download aria-hidden="true" />
                  升级到 {plainVersion(latest?.tag)}
                </Button>
              ) : null}
            </>
          }
        />

        <CardBody className="flex flex-col gap-4">
          <VersionLine status={data} />

          {busy || restarted || phase === "failed" || phase === "ready" ? (
            <ProgressPanel
              phase={phase}
              progress={progress}
              newVersion={liveVersion}
              restarted={restarted}
            />
          ) : null}

          {data?.rollback ? (
            <Alert
              tone="warn"
              title={`版本回退了：装过 ${data.rollback.from}，现在跑的是 ${data.rollback.to}`}
            >
              这台机器在 {absoluteDate(data.rollback.at)} 由在线升级装到过{" "}
              {data.rollback.from}。多半是容器被重建，回到了镜像里的版本——
              把镜像标签换成新版本再重建，就不会再退回去了。若是你主动降级的，忽略即可。
            </Alert>
          ) : null}

          {!data?.enabled ? (
            <Alert tone="info" title="功能已关闭">
              在线升级在配置中被关掉了（update.enabled）。开启后可在这里检查并安装新版本。
            </Alert>
          ) : !data.canUpdate ? (
            // 容器里程序文件不持久，是唯一需要整段指引的情形；其余原因
            // （目录只读、定位不到自己）一句话说清就够。
            data.container && !data.persistent ? (
              <ContainerGuide image={data.image} hasUpdate={data.hasUpdate} />
            ) : (
              <Alert tone="warn" title="这台机器上不能就地升级">
                {data.reason}
              </Alert>
            )
          ) : data.container && !data.persistent ? (
            <Alert tone="warn" title="容器里的就地升级是开着的">
              程序文件在容器自己的文件系统上：重启容器不丢，但
              <strong>重建</strong>
              容器（改配置、拉新镜像）会回到镜像里的版本。真退回去了这里会说出来。
            </Alert>
          ) : null}

          {status.error ? (
            <Alert tone="danger" title="读取升级状态失败">
              {status.error.message}
            </Alert>
          ) : data?.checkError ? (
            <Alert tone="warn" title="检查更新失败">
              {data.checkError}
            </Alert>
          ) : null}

          {latest && data?.hasUpdate ? (
            <ReleaseNotes
              release={latest}
              open={notesOpen}
              onToggle={() => setNotesOpen((value) => !value)}
            />
          ) : null}

          <Mirrors status={data} disabled={busy || !data?.enabled} />

          <Backups
            backups={data?.backups ?? []}
            dir={data?.backupDir ?? ""}
            keep={data?.keepBackups ?? 0}
            pending={remove.isPending ? confirmDelete : null}
            onDelete={setConfirmDelete}
          />
        </CardBody>
      </Card>

      <ConfirmDialog
        open={confirmUpgrade}
        onOpenChange={setConfirmUpgrade}
        title={`升级到 ${plainVersion(latest?.tag)}`}
        destructive={false}
        confirmLabel="开始升级"
        pending={upgrade.isPending}
        onConfirm={() => upgrade.mutate()}
        consequence={
          <div className="flex flex-col gap-2">
            <p>
              下载发布包、核对校验和，然后替换程序文件。当前版本会先备份到{" "}
              <span className="token">{data?.backupDir}</span>。
            </p>
            <p>
              装好后服务自动重启，其间站点短暂不可用（通常几秒）。
              数据库结构由新版本启动时自动迁移。
            </p>
          </div>
        }
      />

      <ConfirmDialog
        open={confirmDelete !== null}
        onOpenChange={(open) => {
          if (!open) {
            setConfirmDelete(null);
          }
        }}
        title="删除这份备份"
        confirmLabel="删除"
        pending={remove.isPending}
        onConfirm={() => {
          if (confirmDelete) {
            remove.mutate(confirmDelete);
          }
        }}
        consequence={
          <>
            删除后这个版本就只能重新下载了。要回退到它，得先把文件复制回程序目录。
            <br />
            <span className="token">{confirmDelete}</span>
          </>
        }
      />
    </>
  );
}

/**
 * 一行可复制的命令或镜像引用。
 *
 * 容器部署的升级是一串要照抄的东西，而这一页的读者多半是从面板点进来的——
 * 让他们自己去别处查命令，等于把这块提示写了等于没写。
 */
function CopyLine({ value, label }: { value: string; label: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <div className="flex flex-wrap items-center gap-2">
      <code className="token rounded-control bg-surface-inset px-2 py-1 text-xs text-ink">
        {value}
      </code>
      <Button
        variant="ghost"
        size="xs"
        aria-label={`复制${label}`}
        onClick={async () => {
          try {
            await navigator.clipboard.writeText(value);
            setCopied(true);
            setTimeout(() => setCopied(false), 2000);
          } catch {
            toast.error("剪贴板不可用，请手动选中复制");
          }
        }}
      >
        {copied ? <Check aria-hidden="true" /> : <Copy aria-hidden="true" />}
        {copied ? "已复制" : "复制"}
      </Button>
    </div>
  );
}

/**
 * 容器部署的升级指引。
 *
 * 容器里换掉二进制是白费功夫，但只说「请改用新的镜像标签」而不说是哪个标签、
 * 在哪儿改，对着面板部署的站长等于没说。这里给全三样：标签、命令、以及
 * 「不想每次手动换」的那条路。
 */
function ContainerGuide({
  image,
  hasUpdate,
}: {
  image: string | undefined;
  hasUpdate: boolean;
}) {
  return (
    // 语气是 info 不是 warn：这一块讲的是「这边该怎么升级」，不是出了什么问题。
    // 黄色留给真正需要人去处置的事（版本回退、检查失败），否则一屏两块黄底，
    // 哪一块要紧就看不出来了。
    <Alert tone="info" title="容器部署：换镜像标签升级">
      <div className="flex flex-col gap-2.5">
        <p>
          就地替换二进制在容器里是白费功夫——换掉的文件活在可写层，重建容器就回到
          镜像里的版本。
        </p>

        {hasUpdate && image ? (
          <>
            <p>把镜像换成这个标签后重建容器：</p>
            <CopyLine value={image} label="镜像标签" />
          </>
        ) : null}

        <p>命令行（compose 用 latest 标签时）：</p>
        <CopyLine
          value="docker compose pull && docker compose up -d"
          label="升级命令"
        />

        <p className="text-ink-muted">
          面板上的位置：1Panel 在「容器 → 编辑 → 镜像」，宝塔在 Docker
          模块的容器编辑里。数据都在卷上，重建容器不会丢。
        </p>
        <p className="text-ink-muted">
          不想每次手动换标签：给这个容器装一个 Watchtower（1Panel
          应用商店里有），它会定时拉新镜像并重建容器；或者在配置里开
          update.allowInContainer，就能在这一页直接升级——代价是重建容器时会退回
          镜像里的版本，退了这里会明说。
        </p>
      </div>
    </Alert>
  );
}

/**
 * 当前版本与最新版本的对比。
 *
 * 两个版本号并排、中间一个箭头 —— 这是整块里唯一需要一眼看懂的东西，
 * 故给它最大的字号，其余元信息退到下面一行的小字。
 */
function VersionLine({ status }: { status: Status | undefined }) {
  if (!status) {
    return null;
  }
  const latest = status.latest;

  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap items-center gap-3">
        <span className="token text-lg text-ink tabular">
          {status.current.version}
        </span>
        {latest && status.hasUpdate ? (
          <>
            <ArrowRight aria-hidden="true" className="size-4 text-ink-subtle" />
            <span className="token text-lg font-semibold text-seal tabular">
              {plainVersion(latest.tag)}
            </span>
            <StatusDot state="seal">
              {latest.prerelease ? "预发布版本" : "有新版本"}
            </StatusDot>
          </>
        ) : status.checkedAt && !status.checkError ? (
          <StatusDot state="ok">
            {status.versionComparable
              ? "已是最新版本"
              : "开发构建，不参与版本比较"}
          </StatusDot>
        ) : (
          <StatusDot state="neutral">尚未检查</StatusDot>
        )}
      </div>

      <dl className="flex flex-wrap gap-x-6 gap-y-1 text-xs text-ink-muted">
        <div className="flex gap-1.5">
          <dt>更新源</dt>
          <dd className="token text-ink">{status.repo}</dd>
        </div>
        <div className="flex gap-1.5">
          <dt>自动检查</dt>
          <dd className="text-ink">
            {status.autoCheck
              ? `每 ${humanInterval(status.checkInterval)}一次`
              : "已关闭"}
          </dd>
        </div>
        {status.container ? (
          <div className="flex gap-1.5">
            <dt>部署形态</dt>
            <dd className="text-ink">
              {status.persistent
                ? `容器，程序文件在挂载卷上${status.mountPoint ? `（${status.mountPoint}）` : ""}`
                : "容器，程序文件在镜像里"}
            </dd>
          </div>
        ) : null}
        {status.checkedAt ? (
          <div className="flex gap-1.5">
            <dt>最近检查</dt>
            <dd className="text-ink" title={absoluteDate(status.checkedAt)}>
              {relativeTime(status.checkedAt)}
            </dd>
          </div>
        ) : null}
        {latest?.publishedAt && status.hasUpdate ? (
          <div className="flex gap-1.5">
            <dt>发布于</dt>
            <dd className="text-ink" title={absoluteDate(latest.publishedAt)}>
              {relativeTime(latest.publishedAt)}
            </dd>
          </div>
        ) : null}
      </dl>
    </div>
  );
}

/** 阶段对应的说明文字。动词与按钮一致。 */
const PHASE_TEXT: Record<string, string> = {
  connecting: "正在测速，挑最快的下载地址",
  downloading: "正在下载发布包",
  installing: "正在核对校验和并替换程序文件",
  restarting: "服务正在以新版本重启",
  ready: "新版本已装好，等待重启",
  failed: "升级失败",
};

/**
 * 进度面板。
 *
 * 整块是一个 `role="status"`：下载百分比每 1.5 秒变一次，若每个数字各自
 * 播报一遍，读屏用户听到的是一串噪音。`aria-atomic` 让它每次读完整一句。
 */
function ProgressPanel({
  phase,
  progress,
  newVersion,
  restarted,
}: {
  phase: string;
  progress: Status["progress"] | undefined;
  newVersion: string | undefined;
  restarted: boolean;
}) {
  if (restarted) {
    return (
      <Alert tone="ok" title={`已升级到 ${plainVersion(newVersion)}`}>
        <div className="flex flex-col items-start gap-2">
          <p>服务已经带着新版本回来了。刷新页面以载入新的后台界面。</p>
          <Button
            size="sm"
            variant="secondary"
            onClick={() => location.reload()}
          >
            <RotateCw aria-hidden="true" />
            刷新页面
          </Button>
        </div>
      </Alert>
    );
  }

  if (phase === "failed") {
    return (
      <Alert tone="danger" title="升级失败">
        {progress?.error ?? "未知原因"}
        <p className="mt-1 text-ink-muted">
          程序文件没有被改动，站点仍在原版本上运行。修掉原因后可以再试一次。
        </p>
      </Alert>
    );
  }

  const downloading = phase === "downloading";
  const total = progress?.total ?? 0;
  const done = progress?.downloaded ?? 0;
  const percent =
    total > 0 ? Math.min(100, Math.round((done / total) * 100)) : 0;

  return (
    <output
      aria-atomic="true"
      className="flex flex-col gap-2 rounded-control border border-line bg-surface-raised px-3 py-2.5"
    >
      <div className="flex flex-wrap items-center justify-between gap-2 text-sm">
        <StatusDot
          state={phase === "ready" ? "warn" : "seal"}
          pulse={phase !== "ready"}
        >
          {PHASE_TEXT[phase] ?? "正在升级"}
          {progress?.version ? ` ${progress.version}` : ""}
        </StatusDot>
        {downloading && total > 0 ? (
          <span className="tabular text-xs text-ink-muted">
            {progress?.source ? `从 ${progress.source} 下载，` : ""}
            {fileSize(done)} / {fileSize(total)}（{percent}%）
          </span>
        ) : null}
      </div>

      {downloading ? (
        // 条本身对读屏是冗余的：百分比与字节数就在上面那行文字里，
        // 而整块是一个 status 区域，会把那句话完整读出来。
        <div
          aria-hidden="true"
          className="h-1.5 overflow-hidden rounded-control bg-surface-active"
        >
          {/*
            用 scaleX 推进度而不是改 width：改宽度每帧都要重新布局，
            而这条进度条每 1.5 秒动一次、一直动几分钟。
          */}
          <div
            className={cn(
              "h-full origin-left bg-seal transition-transform duration-300",
              total > 0 ? "" : "animate-pulse",
            )}
            style={{ transform: `scaleX(${total > 0 ? percent / 100 : 1})` }}
          />
        </div>
      ) : null}

      {phase === "connecting" ? (
        <p className="text-xs text-ink-muted">
          加速地址和 GitHub 直连各下一小段比快慢，最多十秒。
        </p>
      ) : null}

      {phase === "ready" ? (
        <p className="text-xs text-ink-muted">
          当前进程没能自己重启，请手动重启服务以启用新版本。
        </p>
      ) : null}
    </output>
  );
}

/**
 * 发布说明里的一个块。
 *
 * 发布说明是 Markdown，但这里**不做 Markdown 渲染**：解析成 HTML 就得连着
 * 净化一起做，而这段文字来自更新源仓库——那正是升级链路上最该少信任一分的地方。
 * 只按行首的标记分出标题与列表项，其余当段落，全程是纯文本进纯文本出。
 * 代价是行内语法（粗体、链接）原样显示，想看排好版的右上角有发布页链接。
 */
type NotesBlock =
  | { kind: "heading"; text: string }
  | { kind: "para"; text: string }
  | { kind: "list"; items: string[] };

function parseNotes(raw: string): NotesBlock[] {
  const blocks: NotesBlock[] = [];
  for (const line of raw.split("\n")) {
    const text = line.trim();
    if (text === "") {
      continue;
    }
    const heading = /^#{1,6}\s+(.*)$/.exec(text);
    if (heading?.[1]) {
      blocks.push({ kind: "heading", text: heading[1] });
      continue;
    }
    const item = /^[*+-]\s+(.*)$/.exec(text);
    if (item?.[1]) {
      const last = blocks.at(-1);
      // 连续的条目合成一个列表，中间被标题或段落打断就另起一组。
      if (last?.kind === "list") {
        last.items.push(item[1]);
      } else {
        blocks.push({ kind: "list", items: [item[1]] });
      }
      continue;
    }
    blocks.push({ kind: "para", text });
  }
  return blocks;
}

/**
 * 发布说明。
 */
function ReleaseNotes({
  release,
  open,
  onToggle,
}: {
  release: NonNullable<Status["latest"]>;
  open: boolean;
  onToggle: () => void;
}) {
  const notes = release.notes?.trim();

  return (
    <div className="flex flex-col gap-2 border-line border-t pt-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h3 className="text-sm font-medium text-ink">
          {release.name || release.tag} 的变更
        </h3>
        <a
          href={release.htmlUrl}
          target="_blank"
          rel="noreferrer noopener"
          className="inline-flex items-center gap-1 text-xs text-seal hover:underline"
        >
          <ExternalLink aria-hidden="true" className="size-3.5" />在 GitHub
          上查看
        </a>
      </div>

      {notes ? (
        <>
          <div
            className={cn(
              "token flex flex-col gap-2 text-sm text-ink-muted",
              // 折起时限高而不是限行数：块与块之间有间距，按行数裁会裁在半个块上。
              open ? "" : "max-h-40 overflow-hidden",
            )}
          >
            {parseNotes(notes).map((block, index) => {
              const key = `${block.kind}-${index}`;
              if (block.kind === "heading") {
                return (
                  <p key={key} className="font-medium text-ink">
                    {block.text}
                  </p>
                );
              }
              if (block.kind === "list") {
                return (
                  <ul key={key} className="flex list-disc flex-col gap-1 pl-5">
                    {block.items.map((item) => (
                      <li key={item}>{item}</li>
                    ))}
                  </ul>
                );
              }
              return <p key={key}>{block.text}</p>;
            })}
          </div>
          <Button
            variant="link"
            size="sm"
            className="self-start"
            onClick={onToggle}
          >
            {open ? "收起" : "展开全部"}
          </Button>
        </>
      ) : (
        <p className="text-sm text-ink-muted">这一版没有附带发布说明。</p>
      )}
    </div>
  );
}

/** 加速地址的域名，列表里只显示这一段；完整地址放在 title 里。 */
function mirrorHost(prefix: string): string {
  try {
    return new URL(prefix).host;
  } catch {
    return prefix;
  }
}

/**
 * 下载加速地址。
 *
 * 平时只列出域名，要改时才展开成多行输入框：这一栏多数站长只看一眼。
 * 说明要讲清两件事：这些是别人的公共服务，可能失效；但它们改不了安装包。
 * 不说后一句，站长看到第三方地址会不放心。
 */
function Mirrors({
  status,
  disabled,
}: {
  status: Status | undefined;
  disabled: boolean;
}) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState("");
  const mirrors = status?.mirrors ?? [];
  const custom = status?.mirrorsCustom ?? false;

  const save = useMutation({
    mutationFn: (list: string[]) =>
      runMutation(
        () =>
          api.PUT("/api/v1/console/update/mirrors", {
            body: { mirrors: list },
          }),
        { success: "加速地址已保存", invalidate: ["update-status"] },
      ),
    onSuccess: () => setEditing(false),
  });

  const reset = useMutation({
    mutationFn: () =>
      runMutation(() => api.DELETE("/api/v1/console/update/mirrors"), {
        success: "已恢复内置的加速地址",
        invalidate: ["update-status"],
      }),
    onSuccess: () => setEditing(false),
  });

  function startEdit() {
    setDraft(mirrors.join("\n"));
    save.reset();
    setEditing(true);
  }

  return (
    <div className="flex flex-col gap-2 border-line border-t pt-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h3 className="flex items-center gap-1.5 text-sm font-medium text-ink">
          <Gauge aria-hidden="true" className="size-4 text-ink-muted" />
          下载加速
        </h3>
        {editing ? null : (
          <Button
            variant="ghost"
            size="sm"
            disabled={disabled}
            onClick={startEdit}
          >
            <Pencil aria-hidden="true" />
            修改
          </Button>
        )}
      </div>

      <p className="text-xs text-ink-muted">
        {
          "升级时这些地址和 GitHub 直连各先下一小段比快慢，从最快的那个分几条连接下载。它们是别人提供的公共服务，可能失效，但改不了安装包：下完要和 GitHub 给的校验值对上才会安装。"
        }
      </p>

      {editing ? (
        <form
          className="flex flex-col gap-3"
          onSubmit={(event) => {
            event.preventDefault();
            save.mutate(
              draft
                .split("\n")
                .map((line) => line.trim())
                .filter(Boolean),
            );
          }}
        >
          <Field>
            <FieldLabel htmlFor="update-mirrors">加速地址</FieldLabel>
            <Textarea
              id="update-mirrors"
              rows={5}
              value={draft}
              spellCheck={false}
              aria-invalid={save.isError}
              aria-describedby="update-mirrors-hint"
              className="font-mono text-sm"
              onChange={(event) => setDraft(event.target.value)}
            />
            <FieldHint id="update-mirrors-hint">
              每行一个，以 https:// 开头，最多 8 个。全部删掉就只直连 GitHub。
            </FieldHint>
            <FieldError>{save.error?.message}</FieldError>
          </Field>
          <div className="flex flex-wrap gap-2">
            <Button
              type="submit"
              size="sm"
              variant="primary"
              loading={save.isPending}
            >
              保存
            </Button>
            <Button
              type="button"
              size="sm"
              variant="secondary"
              onClick={() => setEditing(false)}
            >
              取消
            </Button>
            {custom ? (
              <Button
                type="button"
                size="sm"
                variant="ghost"
                loading={reset.isPending}
                onClick={() => reset.mutate()}
              >
                恢复内置地址
              </Button>
            ) : null}
          </div>
        </form>
      ) : mirrors.length === 0 ? (
        <p className="text-sm text-ink-muted">没有加速地址，只直连 GitHub。</p>
      ) : (
        <ul className="flex flex-wrap items-center gap-x-3 gap-y-1">
          {mirrors.map((prefix) => (
            <li key={prefix} className="token text-sm text-ink" title={prefix}>
              {mirrorHost(prefix)}
            </li>
          ))}
          {custom ? null : (
            <li>
              <Badge>内置</Badge>
            </li>
          )}
        </ul>
      )}
    </div>
  );
}

/**
 * 旧版本备份。
 *
 * 用 ul/li 而不是表格：这是一列文件，读屏用户不该在这里听到「表格，4 列」。
 * 没有备份时留一行说明而不是整块消失 —— 站长需要知道升级会在哪里留下什么。
 */
function Backups({
  backups,
  dir,
  keep,
  pending,
  onDelete,
}: {
  backups: NonNullable<Status["backups"]>;
  dir: string;
  keep: number;
  pending: string | null;
  onDelete: (name: string) => void;
}) {
  return (
    <div className="flex flex-col gap-2 border-line border-t pt-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h3 className="flex items-center gap-1.5 text-sm font-medium text-ink">
          <Archive aria-hidden="true" className="size-4 text-ink-muted" />
          旧版本备份
        </h3>
        <p className="text-xs text-ink-muted">
          {keep > 0
            ? `升级后自动保留最近 ${keep} 份`
            : "不自动清理，请自行管理"}
        </p>
      </div>

      {backups.length === 0 ? (
        <p className="text-sm text-ink-muted">
          还没有备份。升级时会先把当前版本复制到{" "}
          <span className="token">{dir}</span>。
        </p>
      ) : (
        <ul className="flex flex-col divide-y divide-line">
          {backups.map((backup) => (
            <li
              key={backup.name}
              className="flex flex-wrap items-center gap-x-4 gap-y-1 py-2"
            >
              <span className="token min-w-0 flex-1 text-sm text-ink">
                {backup.name}
              </span>
              <span className="tabular text-xs text-ink-muted">
                {fileSize(backup.size)}
              </span>
              <span
                className="text-xs text-ink-muted"
                title={absoluteDate(backup.createdAt)}
              >
                {relativeTime(backup.createdAt)}
              </span>
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label={`删除备份 ${backup.name}`}
                loading={pending === backup.name}
                onClick={() => onDelete(backup.name)}
              >
                <Trash2 aria-hidden="true" />
              </Button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
