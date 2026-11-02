# 后端项目部署

> **执行目录约定**：本文所有相对路径命令（`./scripts/...`、`make ...`、`docker compose ...`）
> 一律在 **`backend/` 目录**下执行——仓库根目录没有 Makefile，脚本也不在根目录。

- 所有的Docker配置文件都在`backend`目录下。
- 所有的部署脚本都在`backend/scripts`目录下。

> 详细的脚本使用说明请参考：[scripts/README.md](../backend/scripts/README.md)

Shell脚本需要赋予执行权限（`scripts/` 下含多级子目录，`**` 在多数 shell 里不会递归，用 `find`）：

```bash
cd backend
find ./scripts -name '*.sh' -exec chmod +x {} +
```

## 初始化操作系统环境

在我们拿到服务器后，首先要做的就是初始化操作系统环境。我们需要安装一些必要的工具和软件包。

> 推荐使用项目提供的一键安装脚本，详见 [环境准备脚本](../backend/scripts/env/)

### Linux / macOS

**生产环境：**

```bash
./scripts/env/install_unix_prod.sh
```

**开发环境：**

```bash
./scripts/env/install_unix_dev.sh
```

### Windows（PowerShell 管理员）

```powershell
.\scripts\env\install_windows_dev.ps1
```

> 脚本用 `$MyInvocation` 解析自身所在目录来加载 `scripts/env/lib/*.ps1`（见 `install_windows_dev.ps1:13`），
> 因此**在哪个目录调用都一样**——从 `backend/` 用上面的相对路径，或从仓库根目录用
> `powershell -ExecutionPolicy Bypass -File backend\scripts\env\install_windows_dev.ps1`
> （[windows-startup-guide.md](./windows-startup-guide.md) 的方式）都成立。

## Docker 两种部署模式

部署项目有两种方法：

1. **完整模式**：三方中间件和微服务都运行在Docker之下；
2. **依赖模式（推荐开发）**：三方中间件运行在Docker下，微服务在本地IDE运行调试。

### 口令与 `.env`（两种模式都需要）

Compose 文件里不再写死口令，统一从 `backend/.env` 读取。先复制模板再填：

```bash
cd backend
cp .env.example .env   # 然后编辑 .env
```

| 变量 | 用途 | 完整模式 | 依赖模式 |
|---|---|---|---|
| `POSTGRES_PASSWORD` | PostgreSQL `postgres` 用户口令 | 必填 | 必填 |
| `REDIS_PASSWORD` | Redis 口令（缓存和 asynq 任务队列共用） | 必填 | 必填 |
| `MINIO_ROOT_USER` / `MINIO_ROOT_PASSWORD` | MinIO 账号（密码至少 8 位） | 必填 | 必填 |
| `GWA_ADMIN_INITIAL_PASSWORD` | 空库首次启动时播种的 admin 密码 | 必填 | 不用 |
| `GWA_AUTH_JWT_PRIVATE_KEY` | 生产 JWT 私钥，见下文「生产环境密钥配置」 | 可选 | 不用 |
| `GOWIND_CRYPTO_KEY` | 签名图片 URL 的 HMAC 密钥 | 可选 | 不用 |

- 必填项缺失时 `docker compose` 直接报错退出（`${VAR:?...}`），`full_deploy.sh` 也会先检查一遍。
- 口令只用字母和数字：它们会被拼进数据库 DSN 和 `redis://` URI。
- `.env` 已在 `backend/.gitignore` 和 `.dockerignore` 里，不会进 Git 和镜像。
- 完整模式下后端容器拿到的是同一份口令：Compose 把它们以 `GWA_DB_PASSWORD`、`GWA_REDIS_PASSWORD`、`GWA_MINIO_ACCESS_KEY`、`GWA_MINIO_SECRET_KEY` 传进容器，`configs/data.yaml`、`server.yaml`、`oss.yaml` 里的 `${GWA_XXX:默认值}` 占位符读取它们（见下文「配置里的环境变量占位符」）。

### 数据持久化与端口

- PostgreSQL、Redis、MinIO 和后端的审计归档目录（`/app/data`）都挂在 Compose **具名卷**上（`postgres-data`、`redis-data`、`minio-data`、`admin-data`）。`docker compose down` 再 `up -d` 数据还在；`docker compose down -v` 会连卷一起删掉，**不要在生产上用**。Redis 开了 AOF，重启后未执行的任务还在。
- 完整模式只对外暴露后端 `7788`（HTTP）和 `7789`（SSE）。数据库、Redis、MinIO 不映射宿主机端口，只在 Compose 网络内可达。需要临时连库时用 `docker compose exec postgres psql -U postgres gwa`。
- 依赖模式（`docker-compose.libs.yaml`）为了让本机后端连得上，把 5432 / 6379 / 9000 / 9001 映射出来，但只绑 `127.0.0.1`。
- 旧版本 Compose 没有挂卷，升级前若有要保留的数据，先用 `pg_dump` 等工具导出，换新 Compose 后再导入。

