/*
 * 文章目录。
 *
 * 从正文的 h2 / h3 现场生成，而不是让服务端再解析一遍已经渲染好的 HTML ——
 * 浏览器本来就要遍历这些节点，顺手做掉即可。
 *
 * 标题不足三个时整块保持隐藏（模板里的 hidden 不摘掉）：两三个标题的文章
 * 读者一眼就看完了，目录只是多占一块地方。
 *
 * 锚点跳转交给 Lenis（它在 motion.js 里用 anchors: true 建的），
 * 动效关掉时退化成浏览器原生的跳转；这里两种情况下都只需要 <a href="#id">。
 */
(function () {
  var nav = document.querySelector("[data-toc]");
  if (!nav) return;

  var list = nav.querySelector("[data-toc-list]");
  var prose = document.querySelector(".prose");
  if (!list || !prose) return;

  var headings = Array.prototype.slice
    .call(prose.querySelectorAll("h2, h3"))
    .filter(function (heading) {
      return heading.textContent.trim().length > 0;
    });
  if (headings.length < 3) return;

  // 重复的锚点不能用同一段文字：中文标题重复率不低（「小结」），
  // 一次访问里出现两个 #小结，链接就会全部跳到第一个。
  var used = Object.create(null);

  headings.forEach(function (heading, i) {
    var id = heading.id;
    if (!id) {
      var base =
        (heading.textContent.trim()
          .toLowerCase()
          .replace(/[^\w一-龥]+/g, "-")
          .replace(/^-+|-+$/g, "") || "section") + "-" + (i + 1);
      id = base;
      while (used[id]) {
        id = base + "-" + i;
      }
      heading.id = id;
    }
    used[id] = true;

    var item = document.createElement("li");
    item.className = heading.tagName === "H3" ? "toc-item level-3" : "toc-item";
    var link = document.createElement("a");
    link.href = "#" + id;
    link.textContent = heading.textContent.trim();
    item.appendChild(link);
    list.appendChild(item);
  });

  nav.hidden = false;

  if (!("IntersectionObserver" in window)) return;

  var links = Array.prototype.slice.call(list.querySelectorAll("a"));
  var byId = Object.create(null);
  links.forEach(function (link) {
    byId[link.getAttribute("href").slice(1)] = link;
  });

  /*
   * 用「最近一个越过视线上沿的标题」当当前项，而不是「正在视口里的标题」：
   * 后者在一屏放得下三个标题时会同时点亮三个。
   * rootMargin 的上边距留出页眉的高度，否则当前项总比视线慢半拍。
   */
  var observer = new IntersectionObserver(
    function (entries) {
      entries.forEach(function (entry) {
        if (!entry.isIntersecting) return;
        var link = byId[entry.target.id];
        if (!link) return;
        links.forEach(function (other) {
          other.removeAttribute("aria-current");
        });
        link.setAttribute("aria-current", "true");
      });
    },
    { rootMargin: "-80px 0px -70% 0px", threshold: 0 },
  );

  headings.forEach(function (heading) {
    observer.observe(heading);
  });
})();
