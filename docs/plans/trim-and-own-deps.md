# 裁剪与依赖自主化实施计划

> 编写日期：2026-10-25。文中所有数字都是当天在本仓实测的，复测命令见附录。
> 读者：负责实施的 agent。**动手前先读完全文和根目录 `AGENTS.md`**。

## 1. 目标

把本仓变成一个**只保留基础 RBAC 后台、源码完全自有**的项目：

1. **裁剪**：删掉用不上的功能模块，清单见第 3.2 节，全部已由用户确定。
2. **依赖自主**：把上游作者（GitHub `tx7do` 及其 `go-wind` 组织）的所有代码并进本仓，**作为本仓主模块的普通包**。完成后本仓只依赖社区维护的独立第三方库（kratos、ent、redis、asynq、casbin 等），不再依赖该作者发布的任何模块、proto 或工具。

### 不做的事（明确排除）

- **不拆多租户底层**。49/65 张表带 `tenant_id`，隔离逻辑在 ent 隐私层和 JWT 里，拆掉等于重写数据层。单租户使用时所有数据都在 `tid=0`（平台）下，租户闸门只在 `tid>0` 时生效，底层保持休眠即可。只隐藏租户相关菜单。
- **不拆组织/岗位/成员关系、数据范围、字段权限**。和用户模型、令牌绑得很深。
- **不改业务行为**。吸收依赖阶段必须做到「生成代码除 import 路径外逐字节一致」（第 7.3 节）。
- **不跟上游同步**。本仓与上游无共同 git 历史；吸收完成后上游的修复只按需手工移植。

## 2. 执行规则（所有阶段通用）

1. **一个阶段一个分支**：从 `main` 切出 `chore/trim-phase-<N>`，阶段内可以多次提交。**不要 push、不要合并**，完成后向用户汇报，由用户合并。
2. **门禁全绿才能声称完成**（第 10 节）。任何新报错都按自己引入的处理，不存在「可以忽略的既有错误」。
3. **生成代码不手改**：`backend/api/gen/**`、`internal/data/ent/**`（`schema/` 除外）、`frontend/admin/react/src/api/generated/**` 都要通过重新生成来改。唯一例外见第 6.3 节。
4. 遵守 `AGENTS.md` 全仓铁律，尤其是**不吞错**。
5. **删除前先 grep**：每删一个模块，先用第 5.1 节的检索方式找出全部引用点，确认没有遗漏再删。删完以后，残留的引用必须是零。
6. **遇到计划没覆盖的耦合，停下来汇报**，不要自行扩大删除范围，也不要为了让编译通过而改业务语义。
7. 每个阶段结束时更新本文第 11 节「执行记录」：做了什么、门禁结果、遗留问题。

## 3. 决策记录

### 3.1 已确定

| 决策 | 内容 |
|---|---|
| 路线 | 在本项目上裁剪（路线 A），不换底座 |
| 前端 | 只保留 React。vue-element、vue-vben **删除**（工作区里已经删了，第 0 阶段提交） |
| ORM | 只保留 Ent，**删除 GORM 平行后端** |
| AI | **整体删除** |
| 多租户 | 底层保留休眠；租户、套餐、配额只**隐藏菜单**，代码不删（见第 5.4 节） |
| Go module 名 | **保持 `go-wind-admin`，不改** |
| 依赖吸收方式 | 源码并入主模块并改写 import 路径，**不用 `replace`**。最终 `go.mod` 里不能出现 `tx7do` |
| 吸收代码位置 | `backend/third_party/<原仓库名>/<原包路径>`，import 路径为 `go-wind-admin/third_party/...` |
| gow CLI | 弃用，改用 Makefile 里的等价命令（`make api/openapi/ts/ent`、`app.mk` 的 `run`） |
| 数据库备份 | 交给程序外的第三方方案（数据库自带工具或运维平台），**程序内的备份代码全部删除** |

### 3.2 模块去留（用户已确定，2026-10-25）

> **实施 agent：严格按「决定」列执行。表里没有列出、又和这些模块有牵连的功能，不要自行处理，先回头问用户。**

