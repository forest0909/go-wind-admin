# post-phase4 hardening 报告

> 分支 `fix/post-phase4-hardening`，从 `chore/trim-phase-4` 的 `f302b0d` 切开。没有 push，没有改已有提交。每个提交除了要求的 `Co-Authored-By: Claude Opus 5.5` 之外，环境又自动加了一行 `Co-authored-by: Cursor`。按你的规则没有 amend。
>
> 没有启动后端，没有连接配置里的数据库、Redis、MinIO，没有设置 `GUARD_TEST_PG_DSN`。

## 1. 任务结果

### 任务 1：完成 `dd12f81`

本地配置改走 `configs/local/`，后加载，同名键覆盖主配置。远程 `config.yaml` 那条分支仍在，而且排在 `local/` 前面，所以本机目录盖得过远程源。

文件：`.gitignore`，三份根 README，`backend/AGENTS.md`，`backend/README.md`，`backend/third_party/kratos-bootstrap/VERSION.md`，`config/config.go`，`config/config_test.go`，`docs/backend_deploy.md`，`docs/backend_file_upload.md`，`docs/backend_project_struct.md`，`docs/tutorial/02-get-it-running.md`，`docs/windows-startup-guide.md`。

跟踪中的 `configs/{data,oss,server}.yaml` 已回到 HEAD。差异只在被 gitignore 的 `configs/local/`。

### 任务 2：完成 `8e1535b`

用了事务方案，没有改成「先校验再插入」。`UserService.Create` 在同一个 ent 事务里写用户行、角色/组织/岗位关联和凭据。复杂度不够时整笔回滚。

文件：`user_repo.go`（补了 `BeginTx`），`user_service.go`，`user_service_sqlite_test.go`，`docs/authentication.md`。

### 任务 3：完成 `8bb39d7`

角色授权变更成功后，按 `userRoleRepo.ListUserIDsByRoleIDs`（不过滤过期关联）查出持有人，逐个 `RevokeUserTokenAllClientTypes`。吊销失败只记日志。只改名称、描述、排序和审计字段不吊销。操作者自己持有该角色时一样被踢下线。

文件：`wiring_ent.go`，`role_service.go`，`role_service_sqlite_test.go`，`docs/authentication.md`，`docs/data_scope_design.md`，`docs/tenant_isolation.md`，`docs/tutorial/05-permission-model.md`。

授权变更路径的完整清单见第 4 节。

### 任务 4 和 5：完成，合成一个提交 `56cc077`

删掉了 `stripRecipientUserIDQuery`、`removeRecipientUserKeys`、`isRecipientUserFilterKey`，以及 `List` 里改写 `req.FilteringType` 的几行。登录用户的 query 若把 `recipient_user_id` 写成别人，和归属谓词 AND 之后返回空表。

`Count`、`IsExist`、`Get`、`Update`、`Delete` 没有对外 HTTP。行为没改，每个方法上方加了注释：不做收件人归属检查，只供系统上下文或内部任务使用；暴露给 HTTP 之前必须加 `inboxRestrictToViewer`。调用方见第 5 节。

文件：`internal_message_recipient_repo.go`，`internal_message_recipient_repo_sqlite_test.go`，`docs/plans/trim-and-own-deps.md`，`docs/tenant_isolation.md`。

### 任务 6：完成（只分析）

没有改代码。分析在第 6 节。

### 任务 7：完成 `b4b2338`

只新增 `docs/plans/multi-tenant-readiness.md`。没有改那些路径的代码。

### 任务 8：部分完成

只动了不改变运行时行为的未使用参数、未使用的 `catch` 绑定，以及一处 `prefer-const`。没有碰 `no-explicit-any`、`react-hooks/exhaustive-deps`、`no-empty-object-type`，也没有碰 `src/api/generated/`。

用来从展开对象里剔除字段的绑定留着。删掉它们会把 `tenant_id` 留在查询里，或把 `onHeaderCell` 铺到 DOM 上。清单见第 9 节。

| 提交 | 目录 |
|---|---|
| `e6fe28c` | `src/api/client.ts` |
| `28d0a4e` | `src/components/common/Editor` |
| `a6f464c` | `src/layouts/MainLayout` |
| `9214d41` | `src/pages/app/internal-message` |
| `efc337a` | `src/pages/app/opm` |
| `043827b` | `src/pages/app/permission` |
| `9892858` | `src/pages/app/system` |
| `5ac356d` | `src/pages/app/tenant` |
| `dd512d1` | `src/stores/auth.ts` |
| `88b4faa` | `src/utils/date.ts` |
| `df4ee8c` | 基线 `docs/plans/react-lint-baseline.tsv` |

