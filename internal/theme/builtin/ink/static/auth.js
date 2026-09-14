/*
 * 墨 Ink 的账户弹窗：页眉的「登录 / 注册」把对应页面上的表单取过来，在屏幕中央打开。
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

  /* 打开弹窗时锁住背后的滚动。<dialog> 只做到「背景不可交互」，
   * 滚轮照样能把背后的文章滚走——人在填表单，身后的页面自己动起来。 */
  function lockScroll(on) {
    document.documentElement.classList.toggle("auth-open", on);
  }

  /*
   * 「显示密码」那个按钮用的两个图标，取自 Lucide 的 eye 与 eye-off
   * ——与页眉的下拉箭头、弹窗的关闭键同一套（全站只用这一套图标）。
   * 只写路径，外框与描边属性由 CSS 的 .field-reveal svg 统一给。
   */
  var ICON_EYE =
    '<path d="M2.06 12.35a1 1 0 0 1 0-.7 10.75 10.75 0 0 1 19.88 0 1 1 0 0 1 0 .7 10.75 10.75 0 0 1-19.88 0"/>' +
    '<circle cx="12" cy="12" r="3"/>';
  var ICON_EYE_OFF =
    '<path d="M10.73 5.08a10.74 10.74 0 0 1 11.2 6.57 1 1 0 0 1 0 .7 10.75 10.75 0 0 1-1.44 2.49"/>' +
    '<path d="M14.08 14.16a3 3 0 0 1-4.24-4.24"/>' +
    '<path d="M17.48 17.5a10.75 10.75 0 0 1-15.42-5.15 1 1 0 0 1 0-.7 10.75 10.75 0 0 1 4.45-5.14"/>' +
    '<path d="m2 2 20 20"/>';

  function revealIcon(revealed) {
    return (
      '<svg viewBox="0 0 24 24" aria-hidden="true" focusable="false">' +
      (revealed ? ICON_EYE_OFF : ICON_EYE) +
      "</svg>"
    );
  }

  /*
   * 给密码框加「显示密码」。
   *
   * 由脚本注入而不是写在模板里：没有 JS 时那个按钮点了不会有任何反应，
   * 而一个不响应的控件比没有这个控件更糟。
   *
   * 位置在输入框右端内侧，与 Console 搜索框里的清除键是同一种做法。
   * 第二版把它放在标签那一行的右端、写成「显示 / 隐藏」两个字，
   * 那是「只有线与字」那条纪律的产物；纪律随设计一起作废了。
   *
   * 无障碍：按钮本身没有可见文字，故 aria-label 说明它是什么，
   * aria-pressed 说明它现在处于哪一态——图标按钮必须两样都给。
   */
  function enhancePasswords(root) {
    if (!root || !root.querySelectorAll) {
      return;
    }
    var inputs = root.querySelectorAll('input[type="password"]');
    Array.prototype.forEach.call(inputs, function (input) {
      var field = input.closest ? input.closest(".auth-field") : null;
      if (!field || field.querySelector(".field-reveal")) {
        return;
      }
      /* 用 <span> 而不是 <div> 包输入框：字段外层是 <p>，
       * 里面出现块级元素在 HTML 解析规则下会把那个 <p> 就地截断。
       * 这里是往已有 DOM 里插，不会重新解析，但留着一处不合法的结构
       * 迟早会在某个把它序列化再解析的地方（比如本文件里的 DOMParser）咬回来。 */
      var shell = document.createElement("span");
      shell.className = "field-control";
      input.parentNode.insertBefore(shell, input);
      shell.appendChild(input);

      var button = document.createElement("button");
      button.type = "button";
      button.className = "field-reveal";
      button.setAttribute("aria-label", "显示密码");
      button.setAttribute("aria-pressed", "false");
      button.innerHTML = revealIcon(false);
      button.addEventListener("click", function () {
        var reveal = input.type === "password";
        input.type = reveal ? "text" : "password";
        button.setAttribute("aria-pressed", reveal ? "true" : "false");
        button.innerHTML = revealIcon(reveal);
        // 焦点还给输入框：切换是为了继续打字，不是为了停在按钮上
        input.focus();
      });
      shell.appendChild(button);
    });
  }

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
    /* 独立的登录 / 注册 / 重置页上没有弹窗，但一样有密码框。
     * 放在 dialog 的判断之前，这几页才不会少掉这个按钮。 */
    enhancePasswords(document);

    var dialog = document.getElementById("site-auth");
    if (!dialog || typeof dialog.showModal !== "function") {
      return;
    }
    var body = dialog.querySelector("[data-auth-body]");
    if (!body) {
      return;
    }

    var titleNode = dialog.querySelector("[data-auth-title]");
    var closeButton = dialog.querySelector("[data-auth-close]");

    /* 把一份完整页面里的那块面板取出来放进弹窗。
     * 结构对不上时返回 false，调用方退回整页跳转——主题被第三方改过、
     * 或者账户模块换了模板名，都不该表现为「点了没反应」。 */
    function takePanel(html) {
      var doc = new DOMParser().parseFromString(html, "text/html");
      var panel = doc.querySelector("[data-auth-panel]");
      if (!panel) {
        return false;
      }
      var node = document.importNode(panel, true);
      body.replaceChildren(node);
      /* 标题取自面板自己声明的 data-auth-title（「登录」「创建账户」「重置密码」）。
       * 不从页面的 <h1> 里猜：那依赖 h1 在文档里的位置，
       * 而第三方主题完全可以把它挪走，挪走之后弹窗就会顶着上一次的标题。 */
      if (titleNode && node.dataset.authTitle) {
        titleNode.textContent = node.dataset.authTitle;
      }
      enhancePasswords(node);
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
      if (!dialog.open) {
        dialog.showModal();
        lockScroll(true);
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
      var idle = "";
      /* 提交要有反馈：只把按钮变灰，人不知道是「点上了」还是「点漏了」。
       * 文案由模板在 data-busy 上给（「正在登录」「正在创建」），
       * 这里不去拼「…中」——那会把「发送重置链接」拼成「发送重置链接中」。 */
      if (button) {
        button.disabled = true;
        if (button.dataset.busy) {
          idle = button.textContent;
          button.textContent = button.dataset.busy;
        }
      }

      function restore() {
        if (button) {
          button.disabled = false;
          if (idle) {
            button.textContent = idle;
          }
        }
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
            // 不是 HTML（也没有跳转）：这一趟什么都没发生，按钮得还回去，
            // 否则它会一直停在「正在登录」上，而其实早就结束了。
            restore();
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

    /* 一个委托监听管住所有入口：页眉的「登录 / 注册」，以及面板底部那几个
     * 支线出口（「忘记密码」「还没有账户？注册」「返回登录」）。
     * 它们全都是真的 <a href>，所以没有 JS 时各自都能独立工作；
     * 在弹窗里点它们则是原地换一张表单，人不必离开当前这一页。 */
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
        // 面板底部的支线出口：按它指向的路由换成对应的那张表单
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

    if (closeButton) {
      closeButton.addEventListener("click", function () {
        dialog.close();
      });
    }

    /* 点遮罩关闭。<dialog> 自己不做这件事：它的 ::backdrop 不是子元素，
     * 落在弹窗外的点击，target 就是 dialog 本身。 */
    dialog.addEventListener("click", function (event) {
      if (event.target === dialog) {
        dialog.close();
      }
    });

    /* 关闭就把内容丢掉：面板里的令牌是一次性的，留着它，
     * 下次打开用的还是上一次那枚——服务端已经换了新令牌，提交必被挡下。
     * Esc 关闭走的也是这条（浏览器在 Esc 上派发 close）。 */
    dialog.addEventListener("close", function () {
      body.replaceChildren();
      lockScroll(false);
    });
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
})();
