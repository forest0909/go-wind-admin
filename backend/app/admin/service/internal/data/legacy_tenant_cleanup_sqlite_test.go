package data

import (
	"context"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"go-wind-admin/app/admin/service/internal/data/ent"
	"go-wind-admin/app/admin/service/internal/data/ent/menu"
	"go-wind-admin/app/admin/service/internal/data/ent/permission"
	"go-wind-admin/app/admin/service/internal/data/ent/permissiongroup"
	"go-wind-admin/app/admin/service/internal/data/ent/role"
	"go-wind-admin/app/admin/service/internal/data/enttest"
)

// legacyTenantDB 模拟升级前的旧库：带租户 / 套餐菜单、权限点、模板角色和权限组，
// 以及应当保留的正常配置。
type legacyTenantDB struct {
	keepMenuID       uint32
	keepPermIDs      []uint32
	adminRoleID      uint32
	keepGroupID      uint32
	tenantMenuIDs    []uint32
	tenantPermIDs    []uint32
	templateRoleID   uint32
	legacyTenantRole uint32 // tenant_id>0 的租户角色，属于业务数据，不应被删
}

func seedLegacyTenantDB(t *testing.T, client *ent.Client, ctx context.Context) legacyTenantDB {
	t.Helper()
	var db legacyTenantDB

	// 正常菜单：/system → configs
	system := client.Menu.Create().SetPath("/system").SetName("System").SetComponent("BasicLayout").SetType(menu.TypeCatalog).SaveX(ctx)
	db.keepMenuID = client.Menu.Create().SetPath("configs").SetName("ConfigManagement").SetComponent("app/system/config/index.vue").SetType(menu.TypeMenu).SetParentID(system.ID).SaveX(ctx).ID

	// 旧版租户菜单：/tenant → members、plans，另有一个挂在 /system 下、组件在 app/tenant/ 的页面
	tenantRoot := client.Menu.Create().SetPath("/tenant").SetName("TenantManagement").SetComponent("BasicLayout").SetType(menu.TypeCatalog).SaveX(ctx)
	members := client.Menu.Create().SetPath("members").SetName("TenantMemberManagement").SetComponent("app/tenant/tenant/index.vue").SetType(menu.TypeMenu).SetParentID(tenantRoot.ID).SaveX(ctx)
	plans := client.Menu.Create().SetPath("plans").SetName("PlanManagement").SetComponent("app/tenant/plan/index.vue").SetType(menu.TypeMenu).SetParentID(tenantRoot.ID).SaveX(ctx)
	planBtn := client.Menu.Create().SetPath("plans-create").SetName("PlanCreate").SetType(menu.TypeButton).SetParentID(plans.ID).SaveX(ctx)
	stray := client.Menu.Create().SetPath("quotas").SetName("QuotaManagement").SetComponent("app/tenant/quota/index.vue").SetType(menu.TypeMenu).SetParentID(system.ID).SaveX(ctx)
	db.tenantMenuIDs = []uint32{tenantRoot.ID, members.ID, plans.ID, planBtn.ID, stray.ID}

	// 权限组：系统组「租户管理」、同步出的 tenant 模块组；保留 opm 模块组
	sysTenantGroup := client.PermissionGroup.Create().SetName("租户管理").SetModule("sys").SaveX(ctx)
	syncedTenantGroup := client.PermissionGroup.Create().SetName("menu.tenant.moduleName").SetModule("tenant").SaveX(ctx)
	db.keepGroupID = client.PermissionGroup.Create().SetName("menu.opm.moduleName").SetModule("opm").SaveX(ctx).ID

	newPerm := func(code string, groupID uint32) uint32 {
		return client.Permission.Create().SetName(code).SetCode(code).SetGroupID(groupID).SaveX(ctx).ID
	}
	for _, code := range []string{"sys:tenant_manager", "sys:manage_tenants"} {
		db.tenantPermIDs = append(db.tenantPermIDs, newPerm(code, sysTenantGroup.ID))
	}
	for _, code := range []string{
		"tenant:dir", "tenant:view", "tenant:create", "member:view", "plan:view", "plan:edit",
		"plan-quota:edit", "plan-module:delete", "my-tenant-usage:view", "tenant-branding:create",
	} {
		db.tenantPermIDs = append(db.tenantPermIDs, newPerm(code, syncedTenantGroup.ID))
	}
	// 保留：前缀相近但不是租户资源的编码也不能误删
	for _, code := range []string{"sys:access_backend", "user:view", "planner:view", "tenants-report:view"} {
		db.keepPermIDs = append(db.keepPermIDs, newPerm(code, db.keepGroupID))
	}

	// 关联：权限 ↔ 菜单 / 接口
	api := client.Api.Create().SetPath("/admin/v1/tenants").SetMethod("GET").SaveX(ctx)
	client.PermissionMenu.Create().SetPermissionID(db.tenantPermIDs[3]).SetMenuID(members.ID).SaveX(ctx)
	client.PermissionMenu.Create().SetPermissionID(db.keepPermIDs[1]).SetMenuID(plans.ID).SaveX(ctx) // 保留的权限点挂在被删菜单上
	client.PermissionMenu.Create().SetPermissionID(db.keepPermIDs[1]).SetMenuID(db.keepMenuID).SaveX(ctx)
	client.PermissionApi.Create().SetPermissionID(db.tenantPermIDs[3]).SetAPIID(api.ID).SaveX(ctx)

	// 平台管理员持有租户权限点与正常权限点
	db.adminRoleID = client.Role.Create().SetName("平台管理员").SetCode("platform:admin").SetType(role.TypeSystem).SaveX(ctx).ID
	for _, pid := range []uint32{db.tenantPermIDs[0], db.tenantPermIDs[3], db.keepPermIDs[0], db.keepPermIDs[1]} {
		client.RolePermission.Create().SetRoleID(db.adminRoleID).SetPermissionID(pid).SaveX(ctx)
	}

	// 模板角色及其元数据、授权
	db.templateRoleID = client.Role.Create().SetName("租户管理员模板").SetCode("template:tenant:manager").SetType(role.TypeTemplate).SetIsProtected(true).SaveX(ctx).ID
	client.RoleMetadata.Create().SetRoleID(db.templateRoleID).SetIsTemplate(true).SetTemplateFor("tenant:manager").SaveX(ctx)
	client.RolePermission.Create().SetRoleID(db.templateRoleID).SetPermissionID(db.keepPermIDs[0]).SaveX(ctx)

	// 某租户由模板复制出的管理员角色：tenant_id>0 的业务数据，清理不碰
	db.legacyTenantRole = client.Role.Create().SetTenantID(7).SetName("租户管理员").SetCode("tenant:manager").SetType(role.TypeTenant).SaveX(ctx).ID

	return db
}

