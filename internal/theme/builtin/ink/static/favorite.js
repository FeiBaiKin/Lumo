/*
 * 墨 Ink 的收藏按钮：按下即收藏，再按取消。
 *
 * 走 Public 平面的 REST 接口（PUT / DELETE /api/v1/public/posts/{id}/favorite），
 * 与评论提交同一条路子。按钮在 HTML 里带 hidden，由本脚本放出来：
 * 没有 JS 时不该出现一个点了没反应的按钮（与 comments.js 同一条处置）。
 *
 * 初态（已收藏与否、收藏数）由服务端渲染在按钮上，本脚本不再问一次接口——
 * 那会让按钮在页面加载完之后闪一下，而这个状态服务端本来就知道。
 *
 * 只有已登录访客的页面上才有这个按钮；匿名访客看到的是一个指向登录页的链接
 * （见 partials/favorite.html），本脚本找不到 [data-favorite]，什么都不做。
 */
(function () {
  "use strict";

  var button = document.querySelector("[data-favorite]");
  if (!button) return;

  button.hidden = false;

  var label = button.querySelector("[data-favorite-label]");
  var counter = document.querySelector("[data-favorite-count]");
  var postId = button.dataset.postId;

  /*
   * 当前会话的 CSRF 令牌（双提交模式）。
   *
   * 这段与 comments.js 里的那份**一模一样**，是有意抄的：主题的脚本各自是一个
   * 自足的 IIFE，没有模块系统；为这十来行开一个全局对象，会给每个页面引入
   * 「哪个脚本先加载」的顺序依赖，而那类 bug 比重复的十行难查得多。
   *
   * Public 平面同样校验 CSRF：已登录用户带着会话 Cookie 发请求，
   * 没有这个头会被 403 挡下。Cookie 名在 HTTPS 下带 __Host- 前缀，两种都要找。
   */
  function csrfToken() {
    var names = ["lumo_csrf", "__Host-lumo_csrf"];
    var jar = document.cookie || "";
    for (var i = 0; i < names.length; i++) {
      var parts = jar.split(";");
      for (var j = 0; j < parts.length; j++) {
        var item = parts[j].trim();
        if (item.indexOf(names[i] + "=") === 0) {
          return decodeURIComponent(item.slice(names[i].length + 1));
        }
      }
    }
    return "";
  }

  /* 把服务端回来的最终状态画到按钮上。
   * 三处同时改：aria-pressed 给读屏软件，文字给眼睛，数字给所有人。 */
  function render(state) {
    button.setAttribute("aria-pressed", state.favorited ? "true" : "false");
    if (label) {
      label.textContent = state.favorited ? "已收藏" : "收藏";
    }
    if (counter) {
      counter.textContent = state.count + " 人收藏";
      counter.hidden = state.count <= 0;
    }
  }

  button.addEventListener("click", function () {
    var on = button.getAttribute("aria-pressed") === "true";
    var headers = {};
    var csrf = csrfToken();
    if (csrf) headers["X-CSRF-Token"] = csrf;

    button.disabled = true;
    fetch("/api/v1/public/posts/" + encodeURIComponent(postId) + "/favorite", {
      // 收藏是**置位**不是追加：同一个人对同一篇至多一条，故用 PUT / DELETE。
      // 两个动词都幂等，网络抖动重发一次不会多出一条收藏，也不会多减一次。
      method: on ? "DELETE" : "PUT",
      headers: headers,
      credentials: "same-origin",
    })
      .then(function (res) {
        if (res.status === 401) {
          /* 会话在这一页开着的时候过期了。按钮本来就只渲染给已登录用户，
           * 所以这不是「请先登录」而是「你被登出了」——把人送去登录页并带上回跳地址，
           * 比在按钮旁边写一行红字有用得多。 */
          var here = window.location.pathname + window.location.search;
          window.location.assign("/login?next=" + encodeURIComponent(here));
          return null;
        }
        if (!res.ok) return null;
        return res.json();
      })
      .then(function (state) {
        if (state) render(state);
      })
      .catch(function () {
        /* 网络层失败：什么都不改。按钮停在原来的状态上是诚实的——
         * 先把它翻过去再回滚，会让人以为自己点错了。 */
      })
      .finally(function () {
        button.disabled = false;
      });
  });
})();
