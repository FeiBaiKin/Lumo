/*
 * 墨 Ink 的配色切换：亮 / 暗 / 跟随系统，三态循环。
 *
 * 三件事决定了这个脚本长这样：
 *
 *   1. **偏好存在本机**（localStorage），所以它是访客的，不是站长的。
 *      站长在主题设置里选的那一档是**默认值**，访客点过之后以访客为准 ——
 *      在别人的站点上被强制锁进深色，与弹窗广告是同一种冒犯。
 *   2. **首帧之前就要生效**，否则每次翻页都会亮一下再变暗。那一步在 base.html 的
 *      <head> 里内联（外部脚本与 defer 都晚于首次绘制），这里只负责按钮本身。
 *   3. **不依赖任何库、也不进 motion.js**：切换配色是功能不是装饰。
 *      站长的动效总闸管的是「切的时候要不要那一圈扩散动画」，不是「能不能切」。
 *
 * 没有这段脚本时：按钮一直是 hidden（模板里给的），配色按站长设定走，页面完整可用。
 * 一个按不动的控件比没有这个控件更糟 —— 与 auth.js 注入「显示密码」同一条理由。
 */
(function () {
  "use strict";

  /* 存储键。名字带站点前缀而不是通用名：同一个域下可能还跑着别的东西。 */
  var KEY = "lumo-scheme";

  /* 循环顺序。三态而不是两态：「跟随系统」是访客把选择权还回去的唯一出口，
   * 少了它，访客点过一次之后就再也回不到「跟系统走」那个状态了。 */
  var ORDER = ["auto", "light", "dark"];

  var LABELS = {
    auto: "跟随系统",
    light: "浅色",
    dark: "深色",
  };

  /* 圆形揭示的时长。与 theme.css 里 --t-fast（130ms）不是一回事：
   * 那一条管的是悬停这类微交互，整页换色需要更长才能看清「从哪扩散出来的」。
   * 480ms 与 MDN 给的 view transition 示例同量级。 */
  var REVEAL_MS = 480;

  function readPref() {
    try {
      return window.localStorage.getItem(KEY);
    } catch (e) {
      /* 隐私模式下 localStorage 会抛异常。那就等于没存过。 */
      return null;
    }
  }

  function writePref(value) {
    try {
      window.localStorage.setItem(KEY, value);
    } catch (e) {
      /* 存不下就只影响下次进站，本次仍然照切。 */
    }
  }

  function prefersReducedMotion() {
    return (
      typeof window.matchMedia === "function" &&
      window.matchMedia("(prefers-reduced-motion: reduce)").matches
    );
  }

  /* 站长关掉了「页面动效」。属性由服务端渲染在 <html> 上（见 base.html）。 */
  function motionOff() {
    return document.documentElement.getAttribute("data-motion") === "off";
  }

  function motionAllowed() {
    return !motionOff() && !prefersReducedMotion();
  }

  /* 把偏好落到 <html> 上。CSS 认这两个属性：
   * data-scheme 决定用哪一套颜色变量，data-scheme-pref 决定显示哪一枚图标。 */
  function apply(pref) {
    var root = document.documentElement;
    root.setAttribute("data-scheme", pref);
    root.setAttribute("data-scheme-pref", pref);
  }

  function init() {
    var button = document.querySelector("[data-scheme-toggle]");
    if (!button) {
      return;
    }

    /* 起点 = 存过的偏好，没存过就按「现在生效的那一档」起算：
     * 站长把配色钉死在深色时，按钮显示的就是月亮（现状），而不是「跟随系统」。 */
    var root = document.documentElement;
    var current = readPref();
    if (ORDER.indexOf(current) < 0) {
      current = root.getAttribute("data-scheme");
      if (ORDER.indexOf(current) < 0) {
        current = "auto";
      }
    }
    root.setAttribute("data-scheme-pref", current);

    function describe(pref) {
      return "切换配色，当前：" + LABELS[pref];
    }

    function relabel(pref) {
      /* 无障碍：按钮的名字要说清动作与现状。图标自己带 aria-hidden，
       * 三枚图标是一堆图形，读屏软件读不出「现在是深色」。 */
      button.setAttribute("aria-label", describe(pref));
      button.setAttribute("title", describe(pref));
    }

    relabel(current);
    button.hidden = false;

    /* 上一段扩散动画还在跑吗。见 switchTo 里的说明。 */
    var running = false;

    /* 演示用的扩散中心：从按钮自己身上长出来。
     * 无点击坐标可用时退回屏幕中心（这里必然有按钮，所以那个分支只是兜底）。 */
    function revealOrigin() {
      var box = button.getBoundingClientRect();
      var x = box.left + box.width / 2;
      var y = box.top + box.height / 2;
      if (!isFinite(x) || !isFinite(y)) {
        x = window.innerWidth / 2;
        y = window.innerHeight / 2;
      }
      return { x: x, y: y };
    }

    /* 有 View Transitions 就从按钮位置扩散一个圆，没有就直接换。
     * 直接换也是完整的：只是少了一层「变化从哪来」的交代。 */
    function switchTo(pref) {
      if (
        typeof document.startViewTransition !== "function" ||
        !motionAllowed() ||
        /* 上一次扩散还没走完：直接换。连点两下时排队两段整页动画只会互相打架。 */
        running
      ) {
        apply(pref);
        return;
      }

      var origin = revealOrigin();
      /* 半径取到最远的那个角：短了会在边角留下一块没被覆盖的旧配色。 */
      var radius = Math.hypot(
        Math.max(origin.x, window.innerWidth - origin.x),
        Math.max(origin.y, window.innerHeight - origin.y),
      );

      running = true;

      var transition = document.startViewTransition(function () {
        apply(pref);
      });

      /* ready 在过渡被跳过时会 reject（标签页不可见、同一时刻已有一次过渡）。
       * 那不是错误，只是没动画 —— 静默忽略，且**不做任何补偿**：
       * apply 已经在上面跑过了，页面该变的一定变了。 */
      transition.ready
        .then(function () {
          document.documentElement.animate(
            {
              clipPath: [
                "circle(0px at " + origin.x + "px " + origin.y + "px)",
                "circle(" + radius + "px at " + origin.x + "px " + origin.y + "px)",
              ],
            },
            {
              duration: REVEAL_MS,
              easing: "ease-in",
              pseudoElement: "::view-transition-new(root)",
            },
          );
        })
        .catch(function () {});

      transition.finished
        .catch(function () {})
        .then(function () {
          running = false;
        });
    }

    button.addEventListener("click", function () {
      var next = ORDER[(ORDER.indexOf(current) + 1) % ORDER.length];
      current = next;
      writePref(next);
      relabel(next);
      switchTo(next);
    });
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
})();
