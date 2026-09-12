import { queryClient } from "@/api/query-client";
import { appRoutes } from "@/app";
import { AuthProvider } from "@/components/auth/auth-provider";
import { ThemeProvider } from "@/components/theme/theme-provider";
import { TooltipProvider } from "@/components/ui/tooltip";
import { QueryClientProvider } from "@tanstack/react-query";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { RouterProvider, createBrowserRouter } from "react-router";
import { Toaster } from "sonner";
import "@/styles/global.css";

const container = document.getElementById("root");
if (!container) {
  throw new Error("未找到 #root 挂载点");
}

/*
 * 用数据路由而不是 <BrowserRouter> + <Routes>：编辑器与设置表单靠 useBlocker
 * 拦截站内导航（侧栏、命令面板、浏览器后退），而 useBlocker 只在数据路由下可用。
 *
 * basename 必须是 /console：SPA 由 Go 服务端挂在 /console/ 下并提供 history 回退
 * （internal/console），浏览器地址栏里的路径始终带这个前缀。
 */
const router = createBrowserRouter(appRoutes(), { basename: "/console" });

createRoot(container).render(
  <StrictMode>
    {/*
      主题在最外层：它只依赖 localStorage 与系统设置，
      放在 QueryClient 之内没有任何好处，反而会拖慢首屏上色。

      QueryClient 与命令式代码（api/mutation.ts）共用同一个实例，
      见 api/query-client.ts 的说明。

      AuthProvider 放在路由之外：它不依赖 router，而 RequireAuth 是路由里的元素，
      照常能读到这里的上下文。
    */}
    <ThemeProvider>
      <QueryClientProvider client={queryClient}>
        <TooltipProvider delayDuration={400}>
          <AuthProvider>
            <RouterProvider router={router} />
            {/* 操作结果统一走 toast 播报，顶部居中（Halo 同位）。
                成功与失败的区分由图标与文案承担，不靠背景色 */}
            <Toaster position="top-center" closeButton />
          </AuthProvider>
        </TooltipProvider>
      </QueryClientProvider>
    </ThemeProvider>
  </StrictMode>,
);
