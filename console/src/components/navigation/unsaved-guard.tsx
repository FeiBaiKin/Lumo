import { ConfirmDialog } from "@/components/ui/dialog";
import { type ReactNode, use } from "react";
import { UNSAFE_DataRouterContext, useBlocker } from "react-router";

/**
 * 未保存修改的导航保护。
 *
 * 覆盖站内导航的三条出口：`<Link>` 点击、命令式 `navigate()`、浏览器前进/后退。
 * 它们都不会触发 `beforeunload`，所以单靠那个监听器（过去只有它）等于没有保护：
 * 改完标题点一下侧栏「文章」，修改就悄无声息地没了。
 *
 * 刷新与关闭标签页仍由调用方的 `beforeunload` 负责——useBlocker 管不到浏览器之外的动作。
 *
 * 依赖数据路由（createBrowserRouter，见 main.tsx）。没有数据路由时（组件单测、
 * 把表单单独渲染的场景）直接不渲染拦截器：useBlocker 在没有数据路由时会抛错，
 * 而「少一层站内保护」远好过让整个组件崩掉。
 */
export function UnsavedChangesGuard({
  when,
  title = "有未保存的修改",
  consequence,
  confirmLabel = "放弃修改并离开",
}: {
  /**
   * 返回真表示当前有未保存的修改。
   *
   * 传函数而不是布尔值：调用方用 ref 读取最新的脏状态，
   * 避免「保存成功的同一次渲染里导航」被过期的闭包值挡住。
   */
  when: () => boolean;
  title?: string;
  consequence?: ReactNode;
  confirmLabel?: string;
}) {
  // UNSAFE_ 前缀是 react-router 对这类内部上下文的一贯标注；这里只用它做
  // 「有没有数据路由」的判定，不读其中任何字段。
  const dataRouter = use(UNSAFE_DataRouterContext);
  if (!dataRouter) {
    return null;
  }
  return (
    <Blocker
      when={when}
      title={title}
      consequence={consequence}
      confirmLabel={confirmLabel}
    />
  );
}

/** 真正调用 useBlocker 的部分；只在数据路由下渲染。 */
function Blocker({
  when,
  title,
  consequence,
  confirmLabel,
}: {
  when: () => boolean;
  title: string;
  consequence?: ReactNode;
  confirmLabel: string;
}) {
  const blocker = useBlocker(() => when());

  return (
    <ConfirmDialog
      open={blocker.state === "blocked"}
      onOpenChange={(open) => {
        if (!open) {
          // 取消：留在当前页面，并解除这次拦截。
          blocker.reset?.();
        }
      }}
      title={title}
      consequence={consequence ?? <p>离开当前页面后，尚未保存的修改会丢失。</p>}
      confirmLabel={confirmLabel}
      cancelLabel="留下继续编辑"
      destructive={false}
      onConfirm={() => blocker.proceed?.()}
    />
  );
}
