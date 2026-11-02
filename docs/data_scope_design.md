# 数据权限范围（角色级 Data Scope）设计文档

> V1 已落地并完成端到端验证（2026-09-11）；2026-11 起覆盖用户、组织单元、登录/操作日志。
> 本文档是数据权限的唯一权威说明：五档语义、多角色聚合、执行层接入、运维注意与边界。

## 1. 概述

角色级数据范围（若依/JeecgBoot 式）回答"**用户能看到哪些业务行**"：
管理员给角色配置一个范围档位，登录时把用户所有角色的范围**聚合成并集**写进令牌，
查询时由数据层按令牌注入行级过滤谓词。

与既有两套机制的关系：

| 机制 | 管什么 | 层 |
| --- | --- | --- |
| authz 引擎（casbin/opa） | 用户能否调用某个**接口/操作** | HTTP 中间件 + 策略引擎 |
| **数据范围（本文）** | 能看到哪些**业务行** | 仓储层谓词（用户/组织/日志）+ ent 查询隐私规则（业务表逐表 opt-in） |

两者正交叠加：先过 authz（接口级），再过数据范围（行级组织/个人）。`tenant_id` 列是遗留设施，值固定为 0，见 [tenant_isolation.md](./tenant_isolation.md)。

## 2. 五档语义

`identity.service.v1.DataScope`（与若依一一对应）：

| 档位 | proto 值 | 语义 | 行级谓词 |
| --- | --- | --- | --- |
| 全部 | `ALL` | 全部 | 无（放行） |
| 本部门 | `UNIT_ONLY` | 用户所属组织单元 | `org_unit_id IN (own units)` |
| 本部门及以下 | `UNIT_AND_CHILD` | 所属单元 + 全部后代 | `org_unit_id IN (own ∪ descendants)` |
| 自定义 | `SELECTED_UNITS` | 角色配置的单元集（`sys_role_org_units`） | `org_unit_id IN (configured)` |
| 仅本人 | `SELF` | 自己创建的行 | `created_by = uid` |
| 未指定 | `DATA_SCOPE_UNSPECIFIED` | 防御值，**不对外配置** | 聚合后剔除，空集 → fail-closed 拒绝 |

存储：

- `sys_roles.data_scope` 枚举列，默认 `ALL`（存量行迁移时回填 ALL，上线零行为变化）；
- `sys_role_org_units`（role_id + org_unit_id，租户内唯一）承载 SELECTED_UNITS 的自定义单元集，
  由 `RoleOrgUnitRepo` 维护（写入时校验单元与本角色同租户，跨租户注入 400 拒绝）。

## 3. 登录聚合与令牌承载

登录/刷新令牌时（`AuthenticationService.aggregateDataScopes`）把用户全部角色的档位聚合：

1. **平台管理员**：令牌 `is_platform_admin` 为真时整体 `[ALL]`。该标志由 `fillAdminFlags` 在聚合之前按角色码 `platform:admin` 置位。`tenant_id == 0` 不再短路。单租户下普通用户的租户也是 0，按自己角色的 `data_scope` 聚合。
2. 任一角色 `ALL` → 整体 `[ALL]`（主导）。
3. 其余取**活动类型并集**：`SELF` 与 `UNIT_*` 可共存；`UNIT_ONLY`/`UNIT_AND_CHILD`/`SELECTED_UNITS`
   的单元目标集为各角色贡献的**并集**（本部门取用户所属单元；及以下再做 path 前缀展开；
   自定义取各角色配置集，全部按令牌租户过滤——登录上下文绕过租户隐私层，显式租户谓词是唯一防线）。
4. **退化**（fail-closed，宁可拒绝不可放行）：
   - 单元目标集超过 256（`maxDataScopeUnitIds`，防令牌膨胀）→ 整体 `[UNSPECIFIED]` + error 日志；
   - 无任何有效活动类型（含 UNIT 类目标集为空）→ `[UNSPECIFIED]` + error 日志；
   - `[UNSPECIFIED]` 在 viewer 构建侧剔除为空集，库规则按 "no data scope defined" 拒绝。

