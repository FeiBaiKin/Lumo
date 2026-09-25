import type { Command } from "@/components/editor/slash-menu";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import type { Editor } from "@tiptap/core";
import { DragHandle } from "@tiptap/extension-drag-handle-react";
import type { Node as PMNode } from "@tiptap/pm/model";
import {
  ArrowDown,
  ArrowUp,
  BetweenHorizontalEnd,
  BetweenHorizontalStart,
  Copy,
  GripVertical,
  Plus,
  Trash2,
} from "lucide-react";
import { useCallback, useRef, useState } from "react";

/**
 * 块手柄：按住拖动排序，单击或右键打开这一块的菜单。
 *
 * 没有它的时候，表格、图片这类整块节点插进来之后只能靠撤销拿掉——
 * 光标放不进去，退格也删不动。
 *
 * 菜单挂在一个不可见的定位点上，而不是把手柄本身当成触发器：手柄要保留原生拖动，
 * 而 Radix 的触发器在按下鼠标时就阻止了默认行为，拖动会起不来。
 *
 * 打开菜单时锁住手柄：鼠标移向菜单会离开编辑区，不锁的话手柄会跟着隐藏或跳到别的块。
 * 锁用事务元信息 `lockDragHandle` 而不是同名命令——那个命令属于 DragHandle 扩展，
 * React 版组件只装插件、不注册扩展，调它会直接抛错。关菜单时发 `hideDragHandle`：
 * 同时解锁并收起手柄，等下次鼠标移动再按新位置出现（删除、移动之后旧位置已经不对了）。
 * 操作按打开时记下的位置、在执行那一刻重新取节点。
 */
