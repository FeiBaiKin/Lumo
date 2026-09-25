import { BlockHandle } from "@/components/editor/block-menu";
import { BubbleToolbar } from "@/components/editor/bubble-toolbar";
import { CodeBlock } from "@/components/editor/code-block";
import { LinkDialog, type LinkValue } from "@/components/editor/link-dialog";
import {
  SlashMenu,
  buildCommands,
  createSlashCommand,
  useSlashMenuState,
} from "@/components/editor/slash-menu";
import {
  type MediaPick,
  MediaPickerDialog,
} from "@/components/media/media-picker";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import type { Range } from "@tiptap/core";
import Image from "@tiptap/extension-image";
import Link from "@tiptap/extension-link";
import Placeholder from "@tiptap/extension-placeholder";
import { TableKit } from "@tiptap/extension-table";
import { NodeSelection } from "@tiptap/pm/state";
import { EditorContent, useEditor } from "@tiptap/react";
import StarterKit from "@tiptap/starter-kit";
import {
  Bold,
  Code,
  Code2,
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
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";

/**
 * 块编辑器（TipTap v3 开源扩展集，不碰 Pro 付费项）。
 *
 * 内容格式策略：`raw` 存**规范 HTML**，不存 ProseMirror JSON。
 * 这一条决定了本文件的两件事：
 *   - 初始内容用 `editor.getHTML()` 取，而不是 JSON.stringify
 *   - 自定义块若将来需要携带结构，用 `data-*` 属性而不是私有 JSON 节点
 * 内容不该被编辑器绑架 —— 换编辑器（本页右上角就能换成 Markdown）
 * 或换工具时，存下来的东西任何工具都读得懂。
 *
 * 服务端按权限决定是否净化：持有 content:unsafe_html 的作者保留 iframe 与自定义 HTML，
 * 其余作者的正文在保存时按允许列表净化（internal/content/sanitize.go）。
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
  const [pickerOpen, setPickerOpen] = useState(false);
  const [linkTarget, setLinkTarget] = useState<LinkTarget | null>(null);

  /**
   * 斜杠菜单留下的 `/图片` 那段范围。
   *
   * 放 ref 而不是 state：选择器确认时会**接着**触发关闭回调，
   * 两者读到的必须是同一份「还没处理过」的标记。放 state 的话
   * 关闭回调读到的是上一次渲染的值，于是会把已经删过的范围再删一次 ——
   * 那时位置已经失效，删掉的是正文里的别的东西。
   */
  const pendingRange = useRef<Range | null>(null);

  /** 取走待处理的范围，取一次就清掉：先到的一方负责删它。 */
  const takeRange = useCallback(() => {
    const range = pendingRange.current;
    pendingRange.current = null;
    return range;
  }, []);

  const requestImage = useCallback((range: Range) => {
    pendingRange.current = range;
    setPickerOpen(true);
  }, []);

  /*
   * 斜杠菜单是一个 TipTap 扩展，而扩展只能在创建编辑器时一次性传入 ——
   * v3 没有「事后注册插件」的 API。故这里用 useMemo 把扩展实例稳定下来，
   * 并把它的开关状态接到上方的 React state 上（面板本身由 SlashMenu 渲染）。
   */
  const slashExtension = useMemo(
    () =>
      createSlashCommand({
        onOpenChange: setSlashOpen,
        onRequestImage: requestImage,
      }),
    [setSlashOpen, requestImage],
  );

  // 手柄菜单「在下方插入」用的块类型，与斜杠菜单同一份
  const blockCommands = useMemo(
    () => buildCommands({ requestImage }),
    [requestImage],
  );

  const editor = useEditor({
    editable,
    // 工具条的激活态（加粗、标题）要随光标位置变化，故每次事务都重渲染。
    // v3 默认关闭这一项以省渲染，但这里的工具条正是靠它才跟得上光标。
    shouldRerenderOnTransaction: true,
    extensions: [
      // StarterKit 已含 bold / italic / strike / code / heading / list /
      // blockquote / hr / history 等常用节点
      StarterKit.configure({
        heading: { levels: [1, 2, 3] },
        link: false, // 用下面的 Link 扩展，它带默认 rel 与校验
        codeBlock: false, // 换成带语言选择与着色的 CodeBlock
      }),
      CodeBlock,
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
        // 编辑区本身就是「纸」：左右留白由页面的内容列负责，
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

  /**
   * 打开链接弹窗，并把「当前光标处是什么情况」一并量出来。
   *
   * 三种情况在弹窗里长得不一样：光标落在已有链接上（可编辑、可移除）、
   * 选中了一段文字（只填地址）、什么都没选（连显示文本一起填）。
   */
  const openLink = useCallback(() => {
    if (!editor) {
      return;
    }
    /*
     * 选中的是图片这类整块节点时，链接**加不上去**——链接是行内标记，
     * 而节点上挂不住标记。过去（prompt 时代）这种情况下点了确定什么也不会发生，
     * 看起来像是坏了。这里把光标先移到节点之后，于是它变成「没有选区」，
     * 走下面连显示文本一起填的那条路：链接落在图片后面，是能预期的结果。
     */
    const { selection } = editor.state;
    if (selection instanceof NodeSelection) {
      editor.commands.setTextSelection(selection.to);
    }
    const attrs = editor.getAttributes("link");
    const onLink = editor.isActive("link");
    setLinkTarget({
      href: typeof attrs.href === "string" ? attrs.href : "",
      newWindow: attrs.target === "_blank",
      needsText: editor.state.selection.empty && !onLink,
      canRemove: onLink,
    });
  }, [editor]);

  const applyLink = useCallback(
    (value: LinkValue) => {
      if (!editor) {
        return;
      }
      // target 显式写 null 而不是省略：省略时 setLink 会保留上一次的值，
      // 于是「取消勾选新窗口」看起来没生效。
      const attrs = {
        href: value.href,
        target: value.newWindow ? "_blank" : null,
      };
      if (value.text) {
        editor
          .chain()
          .focus()
          .insertContent({
            type: "text",
            text: value.text,
            marks: [{ type: "link", attrs }],
          })
          .run();
      } else {
        editor.chain().focus().extendMarkRange("link").setLink(attrs).run();
      }
      setLinkTarget(null);
    },
    [editor],
  );

  const removeLink = useCallback(() => {
    editor?.chain().focus().extendMarkRange("link").unsetLink().run();
    setLinkTarget(null);
  }, [editor]);

  /** 插入选中的图片。多选时按选中顺序逐张插入。 */
  const insertImages = useCallback(
    (picks: MediaPick[]) => {
      const range = takeRange();
      if (!editor) {
        return;
      }
      if (range) {
        // 斜杠菜单唤出的，先把 `/图片` 那段删掉
        editor.chain().focus().deleteRange(range).run();
      }
      for (const pick of picks) {
        editor
          .chain()
          .focus()
          .setImage({
            src: pick.url,
            alt: pick.alt,
            ...(pick.title ? { title: pick.title } : {}),
          })
          .run();
      }
      setPickerOpen(false);
    },
    [editor, takeRange],
  );

  /** 关掉选择器。范围还没被取走说明是取消，此时要把 `/图片` 清掉。 */
  const closePicker = useCallback(
    (open: boolean) => {
      if (!open) {
        const range = takeRange();
        if (range && editor) {
          editor.chain().focus().deleteRange(range).run();
        }
      }
      setPickerOpen(open);
    },
    [editor, takeRange],
  );

  if (!editor) {
    return (
      <div className="flex min-h-[60vh] items-center justify-center text-sm text-ink-muted">
        正在准备编辑器
      </div>
    );
  }

  const toolbar = (
    <Toolbar
      editor={editor}
      onOpenLink={openLink}
      onInsertImage={() => {
        // 工具条唤出的没有 `/图片` 要清理
        pendingRange.current = null;
        setPickerOpen(true);
      }}
    />
  );

  return (
    <div className="flex flex-col">
      {toolbarContainer === undefined ? (
        <div className="border-line border-b bg-surface">{toolbar}</div>
      ) : toolbarContainer ? (
        createPortal(toolbar, toolbarContainer)
      ) : null}

      <div className="relative">
        <EditorContent editor={editor} />

        {/* 块手柄：拖动排序，单击或右键打开这一块的菜单 */}
        <BlockHandle editor={editor} commands={blockCommands} />
      </div>

      <BubbleToolbar editor={editor} onOpenLink={openLink} />

      {/* 斜杠菜单：输入 / 唤出，键盘可导航 */}
      <SlashMenu open={slashOpen} onClose={() => setSlashOpen(null)} />

      <LinkDialog
        open={linkTarget !== null}
        onOpenChange={(open) => {
          if (!open) {
            setLinkTarget(null);
          }
        }}
        initial={{
          href: linkTarget?.href ?? "",
          newWindow: linkTarget?.newWindow ?? false,
        }}
        needsText={linkTarget?.needsText ?? false}
        onSubmit={applyLink}
        onRemove={linkTarget?.canRemove ? removeLink : undefined}
      />

      <MediaPickerDialog
        open={pickerOpen}
        onOpenChange={closePicker}
        onSelect={insertImages}
        kind="image"
        multiple
        title="插入图片"
        confirmLabel="插入"
      />
    </div>
  );
}

/** 打开链接弹窗时光标处的情况。 */
type LinkTarget = {
  href: string;
  newWindow: boolean;
  needsText: boolean;
  canRemove: boolean;
};

/** 工具条：48px 高、居中，按用途分段，段间用竖线分隔（Halo 的 editor-header 同形）。 */
function Toolbar({
  editor,
  onOpenLink,
  onInsertImage,
}: {
  editor: NonNullable<ReturnType<typeof useEditor>>;
  onOpenLink: () => void;
  onInsertImage: () => void;
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
      run: onInsertImage,
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
        onClick={onOpenLink}
        aria-pressed={editor.isActive("link")}
        aria-label={editor.isActive("link") ? "编辑链接" : "插入链接"}
        title={editor.isActive("link") ? "编辑链接" : "插入链接"}
        className={cn(editor.isActive("link") && "bg-seal-soft text-seal")}
      >
        <LinkIcon aria-hidden="true" />
      </Button>
    </div>
  );
}
