# 重新启用多租户前要处理的事

> 2026-10-27 只读分析，没有改这些路径的代码。单租户现在的用法是：业务行的 `tenant_id` 都是 0，侧栏租户菜单隐藏（`HideInMenu`，新库播种；老库要菜单同步才会藏）。下面每一处都把 `tid == 0` 当成平台身份，或者留着明文口令。启用多租户之前要先改，否则租户 0 上的普通账号会继续被当成平台。

判断口径：`viewer.UserContext.IsPlatformContext()` 就是 `tid == 0`（`backend/third_party/go-crud/viewer/user_context.go`）。`SystemContext.IsPlatformContext()` 恒为 true，同时 `IsSystemContext()` 也是 true。数据范围那条 `PermissionRule` 已经改成只认系统上下文，不在这份清单里。

## 1. `TenantService.CreateTenantWithAdminUser` 收明文密码

现在的行为：

- HTTP 还在：`POST /admin/v1/tenants:with-admin`。
- 前端有调用。`frontend/admin/react/src/api/hooks/tenant.ts` 的 `useCreateTenantWithAdminUser`，页面是 `src/pages/app/tenant/tenant/components/TenantDrawer.tsx`。创建提交把表单里的 `values.password` 原样放进请求体，没有走 `encryptLoginPassword`。
- 租户目录菜单播种时 `HideInMenu: true`，权限是 `sys:platform_admin`。侧栏看不到，直接打开路由或老库菜单还在时，这个接口仍会被打到。
- 服务端 `tenant_service.go` 把 `req.GetPassword()` 原样交给 `userCredentialsRepo.CreateWithTx`。`UserCredential` 上没有 `NeedDecrypt`。`prepareCredential` 把拿到的字符串当明文做复杂度校验再 bcrypt。创建用户那条路已经先 AES 解密再入库，这里没有。
- proto `CreateTenantWithAdminUserRequest.password` 没有 `(redact.value)`。非法参数日志走 `applogging.FormatArgs`，字段名 `password` 会被访问日志的兜底名单清空。访问日志中间件同样按字段名兜底。注解本身仍然缺。

单租户风险：没有跨租户问题。风险是口令明文走 JSON。菜单藏起来不等于接口关了。

启用前：

- 前端用和创建用户相同的 AES 加密再提交。
- 服务端在 `CreateWithTx` 之前复用创建用户的解密（`decryptTransportPassword` 那条），不要在 `prepareCredential` 里再复制一份。
- 给 `password` 加上 `(redact.value).string = ""`，然后 `make api`。
- 确认租户菜单重新露出之后，这条创建路径和「创建用户」的口令传输一致。

## 2. `export_common.go` 的 `authorizeExportRequest`

现在的行为：

- 手动导出路由不走 Kratos 中间件，这个函数自己验 Bearer，再重建 viewer。
- `operator.GetTenantId() == 0` 时换成 `SystemContext`（租户谓词和数据范围都放行）。`tid > 0` 才是 `UserContext`。
- 全仓只有 `AuditExportService.ServeExport` 调用它，紧接着 `requireExportPlatformAdmin`（看令牌 `is_platform_admin`，不是看 tid）。不是平台管理员会 403，查询还没发生。

单租户风险：当前这一条导出是安全的，因为第二道闸门看的是平台管理员旗标。所有人的 tid 都是 0，所以第一道分支对所有登录用户都是 `SystemContext`；非管理员在第二道被拒绝。新写一个导出如果只复制 `authorizeExportRequest`、忘了平台管理员闸，单租户下任何登录用户都会以系统上下文导出全表。

启用前：

- 不要用 `tid == 0` 决定是不是系统上下文。平台管理员用 `is_platform_admin`（或显式的系统任务上下文）。
- 租户用户即使将来 tid 不是 0，也要保持现在的 `UserContext`，让 mixin 收窄到本租户。
- 每个新的手动导出口都要自己再过一遍平台闸或租户闸，不能假设这个函数已经做完授权。

## 3. `user_credential_repo.go` 的 `maybeTenantFromViewer`

现在的行为：

