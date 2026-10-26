// RoleService 的 SQLite 内存库集成测试（白盒，包内测试）。
//
// 覆盖目标：
//   - List / Get 的 enrichment：TenantName 经 tenantRepo.ListTenantsByIds 从租户表回填；
//     平台级角色（TenantId 未设置）不回填。
//   - Create / Delete 基本路径：Create 走 auth.FromContext 操作人注入（CreatedBy 回填）
//     与默认角色播种；Delete 走非保护角色删除 + 关联清理。
//
// 桩说明：Create/Update/Delete 尾部会调 authorizer.ResetPolicies，nil 会 panic，
// 按 NewAuthorizer 的 noop 引擎形态构造最小可用 authorizer（Provider 桩返回空策略，
// noop 引擎的 ResetPolicies 直接返回 nil）。
package service

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	paginationV1 "go-wind-admin/third_party/go-crud/api/gen/go/pagination/v1"
	"go-wind-admin/third_party/go-utils/trans"
	"go-wind-admin/third_party/kratos-bootstrap/bootstrap"
	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/app/admin/service/internal/data/ent"
	"go-wind-admin/app/admin/service/internal/data/enttest"

	authenticationV1 "go-wind-admin/api/gen/go/authentication/service/v1"
	identityV1 "go-wind-admin/api/gen/go/identity/service/v1"
	permissionV1 "go-wind-admin/api/gen/go/permission/service/v1"
	conf "go-wind-admin/third_party/kratos-bootstrap/api/gen/go/conf/v1"

	"go-wind-admin/pkg/middleware/auth"
	"go-wind-admin/third_party/go-utils/authorizer"
)

// roleServiceAuthProviderStub：authorizer.Provider 桩，返回空模型/空策略，
// 仅供 noop 引擎的 ResetPolicies 走通（noop 分支不消费策略数据）。
type roleServiceAuthProviderStub struct{}

func (roleServiceAuthProviderStub) ProvideModels(string) authorizer.ModelDataMap { return nil }

func (roleServiceAuthProviderStub) ProvidePolicies(context.Context) (authorizer.PermissionDataMap, error) {
	return nil, nil
}

// newRoleServiceForTest 白盒复刻 NewRoleService 的字段初始化：
// log 换 NopLogger，repo 用 testkit 构造器，authorizer 显式用 noop 引擎最小桩；
// 并复刻生产构造器的 svc.init()（空表时播种 constants.DefaultRoles）。
func newRoleServiceForTest(t *testing.T) *RoleService {
	t.Helper()
	entClient := enttest.NewEntClientForTest(t)
	boot := bootstrap.NewContextWithParam(context.Background(), nil, nil, bLogger.NopLogger())
	// 空类型不再退回 noop。测试要放行时显式指定 noop。
	svc := &RoleService{
		log:          bLogger.NewHelper(bLogger.NopLogger()),
		authorizer:   authorizer.NewAuthorizer(context.Background(), bLogger.NopLogger(), &authorizer.EngineConfig{Type: "noop"}, roleServiceAuthProviderStub{}),
		roleRepo:     data.NewRoleRepoForTest(entClient),
		tenantRepo:   data.NewTenantRepoForTest(entClient),
		userRoleRepo: data.NewUserRoleRepo(boot, entClient),
	}
	svc.init()
	return svc
}

