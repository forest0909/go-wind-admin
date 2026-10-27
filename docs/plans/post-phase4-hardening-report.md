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

- `RoleService.Update`，且 mask 为 nil。nil 表示标量按全量比较；`permissions` / `org_units` / `field_permissions` 在 nil mask 下仓库不替换，所以只改这些关联、却不带 mask，不算变更
- `RoleService.Update`，mask 里有授权字段且仓库这次真的会写入、写入后的值也和旧值不同：`status`（启用/停用）、`data_scope`、`permissions`（权限点；菜单和接口挂在权限点上，`GetRolePermissionMenuIDs` / `GetRolePermissionApiIDs` 只是读）、`org_units` / `orgUnits`（选定组织）、`field_permissions` / `fieldPermissions`
- 同样会吊销的还有 `code`、`type`、`tenant_id` / `tenantId`。它们不是名称、描述、排序。`code` 还是 casbin 的主体
- `RoleService.Delete`。`sys_user_roles.role_id` 没有外键级联，所以删除前先记下持有人，删除成功后再吊销

不吊销的：

- mask 里只有 `name`、`description`、`sort_order` / `sortOrder`，以及 `updated_by`、`id`、`tenant_name`、`is_protected`、时间戳、`remark` 这类展示或审计字段
- `RoleService.Create` 和空库播种。新角色还没有持有人
- 权限、菜单、接口自己的 CRUD。那是改权限点定义，不是改「这个角色拿到了哪些权限点」。本次没有把它们算进吊销

判断发生在 `roleRepo.Update` 改写 mask 之前。保护角色会先被剥掉 `is_protected`、`type`、`status`、`code`；剥完只剩 `updated_by` 时不吊销，因为那些字段实际没写进去。空 mask 不是全量：`Update` 会先追加 `updated_by`，仓库只写审计字段，不吊销。页面把未改的授权字段一并放进 mask 时，按值比较，值相同不吊销。

授权变更的 `Update`，以及 `Delete`，都在写库之前列出持有人。列举失败直接返回错误，不更新、不删除。写成功之后的吊销失败仍只记日志。和改用户角色不同：那里用户 id 是已知的，列举失败仍吊销。

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

这次没有改「旧 access token 继续活到自己的 TTL」。宽限集合还有两个当时没写到的问题，验收后已修，见第 10 节：全量吊销不删 `rtg`；每次轮换把整组宽限时间往后推。

## 7. 替你做的决定

- 配置源顺序是主目录、远程源、`local/`。`local/` 最后加载，同时盖过主配置和远程源。Kratos 的目录加载会跳过子目录，所以 `local/` 不会被主源读两遍。合并是深度合并。local 里显式写出的标量都会覆盖同名键，包括空字符串、false 和 0；没写的键保留前面的值。
- 任务 1 除了指定的两份部署文档，还改了教程 02、上传文档、项目结构、backend AGENTS、backend README 和三份根 README 里指向配置目录的句子，避免文档还写「直接改 yaml」。
- 创建用户用事务，没有退到「插入前单独校验」。
- 角色吊销按仓库这次真的会写入的值，不按 mask 里有没有这个字段。mask 为 nil 时标量按全量比较，三项关联不替换。空 mask 会先追加 `updated_by`，只更新审计字段，不吊销。名称、描述、排序不吊销。`code`、`type`、`tenant_id` 只有真的写进去并且值变了才吊销。保护角色被剥掉的字段不算写成功，不因此吊销。
- 角色持有人列举失败时不更新、不删除。写成功之后吊销失败只记日志。
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

## 10. 验收后修复

Codex 验收上一轮不通过。下面按问题记改动、提交和测试。每个行为测试都先在修复前跑过，确认失败，再改代码，确认通过。配置零值那条除外：实现本来就会用显式标量覆盖，测试在改注释之前就是绿的，要改的是文档。前端空 catch 没有可失败的运行时断言，依据是那 10 个 chore 提交的 diff。

### A. 只改角色名称不再踢人 `8b74852`

管理端保存会把未改的 permissions、fieldPermissions、code、status、dataScope 放进 updateMask。原来只要 mask 里出现授权字段就吊销。`roleUpdateChangesGrant` 改成和 `role_repo.Update` 的写入条件对齐：字段在 mask 里（nil mask 时标量视为全量，空 mask 会先被追加 `updated_by`，只写审计字段），仓库这次真的会写，并且值变了，才吊销。集合忽略顺序和重复。字段权限按 resource 归并，隐藏字段按集合比，空列表和没有该资源相同。`org_units` 不在 mask 里时仓库保留原集合。`data_scope` 为 nil 或 UNSPECIFIED 不写。`tenant_id` 非空写不进去，只有 mask 把它置空才算变更。受保护角色被剥掉的 status、code、type 不参与比较。nil mask 不再对 `GetUpdateMask().Paths` 空指针。

