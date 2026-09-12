import { api } from "@/api/client";
import { useAuth } from "@/components/auth/auth-provider";
import type { NavItem } from "@/components/layout/nav";
import { useNavigation } from "@/components/layout/use-nav";
import { type ThemeChoice, useTheme } from "@/components/theme/theme-provider";
import { Kbd } from "@/components/ui/kbd";
import { cn } from "@/lib/utils";
import * as DialogPrimitive from "@radix-ui/react-dialog";
import { useQuery } from "@tanstack/react-query";
import { Command } from "cmdk";
import {
  BookOpen,
  ExternalLink,
  FileText,
  Image,
  Monitor,
  Moon,
  PenLine,
  Search,
  Sun,
} from "lucide-react";
import {
  type ReactNode,
  createContext,
  use,
  useCallback,
  useEffect,
  useMemo,
  useState,
} from "react";
import { useNavigate } from "react-router";

/**
 * 命令面板（Halo 的全局搜索，Ctrl/⌘ + K）。
 *
 * 三类结果：跳转到某一页、执行一个动作（写文章、切换主题）、按标题找文章或页面。
 * 前两类是静态的，由 cmdk 就地过滤；第三类经接口检索，输入停顿后再发请求。
 *
 * 用 Radix Dialog + cmdk 的 Command 自行组合，而不用 cmdk 自带的 Command.Dialog：
 * 后者没有 DialogTitle，Radix 会在开发期告警，且读屏用户听不到这是什么面板。
 */

type PaletteContextValue = {
  open: boolean;
  setOpen: (open: boolean) => void;
};

const PaletteContext = createContext<PaletteContextValue | null>(null);

export function CommandPaletteProvider({ children }: { children: ReactNode }) {
  const [open, setOpen] = useState(false);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        setOpen((value) => !value);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const value = useMemo(() => ({ open, setOpen }), [open]);
  return <PaletteContext value={value}>{children}</PaletteContext>;
}

export function useCommandPalette(): PaletteContextValue {
  const value = use(PaletteContext);
  if (!value) {
    throw new Error("useCommandPalette 必须在 CommandPaletteProvider 内使用");
  }
  return value;
}

const THEME_OPTIONS: { value: ThemeChoice; label: string; icon: typeof Sun }[] =
  [
    { value: "light", label: "浅色", icon: Sun },
    { value: "dark", label: "深色", icon: Moon },
    { value: "system", label: "跟随系统", icon: Monitor },
  ];

