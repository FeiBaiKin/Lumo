/*
 * 墨 Ink 的账户弹窗：页眉的「登录 / 注册」把对应页面上的表单取过来就地打开。
 *
 * 不依赖任何库，也**不进 motion.js**：登录是功能不是装饰（与 nav.js / code.js 同一条理由）。
 *
 * 渐进增强的方向与 nav.js 相反：那边是「没有 JS 时摊开成可点的链接」，
 * 这边是「没有 JS 时那两个入口就是两个普通链接」，点下去走的是同一个页面，
 * 只是整页跳转。所以入口一直是 <a href="/login">，这段脚本做的只是拦截点击。
 *
 * 为什么取页面而不是在弹窗里再写一份表单：
 *   表单里的 CSRF 令牌、回填值、逐字段错误、蜜罐字段都由服务端产出，
 *   抄一份到主题里就意味着两处要同时改，而漏改的那一次表现为「线上登录偶发失败」。
 *   代价是打开时多一次请求，而这几个页面本来就不可缓存，这个代价本来就付得起。
 *
 * 为什么不在每个页面里就把令牌渲染好：那会把逐访问者不同的令牌写进 HTML，
 * 匿名页从此不可被共享缓存，CDN 还可能把一个访问者的令牌发给另一个。
 */
(function () {
  "use strict";

  // 弹窗里能装的三张表单。键与模板里的 data-auth-tab 一致。
  var ROUTES = {
    login: "/login",
    register: "/register",
    forgot: "/forgot-password",
  };

  /* 取登录面板的地址。带上 next：用户是在读某一篇文章时点的「登录」，
   * 登完把他扔回首页，等于把他正在读的东西弄丢了。
   * 服务端会用 safeNext 过一遍（只收站内单斜杠开头的路径），这里不重复那套判断。 */
  function loginURL() {
    var here = window.location.pathname + window.location.search;
    return ROUTES.login + "?next=" + encodeURIComponent(here);
  }

  function panelURL(name) {
    return name === "login" ? loginURL() : ROUTES[name];
  }

  function init() {
    var dialog = document.getElementById("site-auth");
    if (!dialog || typeof dialog.showModal !== "function") {
      return;
    }
    var body = dialog.querySelector("[data-auth-body]");
    if (!body) {
      return;
    }

    var tabs = Array.prototype.slice.call(
      dialog.querySelectorAll("[data-auth-tab]"),
    );

    /* 「顶边落在页眉那条分隔线上、右边缘对齐正文栏」只能量出来：
     * 页眉高度取决于站名会不会换行、导航有几项，写死一个数值在某些宽度上就会错位。
     * 固定定位的参照物是视口，所以这里用相对视口的坐标，并按可视宽度减掉滚动条。 */
    function place() {
      var header = document.querySelector(".site-header");
      if (!header) {
        return;
      }
      var wrap = header.querySelector(".wrap") || header;
      var viewport = document.documentElement.clientWidth;
      // 页眉被滚出视口时贴着顶边，而不是跑到屏幕外
      var top = Math.max(header.getBoundingClientRect().bottom, 0);
      var gutter = Math.max(viewport - wrap.getBoundingClientRect().right, 0);
      dialog.style.setProperty("--auth-top", top + "px");
      dialog.style.setProperty("--auth-gutter", gutter + "px");
    }

    function markActive(name) {
      tabs.forEach(function (link) {
        if (link.dataset.authTab === name) {
          link.setAttribute("aria-current", "true");
        } else {
          link.removeAttribute("aria-current");
        }
      });
    }

    /* 把一份完整页面里的那块面板取出来放进弹窗。
     * 结构对不上时返回 false，调用方退回整页跳转——主题被第三方改过、
     * 或者账户模块换了模板名，都不该表现为「点了没反应」。 */
    function takePanel(html) {
      var doc = new DOMParser().parseFromString(html, "text/html");
      var panel = doc.querySelector("[data-auth-panel]");
      if (!panel) {
        return false;
      }
      body.replaceChildren(document.importNode(panel, true));
      focusFirstField();
      return true;
    }

    function focusFirstField() {
      var field = body.querySelector("input:not([type=hidden])");
      if (field) {
        field.focus();
      }
    }

    function loading() {
      var note = document.createElement("p");
      note.className = "auth-dialog-loading";
      note.textContent = "载入中";
      body.replaceChildren(note);
    }

    function open(name) {
      var url = panelURL(name);
      if (!url) {
        return;
      }
      markActive(name);
      if (!dialog.open) {
        place();
        dialog.showModal();
      }
      loading();

      fetch(url, { credentials: "same-origin" })
        .then(function (response) {
          if (response.redirected) {
            // 已被登出的账号拿到 302（见账户模块的 getLogin）：跟着走。
            window.location.assign(response.url);
            return null;
          }
          if (!response.ok && response.status >= 500) {
            return null;
          }
          return response.text();
        })
        .then(function (html) {
          if (html === null) {
            // 服务端出问题或页面结构对不上：整页跳转，让用户看到真正的报错页。
            window.location.assign(url);
            return;
          }
          if (!takePanel(html)) {
            window.location.assign(url);
          }
        })
        .catch(function () {
          window.location.assign(url);
        });
    }

    /*
     * 把表单编码成 application/x-www-form-urlencoded。
     *
     * **不要用 new FormData(form) 直接当 fetch 的 body**：那发出去的是
     * multipart/form-data，而服务端按 urlencoded 解析——net/http 的
     * Request.ParseForm 遇到 multipart 时会把 PostForm 置成一张**空表**（不是 nil），
     * 于是 PostFormValue 不再回头去解析 multipart 正文，令牌读出来是空串，
     * CSRF 校验必然失败。
     *
     * 这不是理论上的差异：表现是每次提交都得到「表单已过期，请重新提交」，
     * 一句与真正原因毫无关系的提示，而服务端日志里只有一行 400（走查时实测到过）。
     *
     * 用 URLSearchParams 而不是 FormData，这段请求与没有 JS 时浏览器原生发出的
     * 就完全一致——这正是这个渐进增强方案该有的样子。
     * 这几张表单里没有文件字段，故不必考虑 multipart 唯一的正当用途。
     */
    function encode(form) {
      return new URLSearchParams(new FormData(form));
    }

    /* 提交走 fetch 而不是让它原生提交：原生提交会离开当前页面，
     * 于是「密码填错了」这个人就被甩到一张独立的登录页上，
     * 而弹窗的全部意义就是他不用离开。 */
    function submit(form) {
      var button = form.querySelector("button[type=submit], button:not([type])");
      if (button) {
        button.disabled = true;
      }

      fetch(form.getAttribute("action") || window.location.pathname, {
        method: "POST",
        body: encode(form),
        credentials: "same-origin",
      })
        .then(function (response) {
          /* 成功那一路服务端是 302，fetch 会把 response.redirected 置真；
           * 失败那一路是原地渲染的 200 + 一整页 HTML（或服务端自己的 HTML 错误页）。
           * 靠「有没有被重定向」区分这两者，比去猜状态码稳。 */
          if (response.redirected) {
            window.location.assign(response.url);
            return null;
          }
          var type = response.headers.get("content-type") || "";
          if (type.indexOf("text/html") < 0) {
            return null;
          }
          return response.text();
        })
        .then(function (html) {
          if (html === null) {
            return;
          }
          // 提交失败：换上新面板，错误与回填值都在里面，光标落到第一个出错字段上。
          if (!takePanel(html)) {
            form.submit();
          }
        })
        .catch(function () {
          /* 网络层失败（离线、请求被中止）。退回原生提交，
           * 让浏览器给出它自己那份错误页——比在这里假装成功或什么都不说都诚实。 */
          form.submit();
        });
    }

    /* 一个委托监听管住所有入口：页眉的「登录 / 注册」、弹窗自己的标签栏，
     * 以及面板里那几个出口链接（「忘记密码」「还没有账户？注册」）。
     * 它们全都是真的 <a href>，所以没有 JS 时各自都能独立工作。 */
    document.addEventListener("click", function (event) {
      var link = event.target.closest ? event.target.closest("a[href]") : null;
      if (!link) {
        return;
      }
      /* 新标签页 / 新窗口 / 中键：那是用户明确要求「另开一个」，
       * 拦下来换成原地换内容正好违背他的意思。 */
      if (
        event.defaultPrevented ||
        event.button !== 0 ||
        event.metaKey ||
        event.ctrlKey ||
        event.shiftKey ||
        event.altKey
      ) {
        return;
      }

      var inDialog = dialog.contains(link);
      var name = link.dataset.authTab;
      if (!name) {
        if (!inDialog) {
          return;
        }
        // 面板里的出口链接：按它指向的路由换成对应的标签
        name = nameOf(link.getAttribute("href"));
        if (!name) {
          return;
        }
      }
      event.preventDefault();
      open(name);
    });

    function nameOf(href) {
      for (var key in ROUTES) {
        if (Object.prototype.hasOwnProperty.call(ROUTES, key) && ROUTES[key] === href) {
          return key;
        }
      }
      return "";
    }

    body.addEventListener("submit", function (event) {
      if (!event.target || event.target.tagName !== "FORM") {
        return;
      }
      event.preventDefault();
      submit(event.target);
    });

    /* 关闭就把内容丢掉：面板里的令牌是一次性的，留着它，
     * 下次打开用的还是上一次那枚——服务端已经换了新令牌，提交必被挡下。 */
    dialog.addEventListener("close", function () {
      body.replaceChildren();
      markActive("");
    });

    window.addEventListener("resize", function () {
      if (dialog.open) {
        place();
      }
    });
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
})();
