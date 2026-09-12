import {
  SlashMenu,
  createSlashCommand,
  useSlashMenuState,
} from "@/components/editor/slash-menu";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { DragHandle } from "@tiptap/extension-drag-handle-react";
import Image from "@tiptap/extension-image";
import Link from "@tiptap/extension-link";
import Placeholder from "@tiptap/extension-placeholder";
import { TableKit } from "@tiptap/extension-table";
import { EditorContent, useEditor } from "@tiptap/react";
import StarterKit from "@tiptap/starter-kit";
import {
  Bold,
  Code,
  Code2,
  GripVertical,
  Heading1,
  Heading2,
  Heading3,
  Image as ImageIcon,
  Italic,
  Link as LinkIcon,
  List,
  ListOrdered,
  Minus,
  Quote,
  Redo2,
  Strikethrough,
  Table as TableIcon,
  Undo2,
} from "lucide-react";
import { useCallback, useEffect, useMemo } from "react";
import { createPortal } from "react-dom";

/**
 * 块编辑器（TipTap v3 开源扩展集，不碰 Pro 付费项）。
 *
 * 内容格式策略见 agent.md §3.4：`raw` 存**规范 HTML**，不存 ProseMirror JSON。
 * 这一条决定了本文件的两件事：
 *   - 初始内容用 `editor.getHTML()` 取，而不是 JSON.stringify
 *   - 自定义块若将来需要携带结构，用 `data-*` 属性而不是私有 JSON 节点
 * 内容不该被编辑器绑架 —— 换编辑器（本页右上角就能换成 Markdown）
 * 或换工具时，存下来的东西任何工具都读得懂。
 *
 * 服务端**不做 HTML 净化**（§3.4，与 Halo / Ghost 同策），信任已认证用户的输入。
 * 这也是保留 iframe 嵌入与自定义 HTML 块能力的前提。
 *
 * 工具条可以经 `toolbarContainer` 传送到页面的任意位置（编辑页把它放在
 * 页头之下、正文之上的那条全宽白带里，Halo 的 editor-header 同位）；
 * 不传时工具条就地渲染在正文上方。
 */