令牌承载（`UserTokenPayload` / `OperatorMetadata` 同构）：

| claim | 字段 | 说明 |
| --- | --- | --- |
| `ds` | `data_scope` | 旧单值（过渡保留）。聚合结果恰为 [ALL]/[SELF] 时镜像写入 |
| `dss` | `data_scopes` | 聚合活动类型名数组 |
| `dsu` | `data_scope_unit_ids` | UNIT 类目标集并集（逗号连接十进制串） |

解析端兼容旧令牌：只有 `ds` 时按单元素回退；`dss` 中的 UNSPECIFIED 剔除。
**改角色的数据范围、选定组织或字段权限后，持有该角色的人会被立刻吊销令牌**（含操作者自己），
下次登录按新定义重算。只改角色名称、描述、排序不吊销，旧令牌里的范围继续用到过期。
管理端保存会连同未改动的权限、数据范围、字段权限一起提交；这些值和库里一样时也不吊销。
给某个用户换角色集合同样立刻吊销该用户。详见第 7 节和 `docs/authentication.md` 第 8 节。

## 4. 执行层

### 4.1 覆盖范围与语义

ALL 与平台管理员（令牌恒为 `[ALL]`）不受限制。多档并集取 OR。下表的"单元集"指令牌里的
UNIT 类目标集（本部门 / 及以下 / 自定义，及以下在登录时已展开为后代）。

| 表 | 接口 | UNIT 类 | SELF | 实现 |
| --- | --- | --- | --- | --- |
| 用户 `sys_users` | 列表、计数、详情、修改、删除、重置密码 | 至少属于单元集中一个单元的用户（按 `sys_user_org_units`） | 只有本人 | 仓储层 |
| 组织单元 `sys_org_units` | 列表（树）、计数、详情、修改、删除 | 单元集内的单元 | 本人所属单元 | 仓储层 |
| 登录日志、操作日志 | 列表、详情 | 操作人 `user_id` 属于上面的"可见用户" | 只有本人的日志 | 仓储层 |
| 岗位 `sys_positions` | 全部查询 | `org_unit_id` 在单元集内 | `created_by` = 本人 | ent 查询策略 |

细节：

- **本人总是可见**。用户表和日志表的谓词恒带 `id/user_id = 本人`，所以"自定义"的单元集
  不含自己的单元时，操作人仍能读到自己（用户修改/删除接口要先读操作人本身）。
- **没有组织单元的用户只有 ALL 看得到**（他自己除外）。
- **越界按 id 访问回 404**，不区分"不存在"和"看不见"。修改、删除、重置密码在写之前
  先用带范围的 ctx 读一次目标，越界即 404，什么都不写。
- 组织树：可见单元的父节点不可见时，该单元作为根返回（`parent_id` 原值保留），
  不会被当成孤儿丢掉。
- 缺 viewer、无档位（含 `[UNSPECIFIED]` 剔空）、显式 NONE：403 拒绝（fail-closed）。
- 日志导出只对平台管理员开放，平台管理员恒为 ALL，所以导出不需要另做过滤。

### 4.2 用户、组织单元、日志：仓储层谓词

这几张表**不挂** ent 隐私策略：登录、刷新令牌、站内信广播（遍历全部用户）、组织子树
路径维护和级联删除、关联名称回填都要读全量，挂在 schema 上会把这些内部流程一起收窄。

做法（`internal/data/data_scope_filter.go`）：

- service 在对外接口入口用 `data.WithDataScope(ctx)` 包一层 ctx，只包传给仓储读方法的那次调用；
- 被标记的 `List / Count / Get` 注入子查询谓词：
  - 用户：`id = uid OR id IN (SELECT user_id FROM sys_user_org_units WHERE org_unit_id IN 单元集)`；
  - 组织单元：`id IN 单元集 OR id IN (SELECT org_unit_id FROM sys_user_org_units WHERE user_id = uid)`（后者仅 SELF）；
  - 日志：`user_id = uid OR user_id IN (SELECT user_id FROM sys_user_org_units WHERE org_unit_id IN 单元集)`；
