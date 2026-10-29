# 角色菜单 RBAC 第二阶段交付说明

实现方案改动项 6～9，沿用第一阶段与工作区已有改动；本次未执行 commit。
没有改表结构、proto、生成配置、登录流程、鉴权中间件、数据/字段权限或租户隔离逻辑；前端路由模式代码仍保留。
全部门禁已运行；全量 Go 测试受沙箱网络监听限制失败，因此不能声称全部门禁通过。

## 改动文件

仅列第二阶段修改或新增的文件，不把第一阶段及更早的改动算作本次实现。

- `backend/app/admin/service/internal/service/admin_portal_service.go`
- `backend/app/admin/service/internal/service/admin_portal_service_test.go`
- `backend/pkg/constants/default_data.go`
- `frontend/admin/react/AGENTS.md`
- `frontend/admin/react/src/core/preferences/config/default.ts`
- `frontend/admin/react/src/core/preferences/store/index.ts`
- `frontend/admin/react/src/core/router/factory.ts`
- `frontend/admin/react/src/core/router/generators/generate-routes-backend.ts`
- `frontend/admin/react/src/core/router/types/index.ts`
- `frontend/admin/react/src/core/router/utils/menu.ts`
- `frontend/admin/react/src/layouts/MainLayout/hooks/useMenuData.ts`
- `frontend/admin/react/src/locales/zh-CN/_core/common.json`
- `frontend/admin/react/src/locales/en-US/_core/common.json`
- `frontend/admin/react/src/locales/zh-CN/_modules/menu.json`
- `frontend/admin/react/src/locales/en-US/_modules/menu.json`
- `frontend/admin/react/src/pages/app/permission/menu/components/MenuDrawer.tsx`
- `frontend/admin/react/src/router/config/static.tsx`
- `frontend/admin/react/src/router/index.tsx`
- `frontend/admin/react/tests/backend-menu.test.ts`
- `frontend/admin/react/tests/menu-sync.integration.test.ts`
- `frontend/admin/react/vitest.config.ts`
- `docs/frontend_authority.md`
- `docs/plans/role-menu-rbac-ux-delivery-phase2.md`

## 行为与实现

- `GetNavigation` 先按 `GetIsPlatformAdmin()` 分支：平台管理员获取全部启用非按钮菜单，普通用户仅获取启用角色权限点关联的菜单，并补齐启用的祖先目录，不扩展兄弟页面。祖先缺失/停用、非目录或成环时不把子页提升成根菜单。既有套餐过滤逻辑保留。
- 后端模式侧栏以返回菜单为准，不再按遗留 `meta.authority` 二次过滤；仍按 `meta.hideInMenu` 隐藏，按 `meta.order` 排序，使用 `meta.title` 和 `meta.icon`。目录的旧 redirect 若指向未返回的兄弟页，改指向已授权子树中的页面。
- 默认偏好切为 backend；persist 版本 2 → 3 只迁移 `preferences.app.accessMode`，其他偏好保留，已有更早主题迁移继续保留。未登录时不请求导航。
- `GetNavigation` 请求失败或返回空数组时记录原始 Error，顶部显示持续可见的错误 Alert，菜单为空；不回退静态业务菜单。空菜单根路径进入个人中心，仍受 AuthGuard 保护。
- 后端模式始终保留认证/错误静态路由，并独立注册受 AuthGuard 保护的 `/opm/profile`、`/internal-message/inbox`。这两个页面不依赖角色是否勾了菜单。未额外放开其他隐藏业务页（如用户详情、权限点管理）。

## 菜单 → 页面映射核对

核对默认数据全部 24 个 `MENU`，以及前端业务路由经 `buildSyncMenusRequest` 形成的 24 个页面菜单。
`CATALOG` 对应 `BasicLayout` → `AuthenticatedLayout`，不参与 pageMap 页面匹配。
表中 React 文件路径相对 `frontend/admin/react/src/pages/app/`；去掉 `.tsx` 即归一化键。
pageMap 同时注册目录键及 `/index` 别名；`.vue`、`app/` 与下划线兼容规则保留，无需新增占位页面。

