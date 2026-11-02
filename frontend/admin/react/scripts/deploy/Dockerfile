# 前端镜像：构建 React 产物，交给 nginx 托管并反代 API / SSE。
# 构建上下文是 frontend/admin/react（backend/docker-compose.yaml 的 frontend 服务）。

##################################
# 第一阶段：构建
##################################
FROM node:22.22.0-bookworm-slim AS builder

ENV NODE_OPTIONS=--max-old-space-size=8192
RUN npm install -g pnpm@12.16.1

WORKDIR /app

# 先只拷依赖清单，源码改动时可以复用依赖层
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY patches ./patches
RUN pnpm install --frozen-lockfile

COPY . .

# 同源部署：API 走相对路径 /admin/v1/...，SSE 走 /events，都由同一个 nginx 反代。
# 进程环境变量优先于 .env.production 里的演示站地址。
ENV VITE_API_URL=/ \
    VITE_SSE_URL=/events
RUN pnpm run build

##################################
# 第二阶段：nginx 运行时
##################################
FROM nginx:1.28.0-alpine

COPY --from=builder /app/dist /usr/share/nginx/html
COPY scripts/deploy/nginx.conf /etc/nginx/nginx.conf

EXPOSE 8080

CMD ["nginx", "-g", "daemon off;"]
