<div align="center">

<img src="docs/brand/vortex-tile.svg" width="120" alt="GoWind Admin｜风行" />

# GoWind Admin｜风行

**开箱即用的企业级前后端一体管理脚手架（Go 后端 + React 19 前端）**

[![License](https://img.shields.io/badge/License-MIT-blue.svg)](./LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go)](https://go.dev/)
[![Frontend](https://img.shields.io/badge/Frontend-React_19_only-61DAFB?logo=react)](./frontend/admin/react)
[![React](https://img.shields.io/badge/React-19.x-61DAFB?logo=react)](https://react.dev/)
[![Docker](https://img.shields.io/badge/Docker-Ready-2496ED?logo=docker)](https://www.docker.com/)

[English](./README.en-US.md) | **中文** | [日本語](./README.ja-JP.md)

</div>

---

## 项目亮点

- **前端只有 React 19 + Ant Design**：目录是 `frontend/admin/react`。没有第二套前端
- **权限**：角色、部门、菜单、按钮和数据范围。接口鉴权用 Casbin，不提供 OPA
- **安全**：登录日志和操作日志、口令策略、图形验证码、登录限流、口令加密。详见[安全与等保合规](#安全与等保合规)
- **微服务 + 单体自由切换**：基于 go-kratos 微服务框架，但支持单体架构模式开发与部署，灵活适配团队规模
- **全栈代码生成**：Protobuf → Go API / TypeScript 客户端，Ent Schema → ORM。命令在 `backend/` 下：`make api`、`make ent`、`make ts`。
- **生产就绪**：JWT 鉴权、SSE 消息推送、异步任务调度、Swagger 文档、Docker 一键部署
### 本仓库的定位

**单租户管理端：Go 后端 + React 前端。** 没有第二套前端，也没有 AI 模块。

租户、套餐、配额的代码还在，侧栏菜单已隐藏。接口鉴权是 Casbin。

依赖在 `backend/third_party/`，许可证见 [THIRD_PARTY_LICENSES](./THIRD_PARTY_LICENSES)。命令见 [AGENTS.md](./AGENTS.md)。

---

## 从这里开始

- **想系统上手**：读 [文档索引](./docs/README.md)——文档分**教程层**（[渐进教程](./docs/tutorial/README.md) 8 章：从空环境走到独立开发业务模块、安全部署上线）与**参考层**（每个子系统的唯一权威说明）两层，索引里按你的角色（全栈采用者 / 后端 / 前端 / 运维与安全）标好了推荐阅读路径。
- **想先看到它跑起来**：往下依次是[演示地址](#演示地址)与[快速开始](#快速开始)。

---

## 代码仓库

这是裁剪后的工作副本，不随下面两个上游地址自动更新：

| 平台 | 地址 |
|------|------|
| 仓库 | 当前工作副本 |
| 上游 Gitee | <https://gitee.com/tx7do/go-wind-admin> |
| 上游 AtomGit | <https://atomgit.com/tx7do/go-wind-admin> |

---

## 演示地址

本仓库没有在线演示。本地前端只有 React，开发端口 15888。

| 端 | 说明 |
|---------|--------|
| 前端 | `frontend/admin/react` |
| 后端 | HTTP `:7788` |
| 账号 | `admin` / `Abcd@1234` |

- 后端 Swagger：服务起来之后打开 <http://127.0.0.1:7788/docs/>
- 默认账号密码：`admin` / `Abcd@1234`

---

## 快速开始

### 环境要求

| 工具 | 版本 |
|------|------|
| Go | 1.26+（以 `backend/go.mod` 为准） |
| Node.js | `^20.19.0 \|\| >=22.12.0`（Vite 8 需要）。**不要用 21.x**，也不要用 20.18 及以下 |
| pnpm | `>= 9`。React 端没有钉死 pnpm 版本 |
| Docker | 20.0+ |

### 环境脚本选型

- Linux / macOS 开发环境：`backend/scripts/env/install_unix_dev.sh`
- Linux / macOS 生产环境：`backend/scripts/env/install_unix_prod.sh`
- Windows 开发环境：`backend/scripts/env/install_windows_dev.ps1`

### Docker 两种部署模式

- **full_deploy 完整模式**：同步启动中间件+后端应用，适用于一键演示、生产部署
- **libs_only 依赖模式（推荐开发）**：仅启动中间件，应用本地 IDE 运行调试

### 后端启动

> 后端命令在 `backend/` 下用 Makefile（`make api`、`make ent`、`make -C app/admin/service run`）。见 [AGENTS.md](./AGENTS.md)。

**Linux / macOS：**

```shell
# 以下命令均在 backend/ 目录下执行（scripts/ 只在 backend/ 下）
cd backend

# 赋予脚本执行权限
# scripts 下有三层子目录，glob 会漏掉 env/lib 与 deploy/sse，用 find 一次到位
find ./scripts -name '*.sh' -exec chmod +x {} +

# 开发环境（推荐）
./scripts/env/install_unix_dev.sh
./scripts/docker/libs_only.sh
make -C app/admin/service run

# 生产环境
./scripts/env/install_unix_prod.sh
./scripts/docker/full_deploy.sh

# PM2 进程托管（生产进阶）
./scripts/deploy/pm2_service.sh
```

**Windows（PowerShell 管理员）：**

```powershell
# 以下命令均在 backend/ 目录下执行（scripts/ 只在 backend/ 下）
cd backend

# 放行脚本策略（首次仅需执行一次）
Set-ExecutionPolicy RemoteSigned -Scope CurrentUser

# 初始化环境
.\scripts\env\install_windows_dev.ps1

# 本地开发
.\scripts\docker\libs_only.ps1
make -C app/admin/service run

# 一键完整部署
.\scripts\docker\full_deploy.ps1
```

### 前端启动

本仓库只保留 React，目录是 `frontend/admin/react`。仓库根和 `frontend/admin/` 没有 `package.json`，在这两处安装会 ENOENT。

| 前端版本 | 目录 | 启动命令 | 端口 |
|---------|------|---------|------|
| React | `frontend/admin/react` | `pnpm dev` | 15888 |

```shell
# 先进入 React 目录再安装、启动。
# 不要在仓库根或 frontend/admin/ 下执行 pnpm install。
# 接口 http://localhost:7788 ，账号 admin / Abcd@1234
cd frontend/admin/react
pnpm install
pnpm dev                    # 端口 15888
```

> Node.js 用 `^20.19.0` 或 `>=22.12.0`，不要用 21.x。包管理用 pnpm。
>
> 开发端口在 `frontend/admin/react/.env.development` 的 `VITE_SERVER_PORT`（15888）。被占用时 Vite 自动 +1；后端 CORS 已放行 `15888` 与 `15889`。
>
> 在 `backend/` 执行 `make ts` 只生成 React 客户端。

---

## 技术栈

<table>
<tr><th>层级</th><th>技术</th></tr>
<tr><td><strong>后端框架</strong></td><td><code>Golang</code> · <code>go-kratos v2</code> · <code>Protobuf / Buf</code></td></tr>
<tr><td><strong>ORM</strong></td><td><code>Ent</code> · <code>MySQL</code> · <code>PostgreSQL</code></td></tr>
<tr><td><strong>中间件</strong></td><td><code>Redis</code>（compose 拉 <code>bitnami/redis:latest</code>，未固定版本；代码只用到 Set/Expire/Publish 一类的长期命令，未使用 Redis 8 专属命令） · <code>MinIO</code>（S3 兼容对象存储）</td></tr>
<tr><td><strong>认证授权</strong></td><td><code>JWT</code>（认证） · <code>Casbin</code>（接口鉴权）。配置不是 casbin 时启动失败</td></tr>
<tr><td><strong>实时通信</strong></td><td><code>SSE</code>（服务端推送） · <code>Asynq</code>（异步任务）</td></tr>
<tr><td><strong>前端</strong></td><td>只有 React，技术见「React 版」那一行</td></tr>
<tr><td><strong>已删除</strong></td><td>第二套前端、AI、SSO、MFA、程序内备份都不在本仓库</td></tr>
<tr><td><strong>不对外的代码</strong></td><td>租户、套餐、配额的页面还在，侧栏不展示</td></tr>
<tr><td><strong>React 版</strong></td><td><code>React 19</code> · <code>TypeScript</code> · <code>Vite</code> · <code>Zustand</code> · <code>Ant Design V6</code>（无 UMI） · <code>Tailwind CSS</code></td></tr>
<tr><td><strong>部署运维</strong></td><td><code>Docker</code> · <code>Docker Compose</code> · <code>PM2</code> · <code>Swagger UI</code></td></tr>
</table>

---

## 安全与等保合规

本项目的安全能力参照《网络安全等级保护 2.0》（二级/三级）技术要求设计，面向企业高隐私私有化部署场景开箱即用：

| 等保技术要求 | 落地实现 |
|------------|---------|
| **安全审计** | 登录日志和操作日志，记录客户端 IP（并解析归属地）与前端下发的 `X-Request-ID` 请求号。asynq 每日定时归档：库内留存 180 天（`AUDIT_RETENTION_DAYS` 可调），超期数据导出 JSONL 归档文件留痕，库瘦身与日志留存两不误 |
| **身份鉴别** | 口令复杂度（≥8 位、小写/大写/数字/符号四类取三）、历史口令复用检查（默认近 3 条）、口令有效期（默认 90 天），阈值经「参数管理」平台参数调整（内置参数启动时播种，环境变量配置已废弃）；图形验证码；Redis 登录失败限流（IP + 用户名双维度） |
| **访问控制** | 接口鉴权是 Casbin。角色、权限、接口的对应关系存在数据库里，权限变更会热更新。另有菜单和按钮权限、角色级数据范围、字段级权限 |
| **租户代码** | 隔离和套餐代码仍在，侧栏已隐藏。本仓库按单租户管理端使用，不作为多租户 SaaS 交付 |
| **数据保密性** | 登录、创建用户、重置密码和个人中心改密的口令均应用层 AES 加密传输、bcrypt 哈希存储；敏感任务配置 AES-256-GCM 静态加密（Ent Hook 透明加解密）；JWT RS256 非对称签名；refresh token 走 HttpOnly Cookie；传输层 TLS 由部署层启用（后端 `server.rest.tls` 配置或 nginx / 负载均衡终止） |
| **数据备份恢复** | 程序内不提供数据库备份。用数据库自带工具（如 `pg_dump`）或运维平台做全量备份，并演练恢复 |
| **前端安全** | React 的 `frontend/admin/react/scripts/deploy/nginx.conf` 在生产侧下发 X-Frame-Options / HSTS / Content-Security-Policy。构建期还会向 `index.html` 注入 CSP `<meta>` |

> **说明**：等保测评除技术要求外，还包含管理制度、物理环境、人员组织等非软件范畴的内容。本项目覆盖的是技术措施部分，可为私有化部署的等保测评准备提供直接支撑，但不能替代完整的等保测评流程。

---

## 功能列表

> 各列表页（业务数据与审计日志）均支持按当前筛选条件分页聚合导出，格式可选 CSV / XLSX（上限 1 万行）。

### 组织与权限

| 功能 | 说明 |
|------|-----|
| 用户管理 | 管理和查询用户，支持高级查询和按部门联动用户，用户可禁用/启用、设置/取消主管、重置密码、配置多角色、多部门和上级主管、一键登录指定用户等功能 |
| 租户管理 | 代码和页面还在（`/tenant/members`），侧栏不展示。本仓库不作为多租户 SaaS 使用 |
| 套餐与配额管理 | 代码和页面还在（`/tenant/plans`），侧栏不展示 |
| 角色管理 | 管理角色和角色分组，支持按角色联动用户，设置菜单授权、数据权限范围（五档 / 自定义组织单元集）与字段级权限（黑名单字段集），批量添加和移除员工 |
| 权限管理 | 管理权限分组、菜单、权限点，支持树形列表展示 |
| 组织管理 | 管理组织，支持树形列表展示 |
| 职位管理 | 用户职务管理，可作为用户标签。Excel 导入不解析「所属组织」列（`orgUnitId` 不在导入字段里） |
| 菜单管理 | 配置目录、菜单和按钮。菜单同步只在 React：可以清空重建，也可以按全路径增量合并并保留原菜单 ID 与角色授权 |

### 系统功能

| 功能 | 说明 |
|------|-----|
| 接口管理 | 管理接口，支持接口同步功能，主要用于新增权限点时选择接口，支持树形列表展示、操作日志请求参数和响应结果配置 |
| 字典管理 | 管理数据字典大类及其小类，支持按字典大类联动字典小类、服务端多列排序、数据导入和导出 |
| 任务调度 | 管理和查看任务及其任务运行日志，支持任务新增、修改、删除、启动、暂停、立即执行 |
| 文件管理 | 管理文件上传，支持文件查询、上传到 OSS 或本地、下载、复制文件地址、删除文件、图片支持查看大图功能 |
| 账号登录 | 支持用户名 / 邮箱 / 手机号多标识登录，并带图形验证码与登录限流 |
| 验证码发信 | 个人中心换绑邮箱、换绑手机读 `app/admin/service/configs/mail.yaml` 的 SMTP 和短信账号。host 或 provider 留空表示这一路未配置；发送不再进任务队列 |
| 在线用户 | 查看当前在线会话（用户、租户、客户端类型、登录 IP、User-Agent、设备 ID、登录时间），支持关键词过滤、分页（登录时间倒序）与单会话强制下线，30 秒自动刷新 |
| 参数管理 | 平台全局系统参数的键值管理（区别于业务字典），内置参数启动时播种、禁删可改；服务侧经缓存 accessor 读取，多实例部署下参数变更经 Redis 发布订阅广播失效各实例缓存 |
| 语言管理 | 管理系统支持的多语言，配置语言名称、语言代码、本地名称、启用与默认状态 |

### 消息与日志

| 功能 | 说明 |
|------|-----|
| 消息分类 | 管理消息分类，用于消息管理里的分类选择。分类是**平铺的一层**（`sys_internal_message_categories` 无 parent_id 列，删除也只删本行、不做树形级联） |
| 消息管理 | 管理消息，支持按发送范围（全员 / 指定用户）发送与消息撤销，全员广播走异步任务队列投递（断点恢复、幂等），可查看用户是否已读和已读时间 |
| 站内信 | 只看自己的收件箱，可查看、删除、标为已读、全部已读。平台管理员在用户详情里仍可查看指定用户 |
| 登录日志 | 登录日志列表查询，记录用户登录成功和失败日志，支持 IP 归属地记录 |
| 操作日志 | 操作日志列表查询，记录用户操作正常和异常日志，支持 IP 归属地记录与资源对象定位，查看操作日志详情 |

### 个人中心

| 功能 | 说明 |
|------|-----|
| 个人中心 | 个人信息展示和修改，查看最后登录信息，密码修改、邮箱绑定 / 换绑（验证码校验）等功能 |
| 我的活跃会话 | 查看本人当前在线的会话列表（登录 IP、设备、登录时间等），当前会话仅展示不可下线，其余会话可单独下线 |

---

## 项目结构

```
go-wind-admin/
├── backend/                        # 后端项目
│   ├── api/                        # Protobuf API 定义与生成代码
│   │   ├── protos/                 # .proto 源文件（按领域分层）
│   │   └── gen/go/                 # buf 生成的 Go 代码
│   ├── app/admin/service/          # Admin 服务应用
│   │   ├── cmd/server/             # 入口 (main.go, wiring_ent.go 依赖装配)
│   │   ├── configs/                # 配置文件 (YAML)
│   │   └── internal/               # 业务核心（data/service/server）
│   ├── pkg/                        # 公共包
│   │   ├── oss/                    # 对象存储（MinIO）
│   │   ├── eventbus/               # 事件总线
│   │   └── ...                     # 其他工具包
│   ├── scripts/                    # 部署脚本（env/docker/deploy）
│   └── sql/                        # 演示数据 SQL（默认数据由服务启动自动播种）
├── frontend/admin/                 # 前端（只保留 React）
│   └── react/                      # React 19 + Ant Design V6
└── docs/                           # 项目文档
```

---

## 截图展示

<table>
    <tr>
        <td><img src="./docs/images/admin_login_page.png" alt="后台用户登录界面"/></td>
        <td><img src="./docs/images/admin_dashboard.png" alt="后台分析界面"/></td>
    </tr>
    <tr>
        <td><img src="./docs/images/admin_user_list.png" alt="后台用户列表界面"/></td>
        <td><img src="./docs/images/admin_user_create.png" alt="后台创建用户界面"/></td>
    </tr>
    <tr>
        <td><img src="./docs/images/admin_tenant_list.png" alt="后台租户列表界面"/></td>
        <td><img src="./docs/images/admin_tenant_create.png" alt="后台创建租户界面"/></td>
    </tr>
    <tr>
        <td><img src="./docs/images/admin_org_unit_list.png" alt="组织单位列表界面"/></td>
        <td><img src="./docs/images/admin_org_unit_create.png" alt="创建组织单位界面"/></td>
    </tr>
    <tr>
        <td><img src="./docs/images/admin_position_list.png" alt="后台职位列表界面"/></td>
        <td><img src="./docs/images/admin_position_create.png" alt="后台创建职位界面"/></td>
    </tr>
    <tr>
        <td><img src="./docs/images/admin_role_list.png" alt="后台角色列表界面"/></td>
        <td><img src="./docs/images/admin_role_create.png" alt="后台创建角色界面"/></td>
    </tr>
    <tr>
        <td><img src="./docs/images/admin_permission_list.png" alt="后台权限列表界面"/></td>
        <td><img src="./docs/images/admin_permission_create.png" alt="后台创建权限界面"/></td>
    </tr>
    <tr>
        <td><img src="./docs/images/admin_menu_list.png" alt="后台目录列表界面"/></td>
        <td><img src="./docs/images/admin_menu_create.png" alt="后台创建目录界面"/></td>
    </tr>
    <tr>
        <td><img src="./docs/images/admin_task_list.png" alt="后台调度任务列表界面"/></td>
        <td><img src="./docs/images/admin_task_create.png" alt="后台创建调度任务界面"/></td>
    </tr>
    <tr>
        <td><img src="./docs/images/admin_dict_list.png" alt="后台数据字典列表界面"/></td>
        <td><img src="./docs/images/admin_dict_entry_create.png" alt="后台创建数据字典条目界面"/></td>
    </tr>
    <tr>
        <td><img src="./docs/images/admin_internal_message_list.png" alt="后台站内信消息列表界面"/></td>
        <td><img src="./docs/images/admin_internal_message_publish.png" alt="后台发布站内信消息界面"/></td>
    </tr>
    <tr>
        <td><img src="./docs/images/admin_login_audit_log_list.png" alt="后台登录日志界面"/></td>
        <td><img src="./docs/images/admin_api_audit_log_list.png" alt="后台操作日志界面"/></td>
    </tr>
    <tr>
        <td><img src="./docs/images/admin_api_list.png" alt="API列表界面"/></td>
        <td><img src="./docs/images/api_swagger_ui.png" alt="后端内置Swagger UI界面"/></td>
    </tr>
</table>

## 配套工具

代码生成在 `backend/` 下执行：`make api`、`make openapi`、`make ent`、`make ts`。运行服务：`make -C app/admin/service run`。详见 [AGENTS.md](./AGENTS.md)。

## 社区与贡献

欢迎参与 GoWind Admin 的建设。以下文档说明如何贡献代码、报告问题与反馈安全漏洞：

- [贡献指南](./CONTRIBUTING.md) —— 开发环境、代码生成约定、提交规范与 PR 流程
- [行为准则](./.github/CODE_OF_CONDUCT.md) —— 社区互动预期
- [安全策略](./SECURITY.md) —— 漏洞上报流程与覆盖范围
- [更新日志](./CHANGELOG.md) —— 版本变更记录
- Issue 模板：[Bug 报告](./.github/ISSUE_TEMPLATE/bug_report.md) · [功能请求](./.github/ISSUE_TEMPLATE/feature_request.md)
- [PR 模板](./.github/PULL_REQUEST_TEMPLATE.md)

## 联系我们

- 微信个人号：`yang_lin_bo`（备注：`go-wind-admin`）
- 掘金专栏：[go-wind-admin](https://juejin.cn/column/7541283508041826367)

## 致谢

[![JetBrains](https://resources.jetbrains.com/storage/products/company/brand/logos/jb_beam.svg)](https://jb.gg/OpenSource)

感谢 JetBrains 提供免费的 GoLand & WebStorm 开源授权。
