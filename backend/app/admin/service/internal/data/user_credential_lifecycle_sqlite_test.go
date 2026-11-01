package data

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go-wind-admin/third_party/go-crud/viewer"
	"go-wind-admin/third_party/go-utils/trans"

	authenticationV1 "go-wind-admin/api/gen/go/authentication/service/v1"
	identityV1 "go-wind-admin/api/gen/go/identity/service/v1"
	"go-wind-admin/app/admin/service/internal/data/ent/usercredential"
	"go-wind-admin/app/admin/service/internal/data/enttest"
)

func newUserAndCredentialRepoSqlite(t *testing.T) (*userRepo, *UserCredentialRepo) {
	t.Helper()
	ur := newUserRepoSqlite(t)
	cr := newUserCredentialRepoSqlite(t)
	cr.entClient = ur.entClient
	cr.configRepo.entClient = ur.entClient
	return ur, cr
}

func createUserWithPassword(t *testing.T, ur *userRepo, cr *UserCredentialRepo, ctx context.Context, tenantID uint32, username, pw string) uint32 {
	t.Helper()
	tx, err := ur.BeginTx(ctx)
	require.NoError(t, err)
	u, err := ur.CreateWithTx(ctx, tx, &identityV1.User{Username: trans.Ptr(username), TenantId: trans.Ptr(tenantID)})
	require.NoError(t, err)
	require.NoError(t, cr.CreateWithTx(ctx, tx, &authenticationV1.UserCredential{
		UserId: u.Id, TenantId: trans.Ptr(tenantID),
		IdentityType: authenticationV1.UserCredential_USERNAME.Enum(), Identifier: trans.Ptr(username),
		CredentialType: authenticationV1.UserCredential_PASSWORD_HASH.Enum(), Credential: trans.Ptr(pw),
		IsPrimary: trans.Ptr(true), Status: authenticationV1.UserCredential_ENABLED.Enum(),
	}))
	require.NoError(t, tx.Commit())
	return u.GetId()
}

// 删除用户要连同凭证一起删；否则同名重建后登录 NotSingular → 503。
func TestUserRepoSqlite_DeleteRemovesCredentials(t *testing.T) {
	ur, cr := newUserAndCredentialRepoSqlite(t)
	ctx := enttest.NewSystemContext(context.Background())

	oldID := createUserWithPassword(t, ur, cr, ctx, 0, "zs", "OldPass@1234")
	require.NoError(t, ur.Delete(ctx, &identityV1.DeleteUserRequest{QueryBy: &identityV1.DeleteUserRequest_Id{Id: oldID}}))

	left, err := ur.entClient.Client().UserCredential.Query().Where(usercredential.UserIDEQ(oldID)).Count(ctx)
	require.NoError(t, err)
	require.Zero(t, left, "删除用户后不应留下凭证")

	newID := createUserWithPassword(t, ur, cr, ctx, 0, "zs", "NewPass@1234")
	uid, err := cr.FindUserCredential(ctx, 0, authenticationV1.UserCredential_USERNAME, "zs", "NewPass@1234", false)
	require.NoError(t, err)
	require.Equal(t, newID, uid)
}

// 老库里已经存在的孤儿凭证（user_id 指向已删除用户）：登录查找要跳过它们。
func TestUserCredentialRepoSqlite_FindSkipsOrphanCredentials(t *testing.T) {
	ur, cr := newUserAndCredentialRepoSqlite(t)
	ctx := enttest.NewSystemContext(context.Background())

	// 模拟旧版删除：只有凭证，没有对应用户行
	require.NoError(t, cr.Create(ctx, &authenticationV1.CreateUserCredentialRequest{Data: &authenticationV1.UserCredential{
		UserId: trans.Ptr(uint32(9999)), TenantId: trans.Ptr(uint32(0)),
		IdentityType: authenticationV1.UserCredential_USERNAME.Enum(), Identifier: trans.Ptr("zs"),
		CredentialType: authenticationV1.UserCredential_PASSWORD_HASH.Enum(), Credential: trans.Ptr("OldPass@1234"),
		Status: authenticationV1.UserCredential_ENABLED.Enum(),
	}}))
	newID := createUserWithPassword(t, ur, cr, ctx, 0, "zs", "NewPass@1234")

	uid, err := cr.FindUserCredential(ctx, 0, authenticationV1.UserCredential_USERNAME, "zs", "NewPass@1234", false)
	require.NoError(t, err, "孤儿凭证不应让登录变成 503")
	require.Equal(t, newID, uid)

	_, err = cr.FindUserCredential(ctx, 0, authenticationV1.UserCredential_USERNAME, "zs", "OldPass@1234", false)
	require.Error(t, err, "孤儿凭证的旧密码不能登录")
}

// a1 F10：改密、重置按 user_id 定位（并固定租户 0）。遗留租户的同名凭证即使给出它的 user_id 也碰不到；
// 缺 user_id 直接拒绝。
func TestUserCredentialRepoSqlite_ChangeAndResetByUserID(t *testing.T) {
	repo := newUserCredentialRepoSqlite(t)
	sys := enttest.NewSystemContext(context.Background())
	for _, c := range []struct{ uid, tid uint32 }{{1, 0}, {2, 0}, {501, 5}} {
		identifier := "admin"
		if c.uid == 2 {
			identifier = "other"
		}
		require.NoError(t, repo.Create(sys, &authenticationV1.CreateUserCredentialRequest{Data: &authenticationV1.UserCredential{
			UserId: trans.Ptr(c.uid), TenantId: trans.Ptr(c.tid),
			IdentityType: authenticationV1.UserCredential_USERNAME.Enum(), Identifier: trans.Ptr(identifier),
			CredentialType: authenticationV1.UserCredential_PASSWORD_HASH.Enum(), Credential: trans.Ptr("Str0ng!Passw0rd#42"),
			Status: authenticationV1.UserCredential_ENABLED.Enum(),
		}}))
	}
	ctx := viewer.WithContext(context.Background(), viewer.NewUserContext(1, 0, 0, "", nil))

	// user_id 与 identifier 不匹配：不能借别人的 identifier 改到别的用户
	err := repo.ResetCredential(ctx, 2, &authenticationV1.ResetCredentialRequest{
		IdentityType: authenticationV1.UserCredential_USERNAME, Identifier: "admin", NewCredential: "R3set!Passw0rd#99",
	})
	require.True(t, authenticationV1.IsNotFound(err), "got %v", err)

	// 遗留租户 5 的凭证：不在租户 0，碰不到
	err = repo.ResetCredential(ctx, 501, &authenticationV1.ResetCredentialRequest{
		IdentityType: authenticationV1.UserCredential_USERNAME, Identifier: "admin", NewCredential: "R3set!Passw0rd#99",
	})
	require.True(t, authenticationV1.IsNotFound(err), "got %v", err)

	require.NoError(t, repo.ResetCredential(ctx, 1, &authenticationV1.ResetCredentialRequest{
		IdentityType: authenticationV1.UserCredential_USERNAME, Identifier: "admin", NewCredential: "R3set!Passw0rd#99",
	}))
	uid, err := repo.FindUserCredential(sys, 0, authenticationV1.UserCredential_USERNAME, "admin", "R3set!Passw0rd#99", false)
	require.NoError(t, err)
	require.Equal(t, uint32(1), uid)

	require.Error(t, repo.ResetCredential(ctx, 0, &authenticationV1.ResetCredentialRequest{
		IdentityType: authenticationV1.UserCredential_USERNAME, Identifier: "admin", NewCredential: "R3set!Passw0rd#98",
	}), "缺 user_id 应拒绝")
}
