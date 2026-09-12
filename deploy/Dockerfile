# syntax=docker/dockerfile:1
#
# Lumo 生产镜像：从源码构建，产出单一静态二进制 + distroless 运行层。
#
#   docker build -f deploy/Dockerfile -t lumo:local .
#
# 构建上下文必须是仓库根（不是 deploy/），因为要同时拿到 console/ 与 Go 源码。
# 三个阶段：前端产物 → Go 二进制 → 运行层。前两个阶段固定跑在构建机架构上
# （--platform=$BUILDPLATFORM），交叉编译由 Go 自己完成，不必用 QEMU 模拟目标架构。

# ---------- 阶段 1：Console 前端 ----------
# Node 24：与 README「环境要求」一致，自带 npm 11，认得 package-lock 的 lockfileVersion。
FROM --platform=$BUILDPLATFORM node:24-alpine AS console

WORKDIR /src/console

# 先只复制依赖清单：源码改动不会让 npm ci 这一层失效。
COPY console/package.json console/package-lock.json ./
RUN npm ci

COPY console/ ./
# vite.config.ts 的 outDir 指向 ../internal/console/dist，
# 因此产物落在 /src/internal/console/dist，由下一阶段取走。
RUN npm run build

# ---------- 阶段 2：Go 二进制 ----------
# 版本写到补丁位而不是 golang:1.26-alpine：go.mod 要求 1.26.4，
# 浮动 tag 万一指到更早的补丁版，GOTOOLCHAIN 会在构建中途去网上抓工具链 ——
# 能成功，但把一次确定的构建变成了依赖外网的构建。
FROM --platform=$BUILDPLATFORM golang:1.26.4-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# 前端产物必须在 go build 之前就位：internal/console/console.go 的
# go:embed all:dist 是在编译期读取目录的，dist 为空则后台打不开。
COPY --from=console /src/internal/console/dist ./internal/console/dist

# 版本信息在构建期注入；不传时留源码默认值（0.0.0-dev）。
ARG VERSION=0.0.0-dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown

# buildkit 自动提供，单架构 docker build 时也有值。
ARG TARGETOS
ARG TARGETARCH

# nodynamic 禁掉 gen2brain/webp 的动态库回退，产物不依赖系统 libwebp；
# CGO_ENABLED=0 + -trimpath 出可跨发行版搬运的静态二进制。
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build \
      -tags nodynamic \
      -trimpath \
      -ldflags="-s -w \
        -X github.com/FeiBaiKin/lumo/internal/version.Version=${VERSION} \
        -X github.com/FeiBaiKin/lumo/internal/version.Commit=${COMMIT} \
        -X github.com/FeiBaiKin/lumo/internal/version.Date=${BUILD_DATE}" \
      -o /out/lumo ./cmd/lumo

# 运行层是 distroless，没有 shell 也就没法 mkdir。工作目录在这里建好，
# 随镜像带到运行阶段；属主由那边的 COPY --chown 设定。
RUN mkdir -p /out/data

# ---------- 阶段 3：运行 ----------
# static-debian12 自带 ca-certificates（SMTP STARTTLS 与 S3 走 HTTPS 都要它），
# 时区库则由 _ "time/tzdata" 编进了二进制，故不需要 tzdata 包。
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/lumo /lumo
# 用用户名而不是写死 65532：BuildKit 去本镜像的 /etc/passwd 解析它，
# 解析不到是构建期就报错；写死数字一旦对不上，会变成运行期「工作目录写不进去」的静默故障。
COPY --from=build --chown=nonroot:nonroot /out/data /data

# 工作目录刻意不设成 /data：dataDir 的默认值是相对路径 ./data，
# 那样会得到 /data/data。用绝对路径把它钉死。
WORKDIR /
ENV LUMO_DATA_DIR=/data

# 主题、上传、缓存、日志、备份全在 /data，**必须挂卷**，否则删容器即丢数据。
# 这里不写 VOLUME：那会在用户忘记挂载时静默生成难以追踪的匿名卷，
# 而 deploy/docker-compose.yml 已经显式挂了具名卷。
EXPOSE 8080

USER nonroot:nonroot

# 不设 HEALTHCHECK：镜像里没有 shell，也没有 curl/wget 可用。
# /healthz（进程存活）与 /readyz（额外探测数据库）供反向代理或编排系统从外部探测。
ENTRYPOINT ["/lumo"]
CMD ["serve"]