export function CommandPalette() {
  const { open, setOpen } = useCommandPalette();
  const { can } = useAuth();
  const { items } = useNavigation();
  const { setChoice } = useTheme();
  const navigate = useNavigate();
  const [query, setQuery] = useState("");
  const [debounced, setDebounced] = useState("");

  // 关闭时清空输入，下次打开是一张白纸
  useEffect(() => {
    if (!open) {
      setQuery("");
      setDebounced("");
    }
  }, [open]);

  useEffect(() => {
    const timer = setTimeout(() => setDebounced(query.trim()), 250);
    return () => clearTimeout(timer);
  }, [query]);

  const posts = useQuery({
    queryKey: ["palette", "posts", debounced],
    enabled: open && debounced.length > 0,
    staleTime: 30_000,
    queryFn: async () => {
      const [postsResult, pagesResult] = await Promise.all([
        api.GET("/api/v1/console/posts", {
          params: { query: { q: debounced, size: 6 } },
        }),
        api.GET("/api/v1/console/pages", {
          params: { query: { q: debounced, size: 4 } },
        }),
      ]);
      return {
        posts: postsResult.data?.items ?? [],
        pages: pagesResult.data?.items ?? [],
      };
    },
  });

  const go = useCallback(
    (to: string) => {
      setOpen(false);
      navigate(to);
    },
    [navigate, setOpen],
  );

  const pages: NavItem[] = useMemo(
    () => items.filter((item) => !item.permission || can(item.permission)),
    [items, can],
  );

  const actions = useMemo(() => {
    const out: {
      label: string;
      icon: typeof PenLine;
      keywords: string[];
      run: () => void;
    }[] = [];
    if (can("posts:write")) {
      out.push({
        label: "写文章",
        icon: PenLine,
        keywords: ["new post", "xinjian wenzhang", "xie"],
        run: () => go("/posts/new"),
      });
    }
    if (can("pages:write")) {
      out.push({
        label: "新建页面",
        icon: FileText,
        keywords: ["new page", "xinjian yemian"],
        run: () => go("/pages/new"),
      });
    }
    if (can("media:write")) {
      out.push({
        label: "上传附件",
        icon: Image,
        keywords: ["upload", "shangchuan fujian"],
        run: () => go("/media?upload=1"),
      });
    }
    out.push({
      label: "访问站点",
      icon: ExternalLink,
      keywords: ["site", "home", "fangwen zhandian shouye"],
      run: () => {
        setOpen(false);
        window.open("/", "_blank", "noopener");
      },
    });
    return out;
  }, [can, go, setOpen]);

  return (
    <DialogPrimitive.Root open={open} onOpenChange={setOpen}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay className="overlay-in fixed inset-0 z-command bg-scrim" />
        <DialogPrimitive.Content
          className={cn(
            "panel-in fixed top-[12vh] left-1/2 z-command w-[calc(100vw-2rem)] max-w-xl -translate-x-1/2",
            "overflow-hidden rounded-overlay border border-line bg-surface shadow-overlay outline-none",
          )}
        >
          <DialogPrimitive.Title className="sr-only">
            命令面板
          </DialogPrimitive.Title>
          <DialogPrimitive.Description className="sr-only">
            搜索页面与内容，或执行常用动作
          </DialogPrimitive.Description>

          <Command label="命令面板" loop className="flex flex-col">
            <div className="flex items-center gap-2.5 border-line border-b px-4">
              <Search
                aria-hidden="true"
                className="size-4 shrink-0 text-ink-subtle"
              />
              <Command.Input
                value={query}
                onValueChange={setQuery}
                placeholder="搜索页面、文章，或输入命令"
                className="h-12 w-full bg-transparent text-md text-ink outline-none placeholder:text-ink-subtle"
              />
              <Kbd>Esc</Kbd>
            </div>

            <Command.List className="max-h-[60vh] overflow-y-auto p-2 [&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:text-xs [&_[cmdk-group-heading]]:text-ink-muted">
              <Command.Empty className="px-2 py-8 text-center text-sm text-ink-muted">
                {posts.isFetching ? "正在搜索" : "没有匹配的结果"}
              </Command.Empty>

              {posts.data && posts.data.posts.length > 0 ? (
                <Command.Group heading="文章">
                  {posts.data.posts.map((post) => (
                    <PaletteItem
                      key={`post-${post.id}`}
                      value={`文章 ${post.title} #${post.id}`}
                      keywords={[debounced]}
                      icon={BookOpen}
                      hint={post.status === "published" ? "已发布" : "草稿"}
                      onSelect={() => go(`/posts/${post.id}`)}
                    >
                      {post.title || "（无标题）"}
                    </PaletteItem>
                  ))}
                </Command.Group>
              ) : null}

              {posts.data && posts.data.pages.length > 0 ? (
                <Command.Group heading="页面">
                  {posts.data.pages.map((page) => (
                    <PaletteItem
                      key={`page-${page.id}`}
                      value={`页面 ${page.title} #${page.id}`}
                      keywords={[debounced]}
                      icon={FileText}
                      onSelect={() => go(`/pages/${page.id}`)}
                    >
                      {page.title || "（无标题）"}
                    </PaletteItem>
                  ))}
                </Command.Group>
              ) : null}

              <Command.Group heading="跳转">
                {pages.map((item) => (
                  <PaletteItem
                    key={item.to}
                    value={`跳转 ${item.label}`}
                    keywords={item.keywords ? item.keywords.split(" ") : []}
                    icon={item.icon}
                    onSelect={() => go(item.to)}
                  >
                    {item.label}
                  </PaletteItem>
                ))}
              </Command.Group>

              <Command.Group heading="操作">
                {actions.map((action) => (
                  <PaletteItem
                    key={action.label}
                    value={`操作 ${action.label}`}
                    keywords={action.keywords}
                    icon={action.icon}
                    onSelect={action.run}
                  >
                    {action.label}
                  </PaletteItem>
                ))}
              </Command.Group>

              <Command.Group heading="外观">
                {THEME_OPTIONS.map((option) => (
                  <PaletteItem
                    key={option.value}
                    value={`外观 ${option.label}`}
                    keywords={[
                      "theme",
                      "zhuti",
                      "waiguan",
                      "dark",
                      "light",
                      option.value,
                    ]}
                    icon={option.icon}
                    onSelect={() => {
                      setChoice(option.value);
                      setOpen(false);
                    }}
                  >
                    {option.label}
                  </PaletteItem>
                ))}
              </Command.Group>
            </Command.List>

            <div className="flex items-center gap-3 border-line border-t px-4 py-2 text-xs text-ink-subtle">
              <span className="flex items-center gap-1">
                <Kbd>↑</Kbd>
                <Kbd>↓</Kbd>
                选择
              </span>
              <span className="flex items-center gap-1">
                <Kbd>↵</Kbd>
                打开
              </span>
            </div>
          </Command>
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  );
}

function PaletteItem({
  value,
  keywords,
  icon: Icon,
  hint,
  onSelect,
  children,
}: {
  value: string;
  keywords: string[];
  icon: typeof Search;
  hint?: string;
  onSelect: () => void;
  children: ReactNode;
}) {
  return (
    <Command.Item
      value={value}
      keywords={keywords}
      onSelect={onSelect}
      className={cn(
        "flex cursor-pointer items-center gap-2.5 rounded-control px-2 py-2 text-base text-ink",
        "data-[selected=true]:bg-surface-active",
        "[&_svg]:size-4 [&_svg]:shrink-0 [&_svg]:text-ink-muted",
      )}
    >
      <Icon aria-hidden="true" />
      <span className="min-w-0 flex-1 truncate">{children}</span>
      {hint ? <span className="text-xs text-ink-subtle">{hint}</span> : null}
    </Command.Item>
  );
}
