import path from "node:path";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
// 从 vitest/config 导入，使同一份配置同时覆盖构建与测试。
import { defineConfig } from "vitest/config";

// Console 构建产物直接输出到 Go 的 embed 目标目录（internal/console/dist）。
// base 必须与后端挂载路径 /console/ 一致，否则 index.html 里的资源地址会 404。
export default defineConfig({
  base: "/console/",
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": path.resolve(import.meta.dirname, "./src"),
    },
  },
  build: {
    outDir: path.resolve(import.meta.dirname, "../internal/console/dist"),
    // 不清空目录：.gitkeep 必须保留，否则 go:embed all:dist 编译失败。
    // 旧产物的清理交给 Taskfile 的 console:clean（task console:build 会先跑它），
    // 直接 npm run build / vite build 会绕过清理，构建 Console 请走 task。
    emptyOutDir: false,
    sourcemap: false,
    chunkSizeWarningLimit: 1024,
  },
  server: {
    port: 5173,
    proxy: {
      // 开发态把 API 请求代理到本地 Go 服务，避免 CORS 与 Cookie 问题。
      "/api": { target: "http://127.0.0.1:8080", changeOrigin: true },
      "/apis": { target: "http://127.0.0.1:8080", changeOrigin: true },
      "/healthz": { target: "http://127.0.0.1:8080", changeOrigin: true },
      // 附件由后端以静态文件提供。不代理的话，HMR 模式下后台里的每张图
      // （附件库、选择器、封面预览）都是破图，走查时会误以为是界面坏了。
      "/uploads": { target: "http://127.0.0.1:8080", changeOrigin: true },
    },
  },
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./src/test/setup.ts"],
    css: false,
  },
});
