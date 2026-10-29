# 方案二交付说明

已实现改动项 1～5，保留实施前未提交的增量权限同步、中文名称、权限树分层等改动；没有提交 commit。
未改表结构、proto、鉴权中间件或后端服务依赖；非 CRUD 动作保持原样。
所有门禁均已运行，但全量 Go 测试受沙箱网络监听限制而失败，不能声称全部门禁通过。

## 本次改动文件

下列清单仅列本次新增或继续修改的文件，不把实施前的其他改动算作本次实现。

- `backend/app/admin/service/internal/data/permission_repo.go`
- `backend/app/admin/service/internal/service/admin_portal_service.go`
- `backend/app/admin/service/internal/service/admin_portal_service_test.go`
- `backend/pkg/constants/default_data.go`
- `frontend/admin/react/src/components/common/AccessButton/index.tsx`
- `frontend/admin/react/src/components/common/TableActions/index.tsx`
- `frontend/admin/react/src/router/modules/permissions.tsx`
- `frontend/admin/react/types/autoImports.d.ts`
- `docs/frontend_authority.md`
- `docs/plans/role-menu-rbac-ux-delivery.md`
- `frontend/admin/react/src/locales/zh-CN/_modules/api.json`
- `frontend/admin/react/src/locales/zh-CN/_modules/menu.json`
- `frontend/admin/react/src/locales/en-US/_modules/api.json`
- `frontend/admin/react/src/locales/en-US/_modules/menu.json`
- `frontend/admin/react/src/pages/app/opm/user/list/UserList.tsx`
- `frontend/admin/react/src/pages/app/opm/position/index.tsx`
- `frontend/admin/react/src/pages/app/opm/org-unit/index.tsx`
- `frontend/admin/react/src/pages/app/permission/role/index.tsx`
- `frontend/admin/react/src/pages/app/permission/menu/index.tsx`
- `frontend/admin/react/src/pages/app/permission/api/index.tsx`
- `frontend/admin/react/src/pages/app/permission/permission/PermissionList.tsx`
- `frontend/admin/react/src/pages/app/permission/permission/PermissionGroupList.tsx`
- `frontend/admin/react/src/pages/app/system/config/index.tsx`
- `frontend/admin/react/src/pages/app/system/dict/DictTypeList.tsx`
- `frontend/admin/react/src/pages/app/system/dict/DictEntryList.tsx`
- `frontend/admin/react/src/pages/app/system/language/index.tsx`
- `frontend/admin/react/src/pages/app/system/task/index.tsx`
- `frontend/admin/react/src/pages/app/system/file/index.tsx`
- `frontend/admin/react/src/pages/app/internal-message/category/index.tsx`
- `frontend/admin/react/src/pages/app/internal-message/message/index.tsx`
- `frontend/admin/react/src/pages/app/tenant/tenant/index.tsx`
- `frontend/admin/react/src/pages/app/tenant/plan/PlanList.tsx`
- `frontend/admin/react/src/pages/app/tenant/plan/PlanQuotaList.tsx`

`types/autoImports.d.ts` 是构建工具为新增 AccessButton 自动登记的声明。
API、OpenAPI、TS 客户端、Ent 生成目录没有新增差异。

## 页面 → 资源名 → 动作

路径取自内嵌 `backend/app/admin/service/cmd/server/assets/openapi.yaml`，以下路径省略 `/admin/v1/`。
资源名遵循 `ConvertCodeByPath`，已确认挂码动作对应的 POST / PUT / DELETE 接口存在。