export function HtmlEditor({
  initialContent,
  onChange,
  editable = true,
  toolbarContainer,
}: {
  initialContent: string;
  onChange: (html: string) => void;
  editable?: boolean;
  /**
   * 工具条的挂载点。undefined 表示就地渲染；null 表示挂载点尚未就绪（先不画）；
   * 元素表示经 portal 渲染到该处。
   */
  toolbarContainer?: HTMLElement | null;
}) {
  const [slashOpen, setSlashOpen] = useSlashMenuState();

  /*
   * 斜杠菜单是一个 TipTap 扩展，而扩展只能在创建编辑器时一次性传入 ——
   * v3 没有「事后注册插件」的 API。故这里用 useMemo 把扩展实例稳定下来，
   * 并把它的开关状态接到上方的 React state 上（面板本身由 SlashMenu 渲染）。
   */
  const slashExtension = useMemo(
    () => createSlashCommand({ onOpenChange: setSlashOpen }),
    [setSlashOpen],
  );

  const editor = useEditor({
    editable,
    // 工具条的激活态（加粗、标题）要随光标位置变化，故每次事务都重渲染。
    // v3 默认关闭这一项以省渲染，但这里的工具条正是靠它才跟得上光标。
    shouldRerenderOnTransaction: true,
    extensions: [
      // StarterKit 已含 bold / italic / strike / code / heading / list /
      // blockquote / codeBlock / hr / history 等常用节点
      StarterKit.configure({
        heading: { levels: [1, 2, 3] },
        link: false, // 用下面的 Link 扩展，它带默认 rel 与校验
        codeBlock: { HTMLAttributes: { class: "editor-code-block" } },
      }),
      Link.configure({
        openOnClick: false,
        autolink: true,
        // 前台渲染会再补一次，但编辑器里就要带对 —— 作者直接复制 HTML 时不至于漏
        HTMLAttributes: { rel: "noopener noreferrer nofollow" },
      }),
      Image.configure({ inline: false, allowBase64: false }),
      Placeholder.configure({
        placeholder: "输入正文，或按 / 插入内容块",
      }),
      // TableKit 一次带上 table / row / header / cell
      TableKit.configure({ table: { resizable: true } }),
      slashExtension,
    ],
    content: initialContent,
    onUpdate: ({ editor: instance }) => {
      onChange(instance.getHTML());
    },
    editorProps: {
      attributes: {
        // 编辑区本身就是「纸」（agent.md §11.3）：左右留白由页面的内容列负责，
        // 这里只管上下呼吸与最小高度
        class: cn(
          "prose-editor max-w-none focus:outline-none",
          "min-h-[60vh] py-6",
        ),
        // 拼写检查对中文没有意义，反而在每个词下画红线
        spellcheck: "false",
      },
    },
  });

  // 外部把内容整体换掉时（如切换格式后回填）同步进编辑器。
  // 正常的载入走 key 重建（见 ContentEditor），这条只为「同一实例内换内容」兜底。
  useEffect(() => {
    // StrictMode 会把首次挂载的实例销毁再重建：销毁后的实例 schema 为空，
    // 对它调 getHTML 会在 fromSchema 里读 null.cached 而崩掉整页。
    if (!editor || editor.isDestroyed) {
      return;
    }
    if (editor.getHTML() !== initialContent) {
      editor.commands.setContent(initialContent);
    }
  }, [editor, initialContent]);

  const setLink = useCallback(() => {
    if (!editor) {
      return;
    }
    const previous = editor.getAttributes("link").href as string | undefined;
    // 用 prompt 而不是自建浮层：链接输入是一次性的单字段输入，
    // 为它写一个弹窗组件不值得（接入附件库时再考虑替换）。
    const url = window.prompt(
      "链接地址（留空则移除链接）",
      previous ?? "https://",
    );
    if (url === null) {
      return;
    }
    if (url === "") {
      editor.chain().focus().extendMarkRange("link").unsetLink().run();
      return;
    }
    // 只有 http(s) 与站内相对地址成为链接，与评论模块的白名单同策
    if (!/^(https?:\/\/|\/)/i.test(url)) {
      window.alert("只支持 http(s) 链接或站内相对地址（以 / 开头）");
      return;
    }
    editor.chain().focus().extendMarkRange("link").setLink({ href: url }).run();
  }, [editor]);

  if (!editor) {
    return (
      <div className="flex min-h-[60vh] items-center justify-center text-sm text-ink-muted">
        正在准备编辑器
      </div>
    );
  }

  const toolbar = <Toolbar editor={editor} onSetLink={setLink} />;

  return (
    <div className="flex flex-col">
      {toolbarContainer === undefined ? (
        <div className="border-line border-b bg-surface">{toolbar}</div>
      ) : toolbarContainer ? (
        createPortal(toolbar, toolbarContainer)
      ) : null}

      <div className="relative">
        <EditorContent editor={editor} />

        {/*
          块拖拽手柄（开源扩展）。
          DragHandle 会渲染一个浮动的把手，用它把整块上下移动。
        */}
        <DragHandle editor={editor}>
          <button
            type="button"
            className="flex size-6 cursor-grab items-center justify-center rounded-control text-ink-subtle hover:bg-surface-active hover:text-ink active:cursor-grabbing"
            aria-label="拖动这一块"
            tabIndex={-1}
          >
            <GripVertical aria-hidden="true" className="size-4" />
          </button>
        </DragHandle>
      </div>

      {/* 斜杠菜单：输入 / 唤出，键盘可导航 */}
      <SlashMenu open={slashOpen} onClose={() => setSlashOpen(null)} />
    </div>
  );
}

