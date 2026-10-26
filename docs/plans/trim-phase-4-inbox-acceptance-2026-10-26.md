# Phase 4 收件箱归属与 Apache-2.0 验收记录

- 验收日期：2026-10-26
- 分支：`chore/trim-phase-4`
- HEAD：`6fcccb8`，审查包含当时工作区的未提交改动。
- 范围：收件箱归属修复、相关测试、React 调用、文档及许可证记录；访问日志密码问题不在本轮范围。
- 执行方式：只读审查并跑门禁，没有修改业务代码、暂存或提交文件。本文件为验收后应用户要求新增的报告。
- 证据边界：HTTP 链路及撤权风险来自代码追踪；实际执行了门禁和 SQLite 测试，没有向运行中的业务库发送越权写请求。下文复现步骤不表示已经对业务实例执行。
- 路径均相对仓库根目录，行号对应验收时工作区。

## 0. 复查结果（2026-10-26，后续工作区改动）

**四项都已修。** 复查当时旧令牌撤权仍在；随后 `UserService.Update` 在用户角色集合变化时吊销该用户全部令牌。以下第 1—5 节保留首次验收的历史记录，当前状态以本节为准。复查仍在 `chore/trim-phase-4`，HEAD 为 `6fcccb8`，修复位于未提交工作区中。

| 原问题 | 当前状态 | 当前证据 |
|---|---|---|
| 应修 1：撤销管理员角色后旧令牌仍可代客操作 | **已修（成员变更）** | `user_service.go` 的 `Update` 在写入前比较角色集合，变化后调用 `RevokeUserTokenAllClientTypes`；集合相同或 mask 不含 `role_ids` / `role_id` 不吊销。`TestUserServiceSqlite_UpdateRoleChangeRevokesTokens` 覆盖旧 AT 失效。改角色定义、刷新留旧 AT 不在本次范围 |
| 应修 2：岗位数据范围对 tid=0 普通用户放行 | **代码已修** | `backend/third_party/go-crud/entgo/rule/system.go:51` 只豁免系统上下文；`authentication_data_scope.go:43` 只为平台管理员短路 ALL。新增岗位 SQLite SELF 隔离测试和租户 0 角色聚合测试均通过 |
| 应修 3：租户 0 收件箱测试覆盖不足 | **原报告所列主要缺口已补齐** | `internal_message_recipient_repo_sqlite_test.go:447` 新增租户 0 双用户夹具及三个写口、空 ids、管理员和系统上下文矩阵；第 65 行起补指定他人 query 和 total 断言；`internal_message_service_sqlite_test.go:396` 补结果长度及逐行归属断言 |
| 建议 1：非管理员看到跨用户收件箱 Tab 空列表 | **已修** | `frontend/admin/react/src/pages/app/opm/user/detail/index.tsx:36` 仅平台管理员或查看本人时显示 Tab；已选中但失去可见性时回退基本信息 |

岗位修复需要结合令牌状态判断：修复前为 tid=0 普通用户签发的 ALL 数据范围仍在旧 JWT 中，不能靠更新代码立即收回。重新登录或刷新会重算新令牌的数据范围；旧 AT 在过期或主动吊销前仍可使用。当前配置有效期为 5400 秒，旧令牌问题窗口最长约 90 分钟。

复查重新执行了全部原定门禁：后端 build、vet、全量 test，完整 `make api && make openapi && make ts && make ent`，前端 typecheck、build、lint，以及防上游依赖回流脚本。均满足门禁；lint 仍为 909 条（659 error、250 warning），与基线逐项比对新增 0、消失 0。生成前后已跟踪及非忽略未跟踪文件的内容哈希一致，没有新增生成差异。

另以 `-count=1 -v` 执行新增收件箱列表、租户 0 写入矩阵、岗位数据范围，以及租户 0 登录数据范围聚合和广播可读性用例，最终全部通过、未跳过。一次 service 定向测试与 Ent 生成重叠，读取中间产物导致编译错误；生成结束后已串行重跑 build、vet、全量 test 及 service 定向用例并全部通过，没有将该瞬时错误认定为当前源码缺陷。

历史计划段落已补充指向后续修复结论的说明，`tenant_isolation.md` 收件箱段落中的“三端”措辞也已改为 React。三份本地环境配置仍为已跟踪、未暂存改动，暂存区为空；本次未改业务代码、未暂存或提交文件，仅补充本节复查记录。

