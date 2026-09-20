import { api, problemMessage } from "@/api/client";
import type { components } from "@/api/schema";
import { Logo } from "@/components/layout/logo";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
} from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useDocumentTitle } from "@/lib/use-document-title";
import { cn } from "@/lib/utils";
import { useQuery } from "@tanstack/react-query";
import {
  Check,
  Database,
  Eye,
  EyeOff,
  Globe,
  Loader2,
  PackageCheck,
  ShieldCheck,
} from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { Navigate } from "react-router";

type DatabaseInfo = components["schemas"]["DatabaseInfo"];
type ApplyResult = components["schemas"]["ApplyResult"];
type StatusView = components["schemas"]["StatusView"];
type DatabaseInput = components["schemas"]["DatabaseInput"];

/**
 * 首次安装向导。
 *
 * 只在站点还没有数据库连接信息时可达：此时后端只挂着本页、安装接口与静态资源，
 * 其余请求都被重定向到这里（见 internal/server 的 InstallRedirect）。装完之后
 * 服务会带着新配置重新启动一轮，安装接口随即锁定，本页也就自动跳去登录页。
 *
 * 界面上是 Console 里唯一的两栏页（其余页面都有侧栏，登录页是居中单列）：
 * 左边是四步索引，右边是当前这一步要填的东西。步骤是真的序列，所以用序号。
 *
 * 这里不伪造任何进度：测试连接显示的是目标库真实的版本与编码，执行安装列出的是
 * 后端返回的真实耗时。装到哪一步、慢在哪里，站长都应当看得到。
 */

const STEPS = [
  {
    key: "database",
    title: "数据库连接",
    hint: "站点数据存放的位置",
    icon: Database,
  },
  { key: "site", title: "站点信息", hint: "名称与对外地址", icon: Globe },
  {
    key: "admin",
    title: "管理员账号",
    hint: "你登录后台的身份",
    icon: ShieldCheck,
  },
  {
    key: "apply",
    title: "执行安装",
    hint: "建表、写配置、完成",
    icon: PackageCheck,
  },
] as const;

type StepKey = (typeof STEPS)[number]["key"];

/** 每一步的一句话说明：讲清为什么需要它，而不是重复标题。 */
const STEP_INTRO: Record<StepKey, string> = {
  database: "Lumo 需要一个 PostgreSQL 数据库，内容、设置与账号都存在里面。",
  site: "站点名称会出现在页头与浏览器标签上，对外地址用于生成分享链接与站点地图。",
  admin: "这个账号拥有全部权限，是站点唯一的入口，请务必记住口令。",
  apply: "确认下面的信息，安装会建表、创建管理员并保存配置。",
};

/** 步骤顺序表；上一步/下一步都按它走。 */
const ORDER: StepKey[] = ["database", "site", "admin", "apply"];

/** 用户名规则与数据库约束一致（migrations/00002_auth.sql 的 CHECK）。 */
const USERNAME_PATTERN = /^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/;

