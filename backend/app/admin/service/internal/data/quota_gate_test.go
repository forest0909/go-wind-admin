package data

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"

	"go-wind-admin/app/admin/service/internal/data/ent/api"
	"go-wind-admin/app/admin/service/internal/data/ent/planmodule"
	"go-wind-admin/app/admin/service/internal/data/ent/planquota"
	"go-wind-admin/app/admin/service/internal/data/ent/tenant"
	"go-wind-admin/app/admin/service/internal/data/enttest"
)

func ptrOf[T any](v T) *T { return &v }

// TestCheckTenantAccessApiCallQuotaNotEnforced 闸门不再按 API_CALL 拒绝。
// 套餐挂了 API_CALL 上限，模块白名单放行后请求仍然通过。
func TestCheckTenantAccessApiCallQuotaNotEnforced(t *testing.T) {
	entClient := enttest.NewEntClientForTest(t)
	ctx := enttest.NewSystemContext(context.Background())
	client := entClient.Client()

	planRow, err := client.Plan.Create().Save(ctx)
	require.NoError(t, err)
	require.NoError(t, client.PlanQuota.Create().
		SetPlanID(planRow.ID).
		SetQuotaType(planquota.QuotaTypeApiCall).
		SetQuotaValue(0).
		Exec(ctx))
	require.NoError(t, client.PlanModule.Create().
		SetPlanID(planRow.ID).
		SetModule(planmodule.ModuleSystem).
		Exec(ctx))

	tenantRow, err := client.Tenant.Create().
		SetName("配额闸门接线租户").
		SetCode("QUOTA_GATE_T").
		SetStatus(tenant.StatusOn).
		SetPlanID(planRow.ID).
		Save(ctx)
	require.NoError(t, err)

	require.NoError(t, client.Api.Create().
		SetNillablePath(ptrOf("/admin/v1/quota-gate/probe")).
		SetNillableMethod(ptrOf("GET")).
		SetBusinessModule(api.BusinessModuleSystem).
		Exec(ctx))

	checker := &TenantAccessCheckerImpl{
		entClient: client,
		log:       bLogger.NewHelper(bLogger.NopLogger()),
	}

	require.NoError(t, checker.CheckTenantAccess(ctx, tenantRow.ID, "/admin/v1/quota-gate/probe", "GET"))
}