### 1. 完整模式（三方中间件 + 微服务都在 Docker 下）

先按上一节准备好 `.env`。

**Linux / macOS：**

```bash
./scripts/docker/full_deploy.sh
```

**Windows (PowerShell)：**

```powershell
.\scripts\docker\full_deploy.ps1
```

> Windows 脚本没有做 `.env` 预检，缺必填项时由 `docker compose` 报错。

### 2. 依赖模式（仅启动三方中间件，微服务本地运行）

同样需要 `.env` 里的四个依赖口令。本机后端要用同一份口令：在 `configs/local/` 里写，或者在启动后端的 shell 里导出 `GWA_DB_PASSWORD` 等同名变量。

**Linux / macOS：**

```bash
./scripts/docker/libs_only.sh
```

**Windows (PowerShell)：**

```powershell
.\scripts\docker\libs_only.ps1
```

然后本地运行后端服务：

```bash
make -C app/admin/service run
```

### 3. PM2 进程管理（生产环境物理机部署）

```bash
./scripts/deploy/pm2_service.sh
```

## SSE 反向代理网关（可选）

SSE 推送链路在生产拓扑中由一个独立的 nginx 反向代理网关承载，把 `/events` 透传至后端 admin-service 的 SSE transport（默认 `7789`）。该网关不随 docker-compose 启动，需单独构建与运行。

构建镜像并运行容器：

```bash
bash scripts/deploy/sse/build-local-docker-image.sh

# 先查出网络的真名（见下），再用它启动
docker network ls --filter name=app-tier
docker run -d -p 8013:8080 --network <上面查到的网络名> --name sse-gateway-local sse-gateway-local
```

说明：

- 网关必须与 admin-service 共处同一 Docker 网络才能访问后端。
- **`--network app-tier` 不能照抄**：`backend/docker-compose.yaml:1-3` 只声明了
  `networks: app-tier: {driver: bridge}`，没写 `name:`，所以 Compose 建出的真实网络名带项目名前缀
  （默认项目名取 compose 文件所在目录名，即形如 `<目录名>_app-tier`；用了 `-p` / `COMPOSE_PROJECT_NAME`
  则是另一个前缀）。**具体名字无法凭文档断定，用 `docker network ls --filter name=app-tier` 现场查**。
  想钉死成 `app-tier`，在 compose 的该网络下加一行 `name: app-tier` 后 `docker compose up -d` 重建。
  同样的照抄问题也存在于 `scripts/deploy/sse/build-local-docker-image.sh` 结尾打印的示例命令里
  （脚本只是 echo 一个 sample，不代为执行），以本节为准。
- 配置文件 `scripts/deploy/sse/nginx.conf` 已针对 SSE 关闭 gzip 与一切代理缓冲（`proxy_buffering off` / `proxy_cache off` / `proxy_request_buffering off`），并使用 HTTP/1.1 与长连接保活；这些是 SSE 实时性的必要配置，勿随意回退。
- TLS 终止由外层负载均衡负责，与前端静态站点部署一致，网关自身仅监听 HTTP。
- 独立部署时如后端地址变更，需修改 `nginx.conf` 中的 `upstream`。

## 可信反向代理（客户端 IP）

登录限流和审计日志按客户端 IP 记账。只有当 TCP 直连对端属于可信代理时，后端才采信 `X-Forwarded-For` / `X-Real-IP`，否则一律用对端地址。

默认信任 RFC1918 私网和回环（`10.0.0.0/8`、`172.16.0.0/12`、`192.168.0.0/16`、`127.0.0.0/8`、`::1/128`、`fc00::/7`）。docker 网络里的 nginx、同机反向代理都落在这些网段，所以不配置也能拿到真实 IP。代价是：任何能从内网直连后端的客户端都能伪造 XFF，躲过按 IP 的限流。

后端端口只对反向代理开放时，建议在 `configs/server.yaml` 里收窄到实际代理地址：

```yaml
server:
  rest:
    trusted_proxies:
      cidrs:
        - "172.18.0.2"      # 反向代理容器地址，或其网段 "172.18.0.0/16"
```

- 不写 `trusted_proxies`：保持上面的默认网段。
- 写了 `trusted_proxies` 但 `cidrs` 为空：不信任任何代理，始终用 TCP 对端地址（后端直接对外、前面没有代理时用）。
- 支持 CIDR，也支持单个 IP。非法条目会被跳过。

