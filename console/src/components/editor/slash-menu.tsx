import { cn } from "@/lib/utils";
import { type Editor, Extension, type Range } from "@tiptap/core";
import Suggestion, { type SuggestionOptions } from "@tiptap/suggestion";
import {
  Code2,
  Heading1,
  Heading2,
  Heading3,
  Image as ImageIcon,
  List,
  ListOrdered,
  type LucideIcon,
  Minus,
  Quote,
  Table as TableIcon,
  Type,
} from "lucide-react";
import { useCallback, useEffect, useState } from "react";

/**
 * 斜杠菜单：在正文里输入 `/` 唤出内容块菜单。
 *
 * 自建而不引现成的命令列表组件 —— 需要的只是一个「出候选、方向键选、回车插入」
 * 的小面板，而它必须与编辑器的 Suggestion 插件精确配合。
 *
 * 两个实现上的关键点：
 *
 *   1. **扩展只能在创建编辑器时传入**。TipTap v3 没有「事后注册插件」的 API，
 *      故菜单的开关状态经一个回调冒泡到 React，面板由 React 渲染。
 *      这比把整个面板塞进 ProseMirror 的 decoration 里简单得多 ——
 *      后者要手写 DOM 与事件，而我们已经有一个组件库。
 *   2. **键盘事件由菜单消费**，且必须在 Suggestion 的 `onKeyDown` 里返回 true。
 *      返回 false 时编辑器会把方向键当成光标移动、回车当成新段落，
 *      结果是「菜单还在，但光标已经跑到别处了」。
 */

export type Command = {
  title: string;
  hint: string;
  icon: LucideIcon;
  /** 过滤用的关键词，含拼音，便于用中文输入法习惯查找。 */
  keywords: string;
  run: (editor: Editor, range: Range) => void;
};

const COMMANDS: Command[] = [
  {
    title: "正文",
    hint: "普通段落",
    icon: Type,
    keywords: "zhengwen text paragraph duanluo p",
    run: (editor, range) =>
      editor.chain().focus().deleteRange(range).setParagraph().run(),
  },
  {
    title: "一级标题",
    hint: "文章内的大标题",
    icon: Heading1,
    keywords: "h1 biaoti heading yiji",
    run: (editor, range) =>
      editor.chain().focus().deleteRange(range).setHeading({ level: 1 }).run(),
  },
  {
    title: "二级标题",
    hint: "小节标题",
    icon: Heading2,
    keywords: "h2 biaoti heading erji",
    run: (editor, range) =>
      editor.chain().focus().deleteRange(range).setHeading({ level: 2 }).run(),
  },
  {
    title: "三级标题",
    hint: "更细的分节",
    icon: Heading3,
    keywords: "h3 biaoti heading sanji",
    run: (editor, range) =>
      editor.chain().focus().deleteRange(range).setHeading({ level: 3 }).run(),
  },
  {
    title: "无序列表",
    hint: "圆点列表",
    icon: List,
    keywords: "ul liebiao list wuxu",
    run: (editor, range) =>
      editor.chain().focus().deleteRange(range).toggleBulletList().run(),
  },
  {
    title: "有序列表",
    hint: "带序号",
    icon: ListOrdered,
    keywords: "ol liebiao list youxu shuzi",
    run: (editor, range) =>
      editor.chain().focus().deleteRange(range).toggleOrderedList().run(),
  },
  {
    title: "引用",
    hint: "引述他人的话",
    icon: Quote,
    keywords: "quote yinyong yinwen",
    run: (editor, range) =>
      editor.chain().focus().deleteRange(range).toggleBlockquote().run(),
  },
  {
    title: "代码块",
    hint: "等宽字体、保留缩进",
    icon: Code2,
    keywords: "code daima codeblock",
    run: (editor, range) =>
      editor.chain().focus().deleteRange(range).toggleCodeBlock().run(),
  },
  {
    title: "表格",
    hint: "三行三列，带表头",
    icon: TableIcon,
    keywords: "table biaoge",
    run: (editor, range) =>
      editor
        .chain()
        .focus()
        .deleteRange(range)
        .insertTable({ rows: 3, cols: 3, withHeaderRow: true })
        .run(),
  },
  {
    title: "图片",
    hint: "按地址插入",
    icon: ImageIcon,
    keywords: "image tupian picture img",
    run: (editor, range) => {
      const url = window.prompt("图片地址", "https://");
      if (url && /^(https?:\/\/|\/)/i.test(url)) {
        editor.chain().focus().deleteRange(range).setImage({ src: url }).run();
        return;
      }
      // 取消时只把 `/关键字` 删掉，不留一段垃圾文本
      editor.chain().focus().deleteRange(range).run();
    },
  },
  {
    title: "分隔线",
    hint: "一条水平线",
    icon: Minus,
    keywords: "hr fengexian line",
    run: (editor, range) =>
      editor.chain().focus().deleteRange(range).setHorizontalRule().run(),
  },
];

