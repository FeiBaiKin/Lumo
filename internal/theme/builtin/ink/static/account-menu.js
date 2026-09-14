/*
 * 墨 Ink 的页眉账户菜单：点头像弹出「个人中心 / 我的收藏 / 管理后台 / 退出登录」。
 *
 * 不依赖任何库，也**不进 motion.js**：菜单里装的是功能不是装饰
 * （与 nav.js / auth.js / code.js 同一条理由，站长关掉动效总闸之后它仍然必须能开）。
 *
 * 渐进增强的方向与 nav.js 一致而与 comments.js 相反：没有这段脚本时，
 * 头像就是一个指向 /account 的普通链接，而菜单里的四项在那一页上一件不少。
 * 所以菜单在模板里带 hidden、触发器在模板里是 <a href> —— 这两件事都由本脚本改写。
 *
 * 为什么要把链接换成 <button>：弹出一块面板的东西是按钮不是链接，
 * 读屏软件据此播报「折叠/展开」，键盘用户按 Enter 与 Space 都能开。
 * 但没有脚本时它必须是链接（不然点了什么也不会发生），于是这次替换只能由脚本自己做
 * —— 与 auth.js 注入「显示密码」按钮是同一种处置。
 */
(function () {
  "use strict";

  /* 收起动画的时长，与 theme.css 里 .account-menu.is-closing 的 90ms 一致。
   * 两边分开写是有意的：CSS 负责画，JS 负责什么时候把元素真正藏起来，
   * 而「真正藏起来」这一步没有动画可依附，只能靠定时器（与 nav.js 同一处理）。 */
  var CLOSE_MS = 90;

  function prefersReducedMotion() {
    return (
      typeof window.matchMedia === "function" &&
      window.matchMedia("(prefers-reduced-motion: reduce)").matches
    );
  }

  function init() {
    var wrap = document.querySelector(".site-account");
    if (!wrap) {
      return;
    }
    var link = wrap.querySelector("[data-account-trigger]");
    var menu = document.getElementById("site-account-menu");
    // 匿名访客的页眉上是「登录 / 注册」两个链接，这里什么都没有，直接退出。
    if (!link || !menu) {
      return;
    }

    /*
     * 就地换成按钮。
     *
     * 用 innerHTML 搬运内容：里面是头像图或一个首字 span，都是本模板自己渲染的，
     * 没有事件绑定也没有外部输入（显示名由 html/template 转义过）。
     * 逐个 appendChild 搬子节点得到的是同一结果，多写五行没有换来任何东西。
     */
    var button = document.createElement("button");
    button.type = "button";
    button.className = link.className;
    button.innerHTML = link.innerHTML;
    var label = link.getAttribute("aria-label");
    if (label) {
      button.setAttribute("aria-label", label);
    }
    button.setAttribute("aria-expanded", "false");
    button.setAttribute("aria-controls", menu.id);
    link.parentNode.replaceChild(button, link);

    var timer = 0;

    function cancelPending() {
      if (timer) {
        window.clearTimeout(timer);
        timer = 0;
      }
      menu.classList.remove("is-closing");
    }

    function open() {
      cancelPending();
      menu.hidden = false;
      button.setAttribute("aria-expanded", "true");
    }

    function close() {
      if (menu.hidden) {
        return;
      }
      cancelPending();
      // aria-expanded 立刻置回 false：它说的是「这块内容展开了吗」，
      // 而从按下到真正消失的这 90ms 里，答案已经是「不再展开」了。
      button.setAttribute("aria-expanded", "false");

      if (prefersReducedMotion()) {
        menu.hidden = true;
        return;
      }
      menu.classList.add("is-closing");
      timer = window.setTimeout(function () {
        menu.hidden = true;
        menu.classList.remove("is-closing");
        timer = 0;
      }, CLOSE_MS);
    }

    button.addEventListener("click", function () {
      if (menu.hidden) {
        open();
      } else {
        close();
      }
    });

    /* 菜单里点了任何一项就收起。
     * 站内链接会整页跳走，收不收都看不见；「管理后台」开的是新窗口，
     * 回到这个标签页时菜单还摊着——那才是这一句要管的情况。 */
    menu.addEventListener("click", function (event) {
      if (event.target.closest("a, button")) {
        close();
      }
    });

    // 点页面其他位置收起。用 pointerdown 而不是 click：与 nav.js 一致，
    // 按下就收起，不必等到抬手。
    document.addEventListener("pointerdown", function (event) {
      if (!wrap.contains(event.target)) {
        close();
      }
    });

    // Esc 收起，并把焦点还给头像——焦点停在已经不可见的菜单项里，
    // 键盘用户会彻底迷失位置。
    document.addEventListener("keydown", function (event) {
      if (event.key !== "Escape" || menu.hidden) {
        return;
      }
      event.preventDefault();
      close();
      button.focus();
    });

    // 焦点移出整块账户区时收起。relatedTarget 为空表示焦点去了浏览器界面，
    // 那也算移出。
    wrap.addEventListener("focusout", function (event) {
      if (event.relatedTarget && wrap.contains(event.relatedTarget)) {
        return;
      }
      close();
    });
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
})();