| ID / 默认菜单 | URL（修正后） | 默认 component | 同步后的 component | React 页面 |
|---|---|---|---|---|
| 2 Analytics | `/dashboard/analytics` | `dashboard/analytics/index.vue` | `app/dashboard/analytics/index.vue` | `dashboard/analytics/index.tsx` |
| 4 ProfilePage | `/opm/profile` | `app/opm/user/profile/index.vue` | `app/opm/user/profile/index.vue` | `opm/user/profile/index.tsx` |
| 6 InboxPage | `/internal-message/inbox` | `app/internal_message/inbox/index.vue` | `app/internal-message/inbox/index.vue` | `internal-message/inbox/index.tsx` |
| 11 TenantMemberManagement | `/tenant/members` | `app/tenant/tenant/index.vue` | `app/tenant/tenant/index.vue` | `tenant/tenant/index.tsx` |
| 12 PlanManagement | `/tenant/plans` | `app/tenant/plan/index.vue` | `app/tenant/plan/index.vue` | `tenant/plan/index.tsx` |
| 21 OrgUnitManagement | `/opm/org-units` | `app/opm/org_unit/index.vue` | `app/opm/org-unit/index.vue` | `opm/org-unit/index.tsx` |
| 22 PositionManagement | `/opm/positions` | `app/opm/position/index.vue` | `app/opm/position/index.vue` | `opm/position/index.tsx` |
| 23 UserManagement | `/opm/users` | `app/opm/user/list/index.vue` | `app/opm/user/list/index.vue` | `opm/user/list/index.tsx` |
| 24 UserDetail | `/opm/users/detail/:id` | `app/opm/user/detail/index.vue` | `app/opm/user/detail/index.vue` | `opm/user/detail/index.tsx` |
| 31 PermissionPointManagement | `/permission/codes` | `app/permission/permission/index.vue` | `app/permission/permission/index.vue` | `permission/permission/index.tsx` |
| 32 RoleManagement | `/permission/roles` | `app/permission/role/index.vue` | `app/permission/role/index.vue` | `permission/role/index.tsx` |
| 33 MenuManagement | `/permission/menus` | `app/permission/menu/index.vue` | `app/permission/menu/index.vue` | `permission/menu/index.tsx` |
| 34 APIManagement | `/permission/apis` | `app/permission/api/index.vue` | `app/permission/api/index.vue` | `permission/api/index.tsx` |
| 41 InternalMessageList | `/internal-message/messages` | `app/internal_message/message/index.vue` | `app/internal-message/message/index.vue` | `internal-message/message/index.tsx` |
| 42 InternalMessageCategoryManagement | `/internal-message/categories` | `app/internal_message/category/index.vue` | `app/internal-message/category/index.vue` | `internal-message/category/index.tsx` |
| 51 LoginAuditLog | `/log/login-audit-logs` | `app/log/login_audit_log/index.vue` | `app/log/login-audit-log/index.vue` | `log/login-audit-log/index.tsx` |
| 53 OperationAuditLog | `/log/operation-audit-logs` | `app/log/operation_audit_log/index.vue` | `app/log/operation-audit-log/index.vue` | `log/operation-audit-log/index.tsx` |
| 61 DictManagement | `/system/dict` | `app/system/dict/index.vue` | `app/system/dict/index.vue` | `system/dict/index.tsx` |
| 62 FileManagement | `/system/files` | `app/system/file/index.vue` | `app/system/file/index.vue` | `system/file/index.tsx` |
| 63 TaskManagement | `/system/tasks` | `app/system/task/index.vue` | `app/system/task/index.vue` | `system/task/index.tsx` |
| 65 LanguageManagement | `/system/languages` | `app/system/language/index.vue` | `app/system/language/index.vue` | `system/language/index.tsx` |
| 66 ConfigManagement | `/system/configs` | `app/system/config/index.vue` | `app/system/config/index.vue` | `system/config/index.tsx` |
| 69 OnlineSessionManagement | `/system/online-sessions` | `app/system/online_session/index.vue` | `app/system/online-session/index.vue` | `system/online-session/index.tsx` |
| 103 Workspace | `/dashboard/workspace` | `dashboard/workspace/index.vue` | `app/dashboard/workspace/index.vue` | `dashboard/workspace/index.tsx` |