测试 `TestRoleServiceSqlite_PagePayloadComparesGrantValues`：修复前，完整页面 payload 只改名称仍吊销（访问令牌被判失效）。修复后通过。只改权限点、数据范围、字段权限、选定组织或状态中的一项都会吊销持有人，旁观者不受影响。受保护角色提交 status 或 code，剥离后不吊销。

### B. panic 不再提交半成品用户 `0103414`

`UserService.Create` 的 defer 只看命名返回值 `err`。panic 时 err 仍是 nil，于是 Commit，留下用户行和角色关联，没有凭据。成功路径改为显式 Commit，Commit 失败返回错误。defer 只回滚还没提交的事务；`recover` 拿到值时先 Rollback 再 panic 出去。同一模式收进 `data.RunInTx` / `commitEntTx` / `finishEntTx`。

一并改了的手动事务：`user_repo`、`user_credential_repo`、`role_repo`、`tenant_repo`（`BeginTx` 不再返回会在 err 仍为 nil 时提交的 cleanup）、`tenant_service.CreateTenantWithAdminUser`、`dict_entry_repo`、`dict_type_repo`、`internal_message_recipient_repo`、`membership_repo`、`menu_repo`、`org_unit_repo`、`permission_api_repo`、`permission_group_repo`、`permission_menu_repo`、`plan_module_repo`、`plan_quota_repo`、`plan_repo`。原先有的函数返回值是未命名的，内层失败时 defer 仍可能 Commit，现在失败会回滚。

没有改的：`api_repo.DeleteExcept` 和 `tenant_usage_repo.CleanupTenantData` 本来就是成功才 Commit、出错 Rollback，panic 不会提交。`third_party/go-crud` 的 `MakeTxCleanup` 在提交前已经 recover。

原先任务 2 只覆盖了弱密码回滚。凭据写入 panic 仍会留下没有凭据的用户。这一条才补上。

测试 `TestUserServiceSqlite_CreatePanicRollsBackUser`：修复前用户表有 1 行。修复后用户表、三张关联表、凭据表都是 0 行。

### C. 全量吊销后宽限内的旧刷新令牌不能再换新令牌 `fd2aeb7`

`RevokeToken` 删了 AT、RT 和会话元数据，没删 `rtg:{ct}:{uid}`。校验失败后只要 RT 还在宽限集合里就放行。另外每次轮换都对整个宽限键 `EXPIRE`，后一次轮换把更早的成员也续上。

改成 ZSET，score 是该成员自己的过期时间（Redis TIME + 60 秒）。写入的 Lua 先按 score 清掉已过期成员再 ZADD。查询只认还没过期的成员。遗留的 SET 类型键直接删掉并拒绝。全量吊销删掉整个 `rtg` 键。

走「删除该用户全部令牌」的入口都进 `RevokeToken`：`RevokeUserToken`、`RevokeUserTokenAllClientTypes`（登出当前客户端、改密、重置密码、用户角色变化、角色吊销、租户清理）。单会话的 `RevokeTokenByJti` 不删宽限集合：集合是按用户而不是按 jti，单会话踢下线手里也没有那枚 RT 字符串。这一点记在 `docs/authentication.md`，没有改行为。

宽限窗口和强制下线的文档已按这个提交改过，本节不再重复改。

测试（miniredis，用 `SetTime` 而不是 `FastForward`，后者只缩短键 TTL，不移动 Redis TIME）：

- `TestUserTokenCache_RevokeTokenDropsGraceRefresh`：修复前，RT1 进入宽限后再全量吊销，`IsGrace` 仍为 true。修复后为 false。
- `TestUserTokenCache_GraceExpiryIsPerMember`：第 0 秒轮换 RT1，第 50 秒轮换 RT2，第 70 秒 RT1 被拒、RT2 仍在宽限内。修复前 RT1 仍被接受。
- `TestUserTokenCache_LegacyGraceSetIsRejected`：旧 SET 被拒绝并删除。

第 6 节建议的「把旧 AT 的 TTL 收短」仍然没做。

竞态（只分析，没改）：Lua 确认旧 RT 之后、`AddTokenPair` 写回新令牌之前，全量吊销可能已经完成，这次刷新仍会把新令牌写回去。建议吊销时递增 `rtv:{ct}:{uid}`，刷新在确认旧 RT 时记下版本，写回的 Lua 发现版本变了就放弃。这要改 `doGrantTypeRefreshToken` 和共用的 `AddTokenPair`，超出这一轮。

### D. 列不出持有人时不更新也不删除 `ef669e3`