// TestRoleServiceSqlite_ListEnrichment 验证 List 的 TenantName 回填：
// 租户级角色回填租户名，平台级角色（默认播种）不回填。
func TestRoleServiceSqlite_ListEnrichment(t *testing.T) {
	svc := newRoleServiceForTest(t)
	ctx := enttest.NewSystemContext(context.Background())

	tenant, err := svc.tenantRepo.Create(ctx, &identityV1.Tenant{
		Name:        trans.Ptr("RoleSvc 富集租户甲"),
		Code:        trans.Ptr("ROLESVC_TENANT_A"),
		Status:      identityV1.Tenant_ON.Enum(),
		Type:        identityV1.Tenant_TRIAL.Enum(),
		AuditStatus: identityV1.Tenant_APPROVED.Enum(),
	})
	require.NoError(t, err)
	require.NotNil(t, tenant.Id)

	err = svc.roleRepo.Create(ctx, &permissionV1.CreateRoleRequest{
		Data: &permissionV1.Role{
			TenantId: tenant.Id,
			Name:     trans.Ptr("RoleSvc 租户角色甲"),
			Code:     trans.Ptr("ROLESVC_ROLE_TENANT_A"),
			Status:   permissionV1.Role_ON.Enum(),
			Type:     permissionV1.Role_TENANT.Enum(),
		},
	})
	require.NoError(t, err)

	resp, err := svc.List(ctx, &paginationV1.PagingRequest{})
	require.NoError(t, err)

	var sawTenantRole bool
	var sawPlatformRoleWithoutTenant bool
	for _, item := range resp.GetItems() {
		if item.GetCode() == "ROLESVC_ROLE_TENANT_A" {
			sawTenantRole = true
			require.Equal(t, "RoleSvc 富集租户甲", item.GetTenantName(),
				"租户级角色应回填租户名")
			continue
		}
		// 默认播种的平台级角色（TenantId 未设置）：不应回填租户名。
		if item.GetTenantId() == 0 {
			require.Empty(t, item.GetTenantName(),
				"平台级角色不应回填租户名")
			sawPlatformRoleWithoutTenant = true
		}
	}
	require.True(t, sawTenantRole, "列表应包含新建的租户级角色")
	require.True(t, sawPlatformRoleWithoutTenant, "列表应包含默认播种的平台级角色")
}

// TestRoleServiceSqlite_GetEnrichment 验证 Get 单条查询的 TenantName 回填。
func TestRoleServiceSqlite_GetEnrichment(t *testing.T) {
	svc := newRoleServiceForTest(t)
	ctx := enttest.NewSystemContext(context.Background())

	tenant, err := svc.tenantRepo.Create(ctx, &identityV1.Tenant{
		Name:        trans.Ptr("RoleSvc 富集租户乙"),
		Code:        trans.Ptr("ROLESVC_TENANT_B"),
		Status:      identityV1.Tenant_ON.Enum(),
		Type:        identityV1.Tenant_TRIAL.Enum(),
		AuditStatus: identityV1.Tenant_APPROVED.Enum(),
	})
	require.NoError(t, err)
	require.NotNil(t, tenant.Id)

	err = svc.roleRepo.Create(ctx, &permissionV1.CreateRoleRequest{
		Data: &permissionV1.Role{
			TenantId: tenant.Id,
			Name:     trans.Ptr("RoleSvc 租户角色乙"),
			Code:     trans.Ptr("ROLESVC_ROLE_TENANT_B"),
			Status:   permissionV1.Role_ON.Enum(),
			Type:     permissionV1.Role_TENANT.Enum(),
		},
	})
	require.NoError(t, err)

	// RoleRepo.Create 只返回 error，角色 ID 从列表反查（按唯一 code 定位）。
	listResp, err := svc.List(ctx, &paginationV1.PagingRequest{})
	require.NoError(t, err)
	var roleID uint32
	var found bool
	for _, item := range listResp.GetItems() {
		if item.GetCode() == "ROLESVC_ROLE_TENANT_B" {
			roleID = item.GetId()
			found = true
		}
	}
	require.True(t, found, "创建后列表应包含新角色")

	resp, err := svc.Get(ctx, &permissionV1.GetRoleRequest{
		QueryBy: &permissionV1.GetRoleRequest_Id{Id: roleID},
	})
	require.NoError(t, err)
	require.Equal(t, "RoleSvc 租户角色乙", resp.GetName())
	require.Equal(t, "RoleSvc 富集租户乙", resp.GetTenantName(),
		"租户级角色的 Get 应回填租户名")
}