- 没标记的 ctx、系统上下文、ALL 都不加谓词。

子查询走既有索引 `idx_uou_org_unit_id` / `idx_uou_user_id`，日志表 `user_id` 也有索引，没有改表结构。

测试：`data/data_scope_filter_sqlite_test.go`（单元 × 用户 × 创建人矩阵，三张表），
`service/data_scope_byid_sqlite_test.go`（用户、组织单元按 id 越界 404，日志接口过滤）。

### 4.3 业务表：ent 查询策略（逐表 opt-in）

- 核心规则：go-crud `rule.PermissionRule` —— 系统上下文放行；缺 Viewer 拒绝；
  无 scope / 显式 None 拒绝；ALL 放行；SELF 注入 `created_by = uid`；UNIT 注入
  `org_unit_id IN (targets)`；多 scope 以 OR 并集。`tenant_id == 0` 不再跳过这条规则。
- 挂载方式：`schema/data_scope_guard.go` 提供 `DataScopeQueryPolicy`（仅查询侧）
  与组合策略 `TenantAndDataScopePolicy`（租户变更防护 + 数据范围查询过滤）。
- 现有接入：`sys_positions`（岗位），守卫矩阵单测见 `ent/data_scope_guard_test.go`。
- 和 4.2 不同，策略对**该表的所有查询**生效，不需要 ctx 标记。

新表接入步骤：

1. 实体同时具备 `created_by`（`mixin.OperatorID{}`）与 `org_unit_id` 两列，`org_unit_id` 加索引——
   缺列会在谓词注入时报运行时错误，`make ent` 不会提示；
2. 实体 `Policy()` 返回 `&TenantAndDataScopePolicy{}`（参照 `schema/position.go`）；
3. `make ent` 重新生成，补守卫单测（参照 `data_scope_guard_test.go` 的 (单元×创建人) 矩阵）；
4. 策略只过滤查询。按 id 修改/删除要在 service 里先 `Get` 一次（`Get` 受策略过滤，越界 404）。

## 5. 组织单元物化路径（path 列）约定

`UNIT_AND_CHILD` 的后代展开依赖 `sys_org_units.path` 前缀匹配，约定：

- **根节点**：`/ID/`（各根前缀互不相同）；
- **子孙**：`/父路径/ID/`，如 `/26/27/`。

注意：go-crud 的 `ComputeTreePath` 对空父路径返回 `"/"`，会让所有根节点共享同一前缀、
使前缀展开误匹配全表——`OrgUnitRepo.setTreePath` 已改用自带约定（`computeUnitTreePath`），
且 `Update` 在 `parent_id` 变更后按 parent 链 BFS 重算子树路径（`relocateSubtree`，
顺带自愈历史脏数据，含移入自身后代成环检测）。

**权限组（`sys_permission_groups`）同款维护**：proto 注释的格式
`/1/10/101/（包含自身且首尾带/）` 与上述约定一致，repo 四处已接线——
`Create.setTreePath` 根节点 `/ID/`、`Update` 改父后 `relocateSubtree`、
同步流 `BatchCreate` 后从批内根补算 + `UpdateParentIDs` 重算受影响子树
（事务内走 `tx.Client()` 保证读到未提交的新父关系）。
该 path 当前无运行时读方（组树按 parent_id 构建），属防御性修复。

**两棵树的删除语义**（与 path 维护直接相关）：

- 组织单元：应用层级联——递归收集整棵子树一并删除，无幸存节点故无需重建路径。
  删除前守卫：子树内仍有岗位（`org_unit_id NOT NULL`，无 DB 外键兜底）时 400 拒绝；
  用户↔单元绑定行（`sys_user_org_units`）随子树清理（纯绑定，用户不受影响）。