| 页面 | 资源名 | 挂码动作 | API 路径 |
|---|---|---|---|
| 用户 | `user` | create / edit / delete | `users` |
| 岗位 | `position` | create / edit / delete | `positions` |
| 组织 | `org-unit` | create / edit / delete | `org-units` |
| 角色 | `role` | create / edit / delete | `roles` |
| 菜单 | `menu` | create / edit / delete | `menus` |
| 接口 | `api` | create / edit / delete | `apis` |
| 权限点 | `permission` | create / edit / delete | `permissions` |
| 权限组 | `permission-group` | create / edit / delete | `permission-groups` |
| 配置 | `config` | create / edit / delete | `configs` |
| 字典类型 | `dict` | create / edit / delete | `dict/types` |
| 字典项 | `dict` | create / edit / delete | `dict/entries` |
| 语言 | `dict` | create / edit / delete | `dict/langs` |
| 任务 | `task` | create / edit / delete | `tasks` |
| 文件 | `file` | delete | `files` |
| 消息分类 | `internal-message` | create / edit / delete | `internal-message/categories` |
| 消息管理 | `internal-message` | edit / delete | `internal-message/messages` |
| 租户 | `tenant` | create / edit / delete | `tenants` |
| 套餐 | `plan` | create / edit / delete | `plans` |
| 套餐配额 | `plan-quota` | create / edit / delete | `plan-quotas` |
| 收件箱（未改） | `internal-message` | 无：删除是自定义 POST RPC | `internal-message/inbox/delete` |
| 在线会话（未改） | `online-session` | 无：强制下线为非 CRUD 动作 | `online-session/force-logout` |
| 登录审计（未改） | `login-audit-log` | 无：详情、导出保持原样 | `login-audit-logs` |
| 操作审计（未改） | `operation-audit-log` | 无：详情、导出保持原样 | `operation-audit-logs` |

消息管理的「新增」实际调用 `SendMessage`（`internal-message/send`），文件上传调用自定义上传 RPC，
本轮按非 CRUD 动作处理，不加编码。字典三页共用 `dict` 编码、消息分类与消息管理共用
`internal-message` 编码，是当前路径转换规则的既有结果。

## 已部署实例升级

1. 部署新版后端与 React 前端并重启后端。本次没有 schema/proto 变更，无需数据库迁移。
2. 用平台管理员进入接口管理，执行「接口同步」，等待接口与权限点两步均成功。
3. 进入菜单管理，执行一次「菜单同步」（MERGE），等待菜单与权限点两步均成功。
   `routeToMenu` 会传递 hideInMenu；`MenuRepo.syncMergeTree` 的 `SetMeta(m.Meta)` 和
   `SetNillableRedirect(m.Redirect)` 会更新已有菜单，权限点管理入口隐藏，目录跳转角色管理。
   无需手工隐藏，也不需要重做已有角色绑定。
4. 重新登录以刷新本地 accessCodes。后续建角色 → 勾选页面/按钮 → 分配用户。
   第二步权限点同步若失败会明确提示并记录原始错误，可重试相应同步。

权限点排查地址仍为 `/permission/codes`。

## 门禁结果

| 门禁 | 结果 |
|---|---|
| `go build ./...` | 通过（使用可写 GOCACHE） |
| `go vet ./...` | 通过（使用可写 GOCACHE） |
| `go test ./...` | 失败：9 个包受 TCP/UDP 监听限制影响，原文见下 |
| 第 1 项新增测试、PermissionService、MenuService 定向复验 | 通过 |
| `make api && make openapi && make ts && make ent` | 通过，生成目录无新增差异 |
| `npm run typecheck` | 通过 |
| `npm run build` | 通过 |
| `npm run lint` | 命令退出 1；876 条（628 error、248 warning），符合上限 |
| lint 与实施前逐文件/规则/消息对比 | 无新增、无减少，完全一致 |
| `bash scripts/check-no-upstream-deps.sh` | 通过 |
| `git diff --check` | 通过 |

Go 使用 `GOCACHE=/private/tmp/goadmin-role-menu-cache`，绕过默认缓存目录的访问限制。
前端构建有重复自动导入、console.timeEnd 标签等警告，退出码为 0。
全部命令原始日志保留在 `/private/tmp/role-menu-rbac-verification/`。

### 失败原文：全量 Go 测试

以下各失败包的输出逐字摘自 `go-test.log`，包括 miniredis/httptest 启动失败后引发的 panic。