export function InstallPage() {
  useDocumentTitle("安装");

  const [stepKey, setStepKey] = useState<StepKey>("database");
  const [dbMode, setDbMode] = useState<"fields" | "dsn">("fields");
  const [db, setDb] = useState<DatabaseStepValue>({
    host: "127.0.0.1",
    port: "5432",
    name: "lumo",
    user: "postgres",
    password: "",
    sslMode: "disable",
    dsn: "",
  });
  const [reveal, setReveal] = useState(false);
  const [site, setSite] = useState({ title: "", url: "" });
  const [admin, setAdmin] = useState({
    username: "",
    email: "",
    password: "",
    confirm: "",
  });

  const [errors, setErrors] = useState<Record<string, string>>({});
  const [tested, setTested] = useState<DatabaseInfo | null>(null);
  const [testing, setTesting] = useState(false);
  const [testError, setTestError] = useState("");
  const [applying, setApplying] = useState(false);
  const [applyError, setApplyError] = useState("");
  const [result, setResult] = useState<ApplyResult | null>(null);

  const summaryRef = useRef<HTMLDivElement>(null);

  // 安装完成后服务要重启一轮，重启期间这个查询会失败几次 —— 那是预期内的，
  // 完成页据此显示「正在重启」而不是把链接摆出来让人点一个死链。
  const statusQuery = useQuery({
    queryKey: ["install-status"],
    queryFn: async (): Promise<StatusView | null> => {
      const { data, response } = await api.GET("/api/v1/install/status");
      if (!response.ok) {
        return null;
      }
      return data ?? null;
    },
    retry: false,
    refetchInterval: (query) =>
      result && !query.state.data?.installed ? 800 : false,
  });

  const stepIndex = Math.max(0, ORDER.indexOf(stepKey));
  const current = STEPS[stepIndex] ?? STEPS[0];
  const ready = statusQuery.data?.installed === true;

  // 已经有站点在跑时向导就该消失：直接去登录页。
  useEffect(() => {
    if (errors && Object.keys(errors).length > 0) {
      summaryRef.current?.focus();
    }
  }, [errors]);

  if (statusQuery.data?.installed && !result) {
    return <Navigate to="/login" replace />;
  }

  /** 组装数据库参数：两种填写方式二选一。 */
  function databasePayload(): DatabaseInput {
    if (dbMode === "dsn") {
      return { dsn: db.dsn.trim() };
    }
    return {
      host:
        db.host.trim().toLowerCase() === "localhost"
          ? "127.0.0.1"
          : db.host.trim(),
      port: Number(db.port),
      name: db.name.trim(),
      user: db.user.trim(),
      password: db.password,
      sslMode: db.sslMode,
    };
  }

  function validateStep(): Record<string, string> {
    const found: Record<string, string> = {};

    if (stepKey === "database") {
      if (dbMode === "dsn") {
        if (!db.dsn.trim()) {
          found.dsn = "请填写连接串";
        }
      } else {
        if (!db.host.trim()) {
          found.host = "请填写主机地址";
        }
        const port = Number(db.port);
        if (!/^\d+$/.test(db.port) || port < 1 || port > 65535) {
          found.port = "端口需为 1 到 65535 之间的数字";
        }
        if (!db.name.trim()) {
          found.name = "请填写数据库名";
        }
        if (!db.user.trim()) {
          found.user = "请填写数据库用户名";
        }
      }
    }

    if (stepKey === "site" && site.url.trim()) {
      if (!/^https?:\/\/[^\s]+$/.test(site.url.trim())) {
        found.url = "请填写以 http:// 或 https:// 开头的完整地址";
      }
    }

    if (stepKey === "admin") {
      if (!USERNAME_PATTERN.test(admin.username)) {
        found.username = "只能用小写字母、数字与连字符，2 到 64 位";
      }
      if (!/^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(admin.email)) {
        found.email = "请填写有效的邮箱地址";
      }
      if (admin.password.length < 8) {
        found.password = "口令至少 8 位";
      }
      if (admin.password !== admin.confirm) {
        found.confirm = "两次输入的口令不一致";
      }
    }

    return found;
  }

  function goNext() {
    const found = validateStep();
    setErrors(found);
    if (Object.keys(found).length > 0) {
      return;
    }
    const next = ORDER[stepIndex + 1];
    if (next) {
      setStepKey(next);
    }
  }

  function goBack() {
    setErrors({});
    const prev = ORDER[stepIndex - 1];
    if (prev) {
      setStepKey(prev);
    }
  }

  async function runTest() {
    const found = validateStep();
    setErrors(found);
    if (Object.keys(found).length > 0) {
      return;
    }
    setTesting(true);
    setTestError("");
    setTested(null);
    try {
      const { data, error, response } = await api.POST(
        "/api/v1/install/database/test",
        { body: databasePayload() },
      );
      if (!response.ok || !data) {
        setTestError(problemMessage(error));
        return;
      }
      setTested(data);
    } catch {
      setTestError("无法连接服务器，请确认程序仍在运行");
    } finally {
      setTesting(false);
    }
  }

  async function runApply() {
    setApplying(true);
    setApplyError("");
    try {
      const { data, error, response } = await api.POST(
        "/api/v1/install/apply",
        {
          body: {
            database: databasePayload(),
            site: { title: site.title.trim(), url: site.url.trim() },
            admin: {
              username: admin.username,
              email: admin.email,
              password: admin.password,
            },
          },
        },
      );
      if (!response.ok || !data) {
        setApplyError(problemMessage(error));
        return;
      }
      setResult(data);
    } catch {
      setApplyError(
        "安装过程中连接中断。若数据库与安装信息都已写入，重新打开本页即可继续。",
      );
    } finally {
      setApplying(false);
    }
  }

  /** 步骤圆点：当前用印色实底，已完成描边打勾，未到达只有序号。 */
  function stepDot(index: number) {
    const state =
      index === stepIndex ? "current" : index < stepIndex ? "done" : "todo";
    return (
      <span
        aria-hidden="true"
        className={cn(
          "flex size-6 shrink-0 items-center justify-center rounded-full text-xs font-medium tabular",
          state === "current" && "bg-seal text-seal-on",
          state === "done" && "border border-ok text-ok",
          state === "todo" && "border border-line-strong text-ink-subtle",
        )}
      >
        {state === "done" ? <Check className="size-3.5" /> : index + 1}
      </span>
    );
  }

  return (
    <div className="min-h-dvh bg-chrome lg:grid lg:grid-cols-[20rem_1fr]">
      <aside className="border-line border-b bg-surface lg:border-b-0 lg:border-r">
        <div className="flex flex-col gap-6 px-6 py-6 lg:sticky lg:top-0 lg:h-dvh lg:justify-between lg:py-10">
          <div className="flex flex-col gap-6">
            <Logo size="lg" />

            {/* 窄屏只有一条横向进度，纵向索引留给宽屏 */}
            <div className="flex items-center gap-2 lg:hidden">
              {STEPS.map((item, index) => (
                <div key={item.key} className="flex items-center gap-2">
                  {stepDot(index)}
                  {index < STEPS.length - 1 ? (
                    <span className="h-px w-4 bg-line" aria-hidden="true" />
                  ) : null}
                </div>
              ))}
              <span className="ml-1 text-sm font-medium text-ink">
                {current.title}
              </span>
            </div>

            <ol className="hidden flex-col lg:flex">
              {STEPS.map((item, index) => {
                const Icon = item.icon;
                const active = index === stepIndex;
                return (
                  <li
                    key={item.key}
                    className={cn(
                      "flex items-start gap-3 rounded-control px-3 py-2.5",
                      active && "bg-seal-soft",
                    )}
                  >
                    {stepDot(index)}
                    <div className="flex min-w-0 flex-col">
                      <span
                        className={cn(
                          "text-md",
                          active ? "font-medium text-ink" : "text-ink-muted",
                        )}
                      >
                        {item.title}
                      </span>
                      <span className="text-xs text-ink-muted">
                        {item.hint}
                      </span>
                    </div>
                    <Icon
                      aria-hidden="true"
                      className="ml-auto size-4 shrink-0 text-ink-subtle"
                    />
                  </li>
                );
              })}
            </ol>
          </div>

          <p className="hidden text-xs text-ink-muted lg:block">
            安装前站点不对外开放。安装信息保存在工作目录的 install.json （权限
            0600），其中含数据库口令。
          </p>
        </div>
      </aside>

      <main className="px-6 py-10 lg:py-[8vh]">
        <div className="mx-auto flex w-full max-w-[34rem] flex-col gap-6">
          {result ? (
            <ResultPanel result={result} ready={ready} />
          ) : (
            <>
              <header className="flex flex-col gap-1">
                <h1 className="text-2xl font-semibold text-ink">
                  {current.title}
                </h1>
                <p className="text-sm text-ink-muted">{STEP_INTRO[stepKey]}</p>
              </header>

              {errors && Object.keys(errors).length > 0 ? (
                <Alert
                  ref={summaryRef}
                  tabIndex={-1}
                  tone="danger"
                  title="还有几处要改"
                >
                  <ul className="flex flex-col gap-0.5">
                    {Object.entries(errors).map(([key, message]) => (
                      <li key={key}>{message}</li>
                    ))}
                  </ul>
                </Alert>
              ) : null}

              {applyError ? (
                <Alert tone="danger" title="安装未完成">
                  {applyError}
                </Alert>
              ) : null}

              <div className="flex flex-col gap-5 rounded-overlay border border-line bg-surface p-6">
                {stepKey === "database" ? (
                  <DatabaseStep
                    mode={dbMode}
                    onModeChange={(mode) => {
                      setDbMode(mode);
                      setTested(null);
                      setTestError("");
                      setErrors({});
                    }}
                    value={db}
                    onChange={setDb}
                    errors={errors}
                    reveal={reveal}
                    onToggleReveal={() => setReveal((v) => !v)}
                    tested={tested}
                    testing={testing}
                    testError={testError}
                    onTest={runTest}
                  />
                ) : null}

                {stepKey === "site" ? (
                  <SiteStep value={site} onChange={setSite} errors={errors} />
                ) : null}

                {stepKey === "admin" ? (
                  <AdminStep
                    value={admin}
                    onChange={setAdmin}
                    errors={errors}
                    reveal={reveal}
                    onToggleReveal={() => setReveal((v) => !v)}
                  />
                ) : null}

                {stepKey === "apply" ? (
                  <ReviewStep
                    db={databasePayload()}
                    site={site}
                    admin={admin}
                  />
                ) : null}
              </div>

              <div className="flex items-center justify-between gap-3">
                <Button
                  variant="ghost"
                  size="lg"
                  onClick={goBack}
                  disabled={stepIndex === 0 || applying}
                >
                  上一步
                </Button>

                {stepKey === "apply" ? (
                  <Button
                    variant="primary"
                    size="lg"
                    loading={applying}
                    onClick={runApply}
                  >
                    {applying ? "正在安装" : "开始安装"}
                  </Button>
                ) : (
                  <Button variant="primary" size="lg" onClick={goNext}>
                    下一步
                  </Button>
                )}
              </div>
            </>
          )}
        </div>
      </main>
    </div>
  );
}

