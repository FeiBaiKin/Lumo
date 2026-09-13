/*
 * 代码块走查脚本（人工执行，不进 CI）。
 *
 * 本机没有任何可用浏览器，故用 jsdom 顶替真实浏览器，把线上页面拉下来、
 * 跑一遍 code.js，验证三个按钮（换行 / 复制 / 展开）确实工作。
 *
 * 用法（先让服务跑起来，并确保开发库里有一篇带长代码块的文章）：
 *
 *   cd console && node ../internal/theme/builtin/ink/scripts/verify-code-block.cjs
 *
 * 放在这里而不是 console/ 下，是因为它是主题的走查工具（同目录还有
 * fetch-display-font.py），且 console/ 的 Biome 会把 CLI 脚本的 console.log
 * 当成诊断报出来。jsdom 从 console/node_modules 借，故仍需在 console 目录下执行。
 *
 * 覆盖不到的部分：真实排版（行号栏对齐、配色、hover 显隐）与真实的文本选中
 * 行为——这些都依赖布局引擎，jsdom 没有。那部分只能人工在浏览器里过一遍。
 */
const fs = require("fs");
const path = require("path");

const ROOT = path.resolve(__dirname, "..", "..", "..", "..", "..");
const JS_PATH = path.join(ROOT, "internal", "theme", "builtin", "ink", "static", "code.js");

let JSDOM;
try {
  ({ JSDOM } = require(path.join(ROOT, "console", "node_modules", "jsdom")));
} catch (err) {
  console.error(
    "找不到 jsdom。请先 cd console && npm install，并在 console 目录下运行本脚本。",
  );
  process.exit(2);
}

const BASE = process.env.LUMO_BASE_URL || "http://127.0.0.1:8080";
// 长代码块（21 行 PHP，超出收起上限）与短代码块各一篇。
const LONG_PATH = "/posts/code-block-walkthrough";
const SHORT_PATH = "/posts/" + encodeURIComponent("用-go-写一个内容管理系统的十七个决定");

let failures = 0;
function check(label, cond, extra) {
  if (cond) {
    console.log("  PASS  " + label);
  } else {
    failures++;
    console.log("  FAIL  " + label + (extra ? "  -> " + extra : ""));
  }
}

/* 把 jsdom 装载起来并跑一遍主题脚本。
 *
 * 不等 jsdom 自己发 DOMContentLoaded，而是等它发完：
 * 手动 dispatch 是不行的——jsdom 在构造后的下一个 tick 还会补发一次真实事件，
 * init 会被跑第二遍，于是每个代码块挂上两套按钮。真实浏览器里 readyState 只
 * 迁移一次，不存在这个问题，纯粹是测试替身的坑。 */
async function loadPage(url, codeJs, opts) {
  const html = await (await fetch(url)).text();
  const dom = new JSDOM(html, { runScripts: "outside-only", pretendToBeVisual: true });
  const { window } = dom;
  const { document } = window;

  const pre = document.querySelector(".code-block pre");
  if (pre && opts && opts.fakeHeight) {
    // jsdom 没有布局引擎，scrollHeight 恒为 0；给个假高度才能走到「超出上限」那条分支。
    Object.defineProperty(pre, "scrollHeight", {
      value: opts.fakeHeight,
      configurable: true,
    });
  }

  let copied = null;
  Object.defineProperty(window.navigator, "clipboard", {
    value: {
      writeText: (t) => {
        copied = t;
        return Promise.resolve();
      },
    },
    configurable: true,
  });
  Object.defineProperty(window, "isSecureContext", { value: true, configurable: true });

  window.eval(codeJs);
  await new Promise((resolve) => {
    if (document.readyState !== "loading") return resolve();
    document.addEventListener("DOMContentLoaded", () => resolve(), { once: true });
    window.addEventListener("load", () => resolve(), { once: true });
    setTimeout(resolve, 2000);
  });
  return { window, document, copied: () => copied };
}

function click(window, el) {
  el.dispatchEvent(new window.MouseEvent("click", { bubbles: true }));
}

