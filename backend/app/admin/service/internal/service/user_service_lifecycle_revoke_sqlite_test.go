package service

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go-wind-admin/third_party/go-utils/trans"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	identityV1 "go-wind-admin/api/gen/go/identity/service/v1"
	permissionV1 "go-wind-admin/api/gen/go/permission/service/v1"
)

// 停用（status-only mask）和删除用户都要吊销其全部令牌。修复前两种情况旧 AT 都仍能认证。
func TestUserServiceSqlite_DisableAndDeleteRevokeTokens(t *testing.T) {
	e := newUserServiceForTest(t)
	role := e.seedUserSvcRole(t, nil, permissionV1.Role_SYSTEM, "USERSVC_ROLE_LIFECYCLE")
	e.registerOperator(9001)
	op := opCtx(e.ctx, 9001, 0, true)

	const target = uint32(10501)
	e.stub.usersByID[target] = &identityV1.User{Id: trans.Ptr(target), Username: trans.Ptr("lifecycle-user")}

	// 只改资料（status=NORMAL）不吊销
	at := issueAdminAccessToken(t, e, target, "lifecycle-user")
	_, err := e.svc.Update(op, &identityV1.UpdateUserRequest{
		Id:         target,
		Data:       &identityV1.User{RoleIds: []uint32{role}, Status: identityV1.User_NORMAL.Enum()},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"status"}},
	})
	require.NoError(t, err)
	requireAccessTokenValid(t, e, at, true)

	// mask 里没有 status：即使请求体带了 DISABLED 也不会落库，不吊销
	_, err = e.svc.Update(op, &identityV1.UpdateUserRequest{
		Id:         target,
		Data:       &identityV1.User{RoleIds: []uint32{role}, Status: identityV1.User_DISABLED.Enum(), Nickname: trans.Ptr("x")},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"nickname"}},
	})
	require.NoError(t, err)
	requireAccessTokenValid(t, e, at, true)

	// 停用
	_, err = e.svc.Update(op, &identityV1.UpdateUserRequest{
		Id:         target,
		Data:       &identityV1.User{RoleIds: []uint32{role}, Status: identityV1.User_DISABLED.Enum()},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"status"}},
	})
	require.NoError(t, err)
	requireAccessTokenValid(t, e, at, false)

	// 删除
	at = issueAdminAccessToken(t, e, target, "lifecycle-user")
	_, err = e.svc.Delete(op, &identityV1.DeleteUserRequest{QueryBy: &identityV1.DeleteUserRequest_Id{Id: target}})
	require.NoError(t, err)
	require.Equal(t, []uint32{target}, e.stub.deletes)
	requireAccessTokenValid(t, e, at, false)
}