function DatabaseStep({
  mode,
  onModeChange,
  value,
  onChange,
  errors,
  reveal,
  onToggleReveal,
  tested,
  testing,
  testError,
  onTest,
}: {
  mode: "fields" | "dsn";
  onModeChange: (mode: "fields" | "dsn") => void;
  value: DatabaseStepValue;
  onChange: (value: DatabaseStepValue) => void;
  errors: Record<string, string>;
  reveal: boolean;
  onToggleReveal: () => void;
  tested: DatabaseInfo | null;
  testing: boolean;
  testError: string;
  onTest: () => void;
}) {
  const set = (patch: Partial<DatabaseStepValue>) =>
    onChange({ ...value, ...patch });

  return (
    <>
      <div className="flex items-center gap-1 self-start rounded-control bg-surface-inset p-0.5">
        {(
          [
            ["fields", "逐项填写"],
            ["dsn", "粘贴连接串"],
          ] as const
        ).map(([key, label]) => (
          <button
            key={key}
            type="button"
            onClick={() => onModeChange(key)}
            className={cn(
              "transition-ui rounded-[3px] px-2.5 py-1 text-sm font-medium",
              mode === key
                ? "bg-surface text-ink shadow-card"
                : "text-ink-muted hover:text-ink",
            )}
          >
            {label}
          </button>
        ))}
      </div>

      {mode === "dsn" ? (
        <Field>
          <FieldLabel htmlFor="dsn">连接串</FieldLabel>
          <Input
            id="dsn"
            value={value.dsn}
            onChange={(e) => set({ dsn: e.target.value })}
            placeholder="postgres://用户:口令@主机:5432/库名?sslmode=disable"
            spellCheck={false}
            className="h-10 font-mono text-sm"
            aria-invalid={errors.dsn ? true : undefined}
            aria-describedby={errors.dsn ? "dsn-error" : "dsn-hint"}
          />
          {errors.dsn ? (
            <FieldError id="dsn-error">{errors.dsn}</FieldError>
          ) : (
            <FieldDescription id="dsn-hint">
              口令里的 @ : / 等字符需要按 URL 规则转义。
            </FieldDescription>
          )}
        </Field>
      ) : (
        <>
          <div className="grid grid-cols-3 gap-3">
            <Field className="col-span-2">
              <FieldLabel htmlFor="host">主机</FieldLabel>
              <Input
                id="host"
                value={value.host}
                onChange={(e) => set({ host: e.target.value })}
                spellCheck={false}
                className="h-10"
                aria-invalid={errors.host ? true : undefined}
                aria-describedby={errors.host ? "host-error" : undefined}
              />
              <FieldError id="host-error">{errors.host}</FieldError>
            </Field>

            <Field>
              <FieldLabel htmlFor="port">端口</FieldLabel>
              <Input
                id="port"
                value={value.port}
                onChange={(e) => set({ port: e.target.value })}
                inputMode="numeric"
                className="h-10 tabular"
                aria-invalid={errors.port ? true : undefined}
                aria-describedby={errors.port ? "port-error" : undefined}
              />
              <FieldError id="port-error">{errors.port}</FieldError>
            </Field>
          </div>

          <div className="grid grid-cols-2 gap-3">
            <Field>
              <FieldLabel htmlFor="dbname">数据库名</FieldLabel>
              <Input
                id="dbname"
                value={value.name}
                onChange={(e) => set({ name: e.target.value })}
                spellCheck={false}
                className="h-10"
                aria-invalid={errors.name ? true : undefined}
                aria-describedby={errors.name ? "dbname-error" : undefined}
              />
              <FieldError id="dbname-error">{errors.name}</FieldError>
            </Field>

            <Field>
              <FieldLabel htmlFor="dbuser">用户名</FieldLabel>
              <Input
                id="dbuser"
                value={value.user}
                onChange={(e) => set({ user: e.target.value })}
                spellCheck={false}
                autoComplete="off"
                className="h-10"
                aria-invalid={errors.user ? true : undefined}
                aria-describedby={errors.user ? "dbuser-error" : undefined}
              />
              <FieldError id="dbuser-error">{errors.user}</FieldError>
            </Field>
          </div>

          <Field>
            <FieldLabel htmlFor="dbpass">口令</FieldLabel>
            <div className="relative">
              <Input
                id="dbpass"
                type={reveal ? "text" : "password"}
                value={value.password}
                onChange={(e) => set({ password: e.target.value })}
                autoComplete="off"
                className="h-10 pr-10"
              />
              <button
                type="button"
                onClick={onToggleReveal}
                aria-pressed={reveal}
                className="transition-ui absolute top-1/2 right-1 flex size-8 -translate-y-1/2 items-center justify-center rounded-control text-ink-muted hover:bg-surface-active hover:text-ink"
              >
                {reveal ? (
                  <EyeOff aria-hidden="true" className="size-4" />
                ) : (
                  <Eye aria-hidden="true" className="size-4" />
                )}
                <span className="sr-only">
                  {reveal ? "隐藏口令" : "显示口令"}
                </span>
              </button>
            </div>
          </Field>

          <Field>
            <FieldLabel htmlFor="sslmode">TLS 模式</FieldLabel>
            <Select
              value={value.sslMode}
              onValueChange={(mode) =>
                set({ sslMode: mode as NonNullable<DatabaseInput["sslMode"]> })
              }
            >
              <SelectTrigger id="sslmode" className="h-10">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="disable">disable（本机或内网）</SelectItem>
                <SelectItem value="prefer">prefer（能加密就加密）</SelectItem>
                <SelectItem value="require">require（必须加密）</SelectItem>
                <SelectItem value="verify-full">
                  verify-full（校验证书）
                </SelectItem>
              </SelectContent>
            </Select>
            <FieldDescription>
              云数据库通常要求 require 或 verify-full。
            </FieldDescription>
          </Field>
        </>
      )}

      <div className="flex flex-col gap-3 border-line border-t pt-5">
        <div className="flex items-center gap-3">
          <Button
            variant="secondary"
            onClick={onTest}
            loading={testing}
            disabled={testing}
          >
            {testing ? "正在测试" : "测试连接"}
          </Button>
          {tested ? (
            <span className="flex items-center gap-1.5 text-sm text-ok">
              <Check aria-hidden="true" className="size-4" />
              连接正常
            </span>
          ) : null}
        </div>

        {testError ? (
          <Alert tone="danger" title="连接失败">
            <span className="break-all">{testError}</span>
          </Alert>
        ) : null}

        {tested ? (
          <Alert
            tone={tested.existingUsers > 0 ? "warn" : "ok"}
            title={
              tested.existingUsers > 0
                ? "这个库里已经有一个 Lumo 站点"
                : "数据库可用"
            }
          >
            <span className="flex flex-col gap-0.5">
              <span className="break-all">{tested.serverVersion}</span>
              <span className="tabular">
                数据库 {tested.database}，用户 {tested.user}，编码{" "}
                {tested.encoding}，握手耗时 {tested.latencyMs} ms
              </span>
              {tested.existingUsers > 0 ? (
                <span>
                  现有 {tested.existingUsers} 个用户，继续安装会接管这个站点，
                  不会新建管理员，也不会清空已有内容。
                </span>
              ) : null}
            </span>
          </Alert>
        ) : null}
      </div>
    </>
  );
}

