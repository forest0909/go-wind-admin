package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/app/admin/service/internal/data/ent/menu"
	"go-wind-admin/app/admin/service/internal/data/enttest"
	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"
)

// TestPermissionServiceInit_PurgesLegacyTenantBeforeSync 旧库启动：升级步骤先清遗留租户菜单，
// init 再做权限点同步，同步不会从遗留菜单重新派生出 tenant / member / plan 权限点。
// 改动前（没有清理步骤）同步会按 /tenant 菜单生成 tenant:dir、member:view、plan:view。
func TestPermissionServiceInit_PurgesLegacyTenantBeforeSync(t *testing.T) {
	entClient := enttest.NewEntClientForTest(t)
	svc, client := newPermissionServiceWithEntClient(entClient), entClient.Client()
	ctx := enttest.NewSystemContext(context.Background())

	system := client.Menu.Create().SetPath("/system").SetName("System").SetComponent("BasicLayout").SetType(menu.TypeCatalog).SaveX(ctx)
	client.Menu.Create().SetPath("configs").SetName("ConfigManagement").SetComponent("app/system/config/index.vue").SetType(menu.TypeMenu).SetParentID(system.ID).SaveX(ctx)
	tenantRoot := client.Menu.Create().SetPath("/tenant").SetName("TenantManagement").SetComponent("BasicLayout").SetType(menu.TypeCatalog).SaveX(ctx)
	client.Menu.Create().SetPath("members").SetName("TenantMemberManagement").SetComponent("app/tenant/tenant/index.vue").SetType(menu.TypeMenu).SetParentID(tenantRoot.ID).SaveX(ctx)
	client.Menu.Create().SetPath("plans").SetName("PlanManagement").SetComponent("app/tenant/plan/index.vue").SetType(menu.TypeMenu).SetParentID(tenantRoot.ID).SaveX(ctx)
	client.Api.Create().SetPath("/admin/v1/configs").SetMethod("GET").SetModule("ConfigService").SaveX(ctx)

	// 生产顺序：NewEntClient 里跑升级步骤，之后才构造 PermissionService
	require.NoError(t, data.RunSchemaUpgrades(ctx, entClient.Driver(), bLogger.NewHelper(bLogger.NopLogger())))
	svc.init()

	codes := client.Permission.Query().Select("code").StringsX(ctx)
	require.NotEmpty(t, codes, "同步应生成正常菜单和接口的权限点")
	for _, code := range codes {
		require.False(t, data.IsLegacyTenantPermissionCode(code), "不应再生成租户权限点 %q", code)
	}
	require.Zero(t, client.Menu.Query().Where(menu.ComponentHasPrefix("app/tenant/")).CountX(ctx))

	// 再启动一次：仍然干净
	require.NoError(t, data.RunSchemaUpgrades(ctx, entClient.Driver(), bLogger.NewHelper(bLogger.NopLogger())))
	svc.init()
	codes = client.Permission.Query().Select("code").StringsX(ctx)
	for _, code := range codes {
		require.False(t, data.IsLegacyTenantPermissionCode(code), "第二次启动不应生成租户权限点 %q", code)
	}
}