## 1. 结论：有条件通过

本轮收件箱四个 HTTP 入口的归属修复成立，默认 admin 的代客查看功能保留，全部指定门禁满足要求。仍有两项存量权限风险及测试覆盖缺口，不能据此宣称所有按人隔离及撤权即时生效已经解决。

生成链没有留下新增差异；验收结束时原有 12 个修改文件的状态与验收前一致。

## 2. 门禁结果

| 工作目录 | 命令 | 结果 |
|---|---|---|
| `backend/` | `go build ./...` | 通过，exit 0 |
| `backend/` | `go vet ./...` | 通过，exit 0 |
| `backend/` | `go test ./...` | 通过，exit 0 |
| `backend/` | `make api` | 通过 |
| `backend/` | `make openapi` | 通过 |
| `backend/` | `make ts` | 通过 |
| `backend/` | `make ent` | 通过；生成后 git status、diff 与验收前一致 |
| `frontend/admin/react/` | `npm run typecheck` | 通过 |
| `frontend/admin/react/` | `npm run build` | 通过；有重复导入等构建警告 |
| `frontend/admin/react/` | `npm run lint` | exit 1；909 条：659 error、250 warning，符合现有基线 |
| 仓库根 | `bash scripts/check-no-upstream-deps.sh` | 通过 |
| 仓库根 | `git diff --check` | 通过 |

lint 另用 `npx eslint . -f json` 与 `docs/plans/react-lint-baseline.tsv` 逐项比较，按 severity、path、line、column、rule 计数：基线 909，当前 909，新增 0，消失 0。

为核实本轮测试实际执行，以 `-count=1 -v` 单独运行以下用例，全部通过、未跳过：

```bash
cd backend
go test -count=1 -v ./app/admin/service/internal/data \
  -run '^(TestInternalMessageRecipientInboxListSqlite_TenantZeroStaysOwn|TestInternalMessageRecipientInboxWritesSqlite)$'
go test -count=1 -v ./app/admin/service/internal/service \
  -run '^TestInternalMessageServiceSqlite_PlatformBroadcastIsReadableByTenantUser$'
```

完整 `go test ./...` 成功不代表所有外部数据库集成场景均已实测；部分 PostgreSQL 集成测试按测试代码在数据库不可达时跳过。

## 3. 问题列表

未发现本轮改动引入的阻断问题。以下按严重程度排列；存量问题不归因于本轮新增代码。

### 应修 1：撤销管理员角色后，旧令牌仍能代客读写

位置：

- `backend/app/admin/service/internal/service/user_service.go:553`
- `backend/app/admin/service/internal/data/authenticator.go:282`
- `backend/app/admin/service/internal/data/internal_message_recipient_repo.go:344`
- `backend/app/admin/service/internal/data/user_token_cache.go:324`

依据：角色在登录、刷新时从数据库取得并写入 JWT。普通请求解析 JWT 后检查签名、有效期及 Redis 令牌状态，不重新读取用户角色。修改用户角色不会自动吊销令牌，`UserService.Update` 中的吊销在改密分支才执行。刷新轮换也不会立即删除旧 AT。

复现步骤：

1. 用户 A 带 `platform:admin` 登录，保存其 access token。
2. 另一管理员移除 A 的管理员角色，保留平台管理员角色本身及其策略。
3. 继续使用原 token，查询 B 的收件箱，或向 `/admin/v1/internal-message/inbox/delete` 提交 B 的 `userId`，并令 `recipientIds` 为空。
4. 只要原 token 未过期、未被吊销，认证和 repo 仍按管理员身份处理，B 的收件箱仍可被读写。

影响：当前 `backend/app/admin/service/configs/auth.yaml:12` 配置 AT 有效期为 5400 秒，撤权窗口最长约 90 分钟；无配置时默认有效期为 15 分钟。取得不含管理员角色的新 token，也不使旧 AT 立即失效。

建议修法：角色成员关系变更时调用 `RevokeUserTokenAllClientTypes`；角色权限变更时处理受影响用户，或引入请求期权限版本校验。属于存量认证问题，本轮角色豁免判断仍继承此风险。

### 应修 2：岗位数据范围仍把全部 tid=0 用户当成平台特权

位置：

