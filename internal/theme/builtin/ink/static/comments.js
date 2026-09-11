/*
 * 评论提交。走 Public 平面的 REST 接口而非原生表单提交——
 * 原生提交后浏览器会跳到一个 JSON 响应页，那是个死胡同。
 *
 * 表单在 HTML 里带 hidden，由本脚本移除：没有 JS 时不该出现一个点了没反应的按钮。
 * 字段名与 internal/comment 的请求体一一对应，其中 website2 是蜜罐。
 */
(function () {
  "use strict";

  var form = document.getElementById("comment-form");
  if (!form) return;

  form.hidden = false;

  var status = form.querySelector(".comment-status");
  var button = form.querySelector("button[type=submit]");
  var postId = form.dataset.postId;

  function setStatus(message, state) {
    if (!status) return;
    status.textContent = message;
    if (state) {
      status.dataset.state = state;
    } else {
      delete status.dataset.state;
    }
  }

  function readProblem(body) {
    if (!body) return "";
    // 服务端返回 RFC 9457 problem+json：errors[] 里的明细比 detail 更具体。
    if (Array.isArray(body.errors) && body.errors.length > 0) {
      var first = body.errors[0];
      if (first && first.message) return first.message;
    }
    return body.detail || body.title || "";
  }

  form.addEventListener("submit", function (event) {
    event.preventDefault();

    var data = new FormData(form);
    var payload = {
      content: (data.get("content") || "").trim(),
      name: (data.get("name") || "").trim(),
    };
    var email = (data.get("email") || "").trim();
    var url = (data.get("url") || "").trim();
    var honeypot = data.get("website2") || "";
    // 可选字段留空时不发送：空串过不了服务端的 format:"email" 校验。
    if (email) payload.email = email;
    if (url) payload.url = url;
    if (honeypot) payload.website2 = honeypot;

    if (!payload.content || !payload.name) {
      setStatus("请填写称呼与评论内容。", "error");
      return;
    }

    button.disabled = true;
    setStatus("提交中…");

    fetch("/api/v1/public/posts/" + encodeURIComponent(postId) + "/comments", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    })
      .then(function (res) {
        return res
          .json()
          .catch(function () {
            return null;
          })
          .then(function (body) {
            return { ok: res.ok, body: body };
          });
      })
      .then(function (result) {
        if (!result.ok) {
          setStatus(readProblem(result.body) || "提交失败，请稍后重试。", "error");
          return;
        }
        form.reset();
        // 评论默认待审核：说清楚「已收到但还看不到」，否则用户会反复提交。
        var approved = result.body && result.body.status === "approved";
        setStatus(approved ? "评论已发表。" : "评论已提交，通过审核后显示。");
        if (approved) {
          window.setTimeout(function () {
            window.location.reload();
          }, 800);
        }
      })
      .catch(function () {
        setStatus("网络异常，请稍后重试。", "error");
      })
      .finally(function () {
        button.disabled = false;
      });
  });
})();
