#!/bin/sh
# 把 examples/plugins 下的示例插件编成可安装的 zip 包。
#
# 用法：sh examples/plugins/build.sh [插件名 ...]
# 不带参数时编全部。产物是 <插件名>.zip，在后台「插件 → 安装插件」里直接传。
#
# 为什么要显式指定这几个环境变量：Go 默认按本机平台编译，而插件要的是 wasip1 上的
# c-shared 反应器模块。GOFLAGS / GOWORK 清空是为了不被本地 go.work 与构建标签带偏。
set -eu

cd "$(dirname "$0")"

targets="${*:-comment-guard visit-stats comment-notify}"

for name in $targets; do
	if [ ! -d "$name" ]; then
		echo "没有这个插件：$name" >&2
		exit 1
	fi
	echo "==> $name"
	(
		cd "$name"
		GOOS=wasip1 GOARCH=wasm GOFLAGS= GOWORK=off CGO_ENABLED=0 \
			go build -buildmode=c-shared -o plugin.wasm .
	)
	# 用 Python 打包而不是 zip 命令：Git for Windows 自带的 MSYS 里没有 zip。
	# 打进包的是清单、设置、静态文件与编好的 wasm；源码与 go.mod 不进包——
	# 包是给宿主加载的东西，源码在仓库里看。
	python - "$name" <<'PY'
import os, sys, zipfile

name = sys.argv[1]
with zipfile.ZipFile(f"{name}.zip", "w", zipfile.ZIP_DEFLATED) as z:
    for root, _dirs, files in os.walk(name):
        for file in sorted(files):
            path = os.path.join(root, file)
            arc = os.path.relpath(path, name).replace(os.sep, "/")
            if file in {"go.mod", "go.sum"} or arc.endswith((".go", ".zip")):
                continue
            z.write(path, arc)
PY
	echo "    好：$name.zip"
done