// TestRoleServiceSqlite_CreateAndDelete 验证 Create 的操作人注入与
// Delete 的非保护角色删除路径。
func TestRoleServiceSqlite_CreateAndDelete(t *testing.T) {
	svc := newRoleServiceForTest(t)
	baseCtx := enttest.NewSystemContext(context.Background())
	opCtx := auth.NewContext(baseCtx, &authenticationV1.UserTokenPayload{UserId: 4242})

	// Create：操作人注入 CreatedBy，角色本体与角色元数据同事务落库。
	_, err := svc.Create(opCtx, &permissionV1.CreateRoleRequest{
		Data: &permissionV1.Role{
			Name:   trans.Ptr("RoleSvc 创建角色甲"),
			Code:   trans.Ptr("ROLESVC_CREATE_A"),
			Status: permissionV1.Role_ON.Enum(),
			Type:   permissionV1.Role_TENANT.Enum(),
		},
	})
	require.NoError(t, err, "service.Create 应走完操作人注入与事务创建")

	var createdID uint32
	listResp, err := svc.List(baseCtx, &paginationV1.PagingRequest{})
	require.NoError(t, err)
	var found bool
	for _, item := range listResp.GetItems() {
		if item.GetCode() == "ROLESVC_CREATE_A" {
			found = true
			createdID = item.GetId()
			require.EqualValues(t, 4242, item.GetCreatedBy(),
				"CreatedBy 应为操作人注入的 ID")
		}
	}
	require.True(t, found, "创建后列表应包含新角色")

	// Delete：非保护角色应删除成功（含关联行清理）。
	_, err = svc.Delete(baseCtx, &permissionV1.DeleteRoleRequest{
		QueryBy: &permissionV1.DeleteRoleRequest_Id{Id: createdID},
	})
	require.NoError(t, err, "非保护角色的 service.Delete 应成功")

	after, err := svc.List(baseCtx, &paginationV1.PagingRequest{})
	require.NoError(t, err)
	for _, item := range after.GetItems() {
		require.NotEqual(t, "ROLESVC_CREATE_A", item.GetCode(),
			"删除后列表不应再包含该角色")
	}
}

// roleGrantEnv 给角色授权吊销测试用：真实 user_role 表 + miniredis 认证器。
type roleGrantEnv struct {
	svc    *RoleService
	client *ent.Client
	sys    context.Context
}

func newRoleGrantEnv(t *testing.T) *roleGrantEnv {
	t.Helper()
	entClient := enttest.NewEntClientForTest(t)
	sys := enttest.NewSystemContext(context.Background())
	boot := bootstrap.NewContextWithParam(context.Background(), nil, nil, bLogger.NopLogger())

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	userTokenCache := data.NewUserTokenCacheForTest(rdb)
	authenticator, err := data.NewAuthenticatorForTest(
		&conf.Authentication_Jwt{Method: "HS256", Key: authSvcTestJWTKey},
		userTokenCache,
	)
	require.NoError(t, err)

	svc := &RoleService{
		log:           bLogger.NewHelper(bLogger.NopLogger()),
		authorizer:    authorizer.NewAuthorizer(context.Background(), bLogger.NopLogger(), &authorizer.EngineConfig{Type: "noop"}, roleServiceAuthProviderStub{}),
		roleRepo:      data.NewRoleRepoForTest(entClient),
		tenantRepo:    data.NewTenantRepoForTest(entClient),
		userRoleRepo:  data.NewUserRoleRepo(boot, entClient),
		authenticator: authenticator,
	}
	svc.init()
	return &roleGrantEnv{svc: svc, client: entClient.Client(), sys: sys}
}

func (e *roleGrantEnv) createRole(t *testing.T, code, name string) uint32 {
	t.Helper()
	op := opCtx(e.sys, roleGrantHolderID, 0, true)
	_, err := e.svc.Create(op, &permissionV1.CreateRoleRequest{
		Data: &permissionV1.Role{
			Name:   trans.Ptr(name),
			Code:   trans.Ptr(code),
			Status: permissionV1.Role_ON.Enum(),
			Type:   permissionV1.Role_TENANT.Enum(),
		},
	})
	require.NoError(t, err)
	resp, err := e.svc.List(e.sys, &paginationV1.PagingRequest{})
	require.NoError(t, err)
	for _, item := range resp.GetItems() {
		if item.GetCode() == code {
			return item.GetId()
		}
	}
	t.Fatalf("创建后找不到角色 %s", code)
	return 0
}

func (e *roleGrantEnv) bindHolder(t *testing.T, roleID uint32) {
	t.Helper()
	_, err := e.client.UserRole.Create().
		SetUserID(roleGrantHolderID).
		SetRoleID(roleID).
		Save(e.sys)
	require.NoError(t, err)
}