基线：909 条（659 error，250 warning）→ 876 条（628 error，248 warning）。`--fix` 可修的 warning 从 131 条变为 130 条。少掉的那一条 warning 是 `prefer-const`；另外 `DictEntryDrawer` 的 `save` 去掉未使用参数后，`max-params` 少了一条。

这些目录互不依赖。`npm run typecheck` 和 `npm run build` 在全部改完、拆提交之前跑过一次，通过。没有每个目录各跑一遍。

## 2. 新测试和回退确认

| 测试 | 回退时 |
|---|---|
| `TestLocalOverride_NestedKeyWinsAndOmittedKeyKept` | 失败。不挂 `local/` 时嵌套键仍是主配置的值 |
| `TestRemoteConfigSourceStillLoadsAndLocalOverridesIt` | 失败。不挂 `local/` 时被远程源盖住的键回不去 |
| `TestNoLocalDir_MatchesMainSourceOnly` | 仍然通过。这条本来就是「没有 local 目录时行为不变」，回退功能不会让它失败 |
| `TestUserServiceSqlite_CreateRollsBackUserWhenCredentialRejected` | 失败。弱密码时用户表里留下 1 行，期望 0 行 |
| `TestRoleServiceSqlite_GrantChangeRevokesHolderNotBystander` | 失败。先在停用角色处失败（持有人令牌仍有效，期望失效）。单独拿掉停用、只留改权限时，改权限断言同样失败。只改名称的断言在回退时仍然通过，因为那条路径本来就不吊销 |
| `TestRoleServiceSqlite_DeleteRevokesHolderTokens` | 失败。删除后持有人令牌仍有效 |
| `TestInternalMessageRecipientInboxListSqlite_TenantZeroStaysOwn` 里「写成别人的 recipient_user_id」 | 失败。把 query 丢掉之后得到 1 行，期望 0 行。没有为了通过去改断言 |

回退只用于确认，确认后都已恢复。没有改断言来迁就失败。

## 3. 任务 1 的配置比对

备份目录：`/tmp/goadmin-config-backup-20261027-005657/`（只有当时的 `data.yaml`、`oss.yaml`、`server.yaml`）。

一次性程序在 `/tmp/goadmin-config-cmp.go`，没有提交。它分别起进程调用 `LoadBootstrapConfig`，再用 protojson 比较。

结果：`STRUCT_EQUAL`。两边序列化都是 5733 字节。

和 HEAD 不同、因此写进 `configs/local/` 的键（值不写在这里）：

- `data.database.source`
- `data.redis.addr`
- `data.redis.password`
- `oss.minio` 的 `access_key`、`secret_key`、`endpoint`、`download_host`、`upload_host`
- `server.asynq.uri`
- `server.rest.timeout`

都是字符串标量，没有删键。`git status` 不再出现 `configs/` 下的跟踪文件。

## 4. 任务 3：会改变「某个角色授予什么」的路径

管理端 HTTP 上没有单独的「分配菜单 / 接口 / 权限 / 字段权限 / 数据范围」RPC。这些都是 `Role` 消息上的字段，经 `RoleService.Update` 写入。`RolePermissionRepo.ReplacePermissions`、`RoleOrgUnitRepo.ReplaceOrgUnits`、`RoleFieldPermissionRepo.ReplaceFieldPermissions` 只被 `role_repo.go` 的创建、更新、删除调用。

会吊销的：

- `RoleService.Update`，且 field mask 为空或缺失（当成全量更新）
- `RoleService.Update`，mask 里有授权字段：`status`（启用/停用）、`data_scope`、`permissions`（权限点；菜单和接口挂在权限点上，`GetRolePermissionMenuIDs` / `GetRolePermissionApiIDs` 只是读）、`org_units` / `orgUnits`（选定组织）、`field_permissions` / `fieldPermissions`
- 同样会吊销的还有 `code`、`type`、`tenant_id` / `tenantId`。它们不是名称、描述、排序。`code` 还是 casbin 的主体
- `RoleService.Delete`。`sys_user_roles.role_id` 没有外键级联，所以删除前先记下持有人，删除成功后再吊销

不吊销的：

- mask 里只有 `name`、`description`、`sort_order` / `sortOrder`，以及 `updated_by`、`id`、`tenant_name`、`is_protected`、时间戳、`remark` 这类展示或审计字段
- `RoleService.Create` 和空库播种。新角色还没有持有人
- 权限、菜单、接口自己的 CRUD。那是改权限点定义，不是改「这个角色拿到了哪些权限点」。本次没有把它们算进吊销