```text
--- FAIL: TestUserTokenCache_BasicOperations (0.00s)
    user_token_cache_test.go:22: 
        	Error Trace:	/Users/zzl/CodeSpace/gitea/goadmin/backend/app/admin/service/internal/data/user_token_cache_test.go:22
        	Error:      	Received unexpected error:
        	            	listen tcp 127.0.0.1:0: bind: operation not permitted
        	Test:       	TestUserTokenCache_BasicOperations
panic: runtime error: invalid memory address or nil pointer dereference [recovered, repanicked]
[signal SIGSEGV: segmentation violation code=0x2 addr=0x0 pc=0x10449f1c4]

goroutine 1821 [running]:
testing.tRunner.func1.2({0x10725fa60, 0x1078727b0})
	/Users/zzl/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.4.darwin-arm64/src/testing/testing.go:1974 +0x1a0
testing.tRunner.func1()
	/Users/zzl/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.4.darwin-arm64/src/testing/testing.go:1977 +0x318
panic({0x10725fa60?, 0x1078727b0?})
	/Users/zzl/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.4.darwin-arm64/src/runtime/panic.go:860 +0x12c
github.com/alicebob/miniredis/v2/server.(*Server).Addr(0x374117034fa0?)
	/Users/zzl/go/pkg/mod/github.com/alicebob/miniredis/v2@v2.39.0/server/server.go:123 +0x24
github.com/alicebob/miniredis/v2.(*Miniredis).Addr(0x107717b00?)
	/Users/zzl/go/pkg/mod/github.com/alicebob/miniredis/v2@v2.39.0/miniredis.go:325 +0xa0
go-wind-admin/app/admin/service/internal/data.TestUserTokenCache_BasicOperations(0x374117286008)
	/Users/zzl/CodeSpace/gitea/goadmin/backend/app/admin/service/internal/data/user_token_cache_test.go:27 +0x80
testing.tRunner(0x374117286008, 0x1076fb050)
	/Users/zzl/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.4.darwin-arm64/src/testing/testing.go:2036 +0xc4
created by testing.(*T).Run in goroutine 1
	/Users/zzl/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.4.darwin-arm64/src/testing/testing.go:2101 +0x3a8
FAIL	go-wind-admin/app/admin/service/internal/data	7.113s
```