- viewer 不存在，或者 `TenantID() == 0`，返回「没有租户谓词」。
- 凭据上的 `GetByIdentifier`、`DeleteByIdentifier`，以及同文件里按 identifier 查询的另外几处，在「没有租户谓词」时不加 `tenant_id` 条件。注释写的是「平台管理员 tenant_id=0 不限定」。
- 同一个函数还被角色、用户、岗位、字典类型、任务、成员身份的按 name/code 查询调用。那些调用点在 tid 为 0 时直接返回 400，避免跨租户匹配多行。不构成越权。
- 登录不走这个函数。`FindUserCredential` 由调用方传入 `tenantID`，`TenantIDEQ` 一直在。`VerifyCredential` 写死租户 0。
- 管理端 HTTP 没有把 `GetByIdentifier` / `DeleteByIdentifier` 暴露出去。它们在领域 gRPC 桩和 sqlite 测试里。

单租户风险：凭据行都在租户 0，少一条谓词也不会读到另一个租户。没有对外 HTTP 时，现网请求碰不到。

启用前：

- `tid == 0` 不能再表示「跨租户」。普通用户必须带上自己的租户谓词。
- 平台管理员要跨租户查、删时，单独看 `is_platform_admin` 或系统上下文，不要和「令牌上的租户号是 0」绑在一起。
- `VerifyCredential` 写死租户 0 也要一起改，否则租户用户的登录校验会落在平台凭据上。登录主路径已经按租户号查询，不要让这条内部 RPC 成为例外。

## 4. `schema/tenant_mutation_guard.go`

现在的行为：

- 挂在带租户列的表的 `Policy()` 上，是 `TenantPrivacy` 之外的第二道写守卫。
- `EvalQuery` 什么都不做（查询靠 go-crud 的 `TenantPrivacy`）。
- 非 Create 的变更：没有 viewer 就拒绝。`IsPlatformContext()` 或 `IsSystemContext()` 为真则不注入 `tenant_id` 谓词。其余情况 `WhereP` 加上 `tenant_id = viewer`。
- `UserContext` 的 `IsPlatformContext()` 就是 `tid == 0`，所以租户号为 0 的登录用户改、删任何行都不会被这道守卫挡住。

单租户风险：库里只有租户 0 的行，放行等于「同一租户里的人可以按主键改」。这和现在的单租户模型一致，没有第二个租户可越。数据范围是另一条规则，不靠这个守卫。

启用前：

- 平台放行改成系统上下文，或改成令牌上的平台管理员旗标。不要用 `tid == 0`。
- 业务用户必须先有非 0 的 `tenant_id`，再打开多租户。留在 0 上的普通账号会继续绕过这道守卫，也会绕过下面的 `TenantPrivacy`。

## 5. go-crud 里把 `tid == 0` 当平台的规则

这些文件在 `backend/third_party/go-crud/`。真要改，记进该目录的 `VERSION.md`。

### `entgo/rule/tenant.go` 的 `TenantPrivacy`

现在的行为：查询和变更都在 `IsPlatformContext()` 或 `IsSystemContext()` 时不注入 `tenant_id`。Create 时平台上下文尊重调用方已经写上的 `SetTenantID`，没写则保持原样（可以是 0）。普通用户 Create 会被强制改成自己的 tid。变更时非平台用户不能把 `tenant_id` 改到别处。

这是生产路径。带 `TenantID` mixin 的表都挂了它。

单租户风险：和守卫相同。所有人 tid 都是 0，查询和变更都不加租户谓词。库里没有别的租户，所以看不到别人的租户数据。

启用前：和守卫用同一套判断。平台管理员跨租户是显式能力，不是「租户号碰巧为 0」。租户用户的 Create 必须被盖成自己的 tid，不能因为令牌 tid 是 0 就允许写入任意租户。

### `viewer/enforce.go` 的 `EnforceTenant`

现在的行为：缺 viewer 返回错误。`IsPlatformContext()` 或 `IsSystemContext()` 则 `Enforce=false`（不加谓词）。否则要求调用方注入 `TenantID`。`InjectTenantWhereIntoBuilder` 走这个决策。

单租户风险：决策结果和 `TenantPrivacy` 一样，tid 0 不加谓词。没有第二个租户时不漏数据。

它和 `TenantPrivacy` 各判各的。`TenantPrivacy.EvalQuery` / `EvalMutation`（`entgo/rule/tenant.go`）直接读 `viewer.IsPlatformContext()` 和 `IsSystemContext()`，不调用 `EnforceTenant`。Create 分支也是直接看 `IsPlatformContext()`。只改 `EnforceTenant`，隐私规则不会跟着变。

现在调用 `EnforceTenant` 的是：