判断发生在 `roleRepo.Update` 改写 mask 之前。保护角色会先被剥掉 `is_protected`、`type`、`status`、`code`；剥完只剩 `updated_by` 时不吊销，因为那些字段实际没写进去。

列举持有人失败时只记日志，更新或删除仍然成功。和改用户角色不同：那里用户 id 是已知的，列举失败仍吊销；这里不知道该踢谁。

## 5. 任务 5：调用方

`InternalMessageRecipientRepo` 的这五个方法：

| 方法 | 谁在调用 |
|---|---|
| `Count` | 没有。方法自己之外无引用 |
| `IsExist` | 只有同文件 `Update` 的 `allow_missing` |
| `Get` | 只有 sqlite 测试 `TestInternalMessageRecipientRepoSqlite_EnumReadback`，上下文是系统上下文 |
| `Update` | 没有服务层或 HTTP 调用方 |
| `Delete` | 没有服务层或 HTTP 调用方 |

管理端 HTTP（`i_internal_message_recipient.proto`）只有 `ListUserInbox`、`DeleteNotificationFromInbox`、`MarkNotificationAsRead`、`MarkNotificationsStatus`。它们走 `List` 和已经带 `inboxRestrictToViewer` 的写方法，不走上面五个。

领域 proto `internal_message_recipient.proto` 里有 `Count` / `Get` / `Update` / `Delete`，管理端服务没有实现，也没有挂到 HTTP。

因此没有补归属检查。注释已经写上。

## 6. 任务 6：刷新后旧 access token 仍有效

现在的行为：

`VerifyAndRevokeTokenPair` 跑 Lua `verifyAndRevokeRefreshTokenScript`。脚本只做四件事：读 `rt:{ct}:{uid}:{jti}`，值必须等于这枚 refresh token；删掉这枚 RT；删掉会话元数据 `us:{ct}:{uid}:{jti}`；把这枚 RT 放进宽限集合 `rtg:{ct}:{uid}`，TTL 60 秒（`RefreshTokenGraceWindow`）。返回 1 表示轮换成功。

脚本不碰 `at:{ct}:{uid}:{jti}`。之后 `Authenticate` 走 `IsValidAccessToken`：Redis 里这把 AT 还在，且值和请求一致，就仍然有效，直到这把 AT 自己的 TTL（任务说明里的上限是 5400 秒），或者有人沿 `lin:{ct}:{uid}:{jti}` 代际链把它删掉。强制下线 `RevokeTokenByJti` 会顺着代际链删祖先 AT。改密、改用户角色、以及这次的改角色授权，走的是全量吊销，不靠这条刷新路径。

宽限集合管的是 refresh token，不是 access token。同一枚旧 RT 在 60 秒内再来一次（并发刷新里输掉的那一方），`IsGraceRefreshToken` 放行，给它换一个新会话，而不是报刷新令牌无效把人踢下线。

为什么这样设计：

注释写的是多标签页、多个前端共用同一个浏览器 cookie。刷新轮换会换掉 cookie 里的 RT。如果同时删掉上一枚 AT，另一个标签页内存里还拿着那枚 AT，下一次请求就是 401。

如果刷新时立刻删掉旧 AT，会出什么问题：

1. 标签页 B 还在用 AT1。标签页 A 刷新成功，cookie 换成 RT2，并签发 AT2。若此时删掉 AT1，B 的在途请求和随后的请求都 401。
2. B 再用共享 cookie 去刷新。若 cookie 已经是 RT2，这次刷新会再轮换一次，签发 AT3。若刷新就删旧 AT，A 刚拿到的 AT2 也被删掉。两个标签页互相把对方的 AT 作废，形成 401 循环。
3. 60 秒宽限只覆盖「同一枚旧 RT 又被提交」。它能让输掉的那次刷新另开一个会话，不能让还没去刷新、手里仍是旧 AT 的请求继续成功。在途请求会先 401。

建议：

轮换成功后不要 `DEL` 旧 AT，把 `at:{ct}:{uid}:{jti}` 的剩余 TTL 改成很短，和宽限窗口同一量级（30 到 60 秒）。`EXPIRE` 可以放进现有 Lua，避免和轮换竞态。

代价：

