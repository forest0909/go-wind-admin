# tenant_id：遗留的底层列（固定为 0）

本仓库是单租户管理端。租户、套餐、配额、用量、租户闸门、租户管理员授权、模板角色、
登录租户编号都已删除（2026-11）。**新模块不需要做任何租户处理。**

## 还留着什么

下面这些是底层设施，值恒为 0，不要在业务代码里读写或分支：

| 位置 | 说明 |
|---|---|
| 业务表的 `tenant_id` 列 | `mixin.TenantID[uint32]{}` 注入，默认 0。新表照抄现有 schema 即可，不用写任何租户逻辑 |
| ent 租户隐私规则 | go-crud `rule.TenantPrivacy` 与仓内 `schema/tenant_mutation_guard.go`。viewer 租户为 0 时就是平台上下文，不加租户谓词 |
| viewer 的租户字段、JWT `tid` 声明、Casbin 域 | 都是 0。角色策略按角色所属租户装进 Casbin 域，域固定为 `"0"` |
| 登录 | 凭证和用户行都按 `tenant_id = 0` 查，旧库里 `tenant_id > 0` 的账号登录不了 |

`third_party/go-crud` 里的租户设施没有改动。

## 旧库升级

启动时 `PermissionService.init()` 在权限点同步之前调用 `PermissionRepo.PurgeLegacyTenantConfig`
（`backend/app/admin/service/internal/data/legacy_tenant_cleanup.go`），幂等地删掉这些配置行：

- `/tenant` 菜单整棵子树，以及组件在 `app/tenant/` 下的页面，连同它们的权限点-菜单关联；
- 租户和套餐的权限点：`sys:tenant_manager`、`sys:manage_tenants`，以及资源段为
  `tenant`、`member`、`plan`、`plan-quota`、`plan-module`、`my-tenant-usage`、`tenant-branding`
  的权限点，连同角色授权、接口关联、菜单关联；
- 租户管理员模板角色 `template:tenant:manager`；
- 「租户管理」权限组（组下还有别的权限点时保留）。

已删除接口在 `sys_apis` 里的行由启动期接口同步自动删除。`sys_tenants`、`sys_plans`、
`sys_plan_modules`、`sys_plan_quotas` 四张表和 `sys_apis.business_module`、`sys_menus.module`
两列不再被程序读写，自动迁移不删表、不删列，留在库里无害。

### tenant_id > 0 的业务数据（不自动删除）

旧库里各租户的用户、角色、组织、日志等行不会自动删除。它们登录不了，但平台管理员在列表页
仍能看到。确认不再需要后，按下面的方式手工清理（先备份；PostgreSQL 写法，MySQL 同理）：

```sql
-- 先看有哪些
SELECT tenant_id, count(*) FROM sys_users WHERE tenant_id > 0 GROUP BY tenant_id;

-- 每张带 tenant_id 的业务表各删一次，例如：
DELETE FROM sys_user_credentials WHERE tenant_id > 0;
DELETE FROM sys_user_roles WHERE tenant_id > 0;
DELETE FROM sys_users WHERE tenant_id > 0;
DELETE FROM sys_role_permissions WHERE tenant_id > 0;
DELETE FROM sys_role_metadata WHERE tenant_id > 0;
DELETE FROM sys_roles WHERE tenant_id > 0;
-- 其余带 tenant_id 的表：sys_org_units、sys_positions、sys_memberships*、sys_user_org_units、
-- sys_user_positions、sys_role_org_units、sys_role_field_permissions、sys_dict_*、files、
-- internal_message*、sys_login_audit_logs、sys_operation_audit_logs、sys_tasks

-- 不再使用的表（可选）
DROP TABLE IF EXISTS sys_plan_modules, sys_plan_quotas, sys_tenants, sys_plans;
```
