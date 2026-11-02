package service

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	paginationV1 "go-wind-admin/third_party/go-crud/api/gen/go/pagination/v1"
	"go-wind-admin/third_party/go-crud/viewer"
	"go-wind-admin/third_party/go-utils/trans"
	conf "go-wind-admin/third_party/kratos-bootstrap/api/gen/go/conf/v1"
	"go-wind-admin/third_party/kratos-bootstrap/bootstrap"
	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"

	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/app/admin/service/internal/data/ent"
	"go-wind-admin/app/admin/service/internal/data/enttest"
	"go-wind-admin/pkg/middleware/auth"

	auditV1 "go-wind-admin/api/gen/go/audit/service/v1"
	authenticationV1 "go-wind-admin/api/gen/go/authentication/service/v1"
	identityV1 "go-wind-admin/api/gen/go/identity/service/v1"
	permissionV1 "go-wind-admin/api/gen/go/permission/service/v1"
)

// seedTwoUnits 单元 1、2 各一个成员：用户 10∈1（操作人）、11∈2。
func seedTwoUnits(t *testing.T, client *ent.Client, sys context.Context) {
	t.Helper()
	require.NoError(t, client.OrgUnit.Create().SetID(1).SetName("u1").SetCode("DS1").SetPath("/1/").Exec(sys))
	require.NoError(t, client.OrgUnit.Create().SetID(2).SetName("u2").SetCode("DS2").SetPath("/2/").Exec(sys))
	require.NoError(t, client.User.Create().SetID(10).SetUsername("ds_op").SetNickname("op").Exec(sys))
	require.NoError(t, client.User.Create().SetID(11).SetUsername("ds_other").SetNickname("before").Exec(sys))
	require.NoError(t, client.UserOrgUnit.Create().SetUserID(10).SetOrgUnitID(1).Exec(sys))
	require.NoError(t, client.UserOrgUnit.Create().SetUserID(11).SetOrgUnitID(2).Exec(sys))
}

// unitOnlyOperatorCtx 操作人 10，数据范围 UNIT[1]。
func unitOnlyOperatorCtx() context.Context {
	ctx := viewer.WithContext(context.Background(), viewer.NewUserContext(10, 0, 0, "",
		[]viewer.DataScope{{ScopeType: viewer.ScopeTypeUnit, TargetIDs: []uint64{1}}}))
	return auth.NewContext(ctx, &authenticationV1.UserTokenPayload{UserId: 10})
}

// TestUserService_DataScopeByIDNotFound 数据范围外的用户：详情/修改/删除/重置密码一律 404，行不变。
func TestUserService_DataScopeByIDNotFound(t *testing.T) {
	svc, client, sys := newUserServiceWithRealUserRepo(t)
	seedTwoUnits(t, client, sys)
	ctx := unitOnlyOperatorCtx()

	_, err := svc.Get(ctx, &identityV1.GetUserRequest{QueryBy: &identityV1.GetUserRequest_Id{Id: 11}})
	require.True(t, kerrors.IsNotFound(err), "Get: %v", err)

	_, err = svc.Update(ctx, &identityV1.UpdateUserRequest{
		Id:         11,
		Data:       &identityV1.User{Nickname: trans.Ptr("after")},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"nickname"}},
	})
	require.True(t, kerrors.IsNotFound(err), "Update: %v", err)

	_, err = svc.Delete(ctx, &identityV1.DeleteUserRequest{QueryBy: &identityV1.DeleteUserRequest_Id{Id: 11}})
	require.True(t, kerrors.IsNotFound(err), "Delete: %v", err)

	_, err = svc.EditUserPassword(ctx, &identityV1.EditUserPasswordRequest{UserId: 11, NewPassword: "x"})
	require.True(t, kerrors.IsNotFound(err), "EditUserPassword: %v", err)

	row, err := client.User.Get(sys, 11)
	require.NoError(t, err, "越界删除不应生效")
	require.NotNil(t, row.Nickname)
	require.Equal(t, "before", *row.Nickname, "越界修改不应生效")

	// 范围内的用户（本人）照常可读
	got, err := svc.Get(ctx, &identityV1.GetUserRequest{QueryBy: &identityV1.GetUserRequest_Id{Id: 10}})
	require.NoError(t, err)
	require.EqualValues(t, 10, got.GetId())
}