- `InjectTenantWhereIntoBuilder`：repository 写 builder 上的行级谓词。
- `EnforceOnScopedInstance` / `EnforceOnScopedInstanceAny`：Create 时按决策 `SetTenantID`。

启用前要分开改，不能指望改一处就带动另一处：

1. `UserContext.IsPlatformContext`（`viewer/user_context.go`，现在就是 `tid == 0`）。这一处变了，所有直接调用它的规则一起变：`TenantPrivacy`、`schema/tenant_mutation_guard.go`、`TenantInterceptor`、`entgo/rule/system.go` 里下面四个规则。
2. `EnforceTenant` 自己的平台分支。如果只改这个函数、不改 `IsPlatformContext`，builder 注入和 Create 强制设租户会变，`TenantPrivacy` 仍按 `tid == 0` 放行，查询和变更的放行条件分叉。
3. 如果只改 `IsPlatformContext`、不改 `EnforceTenant`，隐私规则变了，但 `EnforceTenant` 仍读旧的 `IsPlatformContext`，两边会一起变。所以真正要改的是「什么叫平台」这一个谓词；`EnforceTenant` 不必再复制一套判断，除非它要改成认平台管理员旗标而 `IsPlatformContext` 仍表示别的含义。那种情况下两边都要改。

### `entgo/interceptor/tenant.go` 的 `TenantInterceptor`

现在的行为：查询拦截器，同样在平台或系统上下文放行，否则反射注入 `tenant_id`。

生产没有注册。`entgo/mixin/tenant_id.go` 里那行 `Intercept` 是注释；`entgo/client_test.go` 里也是注释。真正跑到的只有这个包自己的拦截器测试。当前查询隔离靠的是 `TenantPrivacy`，不是它。

单租户风险：没挂上，所以没有运行时效果。

启用前：在 `IsPlatformContext` 还等于 `tid == 0` 时不要把它注册进 client。注册之后会和 `TenantPrivacy` 叠两层，而且放行条件和现在的单租户漏洞一样。它直接判断 `IsPlatformContext`，不会因为只改了 `EnforceTenant` 就收紧。要留着，就和 `TenantPrivacy` 用同一套平台判断。

### `entgo/rule/system.go` 里另外 4 个规则

`PermissionRule` 已经去掉 `IsPlatformContext()`，只留 `IsSystemContext()`。下面四个没改，平台判断仍是 `IsPlatformContext() || IsSystemContext()`：

| 规则 | tid==0 时 | 全仓调用 |
|---|---|---|
| `OwnerOnlyRule` | 不注入 `created_by` | 只有定义 |
| `SoftDeleteRule` | 不过滤 `deleted_at`，软删行可见 | 只有定义 |
| `DenyFieldsMutationRule` | 不拦截受保护字段的修改 | 只有定义 |
| `LimitFieldAccessRule` | 不检查字段权限 | 只有定义 |

单租户风险：没有业务调用，现在不会生效。

启用前：如果要把它们挂到表上，先拿掉 `IsPlatformContext()` 放行，和 `PermissionRule` 一样只留系统上下文（或改成平台管理员旗标）。否则一挂上，租户 0 的普通用户就跳过归属、软删和字段保护。

## 6. 和本清单一起看、但代码不在上面几处的闸门

HTTP 租户闸门只在 `tokenPayload.GetTenantId() > 0` 时跑（`pkg/middleware/auth/auth.go`）。说明在 `docs/tenant_isolation.md` 第 4.1 节。单租户下所有人 tid 都是 0，闸门休眠，这是现设计。启用多租户后，只有真正的租户用户会进闸门；留在 tid 0 的普通账号既绕过闸门，也绕过上面的隐私规则。

启用前的顺序建议：

1. 先改 `UserContext.IsPlatformContext` 的含义，再核对直接调用它的规则（`TenantPrivacy`、写守卫、`TenantInterceptor`、`system.go` 四个规则）。`EnforceTenant` 现在也是读这个谓词；只改 `EnforceTenant` 的函数体不会让 `TenantPrivacy` 跟着变。然后迁数据。只迁数据、不改判断，平台管理员会丢跨租户能力；只改判断、不迁数据，现有 tid 0 用户会被收进租户 0 的谓词，和平台行混在一起。
2. 业务用户改到非 0 租户，平台管理员继续 tid 0 也可以，但代码里的放行必须认角色旗标或系统上下文，不能认「tid 是 0」。
3. 然后处理创建租户管理员的口令加密、导出函数的 viewer 选择，以及下面第 7 节的异步广播。队列里 tenant 为 0 的旧任务要先处理掉，再切判断。
4. 最后才考虑注册 `TenantInterceptor` 或挂上那四个还没人调用的规则。

