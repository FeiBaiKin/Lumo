import { useAuth } from "@/components/auth/auth-provider";
import { AppShell } from "@/components/layout/app-shell";
import { LogoMark } from "@/components/layout/logo";
import { DashboardPage } from "@/pages/dashboard";
import { LoginPage } from "@/pages/login";
import { NotFoundPage } from "@/pages/not-found";
import { APP_ROUTES } from "@/pages/routes";
import type { RouteObject } from "react-router";
import { Navigate, useLocation } from "react-router";

/**
 * 路由装配。
 *
 * 两层：`/login` 独立成页（无外壳），其余全部经 `RequireAuth` 进 `AppShell`。
 * 守卫放在路由层而不是各页面里 —— 新增页面时忘了加守卫是很容易犯的错，
 * 而那样的错意味着整个页面在未登录时可用。
 *
 * 页面的路径与组件在 `pages/routes.tsx` 集中登记；本文件只管守卫与外壳，
 * 这样「加一页」不需要动这里。
 *
 * 导出的是**路由对象**而不是 `<Routes>` 元素：未保存修改的保护要用 useBlocker
 * 拦截站内导航，而它只在数据路由（createBrowserRouter）下可用。
 */

function FullPageLoading({ label }: { label: string }) {
  return (
    <div
      className="flex min-h-dvh flex-col items-center justify-center gap-3 bg-surface text-ink-muted"
      aria-busy="true"
    >
      <LogoMark className="size-10 animate-pulse" />
      <span className="text-sm">{label}</span>
    </div>
  );
}

/**
 * 登录守卫。
 *
 * 关键是区分「会话还没查出来」与「确实没登录」：
 * 前者要显示加载态，直接跳登录页会让每次刷新都闪一下登录界面。
 * 跳转时把当前地址放进 state，登录后能回到原处。
 */
function RequireAuth({ children }: { children: React.ReactNode }) {
  const { isLoading, isAnonymous } = useAuth();
  const location = useLocation();

  if (isLoading) {
    return <FullPageLoading label="正在校验会话" />;
  }
  if (isAnonymous) {
    return <Navigate to="/login" replace state={{ from: location.pathname }} />;
  }
  return children;
}

/** 应用路由表；由 main.tsx 交给 createBrowserRouter。 */
export function appRoutes(): RouteObject[] {
  const pages = APP_ROUTES.map((route): RouteObject | null => {
    if (route.element === null) {
      return null;
    }
    const Page = route.element;
    return { path: route.path, element: <Page /> };
  }).filter((route): route is RouteObject => route !== null);

  return [
    { path: "/login", element: <LoginPage /> },
    {
      element: (
        <RequireAuth>
          <AppShell />
        </RequireAuth>
      ),
      children: [
        { index: true, element: <DashboardPage /> },
        ...pages,
        { path: "*", element: <NotFoundPage /> },
      ],
    },
  ];
}