// TestOrgUnitService_DataScopeByIDNotFound 数据范围外的组织单元：详情/修改/删除 404，行不变。
func TestOrgUnitService_DataScopeByIDNotFound(t *testing.T) {
	entClient := enttest.NewEntClientForTest(t)
	svc := &OrgUnitService{
		log:         bLogger.NewHelper(bLogger.NopLogger()),
		orgUnitRepo: data.NewOrgUnitRepoForTest(entClient),
		userRepo:    &orgUnitServiceUserRepoStub{},
	}
	client := entClient.Client()
	sys := enttest.NewSystemContext(context.Background())
	seedTwoUnits(t, client, sys)
	ctx := unitOnlyOperatorCtx()

	_, err := svc.Get(ctx, &identityV1.GetOrgUnitRequest{QueryBy: &identityV1.GetOrgUnitRequest_Id{Id: 2}})
	require.True(t, kerrors.IsNotFound(err), "Get: %v", err)

	_, err = svc.Update(ctx, &identityV1.UpdateOrgUnitRequest{
		Id:         2,
		Data:       &identityV1.OrgUnit{Name: trans.Ptr("renamed")},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"name"}},
	})
	require.True(t, kerrors.IsNotFound(err), "Update: %v", err)

	_, err = svc.Delete(ctx, &identityV1.DeleteOrgUnitRequest{QueryBy: &identityV1.DeleteOrgUnitRequest_Id{Id: 2}})
	require.True(t, kerrors.IsNotFound(err), "Delete: %v", err)

	row, err := client.OrgUnit.Get(sys, 2)
	require.NoError(t, err, "越界删除不应生效")
	require.Equal(t, "u2", *row.Name)

	// 范围内的单元照常可改
	_, err = svc.Update(ctx, &identityV1.UpdateOrgUnitRequest{
		Id:         1,
		Data:       &identityV1.OrgUnit{Name: trans.Ptr("renamed")},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"name"}},
	})
	require.NoError(t, err)
	row, err = client.OrgUnit.Get(sys, 1)
	require.NoError(t, err)
	require.Equal(t, "renamed", *row.Name)
}

// TestAuditLogServices_DataScope 日志列表/详情按操作人是否可见过滤（UNIT[1] 只看到用户 10 的日志）。
func TestAuditLogServices_DataScope(t *testing.T) {
	entClient := enttest.NewEntClientForTest(t)
	client := entClient.Client()
	sys := enttest.NewSystemContext(context.Background())
	seedTwoUnits(t, client, sys)
	boot := bootstrap.NewContextWithParam(context.Background(), nil, nil, bLogger.NopLogger())
	loginSvc := NewLoginAuditLogService(boot, data.NewLoginAuditLogRepo(boot, entClient))
	opSvc := NewOperationAuditLogService(boot, data.NewOperationAuditLogRepo(boot, entClient))

	foreignLogin, err := client.LoginAuditLog.Create().SetUserID(11).Save(sys)
	require.NoError(t, err)
	require.NoError(t, client.LoginAuditLog.Create().SetUserID(10).Exec(sys))
	foreignOp, err := client.OperationAuditLog.Create().SetUserID(11).Save(sys)
	require.NoError(t, err)
	require.NoError(t, client.OperationAuditLog.Create().SetUserID(10).Exec(sys))

	ctx := unitOnlyOperatorCtx()

	logins, err := loginSvc.List(ctx, &paginationV1.PagingRequest{})
	require.NoError(t, err)
	require.Len(t, logins.GetItems(), 1)
	require.EqualValues(t, 10, logins.GetItems()[0].GetUserId())
	_, err = loginSvc.Get(ctx, &auditV1.GetLoginAuditLogRequest{QueryBy: &auditV1.GetLoginAuditLogRequest_Id{Id: foreignLogin.ID}})
	require.True(t, kerrors.IsNotFound(err), "login Get: %v", err)

	ops, err := opSvc.List(ctx, &paginationV1.PagingRequest{})
	require.NoError(t, err)
	require.Len(t, ops.GetItems(), 1)
	require.EqualValues(t, 10, ops.GetItems()[0].GetUserId())
	_, err = opSvc.Get(ctx, &auditV1.GetOperationAuditLogRequest{QueryBy: &auditV1.GetOperationAuditLogRequest_Id{Id: foreignOp.ID}})
	require.True(t, kerrors.IsNotFound(err), "operation Get: %v", err)
}

