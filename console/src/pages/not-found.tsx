import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/states";
import { useDocumentTitle } from "@/lib/use-document-title";
import { FileQuestion } from "lucide-react";
import { Link } from "react-router";

/**
 * 后台内的 404。
 *
 * 与访客前台的 404 是两回事（后者由主题渲染）。这里只需说清
 * 「这个地址不存在」并给出回到概览的出口。
 */
export function NotFoundPage() {
  useDocumentTitle("页面不存在");
  return (
    <div className="flex min-h-[60vh] flex-col items-center justify-center">
      <h1 className="sr-only">页面不存在</h1>
      <EmptyState
        icon={FileQuestion}
        title="这里没有页面"
        description="地址可能拼错了，或者对应的功能已经被移走。"
        action={
          <Button variant="secondary" asChild>
            <Link to="/">返回概览</Link>
          </Button>
        }
      />
    </div>
  );
}
