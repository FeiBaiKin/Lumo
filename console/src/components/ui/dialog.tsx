import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import * as AlertDialogPrimitive from "@radix-ui/react-alert-dialog";
import * as DialogPrimitive from "@radix-ui/react-dialog";
import { X } from "lucide-react";
import type { ComponentProps, ReactNode } from "react";

/**
 * 浮层。
 *
 * 只此一套（对话框 + 确认框），全站不再各写各的 overlay。
 * 面板**不用阴影**，浮层用 —— 阴影在这里真的在表达「浮在内容之上」，
 * 而面板用 1px 描边（agent.md §11.3）。
 */

export const Dialog = DialogPrimitive.Root;
export const DialogTrigger = DialogPrimitive.Trigger;
export const DialogClose = DialogPrimitive.Close;

function Overlay({
  className,
  ...props
}: ComponentProps<typeof DialogPrimitive.Overlay>) {
  return (
    <DialogPrimitive.Overlay
      className={cn(
        "overlay-in fixed inset-0 z-overlay bg-black/45",
        className,
      )}
      {...props}
    />
  );
}

function Panel({
  className,
  children,
  ...props
}: ComponentProps<typeof DialogPrimitive.Content>) {
  return (
    <DialogPrimitive.Portal>
      <Overlay />
      <DialogPrimitive.Content
        className={cn(
          "panel-in fixed top-1/2 left-1/2 z-overlay w-[calc(100vw-2rem)] max-w-lg",
          "-translate-x-1/2 -translate-y-1/2",
          "shadow-overlay rounded-overlay border border-line bg-surface",
          "flex max-h-[calc(100dvh-4rem)] flex-col",
          className,
        )}
        {...props}
      >
        {children}
      </DialogPrimitive.Content>
    </DialogPrimitive.Portal>
  );
}

export function DialogContent({
  className,
  children,
  showClose = true,
  ...props
}: ComponentProps<typeof DialogPrimitive.Content> & { showClose?: boolean }) {
  return (
    <Panel className={className} {...props}>
      {children}
      {showClose ? (
        <DialogPrimitive.Close asChild>
          <Button
            variant="ghost"
            size="icon-sm"
            className="absolute top-3 right-3"
            aria-label="关闭"
          >
            <X aria-hidden="true" />
          </Button>
        </DialogPrimitive.Close>
      ) : null}
    </Panel>
  );
}

export function DialogHeader({
  className,
  ...props
}: ComponentProps<"header">) {
  return (
    <header
      className={cn(
        "flex flex-col gap-1 border-line border-b px-5 py-4 pr-12",
        className,
      )}
      {...props}
    />
  );
}

export function DialogTitle({
  className,
  ...props
}: ComponentProps<typeof DialogPrimitive.Title>) {
  return (
    <DialogPrimitive.Title
      className={cn("text-lg font-semibold text-ink", className)}
      {...props}
    />
  );
}

export function DialogDescription({
  className,
  ...props
}: ComponentProps<typeof DialogPrimitive.Description>) {
  return (
    <DialogPrimitive.Description
      className={cn("text-sm text-ink-muted", className)}
      {...props}
    />
  );
}

export function DialogBody({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      className={cn("min-h-0 flex-1 overflow-y-auto px-5 py-4", className)}
      {...props}
    />
  );
}

export function DialogFooter({
  className,
  ...props
}: ComponentProps<"footer">) {
  return (
    <footer
      className={cn(
        "flex flex-wrap items-center justify-end gap-2 border-line border-t px-5 py-3",
        className,
      )}
      {...props}
    />
  );
}

/**
 * 破坏性操作确认。
 *
 * 检索把「删除前必须确认」列为 High 级条目。这里把它做成一个组件而不是
 * 让各页面自己拼 AlertDialog：文案结构（做什么、影响什么、不可撤销）
 * 一旦各处自拟，很快就会有的写清楚、有的只写「确定吗」。
 *
 * `consequence` 是必填的：一个不说明后果的确认框等于没有确认。
 */
export function ConfirmDialog({
  open,
  onOpenChange,
  title,
  consequence,
  confirmLabel = "确认",
  cancelLabel = "取消",
  destructive = true,
  pending = false,
  onConfirm,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  /** 说清这么做的后果。是必填项 —— 见上方说明。 */
  consequence: ReactNode;
  confirmLabel?: string;
  cancelLabel?: string;
  destructive?: boolean;
  pending?: boolean;
  onConfirm: () => void;
}) {
  return (
    <AlertDialogPrimitive.Root open={open} onOpenChange={onOpenChange}>
      <AlertDialogPrimitive.Portal>
        <Overlay />
        <AlertDialogPrimitive.Content
          className={cn(
            "panel-in fixed top-1/2 left-1/2 z-overlay w-[calc(100vw-2rem)] max-w-md",
            "-translate-x-1/2 -translate-y-1/2",
            "shadow-overlay rounded-overlay border border-line bg-surface",
          )}
        >
          <div className="flex flex-col gap-1 px-5 py-4">
            <AlertDialogPrimitive.Title className="text-lg font-semibold text-ink">
              {title}
            </AlertDialogPrimitive.Title>
            <AlertDialogPrimitive.Description asChild>
              <div className="text-sm text-ink-muted">{consequence}</div>
            </AlertDialogPrimitive.Description>
          </div>
          <div className="flex flex-wrap items-center justify-end gap-2 border-line border-t px-5 py-3">
            <AlertDialogPrimitive.Cancel asChild>
              <Button variant="secondary" disabled={pending}>
                {cancelLabel}
              </Button>
            </AlertDialogPrimitive.Cancel>
            {/* 破坏性操作的按钮不用 autoFocus 抢焦点：默认焦点应落在「取消」上，
                否则连按两次回车就把东西删了 */}
            <AlertDialogPrimitive.Action asChild>
              <Button
                variant={destructive ? "danger" : "primary"}
                disabled={pending}
                onClick={(event) => {
                  // 交给调用方控制关闭时机：异步请求期间要保持对话框开着
                  event.preventDefault();
                  onConfirm();
                }}
              >
                {confirmLabel}
              </Button>
            </AlertDialogPrimitive.Action>
          </div>
        </AlertDialogPrimitive.Content>
      </AlertDialogPrimitive.Portal>
    </AlertDialogPrimitive.Root>
  );
}