- `backend/third_party/go-crud/entgo/rule/system.go:50`
- `backend/app/admin/service/internal/data/ent/schema/data_scope_guard.go:21`
- `backend/app/admin/service/internal/data/ent/schema/position.go:187`
- `backend/third_party/go-crud/viewer/user_context.go:69`

依据：岗位表实际接入 `PermissionRule`。该规则在 `IsPlatformContext()` 为真时直接返回，而普通 `UserContext` 的判断只是 `tid == 0`，因此单租户普通用户的 SELF、UNIT、NONE 都不会执行。

复现步骤：

1. 给租户 0 普通用户 A 配置 SELF 数据范围和岗位查询接口权限。
2. 准备 A、B 分别创建的岗位。
3. A 登录后请求 `GET /admin/v1/positions`。
4. 按当前规则会返回 B 创建的岗位，SELF 归属过滤未生效。

建议修法：数据范围豁免改为明确的管理员身份或系统身份，并增加租户 0 普通用户的 SELF、UNIT、NONE 用例。修改 `third_party` 时同步记录对应 `VERSION.md`。属于本轮 B 项排查发现的存量同类问题。

### 应修 3：测试未完整覆盖租户 0 的写操作矩阵

位置：

- `backend/app/admin/service/internal/data/internal_message_recipient_repo_sqlite_test.go:282`
- `backend/app/admin/service/internal/data/internal_message_recipient_repo_sqlite_test.go:393`
- `backend/app/admin/service/internal/service/internal_message_service_sqlite_test.go:396`

依据：

- 写测试的公共夹具仍将用户 201/202 的收件行写入租户 5；原有改状态、单条删除、整箱删除用例的调用者也属于租户 5。
- 新增租户 0 子测试仅覆盖 `MarkNotificationAsRead`，调用者为用户 1，夹具中没有其自己的收件行，且目标行仍属于租户 5。
- 管理员和系统上下文代客写入仅单独测试了标已读。
- 新增 List 测试没有显式指定他人 query，也没有检查 `total`。
- service 测试仍只寻找自己的行，没有断言结果长度或排除其他用户行；旧的宽松断言与新语义不冲突，但不足以证明新语义。

复现方式：执行第 2 节的定向测试，核对上述夹具、调用者及断言。当前测试通过不能证明“租户 0 双用户、空 ids 清箱”的完整行为。

建议修法：用租户 0 的 A/B 收件行覆盖三个写口及空 ids 操作；管理员和系统上下文补对应放行断言；List 增加指定 B 的 query、`total` 断言；service 测试增加长度及全部归属断言。

### 建议 1：非平台管理员仍可看到跨用户收件箱 Tab，但结果为空

位置：

- `frontend/admin/react/src/pages/app/opm/user/detail/index.tsx:109`
- `frontend/admin/react/src/pages/app/opm/user/detail/InternalMessagePage.tsx:142`

依据：用户详情页没有按平台管理员角色隐藏收件箱 Tab。前端按目标用户过滤，后端同时钉住当前用户，查看他人时两个条件相交为空。

复现步骤：使用拥有用户详情、收件箱查询权限，但没有 `platform:admin` 的账号，进入其他用户详情并打开站内信 Tab。请求成功返回空集，页面容易被理解为“对方没有消息”。

建议修法：对跨用户收件箱 Tab 增加平台管理员角色判断，或展示明确的权限说明。默认 admin 的查看功能不受影响。

## 4. A—E 回答

### A. 修复在真实请求链路上是否成立

**A1：claims 能拿到，调用 repo 前没有丢失 ctx。**

`backend/pkg/middleware/auth/auth.go:64` 注入 claims，第 97 行在同一父 context 上派生 viewer；authz 也通过派生 context 增加值。HTTP 生成 handler 和 `internal_message_recipient_service.go` 继续直接传递该 context。四个收件箱 HTTP 接口不在认证白名单或自助接口鉴权豁免列表内。

List 的 `viewer.WithSystemContext(ctx)` 位于 `backend/app/admin/service/internal/service/internal_message_recipient_service.go:62`，只在收件记录过滤完成之后回填父消息正文；没有把收件记录查询或三个写口改成系统上下文。

**A2：默认 admin 的角色匹配。**

`backend/pkg/constants/role.go:15` 的 `PlatformAdminRoleCode` 值为 `platform:admin`。`backend/pkg/constants/default_data.go:182` 播种该角色，第 256 行将默认 admin 绑定角色 1。登录授权在 `backend/app/admin/service/internal/service/authentication_service.go:306` 将角色码写入 `Roles`，默认 admin 可以继续查看指定用户收件箱。