- 权限组：repo 只删本行，直接子级由 DB 外键 `OnDelete SetNull` 提升为根——
  幸存子树的 path 前缀仍然自洽（无需重建），但违反根节点 `/ID/` 约定，
  后续改父时 `relocateSubtree` 会自动重算自愈。删除前守卫：存在子分组或
  分组下仍有权限点时 400 拒绝（权限点 `group_id` 可空且无 DB 外键）。

**存量数据修复**（历史行 path 均为 "/"，已部署实例执行一次；两表同构）：

```sql
-- 按层级自根向叶执行；根节点：
update sys_org_units set path = '/' || id || '/' where parent_id is null and path <> '/' || id || '/';
-- 每个子节点（父路径已知后）：
update sys_org_units c set path = p.path || c.id || '/'
from sys_org_units p where c.parent_id = p.id and c.path <> p.path || c.id || '/';
-- 反复执行直至影响 0 行（自根向叶逐层收敛）
```

## 6. 三端管理界面

- 角色列表新增「数据权限范围」列（Tag 颜色语义三端一致：ALL=red、UNIT_AND_CHILD=blue、
  UNIT_ONLY=orange、SELECTED_UNITS=purple、SELF=default/灰）。
- 角色（新建/编辑）抽屉新增「数据权限范围」下拉（五档，不列 UNSPECIFIED、必填不可清空）；
  选 `SELECTED_UNITS` 时渲染「授权组织单元」勾选树（react=antd Tree、vue-element=ElTree、
  vben=ApiTree），提交走 Role 消息的 `org_units` 关联字段（与 permissions 关联同形态：
  `update_mask` 带 `orgUnits` 即整体替换，含清空场景；不带则维持既有集）。
- 「数据权限范围」下拉下方有一行说明（`dataScopeHint`），写明覆盖的表和判定依据，
  与 4.1 保持一致。
- i18n：react `_modules/role.json`；vue-element `pages/role.json` + `enum.json` 的
  `role.dataScope`；vben `page.json` 的 `role.*` + 既有 `enum.json role.dataScope`。

## 7. 运维注意

1. **新端点与 Api 表**：本特性未新增管理端点（org_units 走 Role 消息内联关联），
   已部署实例**无需**接口同步；R2 新接入表如有新端点，`make openapi` 后重启即自动同步，
   漏了 `make openapi` 则新端点不在 Api 表里，非平台管理员角色拿不到授权（403）。
2. **生效时延**：给某个用户换角色集合时，`UserService.Update` 会吊销该用户全部令牌，
   下次登录按新角色重算数据范围。改角色定义本身（权限、数据范围档位、选定组织、
   字段权限、启用/停用）或删除角色时，`RoleService` 会吊销所有持有人（含操作者自己），
   旧 access token 立即不能再认证。只改名称、描述、排序不吊销；授权字段的值没变也不吊销。
   见 `docs/authentication.md` 第 8 节。
3. **单元集上限 256**：超过即整体退化拒绝并记 error 日志，调整大组织配置时留意。
4. **存量兼容**：迁移把全部存量角色回填 ALL，升级后行为零变化；旧令牌（仅 ds 声明）
   解析按单元素回退，过渡期后可下线 ds 单值轨道。

## 8. 边界（不做）

- assignment 级覆盖语义（`user_role.proto` / `membership_role.proto` 的 `data_scope = 80`
  为遗留字段，无存储无链路，已注释标记，接入前需完整设计）；
- 字段级权限与数据范围联动；authz 引擎联动；令牌内容对前端透出；
- 新建时的范围校验：新建用户/组织单元时不检查所选单元或父单元是否在范围内；
- 业务表 opt-in 策略的变更侧过滤（见 4.3 第 4 步，靠 service 先 Get）；
- 未接入的其余表（角色、菜单、字典等）不受数据范围限制。
