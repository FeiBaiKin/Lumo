import { api, csrfToken, problemMessage } from "@/api/client";
import type { components } from "@/api/schema";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { createContext, use, useCallback, useMemo } from "react";

/**
 * 会话状态。
 *
 * 会话本身在服务端（HttpOnly Cookie），前端唯一的真相来源是 `/auth/me`。
 * 因此这里不用「登录成功后往 store 里写用户」那种做法 —— 刷新页面、多标签页登录、
 * 会话在别处被吊销，都会让本地缓存与真实状态分叉。一切以 /auth/me 的查询结果为准。
 *
 * 权限判定的入口是 `can()`。它与服务端 `perm.Set.Allows` 的语义一致：
 * 前端只用来决定「按钮显不显示」，真正的放行一律由服务端做 ——
 * 前端隐藏按钮是体验，不是安全措施。
 */

export type Me = components["schemas"]["MeView"];
export type SessionUser = components["schemas"]["UserView"];

export const SESSION_QUERY_KEY = ["session"] as const;

type AuthContextValue = {
  me: Me | undefined;
  user: SessionUser | undefined;
  permissions: Set<string>;
  /** 会话是否是 PAT 调用（无 Cookie，故无需 CSRF）。 */
  isToken: boolean;
  isLoading: boolean;
  /** 是否已确认未登录（查询成功返回 401），用于路由守卫区分「还没查完」与「确实没登录」。 */
  isAnonymous: boolean;
  /** 权限判定。支持 `_any` 语义：持有 `posts:write_any` 时 `can("posts:write")` 为真。 */
  can: (permission: string) => boolean;
  login: (login: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
};

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const queryClient = useQueryClient();

  const { data, isLoading, isError } = useQuery({
    queryKey: SESSION_QUERY_KEY,
    queryFn: async (): Promise<Me | null> => {
      const { data: me, response } = await api.GET("/api/v1/console/auth/me");
      // 401 是「未登录」这一正常状态，不是错误：让它走 null 分支，
      // 否则 React Query 会把每次匿名访问都记为一次失败重试。
      if (response.status === 401) {
        return null;
      }
      if (!me) {
        throw new Error("无法读取会话");
      }
      return me;
    },
    // 会话在服务端滑动过期，前端不必频繁重查；窗口重新聚焦时再确认一次即可。
    staleTime: 60_000,
    retry: false,
  });

  const permissions = useMemo(() => new Set(data?.permissions ?? []), [data]);

  const can = useCallback(
    (permission: string) => {
      if (permissions.has(permission)) {
        return true;
      }
      // 服务端：同时持有基础权限与 _any 权限时以 _any 为准。
      // 前端反过来 —— 只要任一成立即认为可操作，少显示一个按钮比显示一个必然 403 的按钮好。
      const [resource, action] = permission.split(":");
      if (!action?.endsWith("_any")) {
        return permissions.has(`${resource}:${action}_any`);
      }
      return false;
    },
    [permissions],
  );

  const login = useCallback(
    async (loginName: string, password: string) => {
      const { error, response } = await api.POST("/api/v1/console/auth/login", {
        body: { login: loginName, password },
      });
      if (!response.ok) {
        throw new Error(problemMessage(error));
      }
      // 登录会新建会话并换发 CSRF Cookie，之前缓存的任何数据都属于上一个身份。
      await queryClient.invalidateQueries();
    },
    [queryClient],
  );

  const logout = useCallback(async () => {
    // 登出前取一次令牌：清除 Cookie 之后就读不到了。
    // 正常会话下 csrfMiddleware 会自动补这个头，但登出的响应会清 Cookie，
    // 显式带上能保证中间件与这里读到的是同一时刻的值。
    const token = csrfToken();
    await api.POST(
      "/api/v1/console/auth/logout",
      token ? { headers: { "X-CSRF-Token": token } } : {},
    );
    queryClient.clear();
  }, [queryClient]);

  const value = useMemo<AuthContextValue>(
    () => ({
      me: data ?? undefined,
      user: data?.user,
      permissions,
      isToken: data?.authMethod === "token",
      isLoading,
      isAnonymous: !isLoading && (isError || !data),
      can,
      login,
      logout,
    }),
    [data, permissions, isLoading, isError, can, login, logout],
  );

  return <AuthContext value={value}>{children}</AuthContext>;
}

export function useAuth(): AuthContextValue {
  const value = use(AuthContext);
  if (!value) {
    throw new Error("useAuth 必须在 AuthProvider 内使用");
  }
  return value;
}
