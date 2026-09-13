import { useAuth } from "@/components/auth/auth-provider";
import { Logo } from "@/components/layout/logo";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/states";
import { useDocumentTitle } from "@/lib/use-document-title";
import { ShieldOff } from "lucide-react";
import { useState } from "react";

/**
 * 零权限账号看到的页面。
 *
 * **这不是安全边界，别把它当成一条防线。** 零权限账号的 `/auth/me` 是一个合法的 200，
 * Console 又是一份谁都能下载的静态 SPA —— 真正的防线是每个端点各自的权限校验
 * （已有，且不在前端）。这一页只解决一件事：别让人对着一个处处 403 的空后台发呆，
 * 以为自己把站点搞坏了。
 *
 * 因此也不要因为「反正会显示这一页」而放宽任何端点校验。
 */
export function NoConsoleAccessPage() {
  const { user, logout } = useAuth();
  const [pending, setPending] = useState(false);

  useDocumentTitle("没有后台权限");

  async function onLogout() {
    setPending(true);
    try {
      await logout();
    } finally {
      setPending(false);
    }
  }

  return (
    <div className="min-h-dvh bg-surface px-4 py-[8vh]">
      <div className="mx-auto flex w-full max-w-[28rem] flex-col items-center gap-8">
        <Logo size="lg" />

        <EmptyState
          icon={ShieldOff}
          title="这个账号没有后台权限"
          description={
            user?.displayName
              ? `${user.displayName} 是站点前台的会员账号，可以在前台登录、评论；后台需要管理员授予角色。`
              : "你的账号是站点前台的会员账号，可以在前台登录使用；后台需要管理员授予角色。"
          }
          action={
            <>
              <Button variant="secondary" asChild>
                {/* 用原生锚点而不是路由跳转：这是离开 SPA 去访客前台。 */}
                <a href="/">返回站点</a>
              </Button>
              <Button variant="ghost" loading={pending} onClick={onLogout}>
                {pending ? "正在退出" : "退出登录"}
              </Button>
            </>
          }
        />
      </div>
    </div>
  );
}
