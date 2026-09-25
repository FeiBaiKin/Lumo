import { api, problemMessage } from "@/api/client";
import type { components } from "@/api/schema";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/states";
import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router";

type WidgetInfo = components["schemas"]["WidgetInfo"];

/**
 * 插件小组件的选择器（x-widget: plugin-widget）。
 *
 * 可选项来自启用中、且被授予了前台能力的插件，按插件分组；存下来的是「插件/小组件」。
 * 插件停用后值原样保留（前台那一条暂不显示），这里标出「插件未启用」，删不删由站长定。
 */
export function PluginWidgetPicker({
  id,
  value,
  invalid,
  describedBy,
  disabled,
  onChange,
}: {
  id: string;
  value: string;
  invalid?: boolean | undefined;
  describedBy?: string | undefined;
  disabled?: boolean | undefined;
  onChange: (next: string) => void;
}) {
  const widgets = useQuery({
    queryKey: ["plugin-widgets"],
    queryFn: async (): Promise<WidgetInfo[]> => {
      const { data, error, response } = await api.GET(
        "/api/v1/console/plugin-widgets",
      );
      if (!response.ok) {
        throw new Error(problemMessage(error));
      }
      return data?.items ?? [];
    },
  });

  if (widgets.isLoading) {
    return <Skeleton className="h-9 w-full" />;
  }
  if (widgets.error) {
    return (
      <p className="text-sm text-danger">
        读不到插件提供的小组件：{widgets.error.message}
      </p>
    );
  }

  const items = widgets.data ?? [];
  const selected = items.find((item) => item.id === value);
  const stale = value !== "" && !selected;
  const empty = items.length === 0 && !stale;

  const groups: { plugin: string; label: string; items: WidgetInfo[] }[] = [];
  for (const item of items) {
    let group = groups.find((g) => g.plugin === item.plugin);
    if (!group) {
      group = { plugin: item.plugin, label: item.pluginLabel, items: [] };
      groups.push(group);
    }
    group.items.push(item);
  }

  return (
    <div className="flex flex-col gap-1.5">
      <Select
        value={value}
        disabled={disabled || empty}
        onValueChange={onChange}
      >
        <SelectTrigger
          id={id}
          aria-invalid={invalid ? true : undefined}
          aria-describedby={describedBy}
        >
          <SelectValue
            placeholder={empty ? "没有可选的小组件" : "选一个小组件"}
          />
        </SelectTrigger>
        <SelectContent>
          {stale ? (
            <SelectItem value={value}>{value}（插件未启用）</SelectItem>
          ) : null}
          {groups.map((group) => (
            <SelectGroup key={group.plugin}>
              <SelectLabel>{group.label}</SelectLabel>
              {group.items.map((item) => (
                <SelectItem key={item.id} value={item.id}>
                  {item.label}
                </SelectItem>
              ))}
            </SelectGroup>
          ))}
        </SelectContent>
      </Select>
      {empty ? (
        <p className="text-xs text-ink-muted">
          启用中的插件都没有提供小组件。到
          <Link to="/plugins" className="mx-0.5 text-seal hover:underline">
            插件
          </Link>
          页启用一个带小组件的插件，这里就能选了。
        </p>
      ) : null}
      {stale ? (
        <p className="text-xs text-warn">
          提供它的插件没有启用，前台暂时不显示这一条；重新启用插件就会恢复。
        </p>
      ) : null}
      {selected?.description ? (
        <p className="text-xs text-ink-muted">{selected.description}</p>
      ) : null}
    </div>
  );
}
