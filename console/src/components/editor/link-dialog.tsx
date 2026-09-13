import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
} from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { CheckboxRow } from "@/components/ui/toggle";
import { Link2Off } from "lucide-react";
import { useState } from "react";

/**
 * 插入链接的弹窗。
 *
 * 原来这里是 `window.prompt` + `window.alert`：一行输入、校验失败再弹一次，
 * 样式由浏览器决定，装不下「在新窗口打开」这类选项，校验出错还得重来一遍。
 * 自建之后三件事才做得到：地址错误内联在字段旁（不清空已填的内容）、
 * 光标不在文字上时可以连显示文本一起填、移除链接与确定在同一排。
 */

export type LinkValue = {
  href: string;
  /** 选区为空时要插入的文字；有选区时为空串。 */
  text: string;
  newWindow: boolean;
};

export type NormalizedLink =
  | { ok: true; href: string }
  | { ok: false; error: string };

/** 形如 example.com 或 example.com/a?b 的裸域名，补协议后才是地址。 */
const BARE_DOMAIN = /^[\w-]+(\.[\w-]+)+([/?#]|$)/;

/**
 * 规范化用户填的地址。
 *
 * 放行的范围与服务端的正文净化（internal/content/sanitize.go）对齐：
 * http、https、mailto 加站内相对地址。其余协议一律拒绝 ——
 * javascript: 与 data: 能在访客（可能是管理员）的浏览器里执行脚本，
 * 在编辑器里就拦住，比等服务端净化时默默吞掉要诚实。
 *
 * 裸域名补上 https://：这是最常见的一种输入，让它报错只是在为难人。
 */
export function normalizeLinkHref(raw: string): NormalizedLink {
  const value = raw.trim();
  if (!value) {
    return { ok: false, error: "请填写链接地址" };
  }
  if (/\s/.test(value)) {
    return { ok: false, error: "地址里不能有空格" };
  }
  if (/^https?:\/\//i.test(value)) {
    if (/^https?:\/\/$/i.test(value)) {
      return { ok: false, error: "只有协议没有域名，请把地址补全" };
    }
    return { ok: true, href: value };
  }
  if (/^mailto:/i.test(value)) {
    return value.length > "mailto:".length
      ? { ok: true, href: value }
      : { ok: false, error: "请补上邮箱地址" };
  }
  // 站内绝对路径、锚点、查询串：都以当前站点为基准，不需要协议
  if (value.startsWith("/") || value.startsWith("#") || value.startsWith("?")) {
    return { ok: true, href: value };
  }
  if (/^[a-z][a-z0-9+.-]*:/i.test(value)) {
    return {
      ok: false,
      error: "只支持 http(s)、mailto 与站内地址（以 / 开头）",
    };
  }
  if (BARE_DOMAIN.test(value)) {
    return { ok: true, href: `https://${value}` };
  }
  return {
    ok: false,
    error: "看不出这是一个地址；站内链接请以 / 开头",
  };
}

export function LinkDialog({
  open,
  onOpenChange,
  initial,
  needsText,
  onSubmit,
  onRemove,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** 打开时已有的值；新建链接时 href 为空串。 */
  initial: { href: string; newWindow: boolean };
  /** 选区为空且不在已有链接上 —— 此时要连显示文本一起填。 */
  needsText: boolean;
  onSubmit: (value: LinkValue) => void;
  /** 仅在光标落在已有链接上时提供。 */
  onRemove?: (() => void) | undefined;
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      {/* 只在打开时挂载表单体：初值在挂载时生效，不必再写一套「open 变了就重填」 */}
      {open ? (
        <DialogContent size="md">
          <LinkForm
            initial={initial}
            needsText={needsText}
            onSubmit={onSubmit}
            onCancel={() => onOpenChange(false)}
            onRemove={onRemove}
          />
        </DialogContent>
      ) : null}
    </Dialog>
  );
}

function LinkForm({
  initial,
  needsText,
  onSubmit,
  onCancel,
  onRemove,
}: {
  initial: { href: string; newWindow: boolean };
  needsText: boolean;
  onSubmit: (value: LinkValue) => void;
  onCancel: () => void;
  onRemove?: (() => void) | undefined;
}) {
  const [href, setHref] = useState(initial.href);
  const [text, setText] = useState("");
  const [newWindow, setNewWindow] = useState(initial.newWindow);
  const [error, setError] = useState("");

  function submit() {
    const result = normalizeLinkHref(href);
    if (!result.ok) {
      setError(result.error);
      return;
    }
    onSubmit({
      href: result.href,
      // 不填显示文本就用地址本身，与粘贴链接得到的结果一致
      text: needsText ? text.trim() || result.href : "",
      newWindow,
    });
  }

  return (
    <>
      <DialogHeader>
        <DialogTitle>{initial.href ? "编辑链接" : "插入链接"}</DialogTitle>
      </DialogHeader>

      <DialogBody className="flex flex-col gap-4">
        <Field>
          <FieldLabel htmlFor="link-href">链接地址</FieldLabel>
          <Input
            id="link-href"
            value={href}
            // 弹窗里唯一的必填项，打开即可输入
            autoFocus
            onChange={(event) => {
              setHref(event.target.value);
              setError("");
            }}
            onKeyDown={(event) => {
              if (event.key === "Enter") {
                event.preventDefault();
                submit();
              }
            }}
            placeholder="https://example.com 或 /about"
            aria-invalid={error ? true : undefined}
            aria-describedby={error ? "link-href-error" : "link-href-hint"}
          />
          <FieldError id="link-href-error">{error}</FieldError>
          <FieldDescription id="link-href-hint">
            支持 http(s)、站内路径（/about）、页内锚点（#section）与
            mailto:。只写域名会自动补上 https://。
          </FieldDescription>
        </Field>

        {needsText ? (
          <Field>
            <FieldLabel htmlFor="link-text">显示文本</FieldLabel>
            <Input
              id="link-text"
              value={text}
              onChange={(event) => setText(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter") {
                  event.preventDefault();
                  submit();
                }
              }}
              placeholder="留空则显示地址本身"
            />
            <FieldDescription>
              光标处没有选中文字，这段文字会作为链接插入正文。
            </FieldDescription>
          </Field>
        ) : null}

        <CheckboxRow
          id="link-new-window"
          checked={newWindow}
          onCheckedChange={setNewWindow}
          label="在新窗口打开"
          description="外部链接常用；站内跳转一般不需要。"
          className="-mx-2"
        />
      </DialogBody>

      <DialogFooter>
        {onRemove ? (
          <Button variant="ghost" onClick={onRemove} className="mr-auto">
            <Link2Off aria-hidden="true" />
            移除链接
          </Button>
        ) : null}
        <Button variant="secondary" onClick={onCancel}>
          取消
        </Button>
        <Button variant="primary" onClick={submit}>
          确定
        </Button>
      </DialogFooter>
    </>
  );
}