func issueRoleAccessToken(t *testing.T, authn *data.Authenticator, userID uint32, username string) string {
	t.Helper()
	access, _, err := authn.CreateUserToken(context.Background(), authenticationV1.ClientType_admin, &authenticationV1.UserTokenPayload{
		UserId:   userID,
		Username: trans.Ptr(username),
	})
	require.NoError(t, err)
	require.NotEmpty(t, access)
	return access
}

func requireRoleAccessTokenValid(t *testing.T, authn *data.Authenticator, access string, valid bool) {
	t.Helper()
	resp, err := authn.Authenticate(context.Background(), &authenticationV1.ValidateTokenRequest{
		Token:         access,
		ClientType:    authenticationV1.ClientType_admin,
		TokenCategory: authenticationV1.TokenCategory_ACCESS,
	})
	if valid {
		require.NoError(t, err)
		require.True(t, resp.GetIsValid())
		return
	}
	if err == nil {
		require.False(t, resp.GetIsValid())
	}
}

const (
	roleGrantHolderID    uint32 = 71001
	roleGrantBystanderID uint32 = 71002
)

// TestRoleServiceSqlite_GrantChangeRevokesHolderNotBystander 改权限或停用角色后，
// 持有人（这里就是操作者本人）的令牌失效，不持有的人不受影响；只改名称时两边都还有效。
func TestRoleServiceSqlite_GrantChangeRevokesHolderNotBystander(t *testing.T) {
	e := newRoleGrantEnv(t)
	roleID := e.createRole(t, "ROLE_GRANT_REVOKE", "授权吊销前")
	e.bindHolder(t, roleID)
	op := opCtx(e.sys, roleGrantHolderID, 0, true)

	holder := issueRoleAccessToken(t, e.svc.authenticator, roleGrantHolderID, "holder")
	bystander := issueRoleAccessToken(t, e.svc.authenticator, roleGrantBystanderID, "bystander")

	_, err := e.svc.Update(op, &permissionV1.UpdateRoleRequest{
		Id: roleID,
		Data: &permissionV1.Role{
			Name: trans.Ptr("只改名称"),
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"name"}},
	})
	require.NoError(t, err)
	requireRoleAccessTokenValid(t, e.svc.authenticator, holder, true)
	requireRoleAccessTokenValid(t, e.svc.authenticator, bystander, true)

	_, err = e.svc.Update(op, &permissionV1.UpdateRoleRequest{
		Id: roleID,
		Data: &permissionV1.Role{
			Status: permissionV1.Role_OFF.Enum(),
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"status"}},
	})
	require.NoError(t, err)
	requireRoleAccessTokenValid(t, e.svc.authenticator, holder, false)
	requireRoleAccessTokenValid(t, e.svc.authenticator, bystander, true)

	holder = issueRoleAccessToken(t, e.svc.authenticator, roleGrantHolderID, "holder")
	_, err = e.svc.Update(op, &permissionV1.UpdateRoleRequest{
		Id: roleID,
		Data: &permissionV1.Role{
			Permissions: []uint32{9001},
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"permissions"}},
	})
	require.NoError(t, err)
	requireRoleAccessTokenValid(t, e.svc.authenticator, holder, false)
	requireRoleAccessTokenValid(t, e.svc.authenticator, bystander, true)
}

// TestRoleServiceSqlite_DeleteRevokesHolderTokens 删除角色后，持有人的令牌失效，旁人不受影响。
func TestRoleServiceSqlite_DeleteRevokesHolderTokens(t *testing.T) {
	e := newRoleGrantEnv(t)
	roleID := e.createRole(t, "ROLE_GRANT_DELETE", "待删除角色")
	e.bindHolder(t, roleID)

	holder := issueRoleAccessToken(t, e.svc.authenticator, roleGrantHolderID, "holder")
	bystander := issueRoleAccessToken(t, e.svc.authenticator, roleGrantBystanderID, "bystander")

	_, err := e.svc.Delete(e.sys, &permissionV1.DeleteRoleRequest{
		QueryBy: &permissionV1.DeleteRoleRequest_Id{Id: roleID},
	})
	require.NoError(t, err)
	requireRoleAccessTokenValid(t, e.svc.authenticator, holder, false)
	requireRoleAccessTokenValid(t, e.svc.authenticator, bystander, true)
}
