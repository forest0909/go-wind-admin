package service

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go-wind-admin/third_party/go-utils/trans"

	authenticationV1 "go-wind-admin/api/gen/go/authentication/service/v1"
	identityV1 "go-wind-admin/api/gen/go/identity/service/v1"
)

// TestGetTenantBranding 白标查询：命中返回展示字段、未命中 found=false
// （不区分"不存在"与"未设置"，不给枚举租户编号的探测者命中信号）、空编号直接 false。
func TestGetTenantBranding(t *testing.T) {
	e := newAuthenticationServiceForTest(t)

	// seedTenant 不带 LogoUrl——直接落一条带 Logo 的
	_, err := e.svc.tenantRepo.Create(e.ctx, &identityV1.Tenant{
		Name:    trans.Ptr("白标租户甲"),
		Code:    trans.Ptr("BRAND_A"),
		LogoUrl: trans.Ptr("https://cdn.example.com/a.png"),
		Status:  identityV1.Tenant_ON.Enum(),
	})
	require.NoError(t, err)
	_, err = e.svc.tenantRepo.Create(e.ctx, &identityV1.Tenant{
		Name:   trans.Ptr("无Logo租户乙"),
		Code:   trans.Ptr("BRAND_B"),
		Status: identityV1.Tenant_ON.Enum(),
	})
	require.NoError(t, err)

	// 命中带 Logo 的
	resp, err := e.svc.GetTenantBranding(e.ctx, &authenticationV1.GetTenantBrandingRequest{Code: "BRAND_A"})
	require.NoError(t, err)
	require.True(t, resp.GetFound())
	require.Equal(t, "白标租户甲", resp.GetName())
	require.Equal(t, "https://cdn.example.com/a.png", resp.GetLogoUrl())

	// 命中但未设置 Logo：LogoUrl 空串（前端保持默认 Logo）
	resp, err = e.svc.GetTenantBranding(e.ctx, &authenticationV1.GetTenantBrandingRequest{Code: "BRAND_B"})
	require.NoError(t, err)
	require.True(t, resp.GetFound())
	require.Equal(t, "无Logo租户乙", resp.GetName())
	require.Empty(t, resp.GetLogoUrl())

	// 未命中：found=false 而非错误（防枚举）
	resp, err = e.svc.GetTenantBranding(e.ctx, &authenticationV1.GetTenantBrandingRequest{Code: "NO_SUCH"})
	require.NoError(t, err)
	require.False(t, resp.GetFound())
	require.Empty(t, resp.GetName())

	// 空编号：直接 false，不查库
	resp, err = e.svc.GetTenantBranding(e.ctx, &authenticationV1.GetTenantBrandingRequest{Code: "  "})
	require.NoError(t, err)
	require.False(t, resp.GetFound())
}