| 模块 | 决定 | 牵连点（删除时重点检查） |
|---|---|---|
| 脚本系统（Lua/JS 插件） | **删除** | `pkg/scripting`、`script_*` service/repo、实体钩子 `script_entity_hooks.go`、`script_runtime.go`、`pkg/task` 的脚本任务桥；删除后 go-scripts、goja、gopher-lua 依赖应该消失 |
| 通知域：渠道、规则、模板、投递、偏好 | **删除** | `notification_*`、`notifier.go`、`notification_platform_guard.go`、`internal/data/channel/`；**站内信和它之间有接缝，见第 5.4 节** |
| 监控告警（monitor_alert） | **删除** | 依赖通知域，和通知域一起删 |
| 站内信（internal_message） | **保留** | 本体、分类、收件人、SSE 推送都保留；只去掉它和通知域之间的桥接 |
| SSO（OIDC 登录） | **删除** | `authentication_sso.go`、`sso_state_cache.go`、`sso.proto`、前端 `sso-callback` 页和登录页的 SSO 按钮、CI `sso-smoke.yml`、`auth.yaml` 里的 SSO 配置 |
| AK/SK 机器令牌 | **删除** | `access_key_*`、认证链里的机器令牌分支（`CreateMachineToken`、`machine` 角色）、`access_key` proto 包 |
| MFA（TOTP） | **删除** | `mfa_service.go`、`mfa_challenge_cache.go`、`user_mfa_factor` 表、登录流程里的 MFA 闸门、个人中心里的 MFA 设置、前端 `mfa-challenge` 页；依赖 `pquerna/otp` 应该消失 |
| 登录策略（IP/时段限制） | **删除** | `login_policy_*`、`login_policy_checker.go`、登录流程里的策略检查、`system/login-policy` 页。**注意区分**：登录限流（`login_rate_limiter.go`）和图形验证码**不属于**登录策略，保留 |
| 找回密码 | **删除** | `authentication_forgot_password.go`、前端 `forgot-password` 页和登录页入口。`transactional_mail.go` 和 `pkg/mailtext` 还被 `user_profile_contact.go`、`plan_quota_watermark_service.go` 使用，**不要连带删除**，见第 5.4 节 |
| 服务器监控、Redis 缓存监控、任务监控 | **删除** | `server_monitor_*`、`redis_cache_monitor_*`、`task_monitor_service.go` 及对应 proto、页面。**任务调度本身（`task_service.go`）保留** |
| 数据库备份 | **删除** | `backup_*`、`backup_restore.go`、`backup_cleanup.go`、相关定时任务类型、`backend/scripts/backup/`、`docs/backup-restore-drill.md`，以及部署文档里的程序内备份说明 |
| 租户、套餐、配额 | **只隐藏菜单** | 页面、接口、仓储、闸门代码全部保留；做法见第 5.4 节 |
| 扩展审计：API 审计、数据访问审计、权限审计、策略评估日志 | **删除** | 四类日志各自的 proto、表、repo、service、页面（`log/api-audit-log`、`log/data-access-audit-log`、`log/permission-audit-log`、`log/policy-evaluation-log`）；采集中间件在 `pkg/middleware/logging/` 里是**各自独立的子中间件**（`logging.go:101-104`），只删这三个（api、permission、data_access），**login 和 operation 两个保留**；数据访问审计的 `audit_driver_wrapper.go` 包在数据库 driver 外层，删除后 ent client 直接用原始 driver；策略评估日志包在 authz 引擎外层（`rest_server.go:115` 的 `newEvalLoggingEngine`），删除后直接用引擎 |
| 审计归档（`audit_log_archive`）、审计导出（`audit_export_service.go`） | **保留，范围缩到登录日志和操作日志** | 归档负责 `sys_login_audit_logs`、`sys_operation_audit_logs` 的保留期清理，删掉它这两张表会一直增长；导出同样作用在这两类日志上。只从中去掉已删除的日志类型（比如 `audit_log_archive_repo.go` 里 `sys_api_audit_logs` 那一段），其余保留 |

**保留**：用户、角色、权限、权限组、菜单、接口（Api 表）、组织、岗位、字典、参数配置、文件、语言、站内信、登录日志、操作日志、审计归档与导出（仅登录和操作日志）、在线会话、任务调度、登录限流、图形验证码、仪表盘（去掉 AI 卡片）、个人中心（去掉 MFA 设置）。

### 3.3 鉴权引擎

| 问题 | 决定 |
|---|---|
| 接口级鉴权引擎 | **切到 casbin，删除 OPA 和 zanzibar**（用户没有异议，按此执行） |

> ⚠️ **重要发现**：`backend/app/admin/service/configs/auth.yaml:68` 当前是 `authz.type: "noop"`，**接口级鉴权全部放行**。在这个配置下，RBAC 只控制前端菜单和按钮的显隐，后端接口不做拦截（见 `docs/tutorial/05-permission-model.md` 第 3 节）。要做真正的 RBAC，必须切到 `casbin`。这一步放在第 1 阶段末尾单独做、单独验证（第 5.5 节）。

### 3.4 实测基线（2026-10-25）

- 后端：手写代码（service + data + schema，不含测试和生成代码）约 5.2 万行；GORM 平行后端约 9200 行；`pkg/scripting` 约 3700 行。
- 前端（只有 React）：`src` 约 6.8 万行，其中 `pages/app` 约 2.7 万行。
- `go build ./...` 通过。React 端 `node_modules` 还没装，typecheck 基线未测，**第 0 阶段要补测**。
- 作者的依赖：`go.mod` 里 63 个 tx7do 模块，**实际参与编译的有 141 个包、约 7.8 万行**，分布见附录 A。
- 作者的非 Go 依赖：BSR 上的 proto（`buf.build/tx7do/pagination`、`buf.build/go-wind/redact`）；Makefile 里用 `@latest` 安装的 `protoc-gen-go-redact`、`protoc-gen-typescript-http`、`gow`。
- 生成代码里引用作者的包：ent 生成代码引用 `go-crud/entgo/mixin`（63 处）、`go-crud/entgo/rule`、`go-crud/viewer`；`api/gen` 引用分页 pb（211 处）和 `redact/v1`（6 处）。