type DatabaseStepValue = {
  host: string;
  port: string;
  name: string;
  user: string;
  password: string;
  /** 取值与接口的枚举一致，Select 的 onChange 只回字符串故在此收窄。 */
  sslMode: NonNullable<DatabaseInput["sslMode"]>;
  dsn: string;
};

function SiteStep({
  value,
  onChange,
  errors,
}: {
  value: { title: string; url: string };
  onChange: (value: { title: string; url: string }) => void;
  errors: Record<string, string>;
}) {
  return (
    <>
      <Field>
        <FieldLabel htmlFor="site-title">站点名称</FieldLabel>
        <Input
          id="site-title"
          value={value.title}
          onChange={(e) => onChange({ ...value, title: e.target.value })}
          placeholder="我的站点"
          className="h-10"
          autoFocus
        />
        <FieldDescription>留空则使用 Lumo，之后可在后台改。</FieldDescription>
      </Field>

      <Field>
        <FieldLabel htmlFor="site-url">对外地址</FieldLabel>
        <Input
          id="site-url"
          value={value.url}
          onChange={(e) => onChange({ ...value, url: e.target.value })}
          placeholder="https://example.com"
          spellCheck={false}
          className="h-10"
          aria-invalid={errors.url ? true : undefined}
          aria-describedby={errors.url ? "site-url-error" : "site-url-hint"}
        />
        {errors.url ? (
          <FieldError id="site-url-error">{errors.url}</FieldError>
        ) : (
          <FieldDescription id="site-url-hint">
            带协议、不带末尾斜杠。留空时站点地图与订阅源会暂时不可用。
          </FieldDescription>
        )}
      </Field>
    </>
  );
}

