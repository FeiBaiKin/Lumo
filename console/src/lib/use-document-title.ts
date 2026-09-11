import { useEffect } from "react";

/**
 * 设置页面标题。
 *
 * 后台常在浏览器里开十几个标签页，标题一律是「Lumo Console」的话根本分不清哪个是哪个。
 * 标题统一形如「文章 · Lumo」，最后一段是品牌。
 */
export function useDocumentTitle(title: string) {
  useEffect(() => {
    const previous = document.title;
    document.title = title ? `${title} · Lumo` : "Lumo";
    return () => {
      document.title = previous;
    };
  }, [title]);
}