func newOrgUnitServiceWithClient(t *testing.T) (*OrgUnitService, *ent.Client, context.Context) {
	t.Helper()
	entClient := enttest.NewEntClientForTest(t)
	svc := &OrgUnitService{
		log:         bLogger.NewHelper(bLogger.NopLogger()),
		orgUnitRepo: data.NewOrgUnitRepoForTest(entClient),
		userRepo:    &orgUnitServiceUserRepoStub{},
	}
	return svc, entClient.Client(), enttest.NewSystemContext(context.Background())
}

func operatorCtx(uid uint64, scopes ...viewer.DataScope) context.Context {
	ctx := viewer.WithContext(context.Background(), viewer.NewUserContext(uid, 0, 0, "", scopes))
	return auth.NewContext(ctx, &authenticationV1.UserTokenPayload{UserId: uint32(uid)})
}

// TestOrgUnitService_DeleteRejectsSubtreeOutsideScope 删除可见单元时，子树里有范围外的后代就整体 403，
// 不删任何单元、不清任何用户绑定；子树整棵可见时照常级联删除。
func TestOrgUnitService_DeleteRejectsSubtreeOutsideScope(t *testing.T) {
	unit := func(ids ...uint64) viewer.DataScope {
		return viewer.DataScope{ScopeType: viewer.ScopeTypeUnit, TargetIDs: ids}
	}
	self := viewer.DataScope{ScopeType: viewer.ScopeTypeSelf}

	// 单元 1 → 子单元 2；用户 10∈1，用户 11∈2
	seed := func(t *testing.T) (*OrgUnitService, *ent.Client, context.Context) {
		svc, client, sys := newOrgUnitServiceWithClient(t)
		require.NoError(t, client.OrgUnit.Create().SetID(1).SetName("p").SetCode("P").SetPath("/1/").Exec(sys))
		require.NoError(t, client.OrgUnit.Create().SetID(2).SetName("c").SetCode("C").SetPath("/1/2/").SetParentID(1).Exec(sys))
		require.NoError(t, client.UserOrgUnit.Create().SetUserID(10).SetOrgUnitID(1).Exec(sys))
		require.NoError(t, client.UserOrgUnit.Create().SetUserID(11).SetOrgUnitID(2).Exec(sys))
		return svc, client, sys
	}
	del := &identityV1.DeleteOrgUnitRequest{QueryBy: &identityV1.DeleteOrgUnitRequest_Id{Id: 1}}

	for name, ctx := range map[string]context.Context{
		"UNIT_ONLY/SELECTED[1]": operatorCtx(10, unit(1)),
		"SELF（本人属于 1）":          operatorCtx(10, self),
	} {
		t.Run(name, func(t *testing.T) {
			svc, client, sys := seed(t)
			_, err := svc.Delete(ctx, del)
			require.True(t, kerrors.IsForbidden(err), "子树含范围外后代应 403，得到 %v", err)
			n, err := client.OrgUnit.Query().Count(sys)
			require.NoError(t, err)
			require.Equal(t, 2, n, "不应删除任何单元")
			n, err = client.UserOrgUnit.Query().Count(sys)
			require.NoError(t, err)
			require.Equal(t, 2, n, "不应清理任何用户绑定")
		})
	}

	t.Run("UNIT_AND_CHILD[1,2] 整棵可见", func(t *testing.T) {
		svc, client, sys := seed(t)
		_, err := svc.Delete(operatorCtx(10, unit(1, 2)), del)
		require.NoError(t, err)
		n, err := client.OrgUnit.Query().Count(sys)
		require.NoError(t, err)
		require.Zero(t, n, "整棵子树级联删除")
	})
}