---

## 4. 第 0 阶段：基线与止血

1. 工作区里有未提交的改动：删除 vue-element/vue-vben 前端（1674 个文件）和若干文档、配置修改。**用户已确认删除两套 Vue 前端**。先核对 `git status` 里除了这些以外没有别的改动（有的话先问用户），然后在 `main` 上单独提交一次，提交信息写明「删除 vue-element / vue-vben 前端，只保留 React」，再切第 0 阶段分支。提交前顺手检查一遍：仓库里搜 `vue-element`、`vue-vben`，残留只能出现在历史说明性文字里，构建脚本、CI、Makefile、CORS 配置里不能再有。
2. 安装 React 依赖（`frontend/admin/react` 下执行 `pnpm install`），跑 `npm run typecheck` 和 `npm run build`，记录基线结果。
3. 后端跑 `go build ./...` 和 `go test ./...`，记录基线结果：哪些测试因为需要 Postgres 或 pgvector 而 skip，哪些失败。**已有的失败要先汇报**，不要和后面的阶段混在一起。
4. **止血**：把 `backend/Makefile` 里 `plugin`、`cli` 两个目标中**所有** `@latest` 换成当前实际安装的版本号（用 `go version -m $(which <工具>)` 查）。重点是三个 tx7do 工具，它们是作者唯一能直接改到本仓产物的地方。
5. 起一套干净的本地环境（`backend/scripts/docker/libs_only.sh` + 新建空数据库），用 `admin / Abcd@1234` 登录，截图记录菜单全貌，作为之后对比的基线。

**完成标准**：门禁结果已记录；Makefile 里不再有 `@latest`；本地能登录。

---

## 5. 第 1 阶段：裁剪

### 5.1 每个模块的通用删除流程

按「从契约往外删」的顺序，避免生成链把删掉的文件又生成回来：

1. **proto**：删 `backend/api/protos/<域>/...` 和 BFF 的 `backend/api/protos/admin/service/v1/i_<模块>.proto`；在其他 proto 里清理对它的 import 和字段引用。
2. **重新生成**：`make api`、`make openapi`（会更新 `cmd/server/assets/openapi.yaml`，「接口同步」读的是它）、`make ts`。删除对应的旧生成文件，因为 buf 不一定会清理孤儿文件，要逐一核对。
3. **ent schema**：删 `internal/data/ent/schema/<表>.go`，跑 `make ent`。生产库里已存在的表不会被自动删除，在执行记录里写明需要手工 DROP 的表名。
4. **data / service**：删 repo、service 及其测试文件。
5. **装配**：从 `cmd/server/wiring_ent.go` 删掉构造行和传参；从 `internal/server/{rest,sse,asynq}_server.go` 删掉注册。
6. **常量与种子**：`pkg/constants/default_data.go` 删菜单种子、权限种子；`pkg/constants/module_mapping.go` 删模块映射；如果模块有对应的 `Module_*` 枚举，评估是否从 proto 里删（不删也可以，但要在记录里写明）。
7. **任务与事件**：`pkg/task/` 里对应的任务类型、SSE 事件类型、脚本模块（`pkg/scripting/api/module_*.go`）。
8. **前端**：`src/pages/app/<模块>`、`src/router/modules/<模块>.tsx`、`src/api/hooks/<模块>*.ts`、`src/locales/*/_modules/<模块>*.json`，以及其他页面里引用它的组件。
9. **文档**：删专题文档；清理 `docs/README.md`、根 `README*.md`、`AGENTS.md`、`backend/AGENTS.md` 里的索引和描述。
10. **残留检查**：用模块关键字在 `backend/`、`frontend/admin/react/src/`、`docs/` 里 grep，结果必须是零，或者每一处都能说明为什么保留。

**菜单种子注意**：菜单只在 `sys_menu` 表为空时播种（`count==0` 守卫）。**验证必须用新建的空库**，旧库里的菜单不会因为删了种子而消失。

### 5.2 删除 GORM 平行后端（已确定）

GORM 后端是**没接上线的半成品**（见 `wiring_gorm.go` 文件头的阻塞说明），默认构建从来不编译它。

- 删 `backend/app/admin/service/internal/data/gorm/` 整个目录（含 `models/`）、`cmd/server/wiring_gorm.go`。
- `cmd/server/wiring_ent.go` 去掉 `//go:build !gorm_backend` 构建标签，清理注释里提到 GORM 的段落。
- `go mod tidy`，确认 `github.com/tx7do/go-crud/gorm`、`gorm.io/*`、`github.com/tx7do/go-wind`（只有 gorm_client 用它的 log）从 `go.mod` 消失。
- 文档：`backend/AGENTS.md`、`docs/backend_project_struct.md`、`docs/plan_billing.md`、`docs/tenant_isolation.md`、`docs/notification_domain_design.md` 里关于 GORM 的段落删掉或改写。

### 5.3 删除 AI 模块（已确定）

已知牵连点（实施时仍要按第 5.1 节第 10 步全量 grep）：

