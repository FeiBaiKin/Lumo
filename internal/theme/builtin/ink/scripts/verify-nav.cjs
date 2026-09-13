/*
 * 二级菜单走查脚本（人工执行，不进 CI）。
 *
 * 与 verify-code-block.cjs 同路：用 jsdom 顶替真实浏览器跑一遍 static/nav.js，
 * 验证「点击展开 / 再点收起 / 只开一个 / Esc 收起并还焦点 / 点空白处收起」。
 *
 * 与那份脚本的区别：它要连上真实站点（文章与代码块都在库里），
 * 这份**自带 DOM 夹板**，不需要服务与数据库——菜单结构由模板产出，
 * 而这里要验的是脚本对着那套结构做了什么，两者之间只隔着 aria-controls 这一个契约。
 *
 * 用法：
 *
 *   cd console && node ../internal/theme/builtin/ink/scripts/verify-nav.cjs
 *
 * 放在这里而不是 console/ 下，理由同 verify-code-block.cjs；jsdom 从
 * console/node_modules 借，故仍需在 console 目录下执行。
 *
 * 覆盖不到的部分：真实的键盘 Tab 顺序、真实的 CSS 展开动效与布局——
 * jsdom 没有布局引擎，那两项只能在浏览器里人工过一遍
 * （尤其是「没有 JS 时子菜单会不会浮在正文之上」，那条只能靠真浏览器截图看）。
 */
const fs = require("fs");
const path = require("path");

const ROOT = path.resolve(__dirname, "..", "..", "..", "..", "..");
const NAV_JS = path.join(
  ROOT,
  "internal",
  "theme",
  "builtin",
  "ink",
  "static",
  "nav.js",
);

let JSDOM;
try {
  ({ JSDOM } = require(path.join(ROOT, "console", "node_modules", "jsdom")));
} catch {
  console.error(
    "找不到 jsdom。请先 cd console && npm install，并在 console 目录下运行本脚本。",
  );
  process.exit(2);
}

let failures = 0;
function check(label, cond, extra) {
  if (cond) {
    console.log("  PASS  " + label);
  } else {
    failures++;
    console.log("  FAIL  " + label + (extra ? "  -> " + extra : ""));
  }
}

/* 夹板 DOM：结构与 partials/header.html 渲染出来的完全一致，
 * 包括「子菜单**不带** hidden」这一点——那正是没有 JS 时的可用形态。 */
const FIXTURE = `<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><title>导航走查</title></head>
<body>
<header class="site-header">
  <div class="wrap">
    <nav class="site-nav" aria-label="主导航">
      <ul>
        <li class="has-sub">
          <button type="button" class="site-nav-toggle" aria-expanded="false" aria-controls="site-submenu-0">
            关于<svg class="site-nav-chevron" viewBox="0 0 24 24" aria-hidden="true"><path d="m6 9 6 6 6-6"/></svg>
          </button>
          <ul class="site-nav-sub" id="site-submenu-0">
            <li><a href="/about">关于</a></li>
            <li><a href="/contact">联系</a></li>
          </ul>
        </li>
        <li class="has-sub">
          <button type="button" class="site-nav-toggle" aria-expanded="false" aria-controls="site-submenu-1">
            归档<svg class="site-nav-chevron" viewBox="0 0 24 24" aria-hidden="true"><path d="m6 9 6 6 6-6"/></svg>
          </button>
          <ul class="site-nav-sub" id="site-submenu-1">
            <li><a href="/archives/2026">2026</a></li>
          </ul>
        </li>
        <li><a href="/plain">纯链接</a></li>
      </ul>
    </nav>
  </div>
</header>
</body></html>`;

/* 收起是**延迟**的：nav.js 先加 .is-closing 让它淡出（90ms），到点才把 hidden 置上，
 * 这样收起才有动画可依附。断言必须等过这一段，否则测的是动画还没跑完的中间态。
 * 留 150ms 的余量，比 90 宽出一截，避免慢机器上偶发失败。 */
const settle = () => new Promise((resolve) => setTimeout(resolve, 150));

