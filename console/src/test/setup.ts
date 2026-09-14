import "@testing-library/jest-dom/vitest";
import { transferableAbortController } from "node:util";
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

/**
 * jsdom 不实现 `scrollIntoView`。
 *
 * 页面在「跳到出错的字段」「展开某个设置区块」之后都会滚过去，
 * 而缺了它抛出的是 TypeError，表现为一条与滚动毫无关系的测试失败。
 * 给一个空实现即可：滚动位置本来也不是 jsdom 里能断言的东西。
 */
if (!Element.prototype.scrollIntoView) {
  Element.prototype.scrollIntoView = function scrollIntoView() {};
}

/** 让每个用例从干净的存储开始，避免主题选择在用例之间串味。 */
afterEach(() => {
  localStorage.clear();
});

/**
 * 换回 Node 原生的 AbortController / AbortSignal。
 *
 * jsdom 提供的是自己的一套实现，而 `Request` 来自 Node（undici），它要求
 * signal 必须是 Node 的 AbortSignal 实例。数据路由在每次导航时都会
 * `new Request(url, { signal })`，于是必然抛
 * "Expected signal (...) to be an instance of AbortSignal"。
 *
 * Node 没法直接 new 出 AbortSignal，但 util.transferableAbortController()
 * 返回的就是原生控制器，取它的 constructor 即可拿回原生类。
 */
const nativeController = transferableAbortController();
globalThis.AbortController =
  nativeController.constructor as typeof AbortController;
globalThis.AbortSignal = nativeController.signal
  .constructor as typeof AbortSignal;