## 生产环境密钥配置

### 初始管理员密码

空库首次启动时自动播种 `admin` 账号。设置了 `GWA_ADMIN_INITIAL_PASSWORD` 就用它作初始密码（须满足口令复杂度）；没设置时沿用仓库公开的默认密码（见 `pkg/constants/default_data.go`），启动日志会打 WARN。完整 Compose 部署把这个变量设成了必填。它只影响首次播种，库里已有用户后再改它不会改 admin 的密码。

### JWT 密钥

后端 JWT 签名密钥支持通过环境变量注入，优先级高于 `configs/auth.yaml`：

| 环境变量 | 用途 |
|---------|------|
| `GWA_AUTH_JWT_PRIVATE_KEY` | 非对称算法（RS256 / ES256 / Ed25519 等）的 PEM 私钥 |
| `GWA_AUTH_JWT_PUBLIC_KEY` | 非对称算法的 PEM 公钥 |
| `GWA_AUTH_JWT_KEY` | 对称算法（HS256 等）的共享密钥 |

`configs/auth.yaml` 内置的是开发示例密钥，生产环境必须通过上述环境变量替换为自有密钥，否则启动时会告警。

- 只设置 `GWA_AUTH_JWT_PRIVATE_KEY`、不设公钥时，公钥由新私钥派生，yaml 里的示例公钥不再使用。
- 启动时会用当前密钥签一个探针令牌再验签；公私钥不配对直接启动失败，不会等到上线后令牌全部验签失败。

生成命令：

```bash
openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 -out jwt_private_key.pem
openssl pkey -in jwt_private_key.pem -pubout -out jwt_public_key.pem
```

## pprof

`configs/server.yaml` 里 `server.rest.enable_pprof` 默认是 `false`。pprof 路由（`/debug/pprof`、`/debug/heap`、`/debug/profile` 等）不经过登录鉴权，任何能访问 7788 的人都能读到内存、协程和命令行信息，还能触发 CPU profiling。

需要排查时，在 `configs/local/server.yaml` 里写：

```yaml
server:
  rest:
    enable_pprof: true
```

重启后生效。只在内网或本机可达的环境打开，用完删掉这段再重启。

## 上传限制与超时

- REST 服务最外层（早于 CORS、路由、JSON 解码和鉴权）统一限制请求体大小：文件上传 `/admin/v1/file/upload` 约 70.7 MiB（50 MiB 文件经 base64 膨胀后再加 4 MiB 余量），其他接口约 7.7 MiB（按头像上限 5 MiB 的 base64 再加 1 MiB）。`Content-Length` 超限时一个字节都不读直接返回 413；没有 `Content-Length` 的分块请求最多读到上限加 1 字节就返回 413。前置反向代理的 body 上限不要低于上传接口的值。
- REST 端口（7788）底层设了 `ReadHeaderTimeout=10s`、`ReadTimeout=5m`、`IdleTimeout=2m`，不设写超时，大文件下载和审计导出不受影响。SSE 在 7789，是单独的服务，不受这些超时影响。

## 审计归档目录

审计归档任务每天把超过保留期的审计日志导出到 `AUDIT_ARCHIVE_DIR`（默认 `./data/audit-archive`）后从库里删除。镜像里预建了 `/app/data` 并交给运行用户 `appuser`；完整 Compose 设置 `AUDIT_ARCHIVE_DIR=/app/data/audit-archive` 并把 `/app/data` 挂到 `admin-data` 卷。某张表查询、写文件或删除失败时，其他表照常处理，任务最后返回错误，asynq 会记为失败并重试。

## 多实例部署

多个 admin 实例共享同一个数据库和同一个 Redis 时，下面这些跨实例协调都经 Redis 完成，不需要额外配置：

| 内容 | 做法 |
|---|---|
| 启动 | 自动迁移、默认数据播种、接口/权限点/菜单同步、首次策略装载在 Redis 锁 `gowind:startup:migrate-and-seed` 内执行，并发启动的实例依次进行（最长等 10 分钟，超时启动失败）。Redis 连不上时记 ERROR、不加锁继续启动 |
| 授权策略 | 任一实例改了角色/权限点/接口后 INCR `gowind:authz:policy_version` 并广播，其他实例重载；订阅断线重连时按版本号对账 |
| 参数缓存 | 失效广播；订阅重连时清空本地缓存；每个条目最多缓存 5 分钟 |
| SSE 通知 | 经 Redis 频道转发，各实例投递给自己的连接 |
| 系统周期任务 | 审计归档同一轮只执行一次（见 [task_system.md](./task_system.md) 5.2） |

## 备份与恢复

