import { Button } from "@/components/ui/button";
import { PageHeader } from "@/components/ui/panel";
import { useDocumentTitle } from "@/lib/use-document-title";
import { Construction } from "lucide-react";
import { Link } from "react-router";

/**
 * 尚未实现的页面。
 *
 * 阶段 7 按组推进，先立此占位以保证外壳可跑通。
 * 它明确写出「哪个页面、属于哪一组、对应哪个接口」——
 * 一个只写「敬请期待」的占位页在开发期没有任何价值。
 */
function Placeholder({
  title,
  group,
  note,
}: {
  title: string;
  group: string;
  note: string;
}) {
  useDocumentTitle(title);
  return (
    <>
      <PageHeader title={title} description={`${group} · 待实现`} />
      <div className="flex flex-col items-center justify-center gap-3 rounded-panel border border-line border-dashed bg-surface px-6 py-16 text-center">
        <Construction aria-hidden="true" className="size-7 text-ink-subtle" />
        <p className="text-sm text-ink-muted">{note}</p>
        <Button variant="secondary" size="sm" asChild>
          <Link to="/">返回概览</Link>
        </Button>
      </div>
    </>
  );
}

/**
 * 路由表驱动。
 *
 * 用数组而非手写一堆 `<Route>`：占位页的路径与标题必须与 agent.md §8 的页面地图一致，
 * 集中在一处才看得出漏了哪一页。
 */
export const PLACEHOLDER_ROUTES = [
  {
    path: "posts",
    element: () => (
      <Placeholder
        title="文章"
        group="内容"
        note="列表、筛选、批量操作与状态流转，接 /api/v1/console/posts"
      />
    ),
  },
  {
    path: "pages",
    element: () => (
      <Placeholder
        title="页面"
        group="内容"
        note="与文章同构，接 /api/v1/console/pages"
      />
    ),
  },
  {
    path: "categories",
    element: () => (
      <Placeholder
        title="分类"
        group="内容"
        note="树形结构，接 /api/v1/console/categories/tree"
      />
    ),
  },
  {
    path: "tags",
    element: () => (
      <Placeholder title="标签" group="内容" note="接 /api/v1/console/tags" />
    ),
  },
  {
    path: "comments",
    element: () => (
      <Placeholder
        title="评论"
        group="内容"
        note="审核、标垃圾、回复，接 /api/v1/console/comments"
      />
    ),
  },
  {
    path: "media",
    element: () => (
      <Placeholder
        title="附件"
        group="媒体"
        note="上传、网格浏览、改 alt 与标题，接 /api/v1/console/media"
      />
    ),
  },
  {
    path: "themes",
    element: () => (
      <Placeholder
        title="主题"
        group="外观"
        note="上传、启用、按 settings.yaml 渲染设置表单，接 /api/v1/console/themes"
      />
    ),
  },
  {
    path: "menus",
    element: () => (
      <Placeholder
        title="菜单"
        group="外观"
        note="条目树整体替换式保存，接 /api/v1/console/menus"
      />
    ),
  },
  {
    path: "users",
    element: () => (
      <Placeholder
        title="用户"
        group="用户"
        note="创建、启停、设角色、重置口令，含自锁防护提示"
      />
    ),
  },
  {
    path: "roles",
    element: () => (
      <Placeholder
        title="角色"
        group="用户"
        note="权限清单驱动的角色编辑器，接 /api/v1/console/roles"
      />
    ),
  },
  {
    path: "settings/:group",
    element: () => (
      <Placeholder
        title="设置"
        group="设置"
        note="通用表单引擎渲染 JSON Schema + x-widget，站点与主题设置共用"
      />
    ),
  },
  {
    path: "about",
    element: () => (
      <Placeholder title="关于" group="系统" note="版本、环境与构建信息" />
    ),
  },
  {
    path: "logs",
    element: () => (
      <Placeholder
        title="日志"
        group="系统"
        note="运行日志查看（需服务端支持）"
      />
    ),
  },
];
