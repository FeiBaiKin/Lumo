import {
  createContext,
  use,
  useCallback,
  useEffect,
  useMemo,
  useState,
} from "react";

/**
 * 明暗主题。
 *
 * 三态而非两态：默认「跟随系统」。站长在白天与夜里用同一台机器时，
 * 手动二选一会让他在另一种环境下每次都要改一次。
 *
 * 主题选择存在 localStorage；`prefers-color-scheme` 只在「跟随系统」时参与判断，
 * 且监听它的变化 —— 定时切换深色的系统设置要能实时反映过来。
 */

export type ThemeChoice = "light" | "dark" | "system";
export type ResolvedTheme = "light" | "dark";

const STORAGE_KEY = "lumo-console-theme";

type ThemeContextValue = {
  /** 用户的选择（含「跟随系统」） */
  choice: ThemeChoice;
  /** 实际生效的主题 */
  resolved: ResolvedTheme;
  setChoice: (choice: ThemeChoice) => void;
};

const ThemeContext = createContext<ThemeContextValue | null>(null);

function readStoredChoice(): ThemeChoice {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (raw === "light" || raw === "dark" || raw === "system") {
      return raw;
    }
  } catch {
    // 隐私模式下 localStorage 会抛异常；退回默认值即可，不影响功能。
  }
  return "system";
}

function systemTheme(): ResolvedTheme {
  return window.matchMedia("(prefers-color-scheme: dark)").matches
    ? "dark"
    : "light";
}

export function ThemeProvider({ children }: { children: React.ReactNode }) {
  const [choice, setChoiceState] = useState<ThemeChoice>(readStoredChoice);
  const [system, setSystem] = useState<ResolvedTheme>(systemTheme);

  // 跟随系统时要持续监听；用户手动选定后这个监听仍然挂着但不影响结果，
  // 因为 resolved 只在 choice === "system" 时取 system。
  useEffect(() => {
    const query = window.matchMedia("(prefers-color-scheme: dark)");
    const onChange = () => setSystem(query.matches ? "dark" : "light");
    query.addEventListener("change", onChange);
    return () => query.removeEventListener("change", onChange);
  }, []);

  const resolved: ResolvedTheme = choice === "system" ? system : choice;

  // 落类而不是落内联样式：Tailwind 的 dark 变体与 .dark 类绑定，
  // 且整棵子树的语义变量都由这一个类切换。
  useEffect(() => {
    const root = document.documentElement;
    root.classList.toggle("dark", resolved === "dark");
    // 让浏览器原生控件（滚动条、表单控件、日期选择器）也跟着变，
    // 否则暗色页面上会浮出亮色的下拉框。
    root.style.colorScheme = resolved;
  }, [resolved]);

  const setChoice = useCallback((next: ThemeChoice) => {
    setChoiceState(next);
    try {
      localStorage.setItem(STORAGE_KEY, next);
    } catch {
      // 存不下就只在本次会话内生效，不打断用户。
    }
  }, []);

  const value = useMemo(
    () => ({ choice, resolved, setChoice }),
    [choice, resolved, setChoice],
  );

  return <ThemeContext value={value}>{children}</ThemeContext>;
}

export function useTheme(): ThemeContextValue {
  const value = use(ThemeContext);
  if (!value) {
    throw new Error("useTheme 必须在 ThemeProvider 内使用");
  }
  return value;
}