所有 24 个 component 均可匹配，无缺失页面。修正的是菜单 URL/层级：

| 原条目 | 处理 |
|---|---|
| ID 3 `Profile` 目录 `/profile` | 从默认数据删除 |
| ID 4 `ProfilePage` `/profile` | 移到 ID 20 组织人员目录下，path=`profile`，URL=`/opm/profile`，保持隐藏 |
| ID 5 `Inbox` 目录 `/inbox` | 从默认数据删除 |
| ID 6 `InboxPage` `/inbox` | 移到 ID 40 内部消息目录下，path=`inbox`，URL=`/internal-message/inbox`，保持隐藏 |
| ID 2 `/analytics`、ID 103 `/workspace` | 改为 dashboard 下相对子路径 `analytics`、`workspace`，与前端同步的完整路径一致，避免 MERGE 新增同页副本 |

其余默认菜单的隐藏与前端路由一致：租户目录及两个页面、权限点页、用户详情隐藏；个人中心和收件箱隐藏。

## 同步的覆盖语义与生效时机

`routeToMenu` 将路由 `meta.hideInMenu` 放进请求；`MenuRepo.syncMergeTree` 对匹配已有完整路径的行使用
`SetMeta(m.Meta)`，因此整份 meta 被覆盖。**每次菜单同步以代码路由的隐藏属性为准**，手工修改的隐藏、
标题、排序、图标可能被覆盖。没有改变同步语义；隐藏选项旁新增中英文提示。
日常在菜单管理保存后刷新页面或重新登录即重新拉取导航，不需要为生效再点同步。
MERGE 保留现有 ID/状态/角色绑定，但不删除数据库多余行，也不会把旧路径自动改成新路径。

## 已部署实例升级步骤

1. 部署新版后端和 React 静态资源并重启后端。无 schema/proto 变更，无数据库迁移或新接口登记要求。
   旧浏览器 `app-preferences` 自动迁为版本 3 的 backend 模式，无需清空全部偏好。
2. 平台管理员进入菜单管理（`/permission/menus`）。在点菜单同步之前检查旧条目：
   - 把旧 `ProfilePage`（全新播种 ID 4）移到组织人员 `/opm` 下，path 改为 `profile`，保持隐藏；把旧 `InboxPage`（播种 ID 6）移到内部消息 `/internal-message` 下，path 改为 `inbox`，保持隐藏。
   - 子页面移走后，手动删除旧 `/profile`、`/inbox` 目录（播种 ID 3、5）；也可保留但设为隐藏且停用。若旧库已无这些条目则跳过。实际 ID 可能因历史同步不同，应按名称、组件、完整路径定位。
   - 旧 dashboard 子页若仍为绝对 `/analytics`、`/workspace`，将 path 改为相对 `analytics`、`workspace`，父目录保留 `/dashboard`。已存在 `/dashboard/analytics`、`/dashboard/workspace` 的实例无需修改；如果两套均已存在，先确认角色授权并保留正确页面行，再手工删除/停用旧副本。
   这样能让后续 MERGE 按完整路径命中已有行，保留授权 ID；直接同步不能自动清理或搬迁旧行。