**A3：角色是令牌快照，不是每次请求重新查库。**

登录、刷新时重算角色；普通请求解析旧 JWT 并校验缓存。移除管理员角色后，旧 token 可继续代客操作，见“应修 1”。

**A4：系统广播和异步投递保持可用。**

`backend/app/admin/service/internal/service/internal_message_service.go:590` 在 asynq 租户 0 广播时建立 SystemContext，其他租户建立对应 viewer。广播走 `CreateBulk`、定向发送走 `Create`，未受本轮收件箱读写收窄影响。goroutine 回退路径在第 565 行起复制 viewer 到后台 context，也能继续写指定收件人。

### B. 是否存在漏网路径

**B1：当前四个对外收件箱接口已覆盖，但 repo 不能视作所有方法均有归属守卫。**

- List 和三个写口均使用同一个身份判断。
- 头部未读数使用 List 返回的 `total`；count builder 在加入归属谓词后克隆，同样受限。
- SSE 在 `backend/app/admin/service/internal/service/internal_message_service.go:147` 起校验 `stream == token.userId`，不能订阅他人的流。
- repo 的 `Count:78`、`IsExist:93`、`Get:131`、通用 `Update:267`、`Delete:308` 没有按用户归属收窄；当前没有由对外收件箱入口调用它们的路径，不能直接认定为已暴露 HTTP 漏洞。以后暴露或复用时需要明确授权语义。
- 消息管理撤销、级联删除仍按请求指定用户或消息处理，属于独立管理接口，受对应接口权限控制。它们不等同于本次自用收件箱写口。

**B2：发现岗位数据范围这一项实际接入的同类问题。**

全仓追踪 `IsPlatformContext()` 后，`PermissionRule` 在岗位表上的 SELF 等范围豁免有实际影响，见“应修 2”。`OwnerOnlyRule`、字段限制等其他辅助规则当前没有业务调用点。租户规则中的 tid=0 放行仍存在，但不能把所有管理资源直接认定为个人资源越权。

### C. React 调用是否兼容

普通收件箱页面、头部未读数、最近消息、全部已读均传当前用户 ID，兼容新语义。自定义 RPC 的请求体保持扁平结构。默认 admin 在用户详情传目标用户过滤条件，仍能正常代客查看。

非平台管理员的用户详情收件箱 Tab 依赖原来的跨用户行为，新语义下显示空列表，见“建议 1”。

### D. 测试覆盖情况

| 场景 | 当前覆盖 |
|---|---|
| 租户 0 普通用户读列表只返回自己 | 有 |
| 显式 query 指定别人、检查 total | 缺少直接断言 |
| 租户 0 普通用户跨用户标已读 | 有，但夹具不是租户 0 双用户 |
| 租户 0 改状态、单条删除、空 ids 整箱删除 | 未完整覆盖；原测试为租户 5 |
| 平台管理员代客 | List、标已读有；其余写口未单独覆盖 |
| 系统上下文代客 | 标已读有；广播投递有 service 测试 |
| service 测试的新语义 | 现有断言不冲突，但不足以证明只返回自己 |

测试实际运行结果全部通过，覆盖强度问题见“应修 3”。

### E. 文档与代码是否一致

本轮 plan 新增结论、tenant isolation 收件箱说明和 README 三语与实现基本一致。Apache 接受记录、两份 LICENSE/NOTICE、Shivam Rathore 与 Contributors 版权信息一致；两份 LICENSE 和两份 NOTICE 分别用 `cmp` 比较相同。

plan 前面的“未修”“待决定”属于历史记录，建议加链接指向第 463 行起的新结论。`docs/tenant_isolation.md` 中“三端”措辞仍有遗留，与当前只维护 React 的仓库状态不一致。

## 5. 本地环境配置提交提醒

以下文件按要求未验收内容：

- `backend/app/admin/service/configs/data.yaml`
- `backend/app/admin/service/configs/oss.yaml`
- `backend/app/admin/service/configs/server.yaml`

验收结束时，三份配置均为已跟踪、未暂存状态（`git status --short` 显示 ` M`），暂存区为空，本次未提交。它们没有防误提交标记，不能保证后续不会被带入提交；`git add .` 会包含它们，正式提交时须显式排除。本次未修改 Git 索引或文件标记。