| 位置 | 内容 |
|---|---|
| proto | `api/protos/ai/` 整个目录；`api/protos/admin/service/v1/i_ai_*.proto`（9 个） |
| ent schema | `ai_agent_app`、`ai_agent_run`、`ai_chunk`、`ai_conversation`、`ai_doc`、`ai_knowledge_base`、`ai_message`、`ai_provider`、`ai_usage_log` |
| data | `internal/data/ai_*`，含 `ai_vector_migrate.go`（pgvector 迁移）及其测试 |
| service | `internal/service/ai_*` 及其测试 |
| pkg | `pkg/ai/`；`pkg/task/ai_audit_digest.go`、`ai_doc_reindex.go`、`ai_query_run.go`；`pkg/scripting/api/module_ai.go`、`pkg/scripting/sandbox_ai_test.go`，以及 `pkg/scripting/runtime_javascript.go` 里注册 ai 模块的地方 |
| 引用 AI 的非 AI 文件 | `wiring_ent.go`、`server/{rest,sse,asynq}_server.go`、`service/data_export_service.go`、`service/task_service.go`、`service/plan_quota_watermark_service.go`、`service/script_runtime.go`、`data/quota_watermark_scan.go`（AI token 配额）、`pkg/constants/module_mapping.go`（`Module_AI` 映射，以及 `component[:7] == "app/ai/"` 分支）、`pkg/constants/default_data.go`（第 1073～1170 行附近的 AI 菜单种子） |
| 配额 | AI token 用量配额（`plan_quota` 里的 AI 计量类型），连同水位扫描一起清理 |
| 前端 | `src/pages/app/ai/`、`src/router/modules/ai.tsx`、`src/api/hooks/ai-*.ts`（8 个）、`src/locales/{zh-CN,en-US}/_modules/ai*.json`（每种语言 7 个）、`src/components/common/AiGenerateButton/`（`permission/role/components/RoleDrawer.tsx` 在用，要一起改）、`src/pages/app/dashboard/components/AiInsightsCard.tsx` 及仪表盘里对它的引用 |
| 文档 | `docs/ai_module.md`、`docs/ai_agent_orchestration.md`、`docs/tutorial/10-ai-module.md`，以及各 README、AGENTS.md 里关于 AI 的段落；根 `AGENTS.md` 里「AI 功能本地调试：`.zcode/tmp/mock_llm.py`」那一条 |
| 依赖 | `go mod tidy` 后 `github.com/sashabaranov/go-openai`、`github.com/tx7do/go-wind-plugins/ai/openai` 应该消失 |
| 部署 | 部署文档和 docker-compose 里关于 pgvector 的要求改回普通 PostgreSQL |

### 5.4 删除第 3.2 节确定的模块，隐藏租户菜单

按第 3.2 节「决定」列执行，每个模块套用第 5.1 节流程。**一个模块一次提交**，便于回滚。

**建议顺序**（被依赖的后删）：

1. 服务器监控、Redis 缓存监控、任务监控（最独立，先删）
2. 数据库备份
3. 脚本系统（删完以后，AI 那一步里 `pkg/scripting` 的 ai 模块就不用单独处理了；如果先做了 AI，这里一并清理）
4. 监控告警，然后是通知域
5. SSO、AK/SK
6. 找回密码
7. MFA、登录策略（都在登录流程里，放在一起做，做完重点回归登录）
8. 扩展审计（API、数据访问、权限、策略评估四类日志），同时把审计归档和导出的范围缩到登录日志和操作日志

**几个已知耦合，删的时候特别注意：**

- **审计日志**：删完扩展审计后回归：登录一次，登录日志有记录；做一次增删改，操作日志有记录；两类日志都能导出；归档任务能正常跑完（可以临时把保留期调短来验证）。

- **站内信和通知域**：`internal_message_sender.go`、`internal_message_service.go` 引用了通知域（还有一个专门测接缝的测试 `internal_message_notify_seam_sqlite_test.go`）。删通知域时只去掉站内信「转发到通知渠道」这条桥，**站内信本身的发送、收件箱、未读数和 SSE 推送必须照常工作**。删完后在前端发一条站内信，确认收件人能实时收到。
- **邮件发送**：`transactional_mail.go` 和 `pkg/mailtext` 在删完找回密码、通知域、监控告警之后，仍被 `user_profile_contact.go`（个人中心改邮箱、手机的验证）和 `plan_quota_watermark_service.go`（配额水位提醒）使用。**保留邮件基础设施**；如果全部模块删完后它确实已经没有调用方，再在执行记录里提出，由用户决定。
- **登录流程**：MFA 闸门、登录策略检查、SSO 分支、机器令牌分支都在 `authentication_service.go` 和认证中间件里。删完后回归：账号密码登录、刷新令牌、退出登录、错误密码触发限流、图形验证码。
- **个人中心**：MFA 设置入口在个人中心，要一并删掉，其他功能（资料、改密码、改联系方式）保留。

**隐藏租户、套餐、配额菜单**（不删代码，以后想恢复只要改回标记）：