func sortedIDs(ids []uint32) []uint32 {
	out := append([]uint32(nil), ids...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// TestPurgeLegacyTenantConfigSqlite 旧库升级后启动：租户 / 套餐菜单（含子树）、权限点及其关联、
// 模板角色、租户权限组被删掉，正常配置和 tenant_id>0 的业务数据保留；再跑一次什么都不删。
func TestPurgeLegacyTenantConfigSqlite(t *testing.T) {
	entClient := enttest.NewEntClientForTest(t)
	client := entClient.Client()
	ctx := enttest.NewSystemContext(context.Background())
	repo := NewPermissionRepoForTest(entClient)

	db := seedLegacyTenantDB(t, client, ctx)

	res, err := repo.PurgeLegacyTenantConfig(ctx)
	require.NoError(t, err)
	require.Equal(t, len(db.tenantMenuIDs), res.Menus, "租户菜单整棵子树与 app/tenant/ 组件页面应全部删除")
	require.Equal(t, len(db.tenantPermIDs), res.Permissions, "租户 / 套餐权限点应全部删除")
	require.Equal(t, 1, res.Roles, "只删模板角色")
	require.Equal(t, 2, res.PermissionGroups, "系统组「租户管理」和同步出的 tenant 组都应删除")

	// 菜单：只剩 /system 和 configs
	menuIDs := client.Menu.Query().IDsX(ctx)
	require.Len(t, menuIDs, 2)
	require.Contains(t, menuIDs, db.keepMenuID)
	require.Zero(t, client.Menu.Query().Where(menu.ComponentHasPrefix("app/tenant/")).CountX(ctx))

	// 权限点：只剩保留的编码
	permIDs := client.Permission.Query().IDsX(ctx)
	require.Equal(t, sortedIDs(db.keepPermIDs), sortedIDs(permIDs))
	require.Zero(t, client.Permission.Query().Where(permission.CodeIn("sys:tenant_manager", "plan:view")).CountX(ctx))

	// 关联：指向被删权限点或被删菜单的行都没了，正常关联保留
	require.Equal(t, 1, client.PermissionMenu.Query().CountX(ctx), "只剩 user:view ↔ configs")
	require.Zero(t, client.PermissionApi.Query().CountX(ctx))
	adminGrants := client.RolePermission.Query().CountX(ctx)
	require.Equal(t, 2, adminGrants, "平台管理员只剩两条正常授权，模板角色授权随角色删除")

	// 角色：模板角色没了，平台管理员和租户 7 的业务角色保留
	require.False(t, client.Role.Query().Where(role.IDEQ(db.templateRoleID)).ExistX(ctx))
	require.True(t, client.Role.Query().Where(role.IDEQ(db.adminRoleID)).ExistX(ctx))
	require.True(t, client.Role.Query().Where(role.IDEQ(db.legacyTenantRole)).ExistX(ctx), "tenant_id>0 的业务数据不自动删除")
	require.Zero(t, client.RoleMetadata.Query().CountX(ctx))

	// 权限组：只剩 opm
	groupIDs := client.PermissionGroup.Query().IDsX(ctx)
	require.Equal(t, []uint32{db.keepGroupID}, groupIDs)

	// 幂等：再跑一次不报错、不删任何东西
	res, err = repo.PurgeLegacyTenantConfig(ctx)
	require.NoError(t, err)
	require.True(t, res.Empty(), "第二次运行不应再清理任何行: %+v", res)
	require.Len(t, client.Menu.Query().IDsX(ctx), 2)
	require.Equal(t, sortedIDs(db.keepPermIDs), sortedIDs(client.Permission.Query().IDsX(ctx)))
}

// TestPurgeLegacyTenantConfigSqlite_KeepsNonEmptyGroup 管理员手工往「租户管理」组里挪了别的权限点时，组保留。
func TestPurgeLegacyTenantConfigSqlite_KeepsNonEmptyGroup(t *testing.T) {
	entClient := enttest.NewEntClientForTest(t)
	client := entClient.Client()
	ctx := enttest.NewSystemContext(context.Background())
	repo := NewPermissionRepoForTest(entClient)

	g := client.PermissionGroup.Create().SetName("租户管理").SetModule("sys").SaveX(ctx)
	client.Permission.Create().SetName("tenant").SetCode("sys:tenant_manager").SetGroupID(g.ID).SaveX(ctx)
	client.Permission.Create().SetName("custom").SetCode("report:view").SetGroupID(g.ID).SaveX(ctx)

	res, err := repo.PurgeLegacyTenantConfig(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, res.Permissions)
	require.Zero(t, res.PermissionGroups)
	require.True(t, client.PermissionGroup.Query().Where(permissiongroup.IDEQ(g.ID)).ExistX(ctx))
}

// TestIsLegacyTenantPermissionCode 编码按资源段精确匹配，不按子串。
func TestIsLegacyTenantPermissionCode(t *testing.T) {
	for _, code := range []string{"sys:tenant_manager", "sys:manage_tenants", "tenant:view", "member:view", "plan:edit", "plan-quota:view", "plan-module:create", "my-tenant-usage:view", "tenant-branding:create"} {
		require.True(t, IsLegacyTenantPermissionCode(code), code)
	}
	for _, code := range []string{"sys:access_backend", "sys:platform_admin", "user:view", "planner:view", "tenants-report:view", "tenant", ""} {
		require.False(t, IsLegacyTenantPermissionCode(code), code)
	}
}
