# 07 · 审计与等保能力的使用面

> 前置：[01 章](./01-architecture-overview.md)。读完你会知道：平台内置了哪些审计/身份鉴别能力、它们各自记录什么、留存与归档怎么运转、以及管理页在哪配置——做到"会用、会查、知道边界"。

## 1. 登录日志和操作日志

| 日志 | 记什么 | 采集层 |
|---|---|---|
| 登录审计 | 登录尝试：成败、IP、归属地、失败原因 | transport 中间件 |
| 操作审计 | 谁对哪个资源做了什么写操作（resource_type / action） | transport 中间件（从 kratos operation 名静态解析） |

API 审计、数据访问审计、权限变更审计和策略评估日志已删除。ent 直接使用原始 driver，鉴权直接走引擎，不再包一层评估日志。字段与接线见 [audit-log-producer-design.md](../audit-log-producer-design.md)。

## 2. 留存与归档（等保 180 天要求）

- 库内留存默认 **180 天**（`AUDIT_RETENTION_DAYS` 可调）；
- asynq 每日定时归档：超期数据导出 **JSONL 归档文件**留痕，库内清理——留存与库瘦身两不误；
- 归档任务只处理登录日志和操作日志。数据库全量备份不在程序内，用数据库自带工具或运维平台。

## 3. 身份鉴别三件套（管理页可调）

| 能力 | 说明 | 配置位置 |
|---|---|---|
| 口令复杂度 | ≥8 位、小写/大写/数字/符号四类取三 | 参数管理（平台参数，启动播种） |
| 历史口令复用 | 默认禁止复用近 3 条历史口令 | 参数管理 |
| 口令有效期 | 默认 90 天到期强制换新 | 参数管理 |
| 登录限流 | IP + 用户名双维度 Redis 限流 | 代码内固定阈值，见 authentication.md §7.2 |
| 图形验证码 | 登录必填，答案在 Redis，单次有效 | 登录页 |

三件套阈值经「参数管理」调整——**内置参数启动时播种**（空表守卫），已部署实例改参数立即生效，
不需要改代码或环境变量（这三项早先走环境变量，该路径已废弃：生产代码里 `os.Getenv` 只剩
`AUDIT_RETENTION_DAYS` / `AUDIT_ARCHIVE_DIR`（§2 的归档）与 JWT / 加密密钥几处，口令策略已不从环境读）。

## 4. 管理页在哪

登录日志和操作日志各有查询页（列表 + 筛选 + 导出）。

**把一次操作的日志串起来，靠的是 `request_id`。** 前端请求客户端给每个请求注入
`X-Request-ID`（`frontend/admin/react/src/core/transport/rest/request-client.ts`），登录日志和操作日志把它原样落库。

> 下面是 2026-09-25 的历史测量，当时还保留着已删除的审计表，只说明那时没有链路追踪。
> 本机库实测（4.2 万行审计数据）：`sys_api_audit_logs` / `sys_login_audit_logs` / `sys_operation_audit_logs` /
> `sys_data_access_audit_logs` 四类的 `trace_id` **全为 NULL**（0 / 38,655 行）。
> 唯一有值的是当时的 `sys_policy_evaluation_logs.trace_id`（2,887 / 3,193 行），它存的是请求号而不是链路 ID：
> 这 2,887 个值里有 2,882 个能直接 join 上当时的 `sys_api_audit_logs.request_id`。
> 这四张表已经删除，不要再按它们排障。登录日志和操作日志仍然用 `request_id`。

参数管理 / 在线用户（强制下线）在系统管理对应的管理页。

## 5. 边界与诚实声明

- 等保 2.0 除技术要求外还有管理制度、物理环境、人员组织等**非软件范畴**的内容——本平台
  覆盖的是技术措施部分，不能替代完整测评流程；
- 操作审计的 `before_data` / `after_data` 仍留空。2026-09-25 本机库实测
  `sys_operation_audit_logs` 209 行的 before/after 全空，查不到不是配置问题；
- 数据访问审计和策略评估日志已删除。2026-09-25 对 `sys_data_access_audit_logs` 的 `db_user` /
  `table_name` 测量是删除前的记录。

## 深读

- [audit-log-producer-design.md](../audit-log-producer-design.md) —— 登录日志和操作日志的采集层；文内关于已删除四类日志的段落是历史设计
- [sys_config.md](../sys_config.md) —— 平台参数（口令策略三件套阈值的存放处）：缓存读取器、多实例失效广播、内置参数补种
- 根 [README.md](../../README.md)「安全与等保合规」—— 能力矩阵总表（对外口径）
- [backend_deploy.md](../backend_deploy.md) —— 生产部署时与审计相关的配置（留存天数、备份）

下一步：[09 · 部署上线](./09-deployment.md)。
