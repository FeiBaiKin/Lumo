import { afterEach, describe, expect, it } from "vitest";
import { csrfToken, problemMessage } from "./client";

function clearCookies() {
  for (const part of document.cookie.split(";")) {
    const name = part.trim().split("=")[0];
    if (name) {
      document.cookie = `${name}=; Max-Age=0; path=/`;
    }
  }
}

describe("csrfToken", () => {
  afterEach(clearCookies);

  it("从 Cookie 读取令牌", () => {
    document.cookie = "lumo_csrf=abc123; path=/";
    expect(csrfToken()).toBe("abc123");
  });

  it("未登录时返回空串", () => {
    expect(csrfToken()).toBe("");
  });

  it("值经过百分号编码时能还原", () => {
    document.cookie = `lumo_csrf=${encodeURIComponent("a+b/c=")}; path=/`;
    expect(csrfToken()).toBe("a+b/c=");
  });

  it("不会被名字相近的 Cookie 误导", () => {
    document.cookie = "lumo_csrf_other=nope; path=/";
    expect(csrfToken()).toBe("");
  });
});

describe("problemMessage", () => {
  it("优先展示逐条明细", () => {
    expect(
      problemMessage({
        status: 422,
        title: "Unprocessable Entity",
        type: "about:blank",
        detail: "校验失败",
        errors: [
          { location: "body.title", message: "不能为空" },
          { location: "query.page", message: "最小为 1" },
        ],
      }),
    ).toBe("body.title 不能为空；query.page 最小为 1");
  });

  it("没有明细时退回 detail", () => {
    expect(
      problemMessage({
        status: 409,
        title: "Conflict",
        type: "about:blank",
        detail: "标识已被占用",
      }),
    ).toBe("标识已被占用");
  });

  it("网络错误一类拿不到响应体时给出兜底文案", () => {
    expect(problemMessage(undefined)).toBe("请求失败，请稍后重试");
  });
});
