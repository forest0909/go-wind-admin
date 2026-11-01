package data

import (
	"context"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"go-wind-admin/app/admin/service/internal/data/ent/permission"
	"go-wind-admin/app/admin/service/internal/data/ent/rolepermission"
	"go-wind-admin/app/admin/service/internal/data/enttest"

	"go-wind-admin/pkg/constants"
)

// TestPermissionRepoSqlite_SyncTenantManagerGrants 模板和各租户的管理员角色都收敛到
// 租户可用的启用权限点：补缺、删多余（含种子里的 sys:tenant_manager），平台专属和停用的不给，
// 其他角色不动；重复执行结果不变。
func TestPermissionRepoSqlite_SyncTenantManagerGrants(t *testing.T) {
	entClient := enttest.NewEntClientForTest(t)
	repo := newPermissionRepoSqlite(t, entClient)
	client := entClient.Client()
	ctx := enttest.NewSystemContext(context.Background())

	newPerm := func(code string, status permission.Status) uint32 {
		return client.Permission.Create().SetName(code).SetCode(code).SetStatus(status).SaveX(ctx).ID
	}
	access := newPerm(constants.SystemAccessBackendPermissionCode, permission.StatusOn)
	manager := newPerm(constants.SystemTenantManagerPermissionCode, permission.StatusOn)
	userView := newPerm("user:view", permission.StatusOn)
	userCreate := newPerm("user:create", permission.StatusOn)
	apiView := newPerm("api:view", permission.StatusOn)
	_ = newPerm("task:view", permission.StatusOff)

	newRole := func(code string, tenantID uint32) uint32 {
		return client.Role.Create().SetName(code).SetCode(code).SetTenantID(tenantID).SaveX(ctx).ID
	}
	template := newRole(constants.TenantAdminTemplateRoleCode, 0)
	tenantA := newRole(constants.TenantAdminRoleCode, 1)
	tenantB := newRole(constants.TenantAdminRoleCode, 2)
	viewer := newRole("e2e_viewer", 1)

	grant := func(roleID, tenantID, permID uint32) {
		client.RolePermission.Create().SetRoleID(roleID).SetTenantID(tenantID).SetPermissionID(permID).ExecX(ctx)
	}
	grant(template, 0, access)
	grant(template, 0, manager)
	grant(tenantA, 1, access)
	grant(tenantA, 1, manager)
	grant(viewer, 1, apiView)

	grantsOf := func(roleID uint32) []uint32 {
		rows := client.RolePermission.Query().Where(rolepermission.RoleIDEQ(roleID)).AllX(ctx)
		ids := make([]uint32, 0, len(rows))
		for _, r := range rows {
			ids = append(ids, *r.PermissionID)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		return ids
	}
	want := []uint32{access, userView, userCreate}

	for range 2 {
		require.NoError(t, repo.SyncTenantManagerGrants(context.Background(), 0))
		require.Equal(t, want, grantsOf(template))
		require.Equal(t, want, grantsOf(tenantA))
		require.Equal(t, want, grantsOf(tenantB))
		require.Equal(t, []uint32{apiView}, grantsOf(viewer))
	}

	rows := client.RolePermission.Query().Where(rolepermission.RoleIDEQ(tenantB)).AllX(ctx)
	for _, r := range rows {
		require.Equal(t, uint32(2), *r.TenantID, "授权行的租户列跟随角色")
	}
}
