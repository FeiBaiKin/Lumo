/*
 * 墨 Ink 的二级菜单交互：点击展开 / 收起、同时只开一个、Esc 收起。
 *
 * 不依赖任何库，也**不进 motion.js**：主题设置里的动效总闸关掉之后，
 * 菜单仍然必须能展开——导航是功能不是装饰（与 code.js 独立于动效开关是同一条理由）。
 *
 * 渐进增强的方向与 comments.js 相反：那边是「没有 JS 就没有评论区表单」，
 * 这边是「没有 JS 时二级菜单就是一列摊开的链接」。所以模板里刻意**不写 hidden**，
 * 收起这件事完全由这段脚本在初始化时完成。
 */
(function () {
  "use strict";

  // 收起动画的时长，与 theme.css 里的 90ms 保持一致。
  // 两边分开写是有意的：CSS 负责画，JS 负责什么时候把元素真正藏起来，
  // 而「真正藏起来」这一步没有动画可依附，只能靠定时器。
  var CLOSE_MS = 90;

  function prefersReducedMotion() {
    return (
      typeof window.matchMedia === "function" &&
      window.matchMedia("(prefers-reduced-motion: reduce)").matches
    );
  }

  function init() {
    var nav = document.querySelector(".site-nav");
    if (!nav) {
      return;
    }

    var toggles = Array.prototype.slice.call(
      nav.querySelectorAll(".site-nav-toggle"),
    );
    if (!toggles.length) {
      return;
    }

    /*
     * 先给自己盖一个标记：CSS 靠它决定「二级菜单内嵌展开」还是「绝对定位的下拉」。
     *
     * 这一步不能省。没有它的话，桌面上的下拉样式在没有 JS 时照样生效——
     * 子菜单会**浮在正文之上**，把文章盖掉一块。那不是「降级成可点的链接」，
     * 而是一个看起来坏掉的页面。加上标记之后，没跑 JS 时它就是页眉里
     * 一列摊开的链接，把后面的内容往下推，正是这个功能之前的形态。
     */
    nav.dataset.nav = "js";

    // 把按钮与它控制的子菜单配成一对。aria-controls 是这条关系的权威来源，
    // 从 DOM 里再找一遍 `.site-nav-sub` 会得到顺序耦合的代码。
    var items = [];
    toggles.forEach(function (btn) {
      var sub = document.getElementById(btn.getAttribute("aria-controls"));
      if (!sub) {
        return;
      }
      var item = { btn: btn, sub: sub, timer: 0 };
      items.push(item);
      // 初始化：收起。模板里没写 hidden，摊开的那一列就是没有 JS 时的形态。
      sub.hidden = true;
      btn.setAttribute("aria-expanded", "false");
    });

    function cancelPending(item) {
      if (item.timer) {
        window.clearTimeout(item.timer);
        item.timer = 0;
      }
      item.sub.classList.remove("is-closing");
    }

    function close(item) {
      if (item.sub.hidden) {
        return;
      }
      cancelPending(item);
      // aria-expanded 立刻置回 false：它说的是「这块内容展开了吗」，
      // 而从按下到真正消失的这 90ms 里，答案已经是「不再展开」了。
      item.btn.setAttribute("aria-expanded", "false");

      var delay = prefersReducedMotion() ? 0 : CLOSE_MS;
      if (delay === 0) {
        item.sub.hidden = true;
        return;
      }
      item.sub.classList.add("is-closing");
      item.timer = window.setTimeout(function () {
        item.sub.hidden = true;
        item.sub.classList.remove("is-closing");
        item.timer = 0;
      }, delay);
    }

    function open(item) {
      cancelPending(item);
      item.sub.hidden = false;
      item.btn.setAttribute("aria-expanded", "true");
    }

    function closeAll(except) {
      items.forEach(function (item) {
        if (item !== except) {
          close(item);
        }
      });
    }

    function expanded() {
      for (var i = 0; i < items.length; i++) {
        if (items[i].btn.getAttribute("aria-expanded") === "true") {
          return items[i];
        }
      }
      return null;
    }

    items.forEach(function (item) {
      // button 天生响应 Enter 与 Space，这里只处理指针与触摸的点击，
      // 不为键盘再绑一套按键判断。
      item.btn.addEventListener("click", function () {
        if (item.sub.hidden) {
          // 同时只开一个：开新的先关旧的，否则展开几个之后页眉会变成一堵墙。
          closeAll(item);
          open(item);
        } else {
          close(item);
        }
      });
    });

    // 点页面其他位置收起。
    document.addEventListener("pointerdown", function (event) {
      if (!nav.contains(event.target)) {
        closeAll(null);
      }
    });

    // Esc 收起，并把焦点还给触发按钮——焦点停在已经不可见的子项里
    // 会让键盘用户彻底迷失位置。
    document.addEventListener("keydown", function (event) {
      if (event.key !== "Escape") {
        return;
      }
      var current = expanded();
      if (!current) {
        return;
      }
      event.preventDefault();
      close(current);
      current.btn.focus();
    });

    // 焦点移出整个导航时收起。relatedTarget 为空表示焦点去了浏览器界面，
    // 那也算移出。
    nav.addEventListener("focusout", function (event) {
      if (event.relatedTarget && nav.contains(event.relatedTarget)) {
        return;
      }
      closeAll(null);
    });
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
})();
