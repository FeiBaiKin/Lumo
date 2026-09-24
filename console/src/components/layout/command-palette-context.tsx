import {
  type ReactNode,
  createContext,
  use,
  useEffect,
  useMemo,
  useState,
} from "react";

/**
 * 命令面板的开关状态与全局快捷键（Ctrl/⌘ + K）。
 *
 * 与面板本体分开放：侧栏与移动端导航只需要「打开它」，而面板本体带着 cmdk 与检索逻辑，
 * 等第一次打开时才加载（见 app-shell.tsx）。
 */

export type PaletteContextValue = {
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
