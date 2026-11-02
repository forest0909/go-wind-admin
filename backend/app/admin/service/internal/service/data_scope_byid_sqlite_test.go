package service

import (
	"context"
	"testing"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"go-wind-admin/third_party/go-crud/viewer"
	"go-wind-admin/third_party/go-utils/trans"
	paginationV1 "go-wind-admin/third_party/go-crud/api/gen/go/pagination/v1"
	"go-wind-admin/third_party/kratos-bootstrap/bootstrap"
	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"

	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/app/admin/service/internal/data/ent"
	"go-wind-admin/app/admin/service/internal/data/enttest"
	"go-wind-admin/pkg/middleware/auth"

	auditV1 "go-wind-admin/api/gen/go/audit/service/v1"
	authenticationV1 "go-wind-admin/api/gen/go/authentication/service/v1"
	identityV1 "go-wind-admin/api/gen/go/identity/service/v1"
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
