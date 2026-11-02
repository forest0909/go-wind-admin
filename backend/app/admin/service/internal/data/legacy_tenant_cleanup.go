package data

import (
	"context"
	"fmt"
	"strings"

	"go-wind-admin/app/admin/service/internal/data/ent"
	"go-wind-admin/app/admin/service/internal/data/ent/membershiprole"
	"go-wind-admin/app/admin/service/internal/data/ent/menu"
	"go-wind-admin/app/admin/service/internal/data/ent/permission"
	"go-wind-admin/app/admin/service/internal/data/ent/permissionapi"
	"go-wind-admin/app/admin/service/internal/data/ent/permissiongroup"
	"go-wind-admin/app/admin/service/internal/data/ent/permissionmenu"
	"go-wind-admin/app/admin/service/internal/data/ent/role"
	"go-wind-admin/app/admin/service/internal/data/ent/rolefieldpermission"
	"go-wind-admin/app/admin/service/internal/data/ent/rolemetadata"
	"go-wind-admin/app/admin/service/internal/data/ent/roleorgunit"
	"go-wind-admin/app/admin/service/internal/data/ent/rolepermission"
	"go-wind-admin/app/admin/service/internal/data/ent/userrole"

	"go-wind-admin/pkg/constants"
)

// 多租户（租户 / 套餐 / 配额）已移除。旧库里这几类配置行不会被现有同步删除：
// 菜单表是 count==0 守卫加 MERGE，只增不删；权限点同步按编码合并，同样只增不删。
// 不清掉的话，租户和套餐页面会留在侧栏路由和角色页的权限树里。
// 接口表不需要处理：启动期接口同步按内嵌 OpenAPI 文档删掉了已不存在的接口。
//
// 只清这些配置行。tenant_id>0 的业务数据（用户、角色、日志等）不自动删除，清理方法见 docs。

// legacyTenantMenuRootPath 旧版「租户管理」目录（下挂租户成员、套餐页面）。
const legacyTenantMenuRootPath = "/tenant"

// legacyTenantMenuComponentPrefix 租户和套餐页面的组件路径前缀（按组件兜底，覆盖改过路由的菜单）。
const legacyTenantMenuComponentPrefix = "app/tenant/"

// legacyTenantPermissionCodes 旧版系统内置的租户权限点。
var legacyTenantPermissionCodes = map[string]bool{
	"sys:tenant_manager": true,
	"sys:manage_tenants": true,
}

// legacyTenantPermissionResources 由租户和套餐菜单、接口派生出的权限点资源（编码第一段）。
var legacyTenantPermissionResources = map[string]bool{
	"tenant":          true,
	"member":          true,
	"plan":            true,
	"plan-quota":      true,
	"plan-module":     true,
	"my-tenant-usage": true,
	"tenant-branding": true,
}

// legacyTenantRoleCodes 旧版租户管理员模板角色。各租户复制出的 tenant:manager 属于 tenant_id>0 的业务数据，不在此删除。
var legacyTenantRoleCodes = []string{"template:tenant:manager"}

// legacyTenantPermissionGroupModule 旧版同步按「租户管理」目录生成的权限组模块标识。
const legacyTenantPermissionGroupModule = "tenant"

// legacyTenantSysGroupName 旧版种子里的系统权限组「租户管理」。
const legacyTenantSysGroupName = "租户管理"

// IsLegacyTenantPermissionCode 判断权限点是否属于已移除的租户 / 套餐功能。
func IsLegacyTenantPermissionCode(code string) bool {
	if legacyTenantPermissionCodes[code] {
		return true
	}
	resource, _, ok := strings.Cut(code, ":")
	return ok && legacyTenantPermissionResources[resource]
}

// LegacyTenantCleanupResult 本次清掉的行数，全为 0 表示库里已经没有遗留。
type LegacyTenantCleanupResult struct {
	Menus            int
	Permissions      int
	Roles            int
	PermissionGroups int
}

// Empty 没有任何遗留被清理。
func (r LegacyTenantCleanupResult) Empty() bool {
	return r.Menus == 0 && r.Permissions == 0 && r.Roles == 0 && r.PermissionGroups == 0
}

// purgeLegacyTenantConfig 删除旧库遗留的租户 / 套餐菜单、权限点、模板角色和权限组，以及它们的关联行。
// 由升级步骤 v1 调用（schema_upgrade.go），client 绑定在该步的事务上。
// 幂等：没有遗留时只有查询。须在权限点同步之前执行，否则同步会从遗留菜单再派生出权限点。
func purgeLegacyTenantConfig(ctx context.Context, client *ent.Client) (result LegacyTenantCleanupResult, err error) {
	if result.Menus, err = purgeLegacyTenantMenus(ctx, client); err != nil {
		return result, fmt.Errorf("purge legacy tenant menus: %w", err)
	}
	if result.Permissions, err = purgeLegacyTenantPermissions(ctx, client); err != nil {
		return result, fmt.Errorf("purge legacy tenant permissions: %w", err)
	}
	if result.Roles, err = purgeLegacyTenantRoles(ctx, client); err != nil {
		return result, fmt.Errorf("purge legacy tenant roles: %w", err)
	}
	if result.PermissionGroups, err = purgeLegacyTenantPermissionGroups(ctx, client); err != nil {
		return result, fmt.Errorf("purge legacy tenant permission groups: %w", err)
	}
	return result, nil
}

