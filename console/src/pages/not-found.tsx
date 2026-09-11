import { Button } from "@/components/ui/button";
import { useDocumentTitle } from "@/lib/use-document-title";
import { FileQuestion } from "lucide-react";
import { Link } from "react-router";

/**
 * 后台内的 404。
 *
 * 与访客前台的 404 是两回事（后者由主题渲染）。这里只需说清
 * 「这个地址不存在」并给出回到概览的出口，不做推荐内容那类花样。
 */
export function NotFoundPage() {
  useDocumentTitle("页面不存在");
  return (
    <div className="flex flex-col items-center justify-center gap-4 py-24 text-center">
      <FileQuestion aria-hidden="true" className="size-8 text-ink-subtle" />
      <div className="flex flex-col gap-1">
        <h1 className="text-xl font-semibold text-ink">这里没有页面</h1>
        <p className="text-sm text-ink-muted">
          地址可能拼错了，或者对应的功能已经被移走。
        </p>
      </div>
      <Button variant="secondary" asChild>
        <Link to="/">返回概览</Link>
      </Button>
    </div>
  );
}