3. 执行一次「菜单同步」（MERGE），等菜单与权限点两步成功。隐藏、标题、图标、排序从前端路由重新写入；
   权限点页保持隐藏。需要长期改隐藏值时，应同时维护路由定义，否则下次同步会覆盖手工值。
   本阶段没有新增接口，接口同步不是必需步骤；若同时发布其他接口变更，仍按仓库规则先 make openapi、重启，再接口同步。
4. 在角色页核对页面勾选、给普通用户分配角色，用户重新登录或刷新以获取新导航。平台管理员可见全部启用非按钮菜单，隐藏项不会进入侧栏。
5. 检查顶栏通知可进 `/internal-message/inbox`、头像菜单可进 `/opm/profile`，以及普通角色只有指定子页及祖先、没有兄弟页。菜单接口故障/空列表应出现错误提示，不出现静态全量菜单。

## 门禁结果

使用 `GOCACHE=/private/tmp/goadmin-role-menu-cache`（默认缓存目录在沙箱不可写）。
原始日志位于 `/private/tmp/role-menu-rbac-phase2/`。

| 门禁 | 结果 |
|---|---|
| `go build ./...` | 通过 |
| `go vet ./...` | 通过 |
| `go test ./...` | 失败，9 个包受沙箱 TCP/UDP 监听限制，摘录原文见下 |
| 新增 GetNavigation + 第一阶段权限码 + 查询序列化定向测试 | 通过 |
| `make api && make openapi && make ts && make ent` | 通过；生成 Go/API、内嵌 OpenAPI、React TS 客户端、Ent 无 diff |
| `npm run typecheck` | 通过 |
| `npm run build` | 通过，保留既有构建警告 |
| `npm run lint` | 退出 1；876 条（628 errors / 248 warnings），满足 ≤876 |
| `npx vitest run --config vitest.config.ts` | 通过，2 个文件共 7 项：默认/实际同步映射、隐藏属性、路由、偏好迁移 |
| `bash scripts/check-no-upstream-deps.sh` | 通过 |
| `git diff --check` | 通过 |

首次新增测试的枚举名错误已修正；一次生成未使用可写 GOCACHE 导致 Ent 加载失败，已设置后重跑完整生成链通过。
首次并行运行生成与 Go build 曾碰到生成目录短暂不存在，已改为生成结束后完整重跑后端门禁；最终 build/vet 通过。
初次 lint 新增 7 个警告已修正，最终回到基线，没有通过改 ESLint 配置或豁免规则降低计数。

### 失败原文：lint 汇总

```text
✖ 876 problems (628 errors, 248 warnings)
  0 errors and 130 warnings potentially fixable with the `--fix` option.
```

### 失败原文：Go 全量测试

以下逐字摘录失败测试输出（省去失败前的业务日志和通过包列表），完整输出见 `go-test.log`。

```text
--- FAIL: TestUserTokenCache_BasicOperations (0.00s)
    user_token_cache_test.go:22: 
        	Error Trace:	/Users/zzl/CodeSpace/gitea/goadmin/backend/app/admin/service/internal/data/user_token_cache_test.go:22
        	Error:      	Received unexpected error:
        	            	listen tcp 127.0.0.1:0: bind: operation not permitted
        	Test:       	TestUserTokenCache_BasicOperations
panic: runtime error: invalid memory address or nil pointer dereference [recovered, repanicked]
[signal SIGSEGV: segmentation violation code=0x2 addr=0x0 pc=0x104c4ed74]

goroutine 1823 [running]:
testing.tRunner.func1.2({0x107a0fa60, 0x1080227b0})
	/Users/zzl/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.4.darwin-arm64/src/testing/testing.go:1974 +0x1a0
testing.tRunner.func1()
	/Users/zzl/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.4.darwin-arm64/src/testing/testing.go:1977 +0x318
panic({0x107a0fa60?, 0x1080227b0?})
	/Users/zzl/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.4.darwin-arm64/src/runtime/panic.go:860 +0x12c
github.com/alicebob/miniredis/v2/server.(*Server).Addr(0x5842decc1590?)
	/Users/zzl/go/pkg/mod/github.com/alicebob/miniredis/v2@v2.39.0/server/server.go:123 +0x24
github.com/alicebob/miniredis/v2.(*Miniredis).Addr(0x107ec7b00?)
	/Users/zzl/go/pkg/mod/github.com/alicebob/miniredis/v2@v2.39.0/miniredis.go:325 +0xa0
go-wind-admin/app/admin/service/internal/data.TestUserTokenCache_BasicOperations(0x5842def45688)
	/Users/zzl/CodeSpace/gitea/goadmin/backend/app/admin/service/internal/data/user_token_cache_test.go:27 +0x80
testing.tRunner(0x5842def45688, 0x107eab050)
	/Users/zzl/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.4.darwin-arm64/src/testing/testing.go:2036 +0xc4
created by testing.(*T).Run in goroutine 1
	/Users/zzl/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.4.darwin-arm64/src/testing/testing.go:2101 +0x3a8
FAIL	go-wind-admin/app/admin/service/internal/data	3.957s
```