(async () => {
  const codeJs = fs.readFileSync(JS_PATH, "utf8");

  console.log("\n[1] 结构：服务端产出的代码块");
  const page = await loadPage(BASE + LONG_PATH, codeJs, { fakeHeight: 3000 });
  const { window, document } = page;
  const block = document.querySelector(".prose .code-block");
  check("存在 .code-block", !!block);
  if (!block) {
    console.log("\n代码块不存在，后续检查无法继续。");
    process.exit(1);
  }
  check("存在 .code-lines", !!block.querySelector("pre.code-lines"));
  const lines = block.querySelectorAll("span.line");
  check("存在逐行 span.line", lines.length > 0, "行数=" + lines.length);
  check("带语言标识 data-lang", block.getAttribute("data-lang") === "PHP", block.getAttribute("data-lang"));

  console.log("\n[2] 行号不进 DOM 文本（复制时不会被带走）");
  const firstLine = lines[0].textContent;
  check("首行文本不含行号", firstLine.indexOf("1") !== 0, JSON.stringify(firstLine));
  check("DOM 里没有 chroma 行号元素", !block.querySelector(".ln"));

  console.log("\n[3] 三个按钮挂上了");
  const tools = block.querySelector(".code-tools");
  check("存在 .code-tools", !!tools);
  const buttons = Array.from(block.querySelectorAll(".code-tool"));
  check(
    "按钮顺序 = wrap, copy, expand",
    JSON.stringify(buttons.map((b) => b.dataset.codeAction)) ===
      JSON.stringify(["wrap", "copy", "expand"]),
    JSON.stringify(buttons.map((b) => b.dataset.codeAction)),
  );
  check("每个按钮都有 aria-label", buttons.every((b) => b.getAttribute("aria-label")));
  check("工具条有可访问名称", tools && tools.getAttribute("aria-label") === "代码块操作");

  console.log("\n[4] 自动换行");
  const wrapBtn = block.querySelector('[data-code-action="wrap"]');
  click(window, wrapBtn);
  check("点击后加上 is-wrapped", block.classList.contains("is-wrapped"));
  check("aria-pressed=true", wrapBtn.getAttribute("aria-pressed") === "true");
  click(window, wrapBtn);
  check("再点取消 is-wrapped", !block.classList.contains("is-wrapped"));

  console.log("\n[5] 复制");
  const copyBtn = block.querySelector('[data-code-action="copy"]');
  click(window, copyBtn);
  await new Promise((r) => setTimeout(r, 30));
  const copied = page.copied();
  check("剪贴板收到了代码", typeof copied === "string" && copied.length > 0);
  check("复制内容以 <?php 开头", !!copied && copied.indexOf("<?php") === 0);
  check("复制内容不含行号", !!copied && !/^\s*\d/.test(copied));
  check("复制内容含多行", !!copied && copied.split("\n").length > 10);
  check("按钮显示已复制态", copyBtn.classList.contains("is-done"));
  check("播报了状态", block.querySelector('[role="status"]').textContent.includes("已复制"));

  console.log("\n[6] 展开 / 收起");
  const expandBtn = block.querySelector('[data-code-action="expand"]');
  check("超长代码块默认折起", block.classList.contains("is-collapsed"));
  check("展开按钮可见", expandBtn.hidden === false);
  click(window, expandBtn);
  check("点击后展开", !block.classList.contains("is-collapsed"));
  check("aria-expanded=true", expandBtn.getAttribute("aria-expanded") === "true");
  check("标签变成收起代码", expandBtn.getAttribute("aria-label") === "收起代码");
  click(window, expandBtn);
  check("再点折起", block.classList.contains("is-collapsed"));

  console.log("\n[7] 高亮 token 落地");
  const tokens = new Set(
    Array.from(block.querySelectorAll("span[class]"))
      .map((el) => el.className)
      .filter((c) => c !== "line" && c !== "cl"),
  );
  check("有语法 token", tokens.size >= 5, [...tokens].join(","));
  check("关键字是 .k", !!block.querySelector("span.k"));
  check("字符串是 .s1/.s2", !!block.querySelector("span.s1, span.s2"));
  check("注释是 .sd", !!block.querySelector("span.sd"));

  console.log("\n[8] 短代码块不该有展开按钮");
  const short = await loadPage(BASE + SHORT_PATH, codeJs, {});
  const shortBlock = short.document.querySelector(".prose .code-block");
  check("短代码块存在", !!shortBlock);
  if (shortBlock) {
    check("短代码块不折起", !shortBlock.classList.contains("is-collapsed"));
    check(
      "展开按钮被隐藏",
      shortBlock.querySelector('[data-code-action="expand"]').hidden === true,
    );
    check("复制与换行按钮仍在", !!shortBlock.querySelector('[data-code-action="copy"]'));
  }

  console.log(failures === 0 ? "\n全部通过" : "\n失败 " + failures + " 项");
  process.exit(failures === 0 ? 0 : 1);
})().catch((err) => {
  console.error("走查脚本出错:", err && err.message ? err.message : err);
  process.exit(2);
});
