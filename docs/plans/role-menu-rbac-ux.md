# 方案二：管理员视角改成「角色直接勾菜单和按钮」

> 状态：待实现（2026-10-28）。底层的「角色 → 权限点 → 菜单/接口」模型不动，权限点变成系统自动维护的内部实现，管理员只在角色页勾菜单和按钮。

## 背景

- 权限点由「同步权限」从菜单和接口自动生成，已按编码增量合并（ID 稳定、角色绑定保留），名称已中文化；接口派生的 `xxx:create / xxx:edit / xxx:delete` 已挂到同资源页面 `xxx:view` 下（`backend/app/admin/service/internal/service/permission_service.go`、`data/permission_sync.go`）。
- 角色页权限树已是「分组 → 页面 → 查看/新增/编辑/删除」，勾任一操作提交时自动带上「查看」（`frontend/admin/react/src/pages/app/permission/role/constants.ts`）。
- **缺口**：前端没有任何按钮按权限码显示/隐藏（全仓只有用户详情页用了 `useAccess`，且判的是角色），所以「新增/编辑/删除」只在后端接口层生效，界面上看不出效果。

## 目标

管理员的完整操作只有：建角色 → 在角色页勾页面和按钮 → 分配给用户。勾不到的按钮在界面上不显示。

## 改动项

### 1. 平台管理员拿到全部权限码（必须最先做）

`AdminPortalService.GetMyPermissionCode`（`backend/app/admin/service/internal/service/admin_portal_service.go:98`）当前按角色绑定的权限点返回编码，平台管理员角色只绑了几个 `sys:*`。按钮一旦按编码隐藏，平台管理员会看不到所有按钮。

- 操作人是平台管理员（`operator.GetIsPlatformAdmin()`，与 `authentication_data_scope.go` 同口径）时，返回全部**启用**权限点的编码。与 Casbin 里平台管理员 `/* ANY` 的语义一致。
- 补后端测试：平台管理员拿到全部启用编码；普通角色只拿到绑定的编码。

### 2. 按钮按权限码显示

约定：按钮权限码 = `<资源>:<动作>`，动作只处理 `create / edit / delete`。资源名与「同步权限」由接口路径推出来的编码一致（规则见 `backend/third_party/go-utils/converter/api.go` 的 `ConvertCodeByPath`：去版本前缀、去路径参数、复数转单数，如 `/admin/v1/users/{id}` → `user`，`/admin/v1/org-units` → `org-unit`）。

- `components/common/TableActions`：`TableAction` 增加可选字段 `code?: string`；组件内用 `useAccess().hasAccessByCodes` 判定，无权限等同 `hidden`（在计算「更多」溢出之前过滤）。不传 `code` 的动作行为不变。
- 工具栏「新增」按钮：新增一个小组件或 hook（如 `components/common/AccessButton` 或 `useResourceAccess(resource)` 返回 `{ canCreate, canEdit, canDelete }`），各页用它包住「新增」按钮。不要每页手写 `hasAccessByCodes([...])` 字符串拼接。
- 逐页接入：所有使用 `TableActions` 的列表页（约 23 个）和带「新增」按钮的页（约 18 个），给新增/编辑/删除挂上对应编码。重置密码、同步、导出、强制下线等非 CRUD 动作这一轮**不加**编码，保持原样显示。
- **每个页面用的资源名必须核对过**：以 `backend/app/admin/service/cmd/server/assets/openapi.yaml` 里该页实际调用的接口路径为准，按上面的规则推出资源名，并确认对应的 `create/edit/delete` 接口真的存在（不存在的动作不要挂编码，否则按钮对非管理员永远隐藏）。在交付说明里列出「页面 → 资源名 → 挂了哪些动作」的对照表。

### 3. 隐藏「权限点管理」入口