- 旧 AT 里的角色、数据范围、字段黑名单仍可用这一小段时间。角色授权变更已经会整用户吊销，这段窗口主要留给「刷新换了新声明，旧 AT 还没死」和「令牌被拿走之后的剩余寿命」。
- 窗口太短，标签页来不及刷新，401 循环会回来。窗口太长，被盗的旧 AT 就还能用。
- 强制下线仍应立刻删，不能等这段 TTL。代际链已经是这条路径，不要改成只靠过期。
- 多一次 Redis `EXPIRE`，没有额外的键。

这次没有改代码。

## 7. 替你做的决定

- 配置源顺序是主目录、远程源、`local/`。`local/` 最后加载，同时盖过主配置和远程源。Kratos 的目录加载会跳过子目录，所以 `local/` 不会被主源读两遍。合并是深度合并，后面的非空值覆盖前面的。
- 任务 1 除了指定的两份部署文档，还改了教程 02、上传文档、项目结构、backend AGENTS、backend README 和三份根 README 里指向配置目录的句子，避免文档还写「直接改 yaml」。
- 创建用户用事务，没有退到「插入前单独校验」。
- 角色吊销按 field mask。无 mask 当全量。名称、描述、排序之外的字段都吊销，包括 `code`、`type`、`tenant_id`。保护角色被剥掉的字段不算写成功，不因此吊销。
- 角色持有人列举失败只记日志，不把已提交的更新或删除判失败。
- `RoleService.Create` 不吊销。新角色没有持有人。
- 权限点、菜单、接口自己的增删改不吊销。它们不是「这个角色被授予了什么」的写口。
- 历史计划 `docs/plans/trim-and-own-deps.md` 里「改角色定义不在本次范围」留着，那是当时的执行记录。收件箱那句按任务 4 改成了新语义。现行行为写在 `authentication.md`、`data_scope_design.md`、`tenant_isolation.md` 和教程 05。
- 收件箱 `Count` / `Get` / `Update` / `Delete` 确认没有 HTTP 之后，只加注释。
- 前端 lint 没有每个目录单独跑 typecheck/build，合并跑了一次再拆提交。剔除字段用的未使用绑定没有删。
- 每个提交都保留了环境自动加上的 Cursor 共同作者行。

## 8. 最终门禁

| 检查 | 结果 |
|---|---|
| `backend/` `go build ./...` | 通过 |
| `backend/` `go vet ./...` | 通过 |
| `backend/` `go test -count=1 ./...` | 通过，退出码 0，约 92 秒 |
| `make api && make openapi && make ts && make ent` | 通过。之后 `git status` 没有生成文件 diff |
| `frontend/admin/react` `npm run typecheck` | 通过 |
| `frontend/admin/react` `npm run build` | 通过 |
| `npm run lint` 对基线逐条 | 与新基线一致：876 条（628 error，248 warning）。多出来的 0 条，少掉的 0 条。其中两条 `no-non-null-assertion` eslint 对同一位置报了两次，基线也记了两次 |
| `bash scripts/check-no-upstream-deps.sh` | 通过 |
| `git diff --check` | 通过（工作区干净） |
| `git status` | 干净。`configs/` 下没有跟踪文件的改动。`configs/local/` 被 gitignore |

## 9. 还需要你处理的

- 本机覆盖在 `backend/app/admin/service/configs/local/`，已忽略。备份仍在 `/tmp/goadmin-config-backup-20261027-005657/`。要长期留下就自己拷走，`/tmp` 会丢。
- 重新启用多租户按 `docs/plans/multi-tenant-readiness.md`。这次没改代码。创建租户管理员的页面仍在发明文密码：`TenantDrawer.tsx` 调用 `POST /admin/v1/tenants:with-admin`。菜单对新库是隐藏的，接口还在。
- 刷新后的旧 access token 仍活到自己的 TTL。建议和代价在第 6 节，没有改。
- 下面这些未使用绑定是故意留的，删了会改变行为：
  - `pagination.ts` 里去掉 `tenant_id` / `tenantId` 的解构
  - `transform-meta-to-handle.ts` 里去掉 `title` / `icon` / `authority` 的解构
  - `TableSkeleton.tsx` 里去掉 `onHeaderCell` / `resizable`
  - `PageContainer` 里从 `restProps` 拆出的 `contentPadding`、`contentClassName`、`keepAlive`
  - `csv.ts` 里去掉 `current` / `pageSize`
- 前端 lint 还剩 876 条。大头是 `no-explicit-any`（479）、`no-empty-object-type`（79）和若干格式类 warning。按任务没有动它们。
- 改角色授权时，操作者如果自己持有这个角色，会被踢下线。这是预期，文档已写。
- 分支没有 push。
