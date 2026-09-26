// 访问统计的前台脚本。
//
// 宿主把插件的脚本放在页尾，并在标签上带两个属性：
//   data-api   本插件的接口前缀，如 /api/v1/plugins/visit-stats
//   data-post  当前页面的文章 ID，只在文章与页面上有
//
// 所以脚本不用自己猜地址、也不用往页面里塞配置。同一个会话里同一篇只回打一次，
// 免得刷新几次就把数字刷上去。
(() => {
  const el = document.currentScript;
  if (!el) {
    return;
  }
  const post = Number(el.dataset.post || 0);
  const api = el.dataset.api;
  if (!post || !api) {
    return;
  }
  const key = `lumo-visit:${post}`;
  try {
    if (sessionStorage.getItem(key)) {
      return;
    }
    sessionStorage.setItem(key, "1");
  } catch {
    // 隐私模式下 sessionStorage 可能不可用，那就每次都计
  }
  fetch(`${api}/hit`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ post }),
    // keepalive 让请求在页面被关掉之后也能发完
    keepalive: true,
  }).catch(() => {
    // 计数失败不该打扰访客，也不该在控制台留下红字
  });
})();