- 前端路由 `frontend/admin/react/src/router/modules/permissions.tsx` 的 `permission-codes` 加 `hideInMenu: true`；页面仍可通过 URL 访问，供开发排查。
- 后端默认菜单播种 `backend/pkg/constants/default_data.go`：ID 31 `PermissionPointManagement` 加 `HideInMenu: true`；ID 30 目录的 `Redirect` 从 `/permission/codes` 改为 `/permission/roles`。前端路由若有同类 redirect 一并改。
- 已部署实例：确认「菜单同步」（MERGE）会不会把 `hideInMenu` 写进已有菜单行。会，就在交付说明里写明「升级后点一次菜单同步」；不会，就说明需要在菜单管理里手动把「权限点管理」设为隐藏，不要为此写迁移。

### 4. 菜单/接口同步后自动同步权限点

权限点管理页隐藏后，管理员不应再需要单独点「同步权限」。

- 菜单管理页「菜单同步」、API 管理页「接口同步」成功后，前端接着调用 `PermissionService.SyncPermissions`，两步都成功才提示成功；第二步失败要提示并 `console.error` 原始错误（仓库铁律：不吞错）。
- 不改后端服务之间的依赖关系。

### 5. 文档

- `docs/frontend_authority.md`：在 React 部分补一节「按钮权限」，写清编码约定、`TableAction.code` 和新增按钮组件的用法、平台管理员全量编码。删掉或标注该文件里已不适用于 React 的 vben 写法不在本轮范围，不要动。

## 不做

- 不改表结构、proto、生成链（`make api/openapi/ts/ent` 不需要跑出新 diff）。
- 不删权限点管理页面和相关接口。
- 不动登录、鉴权中间件、数据权限、字段权限、租户相关代码。
- 不处理非 CRUD 按钮的权限。

## 验收

- 门禁全过：`backend/` 下 `go build ./...`、`go vet ./...`、`go test ./...`；`frontend/admin/react` 下 `npm run typecheck`、`npm run build`、`npm run lint`（不得超过 `docs/plans/react-lint-baseline.tsv` 的 876 条）。
- 新增的后端测试覆盖第 1 项。
- 交付说明包含：改动文件清单、页面资源名对照表、已部署实例升级步骤、未能核实的点。
- 不提交 commit。

---

# 第二阶段：侧栏改为后端菜单模式（2026-10-29 追加）

> 状态：待实现。第一阶段（上文改动项 1~5）已实现，交付说明见 `docs/plans/role-menu-rbac-ux-delivery.md`。

## 问题

React 端默认 `accessMode: "frontend"`（`frontend/admin/react/src/core/preferences/config/default.ts`），侧栏完全由前端路由表 `src/router/modules/*.tsx` 生成，且这些路由都没有 `meta.authority`。后果：

- 角色勾了哪些菜单对侧栏没有任何影响，所有登录用户看到同一套侧栏，没权限的页面点进去才 403。
- 菜单管理里改隐藏、改名、排序对侧栏无效（用户实际反馈：取消「租户管理」的隐藏并保存，侧栏没变化）。
- 菜单表里有前端不存在的旧路径目录：`/inbox`、`/profile`（`backend/pkg/constants/default_data.go` 约 371–412 行），前端实际路由是 `/internal-message/inbox`、个人中心另有路由。

## 目标

侧栏由 `AdminPortalService.GetNavigation` 按当前用户角色勾选的菜单生成；菜单管理里的隐藏、名称、排序、图标直接反映到侧栏。

## 改动项

### 6. 平台管理员拿到全部菜单（必须最先做）

`GetNavigation`（`backend/app/admin/service/internal/service/admin_portal_service.go`）按角色绑定的权限点取菜单，平台管理员只绑了几个 `sys:*`，切到后端模式后侧栏会几乎为空。

- 操作人是平台管理员（`operator.GetIsPlatformAdmin()`，与第 1 项同口径）时返回全部启用的非按钮菜单。
- 普通角色维持现状：只返回其权限点绑定的菜单。确认父目录的补全逻辑：只勾了子页面时，父目录是否会一并返回；不会的话补上（只补祖先目录，不补兄弟页面）。
- 补后端测试：平台管理员拿到全部菜单；普通角色只勾一个子页面时拿到该页面及其祖先目录，拿不到兄弟页面。

