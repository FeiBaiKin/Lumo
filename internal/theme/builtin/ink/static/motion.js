/*
 * 墨 Ink 的动效（阶段 8）。GSAP + ScrollTrigger + Lenis，全部自托管在 vendor/ 下。
 *
 * 只做三件事，且每一件都能在没有 JS 时退化成「内容原样可见」：
 *   1. 页面入场：一次编排 —— 标题区、元信息栏、列表项依次淡入。整站只有这一处非用户触发的动效。
 *   2. 正文里的图、表、代码块进入视口时淡入。只动这些，不动文字：文字是给读者与爬虫的，
 *      不该有任何一帧是隐形的。
 *   3. 文章页顶部一条 2px 的印色阅读进度线。
 *
 * prefers-reduced-motion 为 reduce 时整段脚本直接返回：所有 gsap.from 都不执行，
 * 元素保持 CSS 里的最终状态，Lenis 也不接管滚动。
 */
(function () {
  "use strict";

  if (!window.gsap || !window.ScrollTrigger) return;
  if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) return;

  var gsap = window.gsap;
  gsap.registerPlugin(window.ScrollTrigger);

  // Lenis：平滑滚动。anchors 让目录里的 #锚点 也走平滑滚动。
  if (window.Lenis) {
    var lenis = new window.Lenis({ autoRaf: true, lerp: 0.12, anchors: true });
    lenis.on("scroll", window.ScrollTrigger.update);

    /*
     * 弹窗打开时停掉平滑滚动。
     *
     * 账户弹窗（auth.js）打开时会给 <html> 挂上 auth-open，CSS 那边是
     * overflow: hidden——但那管不住 Lenis：它自己监听滚轮、自己调 scrollTo，
     * overflow 对程序化滚动不起作用。人在填登录表单，身后的文章却跟着滚轮走，
     * 是这一层最容易被忽略的破绽。
     *
     * 由动效这边去观察那个类，而不是让 auth.js 来调 Lenis：
     * 登录是功能，不该依赖动效层存在与否（这也是 auth.js 一直不进本文件的理由）。
     * 反过来，动效层知道「有一个约定的锁滚动类名」是合理的。
     */
    new MutationObserver(function () {
      if (document.documentElement.classList.contains("auth-open")) {
        lenis.stop();
      } else {
        lenis.start();
      }
    }).observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["class"],
    });
  }

  // 1. 入场编排。y 位移只有 8–14px：读起来是淡入，不是滑入。
  var timeline = gsap.timeline({ defaults: { ease: "power2.out" } });
  var title = document.querySelectorAll("[data-motion='title'] > *");
  var aside = document.querySelectorAll("[data-motion='aside']");
  var items = document.querySelectorAll("[data-motion='list'] > li");
  if (title.length) {
    timeline.from(title, { opacity: 0, y: 14, duration: 0.5, stagger: 0.08 });
  }
  if (aside.length) {
    timeline.from(aside, { opacity: 0, y: 8, duration: 0.35 }, "-=0.25");
  }
  if (items.length) {
    // 长列表把每项的错落压到 0.03s，总时长不会拖沓
    timeline.from(
      items,
      { opacity: 0, y: 10, duration: 0.35, stagger: Math.min(0.04, 0.6 / items.length) },
      "-=0.2"
    );
  }

  // 2. 正文里的插图与表格。once：滚过一次就固定，往回滚不再重放。
  //    代码块的外壳就是 <figure class="code-block">，已被 .prose figure 选中；
  //    这里用 .prose > pre 只兜住没有外壳的裸 <pre>，否则内外两层各动一次，
  //    看起来是一段卡顿的双重淡入。
  gsap.utils
    .toArray(".prose figure, .prose > img, .prose table, .prose > pre")
    .forEach(function (el) {
      gsap.from(el, {
        opacity: 0,
        y: 12,
        duration: 0.4,
        ease: "power1.out",
        scrollTrigger: { trigger: el, start: "top 92%", once: true },
      });
    });

  // 3. 阅读进度。scrub 带一点惯性，线条不会随滚轮一格一格地跳。
  var bar = document.querySelector(".reading-progress");
  var article = document.querySelector(".article");
  if (bar && article) {
    gsap.to(bar, {
      scaleX: 1,
      ease: "none",
      scrollTrigger: {
        trigger: article,
        start: "top top",
        end: "bottom bottom",
        scrub: 0.3,
      },
    });
  }
})();
