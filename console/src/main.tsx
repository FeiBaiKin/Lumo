import { App } from "@/app";
import { AuthProvider } from "@/components/auth/auth-provider";
import { ThemeProvider } from "@/components/theme/theme-provider";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router";
import { Toaster } from "sonner";
import "@/styles/global.css";

const container = document.getElementById("root");
if (!container) {
  throw new Error("未找到 #root 挂载点");
}

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // 后台的数据变更几乎全部由本页发起，窗口聚焦时全量重取只会造成无谓的闪烁。
      // 需要即时性的地方（如搜索）在各自的查询上单独调。
      refetchOnWindowFocus: false,
      staleTime: 30_000,
      // 401 不重试：它是「未登录」这一正常状态，重试只会拖慢跳转登录页。
      retry: (failureCount, error) => {
        if (error instanceof Error && error.message.includes("401")) {
          return false;
        }
        return failureCount < 2;
      },
    },
    mutations: {
      retry: false,
    },
  },
});

createRoot(container).render(
  <StrictMode>
    {/*
      主题在最外层：它只依赖 localStorage 与系统设置，
      放在 QueryClient 之内没有任何好处，反而会拖慢首屏上色。

      basename 必须是 /console：SPA 由 Go 服务端挂在 /console/ 下并提供
      history 回退（internal/console），浏览器地址栏里的路径始终带这个前缀。
    */}
    <ThemeProvider>
      <QueryClientProvider client={queryClient}>
        <BrowserRouter basename="/console">
          <AuthProvider>
            <App />
            {/* 操作结果统一走 toast 播报。richColors 关掉 —— 成功与失败的区分
                由图标与文案承担，不靠背景色，与 badge 的规则一致 */}
            <Toaster position="bottom-right" closeButton />
          </AuthProvider>
        </BrowserRouter>
      </QueryClientProvider>
    </ThemeProvider>
  </StrictMode>,
);