### 7. 菜单数据和 React 页面对齐

后端模式靠 `src/router/index.tsx` 的 `pageMap`（`import.meta.glob('../pages/app/**/*.tsx')`）与 `core/router/generators/generate-routes-backend.ts` 的 `normalizeViewPath` 把菜单的 `component` 映射到页面组件。

- 写一个核对：遍历 `default_data.go` 里全部默认菜单（及它们经「菜单同步」后的形态），确认每个 `MENU` 类型的 `component` 经 `normalizeViewPath` 后都能在 `pageMap` 里找到页面。核对结果写进交付说明；对不上的，以 React 实际页面目录为准修正默认数据或映射规则，不要靠新增空页面凑数。
- `/inbox`、`/profile` 这类前端不存在的旧目录：确认它们在 React 前端的真实入口（收件箱从顶栏通知进入，个人中心从头像菜单进入），在默认菜单数据里改成与前端路由一致的路径并设为 `hideInMenu`；或者如果它们本来就不该作为侧栏菜单存在，从默认数据中删除。已部署实例的处理写进升级步骤（菜单同步能修正的就靠同步，修正不了的写明在菜单管理里手动删除或隐藏哪几条），不写数据库迁移。
- 前端路由 `meta.hideInMenu` 和菜单表的隐藏要一致：「菜单同步」（`buildSyncMenusRequest`，MERGE）会把前端路由的 `hideInMenu` 写回菜单表。确认这一点，并确认管理员在菜单管理里手动改的隐藏不会在下次「菜单同步」时被前端路由覆盖回去；会被覆盖的话，在交付说明里写清楚「隐藏属性以哪一边为准」，并在菜单管理页对应位置加一句提示文案，不要擅自改变同步的覆盖语义。

### 8. 切换默认模式

- `core/preferences/config/default.ts` 的 `accessMode` 改为 `"backend"`。
- 已登录用户的偏好被 zustand persist 存在 `localStorage`（`app-preferences`）里，旧值 `"frontend"` 会覆盖新默认值。确认 persist 的合并逻辑；如果旧值会生效，用该 store 已有的版本迁移机制把 `accessMode` 迁到 `"backend"`（没有迁移机制就加 `version` + `migrate`，只迁这一个字段）。
- 后端模式下 `GetNavigation` 失败或返回空时 React 端是空侧栏（无降级）。保持这个行为，但要确保空侧栏时有可见的错误提示，并 `console.error` 原始错误（铁律：不吞错）。不要加"静默退回前端模式"的降级。
- 前端路由里不在菜单表中的系统页（登录、403/404、个人中心、收件箱等）必须仍可访问：确认后端模式下这些静态路由仍会注册。

### 9. 文档

- `docs/frontend_authority.md` React 部分：写明默认是后端菜单模式、侧栏由角色勾选的菜单决定、菜单管理的改动如何生效、平台管理员看到全部菜单。
- `AGENTS.md` 里如有与默认模式相矛盾的描述，一并更正（`frontend/admin/react/AGENTS.md` 的「权限模式」小节目前写的是 `frontend`（默认））。

## 不做（第二阶段）

- 不改表结构、proto、生成链。
- 不删除前端路由模式的代码，只切换默认值。
- 不动登录、鉴权中间件、数据权限、字段权限、租户隔离逻辑。

## 验收（第二阶段）

- 门禁同上文「验收」，全部通过；前端 lint 不超过 876 条。
- 交付说明另存为 `docs/plans/role-menu-rbac-ux-delivery-phase2.md`，包含：改动文件清单、菜单 → 页面映射核对表（含对不上的条目及处理方式）、已部署实例升级步骤、门禁结果（失败原文）、未能核实的点。
- 不提交 commit。