- 前端（React 默认 `accessMode: "frontend"`，菜单来自 `src/router/modules/*.tsx`）：在 `src/router/modules/tenant.tsx` 的顶层路由上设 `hideInMenu: true`。其他模块里如果有租户相关入口（比如「我的租户用量」），一并隐藏。
- 后端菜单种子（切到后端菜单模式时生效）：`pkg/constants/default_data.go` 里 `TenantManagement`（Id 10）及其子菜单的 meta 设 `HideInMenu`，写法参照同文件已有的 `HideInMenu` 用法。
- 页面路由、接口、权限码都保留，只是菜单里看不到。验收：用空库登录，菜单里没有租户、套餐、配额；直接访问 `/tenant` 地址，页面仍能打开（说明代码没坏）。

### 5.5 接口级鉴权切到 casbin

1. 先读 `docs/tutorial/05-permission-model.md` 第 3 节和 `docs/frontend_authority.md`。
2. 把 `auth.yaml` 的 `authz.type` 改为 `casbin`，同时把同一行注释里的可选值改成只有 `casbin`。
3. **验证**（新建空库）：
   - `admin` 登录后所有菜单和接口可用。
   - 新建一个角色，只授予「用户管理」的查看权限，再新建一个属于该角色的用户。该用户调用用户列表接口返回 200；调用角色创建等未授权接口**返回 403**。
   - 在管理页改角色权限后，不重启服务，新权限立即生效（策略会热重建）。
4. 把以上验证步骤写进执行记录。
5. OPA 和 zanzibar 引擎的代码在第 2 阶段吸收 `go-utils/authorizer` 和 `kratos-authz` 时**不吸收**，并删掉 `newEngine` 里对应的分支。`newEngine` 的 `default` 分支目前会悄悄退回 noop，要改成**启动报错**，防止配置拼错时接口全部放行。

**第 1 阶段完成标准**：门禁全绿；在新建的空库里登录，菜单只剩保留的模块，每个页面都能打开、能完成一次增删改查；第 5.1 节第 10 步的残留检查结果为零。

---

## 6. 第 2 阶段：吸收 Go 依赖

### 6.1 确定吸收清单

在第 1 阶段完成后的代码上重新计算（裁剪会让清单明显变短）：

```bash
cd backend
go list -deps -f '{{if not .Standard}}{{.ImportPath}}{{end}}' ./... | grep '^github.com/tx7do/'
```

**以这条命令的输出为准**，只吸收实际参与编译的包，不整仓拷贝。另外要把 ent 生成代码引用的包（`go-crud/entgo/mixin`、`go-crud/entgo/rule`、`go-crud/viewer`）也算进来；它们本来就在编译图里，这里只是核对一遍。

### 6.2 拷贝源码

1. 源码**从 Go module 缓存里拷**（`go env GOMODCACHE`），版本必须和当前 `go.mod`（及 `go list -m all`）里的完全一致。**不要从 GitHub 拉最新代码**。
2. 目标路径：`backend/third_party/<原仓库名>/<模块内包路径>/`。例如 `github.com/tx7do/go-crud/entgo/mixin` 放到 `backend/third_party/go-crud/entgo/mixin/`。
3. 只拷 `.go` 源文件和包内用到的嵌入资源（`//go:embed`）。原包的 `_test.go` **一起拷过来**，作为回归测试。不拷子模块的 `go.mod` 和 `go.sum`。
4. 每个原仓库目录下放：原 `LICENSE`（MIT，原样保留，不能改）；`VERSION.md`，记录原模块路径、版本号、拷贝日期和本仓做过的修改。
5. 不吸收：OPA 和 zanzibar 引擎（见第 5.5 节），以及第 1 阶段删掉的模块所依赖的包。

### 6.3 改写 import 路径

1. 把全仓所有 `"github.com/tx7do/<仓库>/<路径>"` 改写为 `"go-wind-admin/third_party/<仓库>/<路径>"`。范围包括：手写代码、`third_party` 内部的互相引用，以及 **ent schema**。
2. ent 生成代码改完 schema 后跑 `make ent` 重新生成，**不要手改**。
3. `api/gen` 里的分页 pb 和 redact 引用，**本阶段允许临时用脚本改写**（第 2 节规则 3 的唯一例外）。第 3 阶段会把 proto 源码搬进本仓后重新生成，并要求结果和这里的改写逐字节一致。
4. 一些写在 proto 里的 Go 类型引用（例如 `go_package`），以及 `kratos-bootstrap/api/gen/go/conf/v1` 这类配置 pb，在第 3 阶段一并处理。本阶段先把生成好的 pb 当作普通源码拷进来。
5. `go mod tidy`。原来经由作者的库间接引入的第三方库，现在会变成本仓的直接依赖，这是预期结果。

### 6.4 阶段验收

- `grep -rn 'github.com/tx7do' backend --include='*.go'` 结果为零。
- `go.mod`、`go.sum` 里没有 `tx7do`。
- 断网（`GOFLAGS=-mod=mod GOPROXY=off`）也能 `go build ./...`。前提是模块缓存里已经有剩余的第三方依赖。
- `go test ./...`，包括 `third_party` 下吸收进来的测试，结果和第 0 阶段的基线一致。

---

## 7. 第 3 阶段：吸收 proto 和代码生成工具