```text
--- FAIL: TestGetTenantBranding (0.02s)
    miniredis.go:148: could not start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
--- FAIL: TestAuthSvcSqlite_LoginGrantDispatch (0.02s)
    miniredis.go:148: could not start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
--- FAIL: TestAuthSvcSqlite_LoginCaptchaGate (0.01s)
    miniredis.go:148: could not start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
--- FAIL: TestAuthSvcSqlite_LoginTenantResolution (0.01s)
    miniredis.go:148: could not start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
--- FAIL: TestAuthSvcSqlite_LoginCredentialFailures (0.01s)
    miniredis.go:148: could not start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
--- FAIL: TestAuthSvcSqlite_LoginFullPlatformChain (0.01s)
    miniredis.go:148: could not start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
--- FAIL: TestAuthSvcSqlite_LoginRateLimiterLockout (0.01s)
    miniredis.go:148: could not start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
--- FAIL: TestAuthSvcSqlite_TenantLoginChain (0.01s)
    miniredis.go:148: could not start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
--- FAIL: TestAuthSvcSqlite_AuthorizationDeniedBranches (0.01s)
    miniredis.go:148: could not start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
--- FAIL: TestAuthSvcSqlite_DisabledTenantViaRefresh (0.01s)
    miniredis.go:148: could not start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
--- FAIL: TestAuthSvcSqlite_LogoutAndRevocation (0.01s)
    miniredis.go:148: could not start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
--- FAIL: TestAuthSvcSqlite_RefreshTokenFlow (0.01s)
    miniredis.go:148: could not start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
--- FAIL: TestAuthSvcSqlite_ValidateTokenBranches (0.01s)
    miniredis.go:148: could not start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
--- FAIL: TestAuthSvcSqlite_WhoAmI (0.01s)
    miniredis.go:148: could not start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
--- FAIL: TestAuthSvcSqlite_GenerateAndVerifyCaptcha (0.01s)
    miniredis.go:148: could not start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
--- FAIL: TestAuthSvcSqlite_CookieHelperGuards (0.01s)
    miniredis.go:148: could not start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
--- FAIL: TestAuthSvcSqlite_DisabledRoleDroppedFromAuthority (0.01s)
    miniredis.go:148: could not start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
--- FAIL: TestOnlineSessionServiceSqlite_ListSortsAndPaginates (0.00s)
    miniredis.go:148: could not start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
--- FAIL: TestOnlineSessionServiceSqlite_ListKeywordFilter (0.00s)
    miniredis.go:148: could not start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
--- FAIL: TestOnlineSessionServiceSqlite_ListMySessionsCurrentFlag (0.00s)
    miniredis.go:148: could not start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
--- FAIL: TestOnlineSessionServiceSqlite_RevokeMySession (0.00s)
    miniredis.go:148: could not start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
--- FAIL: TestOnlineSessionServiceSqlite_ForceLogoutSession (0.00s)
    miniredis.go:148: could not start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
--- FAIL: TestRoleServiceSqlite_GrantChangeRevokesHolderNotBystander (0.01s)
    miniredis.go:148: could not start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
--- FAIL: TestRoleServiceSqlite_DeleteRevokesHolderTokens (0.01s)
    miniredis.go:148: could not start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
--- FAIL: TestRoleServiceSqlite_PagePayloadComparesGrantValues (0.01s)
    miniredis.go:148: could not start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
--- FAIL: TestRoleServiceSqlite_HolderListErrorAbortsGrantChange (0.01s)
    miniredis.go:148: could not start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
--- FAIL: TestUserProfileServiceSqlite_GetUser_EnrichesRoleCodes (0.01s)
    miniredis.go:148: could not start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
--- FAIL: TestUserProfileServiceSqlite_GetUser_NoRolesAndErrors (0.01s)
    miniredis.go:148: could not start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
--- FAIL: TestUserProfileServiceSqlite_UpdateUser_OperatorInjection (0.01s)
    miniredis.go:148: could not start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
--- FAIL: TestUserProfileServiceSqlite_DeleteAvatar_ClearsAvatar (0.01s)
    miniredis.go:148: could not start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
--- FAIL: TestUserProfileServiceSqlite_UploadAvatar_UrlPathAndValidation (0.01s)
    miniredis.go:148: could not start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
--- FAIL: TestUserProfileServiceSqlite_ChangePassword_HappyPath (0.01s)
    miniredis.go:148: could not start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
--- FAIL: TestUserProfileServiceSqlite_ChangePassword_RejectsWeakNewPassword (0.01s)
    miniredis.go:148: could not start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
--- FAIL: TestUserProfileServiceSqlite_ChangePassword_BadCredentials (0.01s)
    miniredis.go:148: could not start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
--- FAIL: TestUserProfileServiceSqlite_ChangePassword_HistoryReplayRejected (0.01s)
    miniredis.go:148: could not start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
--- FAIL: TestUserServiceSqlite_ListEnrichment (0.01s)
    miniredis.go:148: could not start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
--- FAIL: TestUserServiceSqlite_GetEnrichment (0.01s)
    miniredis.go:148: could not start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
--- FAIL: TestUserServiceSqlite_CountAndUserExists (0.01s)
    miniredis.go:148: could not start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
--- FAIL: TestUserServiceSqlite_CreateGuards (0.01s)
    miniredis.go:148: could not start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
--- FAIL: TestUserServiceSqlite_CreatePlatform (0.01s)
    miniredis.go:148: could not start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
--- FAIL: TestUserServiceSqlite_CreateRollsBackUserWhenCredentialRejected (0.01s)
    miniredis.go:148: could not start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
--- FAIL: TestUserServiceSqlite_CreatePanicRollsBackUser (0.01s)
    miniredis.go:148: could not start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
--- FAIL: TestUserServiceSqlite_CreateTenant (0.01s)
    miniredis.go:148: could not start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
--- FAIL: TestUserServiceSqlite_UpdatePasswordReset (0.01s)
    miniredis.go:148: could not start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
--- FAIL: TestUserServiceSqlite_UpdateRoleChangeRevokesTokens (0.01s)
    miniredis.go:148: could not start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
--- FAIL: TestUserServiceSqlite_EditUserPassword (0.01s)
    miniredis.go:148: could not start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
--- FAIL: TestUserServiceSqlite_DeleteGuards (0.01s)
    miniredis.go:148: could not start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
--- FAIL: TestUserServiceSqlite_InitSeedsEmptyDB (0.01s)
    miniredis.go:148: could not start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
--- FAIL: TestUserServiceSqlite_InitReseedsMissingCredentials (0.01s)
    miniredis.go:148: could not start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
--- FAIL: TestUserServiceSqlite_CreateDefaultUserCredentialsOverride (0.01s)
    miniredis.go:148: could not start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
FAIL
FAIL	go-wind-admin/app/admin/service/internal/service	6.296s
```