## 7. 站内信异步广播：任务 tenant 为 0 时用系统上下文

位置：`internal_message_service.go` 的 `AsyncBroadcastMessage`（约第 590 行）。

现在的行为：

- asynq handler 的 ctx 没有请求期 viewer。`taskData.TenantId == 0` 时直接 `viewer.WithSystemContext`。大于 0 时用 `NewUserContext(0, tenantId, ...)`，用户 id 是 0。
- 注释写明：升级前已入队、payload 里没有 `tenant_id` 的旧任务，按 0 处理，退回升级前的全平台投递。
- `executeBroadcast` 在这个 ctx 上 `userRepo.List`。系统上下文不过 `TenantPrivacy`，名单是全表用户。收件行再按广播方租户打标。

单租户风险：所有人本来就在租户 0，全表投递和现在的广播范围一致。不构成多一个租户的越权。

启用前：

- 不要再把任务 tenant 0 当成「全平台」。平台管理员的全员广播应看令牌上的平台管理员旗标，或显式的系统任务身份，不能看「payload 里的租户号是 0」。
- 租户广播必须带上该租户的 viewer。用户 id 为 0 的 `UserContext` 只够注入租户谓词，不要把它当成某个真实用户。
- 已经在队列里的旧任务：payload 没有租户字段时会反序列化成 0，一切过去就变成系统上下文，向所有租户发信。启用前停掉 worker，把这些任务标失败或按消息行上的 `tenant_id` 改写后再跑。不能靠「默认 0 = 升级前行为」过渡。消息行本身若也是租户 0，说明当时就是平台广播，要单独认平台管理员，而不是把队列里所有 0 都放行。

## 8. 同一轮 grep 里其余的 `tid == 0`

检索：`IsPlatformContext()`、`tenantID == 0`、`GetTenantId() == 0`、`tid == 0`。测试桩和 mock viewer 不列入。上面第 2 到第 7 节已经写过的不重复。

需要在启用前收紧（留在 tid 0 的普通账号会走到平台分支）：

- `permission_service.go` 的 `List` / `Get`：只有 `operator.GetTenantId() > 0` 才按角色码收窄可见权限。tid 为 0 时列出或读取全部权限点。
- `user_service.go` 的创建和更新：只有操作者 tid 大于 0 才把目标用户的 `tenant_id` 改成操作者自己的。两边都是 0 时，角色查询限定为 `type: SYSTEM`，也就是可以分配系统角色。
- `role_service.go` 的创建和更新：只有操作者 tid 大于 0 才把角色类型收成 `TENANT`，并禁止改系统角色。tid 为 0 可以建、改系统角色。

不构成越权：

- `authorizer_provider.go`：`role.GetTenantId() == 0` 并且角色码是 `platform:admin` 才装通配策略。认的是这一个角色行，不是调用方的 tid。
- `user_service.go` 的 `Delete`：默认管理员（id 为 1，或用户名是 admin 且租户是 0）删不掉。这是多一道保护。
- `user_service.go` 创建用户时 `targetTenantID == 0` 回落到操作者租户，最后仍为 0 就跳过 `USER_LIMIT`。只跳过配额。
- `my_tenant_usage_service.go`：tid 为 0 返回空用量，不读别的租户。
- `file_transfer_service.go`：tid 为 0 不查 `STORAGE` 配额。只跳过配额。
- `user_profile_contact.go`：tid 大于 0 才按用户 locale 选验证码文案。tid 为 0 只是少了这一次语言选择。
- `internal_message_service.go` 的 `executeBroadcast`：收件人 `tenant_id == 0` 且广播方不是 0 时，把收件行改标成广播方租户，避免行落在读者读不到的租户 0。不扩大查询范围。
- `internal_message_recipient_repo.go`：注释和测试都写明 `tenant_id == 0` 不是收件箱放行条件。系统上下文和平台管理员角色另判。
- `data_scope_guard.go`：只认系统上下文，不把 tid 0 当全量。本清单开头已排除。
- 第 3 节里按 name/code 查询、tid 为 0 就返回 400 的那些调用点。