程序内不提供数据库备份。用数据库自带工具（如 `pg_dump`）或运维平台做全量备份，并演练一次恢复。

## 本地覆盖配置

本机要连自己的数据库、Redis 或 MinIO 时，不要改 `backend/app/admin/service/configs/` 根下被 Git 跟踪的 yaml。在同级的 `configs/local/` 里放同名文件，只写要改的键，例如 `local/data.yaml` 里只保留 `data.database.source` 和 `data.redis`。

加载顺序：

1. 主配置目录。按目录读取时会跳过子目录，因此 `local/` 不会被主配置读两遍。
2. 若存在 `config.yaml`，远程配置源仍按原逻辑加载，并覆盖主配置里的同名键。
3. `configs/local/` 仅在目录存在时最后加载。合并是深度合并。这里显式写出的标量都会覆盖同名键，包括空字符串、false 和 0；没写的键保留前面的值。

`configs/local/` 在仓库根 `.gitignore` 里，也在 `backend/.dockerignore` 里，构建镜像时不会被打进去（Dockerfile 复制配置后还会再删一次 `local/`）。部署时可以把同名目录挂进容器的 `/app/configs/local`，用来覆盖镜像里的主配置，同样只放差异键。

### 配置里的环境变量占位符

配置加载时，所有 `GWA_` 开头的环境变量会按原名加入配置，yaml 里可以用 `${GWA_XXX:默认值}` 引用：设置了变量就用变量值，没设置用冒号后的默认值。目前用到的有：

| 占位符 | 位置 |
|---|---|
| `GWA_DB_PASSWORD` | `data.yaml` 的 `data.database.source` |
| `GWA_REDIS_PASSWORD` | `data.yaml` 的 `data.redis.password`、`server.yaml` 的 `server.asynq.uri` |
| `GWA_MINIO_ACCESS_KEY` / `GWA_MINIO_SECRET_KEY` | `oss.yaml` |

默认值和原来写死的开发口令一致，本地开发不设变量时行为不变。`configs/local/` 里写死的值同样会覆盖占位符。本地把主机名改成 `localhost` 的例子见 [windows-startup-guide.md](./windows-startup-guide.md) 第三步。

## 本地开发配置 hosts

完整模式下依赖服务不再映射宿主机端口，下面的 hosts 配置只对依赖模式（端口绑在 127.0.0.1）有意义。如需从宿主机访问，修改`hosts`文件（需要管理员权限）：

- Linux：`/etc/hosts`
- MacOS：`/private/etc/hosts`
- Windows：`C:\Windows\System32\drivers\etc\hosts`

增加以下内容——只对应 `docker-compose.yaml` 里**实际存在**的服务（postgres / redis / minio）：

```ini
127.0.0.1 postgres
127.0.0.1 redis
127.0.0.1 minio
```

> **沿革**：此处早期还列过 `consul` 与 `jaeger`，本仓现状没有这两种部署——`docker-compose.yaml` 里
> jaeger 整段（86 行起）是注释掉的，配置与代码里 `grep -i consul` 零命中，注册中心也未接入。
> 若二次开发自行引入 Consul，再按当时的形态补 hosts 与 `registry` 配置（历史坑：`consul` 作主机名会 502，
> 地址要写 `localhost:8500`）。
>
> **推荐做法**：本地开发使用依赖模式（libs_only），在 `configs/local/` 里把主机名覆盖成 `localhost`，无需修改 hosts，也不要改被跟踪的主配置
> （见 [windows-startup-guide.md](./windows-startup-guide.md) 第三步）。
>
> **与一键脚本的差异**：`scripts/env/install_windows_dev.ps1` 以管理员运行时写的是**带 `.local` 后缀**的
> 域名（其 50-51 行 `$services = @('postgres','mysql','redis','minio')` + `-DomainSuffix ".local"`，
> 由 `scripts/env/lib/host-utils.ps1:111,119-120` 拼成 `postgres.local` 等），与上面的裸主机名清单不一致；
> 其中 `mysql` 一项当前 compose 也未启用。两种写法各自自洽即可，**不要混用**：走脚本就在配置里写
> `postgres.local`，走本文清单就写 `postgres`，用 `localhost` 则两条都不需要。

## 上线前的访问日志

这次修复上线之前，访问日志会记下请求参数里的口令。登录请求是 AES 密文，密钥打在前端包的
`VITE_AES_KEY` 里，可以解回明文。创建用户当时是明文。这段时间产生的访问日志要清理，
或者限制访问。此期间新建过的用户，建议改一次密码。根因和修法见
`docs/plans/trim-and-own-deps.md` 的「字段隐藏、访问日志与创建用户口令」。