// TestDataScope_UpdateAllowMissing allow_missing：ALL 下目标不存在时照旧创建；
// 受限用户对已存在的越界资源带 allow_missing 仍然 404，行不变。
func TestDataScope_UpdateAllowMissing(t *testing.T) {
	all := viewer.DataScope{ScopeType: viewer.ScopeTypeAll}

	t.Run("org unit", func(t *testing.T) {
		svc, client, sys := newOrgUnitServiceWithClient(t)
		seedTwoUnits(t, client, sys)

		_, err := svc.Update(operatorCtx(10, all), &identityV1.UpdateOrgUnitRequest{
			Id:           99,
			Data:         &identityV1.OrgUnit{Name: trans.Ptr("created"), Code: trans.Ptr("AM99")},
			AllowMissing: trans.Ptr(true),
		})
		require.NoError(t, err, "ALL 下 allow_missing 应创建")
		n, err := client.OrgUnit.Query().Count(sys)
		require.NoError(t, err)
		require.Equal(t, 3, n)

		_, err = svc.Update(unitOnlyOperatorCtx(), &identityV1.UpdateOrgUnitRequest{
			Id:           2,
			Data:         &identityV1.OrgUnit{Name: trans.Ptr("hijack")},
			UpdateMask:   &fieldmaskpb.FieldMask{Paths: []string{"name"}},
			AllowMissing: trans.Ptr(true),
		})
		require.True(t, kerrors.IsNotFound(err), "越界 + allow_missing 应 404，得到 %v", err)
		row, err := client.OrgUnit.Get(sys, 2)
		require.NoError(t, err)
		require.Equal(t, "u2", *row.Name)
	})

	t.Run("user", func(t *testing.T) {
		svc, client, sys := newUserServiceWithRealUserRepo(t)
		seedTwoUnits(t, client, sys)
		roleID := seedUserSvcRoleOn(t, svc.roleRepo, sys, nil, permissionV1.Role_SYSTEM, "DS_ALLOW_MISSING")
		// 新建用户带角色会触发令牌吊销，需要一个真实的认证器
		mr := miniredis.RunT(t)
		rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
		t.Cleanup(func() { _ = rdb.Close() })
		authenticator, err := data.NewAuthenticatorForTest(
			&conf.Authentication_Jwt{Method: "HS256", Key: authSvcTestJWTKey}, data.NewUserTokenCacheForTest(rdb))
		require.NoError(t, err)
		svc.authenticator = authenticator

		adminCtx := auth.NewContext(
			viewer.WithContext(context.Background(), viewer.NewUserContext(10, 0, 0, "", []viewer.DataScope{all})),
			&authenticationV1.UserTokenPayload{UserId: 10, IsPlatformAdmin: trans.Ptr(true)})
		_, err = svc.Update(adminCtx, &identityV1.UpdateUserRequest{
			Id:           99,
			Data:         &identityV1.User{Username: trans.Ptr("am_created"), Nickname: trans.Ptr("created"), RoleIds: []uint32{roleID}},
			AllowMissing: trans.Ptr(true),
		})
		require.NoError(t, err, "ALL 下 allow_missing 应创建")
		n, err := client.User.Query().Count(sys)
		require.NoError(t, err)
		require.Equal(t, 3, n)

		_, err = svc.Update(unitOnlyOperatorCtx(), &identityV1.UpdateUserRequest{
			Id:           11,
			Data:         &identityV1.User{Nickname: trans.Ptr("hijack")},
			UpdateMask:   &fieldmaskpb.FieldMask{Paths: []string{"nickname"}},
			AllowMissing: trans.Ptr(true),
		})
		require.True(t, kerrors.IsNotFound(err), "越界 + allow_missing 应 404，得到 %v", err)
		row, err := client.User.Get(sys, 11)
		require.NoError(t, err)
		require.Equal(t, "before", *row.Nickname)
	})
}
