import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import * as DropdownMenuPrimitive from "@radix-ui/react-dropdown-menu";
import type { Editor } from "@tiptap/core";
import { NodeSelection } from "@tiptap/pm/state";
import { BubbleMenu } from "@tiptap/react/menus";
import {
  Bold,
  Check,
  ChevronDown,
  Code,
  Italic,
  Link as LinkIcon,
  type LucideIcon,
  Strikethrough,
} from "lucide-react";
import { useCallback, useRef, useState } from "react";

/*
 * 传给 BubbleMenu 的 options 与 shouldShow 必须引用稳定：它每见到一次新值就派发一个事务，
 * 而编辑器开着「每个事务都重渲染」，新值 → 事务 → 重渲染 → 新值，会一路循环到 React 报错。
 */
const MENU_OPTIONS = { placement: "top", offset: 8 } as const;

/**
 * 选中文字时浮在选区上方的工具栏：只放行内格式与「这一段是什么」。
 *
 * 选中图片（整块节点）或光标在代码块里时不出现：前者挂不上行内格式，
 * 后者有自己的语言选择，两套浮层叠在一起只会互相挡。
 */
export function BubbleToolbar({
  editor,
  onOpenLink,
}: {
  editor: Editor;
  onOpenLink: () => void;
}) {
  const [menuHost, setMenuHost] = useState<HTMLDivElement | null>(null);
  const hostRef = useRef<HTMLDivElement | null>(null);
  hostRef.current = menuHost;

  const shouldShow = useCallback(
    ({
      editor: instance,
      view,
      state,
      from,
      to,
    }: {
      editor: Editor;
      view: Editor["view"];
      state: Editor["state"];
      from: number;
      to: number;
    }) => {
      const { selection } = state;
      if (selection.empty || selection instanceof NodeSelection) {
        return false;
      }
      if (!state.doc.textBetween(from, to).length) {
        return false;
      }
      if (!instance.isEditable || instance.isActive("codeBlock")) {
        return false;
      }
      // 焦点在工具栏自己的下拉里时也算「还在编辑」
      return (
        view.hasFocus() ||
        Boolean(hostRef.current?.contains(document.activeElement))
      );
    },
    [],
  );

  const marks: {
    label: string;
    icon: LucideIcon;
    active: boolean;
    run: () => void;
  }[] = [
    {
      label: "加粗",
      icon: Bold,
      active: editor.isActive("bold"),
      run: () => editor.chain().focus().toggleBold().run(),
    },
    {
      label: "斜体",
      icon: Italic,
      active: editor.isActive("italic"),
      run: () => editor.chain().focus().toggleItalic().run(),
    },
    {
      label: "删除线",
      icon: Strikethrough,
      active: editor.isActive("strike"),
      run: () => editor.chain().focus().toggleStrike().run(),
    },
    {
      label: "行内代码",
      icon: Code,
      active: editor.isActive("code"),
      run: () => editor.chain().focus().toggleCode().run(),
    },
  ];
  const onLink = editor.isActive("link");

  return (
    <BubbleMenu
      editor={editor}
      options={MENU_OPTIONS}
      shouldShow={shouldShow}
      className="z-popover"
    >
      <div
        ref={setMenuHost}
        role="toolbar"
        aria-label="选中文字的格式"
        className="flex items-center gap-0.5 rounded-overlay border border-line bg-surface p-1 shadow-popover"
      >
        <BlockTypeMenu editor={editor} container={menuHost} />
        <span aria-hidden="true" className="mx-0.5 h-5 w-px bg-line" />
        {marks.map((mark) => (
          <Button
            key={mark.label}
            variant="ghost"
            size="icon-sm"
            onClick={mark.run}
            aria-pressed={mark.active}
            aria-label={mark.label}
            title={mark.label}
            className={cn(mark.active && "bg-seal-soft text-seal")}
          >
            <mark.icon aria-hidden="true" />
          </Button>
        ))}
        <span aria-hidden="true" className="mx-0.5 h-5 w-px bg-line" />
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={onOpenLink}
          aria-pressed={onLink}
          aria-label={onLink ? "编辑链接" : "插入链接"}
          title={onLink ? "编辑链接" : "插入链接"}
          className={cn(onLink && "bg-seal-soft text-seal")}
        >
          <LinkIcon aria-hidden="true" />
        </Button>
      </div>
    </BubbleMenu>
  );
}

/** 这一段是什么：正文、三级标题、引用。触发按钮上写着当前是哪种。 */
function BlockTypeMenu({
  editor,
  container,
}: {
  editor: Editor;
  container: HTMLElement | null;
}) {
  const types = [
    {
      label: "正文",
      active: editor.isActive("paragraph") && !editor.isActive("blockquote"),
      run: () => editor.chain().focus().setParagraph().run(),
    },
    ...([1, 2, 3] as const).map((level) => ({
      label: `标题 ${level}`,
      active: editor.isActive("heading", { level }),
      run: () => editor.chain().focus().setHeading({ level }).run(),
    })),
    {
      label: "引用",
      active: editor.isActive("blockquote"),
      run: () => {
        if (!editor.isActive("blockquote")) {
          editor.chain().focus().setBlockquote().run();
        }
      },
    },
  ];
  // 引用里的段落同时是 paragraph 与 blockquote，按从具体到笼统取第一个命中的
  const current =
    types.find((item) => item.label === "引用" && item.active) ??
    types.find((item) => item.active);

  return (
    <DropdownMenuPrimitive.Root modal={false}>
      <DropdownMenuPrimitive.Trigger asChild>
        <Button variant="ghost" size="sm" className="gap-1 px-2">
          {current?.label ?? "段落"}
          <ChevronDown aria-hidden="true" className="size-3.5" />
        </Button>
      </DropdownMenuPrimitive.Trigger>
      {/* 挂进工具栏自己的 DOM：焦点移进下拉时，编辑器才不会把它当成「离开」而收起工具栏 */}
      <DropdownMenuPrimitive.Portal container={container}>
        <DropdownMenuPrimitive.Content
          align="start"
          sideOffset={6}
          className="popover-in z-popover min-w-[8rem] overflow-hidden rounded-overlay border border-line bg-surface p-1 shadow-popover"
        >
          {types.map((item) => (
            <DropdownMenuPrimitive.Item
              key={item.label}
              onSelect={item.run}
              className={cn(
                "transition-ui relative flex cursor-pointer items-center rounded-control py-1.5 pr-8 pl-2 text-base text-ink outline-none select-none",
                "data-[highlighted]:bg-surface-active",
              )}
            >
              {item.label}
              {item === current ? (
                <Check
                  aria-hidden="true"
                  className="absolute right-2 size-4 text-seal"
                />
              ) : null}
            </DropdownMenuPrimitive.Item>
          ))}
        </DropdownMenuPrimitive.Content>
      </DropdownMenuPrimitive.Portal>
    </DropdownMenuPrimitive.Root>
  );
}
