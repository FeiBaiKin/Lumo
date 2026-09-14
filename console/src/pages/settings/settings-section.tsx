import { Card } from "@/components/ui/card";
import { Switch } from "@/components/ui/toggle";
import { resolveIcon } from "@/lib/icons";
import { cn } from "@/lib/utils";
import { ChevronDown } from "lucide-react";
import type { ReactNode } from "react";

/**
 * 设置页里的一个分组区块：标题栏常驻，详细字段点开才展开。
 *
 * 六个分组铺在一页上、全部摊开的话，是一条四十多个字段的长坡——
 * 站长要改的那一项永远在屏幕外。折起来之后，整页第一屏就能看全「有哪些东西可配」，
 * 这正是把五个菜单项合成一页之后要还给用户的东西：**先看见全貌，再进入细节。**
 *
 * 标题栏上有三样不用展开就能读到的信息，缺一样这一页就不能折：
 *
 *   1. **主开关**（分组声明 Toggle 时）。「开放评论」「启用邮件发送」「开放注册」
 *      是站长最常动的东西，让它藏在展开之后，等于把最常用的动作放到最深的地方。
 *      开关在标题按钮**之外**：按钮里套按钮既不合法，键盘上也走不通。
 *   2. **有未保存改动**。收起的区块照样会被整页的保存按钮提交，
 *      不说出来就成了「我不知道自己改了什么」。
 *   3. **有错误**。保存失败时由页面把出错的区块展开，标题上的记号让人知道是哪一块。
 *
 * 展开不做高度动画：分组之间内容高度差着几百像素，一段从 0 撑到 800px 的过渡
 * 只会让页面抖一下。开合由 chevron 的旋转交代，那是「答复用户动作」的动效。
 */

export type SettingsSectionProps = {
  /** 分组名，用于拼 DOM id 与滚动定位。 */
  name: string;
  label: string;
  description?: string | undefined;
  /** 图标名，取自分组声明。 */
  icon?: string | undefined;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** 主开关；分组没有声明 Toggle 时不传。 */
  toggle?:
    | {
        label: string;
        checked: boolean;
        onChange: (checked: boolean) => void;
      }
    | undefined;
  /** 这一组有未保存的改动。 */
  dirty?: boolean;
  /** 这一组有需要修改的字段。 */
  invalid?: boolean;
  disabled?: boolean;
  children: ReactNode;
};

export function SettingsSection({
  name,
  label,
  description,
  icon,
  open,
  onOpenChange,
  toggle,
  dirty = false,
  invalid = false,
  disabled = false,
  children,
}: SettingsSectionProps) {
  const Icon = resolveIcon(icon);
  const bodyId = `settings-body-${name}`;
  const headingId = `settings-heading-${name}`;

  return (
    <Card id={sectionAnchor(name)} className="scroll-mt-20 overflow-hidden">
      <div
        className={cn(
          "flex items-stretch",
          open && "border-line border-b",
          invalid && "border-danger/60 border-l-2",
        )}
      >
        <button
          type="button"
          aria-expanded={open}
          aria-controls={bodyId}
          onClick={() => onOpenChange(!open)}
          className={cn(
            "transition-ui flex min-w-0 flex-1 items-center gap-3 px-4 py-3 text-left",
            "hover:bg-surface-hover",
          )}
        >
          <Icon aria-hidden="true" className="size-4 shrink-0 text-ink-muted" />
          <span className="flex min-w-0 flex-col">
            <span id={headingId} className="flex items-center gap-2">
              <span className="truncate font-medium text-ink text-sm">
                {label}
              </span>
              {/*
                记号只在有话说时出现：常驻一个灰点等于常驻一句「一切正常」，
                而那句话没有人需要读。
              */}
              {invalid ? (
                <span className="rounded-full bg-danger-soft px-1.5 py-0.5 text-[11px] text-danger">
                  需要修改
                </span>
              ) : dirty ? (
                <span className="rounded-full bg-seal-soft px-1.5 py-0.5 text-[11px] text-seal">
                  已修改
                </span>
              ) : null}
            </span>
            {description ? (
              <span className="truncate text-ink-muted text-xs">
                {description}
              </span>
            ) : null}
          </span>
          <ChevronDown
            aria-hidden="true"
            className={cn(
              "transition-ui ml-auto size-4 shrink-0 text-ink-muted",
              open && "rotate-180",
            )}
          />
        </button>

        {toggle ? (
          /*
            开关自己就把状态说清楚了（色与滑块位置），旁边再写一句「已开启」是同一句话
            说两遍。也不画竖线分隔：六行里只有三行有开关，那条线会让这三行的右端
            看起来像外挂了一块。「这里不是展开按钮」由悬停时的底色划界——
            变色的只有左边那个按钮，开关这一块不亮。
          */
          <div className="flex items-center py-3 pr-4 pl-5">
            <Switch
              aria-label={toggle.label}
              aria-describedby={headingId}
              checked={toggle.checked}
              onCheckedChange={toggle.onChange}
              disabled={disabled}
            />
          </div>
        ) : null}
      </div>

      {/*
        用 hidden 而不是条件渲染：收起的区块仍然持有自己的表单状态，
        条件渲染会让「展开 → 改几个字段 → 收起 → 再展开」把刚填的东西丢掉。
        值在页面这一层，但输入框的光标、滚动位置这些仍然属于 DOM。
      */}
      <div id={bodyId} hidden={!open} className="p-4">
        {children}
      </div>
    </Card>
  );
}

/** 区块的锚点 id，页面据此滚动到某一组。 */
export function sectionAnchor(name: string): string {
  return `settings-section-${name}`;
}
