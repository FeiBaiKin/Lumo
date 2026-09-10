import { Loader2, ServerCog } from "lucide-react";
import { useEffect, useState } from "react";

/*
 * 阶段 0 占位外壳。
 *
 * agent.md §11.1 规定：任何前端设计工作开始前必须先调用本地前端设计 skill 统筹。
 * 因此本文件刻意不做任何视觉方向决策（无配色、无字体、无布局体系），
 * 只验证「Vite 构建 → go:embed → Go 静态服务 → 调用后端 API」这条链路通畅。
 * 真正的应用外壳与七组导航在阶段 7 落地，届时会先产出设计方案。
 */

type Health = {
  status: string;
  version: string;
};

type State =
  | { kind: "loading" }
  | { kind: "ready"; health: Health }
  | { kind: "error"; message: string };

export function App() {
  const [state, setState] = useState<State>({ kind: "loading" });

  useEffect(() => {
    const controller = new AbortController();

    async function probe() {
      try {
        const res = await fetch("/healthz", { signal: controller.signal });
        if (!res.ok) {
          throw new Error(`HTTP ${res.status}`);
        }
        const health = (await res.json()) as Health;
        setState({ kind: "ready", health });
      } catch (err) {
        if (controller.signal.aborted) {
          return;
        }
        setState({
          kind: "error",
          message: err instanceof Error ? err.message : "未知错误",
        });
      }
    }

    void probe();
    return () => controller.abort();
  }, []);

  return (
    <main className="flex min-h-dvh items-center justify-center p-8">
      <div className="flex flex-col items-center gap-3 text-center">
        <ServerCog aria-hidden="true" className="size-8" />
        <h1 className="text-xl font-semibold">Lumo Console</h1>
        <p className="text-sm">脚手架就绪 · 界面将在阶段 7 实现</p>
        <p aria-live="polite" className="text-sm">
          {state.kind === "loading" && (
            <span className="inline-flex items-center gap-2">
              <Loader2 aria-hidden="true" className="size-4 animate-spin" />
              正在连接后端
            </span>
          )}
          {state.kind === "ready" &&
            `后端已连通 · 状态 ${state.health.status} · 版本 ${state.health.version}`}
          {state.kind === "error" && `后端未连通（${state.message}）`}
        </p>
      </div>
    </main>
  );
}
