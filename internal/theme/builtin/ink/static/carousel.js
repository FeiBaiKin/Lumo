/*
 * 首页轮播。
 *
 * 用滚动吸附（scroll-snap）做载体，而不是自己算 transform：触屏上的左右滑动、
 * 触控板的横向惯性、键盘的 Home/End 全都是浏览器送的，自己实现一遍必然漏掉几个。
 * 这段脚本只补三件事：按钮翻页、圆点导航、自动播放。
 *
 * 没有这段脚本时：轮播退化成一条可以手动横滑的图文列表，第一张照样在首屏。
 * 所以它**不受** appearance.motion 管辖 —— 关掉动效总闸只是不再平滑滚动。
 */
(function () {
  var root = document.querySelector("[data-carousel-root]");
  if (!root) return;

  var track = root.querySelector("[data-carousel-track]");
  var slides = track ? Array.prototype.slice.call(track.children) : [];
  if (slides.length < 2) return;

  var dots = root.querySelector("[data-carousel-dots]");
  var prev = root.querySelector("[data-carousel-prev]");
  var next = root.querySelector("[data-carousel-next]");
  var reduceMotion = window.matchMedia("(prefers-reduced-motion: reduce)");
  var index = 0;
  var timer = null;

  function width() {
    return track.clientWidth || 1;
  }

  function goTo(target, smooth) {
    index = (target + slides.length) % slides.length;
    track.scrollTo({
      left: index * width(),
      behavior: smooth && !reduceMotion.matches ? "smooth" : "auto",
    });
    paint();
  }

  function paint() {
    if (!dots) return;
    Array.prototype.forEach.call(dots.children, function (dot, i) {
      if (i === index) {
        dot.setAttribute("aria-current", "true");
      } else {
        dot.removeAttribute("aria-current");
      }
    });
  }

  if (dots) {
    slides.forEach(function (slide, i) {
      var dot = document.createElement("button");
      dot.type = "button";
      dot.className = "carousel-dot";
      dot.setAttribute("aria-label", "第 " + (i + 1) + " 张");
      dot.addEventListener("click", function () {
        goTo(i, true);
        stop();
      });
      dots.appendChild(dot);
    });
  }

  if (prev) {
    prev.addEventListener("click", function () {
      goTo(index - 1, true);
      stop();
    });
  }
  if (next) {
    next.addEventListener("click", function () {
      goTo(index + 1, true);
      stop();
    });
  }

  // 手指滑完之后把圆点对上：scroll 事件是唯一能同时覆盖滑动、滚轮与按钮的时机。
  var ticking = false;
  track.addEventListener(
    "scroll",
    function () {
      if (ticking) return;
      ticking = true;
      window.requestAnimationFrame(function () {
        ticking = false;
        var current = Math.round(track.scrollLeft / width());
        if (current !== index && current >= 0 && current < slides.length) {
          index = current;
          paint();
        }
      });
    },
    { passive: true },
  );

  function start() {
    if (timer || reduceMotion.matches) return;
    timer = window.setInterval(function () {
      goTo(index + 1, true);
    }, 6000);
  }

  function stop() {
    if (!timer) return;
    window.clearInterval(timer);
    timer = null;
  }

  // 鼠标停在上面、键盘聚焦进来、标签页切走时都不自动翻页：
  // 正在读的那一张被人从眼皮底下换掉，是轮播最招人烦的地方。
  root.addEventListener("mouseenter", stop);
  root.addEventListener("focusin", stop);
  document.addEventListener("visibilitychange", function () {
    if (document.hidden) {
      stop();
    } else {
      start();
    }
  });

  paint();
  start();
})();