function filterCommands(query: string): Command[] {
  const q = query.trim().toLowerCase();
  if (!q) {
    // 空输入时列出全部 —— 打开菜单就看到有什么可用
    return COMMANDS;
  }
  return COMMANDS.filter(
    (command) =>
      command.title.toLowerCase().includes(q) || command.keywords.includes(q),
  );
}

/** 菜单打开时的状态。 */
export type SlashState = {
  items: Command[];
  query: string;
  range: Range;
  /** 光标的视口坐标，用于定位面板。 */
  clientRect: (() => DOMRect | null) | null;
  /** 执行某一项。 */
  command: (item: Command) => void;
};

type Options = {
  onOpenChange: (state: SlashState | null) => void;
};

/**
 * 创建斜杠菜单扩展。
 *
 * 用工厂函数而不是 `Extension.create` 的静态 options：
 * 回调需要闭包捕获 React 的 setState，而 defaultOptions 是模块级的。
 */
export function createSlashCommand({ onOpenChange }: Options) {
  return Extension.create({
    name: "slashCommand",

    addProseMirrorPlugins() {
      return [
        Suggestion<Command>({
          editor: this.editor,
          char: "/",
          // 允许在行中出现，但只认「前面是空白或行首」的那种 ——
          // 否则写下 https://… 里的斜杠也会弹出菜单
          allowSpaces: false,
          startOfLine: false,
          items: ({ query }) => filterCommands(query),
          command: ({ editor, range, props }) => {
            props.run(editor, range);
          },
          render: () => ({
            onStart: (props) => {
              onOpenChange({
                items: props.items,
                query: props.query,
                range: props.range,
                clientRect: props.clientRect ?? null,
                command: props.command,
              });
            },
            onUpdate: (props) => {
              onOpenChange({
                items: props.items,
                query: props.query,
                range: props.range,
                clientRect: props.clientRect ?? null,
                command: props.command,
              });
            },
            onKeyDown: (props) => keyboardHandler?.(props.event) ?? false,
            onExit: () => onOpenChange(null),
          }),
        } as SuggestionOptions<Command>),
      ];
    },
  });
}

/**
 * 当前的键盘处理器。
 *
 * Suggestion 的 onKeyDown 拿不到 React 组件的状态（选中项），
 * 而选中项必须由组件持有（鼠标悬停也要改它）。用一个模块级变量把两者接起来：
 * 编辑器同一时刻只有一个斜杠菜单，不存在并发问题。
 */
let keyboardHandler: ((event: KeyboardEvent) => boolean) | null = null;

/** 把菜单开关状态提到编辑器组件里的小 hook。 */
export function useSlashMenuState() {
  return useState<SlashState | null>(null);
}