export function BlockHandle({
  editor,
  commands,
}: {
  editor: Editor;
  /** 「在下方插入」子菜单里的块类型（与斜杠菜单同一份）。 */
  commands: Command[];
}) {
  const hovered = useRef<number | null>(null);
  const [menu, setMenu] = useState<{
    pos: number;
    x: number;
    y: number;
  } | null>(null);

  // 引用必须稳定：DragHandle 在它变化时会重建插件，手柄随之闪烁
  const onNodeChange = useCallback(
    ({ node, pos }: { node: PMNode | null; pos: number }) => {
      hovered.current = node ? pos : null;
    },
    [],
  );

  function openMenu(target: HTMLElement) {
    const pos = hovered.current;
    if (pos === null) {
      return;
    }
    const rect = target.getBoundingClientRect();
    editor.commands.setMeta("lockDragHandle", true);
    setMenu({ pos, x: rect.left, y: rect.bottom });
  }

  function closeMenu() {
    setMenu(null);
    editor.commands.setMeta("hideDragHandle", true);
  }

  /** 菜单对准的那一块，按当前文档重新取。 */
  const block = (() => {
    if (!menu) {
      return null;
    }
    const { doc } = editor.state;
    const node = menu.pos <= doc.content.size ? doc.nodeAt(menu.pos) : null;
    if (!node) {
      return null;
    }
    const $pos = doc.resolve(menu.pos);
    return {
      node,
      pos: menu.pos,
      end: menu.pos + node.nodeSize,
      index: $pos.index(),
      parent: $pos.parent,
    };
  })();

  /** 在 at 处插一个空段落，并把光标放进去。 */
  function insertParagraph(at: number) {
    editor
      .chain()
      .insertContentAt(at, { type: "paragraph" })
      .setTextSelection(at + 1)
      .focus()
      .run();
  }

  const insertable = commands.filter((command) => command.id !== "paragraph");

  return (
    <>
      <DragHandle editor={editor} onNodeChange={onNodeChange}>
        <button
          type="button"
          onClick={(event) => openMenu(event.currentTarget)}
          onContextMenu={(event) => {
            event.preventDefault();
            openMenu(event.currentTarget);
          }}
          className="flex size-6 cursor-grab items-center justify-center rounded-control text-ink-subtle hover:bg-surface-active hover:text-ink active:cursor-grabbing"
          aria-label="拖动这一块；单击打开菜单"
          title="拖动排序；单击或右键打开菜单"
          tabIndex={-1}
        >
          <GripVertical aria-hidden="true" className="size-4" />
        </button>
      </DragHandle>

      <DropdownMenu
        open={menu !== null}
        onOpenChange={(open) => {
          if (!open) {
            closeMenu();
          }
        }}
      >
        <DropdownMenuTrigger asChild>
          <span
            aria-hidden="true"
            className="pointer-events-none fixed size-0"
            style={{ left: menu?.x ?? 0, top: menu?.y ?? 0 }}
          />
        </DropdownMenuTrigger>
        <DropdownMenuContent
          align="start"
          aria-label="这一块的操作"
          // 焦点交给编辑器（各操作自己 focus），不回到那个看不见的定位点
          onCloseAutoFocus={(event) => event.preventDefault()}
        >
          <DropdownMenuItem
            disabled={!block}
            onSelect={() => block && insertParagraph(block.pos)}
          >
            <BetweenHorizontalStart aria-hidden="true" />
            在上方插入段落
          </DropdownMenuItem>
          <DropdownMenuItem
            disabled={!block}
            onSelect={() => block && insertParagraph(block.end)}
          >
            <BetweenHorizontalEnd aria-hidden="true" />
            在下方插入段落
          </DropdownMenuItem>
          <DropdownMenuSub>
            <DropdownMenuSubTrigger disabled={!block}>
              <Plus aria-hidden="true" />
              在下方插入
            </DropdownMenuSubTrigger>
            <DropdownMenuSubContent>
              {insertable.map((command) => (
                <DropdownMenuItem
                  key={command.id}
                  onSelect={() => {
                    if (!block) {
                      return;
                    }
                    // 先垫一个空段落再就地转换，与斜杠菜单在空行上的行为一致
                    const at = block.end + 1;
                    insertParagraph(block.end);
                    command.run(editor, { from: at, to: at });
                  }}
                >
                  <command.icon aria-hidden="true" />
                  {command.title}
                </DropdownMenuItem>
              ))}
            </DropdownMenuSubContent>
          </DropdownMenuSub>
          <DropdownMenuItem
            disabled={!block}
            onSelect={() =>
              block &&
              editor
                .chain()
                .insertContentAt(block.end, block.node.toJSON())
                .focus()
                .run()
            }
          >
            <Copy aria-hidden="true" />
            复制这一块
          </DropdownMenuItem>

          <DropdownMenuSeparator />
          <DropdownMenuItem
            disabled={!block || block.index === 0}
            onSelect={() => {
              if (!block || block.index === 0) {
                return;
              }
              const previous = block.parent.child(block.index - 1);
              editor
                .chain()
                .command(({ tr }) => {
                  tr.delete(block.pos, block.end);
                  tr.insert(block.pos - previous.nodeSize, block.node);
                  return true;
                })
                .focus()
                .run();
            }}
          >
            <ArrowUp aria-hidden="true" />
            上移
          </DropdownMenuItem>
          <DropdownMenuItem
            disabled={!block || block.index >= block.parent.childCount - 1}
            onSelect={() => {
              if (!block || block.index >= block.parent.childCount - 1) {
                return;
              }
              const next = block.parent.child(block.index + 1);
              editor
                .chain()
                .command(({ tr }) => {
                  tr.delete(block.pos, block.end);
                  tr.insert(block.pos + next.nodeSize, block.node);
                  return true;
                })
                .focus()
                .run();
            }}
          >
            <ArrowDown aria-hidden="true" />
            下移
          </DropdownMenuItem>

          <DropdownMenuSeparator />
          <DropdownMenuItem
            danger
            disabled={!block}
            onSelect={() =>
              block &&
              editor
                .chain()
                .deleteRange({ from: block.pos, to: block.end })
                .focus()
                .run()
            }
          >
            <Trash2 aria-hidden="true" />
            删除这一块
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </>
  );
}