function AdminStep({
  value,
  onChange,
  errors,
  reveal,
  onToggleReveal,
}: {
  value: {
    username: string;
    email: string;
    password: string;
    confirm: string;
  };
  onChange: (value: AdminStepValue) => void;
  errors: Record<string, string>;
  reveal: boolean;
  onToggleReveal: () => void;
}) {
  const set = (patch: Partial<AdminStepValue>) =>
    onChange({ ...value, ...patch });

  return (
    <>
      <Field>
        <FieldLabel htmlFor="admin-user">用户名</FieldLabel>
        <Input
          id="admin-user"
          value={value.username}
          onChange={(e) => set({ username: e.target.value.trim() })}
          autoComplete="username"
          spellCheck={false}
          autoFocus
          className="h-10"
          aria-invalid={errors.username ? true : undefined}
          aria-describedby={
            errors.username ? "admin-user-error" : "admin-user-hint"
          }
        />
        {errors.username ? (
          <FieldError id="admin-user-error">{errors.username}</FieldError>
        ) : (
          <FieldDescription id="admin-user-hint">
            登录后台与出现在文章署名处，用小写字母、数字与连字符。
          </FieldDescription>
        )}
      </Field>

      <Field>
        <FieldLabel htmlFor="admin-email">邮箱</FieldLabel>
        <Input
          id="admin-email"
          type="email"
          value={value.email}
          onChange={(e) => set({ email: e.target.value.trim() })}
          autoComplete="email"
          spellCheck={false}
          className="h-10"
          aria-invalid={errors.email ? true : undefined}
          aria-describedby={errors.email ? "admin-email-error" : undefined}
        />
        <FieldError id="admin-email-error">{errors.email}</FieldError>
      </Field>

      <div className="grid gap-3 sm:grid-cols-2">
        <Field>
          <FieldLabel htmlFor="admin-pass">口令</FieldLabel>
          <div className="relative">
            <Input
              id="admin-pass"
              type={reveal ? "text" : "password"}
              value={value.password}
              onChange={(e) => set({ password: e.target.value })}
              autoComplete="new-password"
              className="h-10 pr-10"
              aria-invalid={errors.password ? true : undefined}
              aria-describedby={
                errors.password ? "admin-pass-error" : undefined
              }
            />
            <button
              type="button"
              onClick={onToggleReveal}
              aria-pressed={reveal}
              className="transition-ui absolute top-1/2 right-1 flex size-8 -translate-y-1/2 items-center justify-center rounded-control text-ink-muted hover:bg-surface-active hover:text-ink"
            >
              {reveal ? (
                <EyeOff aria-hidden="true" className="size-4" />
              ) : (
                <Eye aria-hidden="true" className="size-4" />
              )}
              <span className="sr-only">
                {reveal ? "隐藏口令" : "显示口令"}
              </span>
            </button>
          </div>
          <FieldError id="admin-pass-error">{errors.password}</FieldError>
        </Field>

        <Field>
          <FieldLabel htmlFor="admin-confirm">再输一次</FieldLabel>
          <Input
            id="admin-confirm"
            type={reveal ? "text" : "password"}
            value={value.confirm}
            onChange={(e) => set({ confirm: e.target.value })}
            autoComplete="new-password"
            className="h-10"
            aria-invalid={errors.confirm ? true : undefined}
            aria-describedby={
              errors.confirm ? "admin-confirm-error" : undefined
            }
          />
          <FieldError id="admin-confirm-error">{errors.confirm}</FieldError>
        </Field>
      </div>

      <FieldDescription>
        建议 12 位以上，混用大小写字母、数字与符号。
      </FieldDescription>
    </>
  );
}