/** 等 jsdom 把文档解析完（readyState 变成 complete），再跑主题脚本。 */
function ready(dom) {
  return new Promise((resolve) => {
    if (dom.window.document.readyState === "complete") {
      resolve();
      return;
    }
    dom.window.addEventListener("load", () => resolve(), { once: true });
  });
}

async function main() {
  const source = fs.readFileSync(NAV_JS, "utf8");
  const dom = new JSDOM(FIXTURE, {
    runScripts: "outside-only",
    pretendToBeVisual: true,
  });
  await ready(dom);

  const { window } = dom;
  const { document } = window;
  window.eval(source);

  const buttons = Array.from(document.querySelectorAll(".site-nav-toggle"));
  const subs = buttons.map((b) => document.getElementById(b.getAttribute("aria-controls")));
  const expanded = (b) => b.getAttribute("aria-expanded") === "true";
  const nav = document.querySelector(".site-nav");
  const click = (el) => el.dispatchEvent(new window.MouseEvent("click", { bubbles: true }));
  const pointerDown = (el) =>
    el.dispatchEvent(new window.Event("pointerdown", { bubbles: true }));

  if (buttons.length !== 2 || subs.some((s) => !s)) {
    console.log("  FAIL  夹板结构与 aria-controls 对不上，无法继续");
    process.exit(1);
  }

  console.log("初始化");
  check(
    "脚本给 nav 盖上了 data-nav=js（CSS 靠它才敢用绝对定位的下拉）",
    nav.dataset.nav === "js",
    "少了这个标记，没有 JS 时子菜单会浮在正文之上",
  );
  check(
    "子菜单默认被收起（模板里没写 hidden，收起是脚本做的）",
    subs.every((s) => s.hidden === true),
  );
  check("按钮的 aria-expanded 初始为 false", buttons.every((b) => !expanded(b)));

  console.log("点击展开与收起");
  click(buttons[0]);
  check("点第一项即展开", subs[0].hidden === false && expanded(buttons[0]));
  click(buttons[0]);
  check("再点一次时 aria-expanded 立刻回到 false", !expanded(buttons[0]));
  await settle();
  check("收起动效结束后子菜单真正隐藏", subs[0].hidden === true);

  console.log("同时只开一个");
  click(buttons[0]);
  click(buttons[1]);
  check("展开态只有一项", buttons.filter((b) => expanded(b)).length === 1);
  await settle();
  check(
    "开第二项时第一项自动收起",
    subs[1].hidden === false && subs[0].hidden === true,
  );

  console.log("Esc 收起并把焦点还给按钮");
  // 光标停在按钮上时按 Esc，最贴近键盘用户的实际操作。
  buttons[1].focus();
  document.dispatchEvent(
    new window.KeyboardEvent("keydown", { key: "Escape", bubbles: true }),
  );
  check("Esc 后焦点立刻回到触发它的按钮", document.activeElement === buttons[1]);
  await settle();
  check("Esc 关闭当前展开项", subs[1].hidden === true);

  console.log("点击页面其他位置收起");
  click(buttons[0]);
  pointerDown(document.body);
  check("点空白处后 aria-expanded 立刻回到 false", buttons.every((b) => !expanded(b)));
  await settle();
  check("点 nav 之外的位置会全部收起", subs.every((s) => s.hidden === true));

  console.log("焦点移出 nav 收起");
  click(buttons[0]);
  nav.dispatchEvent(
    new window.FocusEvent("focusout", {
      bubbles: true,
      relatedTarget: document.body,
    }),
  );
  await settle();
  check("焦点移出整个导航时收起", subs[0].hidden === true);

  console.log("焦点仍在 nav 内时不收起");
  click(buttons[0]);
  nav.dispatchEvent(
    new window.FocusEvent("focusout", {
      bubbles: true,
      relatedTarget: subs[0].querySelector("a"),
    }),
  );
  await settle();
  check(
    "焦点从按钮移进子菜单时保持展开",
    subs[0].hidden === false,
    "这是「Tab 进入子项」的那一步，误关会让键盘用户走不下去",
  );

  console.log("");
  if (failures > 0) {
    console.log(failures + " 项未通过");
    process.exit(1);
  }
  console.log("全部通过");
}

main().catch((err) => {
  console.error("脚本执行失败:", err);
  process.exit(1);
});