授权变更的 `Update` 在 `roleRepo.Update` 之前列出持有人。查询失败返回错误，不写库。`Delete` 同样：列不出来就不删。写成功后再对事先记下的名单吊销，不二次查询。`Delete` 上方「列举失败只记日志」的注释已改掉。`docs/authentication.md` 第 8 节同步改成：列举失败中止写；写成功后的吊销失败仍只记日志。只改名称不列举持有人。

`userRoleRepo` 收成只含 `ListUserIDsByRoleIDs` 的接口，测试才能注入查询错误。生产仍传入 `*data.UserRoleRepo`。

测试 `TestRoleServiceSqlite_HolderListErrorAbortsGrantChange`：修复前，改数据范围返回 nil，角色已被改掉。修复后 `Update` 和 `Delete` 都返回错误，库里的角色不变。

### E. 停用角色不再参与授权 `afdc199`

从用户角色 ID 推导权限的位置，以及这次怎么处理：

| 位置 | 处理 |
|---|---|
| `authentication_service.go` 一对一授权（登录和刷新的 `resolveUserAuthority`） | `EnabledRoleIDs` 之后才算后台访问、角色码、管理员标志、数据范围、隐藏字段 |
| 同文件一对多成员身份 | 每个成员身份的角色 ID 同样过滤。当前默认是一对一，这条分支仍改了 |
| `authorizer_provider.go` `ProvidePolicies` | `status != ON` 不装策略，包括平台管理员通配 |
| `admin_portal_service.go` `GetMyPermissionCode`、`GetNavigation` | 用用户身上的角色 ID 取权限码和菜单之前过滤 |
| `role_repo.go` `ListPermissionIDsByRoleCodes` | 按令牌里的角色码反查权限时再滤一次。`permission_service` 的 `List` / `Get` 用它限制租户用户能看见的权限点 |

没有改的：`ListRoleCodesByRoleIds` / `ListRolesByRoleIds`（个人资料、用户列表回填、按 ID 查角色的 RPC），以及前端角色下拉和列表。停用角色仍显示。比较用户角色集合以决定是否吊销时也不过滤，否则停用前后对不上。

测试 `TestAuthSvcSqlite_DisabledRoleDroppedFromAuthority`：修复前三个子测试都失败。重新授权不报错，令牌里仍有该角色、SELF 数据范围和 `User.email`；casbin 仍放行该接口；平台管理员角色停用后旗标仍为 true，通配策略仍放行。修复后：唯一角色停用再授权返回错误，令牌里没有该角色、数据范围和隐藏字段；casbin 拒绝；平台管理员旗标不再置位，通配不再放行。启用时这三项都先断言成功，确认夹具本身是通的。

### F. 空 catch 补上原因 `f43e31f`

`CodeEditor.tsx` 语言检测失败退回调用方指定的语言或纯文本。同函数里 JSON 解析失败的空 catch 原来只有 `/* ignore */`，改成说明会继续做自动检测。`date.ts` 格式化失败显示占位符 `-`，不把错误抛进列表。两处都是本地展示兜底，没有改成 `console.warn`。

`28d0a4e` 到 `df4ee8c` 这 10 个 chore 提交里，`catch (error)` 改成 `catch {}` 的只有这两处。表里多出来的 `e6fe28c` 改的是没人读的传输参数，没有动 catch。

### G. 文档

1. `config.go`、`docs/backend_deploy.md`、`docs/windows-startup-guide.md`、`backend/AGENTS.md`、`backend/third_party/kratos-bootstrap/VERSION.md`，以及本报告第 7 节，不再写「非空值覆盖」。local 里显式写出的标量，包括空字符串、false 和 0，都会覆盖；没写的键保留原值。`TestLocalOverride_ExplicitZeroValuesReplaceMain` 在改注释之前就已经通过。角色 `tenant_id`「写不进非空值」和唯一索引那几处「非空值」说的是别的事，没改。
2. `docs/plans/multi-tenant-readiness.md`：`TenantPrivacy` 自己判断 `IsPlatformContext`，只改 `EnforceTenant` 不会带动它。补上异步广播在任务 tenant 为 0 时建立 `SystemContext`，以及队列里旧任务不能再默认全平台投递。grep 补进权限目录、用户租户强制、系统角色分配这三处要收紧的分支，其余标了不构成越权。
3. 本报告第 4、6、7 节里不准确的句子已改：非空值覆盖；只改名称仍会因为 mask 里带着未改授权字段而吊销；nil mask 和空 mask 都当成全量（空 mask 只更新审计字段）；任务 2 之后就不会留下没有凭据的用户（panic 仍会提交，见 B）；刷新分析漏掉宽限集合没删和整组续期。
4. 宽限窗口和强制下线的段落在 C 的提交里已经改过 `docs/authentication.md`。

受保护角色带 nil mask 的空指针已在 A 里用安全 getter 去掉。`TestStreamAddSubscriber` 本轮不修；全量测试若撞上，重跑并在门禁表注明。
