/*
 * 落地页模块的一点交互：目前只有命令块的复制按钮。
 *
 * 与 code.js 分开是因为它们服务的东西不同：code.js 管文章正文里的代码块
 * （换行 / 复制 / 展开三个按钮，还要处理行号与高亮），命令块只有一个按钮、
 * 没有高亮、也不折叠。把二十行塞进那八千字节的文件里，只会让两边都更难读。
 *
 * 不受 appearance.motion 管辖：复制是功能不是装饰。
 * 没有这段脚本时按钮一直 hidden，命令照样能手工选中复制。
 */
(function () {
  "use strict";

  var blocks = document.querySelectorAll(".module-command .command-block");
  if (!blocks.length || !navigator.clipboard) {
    return;
  }

  blocks.forEach(function (block) {
    var button = block.querySelector(".command-copy");
    var code = block.querySelector("code");
    if (!button || !code) {
      return;
    }

    // 到这里才显形：剪贴板不可用时按钮压根不该出现，而不是点了没反应。
    button.hidden = false;

    var timer = 0;
    button.addEventListener("click", function () {
      navigator.clipboard.writeText(code.textContent || "").then(
        function () {
          button.textContent = button.dataset.copiedLabel || "已复制";
          window.clearTimeout(timer);
          timer = window.setTimeout(function () {
            button.textContent = button.dataset.copyLabel || "复制";
          }, 2000);
        },
        function () {
          // 失败多半是没拿到剪贴板权限。说出来，别让人以为复制成功了。
          button.textContent = "复制失败";
        },
      );
    });
  });
})();