```text
--- FAIL: TestRedisCache_SetGet (0.00s)
    redis_cache_test.go:31: failed to start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
--- FAIL: TestRedisCache_GetNotFound (0.00s)
    redis_cache_test.go:55: failed to start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
--- FAIL: TestRedisCache_SetMarshalError (0.00s)
    redis_cache_test.go:66: failed to start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
--- FAIL: TestRedisCache_GetUnmarshalError (0.00s)
    redis_cache_test.go:79: failed to start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
--- FAIL: TestRedisCache_Del (0.00s)
    redis_cache_test.go:97: failed to start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
--- FAIL: TestRedisCache_SetGetNil (0.00s)
    redis_cache_test.go:122: failed to start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
--- FAIL: TestRedisCache_BasicTypes (0.00s)
    --- FAIL: TestRedisCache_BasicTypes/int (0.00s)
        redis_cache_test.go:147: failed to start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
    --- FAIL: TestRedisCache_BasicTypes/zero (0.00s)
        redis_cache_test.go:147: failed to start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
    --- FAIL: TestRedisCache_BasicTypes/negative (0.00s)
        redis_cache_test.go:147: failed to start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
FAIL
FAIL	go-wind-admin/third_party/go-crud/cache	0.789s
```

```text
--- FAIL: TestSendMail_AuthAcceptedThenSendSucceeds (0.00s)
    smtp_test.go:522: 
        	Error Trace:	/Users/zzl/CodeSpace/gitea/goadmin/backend/third_party/go-utils/mailer/smtp_test.go:55
        	            				/Users/zzl/CodeSpace/gitea/goadmin/backend/third_party/go-utils/mailer/smtp_test.go:522
        	Error:      	Received unexpected error:
        	            	listen tcp 127.0.0.1:0: bind: operation not permitted
        	Test:       	TestSendMail_AuthAcceptedThenSendSucceeds
--- FAIL: TestSendMail_StartTLSAdvertisedButPlaintext (0.00s)
    smtp_test.go:449: 
        	Error Trace:	/Users/zzl/CodeSpace/gitea/goadmin/backend/third_party/go-utils/mailer/smtp_test.go:55
        	            				/Users/zzl/CodeSpace/gitea/goadmin/backend/third_party/go-utils/mailer/smtp_test.go:449
        	Error:      	Received unexpected error:
        	            	listen tcp 127.0.0.1:0: bind: operation not permitted
        	Test:       	TestSendMail_StartTLSAdvertisedButPlaintext
--- FAIL: TestSendMail_ConnectFailure_ClosedPort (0.00s)
    smtp_test.go:408: 
        	Error Trace:	/Users/zzl/CodeSpace/gitea/goadmin/backend/third_party/go-utils/mailer/smtp_test.go:223
        	            				/Users/zzl/CodeSpace/gitea/goadmin/backend/third_party/go-utils/mailer/smtp_test.go:408
        	Error:      	Received unexpected error:
        	            	listen tcp 127.0.0.1:0: bind: operation not permitted
        	Test:       	TestSendMail_ConnectFailure_ClosedPort
--- FAIL: TestSendMail_ConnectFailure_BadGreeting (0.00s)
    smtp_test.go:436: 
        	Error Trace:	/Users/zzl/CodeSpace/gitea/goadmin/backend/third_party/go-utils/mailer/smtp_test.go:55
        	            				/Users/zzl/CodeSpace/gitea/goadmin/backend/third_party/go-utils/mailer/smtp_test.go:436
        	Error:      	Received unexpected error:
        	            	listen tcp 127.0.0.1:0: bind: operation not permitted
        	Test:       	TestSendMail_ConnectFailure_BadGreeting
--- FAIL: TestSendMail_SessionCommandFailures (0.00s)
    --- FAIL: TestSendMail_SessionCommandFailures/auth_rejected (0.00s)
        smtp_test.go:597: 
            	Error Trace:	/Users/zzl/CodeSpace/gitea/goadmin/backend/third_party/go-utils/mailer/smtp_test.go:55
            	            				/Users/zzl/CodeSpace/gitea/goadmin/backend/third_party/go-utils/mailer/smtp_test.go:597
            	Error:      	Received unexpected error:
            	            	listen tcp 127.0.0.1:0: bind: operation not permitted
            	Test:       	TestSendMail_SessionCommandFailures/auth_rejected
    --- FAIL: TestSendMail_SessionCommandFailures/message_terminator_rejected (0.00s)
        smtp_test.go:597: 
            	Error Trace:	/Users/zzl/CodeSpace/gitea/goadmin/backend/third_party/go-utils/mailer/smtp_test.go:55
            	            				/Users/zzl/CodeSpace/gitea/goadmin/backend/third_party/go-utils/mailer/smtp_test.go:597
            	Error:      	Received unexpected error:
            	            	listen tcp 127.0.0.1:0: bind: operation not permitted
            	Test:       	TestSendMail_SessionCommandFailures/message_terminator_rejected
    --- FAIL: TestSendMail_SessionCommandFailures/rcpt_command_rejected (0.00s)
        smtp_test.go:597: 
            	Error Trace:	/Users/zzl/CodeSpace/gitea/goadmin/backend/third_party/go-utils/mailer/smtp_test.go:55
            	            				/Users/zzl/CodeSpace/gitea/goadmin/backend/third_party/go-utils/mailer/smtp_test.go:597
            	Error:      	Received unexpected error:
            	            	listen tcp 127.0.0.1:0: bind: operation not permitted
            	Test:       	TestSendMail_SessionCommandFailures/rcpt_command_rejected
    --- FAIL: TestSendMail_SessionCommandFailures/data_command_rejected (0.00s)
        smtp_test.go:597: 
            	Error Trace:	/Users/zzl/CodeSpace/gitea/goadmin/backend/third_party/go-utils/mailer/smtp_test.go:55
            	            				/Users/zzl/CodeSpace/gitea/goadmin/backend/third_party/go-utils/mailer/smtp_test.go:597
            	Error:      	Received unexpected error:
            	            	listen tcp 127.0.0.1:0: bind: operation not permitted
            	Test:       	TestSendMail_SessionCommandFailures/data_command_rejected
    --- FAIL: TestSendMail_SessionCommandFailures/mail_command_rejected (0.00s)
        smtp_test.go:597: 
            	Error Trace:	/Users/zzl/CodeSpace/gitea/goadmin/backend/third_party/go-utils/mailer/smtp_test.go:55
            	            				/Users/zzl/CodeSpace/gitea/goadmin/backend/third_party/go-utils/mailer/smtp_test.go:597
            	Error:      	Received unexpected error:
            	            	listen tcp 127.0.0.1:0: bind: operation not permitted
            	Test:       	TestSendMail_SessionCommandFailures/mail_command_rejected
--- FAIL: TestSendMail_PlaintextSessionSucceeds (0.00s)
    --- FAIL: TestSendMail_PlaintextSessionSucceeds/empty_from_and_username_fallback (0.00s)
        smtp_test.go:490: 
            	Error Trace:	/Users/zzl/CodeSpace/gitea/goadmin/backend/third_party/go-utils/mailer/smtp_test.go:55
            	            				/Users/zzl/CodeSpace/gitea/goadmin/backend/third_party/go-utils/mailer/smtp_test.go:490
            	Error:      	Received unexpected error:
            	            	listen tcp 127.0.0.1:0: bind: operation not permitted
            	Test:       	TestSendMail_PlaintextSessionSucceeds/empty_from_and_username_fallback
    --- FAIL: TestSendMail_PlaintextSessionSucceeds/explicit_from (0.00s)
        smtp_test.go:490: 
            	Error Trace:	/Users/zzl/CodeSpace/gitea/goadmin/backend/third_party/go-utils/mailer/smtp_test.go:55
            	            				/Users/zzl/CodeSpace/gitea/goadmin/backend/third_party/go-utils/mailer/smtp_test.go:490
            	Error:      	Received unexpected error:
            	            	listen tcp 127.0.0.1:0: bind: operation not permitted
            	Test:       	TestSendMail_PlaintextSessionSucceeds/explicit_from
FAIL
FAIL	go-wind-admin/third_party/go-utils/mailer	0.423s
```

