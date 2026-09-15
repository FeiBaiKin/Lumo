import createClient, { type Middleware } from "openapi-fetch";
import type { components, paths } from "./schema";

/**
 * Console 的 API 客户端。
 *
 * 类型全部来自 `schema.d.ts`——它由 `task console:api` 从后端导出的 OpenAPI 规范生成，
 * 不手写、不手改。手写两遍类型是生态项目的慢性病。
 */

/** Console 与 API 同源（都由同一个二进制提供），故用相对地址。 */
const BASE_URL = "/";

/** RFC 9457 的错误体，全站唯一的错误模型。 */
export type Problem = components["schemas"]["Problem"];

/** 请求校验失败时逐条列出的明细。 */
export type ErrorDetail = components["schemas"]["ErrorDetail"];

/** 双提交模式下 CSRF 令牌所在的 Cookie；HTTPS 下服务端改用 `__Host-` 前缀。 */
const CSRF_COOKIES = ["__Host-lumo_csrf", "lumo_csrf"];

const CSRF_HEADER = "X-CSRF-Token";

/** 安全方法不改状态，服务端也不对它们校验 CSRF。 */
const SAFE_METHODS = new Set(["GET", "HEAD", "OPTIONS", "TRACE"]);

function readCookie(name: string): string {
  for (const part of document.cookie.split(";")) {
    const [key, ...rest] = part.trim().split("=");
    if (key === name) {
      return decodeURIComponent(rest.join("="));
    }
  }
  return "";
}

/**
 * 读取当前会话的 CSRF 令牌。
 *
 * 取自 Cookie 而不是登录响应：会话 Cookie 是 HttpOnly 的，CSRF Cookie 则特意留给前端读
 * （见 `internal/auth/session.go`）。这样刷新页面、多标签页都不需要再把令牌存一份。
 */
export function csrfToken(): string {
  for (const name of CSRF_COOKIES) {
    const value = readCookie(name);
    if (value) {
      return value;
    }
  }
  return "";
}

/** 非安全方法自动补上 CSRF 头；PAT 调用不走这里，服务端也不对它要求 CSRF。 */
const csrfMiddleware: Middleware = {
  onRequest({ request }) {
    if (SAFE_METHODS.has(request.method.toUpperCase())) {
      return undefined;
    }
    const token = csrfToken();
    if (token) {
      request.headers.set(CSRF_HEADER, token);
    }
    return request;
  },
};

export const api = createClient<paths>({
  baseUrl: BASE_URL,
  // 会话走 Cookie；同源即可，不需要 include。
  credentials: "same-origin",
});
api.use(csrfMiddleware);

/**
 * 把一次失败的调用转成可直接展示的文字。
 *
 * 优先用逐条明细：`errors[].location` 指到具体字段（`body.title`、`query.page`），
 * 比笼统的「校验失败」有用得多。
 */
export function problemMessage(problem: Problem | undefined): string {
  if (!problem) {
    return "请求失败，请稍后重试";
  }
  const details = (problem.errors ?? [])
    .map((d) => [d.location, d.message].filter(Boolean).join(" "))
    .filter(Boolean);
  if (details.length > 0) {
    return details.join("；");
  }
  return problem.detail || problem.title || "请求失败，请稍后重试";
}