type AdminStepValue = {
  username: string;
  email: string;
  password: string;
  confirm: string;
};

/** 安装前的确认清单：把即将写入的东西逐条摆出来。 */
function ReviewStep({
  db,
  site,
  admin,
}: {
  db: DatabaseInput;
  site: { title: string; url: string };
  admin: { username: string; email: string };
}) {
  const target = db.dsn
    ? maskDsn(db.dsn)
    : `${db.user ?? ""}@${db.host ?? ""}:${db.port ?? ""}/${db.name ?? ""}`;

  const rows: Array<[string, string]> = [
    ["数据库", target],
    ["站点名称", site.title.trim() || "Lumo（默认）"],
    ["对外地址", site.url.trim() || "暂不设置"],
    ["管理员", `${admin.username}（${admin.email}）`],
  ];

  return (
    <>
      <dl className="flex flex-col">
        {rows.map(([label, value]) => (
          <div
            key={label}
            className="flex items-baseline justify-between gap-4 border-line border-b py-2.5 last:border-b-0"
          >
            <dt className="shrink-0 text-sm text-ink-muted">{label}</dt>
            <dd className="min-w-0 truncate text-right text-sm text-ink">
              {value}
            </dd>
          </div>
        ))}
      </dl>
      <Alert tone="info">
        安装会在这个数据库里建表。若库里已经有 Lumo 的数据，迁移是幂等的，
        已有内容不会被清空。
      </Alert>
    </>
  );
}

