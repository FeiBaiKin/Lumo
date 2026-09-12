import { cn } from "@/lib/utils";
import {
  Editor,
  defaultValueCtx,
  editorViewOptionsCtx,
  rootCtx,
} from "@milkdown/kit/core";
import { clipboard } from "@milkdown/kit/plugin/clipboard";
import { cursor } from "@milkdown/kit/plugin/cursor";
import { history } from "@milkdown/kit/plugin/history";
import { listener, listenerCtx } from "@milkdown/kit/plugin/listener";
import { commonmark } from "@milkdown/kit/preset/commonmark";
import { gfm } from "@milkdown/kit/preset/gfm";
import { Milkdown, MilkdownProvider, useEditor } from "@milkdown/react";
import { useRef } from "react";

/**
 * Markdown 编辑器（Milkdown 7）。
 *
 * 与块编辑器**产出同一对字段**：`raw` + `rawType`（agent.md §3.4）。
 * 两个编辑器都是 ProseMirror 系，但存下来的东西不同：这里是 Markdown 源码，
 * 那边是规范 HTML。作者按习惯选，主题只消费渲染结果，换编辑器不伤主题。
 *
 * 三条实现上的取舍：
 *
 *   1. **不暴露 handle**。父页面通过 onChange 持有最新值即可 ——
 *      再给一个 getMarkdown() 只会让「该读哪个」出现两个答案。
 *      需要重置内容时，用 key 让编辑器整体重建（见 ContentEditor 的用法）。
 *   2. **回调放 ref**。把 onChange 放进 useEditor 的依赖数组会让编辑器
 *      每次渲染都重建，表现为「打一个字光标就跳回开头」。
 *   3. **语法集必须与服务端对齐**。commonmark 是基础，gfm 补表格、
 *      任务列表、删除线与自动链接；服务端用 goldmark + extension.GFM 渲染。
 *      这里少一个扩展，作者就会遇到「编辑器里能写、发表后不生效」。
 */

export function MarkdownEditor({
  initialContent,
  onChange,
  className,
}: {
  initialContent: string;
  onChange: (markdown: string) => void;
  // 显式 `| undefined`：exactOptionalPropertyTypes 下透传可选值必须允许它
  className?: string | undefined;
}) {
  return (
    <MilkdownProvider>
      <Surface
        initialContent={initialContent}
        onChange={onChange}
        className={className}
      />
    </MilkdownProvider>
  );
}

function Surface({
  initialContent,
  onChange,
  className,
}: {
  initialContent: string;
  onChange: (markdown: string) => void;
  className?: string | undefined;
}) {
  const changeRef = useRef(onChange);
  changeRef.current = onChange;

  useEditor((root) =>
    Editor.make()
      .config((ctx) => {
        ctx.set(rootCtx, root);
        ctx.set(defaultValueCtx, initialContent);
        // 编辑区本身就是「纸」（agent.md §11.3）；左右留白由页面的内容列负责
        ctx.update(editorViewOptionsCtx, (prev) => ({
          ...prev,
          attributes: {
            class: cn("prose-editor max-w-none focus:outline-none py-6"),
            spellcheck: "false",
          },
        }));
        ctx.get(listenerCtx).markdownUpdated((_, markdown, prev) => {
          // Milkdown 在初始化时也会触发一次。与新值不同的才上报，
          // 否则打开页面的瞬间就变成了「有未保存的修改」。
          if (markdown !== prev) {
            changeRef.current(markdown);
          }
        });
      })
      .use(commonmark)
      .use(gfm)
      .use(listener)
      .use(history)
      .use(clipboard)
      .use(cursor),
  );

  return (
    // min-h 与块编辑器一致，两者切换时页面高度不跳
    <div className={cn("min-h-[60vh]", className)}>
      <Milkdown />
    </div>
  );
}
