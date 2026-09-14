/*
 * 墨 Ink 的二级菜单交互：桌面悬停展开 / 点击切换 / 同时只开一个 / Esc 收起。
 *
 * 不依赖任何库，也**不进 motion.js**：主题设置里的动效总闸关掉之后，
 * 菜单仍然必须能展开——导航是功能不是装饰（与 code.js 独立于动效开关是同一条理由）。
 *
 * 渐进增强的方向与 comments.js 相反：那边是「没有 JS 就没有评论区表单」，
 * 这边是「没有 JS 时二级菜单就是一列摊开的链接」。所以模板里刻意**不写 hidden**，
 * 收起这件事完全由这段脚本在初始化时完成。
 *
 * 两条展开路径，判据都是「这次交互到底是怎么来的」，不猜设备类型：
 *   - 指针是鼠标、且宽到足以把子菜单摆成绝对定位的下拉时，进入一级项即展开（见 HOVER_CLOSE_MS）；
 *   - 其余情况（触屏、笔、窄屏、键盘）靠点击 / Enter / Space 切换。
 * 用 pointerType 而不是媒体查询判断指针，是因为带触摸屏的笔记本会同时报两套能力，
 * 而 pointerType 说的正是当下这一下是手指还是鼠标。
 */
(function () {
  "use strict";

  // 收起动画的时长，与 theme.css 里的 90ms 保持一致。
  // 两边分开写是有意的：CSS 负责画，JS 负责什么时候把元素真正藏起来，
  // 而「真正藏起来」这一步没有动画可依附，只能靠定时器。
  var CLOSE_MS = 90;

  /*
   * 鼠标移出后延迟多久才收起。
   *
   * 这段延迟不是手感问题，是必需的：下拉的顶边落在页眉那条分隔线上，
   * 与一级项之间隔着页眉的下内边距（--s6）。指针从一级项往下走的途中，
   * 既不在 li 上也不在子菜单上——若不延迟，菜单会在半路上关掉，然后因为
   * 指针进了子菜单又重新打开，看上去就是一闪。
   * 180ms 够跨过那段空隙，又短到不让人觉得「移开了还不收」。
   */
  var HOVER_CLOSE_MS = 180;

  // 桌面下拉的断点，与 theme.css 里那条 @media (min-width: 641px) 一致。
  var DESKTOP_QUERY = "(min-width: 641px)";

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
      var item = { btn: btn, sub: sub, timer: 0, hoverTimer: 0, li: btn.closest("li") };
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

    /*
     * 悬停的收起定时器与上面那个是两回事，不能共用一个字段：
     * 前者等的是「指针还会不会回来」，后者等的是「淡出动画跑完了没有」，
     * 同一时刻两者可能都在跑（移出后立刻点别处），合成一个就会丢掉其中一次收起。
     */
    function cancelHoverClose(item) {
      if (item.hoverTimer) {
        window.clearTimeout(item.hoverTimer);
        item.hoverTimer = 0;
      }
    }

    function scheduleHoverClose(item) {
      cancelHoverClose(item);
      item.hoverTimer = window.setTimeout(function () {
        item.hoverTimer = 0;
        close(item);
      }, HOVER_CLOSE_MS);
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
      cancelHoverClose(item);
      item.sub.hidden = false;
      item.btn.setAttribute("aria-expanded", "true");
    }

    function closeAll(except) {
      items.forEach(function (item) {
        if (item !== except) {
          cancelHoverClose(item);
          close(item);
        }
      });
    }

    /*
     * 桌面悬停。两个判据缺一不可：
     *   pointerType 是 "mouse"——手指没有「悬停」这回事，触摸设备会在按下前先发一次
     *   pointerenter，不挡掉的话菜单会抢在 click 之前自己弹开，而 click 又把它关掉；
     *   宽度够——窄屏上子菜单是内嵌的（不是绝对定位的下拉），悬停展开会把页眉顶高，
     *   鼠标只是从页眉上掠过就抖一下。窄屏有鼠标的场合走点击那条路。
     * 每次事件都重新问一遍媒体查询：窗口是可以被拉宽的，初始化时问一次会记错。
     */
    function hoverApplies(event) {
      if (event.pointerType !== "mouse") {
        return false;
      }
      return (
        typeof window.matchMedia !== "function" ||
        window.matchMedia(DESKTOP_QUERY).matches
      );
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

      if (!item.li) {
        return;
      }
      // 绑在 li 上而不是按钮上：子菜单是 li 的后代，指针从按钮走到子项时
      // li 的 pointerleave 不会触发，菜单因此不会在中途关掉。
      item.li.addEventListener("pointerenter", function (event) {
        if (!hoverApplies(event)) {
          return;
        }
        closeAll(item);
        open(item);
      });
      item.li.addEventListener("pointerleave", function (event) {
        if (!hoverApplies(event)) {
          return;
        }
        scheduleHoverClose(item);
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
      // 先把悬停的收起定时器清掉：光标就停在触发按钮上时按 Esc，
      // 之后指针不动，那个定时器到点会再调一次 close——虽然此时 close 已经无操作，
      // 但留着它意味着「用户已经明确手关了，脚本还记着要再关一次」。
      cancelHoverClose(current);
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