```text
--- FAIL: TestGetTenantBranding (0.01s)
    miniredis.go:148: could not start miniredis: listen tcp 127.0.0.1:0: bind: operation not permitted
--- FAIL: TestAuthSvcSqlite_LoginGrantDispatch (0.01s)
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
FAIL	go-wind-admin/app/admin/service/internal/service	3.556s
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
FAIL	go-wind-admin/third_party/go-crud/cache	0.308s
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
--- FAIL: TestSendMail_ConnectFailure_BadGreeting (0.00s)
    smtp_test.go:436: 
        	Error Trace:	/Users/zzl/CodeSpace/gitea/goadmin/backend/third_party/go-utils/mailer/smtp_test.go:55
        	            				/Users/zzl/CodeSpace/gitea/goadmin/backend/third_party/go-utils/mailer/smtp_test.go:436
        	Error:      	Received unexpected error:
        	            	listen tcp 127.0.0.1:0: bind: operation not permitted
        	Test:       	TestSendMail_ConnectFailure_BadGreeting
--- FAIL: TestSendMail_ConnectFailure_ClosedPort (0.00s)
    smtp_test.go:408: 
        	Error Trace:	/Users/zzl/CodeSpace/gitea/goadmin/backend/third_party/go-utils/mailer/smtp_test.go:223
        	            				/Users/zzl/CodeSpace/gitea/goadmin/backend/third_party/go-utils/mailer/smtp_test.go:408
        	Error:      	Received unexpected error:
        	            	listen tcp 127.0.0.1:0: bind: operation not permitted
        	Test:       	TestSendMail_ConnectFailure_ClosedPort
--- FAIL: TestSendMail_SessionCommandFailures (0.00s)
    --- FAIL: TestSendMail_SessionCommandFailures/auth_rejected (0.00s)
        smtp_test.go:597: 
            	Error Trace:	/Users/zzl/CodeSpace/gitea/goadmin/backend/third_party/go-utils/mailer/smtp_test.go:55
            	            				/Users/zzl/CodeSpace/gitea/goadmin/backend/third_party/go-utils/mailer/smtp_test.go:597
            	Error:      	Received unexpected error:
            	            	listen tcp 127.0.0.1:0: bind: operation not permitted
            	Test:       	TestSendMail_SessionCommandFailures/auth_rejected
    --- FAIL: TestSendMail_SessionCommandFailures/data_command_rejected (0.00s)
        smtp_test.go:597: 
            	Error Trace:	/Users/zzl/CodeSpace/gitea/goadmin/backend/third_party/go-utils/mailer/smtp_test.go:55
            	            				/Users/zzl/CodeSpace/gitea/goadmin/backend/third_party/go-utils/mailer/smtp_test.go:597
            	Error:      	Received unexpected error:
            	            	listen tcp 127.0.0.1:0: bind: operation not permitted
            	Test:       	TestSendMail_SessionCommandFailures/data_command_rejected
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
    --- FAIL: TestSendMail_SessionCommandFailures/mail_command_rejected (0.00s)
        smtp_test.go:597: 
            	Error Trace:	/Users/zzl/CodeSpace/gitea/goadmin/backend/third_party/go-utils/mailer/smtp_test.go:55
            	            				/Users/zzl/CodeSpace/gitea/goadmin/backend/third_party/go-utils/mailer/smtp_test.go:597
            	Error:      	Received unexpected error:
            	            	listen tcp 127.0.0.1:0: bind: operation not permitted
            	Test:       	TestSendMail_SessionCommandFailures/mail_command_rejected
--- FAIL: TestSendMail_PlaintextSessionSucceeds (0.00s)
    --- FAIL: TestSendMail_PlaintextSessionSucceeds/explicit_from (0.00s)
        smtp_test.go:490: 
            	Error Trace:	/Users/zzl/CodeSpace/gitea/goadmin/backend/third_party/go-utils/mailer/smtp_test.go:55
            	            				/Users/zzl/CodeSpace/gitea/goadmin/backend/third_party/go-utils/mailer/smtp_test.go:490
            	Error:      	Received unexpected error:
            	            	listen tcp 127.0.0.1:0: bind: operation not permitted
            	Test:       	TestSendMail_PlaintextSessionSucceeds/explicit_from
    --- FAIL: TestSendMail_PlaintextSessionSucceeds/empty_from_and_username_fallback (0.00s)
        smtp_test.go:490: 
            	Error Trace:	/Users/zzl/CodeSpace/gitea/goadmin/backend/third_party/go-utils/mailer/smtp_test.go:55
            	            				/Users/zzl/CodeSpace/gitea/goadmin/backend/third_party/go-utils/mailer/smtp_test.go:490
            	Error:      	Received unexpected error:
            	            	listen tcp 127.0.0.1:0: bind: operation not permitted
            	Test:       	TestSendMail_PlaintextSessionSucceeds/empty_from_and_username_fallback
FAIL
FAIL	go-wind-admin/third_party/go-utils/mailer	0.166s
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
FAIL	go-wind-admin/third_party/go-utils/netutil	0.172s
```

