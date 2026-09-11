import "@testing-library/jest-dom/vitest";
import { afterEach, vi } from "vitest";

/**
 * jsdom 没有实现 `matchMedia`，而主题 Provider 要用它读系统偏好。
 *
 * 这里给一个可用的替身：默认「非深色」，并支持 addEventListener，
 * 这样主题切换与「跟随系统」两条路径都能在测试里跑到。
 * 不引 jsdom 的 matchMedia polyfill 包 —— 只为这一个 API 加一个依赖不划算。
 */
if (!window.matchMedia) {
  Object.defineProperty(window, "matchMedia", {
    writable: true,
    value: (query: string) => ({
      matches: false,
      media: query,
      onchange: null,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      addListener: vi.fn(),
      removeListener: vi.fn(),
      dispatchEvent: vi.fn(),
    }),
  });
}

/**
 * jsdom 也不实现 `ResizeObserver`，而 Radix 的若干浮层组件会用到它。
 */
if (!globalThis.ResizeObserver) {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  } as unknown as typeof ResizeObserver;
}

/** 让每个用例从干净的存储开始，避免主题选择在用例之间串味。 */
afterEach(() => {
  localStorage.clear();
});
