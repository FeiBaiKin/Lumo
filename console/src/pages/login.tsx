import { useAuth } from "@/components/auth/auth-provider";
import { Logo } from "@/components/layout/logo";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Field, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { useDocumentTitle } from "@/lib/use-document-title";
import { Eye, EyeOff } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { Navigate, useLocation } from "react-router";

/**
 * 登录页（形态对齐 Halo 的登录页）：白底、居中一列，字标在上、表单卡片在下。
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
    // 已登录就直接进后台；from 保留原本想去的页面，登录后回到那里。
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
    <div className="min-h-dvh bg-surface px-4 py-[8vh]">
      <div className="mx-auto flex w-full max-w-[26rem] flex-col items-center gap-8">
        <Logo size="lg" />

        <div className="flex w-full flex-col gap-5 rounded-overlay border border-line bg-surface p-6">
          <h1 className="text-lg font-semibold text-ink">登录以管理你的站点</h1>

          {error ? (
            <Alert ref={errorRef} tabIndex={-1} tone="danger" title="登录失败">
              {error}
            </Alert>
          ) : null}

          <form onSubmit={onSubmit} className="flex flex-col gap-4">
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
                className="h-10"
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
                  className="h-10 pr-10"
                />
                <button
                  type="button"
                  onClick={() => setReveal((v) => !v)}
                  aria-pressed={reveal}
                  className="transition-ui absolute top-1/2 right-1 flex size-8 -translate-y-1/2 items-center justify-center rounded-control text-ink-muted hover:bg-surface-active hover:text-ink"
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
              loading={pending}
              disabled={!name || !password}
              className="mt-1 w-full"
            >
              {pending ? "正在登录" : "登录"}
            </Button>
          </form>
        </div>

        <a
          href="/"
          className="transition-ui text-sm text-ink-muted hover:text-ink"
        >
          返回站点
        </a>
      </div>
    </div>
  );
}