```text
--- FAIL: TestStoragePutAndGetObject (0.00s)
panic: httptest: failed to listen on a port: listen tcp6 [::1]:0: bind: operation not permitted [recovered, repanicked]

goroutine 13 [running]:
testing.tRunner.func1.2({0x1033a0160, 0x75a24d290040})
	/Users/zzl/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.4.darwin-arm64/src/testing/testing.go:1974 +0x1a0
testing.tRunner.func1()
	/Users/zzl/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.4.darwin-arm64/src/testing/testing.go:1977 +0x318
panic({0x1033a0160?, 0x75a24d290040?})
	/Users/zzl/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.4.darwin-arm64/src/runtime/panic.go:860 +0x12c
net/http/httptest.newLocalListener()
	/Users/zzl/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.4.darwin-arm64/src/net/http/httptest/server.go:72 +0xd4
net/http/httptest.NewUnstartedServer(...)
	/Users/zzl/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.4.darwin-arm64/src/net/http/httptest/server.go:120
net/http/httptest.NewServer({0x103486e80, 0x75a24d292018})
	/Users/zzl/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.4.darwin-arm64/src/net/http/httptest/server.go:107 +0x28
go-wind-admin/third_party/kratos-bootstrap/oss/minio.TestStoragePutAndGetObject(0x75a24d10db08)
	/Users/zzl/CodeSpace/gitea/goadmin/backend/third_party/kratos-bootstrap/oss/minio/client_test.go:139 +0x94
testing.tRunner(0x75a24d10db08, 0x1034816c0)
	/Users/zzl/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.4.darwin-arm64/src/testing/testing.go:2036 +0xc4
created by testing.(*T).Run in goroutine 1
	/Users/zzl/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.4.darwin-arm64/src/testing/testing.go:2101 +0x3a8
FAIL	go-wind-admin/third_party/kratos-bootstrap/oss/minio	0.226s
```