```text
--- FAIL: TestLookupAndCheckHost_ResolvesToBlockedAddress (0.00s)
    ssrf_extra_test.go:161: 
        	Error Trace:	/Users/zzl/CodeSpace/gitea/goadmin/backend/third_party/go-utils/netutil/ssrf_extra_test.go:55
        	            				/Users/zzl/CodeSpace/gitea/goadmin/backend/third_party/go-utils/netutil/ssrf_extra_test.go:161
        	Error:      	Received unexpected error:
        	            	listen udp 127.0.0.1:0: bind: operation not permitted
        	Test:       	TestLookupAndCheckHost_ResolvesToBlockedAddress
--- FAIL: TestLookupAndCheckHost_PublicHostResolutions (0.00s)
    ssrf_extra_test.go:173: 
        	Error Trace:	/Users/zzl/CodeSpace/gitea/goadmin/backend/third_party/go-utils/netutil/ssrf_extra_test.go:55
        	            				/Users/zzl/CodeSpace/gitea/goadmin/backend/third_party/go-utils/netutil/ssrf_extra_test.go:173
        	Error:      	Received unexpected error:
        	            	listen udp 127.0.0.1:0: bind: operation not permitted
        	Test:       	TestLookupAndCheckHost_PublicHostResolutions
--- FAIL: TestLookupAndCheckHost_UnmappedHostErrors (0.00s)
    ssrf_extra_test.go:197: 
        	Error Trace:	/Users/zzl/CodeSpace/gitea/goadmin/backend/third_party/go-utils/netutil/ssrf_extra_test.go:55
        	            				/Users/zzl/CodeSpace/gitea/goadmin/backend/third_party/go-utils/netutil/ssrf_extra_test.go:197
        	Error:      	Received unexpected error:
        	            	listen udp 127.0.0.1:0: bind: operation not permitted
        	Test:       	TestLookupAndCheckHost_UnmappedHostErrors
FAIL
FAIL	go-wind-admin/third_party/go-utils/netutil	0.395s
```

