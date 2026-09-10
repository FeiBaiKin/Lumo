import { App } from "@/app";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import "@/styles/global.css";

const container = document.getElementById("root");
if (!container) {
  throw new Error("未找到 #root 挂载点");
}

createRoot(container).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
