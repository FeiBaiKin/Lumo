/*
 * 墨 Ink 的代码块交互：自动换行、复制、展开/收起。
 *
 * 服务端只吐语义化的结构（行号靠 CSS 计数器，保证复制时不带行号，见
 * internal/content/highlight.go），三个按钮由这里挂上去。这样拆的好处是：
 * 没有 JS 时代码块依然完整可读——高亮、行号、横向滚动都在 CSS 与服务端手里，
 * 丢的只是三个便利按钮，而不是整段代码。
 *
 * 按钮默认隐身，鼠标进入代码块才浮现；触屏与键盘另有出口（见 theme.css）。
 */
(function () {
  "use strict";

  // 内联的 Lucide 图标（agent.md §11.1：全站只用 Lucide，且界面禁用表情符号）。
  var ICONS = {
    wrap:
      '<path d="M3 6h18"/><path d="M3 12h13a3 3 0 1 1 0 6h-3"/><path d="m16 15-3 3 3 3"/><path d="M3 18h7"/>',
    copy:
      '<rect width="12" height="12" x="9" y="9" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/>',
    check: '<path d="M20 6 9 17l-5-5"/>',
    expand: '<path d="m7 15 5 5 5-5"/><path d="m7 9 5-5 5 5"/>',
    collapse: '<path d="m7 20 5-5 5 5"/><path d="m7 4 5 5 5-5"/>',
  };

  // 复制成功的反馈时长。1.6s 够看清，又不会久到让人以为按钮卡住了。
  var COPY_FEEDBACK_MS = 1600;

  function svg(inner, cls) {
    return (
      '<svg class="' +
      cls +
      '" viewBox="0 0 24 24" aria-hidden="true" focusable="false">' +
      inner +
      "</svg>"
    );
  }

  function makeButton(action, label, iconInner) {
    var btn = document.createElement("button");
    btn.type = "button";
    btn.className = "code-tool";
    btn.dataset.codeAction = action;
    btn.setAttribute("aria-label", label);
    btn.title = label;
    btn.innerHTML = svg(iconInner, "icon-idle") + svg(ICONS.check, "icon-done");
    return btn;
  }

  /* 复制到剪贴板。
   *
   * navigator.clipboard 只在安全上下文（https 或 localhost）可用，
   * 而本地开发与不少自建站是 http 的，故保留 execCommand 这条退路。 */
  function copyText(text, done) {
    if (navigator.clipboard && window.isSecureContext) {
      navigator.clipboard.writeText(text).then(done, function () {
        fallbackCopy(text, done);
      });
      return;
    }
    fallbackCopy(text, done);
  }

  function fallbackCopy(text, done) {
    var area = document.createElement("textarea");
    area.value = text;
    // 放在视口外：留在视口内会触发滚动跳动。
    area.setAttribute("readonly", "");
    area.style.position = "fixed";
    area.style.top = "-1000px";
    area.style.opacity = "0";
    document.body.appendChild(area);
    area.select();
    var ok = false;
    try {
      ok = document.execCommand("copy");
    } catch (err) {
      ok = false;
    }
    document.body.removeChild(area);
    if (ok) done();
  }

  /* 取代码的纯文本。
   *
   * textContent 天然不含行号——行号是 CSS 生成的伪元素内容，不在 DOM 里。
   * 这正是「行号不由服务端写成文本」换来的好处。 */
  function codeText(pre) {
    return pre.textContent.replace(/\n+$/, "");
  }

  function setup(block) {
    // 幂等：脚本被引入两次时（主题里手写了一份、又被第三方主题内联一次），
    // 不该在同一段代码上叠出两套按钮。
    if (block.querySelector(".code-tools")) return;

    var pre = block.querySelector("pre");
    if (!pre) return;

    var tools = document.createElement("div");
    tools.className = "code-tools";
    // 工具条自己不参与读屏的阅读顺序，按钮各自带 aria-label。
    tools.setAttribute("role", "group");
    tools.setAttribute("aria-label", "代码块操作");

    var status = document.createElement("span");
    status.className = "code-tool-status";
    status.setAttribute("role", "status");
    status.setAttribute("aria-live", "polite");

    // --- 换行 ---
    var wrapBtn = makeButton("wrap", "自动换行", ICONS.wrap);
    wrapBtn.setAttribute("aria-pressed", "false");
    wrapBtn.addEventListener("click", function () {
      var on = block.classList.toggle("is-wrapped");
      wrapBtn.setAttribute("aria-pressed", String(on));
      wrapBtn.setAttribute("aria-label", on ? "取消自动换行" : "自动换行");
      wrapBtn.title = wrapBtn.getAttribute("aria-label");
      // 换行会改变高度，收起态的判定要跟着重算。
      refreshCollapse();
    });

    // --- 复制 ---
    var copyBtn = makeButton("copy", "复制代码", ICONS.copy);
    var copyTimer = null;
    copyBtn.addEventListener("click", function () {
      copyText(codeText(pre), function () {
        copyBtn.classList.add("is-done");
        copyBtn.setAttribute("aria-label", "已复制");
        copyBtn.title = "已复制";
        status.textContent = "代码已复制";
        if (copyTimer) clearTimeout(copyTimer);
        copyTimer = setTimeout(function () {
          copyBtn.classList.remove("is-done");
          copyBtn.setAttribute("aria-label", "复制代码");
          copyBtn.title = "复制代码";
          status.textContent = "";
        }, COPY_FEEDBACK_MS);
      });
    });

    // --- 展开 / 收起 ---
    var expandBtn = makeButton("expand", "展开代码", ICONS.expand);
    expandBtn.setAttribute("aria-expanded", "false");
    expandBtn.addEventListener("click", function () {
      var collapsed = block.classList.toggle("is-collapsed");
      expandBtn.setAttribute("aria-expanded", String(!collapsed));
      var label = collapsed ? "展开代码" : "收起代码";
      expandBtn.setAttribute("aria-label", label);
      expandBtn.title = label;
      expandBtn.innerHTML =
        svg(collapsed ? ICONS.expand : ICONS.collapse, "icon-idle") +
        svg(ICONS.check, "icon-done");
    });

    /*
     * 收起的高度上限从 CSS 变量读，避免把 24rem 这个数在 CSS 与 JS 里各写一遍
     * ——CSS 始终是权威（见 theme.css 的 --code-collapse-h）。
     *
     * FALLBACK 只在变量读不到时生效（样式表没加载、被第三方主题覆盖掉）。
     * 有它兜底，「读不到阈值」才不至于变成「展开功能整个消失」。
     */
    var FALLBACK_COLLAPSE_REM = 24;

    function collapseLimit() {
      var rootFont = parseFloat(getComputedStyle(document.documentElement).fontSize) || 16;
      var raw = getComputedStyle(block).getPropertyValue("--code-collapse-h");
      var n = parseFloat(raw);
      if (!n) return FALLBACK_COLLAPSE_REM * rootFont;
      return raw.indexOf("rem") >= 0 ? n * rootFont : n;
    }

    function refreshCollapse() {
      var limit = collapseLimit();
      var collapsed = block.classList.contains("is-collapsed");
      // 量的是内容真实高度：临时解除折叠再量，量完恢复。
      var expandedHeight;
      if (collapsed) {
        block.classList.remove("is-collapsed");
        expandedHeight = pre.scrollHeight;
        block.classList.add("is-collapsed");
      } else {
        expandedHeight = pre.scrollHeight;
      }
      expandBtn.hidden = expandedHeight <= limit + 8;
    }

    tools.appendChild(wrapBtn);
    tools.appendChild(copyBtn);
    tools.appendChild(expandBtn);
    block.appendChild(tools);
    block.appendChild(status);

    // 初次进来：超过上限就先折起，让长代码不占满整屏。
    var limit = collapseLimit();
    if (pre.scrollHeight > limit + 8) {
      block.classList.add("is-collapsed");
    }
    refreshCollapse();

    // 字体加载完、窗口变化后高度会变，收起判定要重算。
    if (document.fonts && document.fonts.ready) {
      document.fonts.ready.then(refreshCollapse);
    }
    var resizeTimer = null;
    window.addEventListener("resize", function () {
      if (resizeTimer) clearTimeout(resizeTimer);
      resizeTimer = setTimeout(refreshCollapse, 150);
    });
  }

  function init() {
    var blocks = document.querySelectorAll(".prose .code-block");
    for (var i = 0; i < blocks.length; i++) setup(blocks[i]);
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
})();
