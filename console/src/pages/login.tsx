import { useAuth } from "@/components/auth/auth-provider";
import { Button } from "@/components/ui/button";
import { Field, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { useDocumentTitle } from "@/lib/use-document-title";
import { Eye, EyeOff, Loader2 } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { Navigate, useLocation } from "react-router";

/**
 * 登录页。
 *
 * 没有注册入口 —— v1 不开访客注册（agent.md §7.1），账号由管理员在后台或 CLI 创建。
 * 因此这里不写「还没有账号？」那类链接：指向一个不存在的功能比不写更糟。
 *
 * 失败提示只说「用户名或密码不正确」，不区分账号是否存在：
 * 服务端两侧返回逐字节相同的响应（防账号枚举），前端不该把这个信息又漏回去。
 */
export function LoginPage() {
  const { login, isAnonymous, isLoading } = useAuth();
  const location = useLocation();
  const [name, setName] = useState("");
  const [password, setPassword] = useState("");
  const [reveal, setReveal] = useState(false);
  const [error, setError] = useState("");
  const [pending, setPending] = useState(false);
  const errorRef = useRef<HTMLDivElement>(null);

  useDocumentTitle("登录");

  // 校验失败后把焦点移到错误摘要 —— 只画一个红框，键盘与读屏用户不会知道发生了什么。
  useEffect(() => {
    if (error) {
      errorRef.current?.focus();
    }
  }, [error]);

  if (!isLoading && !isAnonymous) {
    // 已登录就直接进后台；redirectTo 保留原本想去的页面，登录后回到那里。
    const from = (location.state as { from?: string } | null)?.from;
    return <Navigate to={from ?? "/"} replace />;
  }

  async function onSubmit(event: React.FormEvent) {
    event.preventDefault();
    setError("");
    setPending(true);
    try {
      await login(name.trim(), password);
    } catch (err) {
      setError(err instanceof Error ? err.message : "登录失败，请重试");
      setPassword("");
    } finally {
      setPending(false);
    }
  }

  return (
    <div className="flex min-h-dvh flex-col items-center justify-center bg-chrome px-4 py-10">
      {/*
        整页唯一的一级标题，由字标与说明两行共同构成。
        字标用纯文字排出来，不画图形 logo —— 理由与「墨 Ink」不用 Web 字体同源：
        这个界面要能离线跑，而一个纯文字字标在任何机器上都长一样。
        两行合成同一个 h1，而不是「品牌用 p、说明也用 p」：
        屏幕阅读器靠 h1 确认当前在哪一页，而登录页此前一个标题都没有。
      */}
      <div className="flex w-full max-w-sm flex-col gap-6">
        <h1 className="flex flex-col items-center gap-1.5">
          <span className="text-2xl font-semibold tracking-tight text-ink">
            Lumo
          </span>
          <span className="text-sm font-normal text-ink-muted">
            登录以管理你的站点
          </span>
        </h1>

        <form
          onSubmit={onSubmit}
          className="flex flex-col gap-4 rounded-panel border border-line bg-surface p-6"
        >
          {/* 错误摘要：可聚焦、role=alert，位置固定在表单顶部（表单项多了才看得出价值） */}
          {error ? (
            <div
              ref={errorRef}
              tabIndex={-1}
              role="alert"
              className="flex flex-col gap-0.5 rounded-control border border-danger bg-danger-soft px-3 py-2"
            >
              <p className="text-sm font-medium text-danger">登录失败</p>
              <p className="text-xs text-danger">{error}</p>
            </div>
          ) : null}

          <Field>
            <FieldLabel htmlFor="login">用户名或邮箱</FieldLabel>
            <Input
              id="login"
              name="login"
              value={name}
              onChange={(e) => setName(e.target.value)}
              autoComplete="username"
              autoFocus
              required
              aria-invalid={error ? true : undefined}
            />
          </Field>

          <Field>
            <FieldLabel htmlFor="password">密码</FieldLabel>
            <div className="relative">
              <Input
                id="password"
                name="password"
                type={reveal ? "text" : "password"}
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                autoComplete="current-password"
                required
                aria-invalid={error ? true : undefined}
                // 右侧留出显隐按钮的位置，避免文字压到按钮底下
                className="pr-10"
              />
              <button
                type="button"
                onClick={() => setReveal((v) => !v)}
                aria-pressed={reveal}
                className="transition-ui absolute top-1/2 right-1 flex size-7 -translate-y-1/2 items-center justify-center rounded-control text-ink-muted hover:bg-surface-active hover:text-ink"
              >
                {reveal ? (
                  <EyeOff aria-hidden="true" className="size-4" />
                ) : (
                  <Eye aria-hidden="true" className="size-4" />
                )}
                <span className="sr-only">
                  {reveal ? "隐藏密码" : "显示密码"}
                </span>
              </button>
            </div>
          </Field>

          <Button
            type="submit"
            variant="primary"
            size="lg"
            disabled={pending || !name || !password}
            className="mt-1 w-full"
          >
            {pending ? (
              <Loader2 aria-hidden="true" className="animate-spin" />
            ) : null}
            {pending ? "正在登录" : "登录"}
          </Button>
        </form>
      </div>
    </div>
  );
}