### 7.1 Proto

1. 从本地 buf 缓存（或 `buf export`）导出 `buf.build/tx7do/pagination` 和 `buf.build/go-wind/redact` 在 `buf.lock` 里锁定的 commit 对应的 `.proto` 文件，放到 `backend/api/protos/` 下（比如 `pagination/v1/`、`redact/v1/`），保持原 import 路径不变，这样业务 proto 不用改。
2. 把这两个 proto 的 `go_package` 改成 `go-wind-admin/third_party/...`（或 `api/gen/go/...`，和第 6.3 节保持一致）。
3. 从 `backend/api/buf.yaml` 的 `deps` 里删掉这两项，`buf dep update`。
4. kratos-bootstrap 的配置 proto（`conf/v1`）：把源 proto 一起拷进来，纳入生成链，避免 `third_party` 里留着一份没有源码的 pb。
5. 其余 BSR 依赖（googleapis、kratos/apis、gnostic、protoc-gen-validate）属于独立第三方，保留不动。

### 7.2 生成插件

1. `protoc-gen-go-redact`、`protoc-gen-typescript-http`：按第 0 阶段 Makefile 锁定的版本，把源码从模块缓存拷到 `backend/third_party/tools/<插件名>/`。如果它们依赖作者的其他包，一并吸收。
2. 生成插件的 runtime 包（`protoc-gen-go-redact/redact/v1`）也要吸收；生成代码会 import 它。
3. Makefile 的 `plugin` 目标改为从本地源码 `go build` 或 `go install ./third_party/tools/...`。
4. 弃用 gow：删掉 Makefile `cli` 目标里的 gow 安装；把 `AGENTS.md`「后端任务：gow 优先」一节改成 make 命令（`make api` / `make openapi` / `make ts` / `make ent` / `app.mk` 的 `make run`）；清理 `backend/README.md` 和教程里的 gow 用法。

### 7.3 一致性校验（本阶段的核心验收）

1. 依次执行 `make api`、`make openapi`、`make ts`、`make ent`。
2. `git diff` 生成目录（`backend/api/gen`、`internal/data/ent`、`cmd/server/assets/openapi.yaml`、`frontend/admin/react/src/api/generated`）。**和第 2 阶段末尾相比，差异必须为零**；如果有差异，逐条解释，并确认不是行为变化。
3. 全仓 `grep -rn 'tx7do\|go-wind-toolkit\|go-wind-plugins\|buf.build/go-wind'`，除了 `third_party/*/LICENSE`、`VERSION.md` 和 `THIRD_PARTY_LICENSES` 外，结果为零。

---

## 8. 第 4 阶段：收尾与防回流

1. **CI 防回流**：新增一个 workflow（或脚本 `scripts/check-no-upstream-deps.sh`）。`go.mod`、任意 `.go` 文件、`buf.yaml`、Makefile 里出现 `github.com/tx7do`、`buf.build/tx7do`、`buf.build/go-wind` 就报错。
2. **许可证汇总**：在仓库根目录生成 `THIRD_PARTY_LICENSES`，列出 `third_party` 下每个来源的原仓库、版本和完整 MIT 原文。
3. **文档**：
   - `README*.md`：去掉三端前端、多租户 SaaS、AI 等宣传段落，改成本项目的实际定位。
   - `AGENTS.md`：更新仓库布局（新增 `third_party/`）、门禁、后端命令；删掉已不存在模块的文档索引。
   - `backend/third_party/README.md`：说明这些代码的来源、版本和维护规则：「这是本仓自有代码，可以直接改；改了要在对应 `VERSION.md` 里记一笔」。
4. **把现有工作流（`.github/workflows/`）跑一遍**：删掉已不存在模块的 workflow（比如删了 SSO 就删 `sso-smoke.yml`），其余必须通过。
5. 在新建的空库上做一次完整冒烟测试（第 10 节最后一项）。

---

## 9. 风险与回滚

| 风险 | 应对 |
|---|---|
| 删模块漏掉引用，运行时报错（比如菜单还在、点进去 404，或者定时任务找不到处理函数） | 第 5.1 节第 10 步的残留 grep；每个阶段用空库做全量冒烟测试 |
| 删 proto 后旧的生成文件残留 | 重新生成后核对生成目录，删掉孤儿文件 |
| 切 casbin 后管理员被锁在门外 | 第 5.5 节的验证步骤；出问题先回滚这一处配置，不要连带回滚整个阶段 |
| 吸收时版本拷错，行为悄悄变化 | 只从模块缓存按 `go list -m all` 的版本拷；靠第 7.3 节的生成代码零差异和测试基线兜底 |
| 已部署的库里残留 AI 等模块的表 | 不自动删表；在执行记录里列出需要手工 DROP 的表 |
| 回滚 | 每个阶段独立分支，每个模块独立提交，回滚粒度是单个模块的提交 |

## 10. 门禁（每个阶段结束都要跑）

在 `backend/` 下：

- `go build ./...`
- `go vet ./...`
- `go test ./...`，结果和第 0 阶段基线对比，**不能出现新的失败**
- 生成链：`make api && make openapi && make ts && make ent` 后，`git status` 里不应该有未提交的生成代码变化

