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
 * 结构固定为「标题栏（标题 + 关闭）/ 主体（可滚动）/ 底栏（动作靠右）」。
 * 卡片不用阴影表达高度，浮层用 —— 这里阴影真的在表达「浮在内容之上」。
 */

export const Dialog = DialogPrimitive.Root;
export const DialogTrigger = DialogPrimitive.Trigger;
export const DialogClose = DialogPrimitive.Close;

const SIZE_CLASS = {
  sm: "max-w-sm",
  md: "max-w-lg",
  lg: "max-w-2xl",
  xl: "max-w-4xl",
} as const;

export type DialogSize = keyof typeof SIZE_CLASS;

/** 遮罩的样式。两个原语各要一份，但外观必须一致，故共用这一串。 */
const overlayClass = "overlay-in fixed inset-0 z-overlay bg-scrim";

function Overlay({
  className,
  ...props
}: ComponentProps<typeof DialogPrimitive.Overlay>) {
  return (
    <DialogPrimitive.Overlay
      className={cn(overlayClass, className)}
      {...props}
    />
  );
}

/**
 * 确认框的遮罩。
 *
 * 必须用 AlertDialog 的原语，不能复用上面那个：Radix 的 DialogOverlay
 * 要求自己处在 Dialog 上下文里，而 AlertDialog 提供的是另一套上下文。
 * 混用的后果不是「遮罩样式不对」，而是**打开确认框的那一刻抛异常、整页白屏**——
 * 错误信息是 `DialogOverlay must be used within Dialog`。
 */
function AlertOverlay({
  className,
  ...props
}: ComponentProps<typeof AlertDialogPrimitive.Overlay>) {
  return (
    <AlertDialogPrimitive.Overlay
      className={cn(overlayClass, className)}
      {...props}
    />
  );
}

const contentClass = cn(
  "panel-in fixed top-1/2 left-1/2 z-overlay w-[calc(100vw-2rem)]",
  "-translate-x-1/2 -translate-y-1/2",
  "rounded-overlay border border-line bg-surface shadow-overlay",
  "flex max-h-[calc(100dvh-3rem)] flex-col outline-none",
  // 页面习惯把标题栏 / 主体 / 底栏包在一个 <form> 里提交（角色、用户、分类…）。
  // 那个 form 就成了面板唯一的 flex 子项：它自己的高度不设限，于是面板被 max-h 卡住、
  // 内容溢出到面板之外——底部的「保存」按钮落在视口外，而且页面滚动被浮层锁着，滚不到。
  // 让 form 顶替面板当 flex 容器，主体的 overflow-y-auto 才真正生效。
  "[&>form]:flex [&>form]:min-h-0 [&>form]:flex-1 [&>form]:flex-col",
);

export function DialogContent({
  className,
  children,
  size = "md",
  showClose = true,
  ...props
}: ComponentProps<typeof DialogPrimitive.Content> & {
  size?: DialogSize;
  showClose?: boolean;
}) {
  return (
    <DialogPrimitive.Portal>
      <Overlay />
      <DialogPrimitive.Content
        className={cn(contentClass, SIZE_CLASS[size], className)}
        {...props}
      >
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
      </DialogPrimitive.Content>
    </DialogPrimitive.Portal>
  );
}

export function DialogHeader({
  className,
  ...props
}: ComponentProps<"header">) {
  return (
    <header
      className={cn(
        "flex flex-col gap-1 border-line border-b px-5 py-4 pr-14",
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
 * 做成组件而不是让各页面自己拼 AlertDialog：文案结构（做什么、影响什么、不可撤销）
 * 一旦各处自拟，很快就会有的写清楚、有的只写「确定吗」。
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
        <AlertOverlay />
        <AlertDialogPrimitive.Content className={cn(contentClass, "max-w-md")}>
          <div className="flex flex-col gap-1.5 px-5 py-4">
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
            {/* 破坏性操作的按钮不抢焦点：默认焦点落在「取消」上，
                否则连按两次回车就把东西删了 */}
            <AlertDialogPrimitive.Action asChild>
              <Button
                variant={destructive ? "danger" : "primary"}
                loading={pending}
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
