import { cn } from "@/lib/utils";
import * as Popover from "@radix-ui/react-popover";
import CodeBlockLowlight from "@tiptap/extension-code-block-lowlight";
import {
  NodeViewContent,
  type NodeViewProps,
  NodeViewWrapper,
  ReactNodeViewRenderer,
} from "@tiptap/react";
import { Command } from "cmdk";
import bash from "highlight.js/lib/languages/bash";
import c from "highlight.js/lib/languages/c";
import cpp from "highlight.js/lib/languages/cpp";
import csharp from "highlight.js/lib/languages/csharp";
import css from "highlight.js/lib/languages/css";
import diff from "highlight.js/lib/languages/diff";
import dockerfile from "highlight.js/lib/languages/dockerfile";
import go from "highlight.js/lib/languages/go";
import ini from "highlight.js/lib/languages/ini";
import java from "highlight.js/lib/languages/java";
import javascript from "highlight.js/lib/languages/javascript";
import json from "highlight.js/lib/languages/json";
import kotlin from "highlight.js/lib/languages/kotlin";
import markdown from "highlight.js/lib/languages/markdown";
import nginx from "highlight.js/lib/languages/nginx";
import php from "highlight.js/lib/languages/php";
import python from "highlight.js/lib/languages/python";
import ruby from "highlight.js/lib/languages/ruby";
import rust from "highlight.js/lib/languages/rust";
import sql from "highlight.js/lib/languages/sql";
import swift from "highlight.js/lib/languages/swift";
import typescript from "highlight.js/lib/languages/typescript";
import xml from "highlight.js/lib/languages/xml";
import yaml from "highlight.js/lib/languages/yaml";
import { createLowlight } from "lowlight";
import { Check, ChevronDown } from "lucide-react";
import { useState } from "react";

/**
 * 富文本编辑器的代码块：右上角选语言，写作时按所选语言着色。
 *
 * 选中的语言写成 `<pre><code class="language-go">`，前台由服务端的 chroma 按它着色
 * （internal/content/highlight.go），所以 value 必须是 chroma 也认得的名字。
 * 编辑器这一侧用 lowlight（highlight.js 的语法），只注册下面这些常用语言，控制编辑器分包的体积。
 */
export const CODE_LANGUAGES: { value: string; label: string }[] = [
  { value: "", label: "纯文本" },
  { value: "bash", label: "Shell" },
  { value: "c", label: "C" },
  { value: "cpp", label: "C++" },
  { value: "csharp", label: "C#" },
  { value: "css", label: "CSS" },
  { value: "diff", label: "Diff" },
  { value: "dockerfile", label: "Dockerfile" },
  { value: "go", label: "Go" },
  { value: "html", label: "HTML" },
  { value: "ini", label: "INI" },
  { value: "java", label: "Java" },
  { value: "javascript", label: "JavaScript" },
  { value: "json", label: "JSON" },
  { value: "kotlin", label: "Kotlin" },
  { value: "markdown", label: "Markdown" },
  { value: "nginx", label: "Nginx" },
  { value: "php", label: "PHP" },
  { value: "python", label: "Python" },
  { value: "ruby", label: "Ruby" },
  { value: "rust", label: "Rust" },
  { value: "sql", label: "SQL" },
  { value: "swift", label: "Swift" },
  { value: "toml", label: "TOML" },
  { value: "typescript", label: "TypeScript" },
  { value: "xml", label: "XML" },
  { value: "yaml", label: "YAML" },
];

const lowlight = createLowlight({
  bash,
  c,
  cpp,
  csharp,
  css,
  diff,
  dockerfile,
  go,
  ini,
  java,
  javascript,
  json,
  kotlin,
  markdown,
  nginx,
  php,
  python,
  ruby,
  rust,
  sql,
  swift,
  typescript,
  xml,
  yaml,
});
lowlight.registerAlias({ xml: ["html"], ini: ["toml"] });