/** 把连接串里的口令遮掉，确认页上不该出现明文。 */
function maskDsn(dsn: string): string {
  return dsn.replace(/:\/\/([^:@/]+):[^@/]*@/, "://$1:••••@");
}

function ResultPanel({
  result,
  ready,
}: {
  result: ApplyResult;
  ready: boolean;
}) {
  return (
    <>
      <header className="flex flex-col gap-1">
        <h1 className="text-2xl font-semibold text-ink">安装完成</h1>
        <p className="text-sm text-ink-muted">
          {result.adminCreated
            ? "站点已经建好，用刚才的账号登录后台即可开始。"
            : "目标库里已有用户，安装接管了这个站点，未新建管理员。"}
        </p>
      </header>

      <div className="flex flex-col gap-5 rounded-overlay border border-line bg-surface p-6">
        <ul className="flex flex-col gap-2.5">
          {(result.steps ?? []).map((step) => (
            <li key={step.key} className="flex items-center gap-2.5">
              <Check aria-hidden="true" className="size-4 shrink-0 text-ok" />
              <span className="text-md text-ink">{step.label}</span>
              <span className="tabular ml-auto text-xs text-ink-muted">
                {step.durationMs} ms
              </span>
            </li>
          ))}
        </ul>

        <div className="flex items-center gap-2 border-line border-t pt-4 text-sm text-ink-muted">
          {ready ? (
            <>
              <Check aria-hidden="true" className="size-4 text-ok" />
              服务已重启，站点可以访问
            </>
          ) : (
            <>
              <Loader2
                aria-hidden="true"
                className="size-4 animate-spin motion-reduce:animate-none"
              />
              正在以正常模式重启服务
            </>
          )}
        </div>
      </div>

      {/* 服务重启期间链接先不出现：摆一个点不通的按钮比让它晚几秒出现更糟 */}
      <div className="flex flex-wrap items-center gap-3">
        {ready ? (
          <>
            <Button variant="primary" size="lg" asChild>
              <a href="/console/">进入管理后台</a>
            </Button>
            <Button variant="secondary" size="lg" asChild>
              <a href="/">打开站点首页</a>
            </Button>
          </>
        ) : (
          <>
            <Button variant="primary" size="lg" disabled>
              进入管理后台
            </Button>
            <Button variant="secondary" size="lg" disabled>
              打开站点首页
            </Button>
          </>
        )}
      </div>

      <p className="text-xs text-ink-muted">
        安装信息保存在工作目录的 install.json 中，升级与备份时请一并保留。
      </p>
    </>
  );
}