```text
--- FAIL: TestStoragePutAndGetObject (0.00s)
panic: httptest: failed to listen on a port: listen tcp6 [::1]:0: bind: operation not permitted [recovered, repanicked]

goroutine 10 [running]:
testing.tRunner.func1.2({0x100fc8160, 0x52375e163a20})
	/Users/zzl/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.4.darwin-arm64/src/testing/testing.go:1974 +0x1a0
testing.tRunner.func1()
	/Users/zzl/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.4.darwin-arm64/src/testing/testing.go:1977 +0x318
panic({0x100fc8160?, 0x52375e163a20?})
	/Users/zzl/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.4.darwin-arm64/src/runtime/panic.go:860 +0x12c
net/http/httptest.newLocalListener()
	/Users/zzl/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.4.darwin-arm64/src/net/http/httptest/server.go:72 +0xd4
net/http/httptest.NewUnstartedServer(...)
	/Users/zzl/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.4.darwin-arm64/src/net/http/httptest/server.go:120
net/http/httptest.NewServer({0x1010aee80, 0x52375e134480})
	/Users/zzl/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.4.darwin-arm64/src/net/http/httptest/server.go:107 +0x28
go-wind-admin/third_party/kratos-bootstrap/oss/minio.TestStoragePutAndGetObject(0x52375e2998c8)
	/Users/zzl/CodeSpace/gitea/goadmin/backend/third_party/kratos-bootstrap/oss/minio/client_test.go:139 +0x94
testing.tRunner(0x52375e2998c8, 0x1010a96c0)
	/Users/zzl/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.4.darwin-arm64/src/testing/testing.go:2036 +0xc4
created by testing.(*T).Run in goroutine 1
	/Users/zzl/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.4.darwin-arm64/src/testing/testing.go:2101 +0x3a8
FAIL	go-wind-admin/third_party/kratos-bootstrap/oss/minio	0.473s
```

