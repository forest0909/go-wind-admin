##################################
# 第一阶段：构建GO可执行文件
##################################

# 使用官方的 Go 基础镜像作为构建环境
ARG GO_VERSION=1.26.4
FROM golang:${GO_VERSION}-alpine AS builder

ARG SERVICE_NAME=admin
ARG APP_VERSION=1.0.0

# 设置工作目录
WORKDIR /src

# 复制项目源代码到工作目录
COPY . /src

# 下载依赖，国内代理链：首选 goproxy.cn，备选 direct（直接下载）
# 增加 GOSUMDB 配置避免校验服务器访问失败
RUN GOPROXY=https://goproxy.cn,direct GOSUMDB=off go mod download

# 编译可执行文件（使用WORKDIR和相对路径，而不是cd）
# 不写死 GOARCH：跟随构建机架构（x86 服务器出 amd64，ARM 服务器出 arm64）
RUN CGO_ENABLED=0 \
    GOOS=linux \
    go build -ldflags "-X main.version=$APP_VERSION" \
    -o /src/bin/${SERVICE_NAME}-server ./app/${SERVICE_NAME}/service/cmd/server/

# 复制配置文件到统一目录（local/ 是开发者本机覆盖层，.dockerignore 已排除，这里再删一次兜底）
RUN mkdir -p /src/bin/configs && \
    if [ -d "/src/app/${SERVICE_NAME}/service/configs" ]; then \
      cp -r /src/app/${SERVICE_NAME}/service/configs/* /src/bin/configs/ 2>/dev/null || true; \
    fi && \
    rm -rf /src/bin/configs/local

##################################
# 第二阶段：创建最终的运行时镜像
##################################

# 使用 Alpine 作为基础镜像，因为它非常轻量级
FROM docker.io/alpine:3.22

ARG SERVICE_NAME=app

# ca-certificates：出站 HTTPS；tzdata：asynq 按 server.asynq.location（Asia/Shanghai）调用
# time.LoadLocation，镜像里没有时区数据会直接 panic，进程起不来
RUN apk --no-cache add ca-certificates tzdata

# 设置工作目录
WORKDIR /app

# 从第一阶段的构建结果中复制可执行文件到当前工作目录
COPY --from=builder /src/bin/${SERVICE_NAME}-server /app/bin/server

# 拷贝配置文件
COPY --from=builder /src/bin/configs/ /app/configs/

# 创建一个名为 appuser 的非 root 用户；
# 预建 /app/data 交给 appuser，审计归档默认写 /app/data/audit-archive（Compose 把它挂到持久卷）
RUN adduser -D appuser && \
    mkdir -p /app/data/audit-archive && \
    chown -R appuser:appuser /app/data

# 切换到非特权用户
USER appuser:appuser

# REST 和 SSE 端口
EXPOSE 7788 7789

# 设置容器启动时执行的命令
CMD ["/app/bin/server", "-c", "/app/configs"]