/**
 * 交给扩展的 lowlight：没选语言或语言没注册时一律按纯文本排，不自动猜。
 * 前台同样不猜（猜错了满屏乱色比不着色更糟），两边看到的必须是同一个样子。
 */
const noGuessLowlight = {
  highlight: lowlight.highlight,
  listLanguages: lowlight.listLanguages,
  registered: lowlight.registered,
  highlightAuto: (value: string) => ({
    type: "root" as const,
    children: [{ type: "text" as const, value }],
    data: { language: undefined, relevance: 0 },
  }),
};

export const CodeBlock = CodeBlockLowlight.extend({
  addNodeView() {
    return ReactNodeViewRenderer(CodeBlockView);
  },
}).configure({
  lowlight: noGuessLowlight,
  HTMLAttributes: { class: "editor-code-block" },
});

function labelOf(value: string) {
  return CODE_LANGUAGES.find((item) => item.value === value)?.label ?? value;
}

function CodeBlockView({ node, updateAttributes, editor }: NodeViewProps) {
  const language = (node.attrs.language as string | null) ?? "";
  return (
    <NodeViewWrapper className="editor-code-block-wrap">
      {/* 选择器不属于正文：contentEditable=false 让光标与复制都跳过它 */}
      <div contentEditable={false} className="editor-code-block-tools">
        <LanguagePicker
          value={language}
          disabled={!editor.isEditable}
          onChange={(value) => updateAttributes({ language: value || null })}
        />
      </div>
      <pre className="editor-code-block">
        <NodeViewContent<"code">
          as="code"
          className={language ? `language-${language}` : undefined}
        />
      </pre>
    </NodeViewWrapper>
  );
}

/** 语言选择：一个显示当前语言的小按钮，点开是可搜索的列表。 */
function LanguagePicker({
  value,
  disabled,
  onChange,
}: {
  value: string;
  disabled: boolean;
  onChange: (value: string) => void;
}) {
  const [open, setOpen] = useState(false);
  return (
    <Popover.Root open={open} onOpenChange={setOpen}>
      <Popover.Trigger
        type="button"
        disabled={disabled}
        aria-label={`代码语言：${labelOf(value)}，点击更换`}
        className={cn(
          "transition-ui flex h-6 items-center gap-1 rounded-control px-1.5 text-xs text-ink-muted",
          "hover:bg-surface-active hover:text-ink disabled:pointer-events-none",
          "data-[state=open]:bg-surface-active data-[state=open]:text-ink",
        )}
      >
        {labelOf(value)}
        <ChevronDown aria-hidden="true" className="size-3.5" />
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Content
          align="end"
          sideOffset={4}
          className="popover-in z-popover w-52 overflow-hidden rounded-overlay border border-line bg-surface shadow-popover"
          // 关闭后把焦点还给正文，而不是停在已经看不见的按钮上
          onCloseAutoFocus={(event) => event.preventDefault()}
        >
          <Command label="选择代码语言" loop>
            <Command.Input
              placeholder="搜索语言"
              className="h-9 w-full border-line border-b bg-transparent px-3 text-base text-ink placeholder:text-ink-subtle"
            />
            <Command.List className="max-h-64 overflow-y-auto p-1">
              <Command.Empty className="px-2 py-6 text-center text-sm text-ink-muted">
                没有这种语言，可以先选纯文本
              </Command.Empty>
              {CODE_LANGUAGES.map((item) => (
                <Command.Item
                  key={item.value || "plain"}
                  value={`${item.label} ${item.value}`}
                  onSelect={() => {
                    onChange(item.value);
                    setOpen(false);
                  }}
                  className={cn(
                    "relative flex cursor-pointer items-center rounded-control py-1.5 pr-8 pl-2 text-base text-ink select-none",
                    "data-[selected=true]:bg-surface-active",
                  )}
                >
                  {item.label}
                  {item.value === value ? (
                    <Check
                      aria-hidden="true"
                      className="absolute right-2 size-4 text-seal"
                    />
                  ) : null}
                </Command.Item>
              ))}
            </Command.List>
          </Command>
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
}
