// authentication_field_permission.go 的 SQLite 内存库集成测试（白盒，包内测试）。
//
// 覆盖目标（aggregateHiddenFields）：
//   - tenantID==0 的普通用户按角色聚合，User.email 写入令牌 HiddenFields。
//   - 平台管理员不聚合。tenantID==0 与非 0 都为空；非 0 这条用来区分
//     「按 is_platform_admin 跳过」和旧的「tenantID==0 一律返回」。
//   - 装饰器：把普通用户聚合出的令牌放进上下文，List 响应里的 email 被清空。
package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go-wind-admin/third_party/go-utils/trans"

	paginationV1 "go-wind-admin/third_party/go-crud/api/gen/go/pagination/v1"
	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"

	authenticationV1 "go-wind-admin/api/gen/go/authentication/service/v1"
	permissionV1 "go-wind-admin/api/gen/go/permission/service/v1"
	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/app/admin/service/internal/data/ent"
	entRole "go-wind-admin/app/admin/service/internal/data/ent/role"
	"go-wind-admin/app/admin/service/internal/data/enttest"
	"go-wind-admin/pkg/middleware/auth"
)

// newHiddenFieldAuthServiceForTest 白盒构造 aggregateHiddenFields 用到的字段：
// log 用 NopLogger；roleRepo / roleFieldPermissionRepo 走 testkit，共用同一个内存库。
func newHiddenFieldAuthServiceForTest(t *testing.T) (*AuthenticationService, *ent.Client) {
	t.Helper()
	entClient := enttest.NewEntClientForTest(t)
	svc := &AuthenticationService{
		log:                     bLogger.NewHelper(bLogger.NopLogger()),
		roleRepo:                data.NewRoleRepoForTest(entClient),
		roleFieldPermissionRepo: data.NewRoleFieldPermissionRepoForTest(entClient),
	}
	return svc, entClient.Client()
}

// seedHiddenEmailRole 落一条隐藏 User.email 的角色并按 code 反查 ID。
func seedHiddenEmailRole(t *testing.T, client *ent.Client, ctx context.Context, svc *AuthenticationService, code string) uint32 {
	t.Helper()
	require.NoError(t, svc.roleRepo.Create(ctx, &permissionV1.CreateRoleRequest{
		Data: &permissionV1.Role{
			TenantId: trans.Ptr(uint32(0)),
			Name:     trans.Ptr("字段权限测试角色 " + code),
			Code:     trans.Ptr(code),
			Status:   permissionV1.Role_ON.Enum(),
			Type:     permissionV1.Role_SYSTEM.Enum(),
			FieldPermissions: []*permissionV1.RoleFieldPermission{
				{Resource: "User", HiddenFields: []string{"email"}},
			},
		},
	}))
	row, err := client.Role.Query().Where(entRole.CodeEQ(code)).Only(ctx)
	require.NoError(t, err, "按 code 反查刚创建的角色应命中")
	return row.ID
}

// TestAggregateHiddenFields_TenantZeroOrdinaryHasUserEmail tenantID==0 的普通用户
// 按角色聚合，令牌 HiddenFields 含 "User.email"。
func TestAggregateHiddenFields_TenantZeroOrdinaryHasUserEmail(t *testing.T) {
	svc, client := newHiddenFieldAuthServiceForTest(t)
	ctx := enttest.NewSystemContext(context.Background())
	roleID := seedHiddenEmailRole(t, client, ctx, svc, "HFS_T0_USER_EMAIL")
	payload := &authenticationV1.UserTokenPayload{}

	svc.aggregateHiddenFields(ctx, 0, []uint32{roleID}, payload)

	require.Equal(t, []string{"User.email"}, payload.GetHiddenFields(),
		"tenantID==0 的普通用户应把角色隐藏字段写入令牌")
	require.False(t, payload.GetIsPlatformAdmin())
}

// TestAggregateHiddenFields_PlatformAdminEmpty 平台管理员不聚合隐藏字段。
// tenantID==0 与 tenantID!=0 都为空。旧实现只在 tenantID==0 返回，回退后
// 非 0 租户的平台管理员会带上 User.email，本测试失败。
func TestAggregateHiddenFields_PlatformAdminEmpty(t *testing.T) {
	svc, client := newHiddenFieldAuthServiceForTest(t)
	ctx := enttest.NewSystemContext(context.Background())
	roleID := seedHiddenEmailRole(t, client, ctx, svc, "HFS_T0_ADMIN_EMAIL")

	t.Run("tenant0", func(t *testing.T) {
		payload := &authenticationV1.UserTokenPayload{IsPlatformAdmin: trans.Ptr(true)}
		svc.aggregateHiddenFields(ctx, 0, []uint32{roleID}, payload)
		require.Empty(t, payload.GetHiddenFields(), "tenantID==0 的平台管理员隐藏字段应为空")
		require.True(t, payload.GetIsPlatformAdmin())
	})

	t.Run("tenantNonZero", func(t *testing.T) {
		payload := &authenticationV1.UserTokenPayload{IsPlatformAdmin: trans.Ptr(true)}
		svc.aggregateHiddenFields(ctx, 7, []uint32{roleID}, payload)
		require.Empty(t, payload.GetHiddenFields(),
			"平台管理员在非 0 租户也不聚合；这条在 tenantID==0 短路回退后会失败")
	})
}

// TestFieldPermissionUser_ListClearsEmailFromTenantZeroToken 装饰器层：
// 用 tenantID==0 普通用户聚合出的令牌调 List，响应里的 email 被清空。
func TestFieldPermissionUser_ListClearsEmailFromTenantZeroToken(t *testing.T) {
	svc, client := newHiddenFieldAuthServiceForTest(t)
	ctx := enttest.NewSystemContext(context.Background())
	roleID := seedHiddenEmailRole(t, client, ctx, svc, "HFS_T0_DECORATOR_EMAIL")
	payload := &authenticationV1.UserTokenPayload{}
	svc.aggregateHiddenFields(ctx, 0, []uint32{roleID}, payload)
	require.Equal(t, []string{"User.email"}, payload.GetHiddenFields())

	inner := &innerUserServiceStub{}
	wrapped := NewFieldPermissionUserServiceServer(inner)
	listResp, err := wrapped.List(auth.NewContext(context.Background(), payload), &paginationV1.PagingRequest{})
	require.NoError(t, err)
	require.Len(t, listResp.GetItems(), 2)
	for i, item := range listResp.GetItems() {
		require.Empty(t, item.GetEmail(), "第 %d 个用户的 email 应被聚合出的隐藏字段清空", i)
		require.NotEmpty(t, item.GetNickname(), "第 %d 个用户的未命中字段昵称应保持", i)
	}
}