```text
--- FAIL: TestAdjustAddress (0.00s)
    utils_test.go:37: 无法创建监听器: listen tcp :9090: bind: operation not permitted
FAIL
FAIL	go-wind-admin/third_party/kratos-transport/transport	0.510s
```

```text
--- FAIL: TestKeepAliveService (1.00s)
    server_test.go:22: 
        	Error Trace:	/Users/zzl/CodeSpace/gitea/goadmin/backend/third_party/kratos-transport/transport/keepalive/server_test.go:22
        	Error:      	Condition never satisfied
        	Test:       	TestKeepAliveService
    server_test.go:27: 
        	Error Trace:	/Users/zzl/CodeSpace/gitea/goadmin/backend/third_party/kratos-transport/transport/keepalive/server_test.go:27
        	Error:      	Received unexpected error:
        	            	listen tcp 192.168.1.109:53771: bind: operation not permitted
        	Test:       	TestKeepAliveService
FAIL
FAIL	go-wind-admin/third_party/kratos-transport/transport/keepalive	1.492s
```

```text
--- FAIL: TestClientSubscribe (0.00s)
panic: httptest: failed to listen on a port: listen tcp6 [::1]:0: bind: operation not permitted [recovered, repanicked]

goroutine 23 [running]:
testing.tRunner.func1.2({0x10120ff60, 0x6efa175117b0})
	/Users/zzl/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.4.darwin-arm64/src/testing/testing.go:1974 +0x1a0
testing.tRunner.func1()
	/Users/zzl/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.4.darwin-arm64/src/testing/testing.go:1977 +0x318
panic({0x10120ff60?, 0x6efa175117b0?})
	/Users/zzl/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.4.darwin-arm64/src/runtime/panic.go:860 +0x12c
net/http/httptest.newLocalListener()
	/Users/zzl/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.4.darwin-arm64/src/net/http/httptest/server.go:72 +0xd4
net/http/httptest.NewUnstartedServer(...)
	/Users/zzl/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.4.darwin-arm64/src/net/http/httptest/server.go:120
net/http/httptest.NewServer({0x101368f60, 0x6efa17568300})
	/Users/zzl/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.4.darwin-arm64/src/net/http/httptest/server.go:107 +0x28
go-wind-admin/third_party/kratos-transport/transport/sse.newServer()
	/Users/zzl/CodeSpace/gitea/goadmin/backend/third_party/kratos-transport/transport/sse/client_test.go:54 +0xc0
go-wind-admin/third_party/kratos-transport/transport/sse.setup(0x0)
	/Users/zzl/CodeSpace/gitea/goadmin/backend/third_party/kratos-transport/transport/sse/client_test.go:34 +0x20
go-wind-admin/third_party/kratos-transport/transport/sse.TestClientSubscribe(0x6efa175758c8)
	/Users/zzl/CodeSpace/gitea/goadmin/backend/third_party/kratos-transport/transport/sse/client_test.go:105 +0x30
testing.tRunner(0x6efa175758c8, 0x101360f30)
	/Users/zzl/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.4.darwin-arm64/src/testing/testing.go:2036 +0xc4
created by testing.(*T).Run in goroutine 1
	/Users/zzl/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.4.darwin-arm64/src/testing/testing.go:2101 +0x3a8
FAIL	go-wind-admin/third_party/kratos-transport/transport/sse	0.477s
```

### 失败原文：lint 汇总

```text
✖ 876 problems (628 errors, 248 warnings)
  0 errors and 130 warnings potentially fixable with the `--fix` option.
```

完整逐条输出见 `/private/tmp/role-menu-rbac-verification/lint.log`。

## 未能核实

- 无法在此沙箱完成需要 TCP/UDP 监听的 miniredis、SMTP、DNS、HTTP/SSE 等测试；没有跳过或修改这些测试。
- 未在真实已部署数据库执行接口/菜单同步；MERGE 更新行为已按前后端代码链核实。
- 未做浏览器端角色切换、按钮显示及同步第二步失败的端到端验证。
