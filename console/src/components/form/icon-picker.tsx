import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { SearchInput } from "@/components/ui/input";
import { EmptyState } from "@/components/ui/states";
import {
  FALLBACK_ICON,
  ICON_NAMES,
  isKnownIcon,
  resolveIcon,
} from "@/lib/icons";
import { cn } from "@/lib/utils";
import { Check } from "lucide-react";
import { useMemo, useState } from "react";

/**
 * 图标选择器。
 *
 * 做成弹窗而不是下拉：登记表里有七十多个图标，下拉里只能一格格翻，
 * 而选择图标本质上是一次「看图形挑一个」的动作——需要同时看见尽可能多的候选。
 * 全站也一直是弹窗优先。
 *
 * 检索按图标名做子串匹配。图标名是英文的（lucide 的官方名），
 * 中文使用者未必知道该搜什么，因此每一项都把名字显示出来，
 * 让人至少能靠「看」完成选择，检索只是加速手段。
 */
export function IconPicker({
  id,
  value,
  disabled,
  onChange,
}: {
  id: string;
  value: string;
  disabled?: boolean;
  onChange: (icon: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");

  const matched = useMemo(() => {
    const keyword = query.trim().toLowerCase();
    if (!keyword) {
      return ICON_NAMES;
    }
    return ICON_NAMES.filter((name) => name.includes(keyword));
  }, [query]);

  const Current = resolveIcon(value);
  const known = isKnownIcon(value);

  return (
    <>
      <Button
        id={id}
        type="button"
        variant="secondary"
        disabled={disabled}
        onClick={() => {
          setQuery("");
          setOpen(true);
        }}
        className="max-w-md justify-start gap-2"
      >
        <Current aria-hidden="true" className="size-4 shrink-0" />
        <span className="truncate">{value || "选择一个图标"}</span>
      </Button>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent size="lg">
          <DialogHeader>
            <DialogTitle>选择图标</DialogTitle>
            <DialogDescription>
              图标名会存进设置。当前有 {ICON_NAMES.length} 个可用图标。
            </DialogDescription>
          </DialogHeader>
          <DialogBody className="flex flex-col gap-3">
            <SearchInput
              value={query}
              onValueChange={setQuery}
              placeholder="按名字检索，如 book、user"
              aria-label="检索图标"
            />

            {matched.length === 0 ? (
              <EmptyState
                icon={FALLBACK_ICON}
                title="没有匹配的图标"
                description="换一个关键词试试。图标名都是英文的。"
              />
            ) : (
              <ul className="grid max-h-80 grid-cols-[repeat(auto-fill,minmax(4.5rem,1fr))] gap-1 overflow-y-auto">
                {matched.map((name) => {
                  const Icon = resolveIcon(name);
                  const active = name === value;
                  return (
                    <li key={name}>
                      <button
                        type="button"
                        onClick={() => {
                          onChange(name);
                          setOpen(false);
                        }}
                        aria-pressed={active}
                        className={cn(
                          "transition-ui flex w-full flex-col items-center gap-1 rounded-control px-1 py-2",
                          "hover:bg-surface-hover",
                          active && "bg-seal-soft text-seal",
                        )}
                      >
                        <span className="relative">
                          <Icon aria-hidden="true" className="size-5" />
                          {active ? (
                            <Check
                              aria-hidden="true"
                              className="absolute -top-1 -right-1.5 size-3 text-seal"
                              strokeWidth={3}
                            />
                          ) : null}
                        </span>
                        <span className="w-full truncate text-center text-[0.6875rem] text-ink-muted">
                          {name}
                        </span>
                      </button>
                    </li>
                  );
                })}
              </ul>
            )}

            {value && !known ? (
              <p className="text-xs text-warn">
                当前值 {value}{" "}
                不在登记表里，取不到对应图标。换一个，或把它改回已登记的名字。
              </p>
            ) : null}
          </DialogBody>
        </DialogContent>
      </Dialog>
    </>
  );
}
