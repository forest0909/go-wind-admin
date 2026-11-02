#!/usr/bin/env bash
################################################################################
## Docker Compose 启动脚本 - 完整应用版本（应用 + 依赖）
##
## 功能：
##   启动完整的 Docker Compose 应用，包括主应用服务和所有依赖
##
## 启动的服务：
##   - 主应用服务（根据 docker-compose.yml 定义）
##   - PostgreSQL 数据库
##   - Redis 缓存
##   - MinIO 对象存储
##
## Compose 文件：
##   使用 docker-compose.yml 或 docker-compose.yaml（项目根目录）
##
## 使用场景：
##   1. 完整的本地开发环境
##   2. 快速验收测试
##   3. 生产环境部署
##   4. 一键启动所有服务
##
## 用法：
##   bash scripts/full_deploy.sh
##
## 前置条件：
##   backend/.env 已按 .env.example 填好口令和 GWA_ADMIN_INITIAL_PASSWORD
##   （cp .env.example .env 后修改）。缺少必填项时脚本和 docker compose 都会报错退出。
##
## 数据持久化：
##   PostgreSQL / Redis / MinIO / 审计归档使用 Compose 具名卷，
##   docker compose down 后再 up 数据仍在；docker compose down -v 会删除数据。
##
## 示例：
##   bash scripts/docker/full_deploy.sh
##
## 相关脚本：
##   - libs_only.sh  仅启动依赖，不启动应用
##
################################################################################
set -euo pipefail
IFS=$'\n\t'

# 切换到脚本所在目录的上一级（项目根）
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$script_dir/../.." || { echo "Failed to cd to repo root ($script_dir/../..)" >&2; exit 1; }

# 检查 docker-compose 文件
if [ ! -f docker-compose.yml ] && [ ! -f docker-compose.yaml ]; then
  echo "No docker-compose.yml or docker-compose.yaml found in project root." >&2;
  exit 1
fi

# 检查 .env 及必填项（docker compose 也会校验，这里提前给出更清楚的提示）
if [ ! -f .env ]; then
  echo "backend/.env not found. Run: cp .env.example .env, then fill in the passwords." >&2
  exit 1
fi
missing=()
for key in POSTGRES_PASSWORD REDIS_PASSWORD MINIO_ROOT_USER MINIO_ROOT_PASSWORD GWA_ADMIN_INITIAL_PASSWORD GWA_AUTH_JWT_PRIVATE_KEY; do
  if ! grep -Eq "^${key}=.+" .env; then
    missing+=("$key")
  fi
done
if [ ${#missing[@]} -gt 0 ]; then
  echo "Missing required values in backend/.env: ${missing[*]}" >&2
  exit 1
fi

# 选择 docker compose 命令（支持 v2 插件或旧的 docker-compose 可执行文件）
DOCKER_COMPOSE_CMD=()
if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
  DOCKER_COMPOSE_CMD=(docker compose)
elif command -v docker-compose >/dev/null 2>&1; then
  DOCKER_COMPOSE_CMD=(docker-compose)
else
  echo "Neither 'docker compose' plugin nor 'docker-compose' found. Please install docker compose." >&2
  exit 1
fi

# 启动服务
echo "Bringing up services with: $DOCKER_COMPOSE_CMD up -d --force-recreate"
${DOCKER_COMPOSE_CMD[@]} up -d --force-recreate