```text
--- FAIL: TestAdjustAddress (0.00s)
    utils_test.go:37: 无法创建监听器: listen tcp :9090: bind: operation not permitted
FAIL
FAIL	go-wind-admin/third_party/kratos-transport/transport	0.292s
```

```text
--- FAIL: TestKeepAliveService (0.00s)
    server_test.go:27: 
        	Error Trace:	/Users/zzl/CodeSpace/gitea/goadmin/backend/third_party/kratos-transport/transport/keepalive/server_test.go:27
        	Error:      	Received unexpected error:
        	            	listen tcp 192.168.1.109:32384: bind: operation not permitted
        	Test:       	TestKeepAliveService
FAIL
FAIL	go-wind-admin/third_party/kratos-transport/transport/keepalive	0.299s
```

```text
--- FAIL: TestClientSubscribe (0.00s)
panic: httptest: failed to listen on a port: listen tcp6 [::1]:0: bind: operation not permitted [recovered, repanicked]

goroutine 35 [running]:
testing.tRunner.func1.2({0x101677f60, 0x43d370fc5750})
	/Users/zzl/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.4.darwin-arm64/src/testing/testing.go:1974 +0x1a0
testing.tRunner.func1()
	/Users/zzl/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.4.darwin-arm64/src/testing/testing.go:1977 +0x318
panic({0x101677f60?, 0x43d370fc5750?})
	/Users/zzl/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.4.darwin-arm64/src/runtime/panic.go:860 +0x12c
net/http/httptest.newLocalListener()
	/Users/zzl/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.4.darwin-arm64/src/net/http/httptest/server.go:72 +0xd4
net/http/httptest.NewUnstartedServer(...)
	/Users/zzl/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.4.darwin-arm64/src/net/http/httptest/server.go:120
net/http/httptest.NewServer({0x1017d0f60, 0x43d370fd0300})
	/Users/zzl/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.4.darwin-arm64/src/net/http/httptest/server.go:107 +0x28
go-wind-admin/third_party/kratos-transport/transport/sse.newServer()
	/Users/zzl/CodeSpace/gitea/goadmin/backend/third_party/kratos-transport/transport/sse/client_test.go:54 +0xc0
go-wind-admin/third_party/kratos-transport/transport/sse.setup(0x0)
	/Users/zzl/CodeSpace/gitea/goadmin/backend/third_party/kratos-transport/transport/sse/client_test.go:34 +0x20
go-wind-admin/third_party/kratos-transport/transport/sse.TestClientSubscribe(0x43d370fdd8c8)
	/Users/zzl/CodeSpace/gitea/goadmin/backend/third_party/kratos-transport/transport/sse/client_test.go:105 +0x30
testing.tRunner(0x43d370fdd8c8, 0x1017c8f30)
	/Users/zzl/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.4.darwin-arm64/src/testing/testing.go:2036 +0xc4
created by testing.(*T).Run in goroutine 1
	/Users/zzl/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.4.darwin-arm64/src/testing/testing.go:2101 +0x3a8
FAIL	go-wind-admin/third_party/kratos-transport/transport/sse	0.239s
```

## 未能核实

- 沙箱禁止监听本地 TCP/UDP，无法完成 miniredis、SMTP、DNS、HTTP/SSE 等全量测试；没有跳过或修改这些测试。
- 没有连接真实已部署数据库执行移动旧菜单、MERGE 同步及授权保留验证；升级步骤按完整路径匹配与 SetMeta 的代码行为编写，不能自动识别各实例的历史菜单 ID。
- 没有做真实浏览器登录、切换角色、菜单管理保存后刷新、导航失败 Alert 的端到端验收；路由注册、空菜单不降级与偏好迁移由自动化测试核对。