/** 工具条：48px 高、居中，按用途分段，段间用竖线分隔（Halo 的 editor-header 同形）。 */
function Toolbar({
  editor,
  onSetLink,
}: {
  editor: NonNullable<ReturnType<typeof useEditor>>;
  onSetLink: () => void;
}) {
  const actions = [
    {
      group: 0,
      label: "撤销",
      icon: Undo2,
      run: () => editor.chain().focus().undo().run(),
      active: false,
      disabled: !editor.can().undo(),
    },
    {
      group: 0,
      label: "重做",
      icon: Redo2,
      run: () => editor.chain().focus().redo().run(),
      active: false,
      disabled: !editor.can().redo(),
    },
    {
      group: 1,
      label: "一级标题",
      icon: Heading1,
      run: () => editor.chain().focus().toggleHeading({ level: 1 }).run(),
      active: editor.isActive("heading", { level: 1 }),
    },
    {
      group: 1,
      label: "二级标题",
      icon: Heading2,
      run: () => editor.chain().focus().toggleHeading({ level: 2 }).run(),
      active: editor.isActive("heading", { level: 2 }),
    },
    {
      group: 1,
      label: "三级标题",
      icon: Heading3,
      run: () => editor.chain().focus().toggleHeading({ level: 3 }).run(),
      active: editor.isActive("heading", { level: 3 }),
    },
    {
      group: 2,
      label: "加粗",
      icon: Bold,
      run: () => editor.chain().focus().toggleBold().run(),
      active: editor.isActive("bold"),
    },
    {
      group: 2,
      label: "斜体",
      icon: Italic,
      run: () => editor.chain().focus().toggleItalic().run(),
      active: editor.isActive("italic"),
    },
    {
      group: 2,
      label: "删除线",
      icon: Strikethrough,
      run: () => editor.chain().focus().toggleStrike().run(),
      active: editor.isActive("strike"),
    },
    {
      group: 2,
      label: "行内代码",
      icon: Code,
      run: () => editor.chain().focus().toggleCode().run(),
      active: editor.isActive("code"),
    },
    {
      group: 3,
      label: "无序列表",
      icon: List,
      run: () => editor.chain().focus().toggleBulletList().run(),
      active: editor.isActive("bulletList"),
    },
    {
      group: 3,
      label: "有序列表",
      icon: ListOrdered,
      run: () => editor.chain().focus().toggleOrderedList().run(),
      active: editor.isActive("orderedList"),
    },
    {
      group: 3,
      label: "引用",
      icon: Quote,
      run: () => editor.chain().focus().toggleBlockquote().run(),
      active: editor.isActive("blockquote"),
    },
    {
      group: 3,
      label: "代码块",
      icon: Code2,
      run: () => editor.chain().focus().toggleCodeBlock().run(),
      active: editor.isActive("codeBlock"),
    },
    {
      group: 4,
      label: "插入表格",
      icon: TableIcon,
      run: () =>
        editor
          .chain()
          .focus()
          .insertTable({ rows: 3, cols: 3, withHeaderRow: true })
          .run(),
      active: editor.isActive("table"),
    },
    {
      group: 4,
      label: "插入图片",
      icon: ImageIcon,
      run: () => {
        const url = window.prompt("图片地址", "https://");
        if (url && /^(https?:\/\/|\/)/i.test(url)) {
          editor.chain().focus().setImage({ src: url }).run();
        }
      },
      active: false,
    },
    {
      group: 4,
      label: "分隔线",
      icon: Minus,
      run: () => editor.chain().focus().setHorizontalRule().run(),
      active: false,
    },
  ];

  let lastGroup = -1;

  return (
    // 工具条是界面的一部分（器），与编辑区的纸形成对照
    <div
      role="toolbar"
      aria-label="格式工具"
      className="mx-auto flex min-h-12 max-w-measure flex-wrap items-center justify-center gap-0.5 px-2 py-1"
    >
      {actions.map((action) => {
        const divider = action.group !== lastGroup && lastGroup !== -1;
        lastGroup = action.group;
        return (
          <span key={action.label} className="flex items-center">
            {divider ? (
              <span aria-hidden="true" className="mx-1 h-5 w-px bg-line" />
            ) : null}
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={action.run}
              disabled={action.disabled ?? false}
              aria-pressed={action.active}
              aria-label={action.label}
              title={action.label}
              className={cn(action.active && "bg-seal-soft text-seal")}
            >
              <action.icon aria-hidden="true" />
            </Button>
          </span>
        );
      })}

      <span aria-hidden="true" className="mx-1 h-5 w-px bg-line" />

      <Button
        variant="ghost"
        size="icon-sm"
        onClick={onSetLink}
        aria-pressed={editor.isActive("link")}
        aria-label="插入链接"
        title="插入链接"
        className={cn(editor.isActive("link") && "bg-seal-soft text-seal")}
      >
        <LinkIcon aria-hidden="true" />
      </Button>
    </div>
  );
}
