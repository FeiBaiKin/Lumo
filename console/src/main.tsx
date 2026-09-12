import { queryClient } from "@/api/query-client";
import { App } from "@/app";
import { AuthProvider } from "@/components/auth/auth-provider";
import { ThemeProvider } from "@/components/theme/theme-provider";
import { TooltipProvider } from "@/components/ui/tooltip";
import { QueryClientProvider } from "@tanstack/react-query";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router";
import { Toaster } from "sonner";
import "@/styles/global.css";

const container = document.getElementById("root");
if (!container) {
  throw new Error("未找到 #root 挂载点");
}

createRoot(container).render(
  <StrictMode>
    {/*
      主题在最外层：它只依赖 localStorage 与系统设置，
      放在 QueryClient 之内没有任何好处，反而会拖慢首屏上色。

      QueryClient 与命令式代码（api/mutation.ts）共用同一个实例，
      见 api/query-client.ts 的说明。

      basename 必须是 /console：SPA 由 Go 服务端挂在 /console/ 下并提供
      history 回退（internal/console），浏览器地址栏里的路径始终带这个前缀。
    */}
    <ThemeProvider>
      <QueryClientProvider client={queryClient}>
        <TooltipProvider delayDuration={400}>
          <BrowserRouter basename="/console">
            <AuthProvider>
              <App />
              {/* 操作结果统一走 toast 播报，顶部居中（Halo 同位）。
                  成功与失败的区分由图标与文案承担，不靠背景色 */}
              <Toaster position="top-center" closeButton />
            </AuthProvider>
          </BrowserRouter>
        </TooltipProvider>
      </QueryClientProvider>
    </ThemeProvider>
  </StrictMode>,
);