// purgeLegacyTenantMenus 删除「租户管理」目录及其整棵子树，以及组件在 app/tenant/ 下的页面。
func purgeLegacyTenantMenus(ctx context.Context, client *ent.Client) (int, error) {
	roots, err := client.Menu.Query().
		Where(menu.Or(
			menu.And(menu.PathEQ(legacyTenantMenuRootPath), menu.ParentIDIsNil()),
			menu.ComponentHasPrefix(legacyTenantMenuComponentPrefix),
		)).
		IDs(ctx)
	if err != nil {
		return 0, err
	}
	if len(roots) == 0 {
		return 0, nil
	}

	// 逐层收集子树，删除时自叶向根，避免父子外键约束
	levels := [][]uint32{roots}
	seen := make(map[uint32]bool, len(roots))
	for _, id := range roots {
		seen[id] = true
	}
	for frontier := roots; len(frontier) > 0; {
		children, cerr := client.Menu.Query().Where(menu.ParentIDIn(frontier...)).IDs(ctx)
		if cerr != nil {
			return 0, cerr
		}
		var next []uint32
		for _, id := range children {
			if !seen[id] {
				seen[id] = true
				next = append(next, id)
			}
		}
		if len(next) > 0 {
			levels = append(levels, next)
		}
		frontier = next
	}

	all := make([]uint32, 0, len(seen))
	for id := range seen {
		all = append(all, id)
	}
	if _, err = client.PermissionMenu.Delete().Where(permissionmenu.MenuIDIn(all...)).Exec(ctx); err != nil {
		return 0, err
	}
	total := 0
	for i := len(levels) - 1; i >= 0; i-- {
		n, derr := client.Menu.Delete().Where(menu.IDIn(levels[i]...)).Exec(ctx)
		if derr != nil {
			return total, derr
		}
		total += n
	}
	return total, nil
}

// purgeLegacyTenantPermissions 删除租户 / 套餐权限点及其角色授权、接口和菜单关联。
func purgeLegacyTenantPermissions(ctx context.Context, client *ent.Client) (int, error) {
	perms, err := client.Permission.Query().
		Select(permission.FieldID, permission.FieldCode).
		All(ctx)
	if err != nil {
		return 0, err
	}
	var ids []uint32
	for _, p := range perms {
		if p.Code != nil && IsLegacyTenantPermissionCode(*p.Code) {
			ids = append(ids, p.ID)
		}
	}
	if len(ids) == 0 {
		return 0, nil
	}

	if _, err = client.RolePermission.Delete().Where(rolepermission.PermissionIDIn(ids...)).Exec(ctx); err != nil {
		return 0, err
	}
	if _, err = client.PermissionApi.Delete().Where(permissionapi.PermissionIDIn(ids...)).Exec(ctx); err != nil {
		return 0, err
	}
	if _, err = client.PermissionMenu.Delete().Where(permissionmenu.PermissionIDIn(ids...)).Exec(ctx); err != nil {
		return 0, err
	}
	return client.Permission.Delete().Where(permission.IDIn(ids...)).Exec(ctx)
}

// purgeLegacyTenantRoles 删除租户管理员模板角色及其关联行。
func purgeLegacyTenantRoles(ctx context.Context, client *ent.Client) (int, error) {
	ids, err := client.Role.Query().
		Where(role.CodeIn(legacyTenantRoleCodes...)).
		IDs(ctx)
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}

	if _, err = client.RolePermission.Delete().Where(rolepermission.RoleIDIn(ids...)).Exec(ctx); err != nil {
		return 0, err
	}
	if _, err = client.RoleMetadata.Delete().Where(rolemetadata.RoleIDIn(ids...)).Exec(ctx); err != nil {
		return 0, err
	}
	if _, err = client.RoleOrgUnit.Delete().Where(roleorgunit.RoleIDIn(ids...)).Exec(ctx); err != nil {
		return 0, err
	}
	if _, err = client.RoleFieldPermission.Delete().Where(rolefieldpermission.RoleIDIn(ids...)).Exec(ctx); err != nil {
		return 0, err
	}
	if _, err = client.UserRole.Delete().Where(userrole.RoleIDIn(ids...)).Exec(ctx); err != nil {
		return 0, err
	}
	if _, err = client.MembershipRole.Delete().Where(membershiprole.RoleIDIn(ids...)).Exec(ctx); err != nil {
		return 0, err
	}
	return client.Role.Delete().Where(role.IDIn(ids...)).Exec(ctx)
}

// purgeLegacyTenantPermissionGroups 删除「租户管理」权限组。组下还挂着别的权限点（管理员手工挪进来的）时保留。
func purgeLegacyTenantPermissionGroups(ctx context.Context, client *ent.Client) (int, error) {
	ids, err := client.PermissionGroup.Query().
		Where(permissiongroup.Or(
			permissiongroup.ModuleEQ(legacyTenantPermissionGroupModule),
			permissiongroup.And(
				permissiongroup.NameEQ(legacyTenantSysGroupName),
				permissiongroup.ModuleEQ(constants.SystemPermissionModule),
			),
		)).
		IDs(ctx)
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}

	var empty []uint32
	for _, id := range ids {
		n, cerr := client.Permission.Query().Where(permission.GroupIDEQ(id)).Count(ctx)
		if cerr != nil {
			return 0, cerr
		}
		if n == 0 {
			empty = append(empty, id)
		}
	}
	if len(empty) == 0 {
		return 0, nil
	}
	if _, err = client.PermissionGroup.Update().
		Where(permissiongroup.ParentIDIn(empty...)).
		ClearParentID().
		Save(ctx); err != nil {
		return 0, err
	}
	return client.PermissionGroup.Delete().Where(permissiongroup.IDIn(empty...)).Exec(ctx)
}