在 `frontend/admin/react/` 下：

- `npm run typecheck`，0 错误
- `npm run build`
- `npm run lint`：先记录第 0 阶段的基线，之后不能比基线多

运行时冒烟（新建空库）：

- `libs_only` 起中间件，`app.mk` 的 `make run` 启动后端，`npm run dev` 启动前端
- `admin / Abcd@1234` 登录（图形验证码答案可以从 Redis 的 `gowind:captcha:<captchaId>` 读）
- 逐个打开保留的菜单，每个列表页能加载；用户、角色、菜单三个模块各做一次增删改查
- 第 1 阶段之后加做第 5.5 节的接口鉴权验证

## 11. 执行记录

> 每个阶段完成后由实施 agent 追加，格式：日期、阶段、分支、做了什么、门禁结果、遗留问题和需要用户决定的事项。

### 2026-10-26 · 第 1 阶段 · `chore/trim-phase-1`（HEAD `509d6e9`，本记录为后续提交）

阶段内提交从 `dd46de8` 到 `509d6e9`（GORM 平行后端到自助接口豁免）。本记录只做验收，没有再改业务代码。

**改动**

- casbin 的域用令牌里的租户 ID（`pkg/middleware/auth/utils.go` 的 `processAuthz`）；策略按角色编码追加，不再按编码覆盖（`AuthorizerProvider.ProvidePolicies`）。`authz.type` 不是 `casbin` 或配置缺失时，启动直接报错。
- 平台管理员（租户 0、角色编码为平台管理员）写入 `/*` + `ANY` 通配策略。种子里的 ApiId 仍按位置写死为 1..136，空库启动时实际同步出 156 个接口。
- `backend/Makefile` 里不再有 `@latest`。
- 删除监控模块（服务器监控、Redis 缓存监控、任务监控，以及监控告警）；模块枚举去掉 AI（`module.proto` 对 11 / `AI` 做了 `reserved`）；`4175a56` 清理已删模块的残留引用。
- 接口同步按 `(method, path)` 对齐，保留已有 ID 和权限绑定。
- 个人资料更新只允许改资料字段（`profileUpdatablePaths` 丢掉 `roleIds` 等）。OpenAPI：`PUT /admin/v1/me`，`body: "*"`，请求体是 `UpdateUserRequest`（`id`、`data`、`updateMask` 字符串）。冒烟用这个格式把 viewer 的昵称改成 `v2`，`roleIds` 仍是原角色。
- 自助类接口（导航、权限码、个人资料、自己的在线会话、退出登录）只要登录，不走 casbin。站内信收件箱不在豁免里。

**门禁（2026-10-26）**

- `go build ./...`、`go vet ./...`、`go test ./...`：通过，没有新增失败。和 `goadmin-trim-tools/baseline-main-gotest.txt`（main，全绿）比，已删模块的包（GORM、脚本、通知渠道等）不再出现，其余包为 ok。
- `make api && make openapi && make ts && make ent` 之后，git 状态里除了本来就有的本地配置（`data.yaml`、`oss.yaml`、`server.yaml`，含远程库凭据，不提交）没有别的改动。
- 前端 `npm run typecheck`、`npm run build` 通过。`npm run lint` 退出码 1：909 条（659 error，250 warning）。第 0 阶段没留下 lint 基线，这次补上，见 `docs/plans/react-lint-baseline.tsv`。截图基线按交接说明不做。
- 空库冒烟（远程 test 库，`smoke.sh HEAD`）：`RESULT: ALL PASS`。已核对的例外：`GET /admin/v1/file/download` 不带参数返回 500；`GET /admin/v1/initial-context` 返回 500。第一次冒烟里 `PUT /admin/v1/users/{id}` 只带昵称得到 400 `role_ids is required`，这是脚本漏了字段，不是本阶段回归：`UserService.Update` 从上游起就无条件要求 `roleIds`，和 `updateMask` 无关，管理端表单也会带上角色。已改冒烟脚本（仓库外）后重跑通过，并核对昵称写成了 `t2`。

**上游原有的问题（没修，只记录）**

- `GetInitialContext` 没有实现。`AdminPortalService` 嵌入的 HTTP 接口是 nil，调用会空指针 panic，表现为 500。`file/download` 没有 selector 时走 `ErrorDownloadFailed("unknown download selector")`，返回 500。
- 启动时 `PermissionService.init` 在权限表为空时调用 `SyncPermissions`。该函数要 `auth.FromContext`，启动期没有操作人，错误被 `_, _ =` 丢掉，权限点不会种上。管理员要在管理页手动点「权限同步」（冒烟里 `POST /admin/v1/permissions/sync:perms` 返回 200，`user:view` 能查到）。
- Kratos 访问日志记下登录请求参数。密码是 AES 密文，密钥是前端的 `VITE_AES_KEY`，会打进浏览器包，密文可以解回明文。另外 `useCreateUser` 创建用户时密码没有加密，同一条访问日志会记下明文密码。
- 收件箱在单租户下任何人都能读写别人的消息。`InternalMessageRecipientRepo.List` 只在「不是平台上下文、也不是系统上下文」时按当前用户收窄；`tid==0` 被当成平台特权放行，而单租户下所有用户都是 `tid==0`。自助接口豁免故意没把收件箱放进去。
- 租户管理员的种子 ApiId 是按位置写死的 1..128，和平台管理员的 1..136 是同一类问题。平台管理员已改走通配策略，租户管理员没有。

