package data

import (
	"context"
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
	"go-wind-admin/third_party/go-crud/viewer"

	"go-wind-admin/pkg/constants"

	permissionV1 "go-wind-admin/api/gen/go/permission/service/v1"
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

// PurgeLegacyTenantConfig 删除旧库遗留的租户 / 套餐菜单、权限点、模板角色和权限组，以及它们的关联行。
// 幂等：没有遗留时只有查询。须在权限点同步之前调用，否则同步会从遗留菜单再派生出权限点。
func (r *PermissionRepo) PurgeLegacyTenantConfig(ctx context.Context) (result LegacyTenantCleanupResult, err error) {
	ctx = viewer.WithSystemContext(ctx)

	tx, err := r.entClient.Client().Tx(ctx)
	if err != nil {
		r.log.Errorf(ctx, "start transaction failed: %s", err.Error())
		return result, permissionV1.ErrorInternalServerError("start transaction failed")
	}
	var txCommitted bool
	defer finishEntTx(tx, &txCommitted, func(rollbackErr error) {
		r.log.Errorf(context.Background(), "transaction rollback failed: %s", rollbackErr.Error())
	})

	if result.Menus, err = purgeLegacyTenantMenus(ctx, tx); err != nil {
		r.log.Errorf(ctx, "purge legacy tenant menus failed: %s", err.Error())
		return result, permissionV1.ErrorInternalServerError("purge legacy tenant menus failed")
	}
	if result.Permissions, err = purgeLegacyTenantPermissions(ctx, tx); err != nil {
		r.log.Errorf(ctx, "purge legacy tenant permissions failed: %s", err.Error())
		return result, permissionV1.ErrorInternalServerError("purge legacy tenant permissions failed")
	}
	if result.Roles, err = purgeLegacyTenantRoles(ctx, tx); err != nil {
		r.log.Errorf(ctx, "purge legacy tenant roles failed: %s", err.Error())
		return result, permissionV1.ErrorInternalServerError("purge legacy tenant roles failed")
	}
	if result.PermissionGroups, err = purgeLegacyTenantPermissionGroups(ctx, tx); err != nil {
		r.log.Errorf(ctx, "purge legacy tenant permission groups failed: %s", err.Error())
		return result, permissionV1.ErrorInternalServerError("purge legacy tenant permission groups failed")
	}

	if err = commitEntTx(tx, &txCommitted); err != nil {
		r.log.Errorf(ctx, "transaction commit failed: %s", err.Error())
		return result, permissionV1.ErrorInternalServerError("transaction commit failed")
	}
	return result, nil
}

// purgeLegacyTenantMenus 删除「租户管理」目录及其整棵子树，以及组件在 app/tenant/ 下的页面。
func purgeLegacyTenantMenus(ctx context.Context, tx *ent.Tx) (int, error) {
	roots, err := tx.Menu.Query().
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
		children, cerr := tx.Menu.Query().Where(menu.ParentIDIn(frontier...)).IDs(ctx)
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
	if _, err = tx.PermissionMenu.Delete().Where(permissionmenu.MenuIDIn(all...)).Exec(ctx); err != nil {
		return 0, err
	}
	total := 0
	for i := len(levels) - 1; i >= 0; i-- {
		n, derr := tx.Menu.Delete().Where(menu.IDIn(levels[i]...)).Exec(ctx)
		if derr != nil {
			return total, derr
		}
		total += n
	}
	return total, nil
}

// purgeLegacyTenantPermissions 删除租户 / 套餐权限点及其角色授权、接口和菜单关联。
func purgeLegacyTenantPermissions(ctx context.Context, tx *ent.Tx) (int, error) {
	perms, err := tx.Permission.Query().
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

	if _, err = tx.RolePermission.Delete().Where(rolepermission.PermissionIDIn(ids...)).Exec(ctx); err != nil {
		return 0, err
	}
	if _, err = tx.PermissionApi.Delete().Where(permissionapi.PermissionIDIn(ids...)).Exec(ctx); err != nil {
		return 0, err
	}
	if _, err = tx.PermissionMenu.Delete().Where(permissionmenu.PermissionIDIn(ids...)).Exec(ctx); err != nil {
		return 0, err
	}
	return tx.Permission.Delete().Where(permission.IDIn(ids...)).Exec(ctx)
}

// purgeLegacyTenantRoles 删除租户管理员模板角色及其关联行。
func purgeLegacyTenantRoles(ctx context.Context, tx *ent.Tx) (int, error) {
	ids, err := tx.Role.Query().
		Where(role.CodeIn(legacyTenantRoleCodes...)).
		IDs(ctx)
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}

	if _, err = tx.RolePermission.Delete().Where(rolepermission.RoleIDIn(ids...)).Exec(ctx); err != nil {
		return 0, err
	}
	if _, err = tx.RoleMetadata.Delete().Where(rolemetadata.RoleIDIn(ids...)).Exec(ctx); err != nil {
		return 0, err
	}
	if _, err = tx.RoleOrgUnit.Delete().Where(roleorgunit.RoleIDIn(ids...)).Exec(ctx); err != nil {
		return 0, err
	}
	if _, err = tx.RoleFieldPermission.Delete().Where(rolefieldpermission.RoleIDIn(ids...)).Exec(ctx); err != nil {
		return 0, err
	}
	if _, err = tx.UserRole.Delete().Where(userrole.RoleIDIn(ids...)).Exec(ctx); err != nil {
		return 0, err
	}
	if _, err = tx.MembershipRole.Delete().Where(membershiprole.RoleIDIn(ids...)).Exec(ctx); err != nil {
		return 0, err
	}
	return tx.Role.Delete().Where(role.IDIn(ids...)).Exec(ctx)
}

// purgeLegacyTenantPermissionGroups 删除「租户管理」权限组。组下还挂着别的权限点（管理员手工挪进来的）时保留。
func purgeLegacyTenantPermissionGroups(ctx context.Context, tx *ent.Tx) (int, error) {
	ids, err := tx.PermissionGroup.Query().
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
		n, cerr := tx.Permission.Query().Where(permission.GroupIDEQ(id)).Count(ctx)
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
	if _, err = tx.PermissionGroup.Update().
		Where(permissiongroup.ParentIDIn(empty...)).
		ClearParentID().
		Save(ctx); err != nil {
		return 0, err
	}
	return tx.PermissionGroup.Delete().Where(permissiongroup.IDIn(empty...)).Exec(ctx)
}
