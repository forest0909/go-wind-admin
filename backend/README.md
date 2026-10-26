# GO后端

本项目是 Go + Kratos + Ent 的管理后端，支持：

- 多协议：RESTful API + gRPC
- 多存储：MySQL/PostgreSQL、Redis、MinIO
- 服务治理：Etcd 注册发现、Jaeger 链路追踪
- 代码生成：Ent ORM、Protobuf、TypeScript 客户端
- 容器化：Docker + Docker Compose 一键部署
- 文档自动生成：Swagger UI、OpenAPI v3

## 前置环境要求

### 基础环境

| 工具             | 版本要求  | 说明          |
|----------------|-------|-------------|
| Go             | 1.21+ | 核心开发语言      |
| Docker         | 20.0+ | 容器化部署       |
| Docker Compose | v2+   | 服务编排        |
| Make           | 3.8+  | 构建工具        |
| PM2            | 可选    | 进程管理（物理机部署） |

### 中间件（自动部署）

| 中间件        | 版本要求          | 说明                                 |
|------------|---------------|------------------------------------|
| Redis      | 未固定         | compose 拉 `bitnami/redis:latest`。旧版本此处写"必须 ≥8.0，使用 `HExpire`/`HSETEX`"——两条命令在 `backend/**` 与本仓依赖树里均 **0 命中**（2026-09-25 实测，命令见下方"Redis 命令报错"一节），实际只用到 `Set`/`Expire`/`Publish`/`HSet` 一类长期命令 |
| Postgresql | 14+           | 关系型数据库，支持 JSON / 事务                |
| MinIO      | RELEASE.2024+ | 兼容 S3 协议的对象存储服务                    |
| Jaeger     | 1.40+         | 分布式链路追踪，可视化请求耗时                    |

## 内置API文档

### Swagger UI

- [Admin Swagger UI](http://localhost:7788/docs/)

### openapi.yaml

- [Admin openapi.yaml](http://localhost:7788/docs/openapi.yaml)

## 项目目录结构

```text
myproject/
├── api/                    # API定义/Protobuf
├── app/                    # 微服务主目录
│   └── admin/              # 单个服务
│       └── service/        # 服务启动目录
│              ├── cmd/         # 启动入口
│              ├── configs/     # 配置文件
│              └── internal/    # 业务核心代码
│                     ├── data/     # 数据访问层（Ent ORM）
│                     ├── server/   # gRPC/HTTP服务层
│                     └── service/  # 业务逻辑层
├── scripts/             # 部署脚本
├── go.mod               # Go模块依赖
├── go.sum               # 依赖校验
└── Makefile             # 构建命令
```

## 常用命令

在 `backend/` 下执行。本仓不再使用 gow。

```bash
make plugin   # 安装 protoc 插件。redact 和 typescript-http 从 third_party/tools 编译
make api      # Protobuf Go 代码，并生成配置 pb
make ent      # Ent ORM
make openapi  # OpenAPI
make ts       # React 的 TypeScript 客户端
make -C app/admin/service run
```

进入服务目录时，`make api` / `make ent` / `make openapi` / `make run` 同样可用。新模块登记：`make register ENTITY=product`。

## Makefile 构建命令详解

### 环境初始化

```bash
make init     # 安装所有依赖、工具、插件
make cli      # 更新命令行工具
make plugin   # 更新项目插件
```

### 代码生成（开发常用）

```bash
# 进入服务目录
cd app/admin/service

make api      # 生成API/Protobuf代码
make ent      # 生成Ent ORM代码
make openapi  # 生成OpenAPI文档
make ts       # 生成前端TS代码

# 根目录执行:登记新CRUD模块到手写装配(wiring_ent.go + rest_server.go)
cd ../.. && make register ENTITY=product
```

### 运行与构建

```bash
make run      # 调试运行服务
make build    # 编译二进制文件
```

### Docker 容器管理

```bash
make compose-up      # 启动所有服务+中间件
make compose-down    # 停止所有服务+中间件
make docker         # 构建服务Docker镜像
```

## 一键部署整个项目

部署项目有两种方法：

1. 三方中间件和微服务都运行在Docker之下；
2. 三方中间件运行在Docker下，微服务通过PM2管理运行在OS下。

#### 1. 三方中间件和微服务都运行在Docker之下

```bash
cd ./scripts/docker
chmod +x *.sh
./full_deploy.sh
```

#### 2. 三方中间件运行在Docker下，微服务运行在OS下

先部署三方中间件：

```bash
cd ./scripts/docker
chmod +x *.sh
./libs_only.sh
```

接着部署PM2管理的微服务：

```bash
cd ./scripts/deploy
chmod +x *.sh
./pm2_service.sh
```

## 常见问题 FAQ

### go.sum 依赖验证失败

有时候，有的依赖包在`go mod tidy`之后会出现`go.sum`验证不通过的问题，解决方法：

```bash
go clean -modcache
go mod tidy
```

如果还不行的话，可以删除`go.sum`文件，然后重新执行`go mod tidy`。

### 中间件连接失败（postgres/redis/minio）

**原因**：容器内域名解析需要配置 hosts

我们需要修改`hosts`文件，修改需要管理员权限，其配置文件路径在：

- Linux：`/etc/hosts`
- MacOS: `/private/etc/hosts`
- Windows: `C:\Windows\System32\drivers\etc\hosts`

增加以下内容：

```ini
127.0.0.1 postgres
127.0.0.1 redis
127.0.0.1 minio
127.0.0.1 consul
127.0.0.1 jaeger
```

> 注意：如果注册中心使用Consul，consul的地址填写为`consul`会返回`502`，使用`localhost`或者`127.0.0.1`都可以。
> ```yaml
> registry:
> type: "consul"
>
> consul:
> address: "localhost:8500"
> ```

# Redis 命令报错 / 版本过低

- **本仓不要求 Redis 8**：旧版本文档写的"项目使用 `HExpire` 命令，仅支持 Redis 8.0+"没有代码依据——
  `grep -rnE "\.HExpire\(|\.HPExpire\(|\.HExpireAt\(|\.HTTL\(" --include=*.go app pkg` 在本仓 **0 命中**，
  在 `hibiken` / `go-kratos` / `go-redsync` 和本仓 `third_party` 下同样 **0 命中**（2026-09-25 实测）。
  应用实际用到的是 `Set` / `Expire` / `Publish` / `HSet` 一类的长期命令（同口径 837 处调用）。
- **仍然报"unknown command"时**：compose 拉的是 `bitnami/redis:latest`，未在配置里固定版本，
  因此报错一般来自**你自己指向的旧服务端**（例如自建 6.x/7.x 或云厂商锁定版本）。
  按报错的那条命令去查它的最低服务端版本，再决定升级——不要照搬"必须 8.0+"。
- **认证失败**（与版本无关）：`data.yaml` / `server.yaml` 里的 `password` 要与服务端一致，
  开发库默认 `*Abcd123456`。