**已部署的老库要手动处理**（程序不会 DROP 表，也不会改旧菜单。清单对照了 `4175a56`、`e524a2d` 和已删的 ent schema）

- 删表：`sys_login_policies`、`sys_user_mfa_factors`、`sys_access_keys`、`sys_notification_channels`、`sys_notification_deliveries`、`notification_preferences`、`sys_notification_rules`、`notification_templates`、`sys_scripts`、`sys_script_logs`、`sys_api_audit_logs`、`sys_data_access_audit_logs`、`sys_permission_audit_logs`、`sys_policy_evaluation_logs`、`sys_ai_agent_apps`、`sys_ai_agent_runs`、`sys_ai_chunks`、`sys_ai_conversations`、`sys_ai_docs`、`sys_ai_knowledge_bases`、`sys_ai_messages`、`sys_ai_providers`、`sys_ai_usage_logs`、`monitor_alert_rules`。
- 程序内备份没有独立业务表（对象在 MinIO 桶 `backups`）。服务器监控、Redis 缓存监控、任务监控也没有表。
- `sys_apis.business_module`、`sys_menus.module`、`sys_plan_modules.module` 里值为 `AI` 的行（枚举值 11 已 reserved）。
- 旧菜单行。本阶段从种子里拿掉的 path：`/ai`、`/notification`、`access-keys`、`agents`、`api-audit-logs`、`channels`、`chat`、`data-access-audit-logs`、`deliveries`、`knowledge`、`login-policies`、`monitor-alerts`、`permission-audit-logs`、`policy-evaluation-logs`、`providers`、`query`、`redis-cache-monitor`、`rules`、`scripts`、`server-monitor`、`templates`、`usage`。MFA、SSO、找回密码、任务监控、备份在 phase 0 的 `default_data.go` 里没有对应菜单行；老库如果更早种过，也要删。菜单只在表为空时播种，老库不会自己消失。
- 已删类型的 `sys_tasks`：`ai_audit_digest`、`ai_doc_reindex`、`ai_query_run`、`backup`、`monitor_alert_scan`、`notification_delivery_sweep`、`notification_dispatch`、`script_task`。

**故意没动的**

- `role.proto` 里的 `force_mfa`、`ip_allow_list`。
- `user_credential.proto` 里 OTP / SSO 相关的凭据枚举。
- `mailtext.PasswordResetCode`（找回密码页面和接口已删，文案函数还在）。

**需要用户决定的**

- 收件箱越权（单租户 `tid==0` 放行）要不要修。本阶段没改。
- 访问日志里的密码（登录密文可解、创建用户明文）要不要收。本阶段没改。

---

## 附录 A：作者依赖实测（2026-10-25，裁剪前）

实际参与编译的 tx7do 包（`go list -deps`）：

| 来源仓库 | 包数 | 行数（非测试） |
|---|---|---|
| go-crud | 29 | 23,042 |
| kratos-bootstrap | 13 | 18,949 |
| go-utils | 70 | 18,604 |
| go-scripts | 8 | 6,447 |
| kratos-transport | 6 | 5,605 |
| go-wind-toolkit | 1 | 1,973 |
| kratos-authz | 6 | 1,832 |
| kratos-authn | 2 | 781 |
| kratos-swagger-ui | 2 | 439 |
| go-wind-plugins | 3 | 225 |
| go-wind | 1 | 219 |
| **合计** | **141** | **78,116** |

主要仓库根目录的许可证都是 MIT（版权人 Bobo）：go-crud、go-utils、kratos-bootstrap、kratos-transport、kratos-authn、kratos-authz、go-scripts。**其余 4 个（go-wind-toolkit、go-wind-plugins、go-wind、kratos-swagger-ui）吸收前要确认许可证**；不是 MIT 或许可证不明的，停下来汇报。

## 附录 B：复测命令

```bash
# 参与编译的 tx7do 包及行数
cd backend
go list -deps -f '{{if not .Standard}}{{.ImportPath}}{{end}}' ./... | grep '^github.com/tx7do/'

# 业务代码对各仓库的引用文件数
for r in go-crud go-utils kratos-bootstrap kratos-transport kratos-authn kratos-authz go-scripts go-wind-plugins go-wind kratos-swagger-ui go-wind-toolkit; do
  echo "$r $(grep -rlF "github.com/tx7do/$r" app pkg | grep '\.go$' | grep -v '/ent/' | grep -v '/gen/' | wc -l)"
done

# 生成代码里的作者依赖
grep -rhoE '"github.com/tx7do/[^"]+"' app/admin/service/internal/data/ent api/gen | sort | uniq -c

# 带租户字段的 schema
grep -lE 'Tenant' app/admin/service/internal/data/ent/schema/*.go | wc -l
```