/** 菜单面板。 */
export function SlashMenu({
  open,
  onClose,
}: {
  open: SlashState | null;
  onClose: () => void;
}) {
  const [selected, setSelected] = useState(0);

  const total = open?.items.length ?? 0;
  /*
   * 选中项在渲染时钳制，而不是用一个 effect 在项数变化时重置。
   * 两者效果相同，但钳制没有「effect 里 setState」那一拍延迟 ——
   * 项数骤减时不会出现一帧「没有任何一项高亮」。
   */
  const active = total === 0 ? 0 : Math.min(selected, total - 1);

  const commit = useCallback(
    (item: Command) => {
      open?.command(item);
      onClose();
    },
    [open, onClose],
  );

  useEffect(() => {
    if (!open) {
      keyboardHandler = null;
      return;
    }
    keyboardHandler = (event: KeyboardEvent) => {
      if (total === 0) {
        return false;
      }
      switch (event.key) {
        case "ArrowDown":
          setSelected((prev) => (prev + 1) % total);
          return true;
        case "ArrowUp":
          setSelected((prev) => (prev - 1 + total) % total);
          return true;
        case "Enter":
        case "Tab": {
          const item = open.items[active];
          if (item) {
            commit(item);
          }
          return true;
        }
        case "Escape":
          onClose();
          return true;
        default:
          return false;
      }
    };
    return () => {
      keyboardHandler = null;
    };
  }, [open, total, active, commit, onClose]);

  if (!open) {
    return null;
  }

  // 用 fixed + 视口坐标：编辑器内部滚动时面板不会跟着错位
  const rect = open.clientRect?.();

  return (
    /*
     * 这里刻意**不用** role="listbox" / role="option"。
     *
     * 那是一套「焦点（或 aria-activedescendant）落在列表项上」的语义，
     * 而本菜单的设计恰恰相反：焦点始终留在编辑器里（否则作者打一半的字就断了），
     * 方向键与回车由编辑器的 Suggestion 插件交给上面的 keyboardHandler 处理。
     * 套上 listbox 只会让读屏用户以为可以进出这个列表，实际进去什么也操作不了。
     *
     * 诚实的做法：它就是一个带标签的按钮组，主路径的键盘交互由编辑器承担；
     * 每个按钮自身也可聚焦、可点击，作为备选路径。
     */
    <fieldset
      style={{
        position: "fixed",
        top: rect?.bottom ?? 0,
        left: rect?.left ?? 0,
      }}
      className="panel-in z-popover m-0 max-h-72 w-64 overflow-y-auto rounded-overlay border border-line bg-surface p-1 shadow-overlay"
    >
      <legend className="sr-only">插入内容块</legend>
      {open.items.length === 0 ? (
        <p className="px-2 py-3 text-xs text-ink-muted">
          没有匹配「{open.query}」的内容块
        </p>
      ) : (
        open.items.map((item, index) => (
          <button
            key={item.title}
            type="button"
            // 悬停与键盘共用同一个 active ——
            // 两套状态会让「当前选的是哪项」出现两个答案
            onMouseEnter={() => setSelected(index)}
            // 用 onMouseDown 而不是 onClick：click 之前输入框会先失焦，
            // 那会让浏览器丢掉选区，插入位置就错了。
            onMouseDown={(event) => {
              event.preventDefault();
              commit(item);
            }}
            className={cn(
              "flex w-full items-center gap-2.5 rounded-control px-2 py-1.5 text-left",
              index === active
                ? "bg-seal-soft text-seal"
                : "text-ink hover:bg-surface-hover",
            )}
          >
            <item.icon aria-hidden="true" className="size-4 shrink-0" />
            <span className="flex min-w-0 flex-col">
              <span className="text-sm font-medium">{item.title}</span>
              <span
                className={cn(
                  "text-xs",
                  index === active ? "text-seal/80" : "text-ink-muted",
                )}
              >
                {item.hint}
              </span>
            </span>
          </button>
        ))
      )}

      <p className="border-line border-t px-2 py-1.5 text-xs text-ink-subtle">
        ↑↓ 选择 · 回车插入 · Esc 关闭
      </p>
    </fieldset>
  );
}
